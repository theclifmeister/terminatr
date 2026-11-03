package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmokeDirector: a program's director (the programs design §4).
// With programs off, tm program refuses programs-off. On, the human
// makes a program and opens its director: a session of role director in
// the program's folder, its rules (tm skill director) in its system
// prompt and the program's context in its start-up block, the program's
// member listed. The director reads its own program's context and its
// members' tasks, but changes no member's board, and reads no other
// program's context. The member's coordinator gets the Program part and
// the program safety line; with programs off neither shows.
func TestSmokeDirector(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	tm := `"$TERMINATR_BIN" `
	env.scripts(map[string]string{"direct": `
[[step]]
do = "run"
cmd = '` + tm + `program context | grep -c "Program: studio" ; ` + tm + `task list --project demo; echo "list=$?"'

[[step]]
do = "run"
cmd = '` + tm + `task add "Sneak" --project demo; echo "add=$?"; ` + tm + `program context other; echo "other=$?"; echo DIRECT-DONE'
`})
	slug, dir := newProject(env, "demo")
	env.MustCLI("task", "add", "Ship the API", "--project", slug)

	// Off: the command and the director's rules refuse.
	if r := env.CLI("program", "new", "studio"); r.Code == 0 || !strings.Contains(r.Stderr, "programs-off") {
		t.Fatalf("program new while off: %+v", r)
	}
	if r := env.CLI("skill", "director"); r.Code == 0 || !strings.Contains(r.Stderr, "programs-off") {
		t.Fatalf("skill director while off: %+v", r)
	}
	coordOff := env.MustCLI("skill", "coordinator")
	if strings.Contains(coordOff, "director") {
		t.Fatalf("the coordinator's rules name the director while programs are off:\n%s", coordOff)
	}

	writeConfig(t, env, "[features]\nprograms = true\n[programs.studio]\nprojects = [\"demo\"]\n")
	var g struct{ Slug, Dir string }
	if err := json.Unmarshal([]byte(env.MustCLI("program", "new", "studio", "--goal", "One product", "--json")), &g); err != nil {
		t.Fatal(err)
	}
	if g.Dir != filepath.Join(env.Home, "projects", "_programs", "studio") {
		t.Fatalf("program folder %s", g.Dir)
	}
	if r := env.CLI("program", "new", slug); r.Code == 0 || !strings.Contains(r.Stderr, "slug-taken") {
		t.Fatalf("a program named as a project: %+v", r)
	}
	env.Trust(g.Dir, dir)

	id := strings.TrimSpace(env.MustCLI("program", "open", "studio"))
	if again := strings.TrimSpace(env.MustCLI("program", "open", "studio")); again != id {
		t.Fatalf("program open twice: %s, then %s", id, again)
	}
	d := &Session{ID: id}
	info, _ := env.Info(d)
	d.PID = info.PID
	env.track(info.PID, "director "+id)
	if info.Role != "director" || info.Project != "studio" || info.Cwd != g.Dir {
		t.Fatalf("director session %+v", info)
	}
	env.WaitState(d, "idle", agentWait)

	// Its rules in its system prompt, the program's state in the block.
	rules := env.MustCLI("skill", "director")
	env.WaitFake("start", agentWait, func(r FakeRecord) bool {
		b, err := os.ReadFile(r.Str("brief_path"))
		return err == nil && strings.HasPrefix(string(b), rules)
	})
	start := env.WaitFake("context", agentWait, func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "Program: studio") })
	for _, want := range []string{"Your rules (`tm skill director`) are in your system prompt", "Goal: One product", "## Children", "demo: ", "· tasks: 1 "} {
		if !strings.Contains(start.Str("text"), want) {
			t.Errorf("director's start-up block lacks %q:\n%s", want, start.Str("text"))
		}
	}

	env.Prompt(d, "run direct")
	env.WaitFor(d, "DIRECT-DONE", agentWait)
	sc := env.Screen(d)
	for _, want := range []string{"list=0", "Ship the API", "add=1", "other=1"} {
		if !strings.Contains(sc, want) {
			t.Errorf("director's screen lacks %q:\n%s", want, sc)
		}
	}
	if out := env.MustCLI("task", "list", "--project", slug); strings.Contains(out, "Sneak") {
		t.Fatalf("the director changed a member's board:\n%s", out)
	}

	// The member's coordinator: the Program part and the safety line.
	ctx := env.MustCLI("context", "--project", slug)
	if !strings.Contains(ctx, "## Program\nProgram: studio") || !strings.Contains(ctx, g.Dir+"/PROGRAM.md") {
		t.Fatalf("member's context lacks the Program part:\n%s", ctx)
	}
	if on := env.MustCLI("skill", "coordinator"); !strings.Contains(on, "program's director") || !strings.Contains(on, "\n## Program\n") {
		t.Fatalf("the coordinator's rules lack the program parts while on:\n%s", on)
	}

	// Off again: the parts go, and a new director doesn't start.
	writeConfig(t, env, "[programs.studio]\nprojects = [\"demo\"]\n")
	if ctx := env.MustCLI("context", "--project", slug); strings.Contains(ctx, "## Program") {
		t.Fatalf("member's context has the Program part while off:\n%s", ctx)
	}
	if got := env.MustCLI("skill", "coordinator"); got != coordOff {
		t.Fatalf("the coordinator's rules while off differ from before:\n%s", got)
	}
	if r := env.CLI("program", "open", "studio"); r.Code == 0 || !strings.Contains(r.Stderr, "programs-off") {
		t.Fatalf("program open while off: %+v", r)
	}
}
