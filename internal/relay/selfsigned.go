package relay

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// A self-signed relay keeps its key and certificate in its state
// directory: the key for good (its pin is in every paired server's
// peers.json), the certificate issued again with that key when it nears
// its end or lacks an address.
const (
	SelfSignedKey  = "self-signed.key"
	SelfSignedCert = "self-signed.crt"
)

// selfSignedLife is a certificate's life; nobody checks its dates, but
// tools that look at it see a sane one.
const selfSignedLife = 10 * 365 * 24 * time.Hour

// SelfSigned makes sure dir holds a key and a certificate for it
// naming ips, keeping the key there is, and returns their paths and the
// key's pin.
func SelfSigned(dir string, ips []netip.Addr, now time.Time) (certFile, keyFile, pin string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", "", err
	}
	keyFile, certFile = filepath.Join(dir, SelfSignedKey), filepath.Join(dir, SelfSignedCert)
	key, err := loadKey(keyFile)
	if errors.Is(err, fs.ErrNotExist) {
		if key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err == nil {
			err = writeKey(keyFile, key)
		}
	}
	if err != nil {
		return "", "", "", err
	}
	if pin, err = KeyPin(key.Public()); err != nil {
		return "", "", "", err
	}
	if c, err := loadCert(certFile); err == nil && certFits(c, pin, ips, now) {
		return certFile, keyFile, pin, nil
	}
	der, err := issue(key, ips, now)
	if err != nil {
		return "", "", "", err
	}
	if err := writeFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", "", err
	}
	return certFile, keyFile, pin, nil
}

// SelfSignedPin is the pin of the key in dir.
func SelfSignedPin(dir string) (string, error) {
	key, err := loadKey(filepath.Join(dir, SelfSignedKey))
	if err != nil {
		return "", err
	}
	return KeyPin(key.Public())
}

// maxStateFile bounds a key or certificate file read from the state
// directory; each is well under 2 KiB.
const maxStateFile = 64 << 10

// readRegular reads path unless it is a link or other non-regular file,
// or over maxStateFile: the state directory belongs to the relay's user,
// so root reading from it follows no link planted there.
func readRegular(path string) ([]byte, error) {
	li, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !li.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !os.SameFile(li, fi) {
		return nil, fmt.Errorf("%s changed while it was opened", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxStateFile+1))
	if err == nil && len(b) > maxStateFile {
		return nil, fmt.Errorf("%s is over %d bytes", path, maxStateFile)
	}
	return b, err
}

func loadKey(path string) (*ecdsa.PrivateKey, error) {
	b, err := readRegular(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("%s: no PEM key", path)
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an ECDSA key", path)
	}
	return ek, nil
}

func writeKey(path string, k *ecdsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return err
	}
	return writeFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
}

func loadCert(path string) (*x509.Certificate, error) {
	b, err := readRegular(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("%s: no PEM certificate", path)
	}
	return x509.ParseCertificate(blk.Bytes)
}

// certFits: c is for the key pinned, names every one of ips and has more
// than a year to go.
func certFits(c *x509.Certificate, pin string, ips []netip.Addr, now time.Time) bool {
	if CertPin(c) != pin || now.Add(365*24*time.Hour).After(c.NotAfter) {
		return false
	}
	for _, ip := range ips {
		if !slices.ContainsFunc(c.IPAddresses, func(a net.IP) bool {
			got, ok := netip.AddrFromSlice(a)
			return ok && got.Unmap() == ip.Unmap()
		}) {
			return false
		}
	}
	return true
}

func issue(key *ecdsa.PrivateKey, ips []netip.Addr, now time.Time) ([]byte, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "tm relay"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedLife),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, ip := range ips {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip.AsSlice())
	}
	return x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
}

// writeFile writes a new file next to path and renames it over.
func writeFile(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tm-relay-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Chmod(mode)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
