package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
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

	// The pane switch (docs/SPEC.md §16.12).
	h.expect(2, "off|view|type", human, "peer", "panes", "mini", "write")
	h.expect(2, "usage: tm peer pair", human, "peer", "panes", "mini")
	h.expect(1, "unknown-peer", human, "peer", "panes", "studio", "view")
	if out := h.ok(human, "peer", "panes", "mini", "type"); !strings.Contains(out, "Panes type for mini") || *reloads != 1 {
		t.Fatalf("panes: %q, %d reloads", out, *reloads)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "panes: type") {
		t.Fatalf("list with panes: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Alias != "mini" || ps[0].Panes != proto.PanesType {
		t.Fatalf("after panes: %+v", ps)
	}
	h.ok(human, "peer", "panes", "mini", "off")
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
