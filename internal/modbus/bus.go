// Package modbus is the server's event bus for user mods (docs/SPEC.md
// §7.5, Events for user mods): the server publishes what changed, and
// every running code mod gets the events it subscribed to through a
// queue of its own.
//
// Each mod's queue is bounded (QueueSize events; then the oldest is
// dropped and the next event delivered says how many were), so a slow
// or stuck mod never holds up the server or another mod. Events are
// delivered one at a time and in order, and a handler gets HandlerLimit
// before it is cancelled. A mod that (re)subscribes gets mod.ready with
// a snapshot first, never a replay of what it missed.
//
// Payloads are data, never instructions: they carry ids, states and the
// project's own fields, as tm list and the views show them.
package modbus

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// QueueSize is how many events a mod's queue holds before the oldest is
// dropped.
const QueueSize = 1000

// HandlerLimit is how long a mod's handler may run on one event.
const HandlerLimit = 5 * time.Second

// Event is one event to a mod.
type Event struct {
	Name string `json:"name"`
	// Project is the project the event is about; "" for none
	// (clock.minute, a session outside projects).
	Project string `json:"project,omitempty"`
	// Seq numbers the bus's events from 1, in publish order.
	Seq  uint64    `json:"seq"`
	Time time.Time `json:"time"`
	// Dropped is how many events this mod's queue dropped before this
	// one, since the last it was given.
	Dropped int `json:"dropped,omitempty"`
	Data    any `json:"data"`
}

// Bus fans events out to the mods' queues.
type Bus struct {
	mu   sync.Mutex
	seq  uint64
	subs map[string]*Sub // by mod id
	now  func() time.Time
}

// New makes an empty bus.
func New() *Bus { return &Bus{subs: map[string]*Sub{}, now: time.Now} }

// Options are what a mod's subscription lets through.
type Options struct {
	// Grants are the mod's granted permissions (granted ∩ requested):
	// an event that needs one it lacks is not queued (Needs).
	Grants []string
	// Events are the event names the mod listens to; nil for all.
	Events []string
	// Project says whether the mod runs for project slug (its
	// [mods.<id>] projects list); nil for every project. Events without
	// a project always pass.
	Project func(slug string) bool
	// Ready builds mod.ready's snapshot when it is delivered; nil sends
	// it without one.
	Ready func() any
}

// Subscribe opens mod's queue. A mod already subscribed (a restarted
// host) loses its old queue, whose Next ends: the new one starts with
// mod.ready, not with what the old one held.
func (b *Bus) Subscribe(mod string, o Options) *Sub {
	s := &Sub{mod: mod, bus: b, o: o, signal: make(chan struct{}, 1), done: make(chan struct{}), ready: true}
	s.grants = map[string]bool{}
	for _, g := range o.Grants {
		s.grants[g] = true
	}
	b.mu.Lock()
	old := b.subs[mod]
	b.subs[mod] = s
	b.mu.Unlock()
	if old != nil {
		old.close()
	}
	s.wake()
	return s
}

// Active says whether any mod is subscribed: the sources skip their
// work while none is.
func (b *Bus) Active() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs) > 0
}

// Wants says whether some subscribed mod would get event name about
// project: a source whose events cost work (thread.changes runs git)
// skips it while none would.
func (b *Bus) Wants(name, project string) bool {
	b.mu.Lock()
	subs := make([]*Sub, 0, len(b.subs))
	for _, s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()
	ev := Event{Name: name, Project: project}
	for _, s := range subs {
		if s.wants(ev) {
			return true
		}
	}
	return false
}

// Publish queues an event for every mod it is for. It never blocks.
func (b *Bus) Publish(name, project string, data any) {
	b.mu.Lock()
	b.seq++
	ev := Event{Name: name, Project: project, Seq: b.seq, Time: b.now(), Data: data}
	subs := make([]*Sub, 0, len(b.subs))
	for _, s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.Unlock()
	for _, s := range subs {
		if s.wants(ev) {
			s.push(ev)
		}
	}
}

// PublishTo queues an event for mod alone (an input on one of its
// views, a view it shows, action.done for its own binding), if it is
// subscribed and the event is for it. It never blocks.
func (b *Bus) PublishTo(mod, name, project string, data any) {
	b.mu.Lock()
	s := b.subs[mod]
	if s == nil {
		b.mu.Unlock()
		return
	}
	b.seq++
	ev := Event{Name: name, Project: project, Seq: b.seq, Time: b.now(), Data: data}
	b.mu.Unlock()
	if s.wants(ev) {
		s.push(ev)
	}
}

// Sub is one mod's queue.
type Sub struct {
	mod    string
	bus    *Bus
	o      Options
	grants map[string]bool

	mu      sync.Mutex
	q       []Event
	dropped int
	ready   bool // mod.ready not given yet
	closed  bool
	signal  chan struct{}
	done    chan struct{}
}

