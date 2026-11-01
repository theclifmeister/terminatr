package server

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// humanOnlyMethods are the methods whose gate is gateHuman.
var humanOnlyMethods = []string{
	proto.MethodProjectRename, proto.MethodProjectActive,
	proto.MethodModViews, proto.MethodModViewInput, proto.MethodModCommand, proto.MethodModRestart,
	proto.MethodLogList, proto.MethodLogRead, proto.MethodPaneVT, proto.MethodTranscriptWatch, proto.MethodTranscriptPage, proto.MethodSessionFile,
	proto.MethodNotifyClaim, proto.MethodNotifyDismiss,
	proto.MethodModActionCheck, proto.MethodModActionDone, proto.MethodSessionClear, proto.MethodSecretTest,
	proto.MethodSessionStart, proto.MethodSessionStop, proto.MethodSessionKeys, proto.MethodSessionPrompt, proto.MethodSessionUpload, proto.MethodSessionApprove, proto.MethodTaskAsk,
	proto.MethodTaskAccept, proto.MethodTaskSendBack, proto.MethodTaskDelegate, proto.MethodTaskAdd, proto.MethodTaskGet, proto.MethodTaskDone, proto.MethodTaskTemplates, proto.MethodDesignList, proto.MethodProjectOpen,
	proto.MethodMemoryList, proto.MethodMemoryRead, proto.MethodLibraryList, proto.MethodLibraryRead,
	proto.MethodSessionAdopt, proto.MethodSessionAdoptAsk, proto.MethodSessionRemote, proto.MethodSessionModel, proto.MethodQuestionsOpen, proto.MethodCoordinatorClear,
	proto.MethodServerStop, proto.MethodAgentReload, proto.MethodPeerReload, proto.MethodDeviceReload, proto.MethodPeerList, proto.MethodPeerPopupRead, proto.MethodPeerPagesRead, proto.MethodPeerLibraryList, proto.MethodPeerLibraryRead, proto.MethodPeerDesignList, proto.MethodPeerNeedAct, proto.MethodPeerShareAsk, proto.MethodPeerShareAnswer, proto.MethodLinkAct, proto.MethodProjectsWatch, proto.MethodSessionsWatch,
	proto.MethodProjectPin, proto.MethodProjectNew, proto.MethodSessionAdoptRequest, proto.MethodLinkSend,
	proto.MethodModsList, proto.MethodModsSet, proto.MethodModsSetting,
	proto.MethodViewSubscribe, proto.MethodViewAttach, proto.MethodViewDashboard, proto.MethodViewProject,
	proto.MethodViewSelect, proto.MethodViewSideSel, proto.MethodViewExpand, proto.MethodViewTree, proto.MethodViewSections,
	proto.MethodViewSidebar, proto.MethodViewInfo, proto.MethodViewColumns, proto.MethodViewSize,
	proto.MethodViewInput, proto.MethodViewStatus,
}

// notHumanOnly are the control methods that aren't human-only, with
// their gates (0: anyone): a method that leaves humanOnlyMethods has to
// be named here, with the gate it gets instead.
var notHumanOnly = map[string]gate{
	proto.MethodPing: 0, proto.MethodCallerWho: 0, proto.MethodServerStatus: 0,
	proto.MethodServerKeychain: 0, proto.MethodServerCodeHost: 0,
	proto.MethodAgentList: 0, proto.MethodAgentExplain: 0, proto.MethodModHosts: 0,
	proto.MethodHookEvent: 0, proto.MethodToolRun: 0, proto.MethodToolList: 0,
	proto.MethodSessionList: 0, proto.MethodSessionRead: 0, proto.MethodSessionWait: 0,
	proto.MethodSessionWatch: 0, proto.MethodProjectWatch: 0,
	// Gated in its handler (mayAnswer, mayShowAccess, mayExplainAccess).
	proto.MethodSessionAnswer: 0,
	proto.MethodAccessShow:    0,
	proto.MethodAccessExplain: 0,
	proto.MethodCLIRun:        gateAgentOrHuman,
	// Only a thread, checked in its handler (runRecord).
	proto.MethodCommandRun: gateAgentOrHuman,
	proto.MethodViewDigest: gateNotAgent,
	// tm search: an agent its own project (scopeProject), the human any.
	proto.MethodSearchQuery:    gateAgentOrHuman,
	proto.MethodSearchTimeline: gateAgentOrHuman,
	proto.MethodSearchShow:     gateAgentOrHuman,
}

