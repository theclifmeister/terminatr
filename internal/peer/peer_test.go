package peer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

func TestIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peer")
	if _, err := LoadIdentity(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadIdentity on nothing: %v, want not-exist", err)
	}
	id, created, err := EnsureIdentity(dir)
	if err != nil || !created {
		t.Fatalf("EnsureIdentity: %v, created %v", err, created)
	}
	fi, err := os.Stat(filepath.Join(dir, IdentityFile))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("identity key mode %v, want 0600", fi.Mode().Perm())
	}
	again, created, err := EnsureIdentity(dir)
	if err != nil || created || again.ID != id.ID {
		t.Fatalf("EnsureIdentity again: %v, created %v, same id %v", err, created, again.ID == id.ID)
	}
	if id.ID != IDOf(id.Key.Public().(ed25519.PublicKey)) || [32]byte(id.ID) != relay.Fingerprint(id.Key.Public().(ed25519.PublicKey)) {
		t.Error("the server id is not the key's fingerprint")
	}
	if !regexp.MustCompile(`^[0-9A-F]{4}-[0-9A-F]{4}$`).MatchString(id.ID.Short()) || !strings.HasPrefix(id.ID.String(), strings.ToLower(strings.ReplaceAll(id.ID.Short(), "-", ""))) {
		t.Errorf("Short %q of %s", id.ID.Short(), id.ID)
	}

	// A broken key is an error, never replaced: peers have it pinned.
	os.WriteFile(filepath.Join(dir, IdentityFile), []byte("junk"), 0o600)
	if _, _, err := EnsureIdentity(dir); err == nil {
		t.Fatal("EnsureIdentity replaced a broken key")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, IdentityFile)); string(b) != "junk" {
		t.Error("the broken key was overwritten")
	}
}

func TestPeers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peer")
	if ps, err := LoadPeers(dir); err != nil || ps != nil {
		t.Fatalf("LoadPeers on nothing: %v, %v", ps, err)
	}
	a, b := newKey(t), newKey(t)
	ps := []Peer{{Alias: "mini", Key: pub(a), Relay: "wss://relay.example.net"}, {Alias: "studio", Key: pub(b), Relay: "wss://relay.example.net:8443"}}
	if err := SavePeers(dir, ps); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPeers(dir)
	if err != nil || len(got) != 2 || got[1].Alias != "studio" || !got[1].Key.Equal(pub(b)) || got[1].Relay != ps[1].Relay {
		t.Fatalf("LoadPeers: %+v, %v", got, err)
	}
	for name, bad := range map[string][]Peer{
		"no alias":  {{Key: pub(a), Relay: "wss://r"}},
		"short key": {{Alias: "x", Key: pub(a)[:8], Relay: "wss://r"}},
		"https":     {{Alias: "x", Key: pub(a), Relay: "https://r"}},
		"two names": {{Alias: "x", Key: pub(a), Relay: "wss://r"}, {Alias: "x", Key: pub(b), Relay: "wss://r"}},
		"two keys":  {{Alias: "x", Key: pub(a), Relay: "wss://r"}, {Alias: "y", Key: pub(a), Relay: "wss://r"}},
	} {
		if err := SavePeers(dir, bad); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
}

func TestBackoff(t *testing.T) {
	for n, want := range map[int]time.Duration{0: time.Second, 1: 2 * time.Second, 3: 8 * time.Second, 6: time.Minute, 40: time.Minute} {
		for range 50 {
			if d := backoff(n); d < want/2 || d > want {
				t.Fatalf("backoff(%d) = %s, want %s to %s", n, d, want/2, want)
			}
		}
	}
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, k, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pub(k ed25519.PrivateKey) ed25519.PublicKey { return k.Public().(ed25519.PublicKey) }

// startRelay runs an in-process relay over TLS: its wss URL and the
// outer TLS that trusts it.
func startRelay(t *testing.T) (*relay.Server, *httptest.Server, string, *tls.Config) {
	t.Helper()
	s := relay.New(relay.DefaultConfig())
	hs := httptest.NewUnstartedServer(s)
	hs.StartTLS()
	t.Cleanup(func() { s.Shutdown(); hs.Close() })
	tc := hs.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	return s, hs, "wss" + strings.TrimPrefix(hs.URL, "https"), tc
}

// server is one side of a test: a peer directory with an identity key.
type server struct {
	dir string
	id  *Identity
}

func newServer(t *testing.T) *server {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "peer")
	id, _, err := EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &server{dir: dir, id: id}
}

