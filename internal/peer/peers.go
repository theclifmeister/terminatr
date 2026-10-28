package peer

import (
	"cmp"
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
	"github.com/theclifmeister/terminatr/internal/relay"
)

// PeersFile is the paired servers' list in the peer state directory.
const PeersFile = "peers.json"

// Peer is a paired server: the key pinned at pairing, the local alias the
// user knows it by, and the relay both dial.
type Peer struct {
	Alias string `json:"alias"`
	// Name is the display name the user gave it here (tm peer name),
	// shown in its alias's place; either targets it (Names). The alias
	// stays the key of everything stored. Nothing of it crosses a link.
	Name string `json:"name,omitempty"`
	// Told is the name the server gives itself, as its link last said
	// ([server] name there, §16.8): shown in the alias's place while no
	// Name is set here, never instead of the alias as a key. "" for none,
	// or one that is another peer's, this server's or a project's here.
	Told  string            `json:"told,omitempty"`
	Key   ed25519.PublicKey `json:"key"`   // base64
	Relay string            `json:"relay"` // wss://host[:port][#sha256=<pin>]
	// Panes is what the peer may do with this server's panes
	// (proto.PanesOff, the default when empty, PanesView, PanesType;
	// docs/SPEC.md §16.12). tm writes "type", which tm v0.27.0 reads too,
	// and reads "write" as well.
	Panes string `json:"panes,omitempty"`
	// Conversations is the conversations switch for the peer
	// (docs/SPEC.md §16.14): "off" shares no conversation; on, the
	// default when empty, shares the shared projects' coordinator and
	// thread conversations while Panes is view or write.
	Conversations string `json:"conversations,omitempty"`
}

// ConversationsOn says whether p's conversations switch is on.
func (p Peer) ConversationsOn() bool { return p.Conversations != proto.ConversationsNot }

// PanesLevel is p's pane switch, off when unset.
func (p Peer) PanesLevel() string {
	if level, ok := proto.ParsePanes(p.Panes); ok {
		return level
	}
	return proto.PanesOff
}

// ID is the peer's server id.
func (p Peer) ID() ID { return IDOf(p.Key) }

// Shown is what screens and agents call p: its display name, else its
// alias.
func (p Peer) Shown() string { return cmp.Or(p.Display(), p.Alias) }

// Display is p's display name: the name set here, else the one it gives
// itself; "" for neither.
func (p Peer) Display() string { return cmp.Or(p.Name, p.Told) }

// Both is p with its alias beside its display name, "work
// (del-02-0468-lt)", for where both help (tm peer list, Settings →
// Peers).
func (p Peer) Both() string { return Both(p.Display(), p.Alias) }

// Both is name with alias beside it; alias alone without a name.
func Both(name, alias string) string {
	if name == "" || name == alias {
		return alias
	}
	return name + " (" + alias + ")"
}

