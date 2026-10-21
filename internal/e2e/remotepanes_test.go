package e2e

import (
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/relay"
	"github.com/theclifmeister/terminatr/internal/view"
)

// remotePanelMasks hide what differs between runs in a remote pane's
// info panel: the link's last ping (how long ago the mirror read the
// project is a duration, DefaultMasks').
var remotePanelMasks = []Mask{
	{Name: "rtt", Re: regexp.MustCompile(`linked (<1|\d+) ms`), Repl: "linked <rtt>"},
}

// infoPanelScreen is the info panel of window w at the default layout:
// its columns at the right edge, above the status bar. The sidebar,
// whose header names this machine, is left out.
func infoPanelScreen(w *Window) string {
	w.mu.Lock()
	x := w.cols - view.InfoDefault
	w.mu.Unlock()
	lines := strings.Split(w.Screen(), "\n")
	lines = lines[:max(len(lines)-statusRows, 0)]
	for i, l := range lines {
		r := []rune(l)
		lines[i] = string(r[min(x, len(r)):])
	}
	return TrimScreen(strings.Join(lines, "\n"))
}

// TestSmokeRemotePanes (T402, relay phases 4 and 5): studio and mini,
// two installations paired through an in-process relay; mini shares
// demo, and its pane switch for studio decides. Off, studio can't open
// mini's panes; type, a console on studio opens mini's thread as the
// session mini/<id>, sees its screen, and types into it: the keys reach
// the agent there, and mini's coordinator gets a takeover item in
// mini's words. Turning the switch off closes the pane on studio.
func TestSmokeRemotePanes(t *testing.T) {
	mini, _, _ := threadEnv(t)
	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	writeConfig(t, mini, "[projects.demo]\nshare = true\n")

	mini.MustCLI("server", "start")
	studio.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)
	remote := "mini/" + th.ID

	// Off (the default): the console says why, in studio's words, once
	// studio's mirror has the session.
	var last string
	if !Poll(30*time.Second, func() bool {
		w := studio.Attach(120, 40, remote)
		w.WaitExit(wait)
		last = w.Screen()
		if strings.Contains(last, "doesn't share panes with this server") {
			return true
		}
		time.Sleep(time.Second) // the mirror reads mini again
		return false
	}) {
		t.Fatalf("attach with panes off:\n%s", last)
	}

	// View (T416): the pane shows at mini's size, which the status bar
	// says, and never resizes it; keys are dropped with the note, while
	// the wheel and a drag are studio's own: the drag copies.
	if out := mini.MustCLI("peer", "panes", "studio", "view"); !strings.Contains(out, "Panes view for studio") {
		t.Fatalf("peer panes: %s", out)
	}
	w := studio.Attach(100, 30, remote)
	w.WaitFor("Fake Claude Code", agentWait)
	sizeNote := regexp.MustCompile(`view-only · \d+×\d+ on mini`)
	w.WaitUntil("the status bar says view-only and mini's size", wait, func(sc string) bool {
		return lastLine(sc, "mini › demo › ") && sizeNote.MatchString(sc)
	})
	cols := func() uint16 { info, _ := mini.Info(th); return info.Cols }
	farCols := cols()
	x, y := w.TextAt("Fake Claude Code", 0)
	w.Wheel(true, x, y)
	w.Drag(x, x+3, y)
	w.WaitUntil("the copy", wait, func(sc string) bool { return strings.Contains(sc, "copied 1 line") })
	if !strings.Contains(string(w.Raw()), "\x1b]52;c;") {
		t.Fatal("the drag put nothing on the clipboard")
	}
	w.Paste("dropped at view")
	w.WaitUntil("the view-only note", wait, func(sc string) bool { return strings.Contains(sc, "view-only: mini shares") })
	if c := cols(); c != farCols {
		t.Fatalf("a view-only console resized mini's pane: %d columns, was %d", c, farCols)
	}

	// Raised to write (T416), the open pane types at once: mini tells
	// studio on the link, not at the mirror's next read 30 s on, and the
	// view-only note goes.
	if out := mini.MustCLI("peer", "panes", "studio", "write"); !strings.Contains(out, "Panes write for studio") {
		t.Fatalf("peer panes: %s", out)
	}
	w.WaitUntil("the status bar drops view-only", wait, func(sc string) bool {
		return lastLine(sc, "mini › demo › ") && strings.Contains(sc, "remote") && !strings.Contains(sc, "view-only")
	})
	// Typing goes to mini's agent, and tells mini's coordinator; what
	// was typed at view never reached it.
	if slices.ContainsFunc(mini.FakeRecords("prompt"), func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "dropped at view") }) {
		t.Fatal("a key typed at view reached mini's agent")
	}

	// In a window with room for it, the info panel beside the pane,
	// from studio's mirror of mini: the server and its link, the
	// thread.
	wide := studio.Attach(160, 40, remote)
	wide.WaitFor("THREAD", wait)
	wide.WaitFor("/ 1.0M", agentWait) // the thread's context use, from its transcript, through the mirror
	WaitGolden(t, wait, func() string { return infoPanelScreen(wide) }, "remote-info-panel.txt", remotePanelMasks...)
	wide.CloseWindow()

	w.Paste("hello from studio")
	w.Key(emu.Key{Special: emu.KeyEnter})
	mini.WaitFake("prompt", agentWait, func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "hello from studio") })
	if !Poll(wait, func() bool {
		return strings.Contains(mini.MustCLI("inbox", "list", "--project", "demo"), "the user typed into T1") ||
			strings.Contains(mini.MustCLI("inbox", "list", "--project", "demo"), "from the paired server studio")
	}) {
		t.Fatalf("mini's inbox:\n%s", mini.MustCLI("inbox", "list", "--project", "demo"))
	}
	if items := mini.MustCLI("inbox", "list", "--project", "demo"); !strings.Contains(items, "from the paired server studio") {
		t.Fatalf("the takeover item:\n%s", items)
	}
	// studio has no demo of its own: nothing was written there.
	if r := studio.CLI("inbox", "list", "--project", "demo"); r.Code == 0 && strings.Contains(r.Stdout, "takeover") {
		t.Fatalf("studio raised a takeover: %s", r.Stdout)
	}
	// prefix+R asks mini for a fresh repaint; the pane stays mini's.
	w.Prefix("R")
	w.WaitUntil("the repaint note", wait, func(sc string) bool { return strings.Contains(sc, "repainting from mini") })
	w.WaitFor("hello from studio", wait)

	// Session actions don't cross.
	if r := studio.CLI("session", "stop", remote); r.Code == 0 || !strings.Contains(r.Stdout+r.Stderr, "open it on mini") {
		t.Fatalf("session stop on a remote pane: %d %s %s", r.Code, r.Stdout, r.Stderr)
	}

	// mini restarts: the pane freezes, keys typed meanwhile are dropped,
	// and it comes back on its own once the link and the thread are.
	mini.MustCLI("server", "restart", "--yes")
	th = threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)
	got := func(r FakeRecord) bool { return strings.Contains(r.Str("text"), "again from studio") }
	if !Poll(agentWait, func() bool {
		w.Paste("again from studio")
		w.Key(emu.Key{Special: emu.KeyEnter})
		return Poll(2*time.Second, func() bool { return slices.ContainsFunc(mini.FakeRecords("prompt"), got) })
	}) {
		t.Fatalf("keys never reached mini's thread after its restart:\n%s", w.Screen())
	}
	w.WaitUntil("the pane after mini's restart", wait, func(sc string) bool {
		return strings.Contains(sc, "again from studio") && lastLine(sc, "mini › demo › ")
	})

	// Off again: the pane closes on studio, in fixed words.
	mini.MustCLI("peer", "panes", "studio", "off")
	w.WaitExit(agentWait)
	if sc := w.Screen(); !strings.Contains(sc, "mini turned its panes off for this server") {
		t.Fatalf("after the switch went off:\n%s", sc)
	}
	mini.AssertAlive(th)
}
