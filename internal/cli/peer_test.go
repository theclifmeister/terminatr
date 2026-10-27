package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// stubPeerServer stands in for the server: reloads are counted, and the
// links are links (nil: no server running).
func stubPeerServer(t *testing.T, links *proto.LinkStatus) *int {
	t.Helper()
	n := 0
	oldReload, oldLinks := reloadPeers, peerLinks
	t.Cleanup(func() { reloadPeers, peerLinks = oldReload, oldLinks })
	reloadPeers = func() error { n++; return nil }
	peerLinks = func() (*proto.LinkStatus, bool) { return links, links != nil }
	return &n
}

func TestPeerUsageAndGuard(t *testing.T) {
	h := newHarness(t)
	stubPeerServer(t, nil)
	h.expect(2, "usage: tm peer pair", human, "peer")
	h.expect(2, "usage: tm peer pair", human, "peer", "nope")
	h.expect(2, "usage: tm peer pair", human, "peer", "join")
	h.expect(2, "takes no --relay", human, "peer", "join", "r/x", "--relay", "r")
	h.expect(2, "takes no --yes", human, "peer", "pair", "--yes")
	h.expect(2, "--as: alias", human, "peer", "pair", "--relay", "r.example.net", "--as", "Bad Name")
	h.expect(2, "--relay URL", human, "peer", "pair")
	h.expect(2, "--relay: relay", human, "peer", "pair", "--relay", "https://r.example.net")
	h.expect(2, "16 digits", human, "peer", "join", "r.example.net/1234")
	for _, c := range [][]string{{"pair", "--relay", "r.example.net"}, {"join", "r.example.net/7Q4M-K2XD-9HVA-PT3C"}, {"list"}, {"remove", "mini"}, {"rotate", "--yes"}, {"panes", "mini", "type"}} {
		h.expect(1, "human-only", coord, append([]string{"peer"}, c...)...)
		h.expect(1, "human-only", thr, append([]string{"peer"}, c...)...)
	}
}

