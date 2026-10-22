package link

// Messages over their direction's rate (docs/SPEC.md §7.9) wait: the
// sender's copy is written queued, with the time it should go, and the
// store lists them by direction in order. drain sends a direction's in
// that order as its bucket refills: before each send on it, and on the
// server's outbox poll (Drain). A message to a paired server's project
// goes on to the outbox, queued still. A paused link keeps them until
// the user resumes it; a link turned off, or a project gone, since they
// were queued marks them undelivered and tells the sender. Nothing over
// the rate is dropped.

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// queued is a message waiting for its direction's bucket; User: the
// user approved it (a held message), so it goes as the user's input on
// the link.
type queued struct {
	ID   string `json:"id"`
	User bool   `json:"user,omitempty"`
}

// limits are a direction's: its token bucket, burst messages at once
// then perHour an hour, and the most bytes a message's text may have.
type limits struct{ burst, perHour, maxText int }

// limitsOf are the limits of a direction between projects sides: the
// lower of each one's settings (link_burst, link_per_hour, link_max_kb).
// A paired server's project has none here: its server keeps its own.
func limitsOf(cfg *config.Config, sides ...string) limits {
	d := config.Defaults
	l := limits{d.LinkBurst, d.LinkPerHour, d.LinkMaxKB << 10}
	first := true
	for _, slug := range sides {
		if !project.ValidSlug(slug) {
			continue
		}
		s, err := cfg.Safety(slug)
		if err != nil {
			continue
		}
		o := limits{max(s.LinkBurst, 1), max(s.LinkPerHour, 1), max(s.LinkMaxKB, 1) << 10}
		if first {
			l, first = o, false
			continue
		}
		l = limits{min(l.burst, o.burst), min(l.perHour, o.perHour), min(l.maxText, o.maxText)}
	}
	return l
}

// over reports whether a message from → to at t must wait: others wait
// before it, or the direction's bucket is empty.
func (s *store) over(from, to string, l limits, t time.Time) bool {
	return len(s.Queued[from+">"+to]) > 0 || s.refill(from, to, l, t).Tokens < 1
}

// due is when the n-th message waiting from → to should go, at t: once
// the bucket has refilled for it and those before it.
func (s *store) due(from, to string, n int, l limits, t time.Time) time.Time {
	need := float64(n) - s.refill(from, to, l, t).Tokens
	if need <= 0 {
		return t
	}
	return t.Add(time.Duration(need / float64(l.perHour) * float64(time.Hour)))
}

// wait queues p's message m (Out) behind those waiting on its direction:
// written queued with when it should go, its attachments copied into
// p's folder as a held one's are, to go on from there.
func (s *store) wait(p *project.Project, m *Message, att []attachment, user bool, l limits, t time.Time) error {
	k := p.Slug + ">" + m.Peer
	s.Queued[k] = append(s.Queued[k], queued{ID: m.ID, User: user})
	m.State = StateQueued
	m.Due = s.due(p.Slug, m.Peer, len(s.Queued[k]), l, t).UTC().Truncate(time.Second)
	if err := write(p, m); err != nil {
		return err
	}
	return copyAttach(p, m, att)
}

// unqueue takes message id out of p's queue to other, the rest keeping
// their order and the due times of those behind it moving up; false if
// it isn't there (it left); the store is locked.
func (s *store) unqueue(p *project.Project, other, id string, l limits, t time.Time) bool {
	k := p.Slug + ">" + other
	i := slices.IndexFunc(s.Queued[k], func(q queued) bool { return q.ID == id })
	if i < 0 {
		return false
	}
	s.Queued[k] = slices.Delete(s.Queued[k], i, i+1)
	for j := i; j < len(s.Queued[k]); j++ {
		due := s.due(p.Slug, other, j+1, l, t).UTC().Truncate(time.Second)
		update(p, other, s.Queued[k][j].ID, func(x *Message) { x.Due = due })
	}
	if len(s.Queued[k]) == 0 {
		delete(s.Queued, k)
	}
	return true
}

