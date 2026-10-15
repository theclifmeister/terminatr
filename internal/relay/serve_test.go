package relay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// serveTLS runs s with certificate files on a local port and returns
// its address and a client TLS config.
func serveTLS(t *testing.T, s *Server) (string, *tls.Config) {
	t.Helper()
	dir := t.TempDir()
	cf, kf := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	writeCert(t, cf, kf)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Serve(ctx, ln, s, TLS{CertFile: cf, KeyFile: kf}); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return ln.Addr().String(), &tls.Config{InsecureSkipVerify: true}
}

// closedWithin says whether the server closes c within d.
func closedWithin(c net.Conn, br *bufio.Reader, d time.Duration) bool {
	c.SetReadDeadline(time.Now().Add(d))
	_, err := br.Peek(1)
	var ne net.Error
	return err != nil && !(errors.As(err, &ne) && ne.Timeout())
}

func healthz(t *testing.T, c net.Conn, br *bufio.Reader) {
	t.Helper()
	io.WriteString(c, "GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n")
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func TestServeClosesAnIdleHTTPConnection(t *testing.T) {
	s := New(DefaultConfig())
	s.http.idle = 200 * time.Millisecond
	addr, tc := serveTLS(t, s)
	c, err := tls.Dial("tcp", addr, tc)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	br := bufio.NewReader(c)
	healthz(t, c, br)
	if !closedWithin(c, br, 3*time.Second) {
		t.Fatal("an idle connection after /healthz stays open")
	}
}

func TestServeClosesASlowTLSHandshake(t *testing.T) {
	s := New(DefaultConfig())
	s.http.handshake = 200 * time.Millisecond
	addr, _ := serveTLS(t, s)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !closedWithin(c, bufio.NewReader(c), 3*time.Second) {
		t.Fatal("a connection that never starts TLS stays open")
	}
}

// The HTTP deadlines don't reach a WebSocket: it lives past them.
func TestServeWebSocketOutlivesTheHTTPDeadlines(t *testing.T) {
	s := New(DefaultConfig())
	s.http = httpTimeouts{header: 300 * time.Millisecond, handshake: 300 * time.Millisecond, idle: 300 * time.Millisecond}
	addr, tc := serveTLS(t, s)
	ca, cb, _, _ := mate(t, "wss://"+addr, tc)
	time.Sleep(time.Second)
	go ca.Write([]byte("still here"))
	buf := make([]byte, 10)
	cb.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(cb, buf); err != nil || string(buf) != "still here" {
		t.Fatalf("after the HTTP deadlines: %q %v", buf, err)
	}
}

func TestServeLimitsConnectionsPerAddressAtAccept(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxPerIP = 2
	_, perIP := cfg.acceptLimits()
	addr, _ := serveTLS(t, New(cfg))
	var cs []net.Conn
	defer func() {
		for _, c := range cs {
			c.Close()
		}
	}()
	for i := 0; i < perIP; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		cs = append(cs, c)
	}
	over, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer over.Close()
	if !closedWithin(over, bufio.NewReader(over), 3*time.Second) {
		t.Fatalf("connection %d from one address isn't closed", perIP+1)
	}
	for _, c := range cs {
		if closedWithin(c, bufio.NewReader(c), 50*time.Millisecond) {
			t.Fatal("a connection within the limit was closed")
		}
	}
	cs[0].Close()
	cs = cs[1:]
	// Its place is free again: TLS goes through.
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a freed place isn't taken: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLimitListenerCountsInAllAndSparesTrustedProxies(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	proxy := netip.MustParseAddr("127.0.0.1")
	l := newLimitListener(ln, 3, 1, func(a netip.Addr) bool { return a == proxy })
	accepted := make(chan net.Conn, 10)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	// One address over its per-address limit of 1, since it's trusted;
	// the global limit of 3 still holds.
	var dialed []net.Conn
	for i := 0; i < 4; i++ {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		dialed = append(dialed, c)
	}
	var got []net.Conn
	for len(got) < 3 {
		select {
		case c := <-accepted:
			got = append(got, c)
		case <-time.After(3 * time.Second):
			t.Fatalf("accepted %d of 3", len(got))
		}
	}
	if !closedWithin(dialed[3], bufio.NewReader(dialed[3]), 3*time.Second) {
		t.Fatal("the fourth connection isn't closed")
	}
	if all, _ := l.counts(ipKey(proxy)); all != 3 {
		t.Fatalf("open %d, want 3", all)
	}
	got[0].Close()
	got[0].Close() // a second close gives nothing back
	if all, one := l.counts(ipKey(proxy)); all != 2 || one != 2 {
		t.Fatalf("after a close: %d in all, %d from the address", all, one)
	}
}

