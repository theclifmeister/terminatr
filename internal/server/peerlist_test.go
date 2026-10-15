package server

import (
	"crypto/ed25519"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// oneProject exports one shared project, "web", with one thread.
type oneProject struct{}

func (oneProject) Projects(caller.Caller) ([]proto.PeerProject, error) {
	return []proto.PeerProject{{Project: "web", Goal: "the web app", Threads: 1}}, nil
}

func (oneProject) Watch(_ caller.Caller, slug string) (proto.PeerWatch, error) {
	if slug != "web" {
		return proto.PeerWatch{}, proto.Errorf(proto.ErrNotShared, "no shared project %q", slug)
	}
	return proto.PeerWatch{Watch: proto.ProjectWatch{Project: "web", Threads: []proto.WatchThread{{ID: "t-0001"}}},
		Sessions: []proto.PeerSession{{ID: "s-9", Role: "thread", Project: "web", Thread: "t-0001"}}}, nil
}

func (oneProject) Changes() (<-chan struct{}, func()) { return nil, func() {} }

// TestPeerList pairs the server with another that shares a project, and
// reads the server's mirror of it with peer.list: the project with its
// snapshot while the link is up, the same snapshot stale once the other
// stops.
func TestPeerList(t *testing.T) {
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
	m := peer.Start(peer.Options{Dir: otherDir, Version: "9.9.9", Export: oneProject{},
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	defer m.Close()

	startServer(t, p)
	c, err := client.Connect(p, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	list := func(what string, ok func(proto.PeerMirror) bool) proto.PeerMirror {
		t.Helper()
		var l proto.PeerList
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			// A fresh value each time: decoding into the last one keeps
			// what this answer leaves out (stale is omitempty).
			l = proto.PeerList{}
			call(t, c, proto.MethodPeerList, nil, &l)
			if len(l.Peers) == 1 && ok(l.Peers[0]) {
				return l.Peers[0]
			}
		}
		t.Fatalf("peer.list never %s: %+v", what, l)
		return proto.PeerMirror{}
	}
	pm := list("synced web", func(pm proto.PeerMirror) bool {
		return pm.State == proto.LinkUp && len(pm.Projects) == 1 && !pm.Projects[0].Stale && pm.Projects[0].Watch != nil
	})
	w := pm.Projects[0]
	if pm.Alias != "mini" || pm.ID != other.ID.String() || !pm.Watch || w.Project != "web" || w.Goal != "the web app" ||
		w.Threads != 1 || len(w.Sessions) != 1 || w.Sessions[0].ID != "s-9" || w.Synced.IsZero() {
		t.Fatalf("peer.list %+v", pm)
	}

	m.Close()
	pm = list("marked web stale", func(pm proto.PeerMirror) bool {
		return pm.State != proto.LinkUp && len(pm.Projects) == 1 && pm.Projects[0].Stale
	})
	if w := pm.Projects[0]; w.Watch == nil || w.Threads != 1 || w.Synced.IsZero() {
		t.Fatalf("a stale project lost its snapshot: %+v", w)
	}
}
