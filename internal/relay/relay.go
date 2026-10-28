// Package relay is tm relay serve, the meeting point of two linked tm
// servers, and the client side its link dials with (docs/SPEC.md §16).
//
// The relay checks who connects (a signed challenge), puts the two
// connections of a room together and copies their bytes. It stores
// nothing and sees only key fingerprints, addresses, sizes and timing:
// the servers run TLS with pinned keys inside the copied stream.
package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxMessage   = 64 << 10 // the largest message either way
	writeTimeout = 30 * time.Second
	closeGrace   = time.Second
)

// Config is the relay's limits. DefaultConfig fills every field.
type Config struct {
	MaxConns   int     // connections in all
	MaxPerIP   int     // connections from one address (IPv6: one /64)
	ConnRate   float64 // new connections per second from one address
	ConnBurst  float64
	BytesRate  float64 // bytes per second one address may send
	BytesBurst float64

	// MaxWaiting is how many connections may wait in rooms at once, so
	// that waiting can't take every connection; 0 is half of MaxConns.
	MaxWaiting int
	// MaxWaitingPerIP is how many of them may come from one address (an
	// IPv6 address counts by its /48).
	MaxWaitingPerIP int
	TotalBytesRate  float64 // bytes per second all connections may send together
	TotalBytesBurst float64
	RoomBytesRate   float64 // bytes per second one mated room may carry, both ways
	RoomBytesBurst  float64
	ControlRate     float64 // pings and pongs per second one connection may send
	ControlBurst    float64

	HelloTimeout time.Duration // from upgrade to a valid hello
	LinkWait     time.Duration // how long a link room that mated here before waits for its peer
	NewLinkWait  time.Duration // how long a link room that never mated here waits
	PairWait     time.Duration // how long a pair room waits
	PairLife     time.Duration // how long a mated pair room lasts
	PairBytes    int64         // bytes a mated pair room may carry in all
	Ping         time.Duration // ping interval
	Idle         time.Duration // closed after this long without a frame

	// TrustedProxies may set X-Forwarded-For: behind a TLS-terminating
	// proxy, the client's address is the last hop they added.
	TrustedProxies []netip.Prefix
	LogIPs         bool
	Log            *log.Logger // nil: no log

	// Version is the relay's tm release, told to every client in the
	// challenge; "" tells none.
	Version string
	// AutoUpdate, "on" or "off", is whether the host updates the relay
	// by itself; told in /healthz's Tm-Auto-Update header, "" tells none.
	AutoUpdate string
	// UpdateFile is where an update request is written (update.go), in
	// the relay's state directory; "" takes none.
	UpdateFile string
	// UpdateEvery is the least time between two requests taken, from
	// any key; UpdateKeyEvery the least between two from one key.
	UpdateEvery    time.Duration
	UpdateKeyEvery time.Duration
	// UpdateAfter is how long a link room must have been mated, with
	// bytes sent both ways, before its keys may ask for an update.
	UpdateAfter time.Duration
}

// DefaultConfig is the relay's limits unless flags say otherwise.
func DefaultConfig() Config {
	return Config{
		// An address may wait in 16 rooms and still mate as many: a
		// phone on the server's own network mates from its address.
		MaxConns: 1024, MaxPerIP: 32,
		ConnRate: 0.5, ConnBurst: 10,
		BytesRate: 4 << 20, BytesBurst: 8 << 20,
		MaxWaitingPerIP: 16,
		TotalBytesRate:  16 << 20, TotalBytesBurst: 32 << 20,
		RoomBytesRate: 2 << 20, RoomBytesBurst: 64 << 20,
		ControlRate: 0.2, ControlBurst: 10,
		HelloTimeout:   10 * time.Second,
		LinkWait:       time.Hour,
		NewLinkWait:    5 * time.Minute,
		PairWait:       10 * time.Minute,
		PairLife:       2 * time.Minute,
		PairBytes:      256 << 10,
		Ping:           30 * time.Second,
		Idle:           90 * time.Second,
		UpdateEvery:    10 * time.Minute,
		UpdateKeyEvery: time.Hour,
		UpdateAfter:    10 * time.Minute,
	}
}

type roomKey struct {
	kind string
	id   [32]byte
}

func (k roomKey) String() string { return k.kind + " room " + hex.EncodeToString(k.id[:4]) }

// mated is a room whose two connections are joined.
type mated struct {
	a, b  *conn
	since time.Time
	bytes atomic.Int64 // carried so far, for pair rooms
	room  bucket       // bytes it may carry; under Server.mu
}

