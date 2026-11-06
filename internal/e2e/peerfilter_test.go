package e2e

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestPeerSidebarFilter (T1086): mini shares api with its pages on. In
// studio's sidebar the filter finds api's open task over a real link, under
// mini's header: first in a session's sidebar, then on a fresh console's
// dashboard, where enter on the row shows api's dashboard. A done task stays
// out, and when mini turns the pages off the row goes.
func TestPeerSidebarFilter(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	studio.FakeClaude()
	_, webDir := newProject(studio, "web")
	studio.Trust(webDir)
	mini.MustCLI("project", "new", "api")
	writeConfig(t, mini, "[projects.api]\nshare = true\n")
	mini.MustCLI("task", "add", "Quince billing", "--project", "api")
	mini.MustCLI("task", "add", "Quince export", "--status", "done", "--project", "api")
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("peer", "pages", "studio", "api", "on")

	found := func(sc string) bool {
		return strings.Contains(sc, "T1 Quince") && strings.Contains(sc, "◉ mini") && !strings.Contains(sc, "no match")
	}

	// In a session: down to web's coordinator and into it, then the filter
	// from the sidebar.
	w := studio.Window(120, 30)
	w.WaitFor("◉ mini · ", 30*time.Second)
	w.WaitFor("■ api", 30*time.Second)
	w.Key(keyTab)
	w.WaitFor("esc back to the list", wait)
	w.Key(keyDown)
	w.WaitFor("web coordinator", wait)
	w.Key(keyEnter)
	w.WaitUntil("in web's coordinator", wait, func(sc string) bool { return lastLine(sc, "web › coordinator") })
	w.Prefix("\t")
	w.WaitUntil("sidebar focused", wait, func(sc string) bool { return lastLine(sc, "preview") })
	// Off the coordinator's row, where a key would type through to it.
	w.Key(keyUp)
	w.WaitFor("esc back", wait)
	w.Keys("/", "quince")
	// The switch reaches studio's mirror on its next read of mini's list.
	sc := w.WaitUntil("the task in a session's sidebar", 90*time.Second, found)
	if strings.Contains(sc, "T2 Quince") {
		t.Fatalf("a done task shows:\n%s", sc)
	}

	// On a fresh console's dashboard.
	d := studio.Window(120, 30)
	d.WaitFor("◉ mini · ", 30*time.Second)
	d.Key(keyTab)
	d.WaitFor("esc back to the list", wait)
	d.Keys("/", "quince")
	d.WaitUntil("the task on the dashboard", 30*time.Second, found)
	d.Key(keyEnter)
	for i := 0; i < 4 && !strings.Contains(d.Screen(), "mini / api T1 Quince billing"); i++ {
		d.Key(keyDown)
		time.Sleep(100 * time.Millisecond)
	}
	d.WaitFor("mini / api T1 Quince billing", wait)
	d.WaitFor("tm dashboard · mini › api", wait)

	// The pages go off on mini: the row goes, within a read of the mirror.
	mini.MustCLI("peer", "pages", "studio", "api", "off")
	d.WaitUntil("the task gone", 90*time.Second, func(sc string) bool { return !strings.Contains(sc, "T1 Quince") })
}
