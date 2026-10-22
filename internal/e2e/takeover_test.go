package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// TestTakeoverOnlyText (T691): in a thread's pane showing the agent's
// greyed suggestion, clicks, the wheel, arrows, page keys, Tab and Esc
// raise no takeover and leave the suggestion; the first text typed into
// the box raises one, and later text no other.
func TestTakeoverOnlyText(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	env.Setenv("FAKEAGENT_SUGGEST", "try: run the tests")
	env.MustCLI("server", "start")
	env.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)
	if !Poll(agentWait, func() bool { return strings.Contains(env.Screen(th), "try: run the tests") }) {
		t.Fatalf("no suggestion in the box:\n%s", env.Screen(th))
	}

	const cols, rows = 150, 34
	w := env.Window(cols, rows)
	w.WaitFor("SESSIONS", wait)
	y := -1
	w.WaitUntil("the thread's row", agentWait, func(sc string) bool { y = treeRow(sc, "demo", "t-0001"); return y >= 0 })
	w.Click(4, y)
	w.Key(Enter)
	w.WaitUntil("in the thread's pane", agentWait, func(sc string) bool {
		return strings.Contains(sc, "try: run the tests") && lastLine(sc, "demo › ")
	})

	x, py := w.TextAt("try: run the tests", 1)
	w.Click(x, py)
	w.Click(x+4, py-3)
	w.Wheel(true, x, py)
	w.Wheel(false, x, py)
	for _, k := range []emu.SpecialKey{emu.KeyUp, emu.KeyDown, emu.KeyLeft, emu.KeyRight,
		emu.KeyPageUp, emu.KeyPageDown, emu.KeyHome, emu.KeyEnd, emu.KeyTab, emu.KeyEscape} {
		w.Key(emu.Key{Special: k})
		time.Sleep(50 * time.Millisecond)
	}
	takeover := func() bool {
		return strings.Contains(env.MustCLI("inbox", "list", "--project", "demo"), "takeover")
	}
	if Poll(5*time.Second, takeover) {
		t.Fatalf("no text typed, yet a takeover:\n%s", env.MustCLI("inbox", "list", "--project", "demo"))
	}
	if sc := env.Screen(th); !strings.Contains(sc, "try: run the tests") {
		t.Fatalf("the suggestion went:\n%s", sc)
	}

	w.Type("x")
	if !Poll(agentWait, func() bool { return strings.TrimSpace(promptLine(env.Screen(th))) == "❯ x" }) {
		t.Fatalf("the key never reached the box:\n%s", env.Screen(th))
	}
	waitInbox(t, env, "takeover: the user typed into t-0001 (Fix the login)'s pane")
	w.Paste(" and more")
	w.WaitFor("x and more", wait)
	if Poll(4*time.Second, func() bool {
		return strings.Count(env.MustCLI("inbox", "list", "--project", "demo"), "takeover: ") > 1
	}) {
		t.Fatalf("more than one takeover for the console:\n%s", env.MustCLI("inbox", "list", "--project", "demo"))
	}
	if j, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md")); strings.Count(string(j), "human thread.takeover t-0001") != 1 {
		t.Fatalf("journal:\n%s", j)
	}
}
