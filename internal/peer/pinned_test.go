package peer

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// startPinnedRelay runs an in-process relay on 127.0.0.1 with a
// self-signed certificate made in state, as tm relay serve --self-signed
// does: its address with the pin.
func startPinnedRelay(t *testing.T, state string) (*relay.Server, string) {
	t.Helper()
	cert, key, pin, err := relay.SelfSigned(state, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := relay.New(relay.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		relay.Serve(ctx, ln, s, relay.TLS{CertFile: cert, KeyFile: key})
	}()
	t.Cleanup(func() { cancel(); <-done })
	return s, "wss://" + ln.Addr().String() + "#sha256=" + pin
}

func TestRelayURLPin(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	for in, want := range map[string]string{
		"203.0.113.5#sha256=" + pin:                            "wss://203.0.113.5#sha256=" + pin,
		"wss://203.0.113.5/v1/#SHA256=" + strings.ToUpper(pin): "wss://203.0.113.5#sha256=" + pin,
		"wss://[2001:db8::7]:8443#sha256=" + pin:               "wss://[2001:db8::7]:8443#sha256=" + pin,
		"relay.example.net":                                    "wss://relay.example.net",
	} {
		if got, err := RelayURL(in); err != nil || got != want {
			t.Errorf("RelayURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"203.0.113.5#sha256=abc", "203.0.113.5#md5=" + pin, "ws://127.0.0.1#sha256=" + pin, "203.0.113.5?x#sha256=" + pin} {
		if got, err := RelayURL(bad); err == nil {
			t.Errorf("RelayURL(%q) took it: %q", bad, got)
		}
	}
	// peers.json takes a pinned relay, and refuses a bad pin.
	dir := t.TempDir()
	key := pub(newKey(t))
	if err := SavePeers(dir, []Peer{{Alias: "nas", Key: key, Relay: "wss://203.0.113.5#sha256=" + pin}}); err != nil {
		t.Fatal(err)
	}
	if ps, err := LoadPeers(dir); err != nil || ps[0].Relay != "wss://203.0.113.5#sha256="+pin {
		t.Fatalf("LoadPeers %+v %v", ps, err)
	}
	if err := SavePeers(dir, []Peer{{Alias: "nas", Key: key, Relay: "wss://203.0.113.5#sha256=12"}}); err == nil {
		t.Fatal("SavePeers took a bad pin")
	}
	if ok, err := SetRelay(dir, "nas", "wss://198.51.100.9#sha256="+pin); !ok || err != nil {
		t.Fatalf("SetRelay %v %v", ok, err)
	}
	if ps, _ := LoadPeers(dir); ps[0].Relay != "wss://198.51.100.9#sha256="+pin || !ps[0].Key.Equal(key) {
		t.Fatalf("after SetRelay %+v", ps)
	}
	if ok, _ := SetRelay(dir, "nobody", "wss://x.example"); ok {
		t.Fatal("SetRelay found nobody")
	}
}

// A pinned relay's code carries the pin, reads back, and is refused by
// a tm that predates pins (whose RelayURL took no fragment) instead of
// being taken without it.
func TestCodePin(t *testing.T) {
	pin := strings.Repeat("0f", 32)
	var sec Secret
	for i := range sec {
		sec[i] = byte(i * 37)
	}
	for relayURL, text := range map[string]string{
		"wss://203.0.113.5#sha256=" + pin:        "203.0.113.5#sha256=" + pin + "/" + sec.String(),
		"wss://203.0.113.5:8443#sha256=" + pin:   "203.0.113.5:8443#sha256=" + pin + "/" + sec.String(),
		"wss://[2001:db8::7]#sha256=" + pin:      "[2001:db8::7]#sha256=" + pin + "/" + sec.String(),
		"wss://h.example/tm/relay#sha256=" + pin: "wss://h.example/tm/relay#sha256=" + pin + "/" + sec.String(),
		"wss://relay.example.net":                "relay.example.net/" + sec.String(),
	} {
		c := Code{Relay: relayURL, Secret: sec}
		if c.String() != text {
			t.Errorf("%s: code %q, want %q", relayURL, c.String(), text)
		}
		back, err := ParseCode(c.String())
		if err != nil || back != c {
			t.Errorf("ParseCode(%q) = %+v, %v", c.String(), back, err)
		}
		if c.Pinned() != strings.Contains(relayURL, "#") {
			t.Errorf("%s: Pinned %v", relayURL, c.Pinned())
		}
		// Typed again: the secret's look-alikes, a pin in capitals.
		typed := strings.ReplaceAll(text, pin, strings.ToUpper(pin))
		typed = strings.ReplaceAll(typed, sec.String(), strings.ToLower(strings.ReplaceAll(sec.String(), "-", " ")))
		if back, err := ParseCode(typed); err != nil || back != c {
			t.Errorf("ParseCode(%q) = %+v, %v", typed, back, err)
		}
	}
	// An old code (no pin) reads as before.
	if c, err := ParseCode("relay.example.net/" + sec.String()); err != nil || c.Relay != "wss://relay.example.net" || c.Pinned() {
		t.Fatalf("old code: %+v %v", c, err)
	}
	// tm before pins: its RelayURL refused any fragment.
	old := func(s string) error {
		i := strings.LastIndexByte(s, '/')
		u := "wss://" + s[:i]
		if strings.Contains(u, "#") {
			return errors.New("a host name, or a wss:// URL")
		}
		return nil
	}
	if old(Code{Relay: "wss://203.0.113.5#sha256=" + pin, Secret: sec}.String()) == nil {
		t.Fatal("a tm without pins would take a pinned code")
	}
	if _, err := ParseCode("203.0.113.5#sha256=12/" + sec.String()); err == nil || !strings.Contains(err.Error(), "64 hex digits") {
		t.Fatalf("bad pin: %v", err)
	}
}

// The client takes a self-signed relay by its pin alone, and refuses it
// under another pin, or without one.
func TestDialPinned(t *testing.T) {
	_, url := startPinnedRelay(t, filepath.Join(t.TempDir(), "relay"))
	a := newServer(t)
	dial := func(u string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		d := relay.Dialer{Key: a.id.Key, Kind: relay.KindPair, OnWait: cancel}
		c, err := d.Dial(ctx, u)
		if c != nil {
			c.Close()
		}
		return err
	}
	if err := dial(url); !errors.Is(err, context.Canceled) {
		t.Fatalf("pinned: %v (want the wait in the room)", err)
	}
	base, _, _ := relay.SplitPin(url)
	var ve *tls.CertificateVerificationError
	if err := dial(base); !errors.As(err, &ve) {
		t.Fatalf("no pin: %v", err)
	}
	var pe *relay.PinError
	err := dial(base + "#sha256=" + strings.Repeat("ab", 32))
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "has a different key than when you paired") || !strings.Contains(err.Error(), "tm peer relay") {
		t.Fatalf("other pin: %v", err)
	}
}

// Two servers pair through a relay on 127.0.0.1 with a self-signed
// certificate, the joiner learning its pin from the code; their link
// comes up through it with no CA at all. The relay set up again with a
// new key is refused until both are given its new address.
func TestPairThroughPinnedRelay(t *testing.T) {
	_, url := startPinnedRelay(t, filepath.Join(t.TempDir(), "relay"))
	a, b := newServer(t), newServer(t)
	code, err := NewCode(url)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseCode(code.String())
	if err != nil || back.Relay != url {
		t.Fatalf("the joiner reads %q from %q: %v", back.Relay, code.String(), err)
	}
	_, pa, pb, ea, eb := pairBoth(t, a, b, url, PairOptions{Version: "0.28.0"}, true, true)
	if ea != nil || eb != nil {
		t.Fatalf("pair: %v / %v", ea, eb)
	}
	if _, err := Pin(a.dir, pa.Key, url, pa.Name, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Pin(b.dir, pb.Key, back.Relay, pb.Name, ""); err != nil {
		t.Fatal(err)
	}
	ma, mb := a.start(t, "0.28.0", nil), b.start(t, "0.28.0", nil)
	pl := waitLink(t, ma, "mini", "up", isUp)
	waitLink(t, mb, "studio", "up", isUp)
	if !strings.HasPrefix(pl.Transport, "relay 127.0.0.1:") {
		t.Errorf("transport %q", pl.Transport)
	}

	// The relay comes back with another key: refused, and said why.
	_, moved := startPinnedRelay(t, filepath.Join(t.TempDir(), "relay2"))
	base, _, _ := relay.SplitPin(moved)
	_, oldPin, _ := relay.SplitPin(url)
	for _, s := range []*server{a, b} {
		ps, _ := LoadPeers(s.dir)
		ps[0].Relay = base + "#sha256=" + oldPin
		SavePeers(s.dir, ps)
	}
	ma.Reload()
	waitLink(t, ma, "mini", "refused for its key", func(p proto.PeerLink) bool {
		return p.State == proto.LinkOffline && strings.Contains(p.Error, "has a different key than when you paired")
	})
	// tm peer relay on both: up again.
	for _, s := range []struct {
		srv   *server
		m     *Manager
		alias string
	}{{a, ma, "mini"}, {b, mb, "studio"}} {
		if ok, err := SetRelay(s.srv.dir, s.alias, moved); !ok || err != nil {
			t.Fatal(ok, err)
		}
		s.m.Reload()
	}
	waitLink(t, ma, "mini", "up through the new relay", isUp)
	waitLink(t, mb, "studio", "up through the new relay", isUp)
}
