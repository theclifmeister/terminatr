package relay

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The certificate in the state directory is read as the key is: no
// link, nothing but a regular file, and a bounded size.
func TestLoadCertRefusesLinksAndBigFiles(t *testing.T) {
	dir := t.TempDir()
	ips := []netip.Addr{netip.MustParseAddr("192.0.2.1")}
	certFile, _, _, err := SelfSigned(dir, ips, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadCert(certFile); err != nil {
		t.Fatalf("the certificate SelfSigned wrote: %v", err)
	}

	elsewhere := filepath.Join(t.TempDir(), "other.crt")
	b, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(elsewhere, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(certFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, certFile); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCert(certFile); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a link: err = %v, want not a regular file", err)
	}
	// SelfSigned writes a fresh certificate over the link, not through it.
	if _, _, _, err := SelfSigned(dir, ips, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(elsewhere); !bytes.Equal(got, b) {
		t.Fatal("SelfSigned wrote through the link")
	}

	big := append(bytes.Repeat([]byte("\n"), maxStateFile), b...)
	if err := os.WriteFile(certFile, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCert(certFile); err == nil || !strings.Contains(err.Error(), "over") {
		t.Fatalf("a big file: err = %v, want over the bound", err)
	}
	if _, err := readRegular(certFile); err == nil {
		t.Fatal("readRegular read a file over maxStateFile")
	}
}
