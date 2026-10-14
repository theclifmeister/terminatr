package doctor

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestPeers(t *testing.T) {
	d := testDeps(t)
	if cs := Peers(d, Live{}); cs != nil {
		t.Fatalf("never paired: %+v", cs)
	}
	dir := peer.DirIn(d.Paths.Home)
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cs := Peers(d, Live{}); len(cs) != 1 || cs[0].Status != OK || !strings.Contains(cs[0].Detail, "no peers") {
		t.Fatalf("a key, no peers: %+v", cs)
	}
	k1, _, _ := ed25519.GenerateKey(nil)
	k2, _, _ := ed25519.GenerateKey(nil)
	ps := []peer.Peer{{Alias: "mini", Key: k1, Relay: "wss://r.example.net"}, {Alias: "nas", Key: k2, Relay: "wss://r.example.net"}}
	if err := peer.SavePeers(dir, ps); err != nil {
		t.Fatal(err)
	}
	if cs := Peers(d, Live{}); len(cs) != 1 || cs[0].Status != OK || !strings.Contains(cs[0].Detail, "2 paired; the server links them when it runs") {
		t.Fatalf("server stopped: %+v", cs)
	}
	live := Live{Running: true, Link: &proto.LinkStatus{ID: id.ID.String(), Peers: []proto.PeerLink{
		{Alias: "mini", ID: ps[0].ID().String(), State: proto.LinkUp},
		{Alias: "nas", ID: ps[1].ID().String(), State: proto.LinkOffline, Reason: proto.LinkReasonPeer},
	}}}
	if cs := Peers(d, live); len(cs) != 1 || cs[0].Status != OK || !strings.HasSuffix(cs[0].Detail, ": mini up, nas offline (peer)") {
		t.Fatalf("up and asleep: %+v", cs)
	}
	live.Link.Peers[1].Reason, live.Link.Peers[1].Error = proto.LinkReasonRefused, "the other end's key is not the paired key"
	if cs := Peers(d, live); len(cs) != 1 || cs[0].Status != Warn || !strings.Contains(cs[0].Detail, "nas offline (refused): the other end's key") {
		t.Fatalf("refused: %+v", cs)
	}
	os.WriteFile(filepath.Join(dir, peer.PeersFile), []byte("{"), 0o600)
	if cs := Peers(d, live); len(cs) != 1 || cs[0].Status != Fail {
		t.Fatalf("broken peers.json: %+v", cs)
	}
}
