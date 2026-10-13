package e2e

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// TestSmokeSwitchOneFrame: switching between the dashboard and a session
// never shows anything between the two screens (T372): the alternate
// screen stays on (no 1049l and 1049h, which showed the shell's screen
// and then a blank one), nothing is drawn outside a synchronised update,
// the session's frames never clear the screen, and wherever the terminal
// could show the window during the switch it shows one screen or the
// other whole.
func TestSmokeSwitchOneFrame(t *testing.T) {
	env := New(t)
	s := env.Start("shell")
	env.WaitFor(s, "$", wait)
	w := env.Window(120, 30)
	w.WaitFor(s.ID+" ", wait)

	const dash, attached = "enter attach", "prefix+d dashboard"
	for i := range 2 {
		w.Quiet(300 * time.Millisecond)
		n := len(w.Raw())
		w.Key(Enter)
		w.WaitUntil("attached", wait, func(sc string) bool { return lastLine(sc, attached) })
		w.Quiet(300 * time.Millisecond)
		raw := w.Raw()
		checkSwitch(t, raw, n, 120, 30, dash, attached)
		if bytes.Contains(raw[n:], []byte("\x1b[2J")) {
			t.Errorf("switch %d: the session's frame clears the screen", i)
		}

		n = len(raw)
		w.Detach()
		w.WaitFor("OTHER SESSIONS", wait)
		w.Quiet(300 * time.Millisecond)
		checkSwitch(t, w.Raw(), n, 120, 30, attached, dash)
	}
	w.Quit()
	w.WaitExit(wait)
	if m := w.Modes(); m.AltScreen {
		t.Errorf("quitting left the alternate screen on: %+v", m)
	}
}

// TestSwitchSidebarOneFrame: the sidebar's cursor moving between a
// thread (its session shows) and its project (the project's dashboard
// shows) switches screens just as cleanly (T372).
func TestSwitchSidebarOneFrame(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	startThread(t, env, projDir)
	w := env.Window(120, 30)
	w.WaitUntil("demo's thread", wait, func(sc string) bool { return treeRow(sc, "demo", "t-0001 ") >= 0 })
	w.Key(keyTab)
	w.WaitFor("esc back to the list", wait)
	w.Key(keyDown) // demo's coordinator, not running: the project's dashboard
	w.Key(keyDown) // t-0001: its session
	const thread, project = "demo › t-0001", " demo ─"
	w.WaitUntil("t-0001 previewed", wait, func(sc string) bool { return lastLine(sc, thread) })
	for range 2 {
		w.Quiet(500 * time.Millisecond)
		n := len(w.Raw())
		w.Key(keyUp)
		w.WaitUntil("demo's dashboard", wait, func(sc string) bool { return strings.Contains(sc, project) })
		w.Quiet(500 * time.Millisecond)
		raw := w.Raw()
		checkSwitch(t, raw, n, 120, 30, thread, project)

		n = len(raw)
		w.Key(keyDown)
		w.WaitUntil("t-0001 previewed", wait, func(sc string) bool { return lastLine(sc, thread) })
		w.Quiet(500 * time.Millisecond)
		checkSwitch(t, w.Raw(), n, 120, 30, project, thread)
	}
}

// checkSwitch checks the output from raw[n:], a switch from the screen
// showing from to the one showing to (text each shows): replayed on a
// terminal of cols×rows, wherever it could show the window (outside a
// synchronised update) it shows one of them, and nothing is drawn there.
func checkSwitch(t *testing.T, raw []byte, n, cols, rows int, from, to string) {
	t.Helper()
	sw := raw[n:]
	if bytes.Contains(sw, []byte("\x1b[?1049")) {
		t.Errorf("%s → %s: the switch leaves or enters the alternate screen: %q", from, to, sw)
	}
	term, err := emu.New(uint16(cols), uint16(rows))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Write(raw[:n])
	shown := func(where string) {
		sc, err := term.Screen()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sc, from) && !strings.Contains(sc, to) {
			t.Errorf("%s → %s: %s the window shows neither:\n%s", from, to, where, sc)
		}
	}
	start, end := []byte("\x1b[?2026h"), []byte("\x1b[?2026l")
	for len(sw) > 0 {
		// Outside a synchronised update: modes and queries only.
		i := bytes.Index(sw, start)
		if i < 0 {
			i = len(sw)
		}
		if text := strings.TrimSpace(ansi.Strip(string(sw[:i]))); text != "" {
			t.Errorf("%s → %s: drawn outside a synchronised update: %q", from, to, text)
		}
		term.Write(sw[:i])
		shown("between updates")
		sw = sw[i:]
		if len(sw) == 0 {
			break
		}
		j := bytes.Index(sw, end)
		if j < 0 {
			j = len(sw)
		} else {
			j += len(end)
		}
		term.Write(sw[:j])
		shown("after an update")
		sw = sw[j:]
	}
	sc, _ := term.Screen()
	if !strings.Contains(sc, to) {
		t.Errorf("%s → %s: the switch didn't reach it:\n%s", from, to, sc)
	}
}
