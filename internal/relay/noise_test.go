package relay

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type noiseRig struct {
	n     *handshakeNoise
	lines []string
	at    time.Time
}

func newNoiseRig(ips bool) *noiseRig {
	r := &noiseRig{at: time.Date(2026, 10, 23, 4, 0, 0, 0, time.UTC)}
	r.n = newHandshakeNoise(func(l string) { r.lines = append(r.lines, l) }, ips, func() time.Time { return r.at })
	return r
}

func handshakeLine(ip string, port int, reason string) string {
	return fmt.Sprintf("%s%s:%d: %s", handshakePrefix, ip, port, reason)
}

func TestNoiseFoldsKnownKindsIntoOneLine(t *testing.T) {
	r := newNoiseRig(true)
	reasons := map[string]int{
		"acme/autocert: missing server name":             980,
		"client sent an HTTP request to an HTTPS server": 63,
		"EOF": 5,
		"read tcp 192.0.2.1:443->198.51.100.2:5555: read: connection reset by peer": 4,
		"read tcp 192.0.2.1:443->198.51.100.2:5555: i/o timeout":                    3,
		"tls: client offered only unsupported versions: [302 301]":                  2,
		"tls: no cipher suite supported by both client and server":                  1,
	}
	i := 0
	for reason, k := range reasons {
		for range k {
			r.n.line(handshakeLine(fmt.Sprintf("203.0.113.%d", i%7), 40000+i, reason))
			i++
		}
	}
	if len(r.lines) != 0 {
		t.Fatalf("logged one by one: %q", r.lines[0])
	}
	r.at = r.at.Add(time.Hour)
	r.n.flush()
	want := "tls: 1058 handshakes refused in the last hour from 7 addresses (no server name 980, plain http 63, eof 5, reset 4, timeout 3, old tls 2, no shared cipher 1)"
	if len(r.lines) != 1 || r.lines[0] != want {
		t.Fatalf("summary: %q\nwant %q", r.lines, want)
	}
	r.n.flush()
	if len(r.lines) != 1 {
		t.Fatalf("a quiet hour logged %q", r.lines[1])
	}
}

func TestNoisePassesOtherLines(t *testing.T) {
	r := newNoiseRig(false)
	for _, l := range []string{
		"http: Accept error: accept tcp [::]:443: too many open files; retrying in 5ms",
		"http: panic serving 203.0.113.7:4444: boom",
		handshakePrefix + "203.0.113.7:4444: acme/autocert: unable to fetch certificate",
	} {
		r.n.line(l)
	}
	want := []string{
		"http: Accept error: accept tcp (address): too many open files; retrying in 5ms",
		"http: panic serving (address): boom",
		handshakePrefix + "(address): acme/autocert: unable to fetch certificate",
	}
	if strings.Join(r.lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", r.lines)
	}
	r.n.flush()
	if len(r.lines) != len(want) {
		t.Fatalf("a summary for nothing folded: %q", r.lines[len(want)])
	}
	r = newNoiseRig(true)
	r.n.line("http: panic serving 203.0.113.7:4444: boom")
	if r.lines[0] != "http: panic serving 203.0.113.7:4444: boom" {
		t.Fatalf("with --log-ips: %q", r.lines[0])
	}
}

func TestNoiseRateLimitsUnknownErrors(t *testing.T) {
	r := newNoiseRig(false)
	for i := range unknownPerMinute + 15 {
		r.n.line(handshakeLine("203.0.113.9", 1000+i, "tls: bad record MAC"))
	}
	if len(r.lines) != unknownPerMinute {
		t.Fatalf("logged %d unknown errors in a minute, want %d", len(r.lines), unknownPerMinute)
	}
	r.at = r.at.Add(time.Minute)
	r.n.line(handshakeLine("203.0.113.9", 999, "tls: bad record MAC"))
	if len(r.lines) != unknownPerMinute+1 {
		t.Fatal("the next minute logged no unknown error")
	}
	r.at = r.at.Add(10 * time.Minute)
	r.n.flush()
	want := "tls: 15 handshakes refused in the last 11m from 1 address (other 15)"
	if got := r.lines[len(r.lines)-1]; got != want {
		t.Fatalf("summary %q, want %q", got, want)
	}
}

// Serve logs the summary when it stops, without the client's address.
func TestServeFlushesNoiseOnStop(t *testing.T) {
	var buf lockedBuffer
	cfg := DefaultConfig()
	cfg.Log = log.New(&buf, "", 0)
	dir := t.TempDir()
	cf, kf := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	writeCert(t, cf, kf)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Serve(ctx, ln, New(cfg), TLS{CertFile: cf, KeyFile: kf}); close(done) }()
	for range 3 {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(c, "GET / HTTP/1.1\r\nHost: x\r\n\r\n")
		io.Copy(io.Discard, c) // net/http answers 400 and closes
		c.Close()
	}
	cancel()
	<-done
	got := buf.String()
	if !strings.Contains(got, "tls: 3 handshakes refused in the last 1m from 1 address (plain http 3)") ||
		strings.Contains(got, "127.0.0.1") || strings.Contains(got, "handshake error") {
		t.Fatalf("log: %q", got)
	}
}
