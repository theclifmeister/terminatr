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
// studio's acme links mini/docs. acme's Linked projects opens the
// Network popup on acme: its link to mini/docs under it, mini's api and
// docs under mini, docs with the link acme has to it. Once mini stops
// sharing them both leave mini's rows and the link, still set, says not
// shared any more; X unlinks it after a Y and it goes.
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
	// Linked projects is two rows above the last (Share, then Mods).
	for range 40 {
		w.Key(keyDown)
	}
	w.Key(keyUp)
	w.Key(keyUp)
	w.WaitFor("Enter opens the Network popup on this project", wait)
	w.Key(keyEnter)
	w.WaitFor("Network · ", wait)
	screen := w.WaitUntil("mini's projects", 30*time.Second, func(s string) bool {
		return strings.Contains(s, "└─ ⇄ mini/docs") && strings.Contains(s, "■ api") && strings.Contains(s, "⇄ acme")
	})
	if strings.Contains(screen, "not shared any more") {
		t.Fatalf("a shared project marked:\n%s", screen)
	}

	unshareProject(t, mini, "api")
	unshareProject(t, mini, "docs")
	w.WaitUntil("api and docs gone from mini, the link marked", 30*time.Second, func(s string) bool {
		return !strings.Contains(s, "■ api") && !strings.Contains(s, "■ docs") && strings.Contains(s, "not shared any more")
	})

	// The cursor is on acme; the link is under it.
	w.Key(keyDown)
	w.WaitFor("X unlink", wait)
	w.Type("X")
	w.WaitFor("This removes this side", wait)
	w.Type("Y")
	w.WaitUntil("the link gone", wait, func(s string) bool { return !strings.Contains(s, "⇄ mini/docs") })
	if out := studio.MustCLI("link", "list", "--project", "acme"); strings.Contains(out, "mini/docs") {
		t.Fatalf("acme still links mini/docs:\n%s", out)
	}
}
