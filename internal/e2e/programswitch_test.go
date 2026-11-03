package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// directorIDs are the running director sessions of program slug.
func directorIDs(env *Env, slug string) []string {
	var out []string
	for _, s := range env.Sessions() {
		if s.Role == "director" && s.Project == slug {
			out = append(out, s.ID)
		}
	}
	return out
}

// TestSmokeProgramsSwitch: the programs switch (the programs design
// §3.5). Turning programs off by hand in config.toml stops the running
// director at the next sweep, its record kept dormant, also across a
// server restart; tm program status says what is dormant and tm program
// open refuses. Turning them back on resumes the director under its id,
// told once that programs are back on with what its child did
// meanwhile, and tm program delete refuses while it runs.
func TestSmokeProgramsSwitch(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	env.Setenv("TERMINATR_TICK_SWEEP", "1s")
	slug, dir := newProject(env, "demo")
	on := "[features]\nprograms = true\n[programs.studio]\nprojects = [\"demo\"]\n"
	off := "[programs.studio]\nprojects = [\"demo\"]\n"
	writeConfig(t, env, on)
	var g struct{ Dir string }
	if err := json.Unmarshal([]byte(env.MustCLI("program", "new", "studio", "--json")), &g); err != nil {
		t.Fatal(err)
	}
	env.Trust(g.Dir, dir)
	id := strings.TrimSpace(env.MustCLI("program", "open", "studio"))
	d := &Session{ID: id}
	info, _ := env.Info(d)
	env.track(info.PID, "director "+id)
	env.WaitState(d, "idle", agentWait)
	if out := env.MustCLI("program", "status"); !strings.HasPrefix(out, "programs: on, 1 program\n") {
		t.Fatalf("status while on:\n%s", out)
	}

	// Off by hand: the sweep stops the director, into dormant.
	writeConfig(t, env, off)
	if !Poll(agentWait, func() bool { return len(directorIDs(env, "studio")) == 0 }) {
		t.Fatal("the director still runs with programs off")
	}
	if got := dormantIDs(env); len(got) != 1 || got[0] != id {
		t.Fatalf("dormant %v, want the director %s", got, id)
	}
	out := env.MustCLI("program", "status")
	if !strings.HasPrefix(out, "programs: off, 1 program dormant\n") || !strings.Contains(out, "studio  demo") {
		t.Fatalf("status while off:\n%s", out)
	}
	if r := env.CLI("program", "open", "studio"); r.Code == 0 || !strings.Contains(r.Stderr, "programs-off") {
		t.Fatalf("program open while off: %+v", r)
	}

	// A restart leaves it dormant; the child carries on.
	env.MustCLI("server", "restart", "--yes")
	env.MustCLI("session", "list")
	if got := directorIDs(env, "studio"); len(got) != 0 {
		t.Fatalf("a director runs after a restart while off: %v", got)
	}
	if got := dormantIDs(env); len(got) != 1 {
		t.Fatalf("dormant after a restart %v, want the director", got)
	}
	env.MustCLI("task", "add", "Ship while dormant", "--project", slug)

	// Back on: the same session resumes, told what changed.
	writeConfig(t, env, on)
	if !Poll(agentWait, func() bool { got := directorIDs(env, "studio"); return len(got) == 1 && got[0] == id }) {
		t.Fatalf("the director %s didn't resume: running %v", id, directorIDs(env, "studio"))
	}
	nudge := env.WaitFake("prompt", agentWait, func(r FakeRecord) bool {
		return strings.HasPrefix(r.Str("text"), "[tm] Programs are back on")
	})
	if text := nudge.Str("text"); !strings.Contains(text, "demo: ") || !strings.Contains(text, "1 task.add") {
		t.Fatalf("the back-on nudge doesn't name the child's task:\n%s", nudge.Str("text"))
	}
	if r := env.CLI("program", "delete", "studio", "--yes"); r.Code == 0 || !strings.Contains(r.Stderr, "director-running") {
		t.Fatalf("program delete while its director runs: %+v", r)
	}
}
