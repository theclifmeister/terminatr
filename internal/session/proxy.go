package session

import (
	"sync"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Proxy is this server's copy of a paired server's pane (docs/SPEC.md
// §16.12): an emulator fed the far side's repaints and output, which
// consoles attach to as to a session's, with a snapshot and then the
// same bytes. The far side answers the program's queries; the proxy has
// no PTY and answers none.
type Proxy struct {
	id  string
	log func(string, ...any)

	mu     sync.Mutex
	term   *emu.Terminal
	subs   map[*Subscriber]struct{}
	ready  chan struct{}
	closed bool
}

// NewProxy returns a proxy pane called id, empty until its first
// Repaint (Ready).
func NewProxy(id string, logf func(string, ...any)) *Proxy {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Proxy{id: id, log: logf, subs: map[*Subscriber]struct{}{}, ready: make(chan struct{})}
}

// ID is the proxy's id ("alias/session").
func (p *Proxy) ID() string { return p.id }

// Ready is closed once the first repaint came.
func (p *Proxy) Ready() <-chan struct{} { return p.ready }

// Repaint starts the pane again from vt, at cols×rows: every subscriber
// is resynced with a snapshot of it.
func (p *Proxy) Repaint(cols, rows uint16, vt []byte) {
	term, err := emu.New(cols, rows)
	if err != nil {
		p.logf("%v", err)
		return
	}
	term.Write(vt)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		term.Close()
		return
	}
	first := p.term == nil
	if !first {
		p.term.Close()
	}
	p.term = term
	if first {
		close(p.ready)
		return
	}
	for sub := range p.subs {
		sub.resyncLocked()
	}
}

// Output feeds the pane's output.
func (p *Proxy) Output(b []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil || p.closed {
		return
	}
	p.term.Write(b)
	for sub := range p.subs {
		sub.enqueue(proto.FrameOutput, b)
	}
}

// Resize follows the far pane's resize.
func (p *Proxy) Resize(cols, rows uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil || p.closed || cols == 0 || rows == 0 {
		return
	}
	if c, r := p.term.Size(); c == cols && r == rows {
		return
	}
	if err := p.term.Resize(cols, rows); err != nil {
		p.logf("%v", err)
		return
	}
	for sub := range p.subs {
		sub.enqueue(proto.FrameResize, proto.Size(cols, rows))
	}
}

// Size is the pane's size; 0 before the first repaint.
func (p *Proxy) Size() (cols, rows uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return 0, 0
	}
	return p.term.Size()
}

// Close ends the pane: every subscriber gets a FrameClosed with reason.
func (p *Proxy) Close(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	for sub := range p.subs {
		sub.enqueue(proto.FrameClosed, []byte(reason))
		sub.close()
	}
	p.subs = map[*Subscriber]struct{}{}
	if p.term != nil {
		p.term.Close()
		p.term = nil
	}
}

// Attach registers a console's subscriber, as Session.Attach: a
// snapshot, then output and resizes.
func (p *Proxy) Attach() (*Subscriber, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil || p.closed {
		return nil, ErrExited
	}
	sub := &Subscriber{p: p, q: newFrameQueue(), max: MaxPending}
	if !sub.start() {
		return nil, ErrExited
	}
	p.subs[sub] = struct{}{}
	return sub, nil
}

// AttachRemote registers a pane.vt client's subscriber, as
// Session.AttachRemote: a repaint in plain VT, then output and resizes;
// a backlog over MaxRemotePending becomes a fresh repaint.
func (p *Proxy) AttachRemote() (*Subscriber, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil || p.closed {
		return nil, ErrExited
	}
	sub := &Subscriber{p: p, q: newFrameQueue(), remote: true, max: MaxRemotePending, full: true}
	if !sub.start() {
		return nil, ErrExited
	}
	p.subs[sub] = struct{}{}
	return sub, nil
}

// Subscribers counts the attached consoles.
func (p *Proxy) Subscribers() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.subs)
}

// RequestDigest puts the proxy's digest into sub's stream, as
// Session.RequestDigest: the console checks its mirror against this
// copy, the same build's.
func (p *Proxy) RequestDigest(sub *Subscriber) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return ErrExited
	}
	d, err := p.term.Digest()
	if err != nil {
		return err
	}
	sub.enqueue(proto.FrameDigest, []byte(d))
	return nil
}

// Screen is the pane's visible rows, as Session.Read gives them.
func (p *Proxy) Screen() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return "", ErrExited
	}
	return p.term.Screen()
}

// Paste is text as the far program takes a paste now: bracketed if it
// asked for it, its control characters neutralised otherwise.
func (p *Proxy) Paste(text string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return nil, ErrExited
	}
	return emu.Paste(p.term, []byte(text))
}

// The proxy as a subscriber's pane.

func (p *Proxy) lock()                   { p.mu.Lock() }
func (p *Proxy) unlock()                 { p.mu.Unlock() }
func (p *Proxy) terminal() *emu.Terminal { return p.term }
func (p *Proxy) remove(sub *Subscriber)  { delete(p.subs, sub) }
func (p *Proxy) logf(f string, a ...any) {
	p.log("proxy %s: "+f, append([]any{p.id}, a...)...)
}
