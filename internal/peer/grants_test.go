package peer

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home/hometest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func grantKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

// grantsAt is a ledger over a paired mini, kept before the file changes.
func grantsAt(t *testing.T) (*grants, string) {
	t.Helper()
	dir := hometest.PrivateDir(t)
	if _, err := Pin(dir, grantKey(t), "wss://r.example.net", "mini", "mini"); err != nil {
		t.Fatal(err)
	}
	g := loadGrants(dir, nil)
	if _, _, err := g.read(); err != nil {
		t.Fatal(err)
	}
	return g, dir
}

func mustRead(t *testing.T, g *grants) ([]Peer, []proto.PeerRefusal) {
	t.Helper()
	peers, refused, err := g.read()
	if err != nil {
		t.Fatal(err)
	}
	return peers, refused
}

func TestGrantsFileRaisesNothing(t *testing.T) {
	g, dir := grantsAt(t)
	for _, set := range []func() (bool, error){
		func() (bool, error) { return SetPanes(dir, "mini", proto.PanesType) },
		func() (bool, error) { return SetNeeds(dir, "mini", proto.NeedsAnswer) },
		func() (bool, error) { return SetPages(dir, "mini", "acme", true) },
		func() (bool, error) { return SetFiles(dir, "mini", "acme", true) },
	} {
		if _, err := set(); err != nil {
			t.Fatal(err)
		}
	}
	peers, refused := mustRead(t, g)
	if len(peers) != 1 {
		t.Fatalf("peers %+v", peers)
	}
	p := peers[0]
	if p.PanesLevel() != proto.PanesOff || p.NeedsLevel() != proto.NeedsOff || p.PagesOn("acme") || p.FilesOn("acme") {
		t.Fatalf("a raise in the file counted: %+v", p)
	}
	if len(refused) != 1 || strings.Join(refused[0].Words, ",") != "panes write,needs answer,pages acme,files acme" {
		t.Fatalf("refused %+v", refused)
	}
}

func TestGrantsHumanRaiseHoldsAndCarriesNothingElse(t *testing.T) {
	g, dir := grantsAt(t)
	if _, err := SetFiles(dir, "mini", "other", true); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPanes(dir, "mini", proto.PanesType); err != nil {
		t.Fatal(err)
	}
	if _, err := g.set(proto.PeerSwitchParams{Alias: "mini", Switch: proto.PeerSwitchFiles, Project: "acme", Value: "on"}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.set(proto.PeerSwitchParams{Alias: "mini", Switch: proto.PeerSwitchPanes, Value: "view"}); err != nil {
		t.Fatal(err)
	}
	peers, refused := mustRead(t, g)
	p := peers[0]
	if !p.FilesOn("acme") || p.FilesOn("other") || p.PanesLevel() != proto.PanesView {
		t.Fatalf("%+v", p)
	}
	if len(refused) != 1 || !slices.Equal(refused[0].Words, []string{"files other"}) {
		t.Fatalf("refused %+v", refused)
	}
}

func TestGrantsLowerHoldsAndStaysLower(t *testing.T) {
	g, dir := grantsAt(t)
	if _, err := g.set(proto.PeerSwitchParams{Alias: "mini", Switch: proto.PeerSwitchPanes, Value: "write"}); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPanes(dir, "mini", proto.PanesOff); err != nil {
		t.Fatal(err)
	}
	if peers, _ := mustRead(t, g); peers[0].PanesLevel() != proto.PanesOff {
		t.Fatalf("off didn't hold: %+v", peers[0])
	}
	if _, err := SetPanes(dir, "mini", proto.PanesType); err != nil {
		t.Fatal(err)
	}
	if peers, refused := mustRead(t, g); peers[0].PanesLevel() != proto.PanesOff || len(refused) != 1 {
		t.Fatalf("a lowered grant came back: %+v %+v", peers[0], refused)
	}
}

func TestGrantsUnknownPairingIsNotServed(t *testing.T) {
	g, dir := grantsAt(t)
	if _, err := Pin(dir, grantKey(t), "wss://r.example.net", "evil", "evil"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPanes(dir, "evil", proto.PanesType); err != nil {
		t.Fatal(err)
	}
	peers, refused := mustRead(t, g)
	if len(peers) != 1 || peers[0].Alias != "mini" {
		t.Fatalf("peers %+v", peers)
	}
	if len(refused) != 1 || refused[0].Alias != "evil" || refused[0].Words[0] != proto.PeerRefusalPairing || refused[0].Panes != proto.PanesOff {
		t.Fatalf("refused %+v", refused)
	}
	// A switch doesn't bring it in by the side.
	if _, err := g.set(proto.PeerSwitchParams{Alias: "evil", Switch: proto.PeerSwitchNeeds, Value: "view"}); err == nil {
		t.Fatal("a switch on a pairing tm didn't keep")
	}
	// The human's pairing keeps it, its switches at their defaults.
	if _, err := g.set(proto.PeerSwitchParams{Alias: "evil", Switch: proto.PeerSwitchPin}); err != nil {
		t.Fatal(err)
	}
	peers, refused = mustRead(t, g)
	if len(peers) != 2 || len(refused) != 1 || !slices.Equal(refused[0].Words, []string{"panes write"}) {
		t.Fatalf("after pin: %+v %+v", peers, refused)
	}
	// tm peer allow keeps all the file says.
	res, err := g.set(proto.PeerSwitchParams{Alias: "evil", Switch: proto.PeerSwitchAllow})
	if err != nil || !slices.Equal(res.Allowed, []string{"panes write"}) {
		t.Fatalf("allow: %+v %v", res, err)
	}
	if _, refused = mustRead(t, g); len(refused) != 0 {
		t.Fatalf("after allow: %+v", refused)
	}
}

func TestGrantsFileReadOnlyAtStart(t *testing.T) {
	g, dir := grantsAt(t)
	if _, err := SetPanes(dir, "mini", proto.PanesType); err != nil {
		t.Fatal(err)
	}
	// Writing the ledger too changes nothing for a server that runs.
	forged := `{"grants":{"` + mustPeer(t, dir).ID().String() + `":{"panes":"type","conversations":"on","needs":"off"}}}`
	if err := os.WriteFile(filepath.Join(dir, GrantsFile), []byte(forged), 0o600); err != nil {
		t.Fatal(err)
	}
	if peers, _ := mustRead(t, g); peers[0].PanesLevel() != proto.PanesOff {
		t.Fatalf("a running server read the file: %+v", peers[0])
	}
}

func mustPeer(t *testing.T, dir string) Peer {
	t.Helper()
	peers, err := LoadPeers(dir)
	if err != nil || len(peers) == 0 {
		t.Fatalf("%v %v", peers, err)
	}
	return peers[0]
}

func TestGrantsAcrossRestartAndBrokenFile(t *testing.T) {
	g, dir := grantsAt(t)
	if _, err := g.set(proto.PeerSwitchParams{Alias: "mini", Switch: proto.PeerSwitchPanes, Value: "view"}); err != nil {
		t.Fatal(err)
	}
	again := loadGrants(dir, nil)
	if peers, refused := mustRead(t, again); peers[0].PanesLevel() != proto.PanesView || len(refused) != 0 {
		t.Fatalf("restart: %+v %+v", peers, refused)
	}
	if err := os.WriteFile(filepath.Join(dir, GrantsFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if peers, refused := mustRead(t, loadGrants(dir, nil)); len(peers) != 0 || len(refused) != 1 {
		t.Fatalf("a broken file must refuse: %+v %+v", peers, refused)
	}
}
