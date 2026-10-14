package e2e

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/relay"
)

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

	if out := mini.MustCLI("peer", "panes", "studio", "type"); !strings.Contains(out, "Panes type for studio") {
		t.Fatalf("peer panes: %s", out)
	}
	// The mirror hears of the switch with peer.projects, every 30 s at
	// most; peer.attach itself answers at once.
	w := studio.Attach(120, 40, remote)
	w.WaitFor("Fake Claude Code", agentWait)
	w.WaitUntil("the status bar names the server", wait, func(sc string) bool {
		return lastLine(sc, "mini › demo › ") && strings.Contains(sc, "remote")
	})
	// The pane is mini's screen: its first rows, as the far server has
	// them.
	far := strings.Split(strings.TrimSpace(mini.Screen(th)), "\n")
	w.WaitFor(strings.TrimSpace(far[0]), wait)

	// Typing goes to mini's agent, and tells mini's coordinator.
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
