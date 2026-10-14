package peer

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Remote panes (docs/SPEC.md §16.12): a paired server watches, and with
// the switch at type types into, the coordinator and thread panes of
// this server's shared projects. peer.attach opens a stream whose id the
// caller picked; binary frames carry it both ways. The server that runs
// the session (far) sends a repaint (emu.Repaint, which any build reads),
// then output and resizes; the one that opened the stream (near) sends
// keys, its size and acks. Frame types say the direction, so each end's
// stream ids are its own.

// CapPane is the link capability of peer.attach and its frames.
const CapPane = "pane-vt"

// Link frame types. Far to near:
const (
	frameOutput  = byte(proto.FrameOutput) // PTY bytes, in order
	frameResize  = byte(proto.FrameResize) // u16 cols, u16 rows
	frameClosed  = byte(proto.FrameClosed) // a fixed word: proto.PaneExited …
	frameRepaint = byte(proto.FrameRepaint)
)

// Near to far:
const (
	frameInput      = byte(proto.FrameInput)  // bytes for the PTY
	frameDetach     = byte(proto.FrameDetach) // ends the stream
	frameAck        = 21                      // u32: output bytes taken since the last ack
	frameSize       = 22                      // u16 cols, u16 rows: typing claims the size
	frameRepaintReq = 23                      // asks for a fresh repaint
)

// Pane stream limits. The far end keeps at most paneWindow bytes of a
// stream's output unacked; the near end acks every paneAckEvery. A link
// serves at most maxPaneStreams streams.
var (
	paneWindow   = 256 << 10
	paneAckEvery = 32 << 10
)

const maxPaneStreams = 8

// PaneHost is what the server lends a link for its panes.
type PaneHost interface {
	// OpenPane subscribes to the pane of session, the coordinator's or a
	// thread's of the shared project, for the paired server c: anything
	// else is proto.ErrNotShared, and a session that isn't running
	// proto.ErrUnknownSession.
	OpenPane(c caller.Caller, project, session string) (PaneSource, proto.PeerSession, error)
}

// PaneSource is one open pane on the server that runs it.
type PaneSource interface {
	// Next and Resyncs are session.Subscriber's (AttachRemote): encoded
	// attach frames, a repaint first; Resyncs moving means the frames
	// taken before are stale.
	Next() ([]byte, bool)
	Resyncs() int
	// Resync queues a fresh repaint.
	Resync()
	// Input types p into the pane, Resize claims its size: only on a
	// stream whose peer may type.
	Input(p []byte) error
	Resize(cols, rows uint16) error
	// Close ends the subscription.
	Close()
}

// PaneSink takes an opened stream's frames on the server that opened it,
// in order, on the link's reader: it mustn't block.
type PaneSink interface {
	// Repaint starts the pane again: a fresh emulator of the size, fed
	// vt; Output frames that follow continue it.
	Repaint(cols, rows uint16, vt []byte)
	Output(p []byte)
	Resize(cols, rows uint16)
	// Closed ends the stream: a fixed word (proto.PaneExited …), or
	// proto.PaneLink when the link dropped.
	Closed(reason string)
}

// paneMux is a session's pane streams: those it serves and those it
// opened.
type paneMux struct {
	s      *session
	mu     sync.Mutex
	served map[uint32]*served
	opened map[uint32]*PaneConn
	next   uint32
	ended  bool
	// window and ackEvery are paneWindow and paneAckEvery when the
	// session began.
	window, ackEvery int
}

func newPaneMux(s *session) *paneMux {
	return &paneMux{s: s, served: map[uint32]*served{}, opened: map[uint32]*PaneConn{},
		window: paneWindow, ackEvery: paneAckEvery}
}

// frame takes a frame from the reader.
func (m *paneMux) frame(stream uint32, typ byte, payload []byte) {
	switch typ {
	case frameOutput, frameResize, frameClosed, frameRepaint:
		m.mu.Lock()
		c := m.opened[stream]
		m.mu.Unlock()
		if c != nil {
			c.frame(typ, payload)
		}
	case frameInput, frameDetach, frameAck, frameSize, frameRepaintReq:
		m.mu.Lock()
		sv := m.served[stream]
		m.mu.Unlock()
		if sv != nil {
			sv.frame(typ, payload)
		}
	}
	// Other types are a newer peer's: ignored.
}

// end closes every stream: the session is over.
func (m *paneMux) end() {
	m.mu.Lock()
	m.ended = true
	ss, opened := m.served, m.opened
	m.served, m.opened = map[uint32]*served{}, map[uint32]*PaneConn{}
	m.mu.Unlock()
	for _, sv := range ss {
		sv.stop()
	}
	for _, c := range opened {
		c.end(proto.PaneLink)
	}
}

// closeServed ends every served stream, telling the peer why.
func (m *paneMux) closeServed(reason string) {
	m.mu.Lock()
	ss := m.served
	m.served = map[uint32]*served{}
	m.mu.Unlock()
	for _, sv := range ss {
		sv.s.sendFrame(sv.id, frameClosed, []byte(reason))
		sv.stop()
	}
}

