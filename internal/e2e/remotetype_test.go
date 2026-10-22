package e2e

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestRemotePaneTypeThrough (T679): studio and mini paired, mini sharing
// demo with panes writable. A click on mini's thread row in studio's
// sidebar shows its pane, and mini's thread takes the pane's size at
// once, as a local pane does, not once a key is typed. Then, with the
// sidebar's cursor on mini's coordinator row, printable keys type
// through to it: its pane opens and they land in its prompt, no enter
// first. On the thread's row a key types nothing.
func TestRemotePaneTypeThrough(t *testing.T) {
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
	mini.MustCLI("peer", "panes", "studio", "write")
	coord := &Session{ID: strings.TrimSpace(mini.MustCLI("project", "open", "demo"))}
	mini.WaitState(coord, "idle", agentWait)
	mini.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)

	const cols, rows = 150, 34
	w := studio.Window(cols, rows)
	w.WaitFor("SESSIONS", wait)
	y := -1
	w.WaitUntil("mini's thread row", agentWait, func(sc string) bool { y = treeRow(sc, "demo", "t-0001"); return y >= 0 })
	w.Click(4, y)
	w.WaitUntil("mini's thread pane", agentWait, func(sc string) bool {
		return strings.Contains(sc, "Fake Claude Code") && lastLine(sc, "mini › demo › ")
	})
	// Sized at once, no key typed.
	waitPaneSize(t, mini, th, threadCols(cols), rows-statusRows)

	// The click gave the sidebar the keyboard, its cursor on the
	// thread's row: a key types nothing there.
	w.WaitFor("⏎ open · ← project", wait)
	w.Type("x")
	// Up to mini's coordinator: the key bar says typing writes to it.
	w.Key(keyUp)
	w.WaitFor("type to write to it", wait)
	w.Type("Hello there/x")
	sc := w.WaitFor("❯ Hello there/x", agentWait)
	if !lastLine(sc, "mini › demo › coordinator") {
		t.Fatalf("not on mini's coordinator:\n%s", sc)
	}
	if strings.Contains(mini.Screen(th), "❯ x") {
		t.Fatalf("a key typed on the thread's row reached it:\n%s", mini.Screen(th))
	}
	if strings.Contains(mini.Screen(coord), "❯ xHello") {
		t.Fatalf("a key typed on the thread's row reached the coordinator:\n%s", mini.Screen(coord))
	}
}
