package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
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
// are off, for a program that isn't there or is left out, with remote
// control, and in another folder; else it runs in its program's folder.
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
		"remote control": {proto.SessionStartParams{Agent: "claude", Role: proto.RoleDirector, Project: "acme", RemoteControl: true}, proto.ErrRefused},
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