func (p Peer) check() error {
	if p.Alias == "" {
		return errors.New("a peer without an alias")
	}
	if len(p.Key) != ed25519.PublicKeySize {
		return fmt.Errorf("peer %s: the key is %d bytes, want %d", p.Alias, len(p.Key), ed25519.PublicKeySize)
	}
	if p.Name != "" && CheckName(p.Name) != nil {
		return fmt.Errorf("peer %s: %w", p.Alias, CheckName(p.Name))
	}
	if p.Told != "" && CheckName(p.Told) != nil {
		return fmt.Errorf("peer %s: told %w", p.Alias, CheckName(p.Told))
	}
	if p.Panes != "" && !proto.ValidPanes(p.Panes) {
		return fmt.Errorf("peer %s: panes %q is not off, view or write", p.Alias, p.Panes)
	}
	if p.Conversations != "" && p.Conversations != proto.ConversationsOn && p.Conversations != proto.ConversationsNot {
		return fmt.Errorf("peer %s: conversations %q is not on or off", p.Alias, p.Conversations)
	}
	u, err := url.Parse(p.Relay)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" {
		return fmt.Errorf("peer %s: relay %q is not a wss:// URL", p.Alias, p.Relay)
	}
	if pin, err := relay.ParsePin(u.Fragment); err != nil || (pin != "" && u.Scheme != "wss") {
		return fmt.Errorf("peer %s: relay %q: a pin is #sha256= and 64 hex digits, on a wss:// URL", p.Alias, p.Relay)
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
	// A name is one more word for its peer: no other peer's alias or
	// name, or <name>/<project> would mean two servers.
	names := map[string]string{}
	for _, p := range peers {
		if p.Name == "" || p.Name == p.Alias {
			continue
		}
		if alias[p.Name] {
			return fmt.Errorf("peer %s: the name %s is another peer's alias", p.Alias, p.Name)
		}
		if other, ok := names[p.Name]; ok {
			return fmt.Errorf("peers %s and %s are both named %s", other, p.Alias, p.Name)
		}
		names[p.Name] = p.Alias
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

// CheckName says why s can't be a peer's display name: an alias's rules.
func CheckName(s string) error {
	if CheckAlias(s) != nil {
		return fmt.Errorf("name %q: 1 to %d of a-z, 0-9 and -, not starting with -", s, MaxAlias)
	}
	return nil
}

// HostLabel is the name a server gives the other side when pairing
// (docs/SPEC.md §16.8): the first label of its host name, in lower case.
func HostLabel(host string) string {
	host, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(host)), ".")
	return host
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
				// Pairing again keeps the switches and the names.
				p.Panes, p.Conversations, p.Name, p.Told = q.Panes, q.Conversations, q.Name, q.Told
			case q.Alias == as:
			default:
				kept = append(kept, q)
			}
		}
		if as != "" {
			if i := slices.IndexFunc(kept, func(q Peer) bool { return q.Name == as }); i >= 0 {
				return nil, fmt.Errorf("alias %s: it is the name of %s here (tm peer name %s NEW changes it)", as, kept[i].Alias, kept[i].Alias)
			}
			p.Alias = as
		}
		if p.Name == p.Alias {
			p.Name = ""
		}
		if p.Alias == "" {
			base := DefaultAlias(name)
			p.Alias = base
			for n := 2; slices.ContainsFunc(kept, func(q Peer) bool { return q.Alias == p.Alias || q.Name == p.Alias || q.Told == p.Alias }); n++ {
				p.Alias = fmt.Sprintf("%s-%d", base, n)
			}
		}
		if p.Name != "" && slices.ContainsFunc(kept, func(q Peer) bool { return q.Alias == p.Name || q.Name == p.Name }) {
			// Another peer took the name meanwhile: the alias shows.
			p.Name = ""
		}
		if p.Told == p.Alias || slices.ContainsFunc(kept, func(q Peer) bool { return q.Alias == p.Told || q.Name == p.Told || q.Told == p.Told }) {
			p.Told = ""
		}
		for i, q := range kept {
			// A name it told yields to the alias or name pinned now.
			if q.Told == p.Alias || q.Told == p.Name {
				kept[i].Told = ""
			}
		}
		return append(kept, p), nil
	})
	return p, err
}

// SetPanes sets the pane switch for the peer called alias, or named so
// (docs/SPEC.md §16.12); false when there is none.
func SetPanes(dir, alias, word string) (bool, error) {
	level, ok := proto.ParsePanes(word)
	if !ok {
		return false, fmt.Errorf("panes %q: off, view or write", word)
	}
	found := false
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		alias := NamesOf(peers).Alias(alias)
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

// SetConversations turns the conversations switch for the peer called
// alias, or named so, on or off (docs/SPEC.md §16.14); false when there
// is none.
func SetConversations(dir, alias string, on bool) (bool, error) {
	found := false
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		alias := NamesOf(peers).Alias(alias)
		for i := range peers {
			if peers[i].Alias == alias {
				found = true
				peers[i].Conversations = ""
				if !on {
					peers[i].Conversations = proto.ConversationsNot
				}
			}
		}
		return peers, nil
	})
	return found, err
}

// Unpin drops the peer called alias, or named so; false when there is
// none.
func Unpin(dir, alias string) (bool, error) {
	found := false
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		alias := NamesOf(peers).Alias(alias)
		return slices.DeleteFunc(peers, func(q Peer) bool {
			if q.Alias == alias {
				found = true
			}
			return q.Alias == alias
		}), nil
	})
	return found, err
}

// SetRelay sets the relay of the peer called alias (or named so) to relayURL
// (RelayURL's form): the relay moved, or has a new key; false when there
// is no such peer.
func SetRelay(dir, alias, relayURL string) (bool, error) {
	found := false
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		alias := NamesOf(peers).Alias(alias)
		for i := range peers {
			if peers[i].Alias == alias {
				found = true
				peers[i].Relay = relayURL
			}
		}
		return peers, nil
	})
	return found, err
}

// NameTaken says why name can't be a peer's name though no peer has it:
// this server's own name, or a project here, would make <name>/<project>
// mean two things. "" when it can be.
type NameTaken func(name string) string

