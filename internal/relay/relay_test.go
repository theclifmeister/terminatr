package relay

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// relayFor starts an in-process relay over TLS and returns its wss URL
// and the client TLS config that trusts it.
func relayFor(t *testing.T, cfg Config) (*Server, string, *tls.Config) {
	t.Helper()
	s := New(cfg)
	hs := httptest.NewUnstartedServer(s)
	hs.StartTLS()
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	tc := hs.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	return s, "wss" + strings.TrimPrefix(hs.URL, "https"), tc
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func fp(k ed25519.PrivateKey) [32]byte { return Fingerprint(k.Public().(ed25519.PublicKey)) }

func linkDialer(own, peer ed25519.PrivateKey, tc *tls.Config) *Dialer {
	return &Dialer{Key: own, Kind: KindLink, Room: LinkRoom(fp(own), fp(peer)), Peer: fp(peer), TLS: tc}
}

type dialed struct {
	c   *Conn
	err error
}

// dialAsync dials in the background and reports when the relay said
// "wait".
func dialAsync(ctx context.Context, d *Dialer, url string) (<-chan dialed, <-chan struct{}) {
	out := make(chan dialed, 1)
	waiting := make(chan struct{})
	var once sync.Once
	d.OnWait = func() { once.Do(func() { close(waiting) }) }
	go func() {
		c, err := d.Dial(ctx, url)
		out <- dialed{c, err}
	}()
	return out, waiting
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func got(t *testing.T, ch <-chan dialed) dialed {
	t.Helper()
	select {
	case d := <-ch:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a dial")
	}
	return dialed{}
}

func closeReason(err error) string {
	var ce *CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return ""
}

// mate dials two linked keys and returns both ends.
func mate(t *testing.T, url string, tc *tls.Config) (*Conn, *Conn, ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	a, b := newKey(t), newKey(t)
	ctx := context.Background()
	ra, wa := dialAsync(ctx, linkDialer(a, b, tc), url)
	waitFor(t, wa, "the first side to wait")
	rb, _ := dialAsync(ctx, linkDialer(b, a, tc), url)
	da, db := got(t, ra), got(t, rb)
	if da.err != nil || db.err != nil {
		t.Fatalf("dial: %v, %v", da.err, db.err)
	}
	t.Cleanup(func() { da.c.Close(); db.c.Close() })
	return da.c, db.c, a, b
}

func TestLinkRoomCopiesBytesBothWays(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	ca, cb, _, _ := mate(t, url, tc)

	big := make([]byte, 3*maxMessage+17) // more than one message
	rand.Read(big)
	go ca.Write(big)
	buf := make([]byte, len(big))
	if _, err := io.ReadFull(cb, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, big) {
		t.Fatal("b read different bytes")
	}
	go cb.Write([]byte("back"))
	small := make([]byte, 4)
	if _, err := io.ReadFull(ca, small); err != nil || string(small) != "back" {
		t.Fatalf("a read %q, %v", small, err)
	}
}

// TLS 1.3 runs inside the relayed stream, as the link will.
func TestTLSInsideTheRoom(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	ca, cb, _, _ := mate(t, url, tc)
	srvCert := httptest.NewTLSServer(http.NotFoundHandler())
	cert := srvCert.TLS.Certificates[0]
	pool := srvCert.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	srvCert.Close()

	errc := make(chan error, 1)
	go func() {
		s := tls.Server(cb, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
		b := make([]byte, 5)
		if _, err := io.ReadFull(s, b); err != nil {
			errc <- err
			return
		}
		_, err := s.Write(bytes.ToUpper(b))
		errc <- err
	}()
	c := tls.Client(ca, &tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS13})
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 5)
	if _, err := io.ReadFull(c, b); err != nil || string(b) != "HELLO" {
		t.Fatalf("read %q, %v", b, err)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestPeerGoneClosesTheOtherSide(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	ca, cb, _, _ := mate(t, url, tc)
	ca.Close()
	_, err := cb.Read(make([]byte, 1))
	if closeReason(err) != ErrPeerGone {
		t.Fatalf("err = %v, want peer-gone", err)
	}
}

func TestLinkRoomRefusesAWrongRoom(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	a, b, c := newKey(t), newKey(t), newKey(t)
	d := linkDialer(a, b, tc)
	d.Room = LinkRoom(fp(a), fp(c)) // a room b isn't in
	_, err := d.Dial(context.Background(), url)
	if closeReason(err) != ErrBadRoom {
		t.Fatalf("err = %v, want bad-room", err)
	}
	// Its own fingerprint as the peer.
	d = linkDialer(a, a, tc)
	_, err = d.Dial(context.Background(), url)
	if closeReason(err) != ErrBadRoom {
		t.Fatalf("self room: err = %v, want bad-room", err)
	}
}

// rawHello does the handshake by hand, so a test can send any hello.
func rawHello(t *testing.T, url string, tc *tls.Config, edit func(h *hello, nonce []byte)) error {
	t.Helper()
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
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var ch challenge
	json.Unmarshal(msg, &ch)
	a, b := newKey(t), newKey(t)
	room := LinkRoom(fp(a), fp(b))
	peer := fp(b)
	h := hello{T: msgHello, Kind: KindLink, Key: a.Public().(ed25519.PublicKey),
		Room: hex.EncodeToString(room[:]), Peer: hex.EncodeToString(peer[:])}
	h.Sig = ed25519.Sign(a, helloSigned(ch.Nonce, KindLink, room[:], peer[:]))
	edit(&h, ch.Nonce)
	raw, _ := json.Marshal(h)
	ws.WriteMessage(opText, raw)
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			return err
		}
	}
}

func TestHelloSignatureIsChecked(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	err := rawHello(t, url, tc, func(h *hello, nonce []byte) {
		other := newKey(t)
		h.Sig = ed25519.Sign(other, helloSigned(nonce, h.Kind, nil, nil))
	})
	if closeReason(err) != ErrBadSignature {
		t.Fatalf("err = %v, want bad-signature", err)
	}
	// A signature over another nonce (a replayed hello).
	err = rawHello(t, url, tc, func(h *hello, nonce []byte) {
		_, k, _ := ed25519.GenerateKey(rand.Reader)
		h.Key = k.Public().(ed25519.PublicKey)
		room, _ := hex32(h.Room)
		peer, _ := hex32(h.Peer)
		h.Sig = ed25519.Sign(k, helloSigned(make([]byte, 32), h.Kind, room[:], peer[:]))
	})
	if closeReason(err) != ErrBadSignature {
		t.Fatalf("replay: err = %v, want bad-signature", err)
	}
	err = rawHello(t, url, tc, func(h *hello, _ []byte) { h.Kind = "open" })
	if closeReason(err) != ErrBadHello {
		t.Fatalf("kind: err = %v, want bad-hello", err)
	}
}

func TestSameKeyReplacesItsWaitingConnection(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	a, b := newKey(t), newKey(t)
	ctx := context.Background()
	r1, w1 := dialAsync(ctx, linkDialer(a, b, tc), url)
	waitFor(t, w1, "the first connection to wait")
	r2, w2 := dialAsync(ctx, linkDialer(a, b, tc), url)
	waitFor(t, w2, "the second connection to wait")
	if d := got(t, r1); closeReason(d.err) != ErrReplaced {
		t.Fatalf("first: %v, want replaced", d.err)
	}
	rb, _ := dialAsync(ctx, linkDialer(b, a, tc), url)
	if d := got(t, r2); d.err != nil {
		t.Fatal(d.err)
	} else {
		defer d.c.Close()
	}
	if d := got(t, rb); d.err != nil {
		t.Fatal(d.err)
	} else {
		defer d.c.Close()
	}
}

func TestPairRoomIsCapped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PairBytes = 1000
	_, url, tc := relayFor(t, cfg)
	var room [32]byte
	rand.Read(room[:])
	ctx := context.Background()
	ra, wa := dialAsync(ctx, &Dialer{Key: newKey(t), Kind: KindPair, Room: room, TLS: tc}, url)
	waitFor(t, wa, "the pair room to wait")
	rb, _ := dialAsync(ctx, &Dialer{Key: newKey(t), Kind: KindPair, Room: room, TLS: tc}, url)
	da, db := got(t, ra), got(t, rb)
	if da.err != nil || db.err != nil {
		t.Fatalf("dial: %v, %v", da.err, db.err)
	}
	// A third key in a full pair room.
	_, err := (&Dialer{Key: newKey(t), Kind: KindPair, Room: room, TLS: tc}).Dial(ctx, url)
	if closeReason(err) != ErrRoomFull {
		t.Fatalf("third: %v, want room-full", err)
	}
	go da.c.Write(make([]byte, 600))
	io.ReadFull(db.c, make([]byte, 600))
	go da.c.Write(make([]byte, 600)) // over 1000 in all
	_, err = io.ReadAll(db.c)
	if closeReason(err) != ErrPairLimit {
		t.Fatalf("over the cap: %v, want pair-limit", err)
	}
}