// notControl are proto's methods of the peer link (internal/peer), not
// of the control socket.
var notControl = map[string]bool{
	proto.MethodPeerProjects: true, proto.MethodPeerWatch: true, proto.MethodPeerAttach: true,
	proto.MethodPeerPanes: true, proto.MethodPeerName: true, proto.MethodPeerMessage: true, proto.MethodPeerPopup: true,
	proto.MethodPeerFileFetch: true, proto.MethodPeerTranscript: true,
	proto.MethodPeerNeeds: true, proto.MethodPeerNonce: true, proto.MethodPeerAct: true,
	proto.MethodPeerPages: true, proto.MethodPeerPagesLibrary: true, proto.MethodPeerPagesLibraryRead: true, proto.MethodPeerPagesDesigns: true,
}

// gateOf is method m's gate, an attach connection's among them.
func gateOf(m string) gate {
	if m == attachMethod {
		return gateHuman
	}
	e, _ := methodOf(m)
	return e.gate
}

// handledElsewhere are human-only methods the loop of TestHumanSocket
// doesn't run on the human socket: with empty params they would stop
// the server, start a shell in the home folder, or turn the connection
// into a stream (a refused one ends it).
var handledElsewhere = map[string]bool{
	proto.MethodServerStop: true, proto.MethodSessionStart: true, proto.MethodViewSubscribe: true,
	proto.MethodProjectsWatch: true, proto.MethodSessionsWatch: true, proto.MethodPaneVT: true, proto.MethodTranscriptWatch: true,
}

// onHumanSocketOnly reports whether err is the server socket's refusal
// of a human-only method.
func onHumanSocketOnly(err error) bool {
	var perr *proto.Error
	return errors.As(err, &perr) && perr.Code == "human-only" && strings.Contains(perr.Message, ipc.HumanName)
}

// TestHumanSocket: the server listens on the human socket beside its
// own; the human-only methods, mod.action.check's press tokens among
// them, answer there alone, and every other method on both. A control
// Dial takes the human socket.
func TestHumanSocket(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	if fi, err := os.Stat(p.HumanSocket()); err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("human socket %s: %v %v", p.HumanSocket(), fi, err)
	}

	srv, err := client.DialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	human, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer human.Close()
	if got := human.RemoteAddr().String(); got != p.HumanSocket() {
		t.Fatalf("client.Dial connected to %q, want the human socket %q", got, p.HumanSocket())
	}

	for _, m := range humanOnlyMethods {
		if gateOf(m) != gateHuman {
			t.Errorf("%s isn't human-only", m)
		}
		if err := srv.Call(m, struct{}{}, nil); !onHumanSocketOnly(err) {
			t.Errorf("%s on the server socket: %v, want the human-only refusal", m, err)
		}
		// The handler answers: what it says of empty params doesn't
		// matter here, only that it isn't the socket's refusal.
		if handledElsewhere[m] {
			continue
		}
		if err := human.Call(m, struct{}{}, nil); onHumanSocketOnly(err) {
			t.Errorf("%s on the human socket: %v", m, err)
		}
	}
	// The connection outlives a refusal, and the rest answer on both.
	for _, c := range []*client.Client{srv, human} {
		if err := c.Call(proto.MethodPing, nil, nil); err != nil {
			t.Errorf("ping: %v", err)
		}
	}
	// session.answer: the human answers on the human socket only.
	var started proto.SessionStartResult
	call(t, human, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir()}, &started)
	ans := proto.SessionAnswerParams{ID: started.Session.ID, Answer: "free text"}
	var perr *proto.Error
	if err := srv.Call(proto.MethodSessionAnswer, ans, nil); !errors.As(err, &perr) || perr.Code != "human-only" {
		t.Errorf("session.answer on the server socket: %v, want human-only", err)
	}
	if err := human.Call(proto.MethodSessionAnswer, ans, nil); !errors.As(err, &perr) || perr.Code != proto.ErrNoQuestion {
		t.Errorf("session.answer on the human socket: %v, want %s", err, proto.ErrNoQuestion)
	}

	// An agent's session doesn't try the human socket.
	t.Setenv(caller.EnvSession, "s-1")
	t.Setenv(caller.EnvRole, string(caller.Thread))
	agent, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if got := agent.RemoteAddr().String(); got != p.Socket {
		t.Errorf("an agent's client.Dial connected to %q, want %q", got, p.Socket)
	}
	for _, m := range []string{proto.MethodCLIRun, proto.MethodCallerWho, proto.MethodHookEvent,
		proto.MethodSessionList, proto.MethodSessionRead, proto.MethodSessionWait,
		proto.MethodToolRun, proto.MethodToolList, proto.MethodViewDigest} {
		if gateOf(m) == gateHuman {
			t.Errorf("an agent's own method %s is human-only", m)
		}
	}
}

