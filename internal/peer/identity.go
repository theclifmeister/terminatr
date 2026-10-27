// Package peer joins this tm server to the servers it is paired with
// (docs/SPEC.md §16.7). Each server has an Ed25519 identity key; its
// server id is the key's SHA-256. For every paired peer the server keeps
// a link: it dials the peer's relay (internal/relay), runs TLS 1.3 inside
// the relay's stream with both keys pinned, says hello with its link
// protocol and capabilities, and then speaks tm's NDJSON and frames over
// that stream. A link that drops dials again with backoff.
//
// The transport is an interface (Transport), so a direct or tunnelled
// address can carry the same link later: what a link trusts is the
// pinned key, never the network.
package peer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// IdentityFile is the identity key's name in the peer state directory:
// PKCS #8 in PEM, mode 0600.
const IdentityFile = "identity.key"

// Dir is the peer state directory, <home>/state/peer: the identity key
// and the peers. The server owns it; no agent reads it.
func Dir() (string, error) {
	h, err := home.Dir()
	if err != nil {
		return "", err
	}
	return DirIn(h), nil
}

// DirIn is the peer state directory of the home h.
func DirIn(h string) string { return filepath.Join(h, "state", "peer") }

// ID is a server id: the SHA-256 of its public key (relay.Fingerprint).
type ID [32]byte

// IDOf is pub's server id.
func IDOf(pub ed25519.PublicKey) ID { return sha256.Sum256(pub) }

// String is the id in full, 64 lowercase hex digits.
func (id ID) String() string { return hex.EncodeToString(id[:]) }

// Short is the id as people read it: its first 8 hex digits, upper case,
// in two groups (4F2A-91C3).
func (id ID) Short() string {
	s := strings.ToUpper(hex.EncodeToString(id[:4]))
	return s[:4] + "-" + s[4:]
}

// Identity is this server's key and id.
type Identity struct {
	Key ed25519.PrivateKey
	ID  ID
}

func newIdentity(k ed25519.PrivateKey) *Identity {
	return &Identity{Key: k, ID: IDOf(k.Public().(ed25519.PublicKey))}
}

// LoadIdentity reads the identity key in dir. A server that was never
// paired has none: the error matches fs.ErrNotExist.
func LoadIdentity(dir string) (*Identity, error) {
	if err := fsx.CheckPrivate(dir); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, IdentityFile))
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s: not a PEM private key", IdentityFile)
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", IdentityFile, err)
	}
	ek, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an Ed25519 key", IdentityFile)
	}
	return newIdentity(ek), nil
}

// EnsureIdentity reads the identity key in dir, or makes one when there
// is none (pairing does, the first time). created says it was made now.
// An unreadable key is an error, never replaced: the peers have it
// pinned.
func EnsureIdentity(dir string) (id *Identity, created bool, err error) {
	id, err = LoadIdentity(dir)
	if !errors.Is(err, fs.ErrNotExist) {
		return id, false, err
	}
	id, err = writeIdentity(dir)
	return id, err == nil, err
}

// RotateIdentity replaces the identity key in dir with a new one, even
// one that can't be read: the human asked. Every peer has the old one
// pinned, so each must pair again.
func RotateIdentity(dir string) (*Identity, error) { return writeIdentity(dir) }

// writeIdentity makes a new key and writes it to dir.
func writeIdentity(dir string) (*Identity, error) {
	if err := fsx.EnsurePrivateDir(dir); err != nil {
		return nil, err
	}
	_, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return nil, err
	}
	b := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := fsx.WriteAtomic(filepath.Join(dir, IdentityFile), b, 0o600); err != nil {
		return nil, err
	}
	return newIdentity(k), nil
}

// IdentityOf is the identity of the key k: a device's, which keeps its
// own key (docs/SPEC.md §16.15), or a test's.
func IdentityOf(k ed25519.PrivateKey) *Identity { return newIdentity(k) }
