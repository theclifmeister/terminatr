package doctor

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

func peerLine(t *testing.T, cs []Check, alias string) Check {
	t.Helper()
	got := find(cs, "peer "+alias)
	if len(got) != 1 {
		t.Fatalf("want one line for %s, got %+v", alias, cs)
	}
	return got[0]
}

func TestPeers(t *testing.T) {
	d := testDeps(t)
	d.Version = "v0.27.0"
	if cs := Peers(d, Live{}); cs != nil {
		t.Fatalf("never paired: %+v", cs)
	}
	dir := peer.DirIn(d.Paths.Home)
	id, _, err := peer.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cs := Peers(d, Live{}); len(cs) != 1 || cs[0].Status != OK || !strings.Contains(cs[0].Detail, "no peers") {
		t.Fatalf("a key, no peers: %+v", cs)
	}
	k1, _, _ := ed25519.GenerateKey(nil)
	k2, _, _ := ed25519.GenerateKey(nil)
	ps := []peer.Peer{{Alias: "mini", Key: k1, Relay: "wss://r.example.net", Panes: "type"}, {Alias: "nas", Key: k2, Relay: "wss://r.example.net"}}
	if err := peer.SavePeers(dir, ps); err != nil {
		t.Fatal(err)
	}
	if cs := Peers(d, Live{}); len(cs) != 1 || cs[0].Status != OK || !strings.Contains(cs[0].Detail, "2 paired; the server links them when it runs") {
		t.Fatalf("server stopped: %+v", cs)
	}
	seen := time.Now().Add(-3 * time.Hour)
	live := Live{Running: true,
		Link: &proto.LinkStatus{ID: id.ID.String(), Peers: []proto.PeerLink{
			{Alias: "mini", ID: ps[0].ID().String(), State: proto.LinkUp, RTT: 12.4, Version: "v0.27.0"},
			{Alias: "nas", ID: ps[1].ID().String(), State: proto.LinkOffline, Reason: proto.LinkReasonPeer, Since: seen, Version: "v0.27.0"},
		}},
		Peers: &proto.PeerList{Peers: []proto.PeerMirror{
			{Alias: "mini", ID: ps[0].ID().String(), Panes: proto.PanesView, Projects: []proto.PeerMirrorProject{{}, {}}},
		}}}

	t.Run("up and asleep", func(t *testing.T) {
		cs := Peers(d, live)
		mini := peerLine(t, cs, "mini")
		if mini.Status != OK || !strings.Contains(mini.Detail, "up, 12 ms") || !strings.Contains(mini.Detail, "tm v0.27.0") ||
			!strings.Contains(mini.Detail, "they may write ours, we may view theirs") || !strings.Contains(mini.Detail, "2 from them") ||
			!strings.Contains(mini.Detail, "no messages waiting") {
			t.Fatalf("up: %+v", mini)
		}
		if p := mini.Peer; p == nil || p.LatencyMS != 12.4 || p.PanesMine != "write" || p.PanesTheirs != "view" || p.SharedIn == nil || *p.SharedIn != 2 {
			t.Fatalf("up fields: %+v", mini.Peer)
		}
		nas := peerLine(t, cs, "nas")
		if nas.Status != Warn || !strings.Contains(nas.Detail, "offline (peer), last seen 3 h ago") || !strings.Contains(nas.Detail, "they may off ours") {
			t.Fatalf("offline: %+v", nas)
		}
		if nas.Peer.SharedIn != nil || nas.Peer.PanesTheirs != "" {
			t.Fatalf("unknown stays unknown: %+v", nas.Peer)
		}
	})
	t.Run("refused", func(t *testing.T) {
		l := *live.Link
		l.Peers = append([]proto.PeerLink(nil), l.Peers...)
		l.Peers[1].Reason, l.Peers[1].Error = proto.LinkReasonRefused, "the other end's key is not the paired key"
		lv := live
		lv.Link = &l
		nas := peerLine(t, Peers(d, lv), "nas")
		if nas.Status != Warn || !strings.Contains(nas.Detail, "offline (refused)") || !strings.Contains(nas.Detail, "the other end's key") {
			t.Fatalf("refused: %+v", nas)
		}
	})
	t.Run("never seen", func(t *testing.T) {
		l := *live.Link
		l.Peers = append([]proto.PeerLink(nil), l.Peers...)
		l.Peers[1].Version = ""
		lv := live
		lv.Link = &l
		if nas := peerLine(t, Peers(d, lv), "nas"); nas.Status != Warn || !strings.Contains(nas.Detail, "not seen since the server started") {
			t.Fatalf("never seen: %+v", nas)
		}
	})
	t.Run("version skew", func(t *testing.T) {
		l := *live.Link
		l.Peers = append([]proto.PeerLink(nil), l.Peers...)
		l.Peers[0].Version = "v0.24.1"
		lv := live
		lv.Link = &l
		if mini := peerLine(t, Peers(d, lv), "mini"); mini.Status != Warn || !mini.Peer.Skew || !strings.Contains(mini.Detail, "far from this tm") {
			t.Fatalf("skew: %+v", mini)
		}
		l.Peers[0].Version = "v0.26.0" // one minor apart is no skew
		if mini := peerLine(t, Peers(d, lv), "mini"); mini.Status != OK || mini.Peer.Skew {
			t.Fatalf("near: %+v", mini)
		}
	})
	t.Run("queued", func(t *testing.T) {
		q := filepath.Join(d.Paths.Home, "state", "link", "outbox", "mini")
		os.MkdirAll(q, 0o700)
		os.WriteFile(filepath.Join(q, "0000000001.json"), []byte("{}"), 0o600)
		mini := peerLine(t, Peers(d, live), "mini")
		if mini.Status != OK || mini.Peer.Queued != 1 || !strings.Contains(mini.Detail, "messages: 1 queued, 0 undelivered") {
			t.Fatalf("queued: %+v", mini)
		}
	})
	t.Run("undelivered", func(t *testing.T) {
		c := peerCheck(PeerHealth{Alias: "mini", State: proto.LinkUp, PanesMine: "off", Undelivered: 2})
		if c.Status != Warn || !strings.Contains(c.Detail, "2 undelivered") {
			t.Fatalf("undelivered: %+v", c)
		}
	})
	t.Run("broken peers.json", func(t *testing.T) {
		os.WriteFile(filepath.Join(dir, peer.PeersFile), []byte("{"), 0o600)
		if cs := Peers(d, live); len(cs) != 1 || cs[0].Status != Fail {
			t.Fatalf("broken peers.json: %+v", cs)
		}
	})
}

