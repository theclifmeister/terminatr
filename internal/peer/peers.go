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
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/flock"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// PeersFile is the paired servers' list in the peer state directory.
const PeersFile = "peers.json"

// Peer is a paired server: the key pinned at pairing, the local alias the
// user knows it by, and the relay both dial.
type Peer struct {
	Alias string            `json:"alias"`
	Key   ed25519.PublicKey `json:"key"`   // base64
	Relay string            `json:"relay"` // wss://host[:port]
	// Panes is what the peer may do with this server's panes
	// (proto.PanesOff, the default when empty, PanesView, PanesType;
	// docs/SPEC.md §16.12).
	Panes string `json:"panes,omitempty"`
}

// PanesLevel is p's pane switch, off when unset.
func (p Peer) PanesLevel() string {
	if p.Panes == "" {
		return proto.PanesOff
	}
	return p.Panes
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
	if p.Panes != "" && !proto.ValidPanes(p.Panes) {
		return fmt.Errorf("peer %s: panes %q is not off, view or type", p.Alias, p.Panes)
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

// lockFile serializes changes to the peers: two tm peer commands at once
// would otherwise lose one's change.
const lockFile = "peers.lock"

// change runs f on the peers in dir under the lock and saves what it
// returns.
func change(dir string, f func([]Peer) ([]Peer, error)) error {
	if err := fsx.EnsurePrivateDir(dir); err != nil {
		return err
	}
	lk, err := flock.Wait(filepath.Join(dir, lockFile))
	if err != nil {
		return err
	}
	defer lk.Unlock()
	peers, err := LoadPeers(dir)
	if err != nil {
		return err
	}
	if peers, err = f(peers); err != nil {
		return err
	}
	return SavePeers(dir, peers)
}

// MaxAlias is the longest alias.
const MaxAlias = 32

// CheckAlias says why s can't be an alias: 1 to 32 of a-z, 0-9 and -,
// not starting with -.
func CheckAlias(s string) error {
	ok := s != "" && len(s) <= MaxAlias && s[0] != '-'
	for _, r := range s {
		ok = ok && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	}
	if !ok {
		return fmt.Errorf("alias %q: 1 to %d of a-z, 0-9 and -, not starting with -", s, MaxAlias)
	}
	return nil
}

// DefaultAlias is the alias for a server named name (its host name): the
// first label in lower case, other characters as -; "peer" when nothing
// is left.
func DefaultAlias(name string) string {
	name, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(name)), ".")
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > MaxAlias-3 {
		s = strings.TrimRight(s[:MaxAlias-3], "-")
	}
	if s == "" {
		return "peer"
	}
	return s
}

// Pin keeps key as a peer through relayURL, after a pairing with the
// server called name. as, when set, is the alias, and replaces a peer
// already called that; else a key already pinned keeps its alias, and a
// new one gets DefaultAlias(name), with -2, -3 … when that is taken.
func Pin(dir string, key ed25519.PublicKey, relayURL, name, as string) (Peer, error) {
	if as != "" {
		if err := CheckAlias(as); err != nil {
			return Peer{}, err
		}
	}
	p := Peer{Key: key, Relay: relayURL}
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		var kept []Peer
		for _, q := range peers {
			switch {
			case q.ID() == p.ID():
				if as == "" {
					p.Alias = q.Alias
				}
				// Pairing again keeps the pane switch.
				p.Panes = q.Panes
			case q.Alias == as:
			default:
				kept = append(kept, q)
			}
		}
		if as != "" {
			p.Alias = as
		}
		if p.Alias == "" {
			base := DefaultAlias(name)
			p.Alias = base
			for n := 2; slices.ContainsFunc(kept, func(q Peer) bool { return q.Alias == p.Alias }); n++ {
				p.Alias = fmt.Sprintf("%s-%d", base, n)
			}
		}
		return append(kept, p), nil
	})
	return p, err
}

// SetPanes sets the pane switch for the peer called alias (docs/SPEC.md
// §16.12); false when there is none.
func SetPanes(dir, alias, level string) (bool, error) {
	if !proto.ValidPanes(level) {
		return false, fmt.Errorf("panes %q: off, view or type", level)
	}
	found := false
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		for i := range peers {
			if peers[i].Alias == alias {
				found = true
				peers[i].Panes = level
				if level == proto.PanesOff {
					peers[i].Panes = ""
				}
			}
		}
		return peers, nil
	})
	return found, err
}

// Unpin drops the peer called alias; false when there is none.
func Unpin(dir, alias string) (bool, error) {
	found := false
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		return slices.DeleteFunc(peers, func(q Peer) bool {
			if q.Alias == alias {
				found = true
			}
			return q.Alias == alias
		}), nil
	})
	return found, err
}