func TestWaitTimesOut(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LinkWait = 100 * time.Millisecond
	_, url, tc := relayFor(t, cfg)
	a, b := newKey(t), newKey(t)
	_, err := linkDialer(a, b, tc).Dial(context.Background(), url)
	if closeReason(err) != ErrTimeout {
		t.Fatalf("err = %v, want timeout", err)
	}
}

func TestHelloTimesOut(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HelloTimeout = 100 * time.Millisecond
	_, url, tc := relayFor(t, cfg)
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
	ws.ReadMessage() // the challenge; then say nothing
	_, _, err = ws.ReadMessage()
	if closeReason(err) != ErrTimeout {
		t.Fatalf("err = %v, want timeout", err)
	}
}

func TestDialContextCancelsTheWait(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	r, w := dialAsync(ctx, linkDialer(newKey(t), newKey(t), tc), url)
	waitFor(t, w, "wait")
	cancel()
	if d := got(t, r); !errors.Is(d.err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", d.err)
	}
}

func TestPerIPConnectionLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxPerIP = 2
	_, url, tc := relayFor(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 2; i++ {
		_, w := dialAsync(ctx, linkDialer(newKey(t), newKey(t), tc), url)
		waitFor(t, w, "wait")
	}
	_, err := linkDialer(newKey(t), newKey(t), tc).Dial(ctx, url)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want HTTP 429", err)
	}
}

