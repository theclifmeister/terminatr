package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestDirectorGuardRules: a director is guarded as a coordinator is
// (no worktree-only, no merge rule), its floor every project's folder
// and the other programs' but not its own; with programs off it gets
// no rules, since no director runs then.
func TestDirectorGuardRules(t *testing.T) {
	s, root := programFixture(t)
	writeConfig(t, programsOn)
	own := filepath.Join(root, "acme")
	r := SessionRecord{ID: "s-9", Role: proto.RoleDirector, Project: "acme", Cwd: own, Agent: "claude"}
	g := s.guardRulesFor(r)
	if !g.On || g.Role != proto.RoleDirector {
		t.Fatalf("director rules: %+v", g)
	}
	if slices.Contains(g.Rules, "worktree-only") || slices.Contains(g.Rules, "merge") || g.Writable != nil || g.AccessWrites {
		t.Errorf("a director got a thread's rules: %v", g.Rules)
	}
	demo, _ := project.Dir("demo")
	for _, want := range []string{realPath(demo), filepath.Join(root, "beta")} {
		if !slices.Contains(g.NoWrite, want) {
			t.Errorf("floor lacks %s: %v", want, g.NoWrite)
		}
	}
	if slices.Contains(g.NoWrite, own) {
		t.Errorf("its own folder is in its floor: %v", g.NoWrite)
	}
	if !slices.Contains(g.NoWrite, filepath.Join(own, "TASKS.md")) {
		t.Errorf("tm's board isn't in its floor: %v", g.NoWrite)
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	if g := s.guardRulesFor(r); g.On {
		t.Errorf("rules for a director while programs are off: %+v", g)
	}
}

// TestDirectorCommands: the read-only commands and the program's own
// acts run unprompted; tm program start only under delegate; nothing
// of the user's word ever.
func TestDirectorCommands(t *testing.T) {
	s, _ := programFixture(t)
	writeConfig(t, programsOn)
	a, err := s.accessFor(proto.RoleDirector, "acme", nil, loadLaunchConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tm program context", "tm program route", "tm program send", "tm task list", "tm skill"} {
		if !slices.Contains(a.Commands, want) {
			t.Errorf("propose: %s isn't unprompted: %v", want, a.Commands)
		}
	}
	for _, c := range a.Commands {
		if c == "tm program start" || strings.HasPrefix(c, "tm task status") || strings.HasPrefix(c, "tm thread") && c != "tm thread list" && c != "tm thread show" {
			t.Errorf("propose: %s is unprompted", c)
		}
	}
	writeConfig(t, strings.Replace(programsOn, "projects = [\"demo\"]\n", "projects = [\"demo\"]\nauthority = \"delegate\"\n", 1))
	if cmds := directorCommands(loadLaunchConfig().cfg, "acme"); !slices.Contains(cmds, "tm program start") {
		t.Errorf("delegate: tm program start prompts: %v", cmds)
	}
	if cmds := directorCommands(loadLaunchConfig().cfg, "beta"); slices.Contains(cmds, "tm program start") {
		t.Errorf("another program's delegate reached beta: %v", cmds)
	}
}

// TestDirectorStart: session.start refuses a director while programs
// are off, for a program that isn't there or is left out, and in
// another folder; else it runs in its program's folder, with remote
// control too (a coordinator's parity, T962).
func TestDirectorStart(t *testing.T) {
	_, root := programFixture(t)
	p := proto.SessionStartParams{Agent: "claude", Role: proto.RoleDirector, Project: "acme"}
	code := func(p proto.SessionStartParams) string {
		_, perr := directorStart(p)
		if perr == nil {
			return ""
		}
		return perr.Code
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	if c := code(p); c != config.ProgramsOff {
		t.Errorf("off: %q", c)
	}
	writeConfig(t, programsOn)
	if dir, perr := directorStart(p); perr != nil || dir != filepath.Join(root, "acme") {
		t.Fatalf("start: %q %v", dir, perr)
	}
	for name, c := range map[string]struct {
		p    proto.SessionStartParams
		code string
	}{
		"no agent":       {proto.SessionStartParams{Role: proto.RoleDirector, Project: "acme"}, proto.ErrBadParams},
		"no program":     {proto.SessionStartParams{Agent: "claude", Role: proto.RoleDirector, Project: "gone"}, "unknown-program"},
		"remote control": {proto.SessionStartParams{Agent: "claude", Role: proto.RoleDirector, Project: "acme", RemoteControl: true}, ""},
		"elsewhere":      {proto.SessionStartParams{Agent: "claude", Role: proto.RoleDirector, Project: "acme", Cwd: t.TempDir()}, proto.ErrBadParams},
	} {
		if got := code(c.p); got != c.code {
			t.Errorf("%s: %q, want %q", name, got, c.code)
		}
	}
	// A project named as the program, made by hand: the project wins.
	projects := filepath.Dir(root)
	if err := os.MkdirAll(filepath.Join(projects, "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if c := code(p); c != "program-left-out" {
		t.Errorf("left out: %q", c)
	}
}

// TestScopeProgram: project.watch lets a project's own agents and its
// program's director through; a director names no project as its own
// (scopeProject), and reaches no project outside its program, nor any
// while programs are off.
func TestScopeProgram(t *testing.T) {
	programFixture(t)
	writeConfig(t, programsOn)
	dir := caller.Caller{Kind: caller.Director, Project: "acme", Session: "s-9"}
	watch := func(c caller.Caller, slug string) bool {
		req := proto.Request{Method: proto.MethodProjectWatch, Params: json.RawMessage(`{"project":"` + slug + `"}`)}
		_, perr := (&Server{}).mayAddress(req, methods[proto.MethodProjectWatch], false, func() caller.Caller { return c })
		return perr == nil
	}
	if !watch(dir, "demo") {
		t.Errorf("the director can't watch its member")
	}
	if watch(dir, "web") {
		t.Errorf("the director watches another program's member")
	}
	if watch(dir, "acme") {
		t.Errorf("the director watches its program's slug as a project")
	}
	if !watch(caller.Caller{Kind: caller.Coordinator, Project: "web"}, "web") || watch(caller.Caller{Kind: caller.Coordinator, Project: "web"}, "demo") {
		t.Errorf("a coordinator's scope changed")
	}
	search := proto.Request{Method: proto.MethodSearchQuery, Params: json.RawMessage(`{"project":"acme","query":"x"}`)}
	if _, perr := (&Server{}).mayAddress(search, methods[proto.MethodSearchQuery], false, func() caller.Caller { return dir }); perr == nil {
		t.Errorf("scopeProject let a director name its program's slug")
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	if watch(dir, "demo") {
		t.Errorf("the director watches its member while programs are off")
	}
}

// TestDirectorContext: a director's block is its rules (or the header
// saying they are in its system prompt) and the program's context;
// while programs are off, only a line saying so.
func TestDirectorContext(t *testing.T) {
	programFixture(t)
	writeConfig(t, programsOn)
	r := SessionRecord{Role: proto.RoleDirector, Project: "acme"}
	b, err := contextFor(r, "")()
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"tm skill director v", "## Program\nProgram: acme", "Members: demo", "## Children\ndemo: ", "## Umbrella tasks (TASKS.md)", "## Journal"} {
		if !strings.Contains(text, want) {
			t.Errorf("block lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Needs you (rolled up)") {
		t.Errorf("a Needs you part without a needs reader:\n%s", text)
	}
	needs := func(slug string) *proto.ProgramNeedsResult {
		return &proto.ProgramNeedsResult{Program: slug, Needs: []proto.ProgramNeed{{PeerNeed: proto.PeerNeed{ID: "review:T1:1", Kind: proto.NeedReview, Task: "T1", Title: "OIDC login"},
			Project: "demo", Priority: "must", Acts: []string{proto.ActAccept, proto.ActSendBack}}}}
	}
	b, _ = contextWith(r, "", needs)()
	if !strings.Contains(string(b), "## Needs you (rolled up)\nmust demo T1 review: OIDC login · acts: accept, send-back\n") ||
		strings.Index(string(b), "## Needs you") > strings.Index(string(b), "## Children") {
		t.Errorf("needs part:\n%s", b)
	}
	b, _ = contextWith(r, "", func(string) *proto.ProgramNeedsResult { return nil })()
	if !strings.Contains(string(b), "(not known now") {
		t.Errorf("unknown needs:\n%s", b)
	}
	r.Prompt = &PromptStamp{Version: "dev"}
	b, _ = contextFor(r, "")()
	if !strings.HasPrefix(string(b), "Your rules (`tm skill director`) are in your system prompt") || len(b) > project.StartupCut {
		t.Errorf("prompted block: %d bytes\n%s", len(b), b)
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	b, _ = contextFor(r, "")()
	if !strings.Contains(string(b), "Programs are off") || strings.Contains(string(b), "## Children") {
		t.Errorf("off: %s", b)
	}
}

// TestDirectorSessionActs: the programs design §10's session rows: a
// director prompts no session, approves no dialog and answers no
// thread's question menu, in its children or anywhere, whatever socket
// it reaches.
func TestDirectorSessionActs(t *testing.T) {
	dir := caller.Caller{Kind: caller.Director, Project: "acme", Session: "s-9"}
	who := func() caller.Caller { return dir }
	for _, m := range []string{proto.MethodSessionPrompt, proto.MethodSessionApprove} {
		for _, human := range []bool{false, true} {
			if perr := mayCall(m, methods[m].gate, human, who); perr == nil {
				t.Errorf("%s (human socket %v) let a director through", m, human)
			}
		}
	}
	child := proto.SessionInfo{ID: "s-2", Role: proto.RoleThread, Project: "demo"}
	for _, human := range []bool{false, true} {
		if perr := (&Server{}).mayAnswer(child, dir, human); perr == nil || perr.Code != "human-only" {
			t.Errorf("session.answer (human socket %v): %v", human, perr)
		}
	}
}

// TestDirectorParity: a director's save and clear, remote control and
// model setting go as a coordinator's (the programs design §4.1):
// coordinator.clear takes the program, session.remote and session.model
// take the director's session, and all refuse programs-off once the
// switch is off.
func TestDirectorParity(t *testing.T) {
	p := testPaths(t)
	newWatchProject(t)
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\"]\n")
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"clears\"\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n[inject]\nclear = \"/clear\"\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "clears.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	code := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return perr.Code
		}
		return ""
	}
	if err := c.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Program: "nope"}, nil); code(err) != "unknown-program" {
		t.Errorf("unknown program: %v", err)
	}
	if err := c.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: "demo", Program: "acme"}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("both: %v", err)
	}
	if err := c.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Program: "acme"}, nil); code(err) != proto.ErrRefused || !strings.Contains(err.Error(), "no director running") {
		t.Errorf("no director: %v", err)
	}
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "clears", Role: proto.RoleDirector, Project: "acme"}, &started)
	id := started.Session.ID
	var res proto.CoordinatorClearResult
	call(t, c, proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Program: "acme"}, &res)
	if res.Session != id {
		t.Errorf("asked %q, want %s", res.Session, id)
	}
	// The test agent has no remote control: refused for that, not for
	// being a director.
	if err := c.Call(proto.MethodSessionRemote, proto.SessionRemoteParams{ID: id, On: true}, nil); err == nil || !strings.Contains(err.Error(), "clears has no remote control") {
		t.Errorf("remote: %v", err)
	}
	var mres proto.SessionModelResult
	call(t, c, proto.MethodSessionModel, proto.SessionModelParams{ID: id}, &mres)
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	if err := c.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Program: "acme"}, nil); code(err) != config.ProgramsOff {
		t.Errorf("off: clear: %v", err)
	}
	if err := c.Call(proto.MethodSessionRemote, proto.SessionRemoteParams{ID: id, On: true}, nil); code(err) != config.ProgramsOff {
		t.Errorf("off: remote: %v", err)
	}
	if err := c.Call(proto.MethodSessionModel, proto.SessionModelParams{ID: id}, nil); code(err) != config.ProgramsOff {
		t.Errorf("off: model: %v", err)
	}
}

