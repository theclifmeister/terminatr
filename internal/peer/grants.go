package peer

// What the human granted each paired server (docs/SPEC.md §16.8, Who raises
// a switch). peers.json says what the user's tm wrote, but any process of
// this user can write that file, so the server believes it only up to what
// it was asked for through peer.switch on the human socket. Lower holds at
// once, whoever wrote it; higher doesn't count until the human says so, and
// the Network popup says what was refused.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// GrantsFile keeps the grants across restarts, in the peer state directory.
// The server reads it at its start only and alone writes it: while it runs,
// what it holds in memory is the record.
const GrantsFile = "grants.json"

// grant is what the human let one paired server do, its switches as Peer
// spells them out: levels in full, never "" for off.
type grant struct {
	Panes         string   `json:"panes"`
	Conversations string   `json:"conversations"`
	Needs         string   `json:"needs"`
	Pages         []string `json:"pages,omitempty"`
	Files         []string `json:"files,omitempty"`
}

// grantOf is what p's switches say.
func grantOf(p Peer) grant {
	return grant{Panes: p.PanesLevel(), Conversations: convWord(p.ConversationsOn()), Needs: p.NeedsLevel(),
		Pages: slices.Clone(p.Pages), Files: slices.Clone(p.Files)}
}

// newGrant is a fresh pairing's grant: every switch at its default.
func newGrant() grant {
	return grantOf(Peer{})
}

func convWord(on bool) string {
	if on {
		return proto.ConversationsOn
	}
	return proto.ConversationsNot
}

// lower is the lower of a and b on levels, in order from least to most.
func lower(levels []string, a, b string) string {
	return levels[min(slices.Index(levels, a), slices.Index(levels, b))]
}

var (
	panesOrder = []string{proto.PanesOff, proto.PanesView, proto.PanesType}
	convOrder  = []string{proto.ConversationsNot, proto.ConversationsOn}
	needsOrder = []string{proto.NeedsOff, proto.NeedsView, proto.NeedsAnswer}
)

// meet is what holds between g and p's switches, and the words for what p
// says beyond g.
func (g grant) meet(p Peer) (grant, []string) {
	disk := grantOf(p)
	out := grant{
		Panes:         lower(panesOrder, g.Panes, disk.Panes),
		Conversations: lower(convOrder, g.Conversations, disk.Conversations),
		Needs:         lower(needsOrder, g.Needs, disk.Needs),
	}
	var words []string
	if out.Panes != disk.Panes {
		words = append(words, "panes "+proto.PanesWord(disk.Panes))
	}
	if out.Conversations != disk.Conversations {
		words = append(words, "conversations "+disk.Conversations)
	}
	if out.Needs != disk.Needs {
		words = append(words, "needs "+disk.Needs)
	}
	both := func(kind string, have, said []string) []string {
		var kept []string
		for _, slug := range said {
			if slices.Contains(have, slug) {
				kept = append(kept, slug)
			} else {
				words = append(words, kind+" "+slug)
			}
		}
		return kept
	}
	out.Pages = both("pages", g.Pages, disk.Pages)
	out.Files = both("files", g.Files, disk.Files)
	return out, words
}

// on applies g's switches to p, its names and relay kept.
func (g grant) on(p Peer) Peer {
	p.Panes, p.Conversations, p.Needs, p.Pages, p.Files = g.Panes, g.Conversations, g.Needs, g.Pages, g.Files
	return p
}

func (g grant) equal(o grant) bool {
	return g.Panes == o.Panes && g.Conversations == o.Conversations && g.Needs == o.Needs &&
		slices.Equal(g.Pages, o.Pages) && slices.Equal(g.Files, o.Files)
}

// grants is the server's record, by server id.
type grants struct {
	mu   sync.Mutex
	dir  string
	logf func(string, ...any)
	by   map[string]grant
	// adopt: there was no file at the start, as on the first run of a tm
	// that keeps them. The first read of the peers keeps what they say.
	adopt bool
	// said is the refusals as last logged, so a refusal logs once.
	said string
}

type grantsDoc struct {
	Grants map[string]grant `json:"grants"`
}