func TestNewConnectionRate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConnBurst = 2
	cfg.ConnRate = 0.001
	_, url, tc := relayFor(t, cfg)
	get := func() int {
		host := strings.TrimPrefix(url, "wss://")
		c, err := tls.Dial("tcp", host, tc)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_, err = clientHandshake(c, host, "/v1", maxMessage)
		var he *HTTPError
		if errors.As(err, &he) {
			return he.Status
		}
		return http.StatusSwitchingProtocols
	}
	for i := 0; i < 2; i++ {
		if s := get(); s != http.StatusSwitchingProtocols {
			t.Fatalf("connection %d: %d", i, s)
		}
	}
	if s := get(); s != http.StatusTooManyRequests {
		t.Fatalf("third: %d, want 429", s)
	}
}

func TestShutdownClosesRooms(t *testing.T) {
	s, url, tc := relayFor(t, DefaultConfig())
	_, cb, _, _ := mate(t, url, tc)
	s.Shutdown()
	_, err := cb.Read(make([]byte, 1))
	if r := closeReason(err); r != ErrShutdown && r != ErrPeerGone {
		t.Fatalf("err = %v, want shutdown", err)
	}
}

func TestUpgradeChecks(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	base := "https" + strings.TrimPrefix(url, "wss")
	cl := &http.Client{Transport: &http.Transport{TLSClientConfig: tc}}
	req := func(path string, h map[string]string) int {
		r, _ := http.NewRequest("GET", base+path, nil)
		for k, v := range h {
			r.Header.Set(k, v)
		}
		resp, err := cl.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := req("/healthz", nil); c != 200 {
		t.Errorf("healthz: %d", c)
	}
	if c := req("/elsewhere", nil); c != 404 {
		t.Errorf("elsewhere: %d", c)
	}
	if c := req("/v1", nil); c != http.StatusUpgradeRequired {
		t.Errorf("no upgrade: %d", c)
	}
	h := map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Version": "13",
		"Sec-WebSocket-Key": "dGhlIHNhbXBsZSBub25jZQ==", "Sec-WebSocket-Protocol": "chat"}
	if c := req("/v1", h); c != http.StatusBadRequest {
		t.Errorf("wrong subprotocol: %d", c)
	}
}