func TestLimitListenerCountsIPv6ByPrefix(t *testing.T) {
	l := newLimitListener(nil, 10, 1, func(netip.Addr) bool { return false })
	a := ipKey(netip.MustParseAddr("2001:db8:1:2::1"))
	b := ipKey(netip.MustParseAddr("2001:db8:1:2:ffff::9"))
	if !l.take(a, true) || l.take(b, true) {
		t.Fatal("two addresses in one /64 count apart")
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// handshakeErrorLog serves with logIPs, fails one TLS handshake and
// returns the log.
func handshakeErrorLog(t *testing.T, logIPs bool) string {
	var buf lockedBuffer
	cfg := DefaultConfig()
	cfg.LogIPs = logIPs
	cfg.Log = log.New(&buf, "", 0)
	addr, _ := serveTLS(t, New(cfg))
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("\x16\x03\x01\x00\x05hello-not-a-client-hello"))
	io.Copy(io.Discard, c)
	c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(buf.String(), "handshake error") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return buf.String()
}

func TestServeErrorLogHasNoAddresses(t *testing.T) {
	got := handshakeErrorLog(t, false)
	if !strings.Contains(got, "handshake error") || strings.Contains(got, "127.0.0.1") {
		t.Fatalf("log: %q", got)
	}
	if got := handshakeErrorLog(t, true); !strings.Contains(got, "127.0.0.1") {
		t.Fatalf("with --log-ips: %q", got)
	}
}

func TestHideAddrs(t *testing.T) {
	for in, want := range map[string]string{
		"http: TLS handshake error from 203.0.113.7:51234: EOF":                   "http: TLS handshake error from (address): EOF",
		"http: TLS handshake error from [2001:db8::1]:443: EOF":                   "http: TLS handshake error from (address): EOF",
		"read tcp 192.0.2.1:443->198.51.100.2:5555: i/o timeout":                  "read tcp (address)->(address): i/o timeout",
		"dial ::ffff:10.0.0.1 and 2001:db8::2 failed.":                            "dial (address) and (address) failed.",
		"certificate reloaded from /etc/x.pem at 10:06:07, version 1.2, v0.27.0.": "certificate reloaded from /etc/x.pem at 10:06:07, version 1.2, v0.27.0.",
	} {
		if got := hideAddrs(in); got != want {
			t.Errorf("hideAddrs(%q) = %q, want %q", in, got, want)
		}
	}
}

// A connection keeps no big write buffer after a big message.
func TestWriteKeepsNoBigBuffer(t *testing.T) {
	s, url, tc := relayFor(t, DefaultConfig())
	ca, cb, _, _ := mate(t, url, tc)
	msg := bytes.Repeat([]byte("x"), maxMessage)
	go ca.Write(msg)
	if _, err := io.ReadFull(cb, make([]byte, len(msg))); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.all {
		c.ws.wmu.Lock()
		n := cap(c.ws.wbuf)
		c.ws.wmu.Unlock()
		if n > wbufKeep {
			t.Fatalf("a connection keeps a %d-byte write buffer", n)
		}
	}
	for _, w := range []*wsConn{ca.ws, cb.ws} {
		if cap(w.wbuf) > wbufKeep {
			t.Fatalf("a client keeps a %d-byte write buffer", cap(w.wbuf))
		}
	}
}

func TestConnsFor(t *testing.T) {
	if n := ConnsFor(200 << 20); n < DefaultConfig().MaxConns {
		t.Errorf("GOMEMLIMIT=200MiB fits %d, fewer than the default %d", n, DefaultConfig().MaxConns)
	}
	if n := ConnsFor(64 << 20); n != 409 {
		t.Errorf("64 MiB: %d", n)
	}
	if n := ConnsFor(1 << 20); n != 16 {
		t.Errorf("1 MiB: %d, want the least, 16", n)
	}
}
