package relay

import (
	"net"
	"net/netip"
	"sync"
)

// limitListener counts every accepted TCP connection, by address (an
// IPv6 address by its /64) and in all, and closes one over either limit
// at once, before TLS or HTTP see it. admit's limits come later, for the
// relay's WebSocket only; these hold every other connection too.
type limitListener struct {
	net.Listener
	max, perIP int
	exempt     func(netip.Addr) bool // trusted proxies: no per-address limit

	mu    sync.Mutex
	all   int
	byKey map[string]int
}

func newLimitListener(ln net.Listener, max, perIP int, exempt func(netip.Addr) bool) *limitListener {
	return &limitListener{Listener: ln, max: max, perIP: perIP, exempt: exempt, byKey: map[string]int{}}
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		key, perIP := "", true
		if ap, err := netip.ParseAddrPort(c.RemoteAddr().String()); err == nil {
			ip := ap.Addr().Unmap()
			key, perIP = ipKey(ip), !l.exempt(ip)
		}
		if l.take(key, perIP) {
			return &limitConn{Conn: c, l: l, key: key}, nil
		}
		c.Close()
	}
}

func (l *limitListener) take(key string, perIP bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.all >= l.max || (perIP && l.byKey[key] >= l.perIP) {
		return false
	}
	l.all++
	l.byKey[key]++
	return true
}

func (l *limitListener) put(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all--
	if l.byKey[key]--; l.byKey[key] <= 0 {
		delete(l.byKey, key)
	}
}

// counts is the connections open in all and from key, for tests.
func (l *limitListener) counts(key string) (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.all, l.byKey[key]
}

// limitConn gives its place back when closed, once.
type limitConn struct {
	net.Conn
	l    *limitListener
	key  string
	once sync.Once
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.l.put(c.key) })
	return err
}
