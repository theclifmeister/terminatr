package server

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// humanOnlyMethods are the methods proto.HumanOnly names.
var humanOnlyMethods = []string{
	proto.MethodProjectRename, proto.MethodProjectActive,
	proto.MethodModViews, proto.MethodModViewInput, proto.MethodModCommand, proto.MethodModRestart,
	proto.MethodNotifyClaim, proto.MethodNotifyDismiss,
	proto.MethodModActionCheck, proto.MethodModActionDone, proto.MethodSessionClear,
	proto.MethodSessionStart, proto.MethodSessionStop, proto.MethodSessionKeys, proto.MethodSessionPrompt,
	proto.MethodSessionAdopt, proto.MethodSessionRemote, proto.MethodSessionModel, proto.MethodQuestionsOpen,
	proto.MethodServerStop, proto.MethodAgentReload,
	proto.MethodViewSubscribe, proto.MethodViewAttach, proto.MethodViewDashboard, proto.MethodViewProject,
	proto.MethodViewSelect, proto.MethodViewSideSel, proto.MethodViewExpand, proto.MethodViewTree,
	proto.MethodViewSidebar, proto.MethodViewInfo, proto.MethodViewColumns, proto.MethodViewSize,
	proto.MethodViewInput,
}

// handledElsewhere are human-only methods the loop of TestHumanSocket
// doesn't run on the human socket: with empty params they would stop
// the server, start a shell in the home folder, or turn the connection
// into a view stream.
var handledElsewhere = map[string]bool{
	proto.MethodServerStop: true, proto.MethodSessionStart: true, proto.MethodViewSubscribe: true,
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

	srv, err := dialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	human, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer human.Close()
	if got := human.conn.RemoteAddr().String(); got != p.HumanSocket() {
		t.Fatalf("Dial connected to %q, want the human socket %q", got, p.HumanSocket())
	}

	for _, m := range humanOnlyMethods {
		if !proto.HumanOnly(m) {
			t.Errorf("proto.HumanOnly(%q) = false", m)
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
	for _, c := range []*Client{srv, human} {
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
	agent, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	if got := agent.conn.RemoteAddr().String(); got != p.Socket {
		t.Errorf("an agent's Dial connected to %q, want %q", got, p.Socket)
	}
	for _, m := range []string{proto.MethodCLIRun, proto.MethodCallerWho, proto.MethodHookEvent,
		proto.MethodSessionList, proto.MethodSessionRead, proto.MethodSessionWait, proto.MethodSessionAsk,
		proto.MethodToolRun, proto.MethodToolList, proto.MethodViewDigest} {
		if proto.HumanOnly(m) {
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
	srv, err := dialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	human, err := Dial(p, proto.KindControl)
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
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir()}, &started)
	id := started.Session.ID

	srv, err := dialAt(p.Socket, proto.KindAttach, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if err := writeJSONLine(srv.conn, proto.AttachRequest{Attach: proto.AttachParams{Session: id}}); err != nil {
		t.Fatal(err)
	}
	var reply proto.AttachReply
	if err := readJSONLine(srv.br, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Attached != nil || !onHumanSocketOnly(reply.Error) {
		t.Errorf("attach on the server socket: %+v, want the human-only refusal", reply)
	}

	a, info, err := Attach(p, proto.AttachParams{Session: id})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer a.Close()
	if got := a.conn.RemoteAddr().String(); got != p.HumanSocket() || info == nil || info.ID != id {
		t.Errorf("Attach connected to %q (%+v), want the human socket", got, info)
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
	} {
		who := func() caller.Caller { return c.c }
		if perr := mayCall(c.m, c.humanS, who); (perr == nil) != c.ok {
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
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		t.Fatalf("Dial without the human socket: %v", err)
	}
	defer c.Close()
	if got := c.conn.RemoteAddr().String(); got != p.Socket {
		t.Fatalf("Dial connected to %q, want %q", got, p.Socket)
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
	c, err := Dial(p, proto.KindControl)
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