func TestDataBeforeMatedIsRefused(t *testing.T) {
	_, url, tc := relayFor(t, DefaultConfig())
	host := strings.TrimPrefix(url, "wss://")
	c, _ := tls.Dial("tcp", host, tc)
	defer c.Close()
	ws, _ := clientHandshake(c, host, "/v1", maxMessage)
	_, msg, _ := ws.ReadMessage()
	var ch challenge
	json.Unmarshal(msg, &ch)
	a, b := newKey(t), newKey(t)
	room, peer := LinkRoom(fp(a), fp(b)), fp(b)
	h := hello{T: msgHello, Kind: KindLink, Key: a.Public().(ed25519.PublicKey),
		Room: hex.EncodeToString(room[:]), Peer: hex.EncodeToString(peer[:]),
		Sig: ed25519.Sign(a, helloSigned(ch.Nonce, KindLink, room[:], peer[:]))}
	raw, _ := json.Marshal(h)
	ws.WriteMessage(opText, raw)
	ws.ReadMessage() // wait
	ws.WriteMessage(opBinary, []byte("early"))
	_, _, err := ws.ReadMessage()
	if closeReason(err) != ErrProtocol {
		t.Fatalf("err = %v, want protocol", err)
	}
}

func TestClientIPBehindTrustedProxy(t *testing.T) {
	s := New(Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	r := func(remote, xff string) string {
		req := httptest.NewRequest("GET", "/v1", nil)
		req.RemoteAddr = remote
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		return s.clientIP(req).String()
	}
	cases := []struct{ remote, xff, want string }{
		{"203.0.113.5:4000", "", "203.0.113.5"},
		{"203.0.113.5:4000", "1.2.3.4", "203.0.113.5"}, // untrusted peer: header ignored
		{"10.0.0.2:4000", "198.51.100.7", "198.51.100.7"},
		{"10.0.0.2:4000", "1.2.3.4, 198.51.100.7", "198.51.100.7"}, // the last hop the proxy added
		{"10.0.0.2:4000", "198.51.100.7, 10.0.0.3", "198.51.100.7"},
		{"10.0.0.2:4000", "junk", "10.0.0.2"},
		{"10.0.0.2:4000", "", "10.0.0.2"},
	}
	for _, c := range cases {
		if g := r(c.remote, c.xff); g != c.want {
			t.Errorf("%s %q: %s, want %s", c.remote, c.xff, g, c.want)
		}
	}
	if ipKey(netip.MustParseAddr("2001:db8::1")) != ipKey(netip.MustParseAddr("2001:db8::ffff")) {
		t.Error("one /64 should count as one address")
	}
}

func TestBucket(t *testing.T) {
	now := time.Unix(0, 0)
	var b bucket
	b.init(2, now)
	if !b.allow(1, 2, now) || !b.allow(1, 2, now) || b.allow(1, 2, now) {
		t.Fatal("burst of 2")
	}
	if !b.allow(1, 2, now.Add(time.Second)) {
		t.Fatal("refill")
	}
	b.init(100, now)
	if w := b.debit(300, 100, 100, now); w != 2*time.Second {
		t.Fatalf("debit wait %v, want 2s", w)
	}
}

// pipeWS is a client and a server wsConn over a pipe.
func pipeWS() (client, server *wsConn) {
	a, b := net.Pipe()
	return newWS(a, nil, true, 1024), newWS(b, nil, false, 1024)
}

func TestWSFragmentsAndControlFrames(t *testing.T) {
	cl, sv := pipeWS()
	go func() {
		// A fragmented message with a ping in the middle.
		cl.wmu.Lock()
		frames := [][]byte{frame(0x00|opBinary, true, []byte("he")), frame(0x80|opPing, true, nil),
			frame(0x00|opCont, true, []byte("ll")), frame(0x80|opCont, true, []byte("o"))}
		for _, f := range frames {
			cl.c.Write(f)
		}
		cl.wmu.Unlock()
	}()
	go func() { cl.ReadMessage() }() // takes the pong
	op, msg, err := sv.ReadMessage()
	if err != nil || op != opBinary || string(msg) != "hello" {
		t.Fatalf("%d %q %v", op, msg, err)
	}
}

func TestWSRefusesUnmaskedAndOversized(t *testing.T) {
	cl, sv := pipeWS()
	go func() { cl.c.Write(frame(0x80|opBinary, false, []byte("x"))); io.Copy(io.Discard, cl.c) }()
	if _, _, err := sv.ReadMessage(); !errors.Is(err, errProtocol) {
		t.Fatalf("unmasked: %v", err)
	}
	cl, sv = pipeWS()
	go func() {
		cl.WriteMessage(opBinary, make([]byte, 1024))
		cl.c.Write(frame(0x80|opBinary, true, make([]byte, 1025)))
	}()
	go func() { io.Copy(io.Discard, cl.c) }()
	if _, _, err := sv.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sv.ReadMessage(); !errors.Is(err, errTooBig) {
		t.Fatalf("oversized: %v", err)
	}
}

// frame builds a raw frame; b0 holds FIN and the opcode.
func frame(b0 byte, mask bool, p []byte) []byte {
	out := []byte{b0}
	var m byte
	if mask {
		m = 0x80
	}
	switch {
	case len(p) < 126:
		out = append(out, m|byte(len(p)))
	default:
		out = append(out, m|126, byte(len(p)>>8), byte(len(p)))
	}
	if mask {
		key := []byte{1, 2, 3, 4}
		out = append(out, key...)
		for i, c := range p {
			out = append(out, c^key[i&3])
		}
		return out
	}
	return append(out, p...)
}

func TestAcceptKey(t *testing.T) {
	// RFC 6455 §1.3's example.
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatal(got)
	}
}

