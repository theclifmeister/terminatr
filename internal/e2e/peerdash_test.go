package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestPeerDashboard (T398, relay phase 2): studio and mini paired
// through an in-process relay, mini sharing api. In studio's console the
// sidebar has mini's group; enter on api shows its read-only dashboard
// (the breadcrumb, the link, nothing that acts on it), and N there opens
// the Network popup, which lists mini with its connection; X on it
// unpairs it after asking, and its group leaves the sidebar.
func TestPeerDashboard(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	mini.MustCLI("project", "new", "api")
	cfg := filepath.Join(mini.Home, "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if data, err = config.Edit(data, "projects.api", "share", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, data, 0o600); err != nil {
		t.Fatal(err)
	}

	mini.MustCLI("server", "start")
	studio.FakeClaude() // no "no agent installed" alarm over the header

	w := studio.Window(110, 30)
	w.WaitFor("◉ mini · ", 30*time.Second)
	w.WaitFor("■ api", 30*time.Second)

	// The keyboard to the sidebar, down to api, and in.
	w.Key(keyTab)
	for i := 0; i < 6 && !strings.Contains(w.Screen(), "mini / api"); i++ {
		w.Key(keyDown)
		time.Sleep(100 * time.Millisecond)
	}
	w.WaitFor("mini / api", wait)
	w.Key(keyEnter)
	w.WaitFor("tm dashboard · mini › api", wait)
	screen := w.WaitFor("◉ mini · linked", wait)
	for _, want := range []string{"To ask its coordinator something", "≡ menu · ↑ ↓ scroll · + new project · , settings"} {
		if !strings.Contains(screen, want) {
			t.Errorf("missing %q in\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "view-only") || strings.Contains(screen, "Read-only") {
		t.Errorf("panes are off: the project isn't called read-only:\n%s", screen)
	}
	w.Type("A")
	w.WaitFor("api is mini's: no project popup here", wait)
	w.Type("S")
	w.WaitFor("api is mini's: nothing here acts on it", wait)

	w.Type("N")
	w.WaitFor("Network · ", wait)
	screen = w.WaitFor("◉ mini · connected", wait)
	if !strings.Contains(screen, "panes off") || !strings.Contains(screen, "shared with you · + link (L)") {
		t.Fatalf("Network:\n%s", screen)
	}
	// studio has no project to share: down from this server is mini.
	w.Key(keyDown)
	w.WaitFor("X unpair", wait)
	w.Type("X")
	w.WaitFor("Unpair mini?", wait)
	w.Type("Y")
	w.WaitFor("No paired server yet", wait)
	w.Key(keyEsc)
	w.WaitFor("mini isn't paired with this server any more", 20*time.Second)
	if out := studio.MustCLI("peer", "list"); !strings.Contains(out, "no peers") {
		t.Fatalf("peer list after remove:\n%s", out)
	}
}

// TestPeerDashboardPrefixPopup (T690): studio has a project of its own,
// web, and mini shares api. On api's remote dashboard prefix+a opened
// web's popup (the list rows' project, this server's first); it says
// instead that api has no project popup here and where its messages
// are, and opens nothing. Back on web, prefix+a opens web's popup.
func TestPeerDashboardPrefixPopup(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	mini.MustCLI("project", "new", "api")
	cfg := filepath.Join(mini.Home, "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if data, err = config.Edit(data, "projects.api", "share", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, data, 0o600); err != nil {
		t.Fatal(err)
	}
	mini.MustCLI("server", "start")
	studio.FakeClaude()
	studio.MustCLI("project", "new", "web")

	w := studio.Window(150, 30)
	w.WaitFor("◉ mini · ", 30*time.Second)
	w.WaitFor("■ api", 30*time.Second)
	w.Key(keyTab)
	for i := 0; i < 8 && !strings.Contains(w.Screen(), "mini / api"); i++ {
		w.Key(keyDown)
		time.Sleep(100 * time.Millisecond)
	}
	w.WaitFor("mini / api", wait)
	w.Key(keyEnter)
	w.WaitFor("tm dashboard · mini › api", wait)

	for _, k := range []string{"a", "t"} {
		w.Prefix(k)
		screen := w.WaitFor("api is mini's: no project popup here; your messages with it", wait)
		if strings.Contains(screen, "1 overview") {
			t.Fatalf("prefix+%s opened a popup:\n%s", k, screen)
		}
	}
	w.Prefix("C")
	w.WaitFor("api is mini's: nothing here acts on it", wait)

	// Back to web, in the sidebar: its own popup.
	w.Key(keyTab)
	for i := 0; i < 8 && strings.Contains(w.Screen(), "mini › api"); i++ {
		w.Key(keyUp)
		time.Sleep(400 * time.Millisecond) // the preview waits for the cursor to rest
	}
	w.WaitUntil("web's dashboard", wait, func(sc string) bool { return !strings.Contains(sc, "mini › api") })
	w.Prefix("a")
	screen := w.WaitFor("1 overview", wait)
	if strings.Contains(screen, "no project popup") {
		t.Fatalf("web's popup:\n%s", screen)
	}
}
