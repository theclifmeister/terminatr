package peer

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"time"
)

// errNotPinned: the other end's TLS key isn't the peer's pinned key.
var errNotPinned = errors.New("the other end's key is not the paired key")

// certificate is a self-signed certificate for id's key. Nobody checks
// its name or dates: a peer pins the key itself, so clock skew or an old
// certificate never breaks a link.
func certificate(id *Identity) (tls.Certificate, error) {
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tm server " + id.ID.Short()},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, id.Key.Public(), id.Key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: id.Key}, nil
}

// pinned accepts exactly one certificate, whose key is want. The TLS
// handshake has already proved the other end holds that certificate's
// private key.
func pinned(want ed25519.PublicKey) func([][]byte, [][]*x509.Certificate) error {
	return func(raw [][]byte, _ [][]*x509.Certificate) error {
		if len(raw) != 1 {
			return errNotPinned
		}
		c, err := x509.ParseCertificate(raw[0])
		if err != nil {
			return errNotPinned
		}
		if k, ok := c.PublicKey.(ed25519.PublicKey); !ok || !k.Equal(want) {
			return errNotPinned
		}
		return nil
	}
}

// tlsClient says which end of a link is the TLS client: the one with the
// lower server id. Both ends work it out alike, whichever dialled first.
func tlsClient(own, peer ID) bool { return bytes.Compare(own[:], peer[:]) < 0 }

// tlsConfig is the link's TLS: 1.3 only, each end presenting its own
// key and accepting only the other's pinned key, never a CA's. No
// session resumption: every connection proves both keys again.
func tlsConfig(cert tls.Certificate, peer ed25519.PublicKey) *tls.Config {
	return &tls.Config{
		MinVersion:             tls.VersionTLS13,
		MaxVersion:             tls.VersionTLS13,
		Certificates:           []tls.Certificate{cert},
		InsecureSkipVerify:     true, // the pin below is the verification
		VerifyPeerCertificate:  pinned(peer),
		ClientAuth:             tls.RequireAnyClientCert,
		SessionTicketsDisabled: true,
	}
}

// LinkTLS is the TLS of a link between id and the key other, as a
// server's link to a peer has it: for a device's link (docs/SPEC.md
// §16.15). client says which end is the TLS client.
func LinkTLS(id *Identity, other ed25519.PublicKey) (cfg *tls.Config, client bool, err error) {
	cert, err := certificate(id)
	if err != nil {
		return nil, false, err
	}
	return tlsConfig(cert, other), tlsClient(id.ID, IDOf(other)), nil
}
