package e2e

import (
	"strings"
	"testing"
	"time"
)

// keyBar is the dashboard's footer: its line with the ≡ menu.
func keyBar(sc string) string {
	for _, l := range strings.Split(sc, "\n") {
		if strings.Contains(l, "≡ menu ·") {
			return l
		}
	}
	return ""
}

// selectRow moves the list's cursor to the top, then down until the footer
// says want (and not not), and fails when no row does.
func selectRow(t *testing.T, w *Window, want, not string) {
	t.Helper()
	for range 12 {
		w.Key(keyUp)
	}
	w.Quiet(300 * time.Millisecond)
	ok := func(sc string) bool {
		f := keyBar(sc)
		return strings.Contains(f, want) && (not == "" || !strings.Contains(f, not))
	}
	for i := 0; i < 12; i++ {
		if ok(w.Screen()) {
			return
		}
		before := w.Screen()
		w.Key(keyDown)
		w.WaitUntil("the cursor moved", wait, func(sc string) bool { return sc != before })
	}
	if !ok(w.Screen()) {
		t.Fatalf("no row whose footer says %q:\n%s", want, w.Screen())
	}
}

// TestSmokeTypeThrough: on the dashboard, a word typed on a running
// coordinator's row attaches it and lands in its prompt whole, the
// capital and the space with it (docs/SPEC.md §4, Type-through); on a
// thread's row the same keys do nothing, and the thread's prompt gets
// what is typed only after enter attached it.
func TestSmokeTypeThrough(t *testing.T) {
	env, _, _ := threadEnv(t)
	env.MustCLI("task", "add", "Fix the login", "--status", "ready", "--project", "demo")
	env.MustCLI("thread", "start", "--task", "T1", "Fix the login", "--project", "demo")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)

	w := env.Window(140, 30)
	w.WaitFor("SESSIONS", wait)
	clickCoordinator(t, w, "demo") // starts it
	w.WaitFor("Fake Claude Code", agentWait)
	coord := coordinatorOf(t, env, "demo")
	env.WaitState(coord, "idle", agentWait)
	w.Prefix("d")
	w.WaitFor("SESSIONS", wait)
	w.Key(keyEsc) // the click gave the sidebar the keyboard: the list's
	w.WaitFor("tab sidebar", wait)

	// The coordinator's row: typing writes to it.
	selectRow(t, w, "type to write to the coordinator", "")
	w.Type("Hello there/x")
	sc := w.WaitFor("❯ Hello there/x", agentWait)
	if !lastLine(sc, "demo › coordinator") {
		t.Fatalf("not attached to the coordinator:\n%s", sc)
	}

	// A thread's row: the keys do nothing until enter attaches it.
	w.Prefix("d")
	w.WaitFor("SESSIONS", wait)
	selectRow(t, w, "enter attach", "type to write")
	w.Type("qux")
	w.Key(keyEnter)
	sc = w.WaitUntil("on t-0001", agentWait, func(sc string) bool { return lastLine(sc, "demo › T1 ") })
	if strings.Contains(sc, "qux") {
		t.Fatalf("the keys typed on a thread's row reached it:\n%s", sc)
	}
	w.Type("ok then")
	w.WaitFor("❯ ok then", agentWait)
}