// TestHumanSocketCLIRun: cli.run from a process the pid walk calls the
// human answers on the human socket only; on the server socket it would
// run project commands as the human.
func TestHumanSocketCLIRun(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	srv, err := client.DialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	human, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer human.Close()
	run := proto.CLIRunParams{Args: []string{"project", "list"}}
	if err := srv.Call(proto.MethodCLIRun, run, nil); !onHumanSocketOnly(err) {
		t.Errorf("cli.run from the human on the server socket: %v, want the human-only refusal", err)
	}
	// The test server runs no project commands: past the gate, it says so.
	var perr *proto.Error
	if err := human.Call(proto.MethodCLIRun, run, nil); !errors.As(err, &perr) || perr.Code != proto.ErrRefused {
		t.Errorf("cli.run on the human socket: %v, want %s", err, proto.ErrRefused)
	}
}

// TestHumanSocketAttach: attach answers on the human socket only, and
// Attach dials it.
func TestHumanSocketAttach(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir()}, &started)
	id := started.Session.ID

	srv, err := client.DialAt(p.Socket, proto.KindAttach, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if err := srv.WriteLine(proto.AttachRequest{Attach: proto.AttachParams{Session: id}}); err != nil {
		t.Fatal(err)
	}
	var reply proto.AttachReply
	if err := srv.ReadLine(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.Attached != nil || !onHumanSocketOnly(reply.Error) {
		t.Errorf("attach on the server socket: %+v, want the human-only refusal", reply)
	}

	a, info, err := client.Attach(p, proto.AttachParams{Session: id})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer a.Close()
	if got := a.RemoteAddr().String(); got != p.HumanSocket() || info == nil || info.ID != id {
		t.Errorf("client.Attach connected to %q (%+v), want the human socket", got, info)
	}
}

// TestMayCall: the human-only methods and attach answer on the human
// socket to a caller outside every agent's session; cli.run on the
// server socket only to an agent the pid walk finds.
func TestMayCall(t *testing.T) {
	human := caller.Caller{Kind: caller.Human}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "p"}
	thread := caller.Caller{Kind: caller.Thread, Project: "p", Thread: "t-0001"}
	for _, c := range []struct {
		m      string
		humanS bool
		c      caller.Caller
		ok     bool
	}{
		{proto.MethodSessionKeys, true, human, true},
		{proto.MethodSessionKeys, false, human, false},
		{proto.MethodSessionKeys, true, coord, false},
		{proto.MethodSessionStart, false, thread, false},
		{proto.MethodSessionStart, true, thread, false},
		{proto.MethodViewSelect, true, coord, false},
		{proto.MethodViewSelect, true, human, true},
		{proto.MethodServerStop, false, human, false},
		{attachMethod, false, human, false},
		{attachMethod, true, thread, false},
		{attachMethod, true, human, true},
		{proto.MethodCLIRun, false, thread, true},
		{proto.MethodCLIRun, false, coord, true},
		{proto.MethodCLIRun, false, human, false},
		{proto.MethodCLIRun, true, human, true},
		{proto.MethodCLIRun, true, thread, true},
		{proto.MethodSessionRead, false, thread, true},
		{proto.MethodHookEvent, false, human, true},
		{proto.MethodViewDigest, false, human, true},
		{proto.MethodViewDigest, true, human, true},
		{proto.MethodViewDigest, false, thread, false},
		{proto.MethodViewDigest, true, coord, false},
	} {
		who := func() caller.Caller { return c.c }
		if perr := mayCall(c.m, gateOf(c.m), c.humanS, who); (perr == nil) != c.ok {
			t.Errorf("%s (human socket %v) from %v: %v, want ok %v", c.m, c.humanS, c.c.Kind, perr, c.ok)
		}
	}
}

