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
// (the breadcrumb, the link, nothing that acts on it), and the settings'
// Peers tab lists mini with its link; x there removes it after asking,
// and its group leaves the sidebar.
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
	for i := 0; i < 6 && !strings.Contains(w.Screen(), "mini / api · view-only"); i++ {
		w.Key(keyDown)
		time.Sleep(100 * time.Millisecond)
	}
	w.WaitFor("mini / api · view-only", wait)
	w.Key(keyEnter)
	w.WaitFor("tm dashboard · mini › api", wait)
	screen := w.WaitFor("◉ mini · linked", wait)
	for _, want := range []string{"view-only", "Read-only: nothing here acts on mini.", "≡ menu · ↑ ↓ scroll · n new project · , settings"} {
		if !strings.Contains(screen, want) {
			t.Errorf("missing %q in\n%s", want, screen)
		}
	}
	w.Type("a")
	w.WaitFor("api is mini's: view-only here", wait)

	w.Type(",")
	w.Type("4")
	w.WaitFor("PAIRED SERVERS · this server ", wait)
	screen = w.WaitFor("◉ mini  linked · ", wait)
	if !strings.Contains(screen, "shares 1 project") || !strings.Contains(screen, "panes  [off] · view · write") {
		t.Fatalf("Peers tab:\n%s", screen)
	}
	// studio has no project to share: the cursor starts on mini's panes.
	w.Type("x")
	w.WaitFor("Remove mini?", wait)
	w.Type("y")
	w.WaitFor("No paired servers", wait)
	w.Key(keyEsc)
	w.WaitFor("mini isn't paired with this server any more", 20*time.Second)
	if out := studio.MustCLI("peer", "list"); !strings.Contains(out, "no peers") {
		t.Fatalf("peer list after remove:\n%s", out)
	}
}
