package e2e

// M4 scenarios: the dashboard (docs/SPEC.md §4). TestSmoke* run
// on every PR; the rest weekly.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// Dashboard keys.
var (
	keyDown = emu.Key{Special: emu.KeyDown}
	keyUp   = emu.Key{Special: emu.KeyUp}
	keyEsc  = emu.Key{Special: emu.KeyEscape}
)

// dashMasks hide the cwd column's temp paths.
var dashMasks = []Mask{{Name: "tmp", Re: regexp.MustCompile(`/\S*/T/\S*|/tmp/\S*`)}}

// popupEdgeCwd hides where the session row's cwd shows past a popup's
// right border: its column follows the duration hidden under the popup
// (9s, then 10s), which DefaultMasks can't mask there.
var popupEdgeCwd = Mask{Name: "edge", Re: regexp.MustCompile(`(?m)│ ?/\S*[ \t]*$`), Repl: "│/"}

// newProject creates a project through the CLI and returns its slug and
// folder.
func newProject(env *Env, name string) (slug, dir string) {
	env.T.Helper()
	var p struct{ Slug, Dir string }
	if err := json.Unmarshal([]byte(env.MustCLI("project", "new", name, "--json")), &p); err != nil {
		env.T.Fatal(err)
	}
	return p.Slug, p.Dir
}

// TestSmokeDashboard: the dashboard's screens, empty, and with a project
// and sessions; a task in review shows in NEEDS YOU, and enter shows it
// in the project popup's Tasks tab, where prefix+a opens the project
// popup anew rather than accepting (docs/SPEC.md §4, the key table); T
// shows the tasks, and D on a task in review only says why it isn't
// delegated.
func TestSmokeDashboard(t *testing.T) {
	env := New(t)
	env.FakeClaude() // an agent installed: what most users have
	w := env.Window(100, 24)
	w.WaitFor("no sessions", wait)
	w.Golden("dashboard-empty.txt")

	slug, _ := newProject(env, "demo")
	// The cursor lands on the first row the dashboard has: wait for the
	// project, or a session started first takes it (the golden's footer
	// says enter opens the project).
	w.WaitFor("demo", wait)
	env.MustCLI("task", "add", "Write the README", "--status", "ready", "--project", slug, "--step", "Draft", "--step", "Review")
	env.MustCLI("task", "add", "Ship it", "--project", slug)
	env.MustCLI("task", "status", "T2", "started", "--project", slug)
	s := env.Start("shell")
	env.WaitFor(s, "$", wait)
	w.WaitFor("0 need you · 1 in motion · 1 on deck", wait)
	w.WaitFor(s.ID+" ", wait)
	w.Golden("dashboard-sessions.txt", dashMasks...)

	env.MustCLI("task", "status", "T1", "review", "--project", slug)
	w.WaitFor("1 needs you (T lists them)", wait)
	w.WaitFor("T1 Write the README", wait)
	if !strings.Contains(w.Screen(), "NEEDS YOU 1") {
		t.Fatalf("a task in review is not in NEEDS YOU:\n%s", w.Screen())
	}
	w.Golden("dashboard-needs-you.txt", dashMasks...)

	// enter on it (the row above the project's): the project popup's
	// Tasks tab, T1 selected.
	w.Key(keyUp)
	w.WaitFor("enter show", wait)
	w.Key(keyEnter)
	w.WaitFor("enter show · A accept · X send back · + new task", wait)
	w.Prefix("a")
	w.WaitFor("+ add repository", wait)
	if strings.Contains(w.Screen(), "Accept T1?") {
		t.Fatalf("prefix+a reached the Tasks tab:\n%s", w.Screen())
	}
	w.Key(keyEsc)
	w.WaitFor("NEEDS YOU 1", wait)

	// The Tasks tab: T1 first (needs you); it only shows.
	w.Type("T")
	w.WaitFor("3 tasks", wait)
	w.WaitFor("T1", wait)
	w.Golden("dashboard-tasks.txt", append(dashMasks, popupEdgeCwd)...)
	// The tab's view is the tasks-tab mod's: a D before its cursor sits on
	// the row says nothing, so press again until it explains (D only says
	// why; it never changes anything).
	said := false
	for try := 0; try < 10 && !said; try++ {
		w.Type("D")
		said = Poll(time.Second, func() bool { return strings.Contains(w.Screen(), "T1 is in review") })
	}
	w.WaitFor("T1 is in review", wait)
	if out := env.MustCLI("task", "show", "T1", "--project", slug, "--json"); !strings.Contains(out, `"status": "review"`) {
		t.Fatalf("T1 after D:\n%s", out)
	}
	w.Key(keyEsc)
	w.Quit()
	w.WaitExit(wait)
}

