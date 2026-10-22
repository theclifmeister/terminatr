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

// TestPeerProjectPopup (T693): mini shares api and lets studio view its
// panes. On api's remote dashboard A opens mini's popup of api, read
// from mini: its goal, its board, its settings and its memory's titles,
// tab by tab; a key that would act says to change it on mini. With the
// switch back to off, A gives the remote dashboard's message again.
func TestPeerProjectPopup(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	mini.MustCLI("project", "new", "api", "--goal", "the billing API")
	cfg := filepath.Join(mini.Home, "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if data, err = config.Edit(data, "projects.api", "share", true); err != nil {
		t.Fatal(err)
	}
	if data, err = config.Edit(data, "projects.api", "branch_name", "users/SECRET-USER/{id}"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, data, 0o600); err != nil {
		t.Fatal(err)
	}
	mini.MustCLI("task", "add", "Refund the duplicate charges", "--status", "ready", "--project", "api")
	mem := filepath.Join(mini.Home, "projects", "api", "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mem, "deploy.md"), []byte("+++\ntitle = \"Deploying the API\"\n+++\nSECRET-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mini.MustCLI("peer", "panes", "studio", "view")
	mini.MustCLI("server", "start")
	studio.FakeClaude()
	studio.MustCLI("project", "new", "web")

	w := studio.Window(150, 34)
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

	w.Type("A")
	w.WaitFor("mini's api", wait)
	w.WaitFor("the billing API", wait)
	w.WaitFor("change it on mini", wait)
	w.Type("3")
	w.WaitFor("Refund the duplicate charges", wait)
	w.Type("4")
	screen := w.WaitFor("Protection", wait)
	if strings.Contains(screen, "SECRET") {
		t.Fatalf("the branch template crossed:\n%s", screen)
	}
	w.Type("6")
	screen = w.WaitFor("Deploying the API", wait)
	if strings.Contains(screen, "SECRET-BODY") {
		t.Fatalf("a note's text crossed:\n%s", screen)
	}
	w.Type("3")
	w.Type("D")
	w.WaitFor("are mini's: change it on mini", wait)
	if out := mini.MustCLI("task", "show", "T1", "--project", "api"); !strings.Contains(out, "status: ready") {
		t.Errorf("D acted on mini's task:\n%s", out)
	}
	w.Key(keyEsc)
	w.WaitUntil("the popup closed", wait, func(sc string) bool { return !strings.Contains(sc, "mini's api") })

	// The switch off: no popup, the message.
	mini.MustCLI("peer", "panes", "studio", "off")
	w.WaitUntil("the switch off reached studio", 20*time.Second, func(string) bool {
		w.Type("A")
		time.Sleep(300 * time.Millisecond)
		sc := w.Screen()
		if strings.Contains(sc, "mini's api") {
			w.Key(keyEsc)
			return false
		}
		return strings.Contains(sc, "api is mini's: no project popup here")
	})
}