// Drain sends what waits over the rate, each direction in order, as far
// as its bucket allows; the server's outbox poll runs it. A look at the
// store comes first, so a server with nothing waiting writes nothing.
func Drain() {
	waiting := false
	readStore(func(s *store) { waiting = len(s.Queued) > 0 })
	if !waiting {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	withStore(func(s *store) error {
		for _, k := range slices.Sorted(maps.Keys(s.Queued)) {
			from, to, _ := strings.Cut(k, ">")
			s.drain(cfg, from, to, now())
		}
		return nil
	})
}

// drain sends from → to's waiting messages in order while the link isn't
// paused and the bucket has a token; the store is locked. One the link
// no longer takes is marked undelivered; one that fails to write stays
// first, for the next drain.
func (s *store) drain(cfg *config.Config, from, to string, t time.Time) {
	k := from + ">" + to
	defer func() {
		if len(s.Queued[k]) == 0 {
			delete(s.Queued, k)
		}
	}()
	if len(s.Queued[k]) == 0 {
		return
	}
	p, err := project.Open(from)
	if err != nil {
		delete(s.Queued, k) // the sender is gone, its copies with it
		return
	}
	l := limitsOf(cfg, from, to)
	for len(s.Queued[k]) > 0 {
		if s.allow(from, to) != nil || s.refill(from, to, l, t).Tokens < 1 {
			return
		}
		q := s.Queued[k][0]
		m, err := readFile(path(p, to, q.ID), to)
		if err != nil || !m.Out || m.State != StateQueued {
			s.Queued[k] = s.Queued[k][1:]
			continue
		}
		err = s.send(cfg, p, m, q.User, l)
		var refusal *tasks.Error
		if err != nil && !errors.As(err, &refusal) {
			return
		}
		s.Queued[k] = s.Queued[k][1:]
		if refusal != nil {
			unqueued(p, m, refusal)
		}
	}
}

// send delivers p's waiting message m, the link checked again; the
// store is locked.
func (s *store) send(cfg *config.Config, p *project.Project, m *Message, user bool, l limits) error {
	d, err := resolve(cfg, m.Peer)
	if err != nil {
		return err
	}
	if code, msg := cfg.LinkRefusal(p.Slug, m.Peer); code != "" {
		return refuse(code, "%s", msg)
	}
	c := caller.Caller{Kind: caller.Coordinator, Project: p.Slug}
	if user || m.By == "human" {
		c = caller.Caller{Kind: caller.Human}
	}
	m.Due = time.Time{}
	return d.deliver(s, c, p, m, nil, l)
}

// unqueued marks p's waiting message m undelivered, refused by the link
// since it was queued, and tells p's inbox.
func unqueued(p *project.Project, m *Message, why *tasks.Error) {
	update(p, m.Peer, m.ID, func(x *Message) { x.State, x.Refused, x.Due = StateUndelivered, why.Code, time.Time{} })
	p.AddMessageItem(KindUndelivered, m.Peer, fmt.Sprintf("your message %s to %s waited over the link's rate and wasn't delivered: %s", m.ID, m.Peer, why.Msg), false, m.ID)
	p.Journal(caller.Caller{Kind: caller.Ticker}, "link.undelivered", m.Peer, m.ID+" ("+why.Code+")")
}

// DueWords say when a message waiting over the rate should go: "about
// 15:04", or with the day when that isn't today.
func DueWords(m *Message) string {
	due := m.Due.Local()
	if y, mo, d := now().Local().Date(); due.Year() == y && due.Month() == mo && due.Day() == d {
		return "about " + due.Format("15:04")
	}
	return "about " + due.Format("Jan 2 15:04")
}

func queuedWords(m *Message) string {
	return fmt.Sprintf("%s (%s), over the rate: goes %s", m.ID, KB(m.Bytes), DueWords(m))
}

// StateWords are a message's state as tm link log and read show it:
// the state, and for one waiting over the rate when it should go.
func StateWords(m *Message) string {
	if m.State == StateQueued && !m.Due.IsZero() {
		return "queued over the rate, goes " + DueWords(m)
	}
	return m.State
}
