package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home/hometest"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
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
	stubPeerSwitch(t)
	peerLinks = func() (*proto.LinkStatus, bool) { return links, links != nil }
	return &n
}

// stubPeerSwitch has peer.switch write the file as the server would, with no
// server to start.
func stubPeerSwitch(t *testing.T) {
	t.Helper()
	old := peerSwitch
	t.Cleanup(func() { peerSwitch = old })
	peerSwitch = func(p proto.PeerSwitchParams) (proto.PeerSwitchResult, error) {
		dir, err := peer.Dir()
		if err != nil {
			return proto.PeerSwitchResult{}, err
		}
		if p.Switch == proto.PeerSwitchPin || p.Switch == proto.PeerSwitchAllow {
			return proto.PeerSwitchResult{Alias: p.Alias}, nil
		}
		found, err := peer.ApplySwitch(dir, p)
		if err == nil && !found {
			err = &proto.Error{Code: "unknown-peer", Message: "no peer"}
		}
		return proto.PeerSwitchResult{Alias: p.Alias}, err
	}
}

// Every tm peer and tm device command takes the caller the server sees, not
// only the one the environment says: a process with its session's variables
// unset is still the agent it is.
func TestPeerAndDeviceAreChecked(t *testing.T) {
	for _, cmd := range []string{"peer", "device"} {
		if !checked[cmd] {
			t.Errorf("tm %s isn't in checked", cmd)
		}
	}
	dir := hometest.PrivateDir(t)
	t.Setenv("TERMINATR_HOME", dir)
	raised := false
	old := peerSwitch
	t.Cleanup(func() { peerSwitch = old })
	peerSwitch = func(proto.PeerSwitchParams) (proto.PeerSwitchResult, error) {
		raised = true
		return proto.PeerSwitchResult{}, nil
	}
	var out, errb bytes.Buffer
	e := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Caller: caller.Caller{Kind: caller.Human},
		Getenv: func(string) string { return "" },
		Identify: func() (caller.Caller, bool) {
			return caller.Caller{Kind: caller.Thread, Project: "p", Thread: "t-1"}, true
		}}
	code, handled := e.Run([]string{"peer", "panes", "mini", "write"})
	if !handled || code == ExitOK || raised || !strings.Contains(errb.String(), "human-only") {
		t.Fatalf("code %d raised %v: %s", code, raised, errb.String())
	}
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
	for _, c := range [][]string{{"pair", "--relay", "r.example.net"}, {"join", "r.example.net/7Q4M-K2XD-9HVA-PT3C"}, {"list"}, {"allow", "mini"}, {"remove", "mini"}, {"rotate", "--yes"}, {"panes", "mini", "type"}} {
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

	// The pane switch (docs/SPEC.md §16.12): write, or type, its older
	// name; peers.json keeps "type", which tm v0.27.0 reads too.
	h.expect(2, "off|view|write", human, "peer", "panes", "mini", "talk")
	h.expect(2, "usage: tm peer pair", human, "peer", "panes", "mini")
	h.expect(1, "unknown-peer", human, "peer", "panes", "studio", "view")
	if out := h.ok(human, "peer", "panes", "mini", "write"); !strings.Contains(out, "Panes write for mini") || *reloads != 1 {
		t.Fatalf("panes: %q, %d reloads", out, *reloads)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "panes: write") {
		t.Fatalf("list with panes: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Alias != "mini" || ps[0].Panes != proto.PanesType {
		t.Fatalf("after panes: %+v", ps)
	}
	if out := h.ok(human, "peer", "list", "--json"); !strings.Contains(out, `"panes": "write"`) && !strings.Contains(out, `"panes":"write"`) {
		t.Fatalf("list --json with panes: %q", out)
	}
	h.ok(human, "peer", "panes", "mini", "view")
	if out := h.ok(human, "peer", "panes", "mini", "type"); !strings.Contains(out, "Panes write for mini") {
		t.Fatalf("panes type: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Panes != proto.PanesType {
		t.Fatalf("after panes type: %+v", ps)
	}
	// The conversations switch (§16.14): on by default, off kept in
	// peers.json, on again with no key.
	h.expect(2, "on|off", human, "peer", "conversations", "mini", "maybe")
	h.expect(1, "unknown-peer", human, "peer", "conversations", "studio", "off")
	if out := h.ok(human, "peer", "conversations", "mini", "off"); !strings.Contains(out, "Conversations off for mini") {
		t.Fatalf("conversations off: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Conversations != proto.ConversationsNot || ps[0].ConversationsOn() || ps[0].Panes != proto.PanesType {
		t.Fatalf("after conversations off: %+v", ps)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "panes: write, conversations off") {
		t.Fatalf("list with conversations off: %q", out)
	}
	if out := h.ok(human, "peer", "conversations", "mini", "on"); !strings.Contains(out, "it can read the conversations") || !strings.Contains(out, "at panes write") {
		t.Fatalf("conversations on: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Conversations != "" {
		t.Fatalf("after conversations on: %+v", ps)
	}
	h.ok(human, "peer", "panes", "mini", "off")
	if out := h.ok(human, "peer", "conversations", "mini", "on"); !strings.Contains(out, "its panes are off") {
		t.Fatalf("conversations on, panes off: %q", out)
	}
	h.expect(1, "human-only", coord, "peer", "conversations", "mini", "off")
	// The needs switch (§16.16): off by default, view and answer kept
	// in peers.json, the human's alone.
	h.expect(2, "off|view|answer", human, "peer", "needs", "mini", "write")
	h.expect(1, "unknown-peer", human, "peer", "needs", "studio", "view")
	if out := h.ok(human, "peer", "needs", "mini", "view"); !strings.Contains(out, "Needs view for mini") {
		t.Fatalf("needs view: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Needs != proto.NeedsView {
		t.Fatalf("after needs view: %+v", ps)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "needs: view") {
		t.Fatalf("list with needs: %q", out)
	}
	h.ok(human, "peer", "needs", "mini", "off")
	if ps, _ := peer.LoadPeers(dir); ps[0].Needs != "" {
		t.Fatalf("after needs off: %+v", ps)
	}
	h.expect(1, "human-only", coord, "peer", "needs", "mini", "view")
	h.expect(1, "human-only", thr, "peer", "needs", "mini", "view")
	// The project pages switch (§16.17): off for every project by
	// default, on per project in peers.json, the human's alone.
	h.expect(2, "on|off", human, "peer", "pages", "mini", "web", "view")
	h.expect(2, "isn't a project", human, "peer", "pages", "mini", "../etc", "on")
	h.expect(2, "usage: tm peer pair", human, "peer", "pages", "mini", "on")
	h.expect(1, "unknown-peer", human, "peer", "pages", "studio", "web", "on")
	if out := h.ok(human, "peer", "pages", "mini", "web", "on"); !strings.Contains(out, "Pages on for mini on web") || !strings.Contains(out, "isn't shared") {
		t.Fatalf("pages on: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); !slices.Equal(ps[0].Pages, []string{"web"}) {
		t.Fatalf("after pages on: %+v", ps)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "pages: web") {
		t.Fatalf("list with pages: %q", out)
	}
	if out := h.ok(human, "peer", "pages", "mini", "web", "off"); !strings.Contains(out, "Pages off for mini on web") {
		t.Fatalf("pages off: %q", out)
	}
	if ps, _ := peer.LoadPeers(dir); len(ps[0].Pages) != 0 {
		t.Fatalf("after pages off: %+v", ps)
	}
	h.expect(1, "human-only", coord, "peer", "pages", "mini", "web", "on")
	h.expect(1, "human-only", thr, "peer", "pages", "mini", "web", "on")
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

// tm peer relay gives a paired server another relay: a pinned one too.
func TestPeerRelay(t *testing.T) {
	h := newHarness(t)
	reloads := stubPeerServer(t, nil)
	dir, _ := peer.Dir()
	if _, _, err := peer.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	k1, _, _ := ed25519.GenerateKey(nil)
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "mini", Key: k1, Relay: "wss://r.example.net"}}); err != nil {
		t.Fatal(err)
	}
	pin := strings.Repeat("ab", 32)
	h.expect(2, "usage: tm peer pair", human, "peer", "relay", "mini")
	h.expect(2, "64 hex digits", human, "peer", "relay", "mini", "203.0.113.5#sha256=12")
	h.expect(1, "unknown-peer", human, "peer", "relay", "nas", "203.0.113.5#sha256="+pin)
	h.expect(1, "human-only", coord, "peer", "relay", "mini", "203.0.113.5#sha256="+pin)
	out := h.ok(human, "peer", "relay", "mini", "192.168.1.20#SHA256="+strings.ToUpper(pin))
	if !strings.Contains(out, "through 192.168.1.20 (pinned)") || !strings.Contains(out, "tm peer relay ALIAS wss://192.168.1.20#sha256="+pin) ||
		!strings.Contains(out, "192.168.1.20 is a private address") || *reloads != 1 {
		t.Fatalf("relay: %q, %d reloads", out, *reloads)
	}
	if ps, _ := peer.LoadPeers(dir); ps[0].Relay != "wss://192.168.1.20#sha256="+pin || !ps[0].Key.Equal(k1) {
		t.Fatalf("after: %+v", ps)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "192.168.1.20 (pinned)") {
		t.Fatalf("list: %q", out)
	}
}

// Joining through a self-signed relay on 127.0.0.1: its pin is taken
// from the code, and a relay with another key is refused in so many
// words.
func TestPeerJoinPinned(t *testing.T) {
	h := newHarness(t)
	stubPeerServer(t, nil)
	state := t.TempDir()
	cert, key, pin, err := relay.SelfSigned(state, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := relay.New(relay.DefaultConfig())
	hs := httptest.NewUnstartedServer(s)
	c, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	hs.TLS = &tls.Config{Certificates: []tls.Certificate{c}}
	hs.StartTLS()
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	host := strings.TrimPrefix(hs.URL, "https://")
	code, out, errs := h.run(human, "", "peer", "join", host+"#sha256="+pin+"/7Q4M-K2XD-9HVA-PT3C")
	if code != 1 || !strings.Contains(errs, "nobody is pairing with this code") || strings.Contains(out, "private address") {
		t.Fatalf("join: %d %q %q", code, out, errs)
	}
	h.expect(3, "the relay at "+host+" has a different key than when you paired", human, "peer", "join", host+"#sha256="+strings.Repeat("ab", 32)+"/7Q4M-K2XD-9HVA-PT3C")
}

// tm peer relay-update asks the paired server's relay to update itself,
// with this server's key, while its link through the relay is live.
func TestPeerRelayUpdate(t *testing.T) {
	h := newHarness(t)
	stubPeerServer(t, nil)
	file := filepath.Join(t.TempDir(), "update-request")
	cfg := relay.DefaultConfig()
	cfg.Version, cfg.UpdateFile = "v0.27.0", file
	cfg.UpdateAfter = 0 // the link may ask once it has carried bytes
	s := relay.New(cfg)
	hs := httptest.NewServer(s)
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	url := "ws" + strings.TrimPrefix(hs.URL, "http")

	dir, _ := peer.Dir()
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	pk, k, _ := ed25519.GenerateKey(nil)
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "mini", Key: pk, Relay: url}}); err != nil {
		t.Fatal(err)
	}
	h.expect(2, "usage: tm peer pair", human, "peer", "relay-update")
	h.expect(1, "unknown-peer", human, "peer", "relay-update", "nas")
	h.expect(1, "human-only", coord, "peer", "relay-update", "mini")
	h.expect(1, "only from a server whose link through it is up", human, "peer", "relay-update", "mini")

	// The link, live: this server's key and mini's meet in their room.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	room := relay.LinkRoom(id.ID, relay.Fingerprint(pk))
	done := make(chan *relay.Conn, 2)
	for _, d := range []relay.Dialer{
		{Key: id.Key, Kind: relay.KindLink, Room: room, Peer: relay.Fingerprint(pk)},
		{Key: k, Kind: relay.KindLink, Room: room, Peer: id.ID},
	} {
		go func() {
			c, err := d.Dial(ctx, url)
			if err != nil {
				t.Error(err)
			}
			done <- c
		}()
	}
	var ends []*relay.Conn
	for range 2 {
		if c := <-done; c != nil {
			defer c.Close()
			ends = append(ends, c)
		}
	}
	if len(ends) != 2 {
		t.FailNow()
	}
	// A byte each way, as the link's TLS handshake would send.
	for _, p := range [][2]*relay.Conn{{ends[0], ends[1]}, {ends[1], ends[0]}} {
		p[0].Write([]byte{1})
		if _, err := io.ReadFull(p[1], make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
	}
	out := h.ok(human, "peer", "relay-update", "mini")
	if !strings.Contains(out, "updates to the latest release if it is newer") || !strings.Contains(out, "runs tm 0.27.0 now") {
		t.Fatalf("out: %s", out)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	h.expect(1, "relay-update-too-soon", human, "peer", "relay-update", "mini")
}

// TestPeerFiles: tm peer files is the human's, needs the project shared, and
// shows in tm peer list and tm network; sharing again doesn't bring it back.
func TestPeerFiles(t *testing.T) {
	h := newHarness(t)
	dir, err := peer.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := peer.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	k1, _, _ := ed25519.GenerateKey(nil)
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "mini", Key: k1, Relay: "wss://r.example.net"}}); err != nil {
		t.Fatal(err)
	}
	stubPeerServer(t, nil)
	h.ok(human, "project", "new", "demo")
	if code, _, stderr := h.run(human, "", "peer", "files", "mini", "demo", "on"); code == 0 || !strings.Contains(stderr, "isn't shared") {
		t.Fatalf("on while not shared: %d %q", code, stderr)
	}
	if err := config.SetProject("demo", "share", true); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := h.run(human, "", "peer", "files", "mini", "demo", "maybe"); code == 0 || !strings.Contains(stderr, "isn't one") {
		t.Fatalf("a bad word: %d %q", code, stderr)
	}
	if code, _, stderr := h.run(human, "", "peer", "files", "nobody", "demo", "on"); code == 0 || !strings.Contains(stderr, "no peer") {
		t.Fatalf("a peer that isn't: %d %q", code, stderr)
	}
	if out := h.ok(human, "peer", "files", "mini", "demo", "on"); !strings.Contains(out, "read-only") || !strings.Contains(out, "git-ignored") {
		t.Fatalf("on says: %q", out)
	}
	if out := h.ok(human, "peer", "list"); !strings.Contains(out, "files: demo") {
		t.Fatalf("list: %q", out)
	}
	if out := h.ok(human, "network"); !strings.Contains(out, "reads files of demo") {
		t.Fatalf("network: %q", out)
	}
	if out := h.ok(human, "peer", "files", "mini", "demo", "off"); !strings.Contains(out, "Files off") {
		t.Fatalf("off says: %q", out)
	}
	p, _ := project.Open("demo")
	if j, _ := os.ReadFile(filepath.Join(p.Dir, "JOURNAL.md")); !strings.Contains(string(j), "peer.files") {
		t.Errorf("journal:\n%s", j)
	}
	if out := h.ok(human, "peer", "list"); strings.Contains(out, "files:") {
		t.Fatalf("list after off: %q", out)
	}
}

// tm peer list shows what holds, not what the file says, and says what doesn't
// count; tm peer allow goes to the server.
func TestPeerListRefusedAndAllow(t *testing.T) {
	h := newHarness(t)
	dir, err := peer.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := peer.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	key, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.SavePeers(dir, []peer.Peer{{Alias: "mini", Key: key, Relay: "wss://r.example.net", Panes: proto.PanesType}}); err != nil {
		t.Fatal(err)
	}
	stubPeerServer(t, &proto.LinkStatus{Refused: []proto.PeerRefusal{{Alias: "mini", ID: peer.IDOf(key).String(), Words: []string{"panes write"},
		Panes: proto.PanesOff, Conversations: proto.ConversationsOn, Needs: proto.NeedsOff}}})
	out := h.ok(human, "peer", "list")
	if strings.Contains(out, "panes: write") || !strings.Contains(out, "doesn't count: panes write") || !strings.Contains(out, "tm peer allow mini") {
		t.Fatalf("list: %q", out)
	}
	var asked proto.PeerSwitchParams
	peerSwitch = func(p proto.PeerSwitchParams) (proto.PeerSwitchResult, error) {
		asked = p
		return proto.PeerSwitchResult{Alias: "mini", Allowed: []string{"panes write"}}, nil
	}
	if out := h.ok(human, "peer", "allow", "mini"); !strings.Contains(out, "Allowed for mini: panes write") {
		t.Fatalf("allow: %q", out)
	}
	if asked.Switch != proto.PeerSwitchAllow || asked.Alias != "mini" {
		t.Fatalf("asked %+v", asked)
	}
}
