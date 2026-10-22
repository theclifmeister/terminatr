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
	// Remote: the other side is a project on a paired server
	// ("mini/acme-api"); Theirs is unknown here, Send and Receive are
	// this side's, and that server checks its own.
	Remote bool `json:"remote,omitempty"`
	// Unread counts received messages whose item isn't handled; Held
	// this side's messages waiting for the user's OK.
	Unread int `json:"unread"`
	Held   int `json:"held"`
	// Queued counts this side's messages waiting over the rate.
	Queued int       `json:"queued,omitempty"`
	Last   string    `json:"last,omitempty"` // the newest message's id
	LastAt time.Time `json:"last_at,omitzero"`
	// Asks is the paired server's link request that waits for the
	// user here; Asked this side's newest request to it (request.go).
	Asks  *Request `json:"asks,omitempty"`
	Asked *Request `json:"asked,omitempty"`
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
	if r.Remote && (r.Send || r.Receive) {
		s += " on this side"
	}
	if r.Half() {
		s += ", " + r.HalfWords()
	}
	if r.Paused {
		s += ", paused"
	}
	if r.Asks != nil {
		s += ", asks to link (" + r.Asks.ID + ")"
	} else if r.Asked != nil && r.Asked.Pending() {
		s += ", asked to link back"
	}
	return s
}

// Half reports a half-linked pair: the two entries don't mirror each
// other, which only a hand edit of config.toml makes (config.Half).
func (r Row) Half() bool {
	return !r.Remote && config.Half(r.Mine, r.Theirs, r.HasMine, r.HasTheirs)
}

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
	st := look()
	for _, rq := range st.requests {
		if rq.Project == p.Slug && !slices.Contains(peers, rq.Peer) && (rq.Out || rq.Pending()) {
			peers = append(peers, rq.Peer)
		}
	}
	slices.Sort(peers)
	mine, _ := cfg.Links(p.Slug)
	items, _ := p.Inbox()
	var out []Row
	for _, o := range peers {
		r := Row{Peer: o}
		r.Mine, r.HasMine = mine[o]
		if _, _, remote := config.Remote(o); remote {
			r.Remote = true
			r.Send, r.Receive = r.Mine.Sends(), r.Mine.Receives()
		} else {
			theirs, _ := cfg.Links(o)
			r.Theirs, r.HasTheirs = theirs[p.Slug]
			r.Send = r.Mine.Sends() && r.Theirs.Receives()
			r.Receive = r.Mine.Receives() && r.Theirs.Sends()
		}
		r.Paused = st.paused[pairKey(p.Slug, o)]
		r.Held = st.held[p.Slug+">"+o]
		r.Queued = st.queued[p.Slug+">"+o]
		for _, it := range items {
			if it.Kind == KindMessage && it.Subject == o {
				r.Unread++
			}
		}
		if m := newest(p, o); m != nil {
			r.Last, r.LastAt = m.ID, m.Created
		}
		for _, rq := range st.requests {
			if rq.Project != p.Slug || rq.Peer != o {
				continue
			}
			if rq.Out {
				r.Asked = rq
			} else if rq.Pending() {
				r.Asks = rq
			}
		}
		out = append(out, r)
	}
	return out
}

// newest is p's newest message with other: a look at the links parses
// no other message but the newest of each series of ids. A series
// counts on its own: M-17 here; M-4F2A-17 and M-9C1B-4, one per side,
// with a paired server's project, so the latest created of those wins.
func newest(p *project.Project, other string) *Message {
	ents, _ := os.ReadDir(dir(p, other))
	best := map[string]string{} // by series ("M-", "M-4F2A-"): its highest id
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() || !ValidID(id) {
			continue
		}
		series := id[:strings.LastIndexByte(id, '-')+1]
		if b, ok := best[series]; !ok || idNum(id) > idNum(b) {
			best[series] = id
		}
	}
	var out *Message
	for _, id := range best {
		m, err := readFile(path(p, other, id), other)
		if err != nil {
			continue
		}
		if out == nil || m.Created.After(out.Created) || m.Created.Equal(out.Created) && m.ID > out.ID {
			out = m
		}
	}
	return out
}

// StampPaths are the files and folders Rows read for project p, with
// rows what it gave: the links' store, p's messages folder and each
// link's, for a stat stamp (fsx.StatStamp) that says the rows may have
// changed. The config, which Rows reads too, is the caller's to stamp.
func StampPaths(p *project.Project, rows []Row) []string {
	paths := []string{p.Path("messages")}
	if file, err := storePath(); err == nil {
		paths = append(paths, file)
	}
	for _, r := range rows {
		paths = append(paths, dir(p, r.Peer))
	}
	return paths
}

// Back is what this side knows of a paired server's entry for a remote
// link, from the messages between them (§16.11): Sends, a message came
// from there; Receives, that server took one of this side's (its ok or
// read receipt); Refuses, its last answer was not-linked. The newest
// answer wins; none of them is known before a message crossed.
type Back struct{ Sends, Receives, Refuses bool }

// TheirSide reads Back from msgs, oldest first (Log).
func TheirSide(msgs []*Message) Back {
	var b Back
	for _, m := range msgs {
		switch {
		case !m.Out:
			b.Sends = true
		case m.State == StateSent || m.State == StateRead:
			b.Receives, b.Refuses = true, false
		case m.State == StateUndelivered && m.Refused == "not-linked":
			b.Receives, b.Refuses = false, true
		}
	}
	return b
}

// Words is Back in words, short enough for a column: "linked back
// both ways", "refuses ours", or "their side unknown".
func (b Back) Words() string {
	switch {
	case b.Refuses && b.Sends:
		return "sends, refuses ours"
	case b.Refuses:
		return "refuses ours"
	case b.Sends && b.Receives:
		return "linked back both ways"
	case b.Receives:
		return "takes ours"
	case b.Sends:
		return "sends here"
	}
	return "their side unknown"
}
