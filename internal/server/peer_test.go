package server

import (
	"crypto/ed25519"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home/hometest"

	"github.com/theclifmeister/terminatr/internal/client"
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
	otherDir := hometest.PrivateDir(t)
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
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	link := func(what string, ok func(proto.PeerLink) bool) proto.ServerStatus {
		t.Helper()
		var st proto.ServerStatus
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			st = proto.ServerStatus{} // decoding into the last answer keeps what this one leaves out
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
	c, err := client.Connect(p, false)
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

// TestPeerReload: a key and peers written while the server runs (tm peer
// pair) show once peer.reload reads them; removing the peer drops it.
func TestPeerReload(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var st *proto.LinkStatus
	call(t, c, proto.MethodPeerReload, nil, &st)
	if st != nil {
		t.Fatalf("reload without a key: %+v", st)
	}
	dir := peer.DirIn(p.Home)
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	k, _, _ := ed25519.GenerateKey(nil)
	if _, err := peer.Pin(dir, k, "ws://127.0.0.1:1", "mini", ""); err != nil {
		t.Fatal(err)
	}
	// On the file's word alone it gets no link, and the status says so.
	call(t, c, proto.MethodPeerReload, nil, &st)
	if st == nil || len(st.Peers) != 0 || len(st.Refused) != 1 || st.Refused[0].Words[0] != proto.PeerRefusalPairing {
		t.Fatalf("a pairing in the file alone: %+v", st)
	}
	var res proto.PeerSwitchResult
	call(t, c, proto.MethodPeerSwitch, proto.PeerSwitchParams{Alias: "mini", Switch: proto.PeerSwitchPin}, &res)
	st = nil
	call(t, c, proto.MethodPeerReload, nil, &st)
	if st == nil || st.ID != id.ID.String() || len(st.Peers) != 1 || st.Peers[0].Alias != "mini" || len(st.Refused) != 0 {
		t.Fatalf("after pairing: %+v", st)
	}
	// A raise in the file doesn't count; the same raise from the human does.
	if ok, err := peer.SetPanes(dir, "mini", proto.PanesType); !ok || err != nil {
		t.Fatal(ok, err)
	}
	st = nil
	call(t, c, proto.MethodPeerReload, nil, &st)
	if st == nil || len(st.Refused) != 1 || st.Refused[0].Panes != proto.PanesOff || st.Refused[0].Words[0] != "panes write" {
		t.Fatalf("a raise in the file alone: %+v", st)
	}
	call(t, c, proto.MethodPeerSwitch, proto.PeerSwitchParams{Alias: "mini", Switch: proto.PeerSwitchPanes, Value: proto.PanesType}, &res)
	st = nil
	call(t, c, proto.MethodPeerReload, nil, &st)
	if st == nil || len(st.Refused) != 0 {
		t.Fatalf("after the human's raise: %+v", st)
	}
	var perr *proto.Error
	if err := c.Call(proto.MethodPeerSwitch, proto.PeerSwitchParams{Alias: "nobody", Switch: proto.PeerSwitchPanes, Value: proto.PanesView}, &res); !errors.As(err, &perr) || perr.Code != "unknown-peer" {
		t.Fatalf("a peer that isn't: %v", err)
	}
	if ok, err := peer.Unpin(dir, "mini"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	st = nil
	call(t, c, proto.MethodPeerReload, nil, &st)
	if st == nil || len(st.Peers) != 0 {
		t.Fatalf("after remove: %+v", st)
	}
}
