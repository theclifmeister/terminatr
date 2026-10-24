package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// stubPeerMirror stands in for the server's peer.list (nil: no server).
func stubPeerMirror(t *testing.T, l *proto.PeerList) {
	t.Helper()
	old := peerMirror
	t.Cleanup(func() { peerMirror = old })
	peerMirror = func() (*proto.PeerList, bool) { return l, l != nil }
}

// TestNetwork: tm network is the tree of this server's projects and
// links, the paired servers with what they share, and what needs the
// user, as text and --json; an agent is refused.
func TestNetwork(t *testing.T) {
	h := newHarness(t)
	stubPeerServer(t, nil)
	stubPeerMirror(t, nil)
	h.expect(1, "human-only", coord, "network")
	h.expect(1, "human-only", thr, "network")
	h.expect(2, "usage: tm network", human, "network", "extra")
	if out := h.ok(human, "network"); !strings.Contains(out, "shares 0 of 0 projects") || !strings.Contains(out, "no paired server") || !strings.Contains(out, "nothing needs you") {
		t.Fatalf("empty network %q", out)
	}

	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "web")
	h.ok(human, "project", "new", "blog")
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
	mini := peer.Peer{Alias: "mini", Key: k1, Relay: "wss://r.example.net", Panes: proto.PanesView}
	nas := peer.Peer{Alias: "nas", Key: k2, Relay: "wss://r.example.net"}
	if err := peer.SavePeers(dir, []peer.Peer{mini, nas}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"demo", "web"} {
		if err := config.SetProject(slug, "share", true); err != nil {
			t.Fatal(err)
		}
	}
	h.ok(human, "project", "link", "demo", "web", "--ask")
	h.ok(human, "project", "link", "demo", "mini/api", "--request")
	// mini asks web for a link, and asks this server to share a project.
	fp := mini.ID().Short()[:4]
	if res := link.Receive("mini", mini.ID(), proto.PeerMessage{Epoch: "e", Seq: 1, Kind: proto.PeerLinkRequest, From: "acme", To: "web",
		ID: "R-" + fp + "-1", By: "human", Allow: "send"}); res.Status != proto.PeerDelivered {
		t.Fatalf("link request %+v", res)
	}
	if res := link.Receive("mini", mini.ID(), proto.PeerMessage{Epoch: "e", Seq: 2, Kind: proto.PeerShareRequest, ID: "S-" + fp + "-2", For: "infra",
		Text: "the deploy scripts", By: "human"}); res.Status != proto.PeerDelivered {
		t.Fatalf("share request %+v", res)
	}
	// A held message from demo to web.
	h.ok(coord, "link", "send", "web", "hello", "--project", "demo")

	now := time.Now()
	stubPeerServer(t, &proto.LinkStatus{ID: id.ID.String(), Peers: []proto.PeerLink{
		{Alias: "mini", ID: mini.ID().String(), State: proto.LinkUp, Since: now.Add(-time.Minute), RTT: 38, Version: "0.50.0"},
		{Alias: "nas", ID: nas.ID().String(), State: proto.LinkOffline, Since: now.Add(-4 * time.Minute), Reason: proto.LinkReasonPeer},
	}})
	stubPeerMirror(t, &proto.PeerList{Peers: []proto.PeerMirror{
		{Alias: "mini", ID: mini.ID().String(), State: proto.LinkUp, Pane: true, Panes: proto.PanesType, Listed: true, Version: "0.50.0",
			Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "api", Threads: 2},
				Sessions: []proto.PeerSession{{Role: proto.RoleCoordinator, Project: "api", State: "working"}}}}},
		{Alias: "nas", ID: nas.ID().String(), State: proto.LinkOffline},
	}})

	out := h.ok(human, "network")
	for _, want := range []string{
		"· this server · id " + id.ID.Short() + " · shares 2 of 3 projects with every paired server",
		"  blog  not shared",
		"  demo  shared",
		"    ⇄ mini/api  here both ask · files ask · there asked, not sent yet · waiting on mini's user",
		"    ⇄ web       here both ask · files ask · there both · files ask · linked both ways",
		"      ⚑ M-2 waits for your OK: tm link approve|drop M-2 --project demo",
		"    ⇄ demo  here both · files ask · there both ask · files ask · linked both ways",
		"    ⚑ mini/acme asks to link (send there, R-" + fp + "-1): tm link accept|decline R-" + fp + "-1 --project web",
		"mini · " + mini.ID().Short() + " · up 1m0s · 38 ms · tm 0.50.0 · panes: you give it view, it gives you write",
		"  api  shared with you · coordinator working · linked from demo",
		"  ⚑ mini asks you to share a project with it, for its infra: \"the deploy scripts\"; answer in Network (N) · tm peer decline S-" + fp + "-2 declines it",
		"nas · " + nas.ID().Short() + " · offline 4m0s · peer · panes: you give it off",
		"  shares nothing with you yet: tm peer ask-share nas asks its user to share one",
		"NEEDS YOU 3 · 1 message waits for your OK · 1 link request · 1 share request",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("network lacks %q:\n%s", want, out)
		}
	}

	var js networkTree
	if err := json.Unmarshal([]byte(h.ok(human, "network", "--json")), &js); err != nil {
		t.Fatal(err)
	}
	if !js.Running || js.Server.ID != id.ID.String() || len(js.Server.Projects) != 3 || len(js.Peers) != 2 || js.Needs.Total != 3 || js.Needs.Held != 1 {
		t.Fatalf("--json %+v", js)
	}
	demo := js.Server.Projects[1]
	if demo.Slug != "demo" || !demo.Shared || len(demo.Links) != 2 || demo.Links[1].Peer != "web" || len(demo.Links[1].Held) != 1 || demo.Links[1].Theirs == nil {
		t.Fatalf("--json demo %+v", demo)
	}
	if p := js.Peers[0]; p.Alias != "mini" || p.Panes != "view" || p.TheirPanes != "write" || len(p.Projects) != 1 || p.Projects[0].Linked[0] != "demo" || len(p.Shares) != 1 {
		t.Fatalf("--json mini %+v", p)
	}
}