func loadGrants(dir string, logf func(string, ...any)) *grants {
	g := &grants{dir: dir, logf: logf, by: map[string]grant{}}
	b, err := os.ReadFile(filepath.Join(dir, GrantsFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		g.adopt = true
	case err != nil:
		g.log("peers: %s: %v; every paired server is refused until tm peer allow", GrantsFile, err)
	default:
		var d grantsDoc
		if err := json.Unmarshal(b, &d); err != nil {
			g.log("peers: %s: %v; every paired server is refused until tm peer allow", GrantsFile, err)
		} else if d.Grants != nil {
			g.by = d.Grants
		}
	}
	return g
}

func (g *grants) log(format string, a ...any) {
	if g.logf != nil {
		g.logf(format, a...)
	}
}

func (g *grants) save() error {
	if err := fsx.EnsurePrivateDir(g.dir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(grantsDoc{Grants: g.by}, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(g.dir, GrantsFile), append(b, '\n'), 0o600)
}

func (g *grants) keep() {
	if err := g.save(); err != nil {
		g.log("peers: keeping %s: %v", GrantsFile, err)
	}
}

// settle is what holds of peers, as they are in the file now, and what they
// say that doesn't count. A switch the file lowered stays lowered here too,
// so raising it again takes the human; a peer no longer there leaves.
// Callers hold g.mu.
func (g *grants) settle(peers []Peer) ([]Peer, []proto.PeerRefusal) {
	changed := false
	if g.adopt {
		g.adopt, changed = false, true
		for _, p := range peers {
			g.by[p.ID().String()] = grantOf(p)
		}
		g.log("peers: no %s yet: keeping what %s says for %d paired servers", GrantsFile, PeersFile, len(peers))
	}
	var out []Peer
	var refused []proto.PeerRefusal
	seen := map[string]bool{}
	for _, p := range peers {
		id := p.ID().String()
		seen[id] = true
		have, known := g.by[id]
		if !known {
			off := grantOf(Peer{Conversations: proto.ConversationsNot})
			refused = append(refused, refusedOf(p, off, []string{proto.PeerRefusalPairing}))
			continue
		}
		holds, words := have.meet(p)
		if !holds.equal(have) {
			g.by[id], changed = holds, true
		}
		out = append(out, holds.on(p))
		if len(words) > 0 {
			refused = append(refused, refusedOf(p, holds, words))
		}
	}
	for id := range g.by {
		if !seen[id] {
			delete(g.by, id)
			changed = true
		}
	}
	if changed {
		g.keep()
	}
	if said := refusedWords(refused); said != g.said {
		g.said = said
		if said != "" {
			g.log("peers: %s says what tm wasn't asked for, and it doesn't count: %s", PeersFile, said)
		}
	}
	return out, refused
}

func refusedOf(p Peer, holds grant, words []string) proto.PeerRefusal {
	return proto.PeerRefusal{Alias: p.Alias, ID: p.ID().String(), Words: words, Panes: holds.Panes,
		Conversations: holds.Conversations, Needs: holds.Needs, Pages: holds.Pages, Files: holds.Files}
}

func refusedWords(refused []proto.PeerRefusal) string {
	var parts []string
	for _, r := range refused {
		parts = append(parts, r.Alias+": "+strings.Join(r.Words, ", "))
	}
	return strings.Join(parts, "; ")
}

// read is what holds of the file now.
func (g *grants) read() ([]Peer, []proto.PeerRefusal, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	peers, err := LoadPeers(g.dir)
	if err != nil {
		return nil, nil, err
	}
	out, refused := g.settle(peers)
	return out, refused, nil
}

// NotAllowedError is a switch set for a paired server whose pairing tm wasn't
// asked to keep.
type NotAllowedError struct{ Alias string }

func (e *NotAllowedError) Error() string {
	return fmt.Sprintf("tm didn't pair with %s: tm peer allow %s keeps it, or tm peer remove %s", e.Alias, e.Alias, e.Alias)
}

// ApplySwitch writes one switch to peers.json; false when there is no such
// peer. A switch's raise counts only through Manager.SetSwitch.
func ApplySwitch(dir string, p proto.PeerSwitchParams) (bool, error) {
	on := p.Value == "on"
	switch p.Switch {
	case proto.PeerSwitchPanes:
		return SetPanes(dir, p.Alias, p.Value)
	case proto.PeerSwitchNeeds:
		return SetNeeds(dir, p.Alias, p.Value)
	case proto.PeerSwitchConversations:
		return SetConversations(dir, p.Alias, on)
	case proto.PeerSwitchPages:
		return SetPages(dir, p.Alias, p.Project, on)
	case proto.PeerSwitchFiles:
		return SetFiles(dir, p.Alias, p.Project, on)
	}
	return false, fmt.Errorf("no switch %q", p.Switch)
}

// set does what the human asked: writes the switch and keeps it as theirs,
// that one switch alone, so nothing else the file says comes along with it.
func (g *grants) set(p proto.PeerSwitchParams) (proto.PeerSwitchResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	peers, err := LoadPeers(g.dir)
	if err != nil {
		return proto.PeerSwitchResult{}, err
	}
	g.settle(peers)
	p.Alias = NamesOf(peers).Alias(p.Alias)
	i := slices.IndexFunc(peers, func(q Peer) bool { return q.Alias == p.Alias })
	if i < 0 {
		return proto.PeerSwitchResult{}, &UnknownPeerError{Alias: p.Alias}
	}
	id := peers[i].ID().String()
	have, known := g.by[id]
	res := proto.PeerSwitchResult{Alias: p.Alias}
	switch p.Switch {
	case proto.PeerSwitchPin:
		if !known {
			have = newGrant()
		}
	case proto.PeerSwitchAllow:
		if known {
			_, res.Allowed = have.meet(peers[i])
		} else {
			res.Allowed = []string{proto.PeerRefusalPairing}
		}
		have = grantOf(peers[i])
	default:
		if !known {
			return res, &NotAllowedError{Alias: p.Alias}
		}
		if _, err := ApplySwitch(g.dir, p); err != nil {
			return res, err
		}
		on := p.Value == "on"
		switch p.Switch {
		case proto.PeerSwitchPanes:
			level, _ := proto.ParsePanes(p.Value)
			have.Panes = level
		case proto.PeerSwitchNeeds:
			level, _ := proto.ParseNeeds(p.Value)
			have.Needs = level
		case proto.PeerSwitchConversations:
			have.Conversations = convWord(on)
		case proto.PeerSwitchPages:
			have.Pages = withSlug(have.Pages, p.Project, on)
		case proto.PeerSwitchFiles:
			have.Files = withSlug(have.Files, p.Project, on)
		}
	}
	g.by[id] = have
	return res, g.save()
}

// withSlug is list with slug on or off, sorted as peers.json keeps it.
func withSlug(list []string, slug string, on bool) []string {
	out := slices.DeleteFunc(slices.Clone(list), func(s string) bool { return s == slug })
	if on {
		out = append(out, slug)
		slices.Sort(out)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