// ErrClosed is Next's error once the queue is closed or replaced.
var ErrClosed = errors.New("mod event queue closed")

// Mod is the queue's mod id.
func (s *Sub) Mod() string { return s.mod }

// SetEvents replaces the event names the queue lets through (nil: all),
// for the events the mod's hello and later subscribe notifications named.
// What is queued already stays.
func (s *Sub) SetEvents(events []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.o.Events = slices.Clone(events)
}

func (s *Sub) wants(ev Event) bool {
	if need := Needs(ev.Name); need != "" && !s.grants[need] {
		return false
	}
	s.mu.Lock()
	events := s.o.Events
	s.mu.Unlock()
	if events != nil && !slices.Contains(events, ev.Name) {
		return false
	}
	return ev.Project == "" || s.o.Project == nil || s.o.Project(ev.Project)
}

func (s *Sub) push(ev Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if len(s.q) >= QueueSize {
		s.q[0] = Event{} // let its data go
		s.q = s.q[1:]
		s.dropped++
	}
	s.q = append(s.q, ev)
	s.mu.Unlock()
	s.wake()
}

func (s *Sub) wake() {
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// Len is how many events wait in the queue.
func (s *Sub) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.q)
}

// Wait returns once an event is queued, not counting mod.ready: a mod
// whose host starts lazily starts on its first event. It returns
// ErrClosed once the queue is closed, or ctx's error. It is for the time
// no Next runs: the two share one wake-up.
func (s *Sub) Wait(ctx context.Context) error {
	for {
		s.mu.Lock()
		closed, n := s.closed, len(s.q)
		s.mu.Unlock()
		switch {
		case closed:
			return ErrClosed
		case n > 0:
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
		case <-s.signal:
		}
	}
}

// Next is the mod's next event, waiting for one until ctx ends or the
// queue closes. The first is always mod.ready.
func (s *Sub) Next(ctx context.Context) (Event, error) {
	for {
		s.mu.Lock()
		switch {
		case s.closed:
			s.mu.Unlock()
			return Event{}, ErrClosed
		case s.ready:
			s.ready = false
			s.mu.Unlock()
			return s.readyEvent(), nil
		case len(s.q) > 0:
			ev := s.q[0]
			s.q[0] = Event{}
			s.q = s.q[1:]
			ev.Dropped, s.dropped = s.dropped, 0
			s.mu.Unlock()
			return ev, nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-s.done:
		case <-s.signal:
		}
	}
}

// readyEvent is mod.ready with a fresh snapshot. It takes a sequence
// number like any event, so the mod can tell which events the snapshot
// already holds: none of those queued after it is older.
func (s *Sub) readyEvent() Event {
	var data any
	if s.o.Ready != nil {
		data = s.o.Ready()
	}
	b := s.bus
	b.mu.Lock()
	b.seq++
	ev := Event{Name: ModReady, Seq: b.seq, Time: b.now(), Data: data}
	b.mu.Unlock()
	return ev
}

// Close ends the queue; the bus forgets it unless the mod subscribed
// again since.
func (s *Sub) Close() {
	b := s.bus
	b.mu.Lock()
	if b.subs[s.mod] == s {
		delete(b.subs, s.mod)
	}
	b.mu.Unlock()
	s.close()
}

func (s *Sub) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.q = nil
	close(s.done)
}

// Handler gives one event to a mod (its host), returning when the mod's
// handler did or ctx ended. It returns an error wrapping ErrGone when the
// mod can take no more events (its host exited).
type Handler func(ctx context.Context, ev Event) error

// ErrGone says the mod is gone: Deliver stops.
var ErrGone = errors.New("mod gone")

// Deliver feeds s's events to h one at a time, in order, until ctx
// ends, s closes or h says the mod is gone. A handler past HandlerLimit
// is cancelled and logged, and a failed one logged; either way the next
// event follows. A mod restarted after ErrGone subscribes again and
// starts from mod.ready.
func Deliver(ctx context.Context, s *Sub, h Handler, logf func(format string, args ...any)) error {
	return deliver(ctx, s, h, logf, HandlerLimit)
}

func deliver(ctx context.Context, s *Sub, h Handler, logf func(string, ...any), limit time.Duration) error {
	for {
		ev, err := s.Next(ctx)
		if err != nil {
			return err
		}
		hctx, cancel := context.WithTimeout(ctx, limit)
		start := time.Now()
		err = h(hctx, ev)
		late := hctx.Err() == context.DeadlineExceeded
		cancel()
		switch {
		case errors.Is(err, ErrGone):
			return err
		case ctx.Err() != nil:
			return ctx.Err()
		case late:
			logf("mod %s: %s handler ran past %s; cancelled", s.mod, ev.Name, limit)
		case err != nil:
			logf("mod %s: %s handler: %v (%s)", s.mod, ev.Name, err, time.Since(start).Round(time.Millisecond))
		}
	}
}