func TestPeerListRemoveRotate(t *testing.T) {
	h := newHarness(t)
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "isn't paired") {
		t.Fatalf("list before pairing: %q", out)
	}
	dir, err := peer.Dir()
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	k1, _, _ := ed25519.GenerateKey(nil)
	k2, _, _ := ed25519.GenerateKey(nil)
	peers := []peer.Peer{{Alias: "mini", Key: k1, Relay: "wss://r.example.net"}, {Alias: "nas", Key: k2, Relay: "wss://r.example.net"}}
	if err := peer.SavePeers(dir, peers); err != nil {
		t.Fatal(err)
	}
	reloads := stubPeerServer(t, &proto.LinkStatus{ID: id.ID.String(), Peers: []proto.PeerLink{
		{Alias: "mini", ID: peers[0].ID().String(), State: proto.LinkUp, Since: time.Now().Add(-time.Minute), Version: "0.27.0"},
	}})
	out := h.ok(human, "peer", "list")
	for _, want := range []string{"server id " + id.ID.Short(), "mini  " + peers[0].ID().Short() + "  r.example.net  up 1m0s · tm 0.27.0", "nas   " + peers[1].ID().Short() + "  r.example.net  no link yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	var js struct {
		ID    string      `json:"id"`
		Peers []peerEntry `json:"peers"`
	}
	if err := json.Unmarshal([]byte(h.ok(human, "peer", "list", "--json")), &js); err != nil || js.ID != id.ID.String() || len(js.Peers) != 2 || js.Peers[0].Link == nil || js.Peers[1].Link != nil {
		t.Fatalf("list --json: %+v, %v", js, err)
	}

	// pair defaults to the relay every peer uses (here it then can't
	// reach it: nothing listens on r.example.net in a test).
	if r, err := pairRelay(dir, ""); err != nil || r != "wss://r.example.net" {
		t.Fatalf("pairRelay: %q, %v", r, err)
	}

	// The pane switch (docs/SPEC.md §16.12): write, or type, its older
	// name; peers.json keeps "type", which tm v0.27.0 reads too.
	h.expect(2, "off|view|write", human, "peer", "panes", "mini", "talk")
	h.expect(2, "usage: tm peer pair", human, "peer", "panes", "mini")
	h.expect(1, "unknown-peer", human, "peer", "panes", "studio", "view")
	if out := h.ok(human, "peer", "panes", "mini", "write"); !strings.Contains(out, "Panes write for mini") || *reloads != 1 {
		t.Fatalf("panes: %q, %d reloads", out, *reloads)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "panes: write") {
		t.Fatalf("list with panes: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Alias != "mini" || ps[0].Panes != proto.PanesType {
		t.Fatalf("after panes: %+v", ps)
	}
	if out := h.ok(human, "peer", "list", "--json"); !strings.Contains(out, `"panes": "write"`) && !strings.Contains(out, `"panes":"write"`) {
		t.Fatalf("list --json with panes: %q", out)
	}
	h.ok(human, "peer", "panes", "mini", "view")
	if out := h.ok(human, "peer", "panes", "mini", "type"); !strings.Contains(out, "Panes write for mini") {
		t.Fatalf("panes type: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Panes != proto.PanesType {
		t.Fatalf("after panes type: %+v", ps)
	}
	// The conversations switch (§16.14): on by default, off kept in
	// peers.json, on again with no key.
	h.expect(2, "on|off", human, "peer", "conversations", "mini", "maybe")
	h.expect(1, "unknown-peer", human, "peer", "conversations", "studio", "off")
	if out := h.ok(human, "peer", "conversations", "mini", "off"); !strings.Contains(out, "Conversations off for mini") {
		t.Fatalf("conversations off: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Conversations != proto.ConversationsNot || ps[0].ConversationsOn() || ps[0].Panes != proto.PanesType {
		t.Fatalf("after conversations off: %+v", ps)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "panes: write, conversations off") {
		t.Fatalf("list with conversations off: %q", out)
	}
	if out := h.ok(human, "peer", "conversations", "mini", "on"); !strings.Contains(out, "it can read the conversations") || !strings.Contains(out, "at panes write") {
		t.Fatalf("conversations on: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Conversations != "" {
		t.Fatalf("after conversations on: %+v", ps)
	}
	h.ok(human, "peer", "panes", "mini", "off")
	if out := h.ok(human, "peer", "conversations", "mini", "on"); !strings.Contains(out, "its panes are off") {
		t.Fatalf("conversations on, panes off: %q", out)
	}
	h.expect(1, "human-only", coord, "peer", "conversations", "mini", "off")
	*reloads = 0

	h.expect(1, "unknown-peer", human, "peer", "remove", "studio")
	if out := h.ok(human, "peer", "remove", "nas"); !strings.Contains(out, "Removed nas") || *reloads != 1 {
		t.Fatalf("remove: %q, %d reloads", out, *reloads)
	}
	if ps, _ := peer.LoadPeers(dir); len(ps) != 1 || ps[0].Alias != "mini" {
		t.Fatalf("after remove: %+v", ps)
	}

	if code, out, _ := h.run(human, "n\n", "peer", "rotate"); code != 1 || !strings.Contains(out, "nothing done") {
		t.Fatalf("rotate, no: %d %q", code, out)
	}
	if again, _ := peer.LoadIdentity(dir); again.ID != id.ID {
		t.Fatal("rotate without a yes changed the key")
	}
	code, out, errs := h.run(human, "y\n", "peer", "rotate")
	if code != 0 || !strings.Contains(out, "Pair again with mini: tm peer pair --as mini") || *reloads != 2 {
		t.Fatalf("rotate: %d %q %q", code, out, errs)
	}
	if again, _ := peer.LoadIdentity(dir); again.ID == id.ID {
		t.Fatal("rotate kept the key")
	}
}

// A join nobody waits for fails at once, as a refusal.
func TestPeerJoinNobody(t *testing.T) {
	h := newHarness(t)
	stubPeerServer(t, nil)
	s := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(s)
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	code := "ws" + strings.TrimPrefix(hs.URL, "http") + "/7Q4M-K2XD-9HVA-PT3C"
	h.expect(1, "bad-code: nobody is pairing with this code", human, "peer", "join", code)
	dir, _ := peer.Dir()
	if ps, _ := peer.LoadPeers(dir); len(ps) != 0 {
		t.Fatalf("pinned %+v", ps)
	}
}

// tm peer relay gives a paired server another relay: a pinned one too.
func TestPeerRelay(t *testing.T) {
	h := newHarness(t)
	reloads := stubPeerServer(t, nil)
	dir, _ := peer.Dir()
	if _, _, err := peer.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	k1, _, _ := ed25519.GenerateKey(nil)
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "mini", Key: k1, Relay: "wss://r.example.net"}}); err != nil {
		t.Fatal(err)
	}
	pin := strings.Repeat("ab", 32)
	h.expect(2, "usage: tm peer pair", human, "peer", "relay", "mini")
	h.expect(2, "64 hex digits", human, "peer", "relay", "mini", "203.0.113.5#sha256=12")
	h.expect(1, "unknown-peer", human, "peer", "relay", "nas", "203.0.113.5#sha256="+pin)
	h.expect(1, "human-only", coord, "peer", "relay", "mini", "203.0.113.5#sha256="+pin)
	out := h.ok(human, "peer", "relay", "mini", "192.168.1.20#SHA256="+strings.ToUpper(pin))
	if !strings.Contains(out, "through 192.168.1.20 (pinned)") || !strings.Contains(out, "tm peer relay ALIAS wss://192.168.1.20#sha256="+pin) ||
		!strings.Contains(out, "192.168.1.20 is a private address") || *reloads != 1 {
		t.Fatalf("relay: %q, %d reloads", out, *reloads)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Relay != "wss://192.168.1.20#sha256="+pin || !ps[0].Key.Equal(k1) {
		t.Fatalf("after: %+v", ps)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "192.168.1.20 (pinned)") {
		t.Fatalf("list: %q", out)
	}
}

// Joining through a self-signed relay on 127.0.0.1: its pin is taken
// from the code, and a relay with another key is refused in so many
// words.
func TestPeerJoinPinned(t *testing.T) {
	h := newHarness(t)
	stubPeerServer(t, nil)
	state := t.TempDir()
	cert, key, pin, err := relay.SelfSigned(state, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := relay.New(relay.DefaultConfig())
	hs := httptest.NewUnstartedServer(s)
	c, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	hs.TLS = &tls.Config{Certificates: []tls.Certificate{c}}
	hs.StartTLS()
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	host := strings.TrimPrefix(hs.URL, "https://")
	code, out, errs := h.run(human, "", "peer", "join", host+"#sha256="+pin+"/7Q4M-K2XD-9HVA-PT3C")
	if code != 1 || !strings.Contains(errs, "nobody is pairing with this code") || strings.Contains(out, "private address") {
		t.Fatalf("join: %d %q %q", code, out, errs)
	}
	h.expect(3, "the relay at "+host+" has a different key than when you paired", human, "peer", "join", host+"#sha256="+strings.Repeat("ab", 32)+"/7Q4M-K2XD-9HVA-PT3C")
}

// tm peer relay-update asks the paired server's relay to update itself,
// with this server's key, while its link through the relay is live.
func TestPeerRelayUpdate(t *testing.T) {
	h := newHarness(t)
	stubPeerServer(t, nil)
	file := filepath.Join(t.TempDir(), "update-request")
	cfg := relay.DefaultConfig()
	cfg.Version, cfg.UpdateFile = "v0.27.0", file
	cfg.UpdateAfter = 0 // the link may ask once it has carried bytes
	s := relay.New(cfg)
	hs := httptest.NewServer(s)
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")

	dir, _ := peer.Dir()
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	pk, k, _ := ed25519.GenerateKey(nil)
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "mini", Key: pk, Relay: url}}); err != nil {
		t.Fatal(err)
	}
	h.expect(2, "usage: tm peer pair", human, "peer", "relay-update")
	h.expect(1, "unknown-peer", human, "peer", "relay-update", "nas")
	h.expect(1, "human-only", coord, "peer", "relay-update", "mini")
	h.expect(1, "only from a server whose link through it is up", human, "peer", "relay-update", "mini")

	// The link, live: this server's key and mini's meet in their room.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	room := relay.LinkRoom(id.ID, relay.Fingerprint(pk))
	done := make(chan *relay.Conn, 2)
	for _, d := range []relay.Dialer{
		{Key: id.Key, Kind: relay.KindLink, Room: room, Peer: relay.Fingerprint(pk)},
		{Key: k, Kind: relay.KindLink, Room: room, Peer: id.ID},
	} {
		go func() {
			c, err := d.Dial(ctx, url)
			if err != nil {
				t.Error(err)
			}
			done <- c
		}()
	}
	var ends []*relay.Conn
	for range 2 {
		if c := <-done; c != nil {
			defer c.Close()
			ends = append(ends, c)
		}
	}
	if len(ends) != 2 {
		t.FailNow()
	}
	// A byte each way, as the link's TLS handshake would send.
	for _, p := range [][2]*relay.Conn{{ends[0], ends[1]}, {ends[1], ends[0]}} {
		p[0].Write([]byte{1})
		if _, err := io.ReadFull(p[1], make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
	}
	out := h.ok(human, "peer", "relay-update", "mini")
	if !strings.Contains(out, "updates to the latest release if it is newer") || !strings.Contains(out, "runs tm 0.27.0 now") {
		t.Fatalf("out: %s", out)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	h.expect(1, "relay-update-too-soon", human, "peer", "relay-update", "mini")
}
