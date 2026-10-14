package peer

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// PeersFile is the paired servers' list in the peer state directory.
const PeersFile = "peers.json"

// Peer is a paired server: the key pinned at pairing, the local alias the
// user knows it by, and the relay both dial.
type Peer struct {
	Alias string            `json:"alias"`
	Key   ed25519.PublicKey `json:"key"`   // base64
	Relay string            `json:"relay"` // wss://host[:port]
}

// ID is the peer's server id.
func (p Peer) ID() ID { return IDOf(p.Key) }

func (p Peer) check() error {
	if p.Alias == "" {
		return errors.New("a peer without an alias")
	}
	if len(p.Key) != ed25519.PublicKeySize {
		return fmt.Errorf("peer %s: the key is %d bytes, want %d", p.Alias, len(p.Key), ed25519.PublicKeySize)
	}
	u, err := url.Parse(p.Relay)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" {
		return fmt.Errorf("peer %s: relay %q is not a wss:// URL", p.Alias, p.Relay)
	}
	return nil
}

type peersDoc struct {
	Peers []Peer `json:"peers"`
}

// LoadPeers reads the paired servers in dir; none when the file isn't
// there. Aliases and keys are unique.
func LoadPeers(dir string) ([]Peer, error) {
	b, err := os.ReadFile(filepath.Join(dir, PeersFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d peersDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", PeersFile, err)
	}
	if err := checkPeers(d.Peers); err != nil {
		return nil, fmt.Errorf("%s: %w", PeersFile, err)
	}
	return d.Peers, nil
}

// SavePeers replaces the paired servers in dir.
func SavePeers(dir string, peers []Peer) error {
	if err := checkPeers(peers); err != nil {
		return err
	}
	if err := fsx.EnsurePrivateDir(dir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(peersDoc{Peers: peers}, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(dir, PeersFile), append(b, '\n'), 0o600)
}

func checkPeers(peers []Peer) error {
	alias, key := map[string]bool{}, map[ID]bool{}
	for _, p := range peers {
		if err := p.check(); err != nil {
			return err
		}
		if alias[p.Alias] {
			return fmt.Errorf("two peers named %s", p.Alias)
		}
		if key[p.ID()] {
			return fmt.Errorf("peer %s: the key is paired twice", p.Alias)
		}
		alias[p.Alias], key[p.ID()] = true, true
	}
	return nil
}
