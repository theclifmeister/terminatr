package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// tm peer name: the user's, by alias or name; a name another peer, this
// server or a project here has is refused; tm peer list shows both, and
// tm project link takes either word.
func TestPeerName(t *testing.T) {
	h := newHarness(t)
	dir, err := peer.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := peer.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	k1, _, _ := ed25519.GenerateKey(nil)
	k2, _, _ := ed25519.GenerateKey(nil)
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "del-02-0468-lt", Key: k1, Relay: "wss://r.example.net"}, {Alias: "nas", Key: k2, Relay: "wss://r.example.net"}}); err != nil {
		t.Fatal(err)
	}
	reloads := stubPeerServer(t, &proto.LinkStatus{})
	old := pairName
	pairName = func() string { return "studio" }
	t.Cleanup(func() { pairName = old })
	h.ok(human, "project", "new", "acme")

	h.expect(1, "human-only", coord, "peer", "name", "nas", "box")
	h.expect(1, "human-only", thr, "peer", "name", "nas", "box")
	h.expect(2, "usage: tm peer pair", human, "peer", "name", "nas")
	h.expect(2, "name \"Bad\"", human, "peer", "name", "nas", "Bad")
	h.expect(1, "unknown-peer", human, "peer", "name", "studio", "box")
	h.expect(1, "name-taken", human, "peer", "name", "nas", "del-02-0468-lt")
	h.expect(1, "name-taken", human, "peer", "name", "nas", "studio")
	h.expect(1, "name-taken", human, "peer", "name", "nas", "acme")

	if out := h.ok(human, "peer", "name", "del-02-0468-lt", "work"); !strings.Contains(out, "del-02-0468-lt is now called work here") || *reloads != 1 {
		t.Fatalf("name: %q, %d reloads", out, *reloads)
	}
	h.expect(1, "name-taken", human, "peer", "name", "nas", "work")
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "work (del-02-0468-lt)") {
		t.Fatalf("list: %q", out)
	}
	var js struct{ Peers []peerEntry }
	if err := json.Unmarshal([]byte(h.ok(human, "peer", "list", "--json")), &js); err != nil || js.Peers[0].Name != "work" || js.Peers[0].Alias != "del-02-0468-lt" {
		t.Fatalf("list --json: %+v, %v", js, err)
	}

	// Either word links a project there; config keeps the alias.
	h.ok(human, "project", "link", "acme", "work/gatehouse", "--no-ask")
	out := h.ok(human, "link", "list", "--project", "acme")
	if !strings.Contains(out, "work/gatehouse") {
		t.Fatalf("link list: %q", out)
	}
	h.ok(human, "project", "link", "acme", "del-02-0468-lt/gatehouse", "off")
	if out := h.ok(human, "link", "list", "--project", "acme"); strings.Contains(out, "gatehouse") {
		t.Fatalf("after off by the alias: %q", out)
	}

	// Renamed by its name, then cleared: the alias shows again.
	h.ok(human, "peer", "name", "work", "office")
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "office (del-02-0468-lt)") {
		t.Fatalf("list after rename: %q", out)
	}
	if out := h.ok(human, "peer", "name", "office", ""); !strings.Contains(out, "shows by its alias again") {
		t.Fatalf("clear: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Name != "" {
		t.Fatalf("after clear: %+v", ps)
	}
}
