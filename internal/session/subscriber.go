package session

import (
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// MaxPending bounds how far an attach client may lag behind. Past it the
// backlog is thrown away and replaced by a fresh snapshot: a slow client
// resyncs instead of blocking the PTY reader or being disconnected.
var MaxPending = 4 << 20

// Remote panes (docs/SPEC.md §16.12): a paired server's stream lags at
// most MaxRemotePending behind before its backlog becomes a repaint, and
// gets at most one repaint per RepaintEvery; a repaint carries the last
// RepaintHistory rows of the scrollback.
var (
	MaxRemotePending = 1 << 20
	RepaintEvery     = 250 * time.Millisecond
)

// RepaintHistory is how many scrollback rows a remote pane's repaint
// carries.
const RepaintHistory = 200

// pane is what a subscriber follows: a session's emulator, or a proxy's.
type pane interface {
	lock()
	unlock()
	// terminal is the emulator; nil once the pane has ended. mu held.
	terminal() *emu.Terminal
	// remove drops sub from the pane's subscribers; mu held.
	remove(sub *Subscriber)
	logf(format string, a ...any)
}

// Subscriber is one attached client's ordered frame stream. The pane
// appends encoded frames under its lock; the client's writer drains them
// with Next.
type Subscriber struct {
	p pane
	q *frameQueue
	// remote: a paired server's stream (AttachRemote), which resyncs
	// with a repaint rather than a snapshot.
	remote bool
	max    int
	// repainted is the last repaint; dirty: the backlog was dropped and
	// a repaint waits for RepaintEvery to pass. Both under the pane's mu.
	repainted time.Time
	dirty     bool
}

// Attach registers a subscriber. Its stream starts with a FrameSnapshot
// (libghostty's snapshot of the pane emulator), followed by every byte of
// output after that point and every resize, in order.
func (s *Session) Attach() (*Subscriber, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term == nil {
		return nil, ErrExited
	}
	sub := &Subscriber{p: s, q: newFrameQueue(), max: MaxPending}
	if !sub.start() {
		return nil, ErrExited
	}
	s.subs[sub] = struct{}{}
	return sub, nil
}

// AttachRemote registers a paired server's subscriber (docs/SPEC.md
// §16.12): its stream starts with a FrameRepaint (Repaint, which any
// build can read), then output and resizes as Attach's; a backlog over
// MaxRemotePending becomes a fresh repaint.
func (s *Session) AttachRemote() (*Subscriber, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term == nil {
		return nil, ErrExited
	}
	sub := &Subscriber{p: s, q: newFrameQueue(), remote: true, max: MaxRemotePending}
	if !sub.start() {
		return nil, ErrExited
	}
	s.subs[sub] = struct{}{}
	return sub, nil
}

// start queues the stream's first frame; the pane's mu held.
func (sub *Subscriber) start() bool {
	typ, b, err := sub.state()
	if err != nil {
		sub.p.logf("attach: %v", err)
		return false
	}
	sub.q.push(typ, b)
	return true
}

// state is the frame that rebuilds the pane: a snapshot, or for a remote
// subscriber a repaint; the pane's mu held.
func (sub *Subscriber) state() (proto.FrameType, []byte, error) {
	t := sub.p.terminal()
	if t == nil {
		return 0, nil, ErrExited
	}
	if !sub.remote {
		b, err := t.Snapshot()
		return proto.FrameSnapshot, b, err
	}
	sub.repainted = time.Now()
	vt, err := t.Repaint(RepaintHistory)
	if err != nil {
		return 0, nil, err
	}
	cols, rows := t.Size()
	return proto.FrameRepaint, append(proto.Size(cols, rows), vt...), nil
}

// Next blocks until frames are queued and returns them encoded, ready to
// write. ok is false once the subscriber is closed and drained.
func (sub *Subscriber) Next() (frames []byte, ok bool) { return sub.q.next() }

// Detach removes the subscriber from its pane and ends its stream.
func (sub *Subscriber) Detach() {
	sub.p.lock()
	sub.p.remove(sub)
	sub.dirty = false
	sub.p.unlock()
	sub.q.close()
}

// End ends the stream with a FrameClosed saying reason, as the pane's
// end would: a remote pane whose project stopped being shared.
func (sub *Subscriber) End(reason string) {
	sub.p.lock()
	sub.p.remove(sub)
	sub.dirty = false
	sub.q.push(proto.FrameClosed, []byte(reason))
	sub.p.unlock()
	sub.q.close()
}

// Resyncs counts how often the subscriber fell behind and was resynced:
// its backlog was dropped, so frames taken before are stale.
func (sub *Subscriber) Resyncs() int { return sub.q.resyncCount() }

// Resync drops the backlog and queues the pane's state now: a remote
// pane's prefix+R (docs/SPEC.md §16.12), at most once per RepaintEvery.
func (sub *Subscriber) Resync() {
	sub.p.lock()
	defer sub.p.unlock()
	sub.resyncLocked()
}

// enqueue adds a frame; the pane's mu held.
func (sub *Subscriber) enqueue(typ proto.FrameType, payload []byte) {
	if sub.dirty && (typ == proto.FrameOutput || typ == proto.FrameResize) {
		return // the repaint that waits will include it
	}
	if sub.q.wouldOverflow(len(payload), sub.max) {
		sub.resyncLocked()
		if typ == proto.FrameOutput || typ == proto.FrameResize {
			return // the snapshot already includes it
		}
	}
	sub.q.push(typ, payload)
}

// resyncLocked replaces the backlog with a fresh snapshot or repaint; the
// pane's mu held. A remote subscriber repainted less than RepaintEvery
// ago drops the backlog now and repaints once the time has passed.
func (sub *Subscriber) resyncLocked() {
	if sub.remote {
		if wait := time.Until(sub.repainted.Add(RepaintEvery)); wait > 0 {
			sub.q.reset(0, nil)
			if !sub.dirty {
				sub.dirty = true
				time.AfterFunc(wait, func() {
					sub.p.lock()
					defer sub.p.unlock()
					if sub.dirty {
						sub.dirty = false
						sub.resyncLocked()
					}
				})
			}
			return
		}
	}
	typ, b, err := sub.state()
	if err != nil {
		sub.p.logf("resync: %v", err)
		sub.q.close()
		return
	}
	sub.q.reset(typ, b)
}

func (sub *Subscriber) close() { sub.q.close() }

// The session as a subscriber's pane.

func (s *Session) lock()                   { s.mu.Lock() }
func (s *Session) unlock()                 { s.mu.Unlock() }
func (s *Session) terminal() *emu.Terminal { return s.term }
func (s *Session) remove(sub *Subscriber)  { delete(s.subs, sub) }
func (s *Session) logf(f string, a ...any) {
	s.cfg.Logf("session %s: "+f, append([]any{s.cfg.ID}, a...)...)
}
