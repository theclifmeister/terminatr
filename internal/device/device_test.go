package device

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

func newKey(t *testing.T) *peer.Identity {
	t.Helper()
	_, k, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return peer.IdentityOf(k)
}

func pub(id *peer.Identity) ed25519.PublicKey { return id.Key.Public().(ed25519.PublicKey) }

func startRelay(t *testing.T) (string, *tls.Config) {
	t.Helper()
	s := relay.New(relay.DefaultConfig())
	hs := httptest.NewUnstartedServer(s)
	hs.StartTLS()
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	tc := hs.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	return "wss" + strings.TrimPrefix(hs.URL, "https"), tc
}

func TestRegistry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "device")
	a, b := newKey(t), newKey(t)
	const r = "wss://relay.example.net"
	d, err := Add(dir, pub(a), "Clif's iPhone", r)
	if err != nil || d.Level != LevelView {
		t.Fatalf("Add: %+v, %v", d, err)
	}
	if _, err := Add(dir, pub(b), "clif's iphone", r); !errors.As(err, new(*TakenError)) {
		t.Fatalf("a taken name: %v", err)
	}
	if _, err := SetLevel(dir, "CLIF'S IPHONE", LevelAnswer); err != nil {
		t.Fatal(err)
	}
	// Pairing the same key again keeps its level, takes the new name.
	if d, err = Add(dir, pub(a), "Phone", r+"#sha256="+strings.Repeat("ab", 32)); err != nil || d.Level != LevelAnswer {
		t.Fatalf("again: %+v, %v", d, err)
	}
	if _, err := Add(dir, pub(b), "iPad", r); err != nil {
		t.Fatal(err)
	}
	if _, err := Rename(dir, "ipad", "phone"); !errors.As(err, new(*TakenError)) {
		t.Fatalf("rename to a taken name: %v", err)
	}
	// The short id names a device too.
	if d, err = Rename(dir, b.ID.Short(), "Tablet"); err != nil || d.Name != "Tablet" {
		t.Fatalf("rename by id: %+v, %v", d, err)
	}
	ds, err := Load(dir)
	if err != nil || len(ds) != 2 {
		t.Fatalf("Load: %+v, %v", ds, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, DevicesFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("devices.json: %v %v", fi, err)
	}
	if _, err := Remove(dir, "nobody"); !errors.As(err, new(*UnknownError)) {
		t.Fatalf("remove nobody: %v", err)
	}
	if _, err := Remove(dir, "tablet"); err != nil {
		t.Fatal(err)
	}
	if ds, _ := Load(dir); len(ds) != 1 || ds[0].Name != "Phone" {
		t.Fatalf("after remove: %+v", ds)
	}
	if _, err := SetLevel(dir, "phone", "admin"); err == nil {
		t.Fatal("took level admin")
	}
}

func TestNames(t *testing.T) {
	for _, bad := range []string{"", " x", "x ", "a\nb", "a\x1bb", strings.Repeat("x", MaxName+1)} {
		if CheckName(bad) == nil {
			t.Errorf("CheckName(%q) took it", bad)
		}
	}
	if got := CleanName("  Clif's\n iPhone "); got != "Clif's iPhone" {
		t.Errorf("CleanName: %q", got)
	}
	if CleanName("\x00") != "iPhone" {
		t.Errorf("CleanName of nothing: %q", CleanName("\x00"))
	}
	ds := []Device{{Name: "iPhone", Key: pub(newKey(t))}, {Name: "iPhone 2", Key: pub(newKey(t))}}
	if got := FreeName(ds, "iphone", pub(newKey(t))); got != "iphone 3" {
		t.Errorf("FreeName: %q", got)
	}
	if got := FreeName(ds, "iPhone", ds[0].Key); got != "iPhone" {
		t.Errorf("FreeName for its own key: %q", got)
	}
	if got := FreeName(ds, strings.Repeat("x", MaxName), pub(newKey(t))); CheckName(got) != nil {
		t.Errorf("FreeName of a long one: %q", got)
	}
}

