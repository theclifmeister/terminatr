package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// peerMessageMasks hide what changes from run to run in the Messages
// tab with a paired server: the message and request ids carry a key's
// fingerprint, the ages and times move, and so does the padding a
// masked part leaves before the popup's right edge.
var peerMessageMasks = append([]Mask{
	{Name: "id", Re: regexp.MustCompile(`[MR]-[0-9A-F]{4}-\d+`)},
	{Name: "age", Re: regexp.MustCompile(`\b\d+[smhd]\b`)},
	{Name: "time", Re: regexp.MustCompile(`[A-Z][a-z]{2} \d{1,2} \d\d:\d\d`)},
	{Name: "server", Re: regexp.MustCompile(`◉ \S+ · this server *`)},
	{Name: "since", Re: regexp.MustCompile(`◉ mini · [^│]*`)},
	{Name: "pad", Re: regexp.MustCompile(`(?m) +│$`), Repl: " │"},
}, popupMasks...)

// shareProject shares env's project slug with its paired servers.
func shareProject(t *testing.T, env *Env, slug string) {
	t.Helper()
	cfg := filepath.Join(env.Home, "config.toml")
	data, err := os.ReadFile(cfg)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if data, err = config.Edit(data, "projects."+slug, "share", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestPeerMessagesTab (T683): acme on studio and api on mini, linked
// both ways across the pair, write to each other; docs on mini asks acme
// to link. In studio's console acme's Messages tab lists mini/api with
// its last message and mini/docs asking to link; enter opens the
// conversation, both ways with each message's state; X declines the
// request after a Y and mini hears of it.
func TestPeerMessagesTab(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	studio.MustCLI("project", "new", "acme")
	shareProject(t, studio, "acme")
	mini.MustCLI("project", "new", "api")
	mini.MustCLI("project", "new", "docs")
	studio.MustCLI("project", "link", "acme", "mini/api", "both", "--no-ask")
	mini.MustCLI("project", "link", "api", "studio/acme", "both", "--no-ask")
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	waitPeerState(t, mini, "studio", "up", "")

	out := studio.MustCLI("link", "send", "mini/api", "The API landed.", "--project", "acme")
	m := regexp.MustCompile(`(M-[0-9A-F]{4}-\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("send: %s", out)
	}
	waitLinkState(t, studio, "acme", "mini/api", m[1], "sent")
	out = mini.MustCLI("link", "send", "studio/acme", "Thanks, testing it.", "--re", m[1], "--project", "api")
	back := regexp.MustCompile(`(M-[0-9A-F]{4}-\d+)`).FindStringSubmatch(out)
	if back == nil {
		t.Fatalf("answer: %s", out)
	}
	if !Poll(30*time.Second, func() bool { return linkState(studio, "acme", "mini/api", back[1]) != "" }) {
		t.Fatalf("%s never reached acme", back[1])
	}
	if out := mini.MustCLI("project", "link", "docs", "studio/acme", "both", "--request"); !strings.Contains(out, "asked studio/acme to link back") {
		t.Fatalf("request: %s", out)
	}
	if !Poll(30*time.Second, func() bool {
		return strings.Contains(studio.MustCLI("link", "list", "--project", "acme"), "asks to link")
	}) {
		t.Fatalf("no request on studio:\n%s", studio.MustCLI("link", "list", "--project", "acme"))
	}

	studio.FakeClaude()
	w := studio.Window(120, 40)
	w.WaitFor("SESSIONS", wait)
	w.Type("A")
	w.WaitFor("1 overview", wait)
	w.Type("9")
	w.WaitFor("mini/docs", wait)
	screen := w.WaitFor("last "+back[1], wait)
	for _, want := range []string{"mini/api", "⇄ linked both ways on this side", "1 unread", "asks to link (R-", "enter open", "9 messages 1"} {
		if !strings.Contains(screen, want) {
			t.Errorf("Messages tab lacks %q:\n%s", want, screen)
		}
	}
	w.Golden("popup-messages-peer.txt", peerMessageMasks...)

	w.Key(keyEnter)
	screen = w.WaitFor("Thanks, testing it.", wait)
	for _, want := range []string{"acme · mini/api · ⇄ linked both ways on this side", "→ " + m[1], "The API landed.", "← " + back[1], "mini/api (its user) · re " + m[1], "M write"} {
		if !strings.Contains(screen, want) {
			t.Errorf("conversation lacks %q:\n%s", want, screen)
		}
	}
	w.Golden("popup-conversation-peer.txt", peerMessageMasks...)
	w.Key(keyEsc)

	// The request: X declines after a Y; mini's side hears of it.
	w.WaitFor("mini/docs", wait)
	w.Key(keyDown)
	w.WaitFor("A accept the link · X decline", wait)
	w.Type("X")
	w.WaitFor("Decline", wait)
	w.Type("Y")
	w.WaitFor("declined R-", wait)
	if !Poll(30*time.Second, func() bool {
		return strings.Contains(mini.MustCLI("inbox", "list", "--project", "docs"), "studio/acme declined docs's link request")
	}) {
		t.Fatalf("mini never heard:\n%s", mini.MustCLI("inbox", "list", "--project", "docs"))
	}
}
