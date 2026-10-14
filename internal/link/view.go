package link

import (
	"slices"
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

// Words is a row's state: "linked both ways", "waits for web to accept",
// "notes wants to link", "off".
func (r Row) Words() string {
	var s string
	switch {
	case r.Send && r.Receive:
		s = "linked both ways"
	case r.Send:
		s = "linked, send only"
	case r.Receive:
		s = "linked, receive only"
	case r.HasMine && r.Mine.Sends() && !r.Theirs.Receives():
		s = "waits for " + r.Peer + " to accept"
	case r.HasMine && r.Mine.Receives() && !r.Theirs.Sends():
		s = "waits for " + r.Peer + " to send"
	case r.HasTheirs && !r.HasMine:
		s = r.Peer + " wants to link"
	default:
		s = "off"
	}
	if r.Paused {
		s += ", paused"
	}
	return s
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
	var out []Row
	for _, o := range peers {
		theirs, _ := cfg.Links(o)
		r := Row{Peer: o}
		r.Mine, r.HasMine = mine[o]
		r.Theirs, r.HasTheirs = theirs[p.Slug]
		r.Send = r.Mine.Sends() && r.Theirs.Receives()
		r.Receive = r.Mine.Receives() && r.Theirs.Sends()
		r.Paused = Paused(p.Slug, o)
		for _, it := range items {
			if it.Kind == KindMessage && it.Subject == o {
				r.Unread++
			}
		}
		if log, err := Log(p, o); err == nil {
			for _, m := range log {
				if m.Out && m.State == StateHeld {
					r.Held++
				}
			}
			if n := len(log); n > 0 {
				r.Last, r.LastAt = log[n-1].ID, log[n-1].Created
			}
		}
		out = append(out, r)
	}
	return out
}

// Pending are p's held messages, oldest first: what the dashboard's
// NEEDS YOU lists for the user's OK.
func Pending(p *project.Project) []*Message {
	var out []*Message
	for _, o := range Peers(p) {
		log, _ := Log(p, o)
		for _, m := range log {
			if m.Out && m.State == StateHeld {
				out = append(out, m)
			}
		}
	}
	return out
}
