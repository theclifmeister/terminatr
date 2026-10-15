package cli

import (
	"crypto/ed25519"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestLinkRequestCLI: tm project link --request sets this side and asks
// the paired server's project; tm link accept sets this side's entry to
// a request's mirror (asking first) and decline sets nothing; both are
// the user's, and --request on a project here is a usage error.
func TestLinkRequestCLI(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "web")
	dir, err := peer.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := peer.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	k, _, _ := ed25519.GenerateKey(nil)
	mini := peer.Peer{Alias: "mini", Key: k, Relay: "wss://r.example.net"}
	if err := peer.SavePeers(dir, []peer.Peer{mini}); err != nil {
		t.Fatal(err)
	}

	out := h.ok(human, "project", "link", "demo", "mini/api", "send", "--request")
	if !strings.Contains(out, "asked mini/api to link back") {
		t.Fatalf("out %q", out)
	}
	cfg, _ := config.Load()
	if l, _ := cfg.Links("demo"); l["mini/api"] != (config.Link{Allow: config.LinkSend, Ask: true}) {
		t.Fatalf("demo's entry %+v", l)
	}
	h.expect(1, "request-pending", human, "project", "link", "demo", "mini/api", "send", "--request")
	h.expect(2, "--request asks a paired server's project", human, "project", "link", "demo", "web", "--request")

	// Two requests arrive from mini for web.
	fp := mini.ID().Short()[:4]
	for i, from := range []string{"acme", "docs"} {
		res := link.Receive("mini", mini.ID(), proto.PeerMessage{Epoch: "e", Seq: i + 1, Kind: proto.PeerLinkRequest, From: from, To: "web",
			ID: "R-" + fp + "-" + string(rune('1'+i)), By: "human", Allow: "send"})
		if res.Code != "not-shared" {
			t.Fatalf("web isn't shared: %+v", res)
		}
	}
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}
	for i, from := range []string{"acme", "docs"} {
		res := link.Receive("mini", mini.ID(), proto.PeerMessage{Epoch: "e", Seq: i + 3, Kind: proto.PeerLinkRequest, From: from, To: "web",
			ID: "R-" + fp + "-" + string(rune('3'+i)), By: "human", Allow: "send"})
		if res.Status != proto.PeerDelivered {
			t.Fatalf("delivered: %+v", res)
		}
	}
	if out := h.ok(human, "link", "list", "--project", "web"); !strings.Contains(out, "asks to link (R-"+fp+"-3)") {
		t.Fatalf("list %q", out)
	}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "web"}
	h.expect(1, "human-only", coord, "link", "accept", "R-"+fp+"-3", "--project", "web")
	if out := h.ok(human, "link", "accept", "R-"+fp+"-3", "--project", "web"); !strings.Contains(out, "web → mini/acme receive ask on this side") {
		t.Fatalf("accept %q", out)
	}
	if out := h.ok(human, "link", "decline", "R-"+fp+"-4", "--project", "web"); !strings.Contains(out, "declined R-"+fp+"-4") {
		t.Fatalf("decline %q", out)
	}
	cfg, _ = config.Load()
	l, _ := cfg.Links("web")
	if l["mini/acme"] != (config.Link{Allow: config.LinkReceive, Ask: true}) {
		t.Fatalf("web's entries %+v", l)
	}
	if _, ok := l["mini/docs"]; ok {
		t.Fatal("declining wrote an entry")
	}
	p, _ := project.Open("web")
	items, _ := p.Inbox()
	for _, it := range items {
		if it.Kind == link.KindRequest {
			t.Fatalf("an answered request's item stays: %+v", it)
		}
	}
}
