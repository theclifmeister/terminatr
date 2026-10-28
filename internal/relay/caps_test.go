package relay

// The caps that keep the relay's connections and bandwidth for the
// servers that use it (docs/SPEC.md §16.4).

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// relayFrom is relayFor where each connection comes from the next of
// the addresses from gives, as a proxy would say (the test dials one at
// a time).
func relayFrom(t *testing.T, cfg Config) (s *Server, url string, tc *http.Transport, from func(addr string)) {
	t.Helper()
	s = New(cfg)
	var mu sync.Mutex
	var next string
	hs := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if next != "" {
			r.RemoteAddr = net.JoinHostPort(next, "4000")
		}
		mu.Unlock()
		s.ServeHTTP(w, r)
	}))
	hs.StartTLS()
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	tr := hs.Client().Transport.(*http.Transport)
	return s, "wss" + strings.TrimPrefix(hs.URL, "https"), tr, func(addr string) {
		mu.Lock()
		next = addr
		mu.Unlock()
	}
}

// Waiting connections are capped per address (IPv6 by /48) and in all;
// a full relay makes room for a newcomer from another address by closing
// the oldest waiter of the address with the most, so a real link still
// mates while strangers sit in rooms of their own.
func TestWaitingIsCapped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConns, cfg.MaxWaitingPerIP = 8, 2 // 4 may wait
	_, url, tr, from := relayFrom(t, cfg)
	tc := tr.TLSClientConfig
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type squatter struct {
		addr string
		r    <-chan dialed
	}
	var sq []squatter
	wait := func(addr string) {
		t.Helper()
		from(addr)
		r, w := dialAsync(ctx, linkDialer(newKey(t), newKey(t), tc), url)
		waitFor(t, w, "a waiter from "+addr)
		sq = append(sq, squatter{addr, r})
	}
	refused := func(addr string) {
		t.Helper()
		from(addr)
		_, err := linkDialer(newKey(t), newKey(t), tc).Dial(ctx, url)
		if closeReason(err) != ErrRateLimited {
			t.Fatalf("a waiter from %s: %v, want rate-limited", addr, err)
		}
	}
	wait("2001:db8:1::1")
	wait("2001:db8:1:2::1")    // another /64, the same /48
	refused("2001:db8:1:3::1") // a third from that /48
	wait("198.51.100.1")
	wait("198.51.100.1")
	refused("198.51.100.1")
	// Full: 4 waiting, two addresses with 2 each. A newcomer from an
	// address with none closes the oldest waiter of the address with the
	// most (equal: the first by name).
	a, b := newKey(t), newKey(t)
	from("203.0.113.9")
	ra, wa := dialAsync(ctx, linkDialer(a, b, tc), url)
	waitFor(t, wa, "the real link's first side")
	if d := got(t, sq[2].r); closeReason(d.err) != ErrTimeout {
		t.Fatalf("the oldest waiter of 198.51.100.1: %v, want timeout", d.err)
	}
	// Now one from 203.0.113.9 waits: it can't take a place from an
	// address with 2 (it would only trade places).
	refused("203.0.113.9")
	// The other side mates without waiting.
	from("203.0.113.10")
	rb, _ := dialAsync(ctx, linkDialer(b, a, tc), url)
	da, db := got(t, ra), got(t, rb)
	if da.err != nil || db.err != nil {
		t.Fatalf("the real link: %v, %v", da.err, db.err)
	}
	defer da.c.Close()
	defer db.c.Close()
	exchange(t, da.c, db.c)
}

// The eviction rule alone: room is made only from an address with at
// least two more waiting than the newcomer's, from its oldest waiter.
func TestRoomToWait(t *testing.T) {
	s := New(Config{MaxConns: 6, MaxWaitingPerIP: 3}) // 3 may wait
	t0 := time.Now()
	add := func(wKey string, age time.Duration) *conn {
		c := &conn{wKey: wKey, since: t0.Add(-age)}
		rand.Read(c.chk.room[:])
		s.waiting[roomKey{KindLink, c.chk.room}] = c
		return c
	}
	add("a", time.Minute)
	old := add("a", time.Hour)
	add("b", time.Minute)
	if e, why := s.roomToWait(&conn{wKey: "b"}); e != nil || why != ErrRateLimited {
		t.Fatalf("b (1 vs 2): %v %q", e, why)
	}
	if e, why := s.roomToWait(&conn{wKey: "c"}); e != old || why != "" {
		t.Fatalf("c (0 vs 2): %v %q, want a's oldest", e, why)
	}
	s.cfg.MaxWaiting = 4
	if e, why := s.roomToWait(&conn{wKey: "b"}); e != nil || why != "" {
		t.Fatalf("not full: %v %q", e, why)
	}
	s.cfg.MaxWaitingPerIP = 2
	if _, why := s.roomToWait(&conn{wKey: "a"}); why != ErrRateLimited {
		t.Fatalf("a over its own cap: %q", why)
	}
	if waitKey(netip.MustParseAddr("2001:db8:1:2::1")) != waitKey(netip.MustParseAddr("2001:db8:1:ffff::1")) {
		t.Error("one /48 should count as one address for waiting")
	}
	if New(Config{MaxConns: 50}).cfg.MaxWaiting != 25 {
		t.Error("MaxWaiting should default to half of MaxConns")
	}
}

