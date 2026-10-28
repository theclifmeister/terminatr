package peer

import (
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// selfHome gives the test its own tm home and host name studio.
func selfHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("TERMINATR_HOME", h)
	was := hostName
	hostName = func() string { return "studio" }
	t.Cleanup(func() { hostName = was })
	return h
}

// This server's own name: the host label until [server] name is set; set
// and cleared like a peer's name, refused when a paired server or a
// project has it.
func TestSelfName(t *testing.T) {
	h := selfHome(t)
	dir := filepath.Join(h, "peer")
	if SelfName() != "studio" || OwnName() != "" {
		t.Fatalf("unset: %q %q", SelfName(), OwnName())
	}
	a, b := newKey(t), newKey(t)
	if err := SavePeers(dir, []Peer{{Alias: "mini", Name: "work", Key: pub(a), Relay: "wss://r"}, {Alias: "nas", Told: "attic", Key: pub(b), Relay: "wss://r"}}); err != nil {
		t.Fatal(err)
	}
	taken := func(n string) string {
		if n == "acme" {
			return "a project here is called that"
		}
		return ""
	}
	for _, n := range []string{"mini", "work", "attic", "acme"} {
		var ne *NameError
		if err := SetSelfName(dir, n, taken); !errors.As(err, &ne) {
			t.Errorf("SetSelfName(%s): %v, want a NameError", n, err)
		}
	}
	if err := SetSelfName(dir, "Bad", taken); err == nil || CheckName("Bad") == nil {
		t.Errorf("SetSelfName(Bad): %v", err)
	}
	if err := SetSelfName(dir, "office", taken); err != nil || SelfName() != "office" || OwnName() != "office" {
		t.Fatalf("set: %v, %q", err, SelfName())
	}
	if c, err := config.Load(); err != nil || c.ServerName != "office" || len(c.Unknown) != 0 {
		t.Fatalf("config: %+v, %v", c, err)
	}
	if err := SetSelfName(dir, "", taken); err != nil || SelfName() != "studio" || OwnName() != "" {
		t.Fatalf("clear: %v, %q", err, SelfName())
	}
	// One written by hand that isn't a name: the host label.
	if err := os.WriteFile(filepath.Join(h, "config.toml"), []byte("[server]\nname = \"Not a name\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if SelfName() != "studio" || OwnName() != "" {
		t.Fatalf("invalid: %q %q", SelfName(), OwnName())
	}
}

// A name a server gives itself shows on the other side in its alias's
// place, never instead of a name set there or another peer's word; the
// alias stays.
func TestToldNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peer")
	a, b, c := newKey(t), newKey(t), newKey(t)
	if err := SavePeers(dir, []Peer{
		{Alias: "mbp", Told: "office", Key: pub(a), Relay: "wss://r"},
		{Alias: "mini", Name: "work", Told: "box", Key: pub(b), Relay: "wss://r"},
		{Alias: "nas", Told: "nas", Key: pub(c), Relay: "wss://r"},
	}); err != nil {
		t.Fatal(err)
	}
	peers, _ := LoadPeers(dir)
	if peers[0].Shown() != "office" || peers[0].Both() != "office (mbp)" || peers[1].Shown() != "work" || peers[2].Both() != "nas" {
		t.Fatalf("shown: %s, %s, %s", peers[0].Both(), peers[1].Both(), peers[2].Both())
	}
	n := NamesOf(peers)
	for in, want := range map[string]string{"office": "mbp", "mbp": "mbp", "work": "mini", "box": "box"} {
		if got := n.Alias(in); got != want {
			t.Errorf("Alias(%s) = %s, want %s", in, got, want)
		}
	}
	if n.Show("mbp") != "office" || n.Show("mini") != "work" {
		t.Errorf("Show: %s, %s", n.Show("mbp"), n.Show("mini"))
	}
	// A name set here may not be a told one; setting one wins over it.
	var ne *NameError
	if _, err := SetName(dir, "nas", "office", nil); !errors.As(err, &ne) {
		t.Errorf("SetName to a told name: %v", err)
	}
	if p, err := SetName(dir, "mbp", "laptop", nil); err != nil || p.Shown() != "laptop" || p.Told != "office" {
		t.Fatalf("SetName: %+v, %v", p, err)
	}
	// Pairing again keeps it.
	if p, err := Pin(dir, pub(a), "wss://r", "office", ""); err != nil || p.Alias != "mbp" || p.Told != "office" {
		t.Fatalf("Pin again: %+v, %v", p, err)
	}
}

// startNamed starts a's manager telling the name *name says.
func (s *server) startNamed(t *testing.T, tc *tls.Config, name func() string) *Manager {
	t.Helper()
	m := Start(Options{Dir: s.dir, Version: "1", Logf: t.Logf, RelayTLS: tc, Name: name,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m.Close)
	return m
}

// told waits until s keeps told for the peer alias.
func (s *server) told(t *testing.T, alias, told string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var got Peer
	for time.Now().Before(deadline) {
		peers, _ := LoadPeers(s.dir)
		for _, p := range peers {
			if p.Alias == alias {
				got = p
			}
		}
		if got.Told == told {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s told %q, want %q", alias, got.Told, told)
}

// A server's own name crosses the link: in the hello, and by peer.name
// when it changes; the other side keeps it as told, shows it, and keeps
// its alias. An older tm (no CapName) leaves the name it told; one
// another peer has there isn't kept.
func TestToldOverLink(t *testing.T) {
	selfHome(t)
	_, _, url, tc := startRelay(t)
	a, b, c := newServer(t), newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "mbp", a, url)
	b.pair(t, "nas", c, url)

	var mu sync.Mutex
	name := "office"
	own := func() string { mu.Lock(); defer mu.Unlock(); return name }
	set := func(n string) { mu.Lock(); name = n; mu.Unlock() }

	ma := a.startNamed(t, tc, own)
	mb := b.startNamed(t, tc, func() string { return "" })
	waitLink(t, mb, "mbp", "up, called office", func(p proto.PeerLink) bool { return isUp(p) && p.Name == "office" })
	b.told(t, "mbp", "office")
	if l := waitLink(t, ma, "mini", "up", isUp); l.Name != "" {
		t.Errorf("b has no name set, but a shows %q", l.Name)
	}
	if NamesOf(must(LoadPeers(b.dir))).Alias("office") != "mbp" {
		t.Error("office doesn't name mbp on b")
	}

	// Renamed: peer.name on the live link.
	set("work")
	ma.Reload()
	b.told(t, "mbp", "work")
	waitLink(t, mb, "mbp", "called work", func(p proto.PeerLink) bool { return p.Name == "work" && p.Alias == "mbp" })

	// A name another peer has on b: b shows the alias.
	set("nas")
	ma.Reload()
	b.told(t, "mbp", "")
	waitLink(t, mb, "mbp", "shown by alias", func(p proto.PeerLink) bool { return p.Name == "" })

	// Back to work, then a runs an older tm: b keeps work.
	set("work")
	ma.Reload()
	b.told(t, "mbp", "work")
	ma.Close()
	mo := a.start(t, "0.30.0", tc)
	waitLink(t, mb, "mbp", "up with the older tm", func(p proto.PeerLink) bool { return isUp(p) && p.Version == "0.30.0" })
	b.told(t, "mbp", "work")

	// Cleared on a tm that knows names: b shows the alias again.
	mo.Close()
	set("")
	a.startNamed(t, tc, own)
	b.told(t, "mbp", "")
	waitLink(t, mb, "mbp", "shown by alias again", func(p proto.PeerLink) bool { return isUp(p) && p.Name == "" })
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