func TestEffective(t *testing.T) {
	for l, want := range map[string]string{LevelView: LevelView, LevelAnswer: LevelView, LevelWrite: LevelView, "": LevelView} {
		if got := Effective(l); got != want {
			t.Errorf("Effective(%q) = %q", l, got)
		}
	}
}

func TestInvite(t *testing.T) {
	srv := newKey(t)
	in, err := NewInvite("wss://relay.example.net#sha256="+strings.Repeat("0f", 32), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := in.String()
	if !strings.HasPrefix(s, "terminatr://pair?v=1&relay=wss%3A%2F%2Frelay.example.net%23sha256%3D0f0f") {
		t.Fatalf("invite %s", s)
	}
	back, err := ParseInvite(s)
	if err != nil || back != in {
		t.Fatalf("ParseInvite: %+v, %v", back, err)
	}
	for _, bad := range []string{"", "https://pair?v=1", "terminatr://pair?v=2", strings.Replace(s, "server=", "server=00", 1),
		strings.Replace(s, "secret=", "secret=X", 1)} {
		if _, err := ParseInvite(bad); err == nil {
			t.Errorf("ParseInvite(%q) took it", bad)
		}
	}
}

// pairBoth runs a device pairing through the relay: the server's side
// says yes as yes says; the offer it was shown.
func pairBoth(t *testing.T, url string, tc *tls.Config, srv, phone *peer.Identity, server peer.ID, yes bool) (Offer, *peer.Paired, *peer.Paired, error, error) {
	t.Helper()
	in, err := NewInvite(url, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	waiting := make(chan struct{})
	var offer Offer
	var ps *peer.Paired
	errs := make(chan error, 1)
	go func() {
		var err error
		ps, err = Pair(ctx, srv, in, PairOptions{Name: "studio", Version: "0.57.0", RelayTLS: tc, Waiting: func() { close(waiting) },
			Confirm: func(o Offer) (bool, error) { offer = o; return yes, nil }})
		errs <- err
	}()
	select {
	case <-waiting:
	case err := <-errs:
		t.Fatalf("pair: %v", err)
	}
	in.Server = server
	pp, ep := Join(ctx, phone, in, "Clif's\x1b[2J iPhone", "1.0", tc)
	es := <-errs
	return offer, ps, pp, es, ep
}

func TestPairThroughRelay(t *testing.T) {
	url, tc := startRelay(t)
	srv, phone := newKey(t), newKey(t)
	offer, ps, pp, es, ep := pairBoth(t, url, tc, srv, phone, srv.ID, true)
	if es != nil || ep != nil {
		t.Fatalf("pair: %v / %v", es, ep)
	}
	if offer.Name != "Clif's[2J iPhone" || offer.ID != phone.ID || offer.Version != "1.0" {
		t.Fatalf("offer %+v", offer)
	}
	if !ps.Key.Equal(pub(phone)) || !pp.Key.Equal(pub(srv)) || pp.Name != "studio" {
		t.Fatalf("paired %+v / %+v", ps, pp)
	}
}

func TestPairDeclinedOnServer(t *testing.T) {
	url, tc := startRelay(t)
	srv, phone := newKey(t), newKey(t)
	_, _, _, es, ep := pairBoth(t, url, tc, srv, phone, srv.ID, false)
	if !errors.Is(es, peer.ErrDeclined) || !errors.Is(ep, peer.ErrPeerNo) {
		t.Fatalf("got %v / %v", es, ep)
	}
}

// A phone whose code names another key (someone else's server, or one
// in between) stops before it proves anything.
func TestPairWrongServer(t *testing.T) {
	url, tc := startRelay(t)
	srv, phone := newKey(t), newKey(t)
	_, _, _, _, ep := pairBoth(t, url, tc, srv, phone, newKey(t).ID, true)
	if !errors.Is(ep, peer.ErrNotExpected) {
		t.Fatalf("got %v", ep)
	}
}

// home is a server's peer and device directories, with its identity.
func testHome(t *testing.T) (peerDir, devDir string, id *peer.Identity) {
	t.Helper()
	h := t.TempDir()
	peerDir, devDir = peer.DirIn(h), DirIn(h)
	id, _, err := peer.EnsureIdentity(peerDir)
	if err != nil {
		t.Fatal(err)
	}
	return peerDir, devDir, id
}

// echo serves a link by writing the device's name and level, then each
// line back, and a level line on every change.
func echo(ctx context.Context, c net.Conn, d Device, level *Level) error {
	go func() {
		for {
			v, ch := level.Get()
			if _, err := c.Write([]byte(d.Name + " " + v + "\n")); err != nil {
				return
			}
			select {
			case <-ch:
			case <-ctx.Done():
				return
			}
		}
	}()
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		c.Write(append(sc.Bytes(), '\n'))
	}
	return sc.Err()
}

func waitState(t *testing.T, m *Manager, name, state string) proto.DeviceLink {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last []proto.DeviceLink
	for time.Now().Before(deadline) {
		last = m.Status()
		for _, l := range last {
			if l.Name == name && l.State == state {
				return l
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("device %s never %s; last %+v", name, state, last)
	return proto.DeviceLink{}
}

func TestLink(t *testing.T) {
	url, tc := startRelay(t)
	peerDir, devDir, srv := testHome(t)
	phone := newKey(t)
	if _, err := Add(devDir, pub(phone), "iPhone", url); err != nil {
		t.Fatal(err)
	}
	m := Start(Options{Dir: devDir, PeerDir: peerDir, Serve: echo, Logf: t.Logf, RelayTLS: tc,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m.Close)
	waitState(t, m, "iPhone", proto.LinkOffline) // waiting for the phone

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, phone, pub(srv), url, tc)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	br := bufio.NewReader(c)
	line := func() string {
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		s, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return strings.TrimSpace(s)
	}
	if got := line(); got != "iPhone view" {
		t.Fatalf("first line %q", got)
	}
	st := waitState(t, m, "iPhone", proto.LinkUp)
	if st.ID != phone.ID.String() || st.Effective != LevelView {
		t.Fatalf("status %+v", st)
	}
	c.Write([]byte("hello\n"))
	if got := line(); got != "hello" {
		t.Fatalf("echo %q", got)
	}

	// A level the release doesn't enable runs at view; the status says
	// both.
	SetLevel(devDir, "iPhone", LevelWrite)
	m.Reload()
	if st := m.Status()[0]; st.Level != LevelWrite || st.Effective != LevelView || st.State != proto.LinkUp {
		t.Fatalf("after access: %+v", st)
	}

	// Removing the device closes its link at once.
	Remove(devDir, "iPhone")
	m.Reload()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := br.ReadString('\n'); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the link stayed open: %v", err)
	}
	if len(m.Status()) != 0 {
		t.Fatalf("status %+v", m.Status())
	}
}

// Another key in the device's room fails the pinned TLS.
func TestLinkRefusesAnotherKey(t *testing.T) {
	url, tc := startRelay(t)
	peerDir, devDir, srv := testHome(t)
	phone, other := newKey(t), newKey(t)
	if _, err := Add(devDir, pub(phone), "iPhone", url); err != nil {
		t.Fatal(err)
	}
	m := Start(Options{Dir: devDir, PeerDir: peerDir, Serve: echo, RelayTLS: tc,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m.Close)
	waitState(t, m, "iPhone", proto.LinkOffline)
	// The other key can't even reach the phone's room: the relay checks
	// that a link room is the hash of the two keys in the hello. So it
	// dials the room with its own key and the server's, and finds nobody.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if c, err := Dial(ctx, other, pub(srv), url, tc); err == nil {
		c.Close()
		t.Fatal("an unpaired key got a link")
	}
}
