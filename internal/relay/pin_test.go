package relay

import (
	"crypto/tls"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The key stays; the certificate is issued again, with the same key and
// so the same pin, when it nears its end or lacks an address.
func TestSelfSigned(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	ip := netip.MustParseAddr("203.0.113.5")
	certFile, keyFile, pin, err := SelfSigned(dir, []netip.Addr{ip}, now)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(keyFile); fi.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v", fi.Mode())
	}
	c, err := loadCert(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if CertPin(c) != pin || len(pin) != 64 || c.NotAfter.Before(now.Add(9*365*24*time.Hour)) || len(c.IPAddresses) != 1 || !c.IPAddresses[0].Equal(ip.AsSlice()) {
		t.Fatalf("cert %v %v %v", c.NotAfter, c.IPAddresses, pin)
	}
	if p, err := SelfSignedPin(dir); err != nil || p != pin {
		t.Fatalf("SelfSignedPin %q %v", p, err)
	}
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		t.Fatal(err)
	}
	same := func(at time.Time, ips ...netip.Addr) *time.Time {
		t.Helper()
		_, _, p, err := SelfSigned(dir, ips, at)
		if err != nil || p != pin {
			t.Fatalf("pin %q, %v; want %q", p, err, pin)
		}
		c, _ := loadCert(certFile)
		return &c.NotAfter
	}
	first := c.NotAfter
	if got := same(now.Add(time.Hour), ip); !got.Equal(first) {
		t.Error("issued again with nothing to change")
	}
	other := netip.MustParseAddr("100.101.102.103")
	if got := same(now.Add(2*time.Hour), ip, other); got.Equal(first) {
		t.Error("not issued again for another address")
	}
	if got := same(now.Add(9*365*24*time.Hour+200*24*time.Hour), ip, other); !got.After(now.Add(18 * 365 * 24 * time.Hour)) {
		t.Error("not issued again near its end")
	}
	// A broken key is an error, never a new key.
	os.WriteFile(keyFile, []byte("nope"), 0o600)
	if _, _, _, err := SelfSigned(dir, []netip.Addr{ip}, now); err == nil {
		t.Fatal("made a new key over a broken one")
	}
}

func TestParsePin(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	for in, want := range map[string]string{"": "", "sha256=" + pin: pin, "SHA256=" + strings.ToUpper(pin): pin} {
		if got, err := ParsePin(in); err != nil || got != want {
			t.Errorf("ParsePin(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"sha256=", "sha256=" + pin[:62], "sha1=" + pin, pin, "sha256=" + pin + "00"} {
		if _, err := ParsePin(bad); err == nil {
			t.Errorf("ParsePin(%q) took it", bad)
		}
	}
	if u, p, err := SplitPin("wss://203.0.113.5:8443#sha256=" + pin); err != nil || u != "wss://203.0.113.5:8443" || p != pin {
		t.Errorf("SplitPin %q %q %v", u, p, err)
	}
}

func TestPrivateWarning(t *testing.T) {
	for in, private := range map[string]bool{
		"wss://192.168.1.20#sha256=x": true, "wss://10.0.0.2": true, "wss://100.101.102.103": true, "wss://127.0.0.1:8443": false,
		"wss://[fd7a:115c:a1e0::1]": true, "wss://203.0.113.5": false, "wss://relay.example.net": false, "wss://[2001:db8::7]": false,
	} {
		if got := PrivateWarning(in); (got != "") != private {
			t.Errorf("PrivateWarning(%q) = %q", in, got)
		}
	}
}
