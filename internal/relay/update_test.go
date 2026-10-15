package relay

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The relay tells every client its tm in the challenge.
func TestChallengeTellsTheVersion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Version = "v0.28.0"
	_, url, tc := relayFor(t, cfg)
	ca, _, _, _ := mate(t, url, tc)
	if v := ca.RelayVersion(); v != "v0.28.0" {
		t.Fatalf("relay version %q", v)
	}
	_, url, tc = relayFor(t, DefaultConfig())
	if ca, _, _, _ := mate(t, url, tc); ca.RelayVersion() != "" {
		t.Fatalf("a relay without a version said %q", ca.RelayVersion())
	}
}

// An update request is a signed hello from a key with a live link
// through the relay that has carried bytes both ways for UpdateAfter;
// it writes the request file, at most once per UpdateKeyEvery from one
// key and once per UpdateEvery from any, and says so.
func TestUpdateRequest(t *testing.T) {
	file := filepath.Join(t.TempDir(), "update-request")
	cfg := DefaultConfig()
	cfg.Version, cfg.UpdateFile = "v0.27.0", file
	s, url, tc := relayFor(t, cfg)
	// The relay reads the clock on its own goroutines, so the test moves
	// it under a lock.
	var clock sync.Mutex
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { clock.Lock(); defer clock.Unlock(); return now }
	advance := func(d time.Duration) { clock.Lock(); now = now.Add(d); clock.Unlock() }
	s.mu.Lock()
	s.now = nowFn
	s.mu.Unlock()
	ctx := context.Background()

	// No link yet: refused, nothing written.
	a, b := newKey(t), newKey(t)
	res, err := linkDialer(a, b, tc).RequestUpdate(ctx, url)
	if err != nil || res.Result != UpdateNotLinked || res.Version != "v0.27.0" {
		t.Fatalf("unlinked: %+v %v", res, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("request file written: %v", err)
	}

	ca, cb, a, b := mate(t, url, tc)
	// Mated, but nothing sent yet: not a link that is used.
	if res, _ := linkDialer(b, a, tc).RequestUpdate(ctx, url); res.Result != UpdateNotLinked {
		t.Fatalf("silent room: %+v", res)
	}
	exchange(t, ca, cb)
	// A stranger naming the live room with its own key: the room isn't
	// its own, so the hello is refused.
	c := newKey(t)
	d := linkDialer(c, b, tc)
	d.Room = LinkRoom(fp(a), fp(b))
	if _, err := d.RequestUpdate(ctx, url); closeReason(err) != ErrBadRoom {
		t.Fatalf("stranger: %v", err)
	}
	// A hello signed by another key than the one it names.
	if reason := forgedUpdate(t, a, b, c); reason != ErrBadSignature {
		t.Fatalf("forged: %q", reason)
	}

	// Too soon until the room has been mated UpdateAfter.
	res, err = linkDialer(b, a, tc).RequestUpdate(ctx, url)
	if err != nil || res.Result != UpdateTooSoon || !res.Next.Equal(nowFn().Add(10*time.Minute)) {
		t.Fatalf("young room: %+v %v", res, err)
	}
	advance(10 * time.Minute)
	res, err = linkDialer(b, a, tc).RequestUpdate(ctx, url)
	if err != nil || res.Result != UpdateQueued {
		t.Fatalf("linked: %+v %v", res, err)
	}
	if body, err := os.ReadFile(file); err != nil || !strings.HasPrefix(string(body), "update to the latest release\n") {
		t.Fatalf("request file %q %v", body, err)
	}
	// From any key, once per UpdateEvery.
	advance(9 * time.Minute)
	res, err = linkDialer(a, b, tc).RequestUpdate(ctx, url)
	if err != nil || res.Result != UpdateTooSoon || !res.Next.Equal(nowFn().Add(time.Minute)) {
		t.Fatalf("too soon: %+v %v", res, err)
	}
	// The limit outlives the relay: a new one reads the last request's time.
	os.Remove(file)
	s2 := New(cfg)
	s2.now = nowFn
	if !s2.updated.Equal(nowFn().Add(-9 * time.Minute)) {
		t.Fatalf("restarted relay's last request %v", s2.updated)
	}
	advance(time.Minute)
	if res, _ := linkDialer(a, b, tc).RequestUpdate(ctx, url); res.Result != UpdateQueued {
		t.Fatalf("ten minutes later, the other key: %+v", res)
	}
	// From one key, once per UpdateKeyEvery: b asked 20 minutes ago.
	advance(10 * time.Minute)
	res, err = linkDialer(b, a, tc).RequestUpdate(ctx, url)
	if err != nil || res.Result != UpdateTooSoon || !res.Next.Equal(nowFn().Add(40*time.Minute)) {
		t.Fatalf("same key: %+v %v", res, err)
	}
	advance(40 * time.Minute)
	if res, _ := linkDialer(b, a, tc).RequestUpdate(ctx, url); res.Result != UpdateQueued {
		t.Fatalf("an hour later: %+v", res)
	}
	// The link stayed up through all of it.
	if s.mu.Lock(); len(s.busy) != 1 {
		t.Errorf("rooms: %d", len(s.busy))
	}
	s.mu.Unlock()
}

// exchange sends a few bytes each way between two mated ends.
func exchange(t *testing.T, a, b *Conn) {
	t.Helper()
	for _, p := range [][2]*Conn{{a, b}, {b, a}} {
		if _, err := p[0].Write([]byte("hi")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 2)
		if _, err := io.ReadFull(p[1], buf); err != nil {
			t.Fatal(err)
		}
	}
}

// A relay without --update-request takes none.
func TestUpdateRequestOff(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	ca, cb, a, b := mate(t, url, tc)
	exchange(t, ca, cb)
	if res, err := linkDialer(a, b, tc).RequestUpdate(context.Background(), url); err != nil || res.Result != UpdateOff {
		t.Fatalf("%+v %v", res, err)
	}
}

// forgedUpdate checks an update hello naming key a but signed by c,
// and returns the relay's error word.
func forgedUpdate(t *testing.T, a, b, c ed25519.PrivateKey) string {
	t.Helper()
	nonce := make([]byte, 32)
	room, peer := LinkRoom(fp(a), fp(b)), fp(b)
	h := hello{T: msgHello, Kind: KindUpdate, Key: a.Public().(ed25519.PublicKey),
		Room: hex.EncodeToString(room[:]), Peer: hex.EncodeToString(peer[:]),
		Sig: ed25519.Sign(c, helloSigned(nonce, KindUpdate, room[:], peer[:]))}
	_, bad := checkHello(&h, nonce)
	return bad
}