// TestSmokeDashboardAllProjects: NEEDS YOU is the current project's
// only, another project's task in review stays out of it; a click on
// the sidebar's PROJECTS header lists every project's NEEDS YOU, each
// row labelled with its project, and a click on a project goes back to
// its own (docs/SPEC.md §4, **All projects**).
func TestSmokeDashboardAllProjects(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	alpha, _ := newProject(env, "alpha")
	beta, _ := newProject(env, "beta")
	env.MustCLI("task", "add", "Write the README", "--project", alpha)
	env.MustCLI("task", "status", "T1", "review", "--project", alpha)
	env.MustCLI("task", "add", "Ship it", "--project", beta)
	env.MustCLI("task", "status", "T1", "blocked", "--project", beta)
	w := env.Window(120, 24)
	w.WaitFor("1 needs you (T lists them)", wait)
	sc := w.WaitFor("NEEDS YOU 1", wait)
	if strings.Contains(sc, "Write the README") == strings.Contains(sc, "Ship it") {
		t.Fatalf("NEEDS YOU isn't one project's:\n%s", sc)
	}

	w.ClickText("PROJECTS", 0)
	w.WaitFor("tm dashboard · all projects", wait)
	w.WaitFor("NEEDS YOU 2", wait)
	w.WaitFor("alpha         T1 Write the README", wait)
	w.WaitFor("beta          T1 Ship it", wait)
	w.Golden("dashboard-all-projects.txt", dashMasks...)

	w.ClickText("■ beta", 0)
	w.WaitUntil("beta's dashboard", wait, func(sc string) bool {
		return !strings.Contains(sc, "all projects") && strings.Contains(sc, "T1 Ship it") && !strings.Contains(sc, "Write the README")
	})
	w.Quit()
	w.WaitExit(wait)
}

// TestSmokeFirstLocalRun is M4's "Try it" with the fake agent: an agent
// session of the user's own, outside any project, shows on the dashboard
// and enter attaches; a prompt typed there blocks on a permission dialog;
// Ctrl+B d shows the session under NEEDS YOU; enter attaches again to
// answer it; closing the terminal loses nothing, and a new dashboard
// shows the session idle.
func TestSmokeFirstLocalRun(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	dir := env.Workdir()
	env.Trust(dir)

	w := env.Window(100, 30)
	w.WaitFor("no sessions", wait)
	s := env.StartAgent("claude", dir)
	w.WaitFor(s.ID+" ", wait)
	w.Key(Enter)
	// Attached, with the status bar on the last row.
	w.WaitFor("Fake Claude Code", agentWait)
	w.WaitFor(s.ID+" › claude", wait)
	env.WaitState(s, "idle", agentWait)
	w.WaitUntil("idle in the status bar", wait, func(sc string) bool { return lastLine(sc, "○ "+s.ID+" › claude") })

	w.Keys("run permission", Enter)
	env.WaitState(s, "blocked/permission", agentWait)
	w.WaitUntil("blocked in the status bar", wait, func(sc string) bool { return lastLine(sc, "▲ "+s.ID+" › claude  permission") })

	// Back to the dashboard: the session needs you.
	w.Detach()
	w.WaitFor("NEEDS YOU", wait)
	w.WaitFor("▲ blocked   permission", wait)
	env.AssertAlive(s)

	// Attach again (the session's row is still selected) and approve.
	w.Key(Enter)
	w.WaitUntil("attached", wait, func(sc string) bool {
		return lastLine(sc, `prefix+d dashboard`) && strings.Contains(sc, "Do you want to create x.txt?")
	})
	w.Type("1")
	env.WaitState(s, "idle", agentWait)
	w.Detach()
	w.WaitUntil("NEEDS YOU gone", wait, func(sc string) bool { return !strings.Contains(sc, "NEEDS YOU") && strings.Contains(sc, "idle") })

	// Close the terminal: nothing is lost.
	w.CloseWindow()
	env.AssertAlive(s)
	w2 := env.Window(100, 30)
	w2.WaitFor(s.ID, wait)
	w2.WaitFor("idle", wait)
	w2.Quit()
	w2.WaitExit(wait)
}

// lastLine reports whether the screen's last non-empty line contains s.
func lastLine(screen, s string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	return len(lines) > 0 && strings.Contains(lines[len(lines)-1], s)
}