func (s *server) pair(t *testing.T, alias string, o *server, url string) {
	t.Helper()
	ps, _ := LoadPeers(s.dir)
	if err := SavePeers(s.dir, append(ps, Peer{Alias: alias, Key: pub(o.id.Key), Relay: url})); err != nil {
		t.Fatal(err)
	}
}

func (s *server) start(t *testing.T, version string, tc *tls.Config) *Manager {
	t.Helper()
	m := Start(Options{Dir: s.dir, Version: version, Logf: t.Logf, RelayTLS: tc,
		Backoff: func(int) time.Duration { return 20 * time.Millisecond }})
	t.Cleanup(m.Close)
	return m
}

// waitLink waits until the link to alias satisfies ok.
func waitLink(t *testing.T, m *Manager, alias, what string, ok func(proto.PeerLink) bool) proto.PeerLink {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last proto.PeerLink
	for time.Now().Before(deadline) {
		if st := m.Status(); st != nil {
			for _, p := range st.Peers {
				if p.Alias == alias {
					last = p
					if ok(p) {
						return p
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("link %s never %s; last %+v", alias, what, last)
	return last
}

func isUp(p proto.PeerLink) bool { return p.State == proto.LinkUp && p.RTT > 0 }

func offline(reason string) func(proto.PeerLink) bool {
	return func(p proto.PeerLink) bool { return p.State == proto.LinkOffline && p.Reason == reason }
}

func TestLinkThroughRelay(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "studio", a, url)

	ma := a.start(t, "0.30.0", tc)
	// Alone in the room: the relay is up, the peer isn't.
	waitLink(t, ma, "mini", "offline · peer", offline(proto.LinkReasonPeer))

	mb := b.start(t, "0.31.0", tc)
	pa := waitLink(t, ma, "mini", "up", isUp)
	pb := waitLink(t, mb, "studio", "up", isUp)
	if pa.Version != "0.31.0" || pa.LinkProtocol != LinkProtocol || pb.Version != "0.30.0" {
		t.Errorf("hellos: a sees %+v, b sees %+v", pa, pb)
	}
	if pa.ID != b.id.ID.String() || !strings.HasPrefix(pa.Transport, "relay 127.0.0.1:") {
		t.Errorf("a's link: %+v", pa)
	}
	st := ma.Status()
	if st.ID != a.id.ID.String() || st.Short != a.id.ID.Short() {
		t.Errorf("status id %s %s, want %s", st.ID, st.Short, a.id.ID)
	}

	// b goes away: a waits in the room again, then b comes back.
	mb.Close()
	waitLink(t, ma, "mini", "offline · peer after b left", offline(proto.LinkReasonPeer))
	mb = b.start(t, "0.31.0", tc)
	waitLink(t, ma, "mini", "up again", isUp)
	waitLink(t, mb, "studio", "up again", isUp)
}

func TestLinkRelayDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "wss://" + ln.Addr().String()
	ln.Close()
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	m := a.start(t, "0.30.0", nil)
	p := waitLink(t, m, "mini", "offline · relay", offline(proto.LinkReasonRelay))
	if p.Error == "" || p.Retry.IsZero() {
		t.Errorf("offline without an error or a retry: %+v", p)
	}
}

func TestLinkRelayShutdown(t *testing.T) {
	rs, _, url, tc := startRelay(t)
	a, b := newServer(t), newServer(t)
	a.pair(t, "mini", b, url)
	b.pair(t, "studio", a, url)
	ma, mb := a.start(t, "1", tc), b.start(t, "1", tc)
	waitLink(t, ma, "mini", "up", isUp)
	waitLink(t, mb, "studio", "up", isUp)
	rs.Shutdown()
	waitLink(t, ma, "mini", "offline · relay", func(p proto.PeerLink) bool {
		return p.State == proto.LinkOffline && p.Reason == proto.LinkReasonRelay && strings.Contains(p.Error, relay.ErrShutdown)
	})
}

func TestReload(t *testing.T) {
	_, _, url, tc := startRelay(t)
	a := &server{dir: filepath.Join(t.TempDir(), "peer")}
	m := Start(Options{Dir: a.dir, Version: "1", RelayTLS: tc})
	t.Cleanup(m.Close)
	if m.Status() != nil {
		t.Fatal("a status without an identity key")
	}
	b, c := newServer(t), newServer(t)
	var err error
	if a.id, _, err = EnsureIdentity(a.dir); err != nil {
		t.Fatal(err)
	}
	a.pair(t, "mini", b, url)
	m.Reload()
	waitLink(t, m, "mini", "waiting", offline(proto.LinkReasonPeer))
	if st := m.Status(); st.ID != a.id.ID.String() || len(st.Peers) != 1 {
		t.Fatalf("status %+v", st)
	}
	// mini leaves, nas comes.
	SavePeers(a.dir, []Peer{{Alias: "nas", Key: pub(c.id.Key), Relay: url}})
	m.Reload()
	if st := m.Status(); len(st.Peers) != 1 || st.Peers[0].Alias != "nas" {
		t.Fatalf("after reload: %+v", st.Peers)
	}
	// A broken file keeps the links and says why.
	os.WriteFile(filepath.Join(a.dir, PeersFile), []byte("{"), 0o600)
	if m.Reload() == nil {
		t.Fatal("Reload took a broken peers file")
	}
	if st := m.Status(); len(st.Peers) != 1 || st.Error == "" {
		t.Fatalf("after a broken reload: %+v", st)
	}
}

// tcpPair is two ends of a loopback TCP connection: buffered, as a
// relay's stream is (net.Pipe isn't, and both ends say hello at once).
func tcpPair() (net.Conn, net.Conn, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	defer ln.Close()
	a, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		return nil, nil, err
	}
	b, err := ln.Accept()
	if err != nil {
		a.Close()
		return nil, nil, err
	}
	return a, b, nil
}

// pipeTransport hands the link one end of a connection whose other end
// is served by far.
type pipeTransport struct {
	far func(net.Conn)
}

func (p pipeTransport) Dial(ctx context.Context, wait func()) (net.Conn, error) {
	a, b, err := tcpPair()
	if err != nil {
		return nil, err
	}
	go p.far(b)
	return a, nil
}

func (pipeTransport) String() string { return "pipe" }

// pipeLink is a link from own to the peer key, over a pipe to far.
func pipeLink(t *testing.T, own *Identity, peer ed25519.PublicKey, far func(net.Conn)) (*link, context.CancelFunc) {
	t.Helper()
	cert, err := certificate(own)
	if err != nil {
		t.Fatal(err)
	}
	l := &link{
		peer: Peer{Alias: "far", Key: peer}, id: own, cert: cert,
		t:       pipeTransport{far},
		hello:   Hello{LinkProtocol: LinkProtocol, Version: "1", Caps: Caps},
		logf:    t.Logf,
		backoff: func(int) time.Duration { return time.Hour },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return l, cancel
}

// farEnd plays the peer paired as as, with key k (an impostor's when
// they differ): TLS pinning want, then a hello.
func farEnd(own *Identity, as ed25519.PublicKey, k ed25519.PrivateKey, want ed25519.PublicKey, h Hello) func(net.Conn) {
	return func(c net.Conn) {
		defer c.Close()
		cert, _ := certificate(newIdentity(k))
		cfg := tlsConfig(cert, want)
		var tc *tls.Conn
		if tlsClient(IDOf(as), own.ID) {
			tc = tls.Client(c, cfg)
		} else {
			tc = tls.Server(c, cfg)
		}
		if tc.Handshake() != nil {
			return
		}
		s := newSession(tc)
		s.hello(h)
		s.serve(func(time.Duration) {})
	}
}

func waitState(t *testing.T, l *link, what string, ok func(proto.PeerLink) bool) proto.PeerLink {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p := l.status(); ok(p) {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never %s; last %+v", what, l.status())
	return proto.PeerLink{}
}

func TestPinnedKeyRefusesAnother(t *testing.T) {
	own := newServer(t).id
	paired, impostor := newKey(t), newKey(t)
	l, _ := pipeLink(t, own, pub(paired), farEnd(own, pub(paired), impostor, pub(own.Key), Hello{LinkProtocol: LinkProtocol, Version: "1"}))
	waitState(t, l, "refused", func(p proto.PeerLink) bool {
		return p.State == proto.LinkOffline && p.Reason == proto.LinkReasonRefused
	})
}

func TestPeerRefusesOurKey(t *testing.T) {
	own := newServer(t).id
	paired := newKey(t)
	// The far end has another key pinned for us.
	l, _ := pipeLink(t, own, pub(paired), farEnd(own, pub(paired), paired, pub(newKey(t)), Hello{LinkProtocol: LinkProtocol, Version: "1"}))
	waitState(t, l, "refused", func(p proto.PeerLink) bool {
		return p.State == proto.LinkOffline && p.Reason == proto.LinkReasonRefused
	})
}

func TestOldLinkProtocol(t *testing.T) {
	own := newServer(t).id
	paired := newKey(t)
	l, _ := pipeLink(t, own, pub(paired), farEnd(own, pub(paired), paired, pub(own.Key), Hello{LinkProtocol: 0, Version: "0.1"}))
	p := waitState(t, l, "offline · version", func(p proto.PeerLink) bool {
		return p.State == proto.LinkOffline && p.Reason == proto.LinkReasonVersion
	})
	if p.Version != "0.1" || !strings.Contains(p.Error, "tm 0.1") {
		t.Errorf("version refusal: %+v", p)
	}
}

func TestNewerPeer(t *testing.T) {
	own := newServer(t).id
	paired := newKey(t)
	l, _ := pipeLink(t, own, pub(paired), farEnd(own, pub(paired), paired, pub(own.Key), Hello{LinkProtocol: LinkProtocol + 1, Version: "9", Caps: []string{"watch"}}))
	p := waitState(t, l, "up", isUp)
	if p.LinkProtocol != LinkProtocol+1 || len(p.Caps) != 1 || p.Caps[0] != "watch" {
		t.Errorf("a newer peer's hello: %+v", p)
	}
}

func TestSessionUnknownMethodAndIdle(t *testing.T) {
	a, b, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	sa, sb := newSession(a), newSession(b)
	sa.ping, sb.ping = time.Hour, time.Hour
	sb.idle = 200 * time.Millisecond
	ended := make(chan error, 1)
	go sa.serve(func(time.Duration) {})
	go func() { ended <- sb.serve(func(time.Duration) {}) }()
	err = sa.call(context.Background(), "peer.nothing", nil, nil)
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != proto.ErrUnknownMethod {
		t.Fatalf("an unknown method: %v", err)
	}
	// sa pings hourly, so sb hears nothing and gives up.
	select {
	case err := <-ended:
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			t.Errorf("idle end: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an idle session never ended")
	}
	a.Close()
}

// slowResult holds its unmarshal until release closes, so a call is
// still taking its response when more copies of it arrive; entered
// closes once the call has its response.
type slowResult struct{ entered, release chan struct{} }

func (r *slowResult) UnmarshalJSON([]byte) error {
	close(r.entered)
	<-r.release
	return nil
}

func TestSessionDuplicateResponses(t *testing.T) {
	a, b, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	s := newSession(a)
	s.ping = time.Hour
	ended := make(chan error, 1)
	go func() { ended <- s.serve(func(time.Duration) {}) }()
	// The far end answers the request four times; serve's first ping
	// may come before it.
	go func() {
		d := json.NewDecoder(b)
		var l line
		for l.Method != "peer.x" {
			l = line{}
			if err := d.Decode(&l); err != nil {
				return
			}
		}
		r, _ := json.Marshal(line{ID: l.ID, Result: json.RawMessage(`{"ok":true}`)})
		for range 4 {
			b.Write(append(r, '\n'))
		}
	}()
	res := &slowResult{entered: make(chan struct{}), release: make(chan struct{})}
	called := make(chan error, 1)
	go func() { called <- s.call(context.Background(), "peer.x", nil, res) }()
	// Give the copies time to arrive while the call is unmarshalling.
	select {
	case <-res.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never got its response")
	}
	time.Sleep(200 * time.Millisecond)
	a.Close()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("serve never returned after the conn closed: duplicate responses blocked the reader")
	}
	close(res.release)
	if err := <-called; err != nil {
		t.Errorf("the call: %v", err)
	}
}

// A link's room is its two keys' (relay.LinkRoom), whatever form the
// relay's URL takes: a domain relay with a CA's certificate (no pin), a
// self-signed one with its pin, a port, a path (T809). The phone has the
// same vector (ios/TerminatrLink LinkTests linkRoomMatchesGo).
func TestRelayTransportRoomIsTheKeys(t *testing.T) {
	own := newIdentity(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32)))
	other := IDOf(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32)).Public().(ed25519.PublicKey))
	for _, u := range []string{
		"wss://terminatr-relay.example.com",
		"wss://terminatr-relay.example.com/",
		"wss://terminatr-relay.example.com:443/v1",
		"wss://relay.example.net#sha256=" + strings.Repeat("0f", 32),
		"ws://127.0.0.1:8080",
	} {
		tr := RelayTransport(u, own, other, nil).(*relayTransport)
		if got := fmt.Sprintf("%x", tr.d.Room); got != "22ed17da5164dceb7740c36d584c64b5a797ca9c6c7c2e7b9d8840eb5d1c152b" {
			t.Errorf("%s: room %s", u, got)
		}
	}
}