// ipState is one address's connections and buckets.
type ipState struct {
	conns int
	newc  bucket
	bytes bucket
	seen  time.Time
}

// Server is a relay. It is an http.Handler; Serve runs it on a listener.
type Server struct {
	cfg Config

	mu      sync.Mutex
	closing bool
	all     map[*conn]struct{}
	waiting map[roomKey]*conn
	busy    map[roomKey]*mated
	ips     map[string]*ipState
	total   bucket                 // all connections' bytes
	known   map[[32]byte]time.Time // link rooms that mated here, when last
	swept   time.Time
	updated time.Time              // the last update request taken
	keyUpd  map[[32]byte]time.Time // the last taken from each key

	now  func() time.Time // tests
	http httpTimeouts
}

// New returns a relay with cfg's limits.
func New(cfg Config) *Server {
	if cfg.MaxWaiting <= 0 {
		cfg.MaxWaiting = max(cfg.MaxConns/2, 1)
	}
	s := &Server{
		cfg: cfg, now: time.Now, http: defaultHTTPTimeouts,
		all:     map[*conn]struct{}{},
		waiting: map[roomKey]*conn{},
		busy:    map[roomKey]*mated{},
		ips:     map[string]*ipState{},
		known:   map[[32]byte]time.Time{},
		updated: lastUpdate(cfg.UpdateFile),
		keyUpd:  map[[32]byte]time.Time{},
	}
	s.total.init(cfg.TotalBytesBurst, s.now())
	return s
}

func (s *Server) logf(format string, a ...any) {
	if s.cfg.Log != nil {
		s.cfg.Log.Printf(format, a...)
	}
}

// ServeHTTP answers /v1 (the relay), /healthz and nothing else.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		// No tm version here: anyone can ask, and the exact release
		// tells which known bugs it has. A linked server gets it in the
		// challenge.
		if s.cfg.AutoUpdate != "" {
			w.Header().Set("Tm-Auto-Update", s.cfg.AutoUpdate)
		}
		io.WriteString(w, "ok\n")
	case "/v1":
		s.serveWS(w, r)
	default:
		http.NotFound(w, r)
	}
}

// Shutdown closes every connection with "shutdown" and refuses new ones.
func (s *Server) Shutdown() {
	s.mu.Lock()
	s.closing = true
	all := make([]*conn, 0, len(s.all))
	for c := range s.all {
		all = append(all, c)
	}
	s.mu.Unlock()
	for _, c := range all {
		c.closeWith(CloseGoingAway, ErrShutdown)
	}
}

// clientIP is the address limits count against: the direct peer, or,
// when that is a trusted proxy, the last address in X-Forwarded-For that
// isn't one.
func (s *Server) clientIP(r *http.Request) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := ap.Addr().Unmap()
	if !s.trusted(ip) {
		return ip
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return ip // a hop we can't read: count the proxy itself
		}
		a = a.Unmap()
		if !s.trusted(a) {
			return a
		}
	}
	return ip
}

func (s *Server) trusted(ip netip.Addr) bool {
	for _, p := range s.cfg.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// ipKey groups IPv6 addresses by /64, which one host can trivially fill.
func ipKey(ip netip.Addr) string {
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}

// waitKey groups addresses for the waiting limits: IPv6 by /48, the
// least a site is usually given.
func waitKey(ip netip.Addr) string {
	if ip.Is6() {
		p, _ := ip.Prefix(48)
		return p.String()
	}
	return ip.String()
}

// admit counts a new connection from key, or says why not.
func (s *Server) admit(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweep(now)
	if s.closing || len(s.all) >= s.cfg.MaxConns {
		return false
	}
	st := s.ips[key]
	if st == nil {
		st = &ipState{}
		st.newc.init(s.cfg.ConnBurst, now)
		st.bytes.init(s.cfg.BytesBurst, now)
		s.ips[key] = st
	}
	st.seen = now
	if st.conns >= s.cfg.MaxPerIP || !st.newc.allow(s.cfg.ConnRate, s.cfg.ConnBurst, now) {
		return false
	}
	st.conns++
	return true
}

// sweep forgets addresses with no connection whose buckets have
// refilled, rooms that last mated over knownFor ago and keys' update
// times that no longer limit them.
func (s *Server) sweep(now time.Time) {
	if now.Sub(s.swept) < time.Minute {
		return
	}
	s.swept = now
	for k, st := range s.ips {
		if st.conns == 0 && now.Sub(st.seen) > 10*time.Minute {
			delete(s.ips, k)
		}
	}
	for k, t := range s.known {
		if now.Sub(t) > knownFor {
			delete(s.known, k)
		}
	}
	for k, t := range s.keyUpd {
		if now.Sub(t) >= s.cfg.UpdateKeyEvery {
			delete(s.keyUpd, k)
		}
	}
}

// knownFor is how long the relay remembers a link room that mated, for
// the longer LinkWait; at most 4×MaxConns rooms.
const knownFor = 24 * time.Hour

// remember notes that link room id mated (under s.mu).
func (s *Server) remember(id [32]byte, now time.Time) {
	if _, ok := s.known[id]; !ok && len(s.known) >= 4*s.cfg.MaxConns {
		var oldest [32]byte
		var at time.Time
		for k, t := range s.known {
			if at.IsZero() || t.Before(at) {
				oldest, at = k, t
			}
		}
		delete(s.known, oldest)
	}
	s.known[id] = now
}

// debit takes n bytes c sent from its address's bucket, the relay's
// and, with m, its room's, and says how long to wait.
func (s *Server) debit(c *conn, m *mated, n int) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var wait time.Duration
	if st := s.ips[c.ipKey]; st != nil {
		st.seen = now
		wait = st.bytes.debit(float64(n), s.cfg.BytesRate, s.cfg.BytesBurst, now)
	}
	wait = max(wait, s.total.debit(float64(n), s.cfg.TotalBytesRate, s.cfg.TotalBytesBurst, now))
	if m != nil {
		wait = max(wait, m.room.debit(float64(n), s.cfg.RoomBytesRate, s.cfg.RoomBytesBurst, now))
	}
	return wait
}

