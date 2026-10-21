package e2e

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// TestSmokePrefixQuit: prefix+q quits the console from everywhere
// (docs/SPEC.md §4): the dashboard's list, every popup, a prompt, the
// sidebar holding the keyboard, a session, a popup over it, the
// session's menu and question, tm attach and tm project open. Plain q
// quits nothing. The server, the session and the view carry on: a new
// console shows the session again.
func TestSmokePrefixQuit(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	alpha, dir := newProject(env, "alpha")
	env.Trust(dir)
	id := strings.TrimSpace(env.MustCLI("project", "open", alpha))
	coord := coordinatorOf(t, env, alpha)
	env.WaitState(coord, "idle", agentWait)
	before, _ := env.Info(coord)

	tab := emu.Key{Special: emu.KeyTab}
	// From the dashboard: each place, opened by its keys, shows its text.
	for _, c := range []struct {
		name, keys, shows string
	}{
		{"the list", "", "SESSIONS"},
		{"the project popup", "A", "1 overview"},
		{"the keys tab", "A6", "On the dashboard"},
		{"the inbox", "I", "The coordinator handles these"},
		{"the tasks", "T", "3 tasks"},
		{"the settings", ",", "enter change"},
		{"the help", "?", "↑ ↓ scroll · esc close"},
		{"a prompt", "+", "New project"},
		{"the sidebar", "\t", "esc back to the list"},
	} {
		w := env.Window(100, 30)
		w.WaitFor("SESSIONS", wait)
		var keys []any
		for i, k := range c.keys {
			switch {
			case k == '\t':
				keys = append(keys, tab)
			case i == 0:
				// The coordinator's row is selected: a plain key would
				// type through to it, so its command is prefix+key.
				keys = append(keys, CtrlB, string(k))
			default:
				keys = append(keys, string(k))
			}
		}
		if len(keys) > 0 {
			w.Keys(keys...)
		}
		w.WaitFor(c.shows, wait)
		if c.name == "the list" {
			// Plain q quits nothing: on the coordinator's row it types
			// through to it.
			w.Type("q")
			w.WaitFor("❯ q", agentWait)
			select {
			case <-w.Exited():
				t.Fatal("plain q quit the dashboard")
			default:
			}
			w.Key(emu.Key{Special: emu.KeyBackspace})
			w.WaitUntil("the prompt empty again", wait, func(sc string) bool { return !strings.Contains(sc, "❯ q") })
			w.Detach()
			w.WaitFor("SESSIONS", wait)
		}
		w.Quit()
		w.WaitExit(wait)
		env.AssertAlive(coord)
	}

	// From a session of view main: the pane, a popup over it, the
	// sidebar, the menu and the remote control question. The view stays
	// on the session, so the next console attaches it again.
	w := env.Window(100, 30)
	w.WaitFor("SESSIONS", wait)
	clickCoordinator(t, w, alpha)
	attached := func(w *Window) {
		t.Helper()
		w.WaitUntil("attached to "+alpha, agentWait, func(sc string) bool { return lastLine(sc, alpha+" › coordinator") })
	}
	attached(w)
	w.Quit()
	w.WaitExit(wait)
	env.AssertAlive(coord)
	for _, c := range []struct {
		name  string
		open  func(w *Window)
		shows string
	}{
		{"a popup over the session", func(w *Window) { w.Prefix("i") }, "The coordinator handles these"},
		{"the sidebar", func(w *Window) { w.Key(CtrlB); w.Key(tab) }, "preview · ⏎ to type here"},
		{"the menu", func(w *Window) { w.ClickText("≡", 29) }, "send the prefix key"},
		{"the question", func(w *Window) { w.Prefix("r") }, "Turn remote control"},
	} {
		// Room for the status bar's whole sidebar hint.
		w := env.Window(104, 30)
		attached(w) // the view kept the session
		c.open(w)
		w.WaitFor(c.shows, wait)
		w.Quit()
		w.WaitExit(wait)
		env.AssertAlive(coord)
	}

	// tm attach and tm project open: their own views, quit alike.
	for _, args := range [][]string{{"attach", id}, {"project", "open", alpha}} {
		w := env.Window(100, 30, args...)
		attached(w)
		w.Quit()
		w.WaitExit(wait)
		if !strings.Contains(w.Screen(), "detached") {
			t.Fatalf("tm %s after prefix q:\n%s", strings.Join(args, " "), w.Screen())
		}
		env.AssertAlive(coord)
	}
	// Remote control was never changed by a question cut short.
	if info, _ := env.Info(coord); info.RemoteControl != before.RemoteControl {
		t.Fatal("prefix q answered yes to the question")
	}
}