// The far end: a stream this server serves.

type served struct {
	m      *paneMux
	s      *session
	id     uint32
	src    PaneSource
	typing func() bool

	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	done     bool
}

// serve starts stream id from src; typing says, at each key, whether the
// peer may type.
func (m *paneMux) serve(id uint32, src PaneSource, typing func() bool) error {
	sv := &served{m: m, s: m.s, id: id, src: src, typing: typing}
	sv.cond = sync.NewCond(&sv.mu)
	m.mu.Lock()
	switch {
	case m.ended:
		m.mu.Unlock()
		return errNotUp
	case m.served[id] != nil:
		m.mu.Unlock()
		return proto.Errorf(proto.ErrBadParams, "stream %d is open", id)
	case len(m.served) >= maxPaneStreams:
		m.mu.Unlock()
		return proto.Errorf(proto.ErrRefused, "at most %d panes at once", maxPaneStreams)
	}
	m.served[id] = sv
	m.mu.Unlock()
	go sv.run()
	return nil
}

func (sv *served) stop() {
	sv.mu.Lock()
	sv.done = true
	sv.cond.Broadcast()
	sv.mu.Unlock()
	sv.src.Close()
}

func (sv *served) frame(typ byte, payload []byte) {
	switch typ {
	case frameInput:
		if sv.typing() {
			sv.src.Input(payload)
		}
	case frameSize:
		if c, r, err := proto.ParseSize(payload); err == nil && c > 0 && r > 0 && sv.typing() {
			sv.src.Resize(c, r)
		}
	case frameAck:
		if len(payload) == 4 {
			sv.mu.Lock()
			sv.inflight = max(0, sv.inflight-int(binary.BigEndian.Uint32(payload)))
			sv.cond.Broadcast()
			sv.mu.Unlock()
		}
	case frameRepaintReq:
		sv.src.Resync()
	case frameDetach:
		sv.m.mu.Lock()
		if sv.m.served[sv.id] == sv {
			delete(sv.m.served, sv.id)
		}
		sv.m.mu.Unlock()
		sv.stop()
	}
}

// run sends the source's frames until it ends, the peer detaches or the
// link drops, keeping at most the window unacked. A batch whose frames
// went stale (the backlog became a repaint) is left unsent.
func (sv *served) run() {
	defer func() {
		sv.m.mu.Lock()
		if sv.m.served[sv.id] == sv {
			delete(sv.m.served, sv.id)
		}
		sv.m.mu.Unlock()
		sv.stop()
	}()
	for {
		if !sv.wait() {
			return
		}
		b, ok := sv.src.Next()
		if !ok {
			return
		}
		r0 := sv.src.Resyncs()
		for len(b) >= 5 {
			typ := b[0]
			n := int(binary.BigEndian.Uint32(b[1:5]))
			if 5+n > len(b) {
				return
			}
			p := b[5 : 5+n]
			b = b[5+n:]
			var err error
			switch proto.FrameType(typ) {
			case proto.FrameRepaint:
				if len(p) < 4 {
					return
				}
				err = sv.data(frameRepaint, p[:4], p[4:], r0)
			case proto.FrameOutput:
				err = sv.data(frameOutput, nil, p, r0)
			case proto.FrameResize:
				err = sv.s.sendFrame(sv.id, frameResize, p)
			case proto.FrameClosed:
				reason := string(p)
				switch reason {
				case proto.PaneNotShared, proto.PaneOff, proto.PaneStopping:
				default:
					reason = proto.PaneExited
				}
				sv.s.sendFrame(sv.id, frameClosed, []byte(reason))
				return
			}
			if err == errStale {
				break
			}
			if err != nil {
				return
			}
		}
	}
}

// errStale: the source resynced while a batch was being sent.
var errStale = fmt.Errorf("stale")

// wait waits for room in the window; false once the stream is done.
func (sv *served) wait() bool {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	for sv.inflight >= sv.m.window && !sv.done {
		sv.cond.Wait()
	}
	return !sv.done
}

// data sends head and data as a frame of typ, then output frames, each
// at most frameChunk, waiting for the window between them.
func (sv *served) data(typ byte, head, data []byte, r0 int) error {
	for first := true; first || len(data) > 0; first = false {
		if !first {
			typ, head = frameOutput, nil
			if !sv.wait() {
				return errNotUp
			}
		}
		if sv.src.Resyncs() != r0 && typ == frameOutput {
			return errStale
		}
		n := min(len(data), frameChunk-len(head))
		if err := sv.s.sendFrame(sv.id, typ, append(append([]byte(nil), head...), data[:n]...)); err != nil {
			return err
		}
		sv.mu.Lock()
		sv.inflight += n
		sv.mu.Unlock()
		data = data[n:]
	}
	return nil
}

// The near end: a stream this server opened.