// TestProgramNeedsOpen: tm program needs --open stamps the director's
// session (needs_open), which a console showing its pane opens the needs
// dialog on; refused not-running without a director, and nothing
// waiting stamps nothing.
func TestProgramNeedsOpen(t *testing.T) {
	p := testPaths(t)
	demo := newWatchProject(t)
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\"]\n")
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"waits\"\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "waits.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var perr *proto.Error
	if err := c.Call(proto.MethodProgramNeedsOpen, proto.ProgramNeedsParams{Program: "acme"}, nil); !errors.As(err, &perr) || perr.Code != "not-running" {
		t.Errorf("no director: %v", err)
	}
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "waits", Role: proto.RoleDirector, Project: "acme"}, &started)
	id := started.Session.ID
	stamp := func() time.Time {
		var list proto.SessionListResult
		call(t, c, proto.MethodSessionList, nil, &list)
		for _, s := range list.Sessions {
			if s.ID == id {
				return s.NeedsOpen
			}
		}
		t.Fatalf("no director in the list")
		return time.Time{}
	}
	var res proto.ProgramNeedsOpenResult
	var needs proto.ProgramNeedsResult
	call(t, c, proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: "acme"}, &needs)
	if len(needs.Needs) == 0 {
		call(t, c, proto.MethodProgramNeedsOpen, proto.ProgramNeedsParams{Program: "acme"}, &res)
		if res.Waiting != 0 || !stamp().IsZero() {
			t.Errorf("nothing waiting: %+v, stamped %v", res, stamp())
		}
		if _, err := demo.Tasks().Add(caller.Caller{Kind: caller.Human}, []tasks.NewTask{{Title: "OIDC login", Status: "review"}}); err != nil {
			t.Fatal(err)
		}
	}
	call(t, c, proto.MethodProgramNeedsOpen, proto.ProgramNeedsParams{Program: "acme"}, &res)
	if res.Session != id || res.Waiting == 0 || stamp().IsZero() {
		t.Errorf("open: %+v, stamped %v", res, stamp())
	}
}
