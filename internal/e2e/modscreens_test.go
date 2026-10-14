package e2e

// TestModScreens: the example mods and the built-in ones as the console
// draws them: dashboard cards, a sidebar and dashboard column and a
// details panel section; a command's full view; the Mods tab; the
// Memory tab. Its goldens are the website's mods screens
// (site/tools/screens.py).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// cutDuration masks a session row's duration the settings popup cuts
// short at its right edge (its unit hidden, only "15…" left), which
// DefaultMasks can't match.
var cutDuration = Mask{Name: "duration", Re: regexp.MustCompile(`\b\d+…`), Repl: "<duration>…"}

func TestModScreens(t *testing.T) {
	env, _, _ := tickerEnv(t)
	state := fakeGH(t, env)
	env.FakeCodex() // a thread whose session reports its context and tokens
	env.Setenv("FAKEAGENT_CWD_LABEL", "~/src/demo")
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"context-gauge", "cost-card", "report-glance", "standup"} {
		env.MustCLI("mod", "install", filepath.Join(root, "examples", "mods", m), "--yes")
		env.MustCLI("mod", "enable", m, "--yes")
	}
	dir := filepath.Join(env.Home, "projects", "demo")
	os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Memory\n\n"+
		"- [Tests before push](memory/tests-first.md): run make test before every push\n"+
		"- [Login flow](memory/login.md): the redirect lives in auth/session.go\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "memory", "tests-first.md"), []byte("Run make test before every push: CI takes ten minutes.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "memory", "login.md"), []byte("The login redirect lives in auth/session.go.\n"), 0o644)

	p := func(args ...string) { env.MustCLI(append(args, "--project", "demo")...) }
	p("task", "add", "Fix the login", "--status", "ready", "--step", "Reproduce the bug", "--step", "Fix it")
	p("task", "add", "Write the docs")
	p("thread", "start", "--task", "T1", "--agent", "claude", "Fix the login")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)
	p("thread", "start", "--task", "T2", "--agent", "codex", "Write the docs")
	th2 := threadSession(t, env, "demo", "t-0002")
	env.WaitState(th2, "idle", agentWait)
	env.Prompt(th2, "hello")
	env.WaitState(th2, "idle", agentWait)
	setPR(t, state, `{"number":7,"url":"https://github.com/o/r/pull/7","state":"OPEN","title":"Fix the login","statusCheckRollup":[{"status":"IN_PROGRESS"}]}`)
	os.WriteFile(filepath.Join(env.scriptsDir(), "mods-report.toml"), []byte(`
[[step]]
do = "run"
cmd = 'printf "PR: https://github.com/o/r/pull/7\n\n## Report\nFixed the login redirect.\n\n## Next\nMerge the PR\n" | "$TERMINATR_BIN" report > "$OUT/report" 2>&1'
`), 0o644)
	p("thread", "prompt", "t-0001", "run mods-report")
	p("task", "steps", "T1", "check", "1")

	// The dashboard, wide enough for the details panel: the context
	// gauge's and cost card's cards, the gauge's column, and the report
	// glance's section under the selected thread's details.
	w := env.Window(180, 28)
	w.WaitFor("SESSIONS", wait)
	w.WaitFor("25.0k tokens", agentWait)
	w.ClickText("T1 Fix the login", 1)
	w.WaitFor("Fixed the login redirect.", agentWait)
	w.WaitFor("PR        https://github.com/o/r/pull/7", wait)
	w.Golden("mods-dashboard.txt", dashMasks...)

	w.Resize(120, 38)
	w.WaitFor("OTHER SESSIONS", wait)
	w.Quiet(400 * time.Millisecond)

	// The ≡ menu ends with the mods' commands and views; standup's
	// command opens its full view.
	w.ClickText(menuGlyph+" menu", 0)
	w.WaitFor("Standup · standup", wait)
	w.Golden("mods-menu.txt", dashMasks...)
	w.Key(keyEsc)
	w.WaitUntil("menu closed", wait, func(sc string) bool { return !strings.Contains(sc, "enter pick") })
	w.Resize(120, 18)
	w.WaitFor("OTHER SESSIONS", wait)
	w.Quiet(400 * time.Millisecond)
	w.Type("S")
	w.WaitFor("Started (2)", agentWait)
	w.Golden("mods-standup.txt", dashMasks...)
	w.Key(keyEsc)
	w.WaitFor("OTHER SESSIONS", wait)
	w.Resize(120, 36)
	w.WaitFor("OTHER SESSIONS", wait)
	w.Quiet(400 * time.Millisecond)

	// The Mods tab: the user's mods, then the built-in ones.
	w.Type(",")
	w.WaitFor("1 General", wait)
	w.Type("3")
	w.WaitFor("Tasks tab", wait)
	w.WaitFor("● Standup", wait)
	w.Golden("mods-settings.txt", append(popupMasks, cutDuration)...)
	w.Key(keyEsc)
	w.WaitUntil("settings closed", wait, func(sc string) bool { return !strings.Contains(sc, "1 General") })

	// The Memory tab, the built-in memory-tab mod, reading MEMORY.md.
	w.Type("a")
	w.WaitFor("1 overview", wait)
	w.Type("6")
	w.WaitFor("Memory  MEMORY.md", agentWait)
	w.Key(keyDown)
	w.Key(Enter)
	w.WaitFor("Login flow: the redirect", wait)
	w.Golden("mods-memory.txt", popupMasks...)
	w.Key(keyEsc)
	w.WaitUntil("popup closed", wait, func(sc string) bool { return !strings.Contains(sc, "1 overview") })

	// A thread's session with the sidebar and its info panel, for the
	// README: the fake agent clears its conversation (the kickoff names
	// temp paths) and shows a little work.
	os.WriteFile(filepath.Join(env.scriptsDir(), "session-shot.toml"), []byte(`
[[step]]
do = "clear"

[[step]]
do = "stream"
ms = 50
text = "Reproducing the login bug first."

[[step]]
do = "tool"
tool = "Bash"
input = { command = "go test ./auth/..." }

[[step]]
do = "stream"
ms = 50
text = "The redirect drops the next parameter after a login. Fixing it in auth/session.go."

[[step]]
do = "tool"
tool = "Edit"
input = { file_path = "auth/session.go" }
`), 0o644)
	p("thread", "prompt", "t-0001", "run session-shot")
	env.WaitFor(th, "Edit(auth/session.go)", agentWait)
	env.WaitState(th, "idle", agentWait)
	w.Resize(160, 30)
	w.WaitFor("OTHER SESSIONS", wait)
	w.Quiet(400 * time.Millisecond)
	w.ClickText("T1 Fix the login", 1)
	w.Key(Enter)
	w.WaitFor("Edit(auth/session.go)", wait)
	w.WaitFor("LAST REPORT", wait)
	w.Golden("session-sidebar.txt", Mask{Name: "worktree", Re: regexp.MustCompile(`worktree  \S+`)})
}