// writeCert writes a fresh self-signed certificate for 127.0.0.1 and
// returns it.
func writeCert(t *testing.T, certFile, keyFile string) *x509.Certificate {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "relay test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0o600)
	c, _ := x509.ParseCertificate(der)
	return c
}

// Serve with certificate files: a renewed pair is picked up without a
// restart, and ctx ends the relay.
func TestServeReloadsTheCertificate(t *testing.T) {
	dir := t.TempDir()
	cf, kf := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	first := writeCert(t, cf, kf)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, New(DefaultConfig()), cf, kf) }()
	url := "wss://" + ln.Addr().String()

	served := func() *x509.Certificate {
		c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.ConnectionState().PeerCertificates[0]
	}
	if !served().Equal(first) {
		t.Fatal("not the first certificate")
	}
	second := writeCert(t, cf, kf)
	later := time.Now().Add(time.Minute)
	os.Chtimes(cf, later, later)
	if !served().Equal(second) {
		t.Fatal("the renewed certificate wasn't loaded")
	}
	// A relay client trusting the new certificate gets through.
	pool := x509.NewCertPool()
	pool.AddCert(second)
	_, w := dialAsync(ctx, linkDialer(newKey(t), newKey(t), &tls.Config{RootCAs: pool}), url)
	waitFor(t, w, "wait")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve didn't return")
	}
}

// FuzzReadMessage: whatever a client sends, the relay's reader neither
// panics nor takes more than its limit.
func FuzzReadMessage(f *testing.F) {
	f.Add(frame(0x80|opBinary, true, []byte("hi")))
	f.Add(append(frame(opText, true, []byte("a")), frame(0x80|opCont, true, []byte("b"))...))
	f.Add(frame(0x80|opClose, true, []byte{0x03, 0xe8, 'x'}))
	f.Add([]byte{0x82, 0xff, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		a, b := net.Pipe()
		go func() { a.Write(data); a.Close() }()
		go io.Copy(io.Discard, a)
		w := newWS(b, nil, false, 1024)
		for i := 0; i < 8; i++ {
			_, msg, err := w.ReadMessage()
			if err != nil {
				break
			}
			if len(msg) > 1024 {
				t.Fatalf("message of %d bytes", len(msg))
			}
		}
		b.Close()
	})
}

// FuzzCheckHello: any hello is refused or names a room it may be in.
func FuzzCheckHello(f *testing.F) {
	f.Add([]byte(`{"t":"hello","kind":"link","key":"AAAA","room":"00","sig":"AA"}`))
	f.Add([]byte(`{"t":"hello","kind":"pair","key":"","room":""}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var h hello
		if json.Unmarshal(raw, &h) != nil {
			return
		}
		if _, bad := checkHello(&h, make([]byte, 32)); bad == "" {
			t.Fatalf("accepted an unsigned hello: %s", raw)
		}
	})
}
