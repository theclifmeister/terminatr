package device

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

// writeKey is a fresh write key's public half, as a phone sends it.
func writeKey(t *testing.T) []byte {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

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
	wk := writeKey(t)
	d, err := Add(dir, pub(a), wk, "Clif's iPhone", r)
	if err != nil || d.Level != LevelView {
		t.Fatalf("Add: %+v, %v", d, err)
	}
	if _, err := Add(dir, pub(b), nil, "clif's iphone", r); !errors.As(err, new(*TakenError)) {
		t.Fatalf("a taken name: %v", err)
	}
	if _, err := Add(dir, pub(b), wk[:64], "iPad", r); err == nil {
		t.Fatal("took a write key that isn't a point")
	}
	if _, err := SetLevel(dir, "CLIF'S IPHONE", LevelAnswer); err != nil {
		t.Fatal(err)
	}
	// Pairing the same key again with the same write key keeps its
	// level and takes the new name.
	if d, err = Add(dir, pub(a), wk, "Phone", r); err != nil || d.Level != LevelAnswer || d.Name != "Phone" {
		t.Fatalf("again: %+v, %v", d, err)
	}
	// With another write key it takes the key and drops to view.
	wk2 := writeKey(t)
	if level, changed := d.Again(wk2); level != LevelView || !changed {
		t.Fatalf("Again with another key: %s %v", level, changed)
	}
	if level, changed := d.Again(wk); level != LevelAnswer || changed {
		t.Fatalf("Again with its key: %s %v", level, changed)
	}
	if d, err = Add(dir, pub(a), wk2, "Phone", r+"#sha256="+strings.Repeat("ab", 32)); err != nil || d.Level != LevelView || !bytes.Equal(d.WriteKey, wk2) {
		t.Fatalf("again, another write key: %+v, %v", d, err)
	}
	if _, err := Add(dir, pub(b), nil, "iPad", r); err != nil {
		t.Fatal(err)
	}
	// Without a write key a device stays at view.
	if _, err := SetLevel(dir, "ipad", LevelAnswer); !errors.As(err, new(*NoWriteKeyError)) {
		t.Fatalf("answer without a write key: %v", err)
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
	if ds, _ := Load(dir); len(ds) != 1 || ds[0].Name != "Phone" || !bytes.Equal(ds[0].WriteKey, wk2) || ds[0].Verifier() == nil {
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

// TestEffective: answer runs when the device has a write key; write
// doesn't run in this release.
func TestEffective(t *testing.T) {
	wk := writeKey(t)
	for _, c := range []struct {
		level string
		key   []byte
		want  string
	}{
		{LevelView, nil, LevelView}, {LevelAnswer, wk, LevelAnswer}, {LevelAnswer, nil, LevelView},
		{LevelWrite, wk, LevelWrite}, {LevelWrite, nil, LevelView}, {"", wk, LevelView},
	} {
		if got := (Device{Level: c.level, WriteKey: c.key}).Effective(); got != c.want {
			t.Errorf("level %q, key %v: %q, want %q", c.level, c.key != nil, got, c.want)
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
	pp, ep := Join(ctx, phone, in, "Clif's\x1b[2J iPhone", "1.0", phoneWriteKey, tc)
	es := <-errs
	return offer, ps, pp, es, ep
}

// phoneWriteKey is the write key pairBoth's phone sends.
var phoneWriteKey []byte

func TestPairThroughRelay(t *testing.T) {
	url, tc := startRelay(t)
	srv, phone := newKey(t), newKey(t)
	phoneWriteKey = writeKey(t)
	offer, ps, pp, es, ep := pairBoth(t, url, tc, srv, phone, srv.ID, true)
	if es != nil || ep != nil {
		t.Fatalf("pair: %v / %v", es, ep)
	}
	if offer.Name != "Clif's[2J iPhone" || offer.ID != phone.ID || offer.Version != "1.0" || !bytes.Equal(offer.WriteKey, phoneWriteKey) {
		t.Fatalf("offer %+v", offer)
	}
	if !ps.Key.Equal(pub(phone)) || !pp.Key.Equal(pub(srv)) || pp.Name != "studio" || !bytes.Equal(ps.WriteKey, phoneWriteKey) {
		t.Fatalf("paired %+v / %+v", ps, pp)
	}
	// A write key that isn't a P-256 point is none: the device views.
	phoneWriteKey = []byte("not a key")
	offer, ps, _, es, ep = pairBoth(t, url, tc, srv, newKey(t), srv.ID, true)
	if es != nil || ep != nil || offer.WriteKey != nil || ps.WriteKey != nil {
		t.Fatalf("a bad write key: %+v %+v %v %v", offer, ps, es, ep)
	}
	phoneWriteKey = nil
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
	wk := writeKey(t)
	if _, err := Add(devDir, pub(phone), wk, "iPhone", url); err != nil {
		t.Fatal(err)
	}
	var logMu sync.Mutex
	var logged []string
	logf := func(format string, a ...any) {
		t.Logf(format, a...)
		logMu.Lock()
		logged = append(logged, fmt.Sprintf(format, a...))
		logMu.Unlock()
	}
	m := Start(Options{Dir: devDir, PeerDir: peerDir, Serve: echo, Logf: logf, RelayTLS: tc,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m.Close)
	waitState(t, m, "iPhone", proto.LinkOffline) // waiting for the phone
	// The log names the room as the relay's log does (T809).
	room := relay.LinkRoom(srv.ID, phone.ID)
	want := fmt.Sprintf("device iPhone: waiting for it in link room %x on relay ", room[:4])
	logMu.Lock()
	if !slices.ContainsFunc(logged, func(l string) bool { return strings.HasPrefix(l, want) }) {
		t.Errorf("log %q, want a line %q…", logged, want)
	}
	logMu.Unlock()

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

	// Answer reaches the open link, then write; the status says it.
	SetLevel(devDir, "iPhone", LevelAnswer)
	m.Reload()
	if got := line(); got != "iPhone answer" {
		t.Fatalf("after answer %q", got)
	}
	SetLevel(devDir, "iPhone", LevelWrite)
	m.Reload()
	if st := m.Status()[0]; st.Level != LevelWrite || st.Effective != LevelWrite || st.State != proto.LinkUp {
		t.Fatalf("after access: %+v", st)
	}
	if got := line(); got != "iPhone write" {
		t.Fatalf("after write %q", got)
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

// A level change while the link dials reaches the link without a race
// on the device its goroutine reads (T810: run with -race).
func TestLinkLevelWhileDialing(t *testing.T) {
	url, tc := startRelay(t)
	peerDir, devDir, _ := testHome(t)
	if _, err := Add(devDir, pub(newKey(t)), writeKey(t), "iPhone", url); err != nil {
		t.Fatal(err)
	}
	m := Start(Options{Dir: devDir, PeerDir: peerDir, Serve: echo, Logf: t.Logf, RelayTLS: tc,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m.Close)
	for _, level := range []string{LevelAnswer, LevelView, LevelAnswer} {
		SetLevel(devDir, "iPhone", level)
		m.Reload()
	}
	if st := waitState(t, m, "iPhone", proto.LinkOffline); st.Level != LevelAnswer {
		t.Fatalf("status %+v", st)
	}
}

// Another key in the device's room fails the pinned TLS.
func TestLinkRefusesAnotherKey(t *testing.T) {
	url, tc := startRelay(t)
	peerDir, devDir, srv := testHome(t)
	phone, other := newKey(t), newKey(t)
	wk := writeKey(t)
	if _, err := Add(devDir, pub(phone), wk, "iPhone", url); err != nil {
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

// The link notes when it was last up, in its status and in the devices
// (T828): when it comes up and when it drops, so a new manager (a
// restarted server) has it before the phone opens again.
func TestLinkLastUp(t *testing.T) {
	url, tc := startRelay(t)
	peerDir, devDir, srv := testHome(t)
	phone, wk := newKey(t), writeKey(t)
	if _, err := Add(devDir, pub(phone), wk, "iPhone", url); err != nil {
		t.Fatal(err)
	}
	m := Start(Options{Dir: devDir, PeerDir: peerDir, Serve: echo, Logf: t.Logf, RelayTLS: tc,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m.Close)
	if st := waitState(t, m, "iPhone", proto.LinkOffline); !st.LastUp.IsZero() {
		t.Fatalf("never linked, last up %v", st.LastUp)
	}
	before := time.Now().Add(-time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, phone, pub(srv), url, tc)
	if err != nil {
		t.Fatal(err)
	}
	if st := waitState(t, m, "iPhone", proto.LinkUp); st.LastUp.Before(before) {
		t.Fatalf("up, last up %v", st.LastUp)
	}
	up := saved(t, devDir).LastUp
	if up.Before(before) {
		t.Fatalf("devices.json last_up %v", up)
	}
	time.Sleep(1100 * time.Millisecond) // a second later: the drop's time differs
	c.Close()
	st := waitState(t, m, "iPhone", proto.LinkOffline)
	deadline := time.Now().Add(5 * time.Second)
	for !saved(t, devDir).LastUp.After(up) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	dropped := saved(t, devDir).LastUp
	if !dropped.After(up) || st.LastUp.Before(dropped) {
		t.Fatalf("after the drop: devices.json %v (up %v), status %v", dropped, up, st.LastUp)
	}
	m.Close()

	// A restart: the new manager's status has it at once.
	m2 := Start(Options{Dir: devDir, PeerDir: peerDir, Serve: echo, RelayTLS: tc,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m2.Close)
	if st := m2.Status()[0]; !st.LastUp.Equal(dropped) {
		t.Fatalf("after a restart, last up %v, want %v", st.LastUp, dropped)
	}
	// Renaming, setting the level and pairing again keep it.
	SetLevel(devDir, "iPhone", LevelAnswer)
	Rename(devDir, "iPhone", "Phone")
	if _, err := Add(devDir, pub(phone), wk, "Phone", url); err != nil {
		t.Fatal(err)
	}
	if d := saved(t, devDir); !d.LastUp.Equal(dropped) || d.Level != LevelAnswer {
		t.Fatalf("after changes %+v", d)
	}
	// An older time changes nothing; a device that went is no error.
	if err := SetLastUp(devDir, phone.ID, dropped.Add(-time.Hour)); err != nil || !saved(t, devDir).LastUp.Equal(dropped) {
		t.Fatalf("older time: %v %v", err, saved(t, devDir).LastUp)
	}
	if err := SetLastUp(devDir, newKey(t).ID, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// saved is the one device in dir.
func saved(t *testing.T, dir string) Device {
	t.Helper()
	ds, err := Load(dir)
	if err != nil || len(ds) != 1 {
		t.Fatalf("devices %+v %v", ds, err)
	}
	return ds[0]
}
