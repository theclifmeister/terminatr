package link

import (
	"os"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// Row is one link of a project as tm link list, tm context and the
// dashboard show it.
type Row struct {
	Peer string `json:"peer"`
	// Mine and Theirs are each side's entry; Has* say whether there is
	// one.
	Mine      config.Link `json:"mine"`
	Theirs    config.Link `json:"theirs"`
	HasMine   bool        `json:"has_mine"`
	HasTheirs bool        `json:"has_theirs"`
	// Send and Receive say which way messages go now: both sides allow
	// it.
	Send    bool `json:"send"`
	Receive bool `json:"receive"`
	Paused  bool `json:"paused,omitempty"`
	// Unread counts received messages whose item isn't handled; Held
	// this side's messages waiting for the user's OK.
	Unread int       `json:"unread"`
	Held   int       `json:"held"`
	Last   string    `json:"last,omitempty"` // the newest message's id
	LastAt time.Time `json:"last_at,omitzero"`
}

// Words is a row's state: "linked both ways", "linked, send only",
// "half-linked: only notes has an entry", "off".
func (r Row) Words() string {
	var s string
	switch {
	case r.Send && r.Receive:
		s = "linked both ways"
	case r.Send:
		s = "linked, send only"
	case r.Receive:
		s = "linked, receive only"
	case r.HasMine || r.HasTheirs:
		s = "not linked"
	default:
		s = "off"
	}
	if r.Half() {
		s += ", " + r.HalfWords()
	}
	if r.Paused {
		s += ", paused"
	}
	return s
}

// Half reports a half-linked pair: the two entries don't mirror each
// other, which only a hand edit of config.toml makes (config.Half).
func (r Row) Half() bool { return config.Half(r.Mine, r.Theirs, r.HasMine, r.HasTheirs) }

// HalfWords says what is half about a half-linked pair.
func (r Row) HalfWords() string {
	switch {
	case !r.HasTheirs:
		return "half-linked: " + r.Peer + " has no entry"
	case !r.HasMine:
		return "half-linked: only " + r.Peer + " has an entry"
	}
	return "half-linked: this side " + r.Mine.Allow + ", " + r.Peer + " " + r.Theirs.Allow
}

// Arrow is a row's direction as a mark: ⇄ → ← or · for none.
func (r Row) Arrow() string {
	switch {
	case r.Send && r.Receive:
		return "⇄"
	case r.Send:
		return "→"
	case r.Receive:
		return "←"
	}
	return "·"
}

// Rows are p's links: every project either side's entry names, and any
// project p has messages with, sorted by slug.
func Rows(cfg *config.Config, p *project.Project) []Row {
	peers := cfg.LinkPeers(p.Slug)
	for _, o := range Peers(p) {
		if !slices.Contains(peers, o) {
			peers = append(peers, o)
		}
	}
	slices.Sort(peers)
	mine, _ := cfg.Links(p.Slug)
	items, _ := p.Inbox()
	st := look()
	var out []Row
	for _, o := range peers {
		theirs, _ := cfg.Links(o)
		r := Row{Peer: o}
		r.Mine, r.HasMine = mine[o]
		r.Theirs, r.HasTheirs = theirs[p.Slug]
		r.Send = r.Mine.Sends() && r.Theirs.Receives()
		r.Receive = r.Mine.Receives() && r.Theirs.Sends()
		r.Paused = st.paused[pairKey(p.Slug, o)]
		r.Held = st.held[p.Slug+">"+o]
		for _, it := range items {
			if it.Kind == KindMessage && it.Subject == o {
				r.Unread++
			}
		}
		if m := newest(p, o); m != nil {
			r.Last, r.LastAt = m.ID, m.Created
		}
		out = append(out, r)
	}
	return out
}

// newest is p's newest message with other, by id, read alone: a look
// at the links parses no other message.
func newest(p *project.Project, other string) *Message {
	ents, _ := os.ReadDir(dir(p, other))
	best, bestN := "", -1
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() || !ValidID(id) {
			continue
		}
		if n := idNum(id); n > bestN {
			best, bestN = id, n
		}
	}
	if best == "" {
		return nil
	}
	m, err := readFile(path(p, other, best), other)
	if err != nil {
		return nil
	}
	return m
}