// pause waits d, or until c is closed (false).
func (c *conn) pause(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-c.done:
		return false
	}
}

// controlOverhead is a control frame's header as a client sends it.
const controlOverhead = 6

// control meters a ping or pong from the client: its bytes count
// against the address and the relay like data, and more than
// ControlRate a second (after ControlBurst) closes the connection.
func (c *conn) control(n int) bool {
	s := c.s
	if !c.ctl.allow(s.cfg.ControlRate, s.cfg.ControlBurst, s.now()) {
		c.closeWith(CloseRateLimit, ErrRateLimited)
		return false
	}
	return c.pause(s.debit(c, nil, n+controlOverhead))
}

// conn is one relay client.
type conn struct {
	s     *Server
	ws    *wsConn
	ip    netip.Addr
	ipKey string
	wKey  string // waitKey(ip)
	start time.Time
	ctl   bucket    // control frames; the reading goroutine's
	since time.Time // waiting since; under Server.mu

	chk  checked
	peer atomic.Pointer[conn]
	room atomic.Pointer[mated] // set before peer
	in   atomic.Int64          // bytes this client sent

	done      chan struct{}
	closeOnce sync.Once
	reason    atomic.Value // string: why the relay closed it
	timers    []*time.Timer

	smu       sync.Mutex // orders "wait" before "mated"
	matedSent bool
}

func (c *conn) closeWith(code int, reason string) {
	c.closeOnce.Do(func() {
		c.reason.Store(reason)
		c.ws.CloseWith(code, reason)
		close(c.done)
	})
}

// shut closes a connection that ended on its own.
func (c *conn) shut() {
	c.closeOnce.Do(func() {
		c.reason.Store("gone")
		c.ws.Close()
		close(c.done)
	})
}

// status sends "wait" or "mated"; a "wait" after "mated" is dropped.
func (c *conn) status(t string) {
	c.smu.Lock()
	defer c.smu.Unlock()
	if c.matedSent {
		return
	}
	c.matedSent = t == msgMated
	c.send(status{T: t})
}

func (c *conn) who() string {
	if c.s.cfg.LogIPs {
		return c.ip.String() + " "
	}
	return ""
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	key := ipKey(ip)
	if !ip.IsValid() || !s.admit(key) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	ws, err := upgrade(w, r, maxMessage)
	if err != nil {
		s.release(nil, key)
		return
	}
	c := &conn{s: s, ws: ws, ip: ip, ipKey: key, wKey: waitKey(ip), start: s.now(), done: make(chan struct{})}
	c.ctl.init(s.cfg.ControlBurst, c.start)
	ws.idle = s.cfg.Idle
	ws.control = c.control
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		c.closeWith(CloseGoingAway, ErrShutdown)
		s.release(nil, key)
		return
	}
	s.all[c] = struct{}{}
	s.mu.Unlock()
	go c.pinger()
	if reason := c.run(); reason != "" {
		c.closeWith(closeCode(reason), reason)
	} else {
		c.shut()
	}
	s.release(c, key)
	if c.chk.kind != "" && c.chk.kind != KindUpdate {
		got, _ := c.reason.Load().(string)
		s.logf("%sleft %s after %s: %d bytes in (%s)", c.who(), roomKey{c.chk.kind, c.chk.room},
			s.now().Sub(c.start).Round(time.Second), c.in.Load(), got)
	}
}