// PaneConn is a pane stream this server opened on a paired server.
type PaneConn struct {
	m    *paneMux
	s    *session
	id   uint32
	sink PaneSink
	// Result is peer.attach's answer.
	Result proto.PeerAttachResult

	mu      sync.Mutex
	unacked int
	closed  bool
	ack     chan struct{}
}

// open opens a stream on session p.Session of the peer, frames to sink.
func (m *paneMux) open(ctx context.Context, p proto.PeerAttachParams, sink PaneSink) (*PaneConn, error) {
	c := &PaneConn{m: m, s: m.s, sink: sink, ack: make(chan struct{}, 1)}
	m.mu.Lock()
	if m.ended {
		m.mu.Unlock()
		return nil, errNotUp
	}
	m.next++
	c.id = m.next
	m.opened[c.id] = c
	m.mu.Unlock()
	p.Stream = c.id
	if err := m.s.call(ctx, proto.MethodPeerAttach, p, &c.Result); err != nil {
		m.mu.Lock()
		if m.opened[c.id] == c {
			delete(m.opened, c.id)
		}
		m.mu.Unlock()
		return nil, err
	}
	go c.acks()
	return c, nil
}

func (c *PaneConn) frame(typ byte, payload []byte) {
	switch typ {
	case frameRepaint:
		if cols, rows, err := proto.ParseSize(payload[:min(4, len(payload))]); err == nil {
			c.sink.Repaint(cols, rows, payload[4:])
			c.took(len(payload) - 4)
		}
	case frameOutput:
		c.sink.Output(payload)
		c.took(len(payload))
	case frameResize:
		if cols, rows, err := proto.ParseSize(payload); err == nil {
			c.sink.Resize(cols, rows)
		}
	case frameClosed:
		c.m.mu.Lock()
		if c.m.opened[c.id] == c {
			delete(c.m.opened, c.id)
		}
		c.m.mu.Unlock()
		c.end(string(payload))
	}
}

// took counts n bytes fed to the sink, waking the acks every
// paneAckEvery.
func (c *PaneConn) took(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.unacked += n
	if c.unacked >= c.m.ackEvery {
		select {
		case c.ack <- struct{}{}:
		default:
		}
	}
}

// acks sends the acks, off the reader: a write that waits mustn't stop
// the link reading.
func (c *PaneConn) acks() {
	for range c.ack {
		c.mu.Lock()
		n, closed := c.unacked, c.closed
		c.unacked = 0
		c.mu.Unlock()
		if closed {
			return
		}
		if n > 0 && c.s.sendFrame(c.id, frameAck, binary.BigEndian.AppendUint32(nil, uint32(n))) != nil {
			return
		}
	}
}

// end tells the sink the stream is over, once.
func (c *PaneConn) end(reason string) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.ack)
	c.mu.Unlock()
	c.sink.Closed(reason)
}

// Input sends keys, in frames of at most frameChunk.
func (c *PaneConn) Input(p []byte) error {
	for len(p) > 0 {
		n := min(len(p), frameChunk)
		if err := c.send(frameInput, p[:n]); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

// Size claims the pane's size for this server's console that typed.
func (c *PaneConn) Size(cols, rows uint16) error { return c.send(frameSize, proto.Size(cols, rows)) }

// Repaint asks for a fresh repaint.
func (c *PaneConn) Repaint() error { return c.send(frameRepaintReq, nil) }

// Close detaches; the sink hears nothing more.
func (c *PaneConn) Close() {
	c.m.mu.Lock()
	open := c.m.opened[c.id] == c
	if open {
		delete(c.m.opened, c.id)
	}
	c.m.mu.Unlock()
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.ack)
	}
	c.mu.Unlock()
	if open {
		c.s.sendFrame(c.id, frameDetach, nil)
	}
}

func (c *PaneConn) send(typ byte, p []byte) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errNotUp
	}
	return c.s.sendFrame(c.id, typ, p)
}

// attach answers a paired server's peer.attach: only while this server's
// switch for it is view or type, and only for a shared project's
// coordinator or thread (PaneHost).
func (d *dispatcher) attach(ctx context.Context, raw json.RawMessage) (any, error) {
	var p proto.PeerAttachParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.attach: %v", err)
	}
	if p.Project == "" || p.Session == "" || p.Stream == 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.attach needs a project, a session and a stream")
	}
	s := sessionOf(ctx)
	if s == nil {
		return nil, errNotUp
	}
	if lv := d.level(); lv != proto.PanesView && lv != proto.PanesType {
		return nil, proto.Errorf(proto.ErrPanesOff, "this server doesn't share panes with yours")
	}
	src, info, err := d.panes.OpenPane(d.caller, p.Project, p.Session)
	if err != nil {
		return nil, err
	}
	typing := func() bool { return d.level() == proto.PanesType }
	if err := s.panes.serve(p.Stream, src, typing); err != nil {
		src.Close()
		return nil, err
	}
	return proto.PeerAttachResult{Input: typing(), Session: info}, nil
}
