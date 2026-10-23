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

// unshareProject stops sharing env's project slug with its paired
// servers, as its Settings › Peers would.
func unshareProject(t *testing.T, env *Env, slug string) {
	t.Helper()
	cfg := filepath.Join(env.Home, "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if data, err = config.Edit(data, "projects."+slug, "share", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestPeerUnshareLinkedProjects (T701): mini shares api and docs, and
// studio's acme links mini/docs. acme's Linked projects lists both under
// mini; once mini stops sharing them, api drops out and docs, still
// linked, shows once, marked not shared any more; enter sets it off and
// it goes too.
func TestPeerUnshareLinkedProjects(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	studio.MustCLI("project", "new", "acme")
	mini.MustCLI("project", "new", "api")
	mini.MustCLI("project", "new", "docs")
	shareProject(t, mini, "api")
	shareProject(t, mini, "docs")
	studio.MustCLI("project", "link", "acme", "mini/docs", "both", "--no-ask")
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")

	studio.FakeClaude()
	w := studio.Window(120, 40)
	w.WaitFor("■ api", 30*time.Second)
	w.Type("A")
	w.WaitFor("1 overview", wait)
	w.Type("4")
	// Linked projects is the row above the last (Share).
	for range 40 {
		w.Key(keyDown)
	}
	w.Key(keyUp)
	w.WaitFor("Enter lists the projects, this server's and the paired servers'", wait)
	w.Key(keyEnter)
	screen := w.WaitFor("mini · paired server, linked", wait)
	for _, want := range []string{"  api ", "  docs "} {
		if !strings.Contains(screen, want) {
			t.Fatalf("Linked projects lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "not shared any more") {
		t.Fatalf("a shared project marked:\n%s", screen)
	}

	unshareProject(t, mini, "api")
	unshareProject(t, mini, "docs")
	screen = w.WaitUntil("api dropped, docs marked", 30*time.Second, func(s string) bool {
		return !strings.Contains(s, "  api ") && strings.Contains(s, "not shared any more")
	})
	if n := strings.Count(screen, "  docs "); n != 1 {
		t.Fatalf("docs shows %d times:\n%s", n, screen)
	}

	// The cursor is on docs, the one row left.
	w.Key(keyEnter)
	w.WaitUntil("docs set off and gone", wait, func(s string) bool {
		return !strings.Contains(s, "  docs ") && !strings.Contains(s, "mini · paired server")
	})
	if out := studio.MustCLI("link", "list", "--project", "acme"); strings.Contains(out, "mini/docs") {
		t.Fatalf("acme still links mini/docs:\n%s", out)
	}
}
