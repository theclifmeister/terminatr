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

// TestPeerNeeds (T838): mini shares api, whose coordinator asks a
// question (tm ask) and which has a task in review. Off, the default,
// studio's all-projects NEEDS YOU counts the question, its text masked.
// With tm peer needs studio view on mini, each need is a row of its own
// with what it asks and "answer on mini", and tm network on studio says
// mini gives it view; off again, the count is back.
func TestPeerNeeds(t *testing.T) {
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
	mini.MustCLI("ask", "add", "Which cache?", "--project", "api", "--header", "Cache",
		"--option", "Redis", "--option", "In memory")
	mini.MustCLI("task", "add", "Rate limits", "--status", "review", "--project", "api")
	mini.MustCLI("server", "start")
	studio.FakeClaude()
	studio.MustCLI("project", "new", "web")

	w := studio.Window(150, 30)
	w.WaitFor("◉ mini · ", 30*time.Second)
	w.WaitFor("■ api", 30*time.Second)
	w.ClickText("PROJECTS", 0)
	w.WaitFor("tm dashboard · all projects", wait)
	screen := w.WaitFor("1 question waiting", 30*time.Second)
	if strings.Contains(screen, "Which cache") {
		t.Fatalf("the question's text crossed with the switch off:\n%s", screen)
	}

	out := mini.MustCLI("peer", "needs", "studio", "view")
	if !strings.Contains(out, "Needs view for studio") {
		t.Fatalf("tm peer needs: %q", out)
	}
	screen = w.WaitFor("asks: Which cache?", 30*time.Second)
	for _, want := range []string{"answer on mini", "T1  Rate limits", "in review"} {
		if !strings.Contains(screen, want) {
			t.Errorf("missing %q in\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "1 question waiting") {
		t.Errorf("the masked count shows beside the needs in full:\n%s", screen)
	}
	w.WaitUntil("tm network says mini gives view", 20*time.Second, func(string) bool {
		return strings.Contains(studio.MustCLI("network"), "it gives you view")
	})

	mini.MustCLI("peer", "needs", "studio", "off")
	w.WaitUntil("the needs masked again", 30*time.Second, func(sc string) bool {
		return strings.Contains(sc, "1 question waiting") && !strings.Contains(sc, "Which cache")
	})
}