// SetName gives the peer who (its alias or its name) the display name
// name, or clears it with "" (the alias shows again). It refuses a name
// another peer has as alias or name, and one taken says is taken.
func SetName(dir, who, name string, taken NameTaken) (Peer, error) {
	if name != "" {
		if err := CheckName(name); err != nil {
			return Peer{}, err
		}
	}
	var got Peer
	err := change(dir, func(peers []Peer) ([]Peer, error) {
		i := slices.IndexFunc(peers, func(q Peer) bool { return q.Alias == who })
		if i < 0 {
			i = slices.IndexFunc(peers, func(q Peer) bool { return q.Name == who })
		}
		if i < 0 {
			return nil, &UnknownPeerError{Alias: who}
		}
		if name == peers[i].Alias {
			name = ""
		}
		if name != "" {
			for j, q := range peers {
				if j != i && (q.Alias == name || q.Name == name || q.Told == name) {
					return nil, &NameError{Name: name, Why: "the paired server " + q.Both() + " is called that"}
				}
			}
			if taken != nil {
				if why := taken(name); why != "" {
					return nil, &NameError{Name: name, Why: why}
				}
			}
		}
		peers[i].Name = name
		got = peers[i]
		return peers, nil
	})
	return got, err
}

// NameError is a display name SetName refused, and why.
type NameError struct{ Name, Why string }

func (e *NameError) Error() string { return fmt.Sprintf("the name %s: %s", e.Name, e.Why) }

// Names maps a paired server's alias or name to its alias, and an alias
// to what screens show, from peers.json. The zero Names knows no peer:
// every word is its own alias.
type Names struct {
	alias map[string]string // alias or name → alias
	shown map[string]string // alias → name, when it has one
}

// NamesOf is the names of peers.
func NamesOf(peers []Peer) Names {
	n := Names{alias: map[string]string{}, shown: map[string]string{}}
	for _, p := range peers {
		n.alias[p.Alias] = p.Alias
	}
	for _, p := range peers {
		if p.Name != "" && p.Name != p.Alias {
			if _, ok := n.alias[p.Name]; !ok {
				n.alias[p.Name] = p.Alias
			}
			n.shown[p.Alias] = p.Name
		}
	}
	// The names servers give themselves after the ones set here: a name
	// set here wins.
	for _, p := range peers {
		if p.Name == "" && p.Told != "" && p.Told != p.Alias {
			if _, ok := n.alias[p.Told]; !ok {
				n.alias[p.Told] = p.Alias
				n.shown[p.Alias] = p.Told
			}
		}
	}
	return n
}

// LoadNames is the names of the peers in dir; none when they can't be
// read.
func LoadNames(dir string) Names {
	peers, _ := LoadPeers(dir)
	return NamesOf(peers)
}

// HomeNames is LoadNames of this tm home's peer state directory.
func HomeNames() Names {
	dir, err := Dir()
	if err != nil {
		return Names{}
	}
	return LoadNames(dir)
}

// Alias is the alias of the peer s names (its alias or its name); s
// itself when no peer has it.
func (n Names) Alias(s string) string {
	if a, ok := n.alias[s]; ok {
		return a
	}
	return s
}

// Key is key, "<server>/<rest>", with the server as its alias: what
// everything stored is keyed by. A key without "/" is a project here,
// as it is.
func (n Names) Key(key string) string {
	if x, rest, ok := strings.Cut(key, "/"); ok {
		return n.Alias(x) + "/" + rest
	}
	return key
}

// Show is what screens call the peer alias: its name, else alias.
func (n Names) Show(alias string) string {
	if s, ok := n.shown[alias]; ok {
		return s
	}
	return alias
}

// ShowKey is key, "<alias>/<rest>", with the server as shown (Show); a
// project here as it is.
func (n Names) ShowKey(key string) string {
	if a, rest, ok := strings.Cut(key, "/"); ok {
		return n.Show(a) + "/" + rest
	}
	return key
}

// Both is the peer alias with its name: Both(name, alias).
func (n Names) Both(alias string) string { return Both(n.shown[alias], alias) }

// RelayHost is the relay r as people read it: its host for wss://, and
// "(pinned)" for one taken by its key's pin.
func RelayHost(r string) string {
	if u, err := url.Parse(r); err == nil && u.Scheme == "wss" && u.Path == "" {
		if u.Fragment != "" {
			return u.Host + " (pinned)"
		}
		return u.Host
	}
	return r
}