// release undoes admit and the room state of c.
func (s *Server) release(c *conn, key string) {
	var peer *conn
	s.mu.Lock()
	if st := s.ips[key]; st != nil {
		st.conns--
		st.seen = s.now()
	}
	if c != nil {
		for _, t := range c.timers {
			t.Stop()
		}
		delete(s.all, c)
		k := roomKey{c.chk.kind, c.chk.room}
		if s.waiting[k] == c {
			delete(s.waiting, k)
		}
		if m := s.busy[k]; m != nil && (m.a == c || m.b == c) {
			delete(s.busy, k)
			if k.kind == KindLink {
				s.remember(k.id, s.now())
			}
		}
		peer = c.peer.Load()
	}
	s.mu.Unlock()
	if peer != nil {
		peer.closeWith(ClosePeerGone, ErrPeerGone)
	}
}

func closeCode(reason string) int {
	switch reason {
	case ErrBadHello:
		return CloseBadHello
	case ErrBadSignature, ErrBadRoom:
		return CloseForbidden
	case ErrTimeout:
		return CloseTimeout
	case ErrReplaced, ErrRoomFull:
		return CloseConflict
	case ErrPeerGone:
		return ClosePeerGone
	case ErrRateLimited, ErrPairLimit:
		return CloseRateLimit
	case ErrShutdown:
		return CloseGoingAway
	}
	return CloseProtocol
}

func (c *conn) pinger() {
	t := time.NewTicker(c.s.cfg.Ping)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if c.ws.Ping() != nil {
				return
			}
		}
	}
}

func (c *conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.ws.WriteMessage(opText, b)
}

// run is one connection's life: the hello, the room, then the copy. It
// returns the error word to close with, or "" when the connection is
// already gone.
func (c *conn) run() string {
	s := c.s
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return ErrShutdown
	}
	helloTimer := time.AfterFunc(s.cfg.HelloTimeout, func() { c.closeWith(CloseTimeout, ErrTimeout) })
	if c.send(challenge{T: msgChallenge, V: Version, Nonce: nonce, Max: maxMessage, TM: s.cfg.Version}) != nil {
		return ""
	}
	op, msg, err := c.ws.ReadMessage()
	if !helloTimer.Stop() {
		return "" // timed out
	}
	if err != nil {
		return ""
	}
	var h hello
	if op != opText || json.Unmarshal(msg, &h) != nil {
		return ErrBadHello
	}
	chk, bad := checkHello(&h, nonce)
	if bad != "" {
		s.logf("%srefused a hello: %s", c.who(), bad)
		return bad
	}
	c.chk = chk
	if chk.kind == KindUpdate {
		return s.update(c)
	}
	if why := s.join(c); why != "" {
		return why
	}
	for {
		op, msg, err := c.ws.ReadMessage()
		if err != nil {
			return ""
		}
		peer := c.peer.Load()
		if op != opBinary || peer == nil {
			return ErrProtocol // data before "mated", or text after it
		}
		c.in.Add(int64(len(msg)))
		m := c.room.Load()
		if c.chk.kind == KindPair && m.bytes.Add(int64(len(msg))) > s.cfg.PairBytes {
			peer.closeWith(CloseRateLimit, ErrPairLimit)
			return ErrPairLimit
		}
		if !c.pause(s.debit(c, m, len(msg))) {
			return ""
		}
		peer.status(msgMated) // the peer hears "mated" before any data
		if peer.ws.WriteMessage(opBinary, msg) != nil {
			return ErrPeerGone
		}
	}
}

