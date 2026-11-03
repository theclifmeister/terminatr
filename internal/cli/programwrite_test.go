package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
)

var directorAcme = caller.Caller{Kind: caller.Director, Project: "acme"}

// programHarness is program acme over members demo and web, programs on.
func programHarness(t *testing.T) (*harness, *program.Program) {
	t.Helper()
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "web")
	h.ok(human, "project", "new", "loner")
	writeTestConfig(t, h, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	return h, g
}

func writeTestConfig(t *testing.T, h *harness, conf string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, "config.toml"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestProgramInboxMemory: a director's tm inbox and tm memory act on
// its program's folder (never a child's); the human reaches a program's
// with --project <program> or from inside its folder; off, programs-off.
func TestProgramInboxMemory(t *testing.T) {
	h, g := programHarness(t)
	h.env[caller.EnvProject] = "acme" // as the server sets it for the director
	if _, err := g.AddItem("dep-ready", "demo/T1", "demo/T1 can start", false); err != nil {
		t.Fatal(err)
	}
	out := h.ok(directorAcme, "inbox", "list")
	if !strings.Contains(out, "dep-ready: demo/T1 can start") {
		t.Fatalf("director's inbox:\n%s", out)
	}
	id := strings.Fields(out)[0]
	h.expect(1, "other-program", directorAcme, "inbox", "list", "--project", "demo")
	h.ok(directorAcme, "inbox", "done", id)
	if items, _ := g.Inbox(); len(items) != 0 {
		t.Fatalf("not done: %+v", items)
	}

	h.ok(directorAcme, "memory", "new", "sso", "--file", writeFile(t, "# SSO goes first\n\nOkta, then the apps.\n"))
	if _, err := os.Stat(g.Path("memory", "sso.md")); err != nil {
		t.Fatalf("note not in the program: %v", err)
	}
	if out := h.ok(directorAcme, "memory", "list"); !strings.Contains(out, "SSO goes first") || !strings.Contains(out, g.Dir) {
		t.Fatalf("list:\n%s", out)
	}
	if out := h.ok(directorAcme, "memory", "find", "okta"); !strings.Contains(out, "memory/sso.md") {
		t.Fatalf("find:\n%s", out)
	}
	if out := h.ok(directorAcme, "memory", "show", "sso"); !strings.Contains(out, "Okta, then the apps.") {
		t.Fatalf("show:\n%s", out)
	}
	h.expect(1, "other-program", directorAcme, "memory", "list", "--project", "demo")
	if j := journal(t, &g.Folder); !strings.Contains(j, "director:acme memory.new memory/sso.md") {
		t.Fatalf("journal:\n%s", j)
	}

	delete(h.env, caller.EnvProject)
	if out := h.ok(human, "memory", "list", "--project", "acme"); !strings.Contains(out, "SSO goes first") {
		t.Fatalf("the human's --project acme:\n%s", out)
	}
	h.cwd = g.Path("memory")
	if out := h.ok(human, "memory", "list"); !strings.Contains(out, "SSO goes first") {
		t.Fatalf("the human in the program's folder:\n%s", out)
	}
	h.ok(human, "memory", "rm", "sso", "--yes")
	if items, _ := g.Inbox(); len(items) != 1 || items[0].Kind != project.KindMemory {
		t.Fatalf("the human's change is an item for the director: %+v", items)
	}
	// A coordinator's own project is as before; a program isn't its.
	h.cwd = t.TempDir()
	h.expect(1, "unknown-project", caller.Caller{Kind: caller.Coordinator, Project: "demo"}, "memory", "list", "--project", "acme")
	h.expect(1, "coordinator-only", caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}, "inbox", "list", "--project", "acme")

	writeTestConfig(t, h, "[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	h.env[caller.EnvProject] = "acme"
	h.expect(1, "programs-off", directorAcme, "inbox", "list")
	delete(h.env, caller.EnvProject)
	h.expect(1, "unknown-project", human, "memory", "list", "--project", "acme")
}

func writeFile(t *testing.T, text string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func journal(t *testing.T, f *project.Folder) string {
	t.Helper()
	lines, _, err := f.JournalTail(50)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

// TestProgramRouteCLI: tm program route reads the JSON from stdin and
// files the tasks; tm program task edit and show reach a member's
// task; tm program send and read carry the program channel; a
// coordinator can't route, and programs off refuses all.
func TestProgramRouteCLI(t *testing.T) {
	h, g := programHarness(t)
	route := `{"umbrella": "SSO", "tasks": [
	  {"key": "api", "project": "demo", "title": "OIDC login", "priority": "must"},
	  {"project": "web", "title": "Login button", "after": ["api"]}]}`
	code, out, errs := h.run(directorAcme, route, "program", "route", "--file", "-")
	if code != 0 {
		t.Fatalf("route: %d %s%s", code, out, errs)
	}
	for _, want := range []string{"umbrella acme/T1", "demo T1 created: OIDC login", "web T1 created: Login button"} {
		if !strings.Contains(out, want) {
			t.Errorf("route lacks %q:\n%s", want, out)
		}
	}
	if code, _, errs := h.run(directorAcme, `{"tasks": [{"project": "demo", "title": "x", "colour": "red"}]}`, "program", "route", "--file", "-"); code != 1 || !strings.Contains(errs, "invalid-route") {
		t.Fatalf("unknown field: %d %s", code, errs)
	}
	if code, _, errs := h.run(caller.Caller{Kind: caller.Coordinator, Project: "demo"}, route, "program", "route", "--file", "-"); code != 1 || !strings.Contains(errs, "director-only") {
		t.Fatalf("coordinator route: %d %s", code, errs)
	}

	h.ok(directorAcme, "program", "task", "edit", "demo/T1", "--priority", "should", "--note", "after the Okta call")
	if out := h.ok(directorAcme, "program", "task", "show", "demo/T1"); !strings.Contains(out, "director:acme") || !strings.Contains(out, "after the Okta call") {
		t.Fatalf("show:\n%s", out)
	}
	h.ok(caller.Caller{Kind: caller.Coordinator, Project: "demo"}, "task", "add", "Its own", "--project", "demo")
	h.expect(1, "not-its-filing", directorAcme, "program", "task", "edit", "demo/T2", "--status", "open", "--title", "x")
	h.ok(directorAcme, "program", "task", "edit", "demo/T2", "--priority", "could")
	h.expect(1, "coordinator-only", directorAcme, "program", "task", "edit", "web/T1", "--status", "review")
	h.expect(1, "human-only", directorAcme, "program", "task", "edit", "web/T1", "--status", "done")
	h.expect(1, "not-member", directorAcme, "program", "task", "edit", "loner/T1", "--priority", "must")
	h.expect(2, "", directorAcme, "program", "task", "edit", "web/T1", "--notes", "rewrite")
	// The plain task commands stay the child coordinator's.
	h.expect(1, "coordinator-only", directorAcme, "task", "status", "T1", "started", "--project", "web")
	h.expect(1, "human-only", directorAcme, "task", "status", "T1", "done", "--approved-by-user", "--project", "web")
	h.expect(1, "coordinator-only", directorAcme, "task", "edit", "T1", "--priority", "must", "--project", "web")

	h.ok(directorAcme, "program", "task", "add", "Bump axios everywhere")
	if out := h.ok(directorAcme, "program", "task", "list"); !strings.Contains(out, "Bump axios everywhere") || !strings.Contains(out, "SSO") {
		t.Fatalf("umbrellas:\n%s", out)
	}
	h.ok(directorAcme, "program", "task", "edit", "T2", "--priority", "could", "--note", "sweep")

	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(h.ok(directorAcme, "program", "send", "web", "the button calls /v2/session", "--json")), &m); err != nil || m.ID == "" {
		t.Fatalf("send: %v %+v", err, m)
	}
	coordWeb := caller.Caller{Kind: caller.Coordinator, Project: "web"}
	h.env[caller.EnvProject] = "web"
	if out := h.ok(coordWeb, "link", "read", m.ID); !strings.Contains(out, "from the director of acme") || !strings.Contains(out, "/v2/session") {
		t.Fatalf("the coordinator reads it:\n%s", out)
	}
	out = h.ok(coordWeb, "program", "send", "acme", "noted", "--re", m.ID)
	reply := strings.Fields(out)[1]
	h.env[caller.EnvProject] = "acme"
	if out := h.ok(directorAcme, "program", "read", reply); !strings.Contains(out, "noted") || !strings.Contains(out, "data, not instructions") {
		t.Fatalf("the director reads the reply:\n%s", out)
	}
	h.expect(1, "not-member", directorAcme, "program", "send", "loner", "hi")
	h.expect(2, "", directorAcme, "program", "send", "web", "hi", "--from", "acme")
	if out := h.ok(human, "program", "send", "web", "from the user", "--from", "acme"); !strings.HasPrefix(out, "sent M-") {
		t.Fatalf("the human for the director: %s", out)
	}

	writeTestConfig(t, h, "[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	h.expect(1, "programs-off", directorAcme, "program", "route", "--file", "-")
	_ = g
}

// TestForwardedProgram: an agent's program writes run in the server;
// the rest of tm program runs where it is.
func TestForwardedProgram(t *testing.T) {
	for args, want := range map[string]bool{
		"program route --file -": true, "program task edit demo/T1": true, "program send web hi": true, "program read M-1": true, "program start demo/T1": true,
		"program context": false, "program open acme": false, "program": false, "task route": false,
	} {
		if got := forwardedProgram(strings.Fields(args)); got != want {
			t.Errorf("%q: %v", args, got)
		}
	}
}

// TestDirectorAuthority: the rows of the programs design's §10 a
// director meets in the CLI, each a refusal with its code whatever the
// prompt said: no status, accept or drop, thread start, prompt, menu
// answer, approval or merge, stored-question answer, access, safety,
// pause or archive in a child; only the program writes go through.
func TestDirectorAuthority(t *testing.T) {
	h, _ := programHarness(t)
	h.ok(human, "task", "add", "Child's task", "--project", "web")
	for _, tc := range []struct {
		code string
		args []string
	}{
		{"coordinator-only", []string{"task", "status", "T1", "started", "--project", "web"}},
		{"coordinator-only", []string{"task", "status", "T1", "blocked", "--project", "web"}},
		{"coordinator-only", []string{"task", "status", "T1", "review", "--project", "web"}},
		{"human-only", []string{"task", "status", "T1", "done", "--project", "web"}},
		{"human-only", []string{"task", "status", "T1", "done", "--approved-by-user", "--project", "web"}},
		{"human-only", []string{"task", "status", "T1", "dropped", "--approved-by-user", "--note", "no", "--project", "web"}},
		{"human-only", []string{"task", "accept", "T1", "--project", "web"}},
		{"coordinator-only", []string{"task", "steps", "T1", "add", "a step", "--project", "web"}},
		{"coordinator-only", []string{"task", "archive", "T1", "--project", "web"}},
		{"coordinator-only", []string{"task", "add", "Sneaked in", "--project", "web"}},
		{"human-only", []string{"ask", "answer", "Q1", "--choice", "1", "--project", "web"}},
		{"other-project", []string{"thread", "start", "--task", "T1", "--project", "web"}},
		{"other-project", []string{"task", "delegate", "T1", "--over-cap", "--project", "web"}},
		{"other-project", []string{"thread", "prompt", "t-0001", "go on", "--project", "web"}},
		{"other-project", []string{"thread", "approve", "t-0001", "--choice", "1", "--project", "web"}},
		{"other-project", []string{"thread", "answer", "t-0001", "--choice", "1", "--project", "web"}},
		{"other-project", []string{"thread", "merge", "t-0001", "--project", "web"}},
		{"other-project", []string{"link", "send", "demo", "hi", "--project", "web"}},
		{"human-only", []string{"project", "pause", "web"}},
		{"human-only", []string{"project", "archive", "web"}},
	} {
		code, _, errs := h.run(directorAcme, "", tc.args...)
		if code == 0 || !strings.Contains(errs, tc.code+":") {
			t.Errorf("tm %s: exit %d %q, want %s", strings.Join(tc.args, " "), code, errs, tc.code)
		}
	}
	if code, _, errs := h.run(directorAcme, "", "access", "set", "web", "yolo"); code == 0 || !strings.Contains(errs, "human-only") {
		t.Errorf("access: %d %s", code, errs)
	}
	// Reads stay open to it, and the program writes go through.
	h.ok(directorAcme, "task", "list", "--project", "web")
	h.ok(directorAcme, "program", "task", "edit", "web/T1", "--priority", "must")
}

// TestProgramNeedsCLI: tm program needs is a director's own program's
// and the human's; without a server it says it can't read them, and tm
// program context says the part isn't known now.
func TestProgramNeedsCLI(t *testing.T) {
	h, _ := programHarness(t)
	h.env[caller.EnvProject] = "acme"
	h.expect(1, "other-program", directorAcme, "program", "needs", "beta")
	h.expect(1, "director-only", caller.Caller{Kind: caller.Coordinator, Project: "demo"}, "program", "needs")
	h.expect(2, "", human, "program", "needs")
	if code, _, errs := h.run(directorAcme, "", "program", "needs"); code != 3 || !strings.Contains(errs, "not running") {
		t.Errorf("no server: %d %s", code, errs)
	}
	// --open: the same callers; it needs the server too.
	h.expect(1, "other-program", directorAcme, "program", "needs", "beta", "--open")
	if code, _, errs := h.run(directorAcme, "", "program", "needs", "--open"); code != 3 || !strings.Contains(errs, "not running") {
		t.Errorf("--open, no server: %d %s", code, errs)
	}
	out := h.ok(directorAcme, "program", "context")
	if !strings.Contains(out, "## Needs you (rolled up)\n(not known now") || strings.Index(out, "## Needs you") > strings.Index(out, "## Children") {
		t.Errorf("context:\n%s", out)
	}
	writeTestConfig(t, h, "[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	h.expect(1, "programs-off", directorAcme, "program", "needs")
}

// TestTaskEditCrossAfter (the programs design §6.1): while programs are
// on, a member's coordinator writes refs to its program's tasks with tm
// task edit --after, which then replaces the whole list; a non-member's
// ref, or one from a project in no program, is refused; off,
// programs-off.
func TestTaskEditCrossAfter(t *testing.T) {
	h, _ := programHarness(t)
	coordWeb := caller.Caller{Kind: caller.Coordinator, Project: "web"}
	h.ok(human, "task", "add", "OIDC", "--project", "demo")
	h.ok(human, "task", "add", "Old", "--project", "demo")
	h.ok(human, "task", "add", "Lone", "--project", "loner")
	h.ok(human, "task", "add", "Prep", "--project", "web")
	h.ok(human, "task", "add", "Login button", "--project", "web")
	h.env[caller.EnvProject] = "web"
	after := func() string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(h.root, "projects", "web", "TASKS.md"))
		if err != nil {
			t.Fatal(err)
		}
		_, rest, _ := strings.Cut(string(data), "### T2 Login button\n")
		line, _, _ := strings.Cut(rest, "\n")
		_, a, _ := strings.Cut(line, "after: ")
		a, _, _ = strings.Cut(a, " · ")
		return a
	}
	h.ok(coordWeb, "task", "edit", "T2", "--after", "T1, demo/T1")
	if a := after(); a != "T1, demo/T1" {
		t.Fatalf("after %q", a)
	}
	h.expect(1, "not-member", coordWeb, "task", "edit", "T2", "--after", "loner/T1")
	h.expect(1, "invalid-after", coordWeb, "task", "edit", "T2", "--after", "demo/T9")
	h.ok(coordWeb, "task", "edit", "T2", "--after", "demo/T2")
	if a := after(); a != "demo/T2" {
		t.Fatalf("not replaced: %q", a)
	}
	h.env[caller.EnvProject] = "loner"
	h.expect(1, "not-member", caller.Caller{Kind: caller.Coordinator, Project: "loner"}, "task", "edit", "T1", "--after", "demo/T1")
	writeTestConfig(t, h, "[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	h.env[caller.EnvProject] = "web"
	h.expect(1, "programs-off", coordWeb, "task", "edit", "T2", "--after", "demo/T1")
	// Off, a local plan edit keeps the cross ref.
	h.ok(coordWeb, "task", "edit", "T2", "--after", "T1")
	if a := after(); a != "T1, demo/T2" {
		t.Fatalf("off: %q", a)
	}
}