// A link room that never mated here waits NewLinkWait; one that did,
// LinkWait.
func TestNewRoomsWaitLess(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NewLinkWait, cfg.LinkWait = 50*time.Millisecond, 600*time.Millisecond
	_, url, tc := relayFor(t, cfg)
	waited := func(a, b ed25519.PrivateKey) time.Duration {
		t.Helper()
		start := time.Now()
		_, err := linkDialer(a, b, tc).Dial(context.Background(), url)
		if closeReason(err) != ErrTimeout {
			t.Fatalf("err = %v, want timeout", err)
		}
		return time.Since(start)
	}
	if d := waited(newKey(t), newKey(t)); d > 500*time.Millisecond {
		t.Fatalf("a new room waited %v", d)
	}
	ca, cb, a, b := mate(t, url, tc)
	ca.Close()
	cb.Close()
	if d := waited(a, b); d < 500*time.Millisecond {
		t.Fatalf("a room that mated waited only %v", d)
	}
}

// All connections together send at most TotalBytesRate (after the
// burst), whatever their addresses; one room at most RoomBytesRate.
func TestBandwidthCaps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TotalBytesRate, cfg.TotalBytesBurst = 1000, 1000
	cfg.RoomBytesRate, cfg.RoomBytesBurst = 100, 500
	s := New(cfg)
	t0 := time.Now()
	now := t0
	s.now = func() time.Time { return now }
	a := &conn{ipKey: "a"}
	b := &conn{ipKey: "b"}
	for _, c := range []*conn{a, b} {
		st := &ipState{}
		st.bytes.init(cfg.BytesBurst, now)
		s.ips[c.ipKey] = st
	}
	s.total.init(cfg.TotalBytesBurst, now)
	if w := s.debit(a, nil, 1000); w != 0 {
		t.Fatalf("within the burst: %v", w)
	}
	// Another address, its own bucket full: the relay's is empty.
	if w := s.debit(b, nil, 500); w != 500*time.Millisecond {
		t.Fatalf("over the total: %v", w)
	}
	now = t0.Add(10 * time.Second) // everything refilled
	m := &mated{}
	m.room.init(cfg.RoomBytesBurst, now)
	if w := s.debit(a, m, 400); w != 0 {
		t.Fatalf("room within its burst: %v", w)
	}
	if w := s.debit(b, m, 200); w != time.Second {
		t.Fatalf("over the room's: %v", w)
	}
}

// The total cap holds for real traffic: two rooms from different
// addresses, each well within its address's rate, share it.
func TestTotalBandwidthSlowsEveryRoom(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TotalBytesRate, cfg.TotalBytesBurst = 1<<20, 128<<10
	_, url, tr, from := relayFrom(t, cfg)
	tc := tr.TLSClientConfig
	var ends [][2]*Conn
	for _, addr := range []string{"198.51.100.1", "198.51.100.2"} {
		from(addr)
		ca, cb, _, _ := mate(t, url, tc)
		ends = append(ends, [2]*Conn{ca, cb})
	}
	const each = 640 << 10
	start := time.Now()
	var wg sync.WaitGroup
	for _, e := range ends {
		wg.Add(2)
		go func() {
			defer wg.Done()
			e[0].Write(make([]byte, each))
		}()
		go func() {
			defer wg.Done()
			io.ReadFull(e[1], make([]byte, each))
		}()
	}
	wg.Wait()
	// (2×640 KiB − 128 KiB) at 1 MiB/s is 1.125 s.
	if d := time.Since(start); d < time.Second {
		t.Fatalf("1.25 MiB through a 1 MiB/s relay in %v", d)
	}
}

// Pings and pongs count against the address's bytes, and a connection
// sending more than ControlRate (after ControlBurst) is closed.
func TestControlFramesAreMetered(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ControlRate, cfg.ControlBurst = 0.001, 3
	cfg.BytesRate = 1 // no refill to speak of
	s, url, tc := relayFor(t, cfg)
	host := strings.TrimPrefix(url, "wss://")
	c, err := tls.Dial("tcp", host, tc)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ws, err := clientHandshake(c, host, "/v1", maxMessage)
	if err != nil {
		t.Fatal(err)
	}
	ws.ReadMessage() // the challenge
	for range 4 {
		ws.write(opPing, make([]byte, 100))
	}
	if _, _, err := ws.ReadMessage(); closeReason(err) != ErrRateLimited {
		t.Fatalf("err = %v, want rate-limited", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ips[ipKey(netip.MustParseAddr("127.0.0.1"))]
	if st == nil || st.bytes.tokens > cfg.BytesBurst-3*100 {
		t.Fatalf("the address's bucket wasn't debited: %+v", st)
	}
}

// TestDefaultPerIPAboveWaiting: the waiting count among an address's
// connections, so the default leaves an address as many again to mate
// with: a server waiting in all its rooms, and its phones on the same
// network mating them.
func TestDefaultPerIPAboveWaiting(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxPerIP < 2*cfg.MaxWaitingPerIP {
		t.Fatalf("--max-per-ip %d, waiting per address %d", cfg.MaxPerIP, cfg.MaxWaitingPerIP)
	}
}