func TestSkewed(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v0.27.0", "v0.27.5", false}, {"v0.27.0", "v0.26.0", false}, {"v0.27.0", "v0.25.9", true}, {"v0.25.0", "v0.27.0", true},
		{"v1.0.0", "v0.27.0", true}, {"v0.27.0", "", false}, {"v0.27.0-rc1", "v0.27.0", false}, {"dev", "v0.1.0", false}} {
		if got := skewed(c.a, c.b); got != c.want {
			t.Errorf("skewed(%q,%q) = %v", c.a, c.b, got)
		}
	}
}

// relayServer runs an in-process relay behind TLS.
func relayServer(t *testing.T, cfg relay.Config) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(relay.New(cfg))
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return ts
}

func probeFor(ts *httptest.Server) *RelayProbe {
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	return &RelayProbe{TLS: &tls.Config{RootCAs: pool}, Timeout: 2 * time.Second}
}

func wss(ts *httptest.Server) string { return "wss://" + strings.TrimPrefix(ts.URL, "https://") }

func TestRelayChecks(t *testing.T) {
	cfg := relay.DefaultConfig()
	cfg.Version, cfg.AutoUpdate = "v0.26.0", "on"
	ts := relayServer(t, cfg)
	d := testDeps(t)
	d.Version = "v0.27.0"

	t.Run("CA certificate, old relay", func(t *testing.T) {
		d.Relay = probeFor(ts)
		h := probeRelay(d.Relay, wss(ts))
		if !h.Reachable || h.CertDays == nil || *h.CertDays < CertWarnDays || h.Version != "v0.26.0" || h.AutoUpdate != "on" {
			t.Fatalf("probe: %+v", h)
		}
		c := relayCheck(withUpdate(h))
		if c.Status != Warn || !strings.Contains(c.Detail, "update available") || !strings.Contains(c.Detail, "auto-update on") {
			t.Fatalf("old relay: %+v", c)
		}
	})
	t.Run("current relay", func(t *testing.T) {
		h := probeRelay(probeFor(ts), wss(ts))
		if c := relayCheck(h); c.Status != OK || !strings.Contains(c.Detail, "certificate good for") {
			t.Fatalf("current: %+v", c)
		}
	})
	t.Run("not trusted is unreachable", func(t *testing.T) {
		h := probeRelay(&RelayProbe{Timeout: 2 * time.Second}, wss(ts))
		if c := relayCheck(h); h.Reachable || c.Status != Warn || !strings.Contains(c.Detail, "not reachable") {
			t.Fatalf("untrusted: %+v %+v", h, c)
		}
	})
	t.Run("pin match and mismatch", func(t *testing.T) {
		pin := relay.CertPin(ts.Certificate())
		h := probeRelay(&RelayProbe{Timeout: 2 * time.Second}, wss(ts)+"#sha256="+pin)
		if c := relayCheck(h); !h.Reachable || h.PinMatch == nil || !*h.PinMatch || h.CertDays != nil || c.Status != OK || !strings.Contains(c.Detail, "pin matches") {
			t.Fatalf("pin: %+v %+v", h, c)
		}
		h = probeRelay(&RelayProbe{Timeout: 2 * time.Second}, wss(ts)+"#sha256="+strings.Repeat("ab", 32))
		if c := relayCheck(h); h.Reachable || h.PinMatch == nil || *h.PinMatch || c.Status != Fail || !strings.Contains(c.Detail, "different key") {
			t.Fatalf("mismatch: %+v %+v", h, c)
		}
	})
	t.Run("down", func(t *testing.T) {
		dead := httptest.NewTLSServer(http.NotFoundHandler())
		url := wss(dead)
		dead.Close()
		h := probeRelay(&RelayProbe{Timeout: time.Second}, url)
		if c := relayCheck(h); h.Reachable || c.Status != Warn {
			t.Fatalf("down: %+v %+v", h, c)
		}
	})
	t.Run("certificate near expiry", func(t *testing.T) {
		for _, days := range []int{5, -1} {
			n := days
			if c := relayCheck(RelayHealth{URL: "r", Reachable: true, CertDays: &n}); c.Status != Warn {
				t.Fatalf("%d days: %+v", days, c)
			}
		}
		n := 14
		if c := relayCheck(RelayHealth{URL: "r", Reachable: true, CertDays: &n}); c.Status != OK {
			t.Fatalf("14 days: %+v", c)
		}
	})
	t.Run("short-lived certificate is read", func(t *testing.T) {
		short := httptest.NewUnstartedServer(relay.New(cfg))
		short.TLS = &tls.Config{Certificates: []tls.Certificate{cert(t, 5*24*time.Hour)}}
		short.StartTLS()
		defer short.Close()
		h := probeRelay(&RelayProbe{TLS: &tls.Config{InsecureSkipVerify: true}, Timeout: 2 * time.Second}, wss(short))
		if h.CertDays == nil || *h.CertDays < 4 || *h.CertDays > 5 || relayCheck(h).Status != Warn {
			t.Fatalf("short: %+v", h)
		}
	})
	t.Run("through Peers, grouped, with the link's relay version", func(t *testing.T) {
		d.Relay = probeFor(ts)
		dir := peer.DirIn(d.Paths.Home)
		id, _, _ := peer.EnsureIdentity(dir)
		k1, _, _ := ed25519.GenerateKey(nil)
		k2, _, _ := ed25519.GenerateKey(nil)
		ps := []peer.Peer{{Alias: "a", Key: k1, Relay: wss(ts)}, {Alias: "b", Key: k2, Relay: wss(ts)}}
		if err := peer.SavePeers(dir, ps); err != nil {
			t.Fatal(err)
		}
		cs := Peers(d, Live{Running: true, Link: &proto.LinkStatus{ID: id.ID.String()}})
		rs := 0
		for _, c := range cs {
			if c.Relay != nil {
				rs++
				if len(c.Relay.Peers) != 2 || !c.Relay.UpdateAvailable || c.Status != Warn {
					t.Fatalf("relay line: %+v", c)
				}
			}
		}
		if rs != 1 {
			t.Fatalf("want one relay line: %+v", cs)
		}
		d.Relay = nil
		for _, c := range Peers(d, Live{}) {
			if c.Relay != nil {
				t.Fatalf("no probe without Deps.Relay: %+v", c)
			}
		}
	})
}

func withUpdate(h RelayHealth) RelayHealth {
	h.UpdateAvailable = true
	return h
}

// cert is a self-signed localhost certificate that lasts ttl.
func cert(t *testing.T, ttl time.Duration) tls.Certificate {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(ttl), IPAddresses: nil}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}