// TestHumanSocketFallback: where the human socket can't be reached (an
// agent's sandbox refuses it, an older server has none) a control Dial
// uses the server socket, which still serves everything but the
// human-only methods.
func TestHumanSocketFallback(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	if err := os.Remove(p.HumanSocket()); err != nil {
		t.Fatal(err)
	}
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatalf("client.Dial without the human socket: %v", err)
	}
	defer c.Close()
	if got := c.RemoteAddr().String(); got != p.Socket {
		t.Fatalf("client.Dial connected to %q, want %q", got, p.Socket)
	}
	if err := c.Call(proto.MethodPing, nil, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := c.Call(proto.MethodModActionCheck, proto.ModActionParams{Mod: "m", Action: "a"}, nil); !onHumanSocketOnly(err) {
		t.Fatalf("mod.action.check on the server socket: %v", err)
	}
}

// TestMayAnswer: session.answer is the human's, on the human socket, or
// the coordinator's of the session's own project for its threads.
func TestMayAnswer(t *testing.T) {
	s := &Server{}
	thread := proto.SessionInfo{ID: "s-2", Role: proto.RoleThread, Project: "p"}
	coordSess := proto.SessionInfo{ID: "s-1", Role: proto.RoleCoordinator, Project: "p"}
	loose := proto.SessionInfo{ID: "s-3", Role: proto.RoleShell}
	human := caller.Caller{Kind: caller.Human}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "p"}
	for _, c := range []struct {
		name   string
		in     proto.SessionInfo
		c      caller.Caller
		humanS bool
		ok     bool
	}{
		{"human on the human socket", thread, human, true, true},
		{"human on the human socket, a coordinator's menu", coordSess, human, true, true},
		{"human on the server socket", thread, human, false, false},
		{"the project's coordinator", thread, coord, false, true},
		{"the project's coordinator on the human socket", thread, coord, true, true},
		{"a coordinator answering a coordinator's menu", coordSess, coord, false, false},
		{"another project's coordinator", thread, caller.Caller{Kind: caller.Coordinator, Project: "q"}, true, false},
		{"a coordinator, a session outside the projects", loose, caller.Caller{Kind: caller.Coordinator}, true, false},
		{"a thread", thread, caller.Caller{Kind: caller.Thread, Project: "p", Thread: "t-0001"}, true, false},
		{"a mod", thread, caller.ForMod("m"), true, false},
	} {
		if perr := s.mayAnswer(c.in, c.c, c.humanS); (perr == nil) != c.ok {
			t.Errorf("%s: %v, want ok %v", c.name, perr, c.ok)
		}
	}
}

// TestNoSandboxEscapesReachesLaunch: session.start's no_sandbox_escapes
// (tm thread start's sandbox_escapes = false) reaches the agent's launch
// spec, where Claude's manifest turns it into allowUnsandboxedCommands.
func TestNoSandboxEscapesReachesLaunch(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `manifest_version = 1
name = "esc"
[launch]
command = ` + strconv.Quote(testSh(t)) + `
args = ["-c", "sleep 30"]
[[launch.files]]
path = "esc"
template = "{{.NoSandboxEscapes}}"
`
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "esc.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, off := range []bool{false, true} {
		var started proto.SessionStartResult
		call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "esc", Cwd: t.TempDir(), NoSandboxEscapes: off}, &started)
		b, err := os.ReadFile(filepath.Join(p.RunDir, "s", started.Session.ID, "esc"))
		if err != nil || string(b) != strconv.FormatBool(off) {
			t.Errorf("no_sandbox_escapes %v: launch file %q, %v", off, b, err)
		}
	}
}

