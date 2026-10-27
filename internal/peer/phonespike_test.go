package peer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// The Go end of the phone link spike (T786, ios/TerminatrLink): a
// server's real link (RelayTransport, pinned TLS, session) to a phone
// that runs TerminatrLink. Both skip unless TM_PHONE_SPIKE_DIR is set;
// ios/TerminatrLink/spike/run.sh sets it. The relay is
// TM_PHONE_SPIKE_RELAY, or else tm relay serve --self-signed's own code
// run here on a loopback port. What the phone needs goes through the
// directory: relay.url, server.pub from this end, phone.pub from the
// phone, keys as 64 hex digits.

func spikeEnv(t *testing.T) (dir, relayURL string) {
	dir, relayURL = os.Getenv("TM_PHONE_SPIKE_DIR"), os.Getenv("TM_PHONE_SPIKE_RELAY")
	if dir == "" {
		t.Skip("TM_PHONE_SPIKE_DIR not set")
	}
	if relayURL == "" {
		cert, key, pin, err := relay.SelfSigned(filepath.Join(dir, "relay"), []netip.Addr{netip.MustParseAddr("127.0.0.1")}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cfg := relay.DefaultConfig()
		cfg.Log = log.New(os.Stderr, "relay: ", log.Lmicroseconds)
		go relay.Serve(ctx, ln, relay.New(cfg), relay.TLS{CertFile: cert, KeyFile: key})
		t.Cleanup(cancel)
		relayURL = "wss://" + ln.Addr().String() + "#" + relay.PinPrefix + pin
	}
	if err := os.WriteFile(filepath.Join(dir, "relay.url"), []byte(relayURL), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("relay: %s", relayURL)
	return dir, relayURL
}

// spikeKeys writes own's key to dir/name and waits for the phone's,
// which it may choose after reading own's (to take either TLS role).
func spikeKeys(t *testing.T, dir, name string, own ed25519.PublicKey) ed25519.PublicKey {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(hex.EncodeToString(own)), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(dir, "phone.pub")); err == nil {
			k, err := hex.DecodeString(strings.TrimSpace(string(b)))
			if err != nil || len(k) != ed25519.PublicKeySize {
				t.Fatalf("phone.pub: %q", b)
			}
			return ed25519.PublicKey(k)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("no phone.pub")
	return nil
}

// TestPhoneSpikeLink: the phone joins the link room, the TLS pins both
// keys, the hellos pass, the server pings the phone and answers its
// spike.echo, and the link ends when the phone leaves.
func TestPhoneSpikeLink(t *testing.T) {
	dir, relayURL := spikeEnv(t)
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	own := newIdentity(k)
	phone := spikeKeys(t, dir, "server.pub", pub(own.Key))
	cert, err := certificate(own)
	if err != nil {
		t.Fatal(err)
	}
	echoed := make(chan string, 1)
	l := &link{
		peer: Peer{Alias: "phone", Key: phone}, id: own, cert: cert,
		t:     RelayTransport(relayURL, own, IDOf(phone), nil),
		hello: Hello{LinkProtocol: LinkProtocol, Version: "spike", Caps: Caps},
		methods: map[string]handler{"spike.echo": func(_ context.Context, p json.RawMessage) (any, error) {
			echoed <- string(p)
			return p, nil
		}},
		logf:    t.Logf,
		backoff: func(int) time.Duration { return time.Hour },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	st := waitStateFor(t, l, 3*time.Minute, "up", func(p proto.PeerLink) bool { return p.State == proto.LinkUp })
	t.Logf("link up: the phone says tm %q, link protocol %d, relay %q", st.Version, st.LinkProtocol, st.RelayVersion)
	select {
	case p := <-echoed:
		t.Logf("spike.echo from the phone: %s", p)
	case <-time.After(30 * time.Second):
		t.Fatal("no spike.echo from the phone")
	}
	st = waitStateFor(t, l, 30*time.Second, "pinged", func(p proto.PeerLink) bool { return p.RTT > 0 })
	t.Logf("the phone answered link.ping: %.2f ms", st.RTT)
	st = waitStateFor(t, l, 30*time.Second, "offline", func(p proto.PeerLink) bool { return p.State == proto.LinkOffline })
	t.Logf("the phone left: %s (%s)", st.Reason, st.Error)
}

// TestPhoneSpikeImpostor: an end that joins the room with the paired
// key but shows another in TLS; the phone must refuse it.
func TestPhoneSpikeImpostor(t *testing.T) {
	dir, relayURL := spikeEnv(t)
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	own := newIdentity(k)
	phone := spikeKeys(t, dir, "server.pub", pub(own.Key))
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	cert, err := certificate(newIdentity(other))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	d := relay.Dialer{Key: own.Key, Kind: relay.KindLink, Room: relay.LinkRoom(own.ID, IDOf(phone)), Peer: IDOf(phone)}
	c, err := d.Dial(ctx, relayURL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cfg := tlsConfig(cert, phone)
	var tc *tls.Conn
	if tlsClient(own.ID, IDOf(phone)) {
		tc = tls.Client(c, cfg)
	} else {
		tc = tls.Server(c, cfg)
	}
	hctx, hcancel := context.WithTimeout(ctx, helloTimeout)
	defer hcancel()
	err = tc.HandshakeContext(hctx)
	if err == nil {
		// TLS 1.3: the client finishes before the server checks it.
		_, err = tc.Read(make([]byte, 1))
	}
	if err == nil {
		t.Fatal("the phone took an impostor's key")
	}
	t.Logf("the phone refused the impostor: %v", err)
}

func waitStateFor(t *testing.T, l *link, d time.Duration, what string, ok func(proto.PeerLink) bool) proto.PeerLink {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if p := l.status(); ok(p) {
			return p
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never %s; last %+v", what, l.status())
	return proto.PeerLink{}
}
