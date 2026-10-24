package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestPeerShareRequestsCLI: tm peer ask-share asks a paired server's
// user to share a project (once at a time, never to an older tm), tm
// peer requests lists what waits both ways, and tm peer decline answers
// one from there; all of it is the user's.
func TestPeerShareRequestsCLI(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	dir, err := peer.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := peer.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	k, _, _ := ed25519.GenerateKey(nil)
	k2, _, _ := ed25519.GenerateKey(nil)
	mini := peer.Peer{Alias: "mini", Key: k, Relay: "wss://r.example.net"}
	nas := peer.Peer{Alias: "nas", Key: k2, Relay: "wss://r.example.net"}
	if err := peer.SavePeers(dir, []peer.Peer{mini, nas}); err != nil {
		t.Fatal(err)
	}
	// nas's hello lists messages without share requests: an older tm.
	stubPeerServer(t, &proto.LinkStatus{Peers: []proto.PeerLink{{Alias: "nas", State: proto.LinkUp, Caps: []string{peer.CapMsg}},
		{Alias: "mini", State: proto.LinkUp, Caps: []string{peer.CapMsg, peer.CapShare}}}})

	coord := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	h.expect(1, "human-only", coord, "peer", "ask-share", "mini")
	h.expect(1, "human-only", coord, "peer", "requests")
	h.expect(1, "human-only", coord, "peer", "decline", "S-0000-1")
	h.expect(2, "usage: tm peer pair", human, "peer", "ask-share")
	h.expect(2, "takes no --note", human, "peer", "requests", "--note", "x")
	h.expect(1, "unknown-server", human, "peer", "ask-share", "lab")
	h.expect(1, "old-tm", human, "peer", "ask-share", "nas")
	h.expect(1, "unknown-project", human, "peer", "ask-share", "mini", "--for", "nosuch")

	out := h.ok(human, "peer", "ask-share", "mini", "--for", "demo", "--note", "the deploy scripts")
	if !strings.Contains(out, "asked mini to share a project (S-") || !strings.Contains(out, ", for demo); its user answers there; tm peer requests shows it") {
		t.Fatalf("ask-share %q", out)
	}
	h.expect(1, "request-pending", human, "peer", "ask-share", "mini")

	// One arrives from mini.
	fp := mini.ID().Short()[:4]
	res := link.Receive("mini", mini.ID(), proto.PeerMessage{Epoch: "e", Seq: 1, Kind: proto.PeerShareRequest, ID: "S-" + fp + "-7", By: "human", For: "infra"})
	if res.Status != proto.PeerDelivered {
		t.Fatalf("delivered: %+v", res)
	}
	out = h.ok(human, "peer", "requests")
	if !strings.Contains(out, "to mini") || !strings.Contains(out, "S-"+fp+"-7") || !strings.Contains(out, "mini asks you to share a project with it, for its infra") {
		t.Fatalf("requests %q", out)
	}
	var got struct {
		Requests []requestEntry `json:"requests"`
	}
	if err := json.Unmarshal([]byte(h.ok(human, "peer", "requests", "--json")), &got); err != nil || len(got.Requests) != 2 {
		t.Fatalf("requests --json: %+v %v", got, err)
	}
	if r := got.Requests[1]; r.Kind != "share" || r.Out || r.Peer != "mini" || r.For != "infra" || r.State != proto.ShareWaiting || r.Expires.Sub(r.Created) != link.RequestExpiry {
		t.Fatalf("the incoming one: %+v", r)
	}

	h.expect(2, "isn't a share request's id", human, "peer", "decline", "R-"+fp+"-7")
	h.expect(1, "unknown-request", human, "peer", "decline", "S-"+fp+"-8")
	if out := h.ok(human, "peer", "decline", "S-"+fp+"-7"); !strings.Contains(out, "declined S-"+fp+"-7 from mini; nothing was shared") {
		t.Fatalf("decline %q", out)
	}
	h.expect(1, "not-pending", human, "peer", "decline", "S-"+fp+"-7")
	if out := h.ok(human, "peer", "requests"); strings.Contains(out, "S-"+fp+"-7") {
		t.Fatalf("a declined request still listed: %q", out)
	}
}