// join puts c in its room: it waits there, mates with the waiter, or
// replaces its own older connection.
func (s *Server) join(c *conn) string {
	k := roomKey{c.chk.kind, c.chk.room}
	var drop []*conn // replaced: closed outside the lock
	s.mu.Lock()
	if m := s.busy[k]; m != nil {
		if m.a.chk.fp != c.chk.fp && m.b.chk.fp != c.chk.fp {
			s.mu.Unlock()
			return ErrRoomFull
		}
		// One side came back before the relay saw it go: end the old
		// pair; the other side reconnects and meets the new one.
		delete(s.busy, k)
		drop = append(drop, m.a, m.b)
	}
	w := s.waiting[k]
	if w != nil && w.chk.fp == c.chk.fp {
		delete(s.waiting, k)
		drop = append(drop, w)
		w = nil
	}
	if w == nil {
		evict, why := s.roomToWait(c)
		if why != "" {
			s.mu.Unlock()
			closeDropped(drop, c)
			return why
		}
		if evict != nil {
			delete(s.waiting, roomKey{evict.chk.kind, evict.chk.room})
		}
		now := s.now()
		c.since = now
		s.waiting[k] = c
		wait := s.cfg.PairWait
		if k.kind == KindLink {
			wait = s.cfg.NewLinkWait
			if _, ok := s.known[k.id]; ok {
				wait = s.cfg.LinkWait
			}
		}
		s.mu.Unlock()
		if evict != nil {
			s.logf("%sclosed the oldest waiting %s to make room", evict.who(), roomKey{evict.chk.kind, evict.chk.room})
			evict.closeWith(CloseTimeout, ErrTimeout)
		}
		closeDropped(drop, c)
		t := time.AfterFunc(wait, func() {
			s.mu.Lock()
			still := s.waiting[k] == c
			if still {
				delete(s.waiting, k)
			}
			s.mu.Unlock()
			if still {
				c.closeWith(CloseTimeout, ErrTimeout)
			}
		})
		s.mu.Lock()
		c.timers = append(c.timers, t)
		s.mu.Unlock()
		c.status(msgWait)
		return ""
	}
	delete(s.waiting, k)
	now := s.now()
	m := &mated{a: w, b: c, since: now}
	m.room.init(s.cfg.RoomBytesBurst, now)
	s.busy[k] = m
	if k.kind == KindLink {
		s.remember(k.id, now)
	}
	w.room.Store(m)
	c.room.Store(m)
	w.peer.Store(c)
	c.peer.Store(w)
	s.mu.Unlock()
	for _, d := range drop {
		d.closeWith(CloseConflict, ErrReplaced)
	}
	if k.kind == KindPair {
		t := time.AfterFunc(s.cfg.PairLife, func() {
			w.closeWith(CloseRateLimit, ErrPairLimit)
			c.closeWith(CloseRateLimit, ErrPairLimit)
		})
		s.mu.Lock()
		c.timers = append(c.timers, t)
		s.mu.Unlock()
	}
	s.logf("mated %s", k)
	w.status(msgMated)
	c.status(msgMated)
	return ""
}

// closeDropped closes the connections c's join ended: its own older
// one replaced, the partner of that one gone.
func closeDropped(drop []*conn, c *conn) {
	for _, d := range drop {
		if d.chk.fp == c.chk.fp {
			d.closeWith(CloseConflict, ErrReplaced)
		} else {
			d.closeWith(ClosePeerGone, ErrPeerGone)
		}
	}
}

// roomToWait says whether c may wait (under s.mu): at most
// MaxWaitingPerIP from its address, else rate-limited; at most
// MaxWaiting in all. A full relay makes room by closing the oldest
// waiter of the address with the most waiting, when that address has at
// least two more than c's (so evictions even out and end); else c is
// rate-limited.
func (s *Server) roomToWait(c *conn) (evict *conn, why string) {
	per := map[string]int{}
	for _, w := range s.waiting {
		per[w.wKey]++
	}
	mine := per[c.wKey]
	if mine >= s.cfg.MaxWaitingPerIP {
		return nil, ErrRateLimited
	}
	if len(s.waiting) < s.cfg.MaxWaiting {
		return nil, ""
	}
	most, n := "", 0
	for k, v := range per {
		if v > n || (v == n && k < most) {
			most, n = k, v
		}
	}
	if n < mine+2 {
		return nil, ErrRateLimited
	}
	for _, w := range s.waiting {
		if w.wKey == most && (evict == nil || w.since.Before(evict.since)) {
			evict = w
		}
	}
	return evict, ""
}

// bucket is a token bucket. Debt is allowed for bytes (the sender
// waits it off); new connections need a whole token.
type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) init(burst float64, now time.Time) { b.tokens, b.last = burst, now }

func (b *bucket) refill(rate, burst float64, now time.Time) {
	if now.Before(b.last) {
		return
	}
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
}

func (b *bucket) allow(rate, burst float64, now time.Time) bool {
	b.refill(rate, burst, now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *bucket) debit(n, rate, burst float64, now time.Time) time.Duration {
	b.refill(rate, burst, now)
	b.tokens -= n
	if b.tokens >= 0 || rate <= 0 {
		return 0
	}
	return time.Duration(-b.tokens / rate * float64(time.Second))
}
