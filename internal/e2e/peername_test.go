package e2e

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestPeerDisplayName (T702): studio calls mini "work" (tm peer name).
// Either word links acme to api there and sends to it, config.toml and
// the folders keep the alias, server.status and tm link list say the
// name, and mini hears nothing of it. In studio's console the sidebar's
// group and the Peers tab show the name.
func TestPeerDisplayName(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	studio.MustCLI("project", "new", "acme")
	mini.MustCLI("project", "new", "api")
	shareProject(t, mini, "api")
	if out := studio.MustCLI("peer", "name", "mini", "work"); !strings.Contains(out, "mini is now called work here") {
		t.Fatalf("peer name: %s", out)
	}
	if r := studio.CLI("peer", "name", "work", "acme"); r.Code == 0 || !strings.Contains(r.Stderr+r.Stdout, "name-taken") {
		t.Fatalf("a project's slug as the name: %d %s%s", r.Code, r.Stdout, r.Stderr)
	}
	studio.MustCLI("project", "link", "acme", "work/api", "both", "--no-ask")
	mini.MustCLI("project", "link", "api", "studio/acme", "both", "--no-ask")
	if b, _ := os.ReadFile(filepath.Join(studio.Home, "config.toml")); !strings.Contains(string(b), `"mini/api"`) || strings.Contains(string(b), "work/") {
		t.Fatalf("config.toml:\n%s", b)
	}
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	waitPeerState(t, mini, "studio", "up", "")

	var st proto.ServerStatus
	if err := json.Unmarshal([]byte(studio.MustCLI("server", "status", "--json")), &st); err != nil || st.Link == nil || len(st.Link.Peers) != 1 || st.Link.Peers[0].Name != "work" {
		t.Fatalf("server.status: %+v %v", st.Link, err)
	}

	ids := regexp.MustCompile(`(M-[0-9A-F]{4}-\d+)`)
	for _, to := range []string{"work/api", "mini/api"} {
		out := studio.MustCLI("link", "send", to, "Hello from "+to, "--project", "acme")
		m := ids.FindStringSubmatch(out)
		if m == nil || !strings.Contains(out, "work/api") {
			t.Fatalf("send to %s: %s", to, out)
		}
		waitLinkState(t, studio, "acme", "mini/api", m[1], "sent")
	}
	if out := studio.MustCLI("link", "list", "--project", "acme"); !strings.Contains(out, "work/api") || strings.Contains(out, "mini/api") {
		t.Fatalf("link list: %s", out)
	}
	if out := studio.MustCLI("link", "log", "work/api", "--project", "acme"); strings.Count(out, "acme → work/api") != 2 {
		t.Fatalf("link log: %s", out)
	}
	// The name is studio's own: mini's inbox names studio.
	if !Poll(30*time.Second, func() bool {
		out := mini.MustCLI("inbox", "list", "--project", "api")
		return strings.Count(out, "studio/acme wrote") == 2 && !strings.Contains(out, "work")
	}) {
		t.Fatalf("mini's inbox:\n%s", mini.MustCLI("inbox", "list", "--project", "api"))
	}

	studio.FakeClaude()
	w := studio.Window(110, 30)
	w.WaitFor("◉ work · ", 30*time.Second)
	w.WaitFor("■ api", 30*time.Second)
	w.Keys(",", "5")
	w.WaitFor("PAIRED SERVERS · this server ", wait)
	w.WaitFor("work (mini)", wait)
}
