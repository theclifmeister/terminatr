package server

import (
	"crypto/ed25519"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// TestServerStatusShowsLinks pairs the server with another (a bare link
// manager) through an in-process relay and follows the link in
// server.status: none before a key, waiting for the peer, then up.
func TestServerStatusShowsLinks(t *testing.T) {
	p := testPaths(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")

	dir := peer.DirIn(p.Home)
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	otherDir := filepath.Join(t.TempDir(), "peer")
	other, _, err := peer.EnsureIdentity(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	pubOf := func(i *peer.Identity) ed25519.PublicKey { return i.Key.Public().(ed25519.PublicKey) }
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "mini", Key: pubOf(other), Relay: url}}); err != nil {
		t.Fatal(err)
	}
	if err := peer.SavePeers(otherDir, []peer.Peer{{Alias: "studio", Key: pubOf(id), Relay: url}}); err != nil {
		t.Fatal(err)
	}

	startServer(t, p)
	c, err := Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	link := func(what string, ok func(proto.PeerLink) bool) proto.ServerStatus {
		t.Helper()
		var st proto.ServerStatus
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			call(t, c, proto.MethodServerStatus, nil, &st)
			if st.Link != nil && len(st.Link.Peers) == 1 && ok(st.Link.Peers[0]) {
				return st
			}
		}
		t.Fatalf("link never %s: %+v", what, st.Link)
		return st
	}

	st := link("waiting for the peer", func(l proto.PeerLink) bool {
		return l.State == proto.LinkOffline && l.Reason == proto.LinkReasonPeer
	})
	if st.Link.ID != id.ID.String() || st.Link.Short != id.ID.Short() || st.Link.Peers[0].Alias != "mini" || st.Link.Peers[0].ID != other.ID.String() {
		t.Fatalf("link status %+v", st.Link)
	}

	m := peer.Start(peer.Options{Dir: otherDir, Version: "9.9.9"})
	defer m.Close()
	link("up", func(l proto.PeerLink) bool {
		return l.State == proto.LinkUp && l.Version == "9.9.9" && l.LinkProtocol == peer.LinkProtocol
	})
}

// TestServerStatusWithoutKey: a server never paired shows no link.
func TestServerStatusWithoutKey(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	c, err := Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var st proto.ServerStatus
	call(t, c, proto.MethodServerStatus, nil, &st)
	if st.Link != nil {
		t.Fatalf("an unpaired server's link: %+v", st.Link)
	}
}
