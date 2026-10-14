package relay

import (
	"crypto"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// A relay without a CA-signed certificate, on an IP address, is pinned
// (docs/SPEC.md §16.2): its URL ends in #sha256=<pin>, the hex SHA-256 of
// its certificate's public key (SubjectPublicKeyInfo, as HPKP and
// openssl pkey -pubin -outform der | sha256sum have it). A client checks
// that key and nothing else: no CA, no names, no dates. A certificate
// issued again for the same key keeps the pin.

// PinPrefix starts a pinned relay URL's fragment.
const PinPrefix = "sha256="

// KeyPin is the pin of a public key.
func KeyPin(pub crypto.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

// CertPin is the pin of a certificate's public key.
func CertPin(c *x509.Certificate) string {
	sum := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// ParsePin reads a URL fragment: "" for none, else sha256= and 64 hex
// digits, given back in lower case.
func ParsePin(fragment string) (string, error) {
	if fragment == "" {
		return "", nil
	}
	h, ok := strings.CutPrefix(strings.ToLower(fragment), PinPrefix)
	if b, err := hex.DecodeString(h); !ok || err != nil || len(b) != sha256.Size {
		return "", fmt.Errorf("#%s: a relay's pin is #sha256= and 64 hex digits", fragment)
	}
	return h, nil
}

// SplitPin is rawURL without its fragment, and the pin the fragment
// holds ("" when none).
func SplitPin(rawURL string) (string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	pin, err := ParsePin(u.Fragment)
	if err != nil {
		return "", "", err
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String(), pin, nil
}

// PinError is a relay whose key isn't the pinned one: it was set up
// again with a new key, or something else answers at its address.
type PinError struct {
	Host      string
	Want, Got string
}

func (e *PinError) Error() string {
	return fmt.Sprintf("the relay at %s has a different key than when you paired (pinned sha256=%s…, it shows sha256=%s…): "+
		"if that relay was set up again, give both servers its new address with tm peer relay ALIAS ADDRESS; else something else answers there",
		e.Host, e.Want[:16], e.Got[:16])
}

// PinnedTLS is the client TLS that takes a relay at host by its key's
// pin alone. base, when not nil, gives the other settings.
func PinnedTLS(base *tls.Config, host, pin string) *tls.Config {
	cfg := base.Clone()
	if cfg == nil {
		cfg = &tls.Config{}
	}
	cfg.RootCAs = nil
	cfg.InsecureSkipVerify = true // VerifyConnection checks the key instead
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("relay: no certificate")
		}
		got := CertPin(cs.PeerCertificates[0])
		if subtle.ConstantTimeCompare([]byte(got), []byte(pin)) != 1 {
			return &PinError{Host: host, Want: pin, Got: got}
		}
		return nil
	}
	if cfg.MinVersion == 0 {
		cfg.MinVersion = tls.VersionTLS12
	}
	return cfg
}

// cgnat is 100.64.0.0/10, shared address space: carrier NAT, and the
// addresses Tailscale gives.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// PrivateWarning says, for a relay on a private address (a LAN, a VPN
// or tailnet), that only servers on that network reach it; "" for any
// other relay, loopback too (a test's).
func PrivateWarning(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil {
		return ""
	}
	ip = ip.Unmap()
	if !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !cgnat.Contains(ip) {
		return ""
	}
	return fmt.Sprintf("%s is a private address: pairing and links through this relay only work where that network reaches (the same LAN, VPN or tailnet)", ip)
}