// TestMethodTable: every method the table names is either answered or
// streamed, a scoped one says what names its scope, and the human-only
// ones are the ones TestHumanSocket checks on both sockets.
func TestMethodTable(t *testing.T) {
	listed := map[string]bool{}
	for _, m := range humanOnlyMethods {
		listed[m] = true
	}
	for name, m := range methods {
		if (m.call == nil) == (m.stream == nil) {
			t.Errorf("%s: call %v, stream %v: want one", name, m.call != nil, m.stream != nil)
		}
		if (m.scope == scopeNone) != (m.name == nil) {
			t.Errorf("%s: scope %d with name %v", name, m.scope, m.name != nil)
		}
		if m.gate == gateHuman && !listed[name] {
			t.Errorf("human-only %s is missing from humanOnlyMethods", name)
		}
	}
	if m, ok := methodOf("view.nosuch"); !ok || m.gate != gateHuman {
		t.Errorf("an unknown view method: %+v %v, want the human's", m, ok)
	}
	if _, ok := methodOf("no.such.method"); ok {
		t.Error("an unknown method is known")
	}
}

// TestTypedTakesDecodedParams: a handler takes the params mayAddress
// decoded when they are its own type, without decoding the request
// again; other params, or none, it decodes itself.
func TestTypedTakesDecodedParams(t *testing.T) {
	h := typed(func(_ *Server, p proto.SessionIDParams, _ callCtx) (any, *proto.Error) { return p.ID, nil })
	bad := proto.Request{Params: json.RawMessage(`not json`)}
	if got, perr := h(&Server{}, bad, callCtx{params: proto.SessionIDParams{ID: "s-1"}}); perr != nil || got != "s-1" {
		t.Fatalf("decoded params: %v %v", got, perr)
	}
	if _, perr := h(&Server{}, bad, callCtx{params: proto.SessionReadParams{ID: "s-2"}}); perr == nil || perr.Code != proto.ErrBadParams {
		t.Fatalf("another type's params: %v, want the request decoded", perr)
	}
	good := proto.Request{Params: json.RawMessage(`{"id":"s-3"}`)}
	if got, perr := h(&Server{}, good, callCtx{}); perr != nil || got != "s-3" {
		t.Fatalf("no decoded params: %v %v", got, perr)
	}
	// mayAddress hands on what it decoded.
	m := methods[proto.MethodSessionRead]
	params, perr := (&Server{}).mayAddress(proto.Request{Method: proto.MethodSessionRead, Params: json.RawMessage(`{"id":"s-4"}`)}, m, true,
		func() caller.Caller { return caller.Caller{Kind: caller.Human} })
	if p, ok := params.(proto.SessionReadParams); perr != nil || !ok || p.ID != "s-4" {
		t.Fatalf("mayAddress: %#v %v", params, perr)
	}
}

// TestEveryMethodHasItsGate: every Method constant of package proto is
// human-only (humanOnlyMethods), another caller's with the gate
// notHumanOnly gives it, or the peer link's; a new method has to be
// put in one of them, and one that changes its gate fails here.
func TestEveryMethodHasItsGate(t *testing.T) {
	human := map[string]bool{}
	for _, m := range humanOnlyMethods {
		human[m] = true
	}
	consts := protoMethods(t)
	for name, m := range consts {
		want, other := notHumanOnly[m]
		switch {
		case notControl[m]:
			if _, ok := methodOf(m); ok {
				t.Errorf("%s (%s) is the peer link's, and the control table answers it", name, m)
			}
			continue
		case human[m] == other:
			t.Errorf("%s (%s): in humanOnlyMethods %v, in notHumanOnly %v; want exactly one", name, m, human[m], other)
			continue
		case human[m]:
			want = gateHuman
		}
		e, ok := methodOf(m)
		if !ok {
			t.Errorf("%s (%s) is not in the method table", name, m)
		} else if e.gate != want {
			t.Errorf("%s (%s): gate %d, want %d", name, m, e.gate, want)
		}
	}
	known := map[string]bool{}
	for _, m := range consts {
		known[m] = true
	}
	for m := range methods {
		if !known[m] {
			t.Errorf("the table's %s is no Method constant of package proto", m)
		}
	}
}

// protoMethods are package proto's string constants named Method*, by
// name, read from its source.
func protoMethods(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	names, err := filepath.Glob(filepath.Join("..", "proto", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	out := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if !strings.HasPrefix(n.Name, "Method") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("proto.%s is not a string literal", n.Name)
					}
					out[n.Name], _ = strconv.Unquote(lit.Value)
				}
			}
		}
	}
	if len(out) < 40 {
		t.Fatalf("found %d Method constants in package proto; the parse missed some", len(out))
	}
	return out
}
