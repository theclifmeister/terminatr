package peer

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"slices"
	"sync"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Remote conversations (docs/SPEC.md §16.14): a paired server reads the
// conversations of this server's shared projects' coordinators and
// threads, the items this server's own app reads with transcript.watch,
// while the pane switch for it is view or write and the conversations
// switch is on. peer.transcript opens a stream whose id the caller
// picked; binary frames carry it both ways. The server that runs the
// session (far) sends events, each its JSON cut into part frames and a
// last one; the one that opened it (near) acks the items it took, as
// transcript.watch's client does, and detaches.

// CapTranscript is the link capability of peer.transcript and its
// frames.
const CapTranscript = "transcript"

// Link frame types of transcript streams. Far to near:
const (
	frameTrPart = 40 // a piece of an event's JSON; more follow
	frameTrLast = 41 // the last piece: the event is whole
)

// Near to far:
const (
	frameTrAck    = 42 // u32: items taken since the last ack
	frameTrDetach = 43 // ends the stream
)

// Transcript stream limits: a link serves at most maxTranscriptStreams,
// and an event is at most maxTranscriptEvent bytes of JSON (a reset of
// proto.TranscriptBacklog items of proto.TranscriptItemText each, with
// room for escapes); a bigger one ends the stream.
const (
	maxTranscriptStreams = 8
	maxTranscriptEvent   = 8 << 20
)

// TranscriptHost is what the server lends a link for its conversations.
type TranscriptHost interface {
	// OpenTranscript opens the conversation of session, the
	// coordinator's or a thread's of the shared project, for the paired
	// server c: anything else is proto.ErrNotShared, and a session that
	// isn't running proto.ErrUnknownSession. The result's Input is the
	// dispatcher's to set.
	OpenTranscript(c caller.Caller, project, session string) (TranscriptSource, proto.PeerTranscriptResult, error)
}

// TranscriptSource is one open conversation on the server that runs it.
type TranscriptSource interface {
	// Run sends the conversation's events through send, a reset first,
	// until the session ends (a closed event), send fails or Close.
	Run(send func(proto.TranscriptEvent) error)
	// Ack takes n items off the stream's window.
	Ack(n int)
	// Close ends Run.
	Close()
}

// TranscriptSink takes an opened stream's events on the server that
// opened it, in order, on the link's reader: it mustn't block, and the
// first may come before OpenTranscript returns. The last
// is a closed event: the far server's (its reason a fixed word,
// proto.PaneExited …), or proto.PaneLink when the link dropped.
type TranscriptSink interface {
	Event(ev proto.TranscriptEvent)
}

// trMux is a session's transcript streams: those it serves and those
// it opened.
type trMux struct {
	s      *session
	mu     sync.Mutex
	served map[uint32]*trServed
	opened map[uint32]*TranscriptConn
	next   uint32
	ended  bool
	// closes counts closeServed calls, as paneMux's.
	closes int
}

func newTrMux(s *session) *trMux {
	return &trMux{s: s, served: map[uint32]*trServed{}, opened: map[uint32]*TranscriptConn{}}
}

// frame takes a frame from the reader.
func (m *trMux) frame(stream uint32, typ byte, payload []byte) {
	switch typ {
	case frameTrPart, frameTrLast:
		m.mu.Lock()
		c := m.opened[stream]
		m.mu.Unlock()
		if c != nil {
			c.frame(typ, payload)
		}
	case frameTrAck, frameTrDetach:
		m.mu.Lock()
		sv := m.served[stream]
		if typ == frameTrDetach && sv != nil {
			delete(m.served, stream)
		}
		m.mu.Unlock()
		if sv == nil {
			return
		}
		if typ == frameTrAck && len(payload) == 4 {
			sv.src.Ack(int(binary.BigEndian.Uint32(payload)))
		} else if typ == frameTrDetach {
			sv.stop("")
		}
	}
}

// end closes every stream: the session is over.
func (m *trMux) end() {
	m.mu.Lock()
	m.ended = true
	ss, opened := m.served, m.opened
	m.served, m.opened = map[uint32]*trServed{}, map[uint32]*TranscriptConn{}
	m.mu.Unlock()
	for _, sv := range ss {
		sv.stop("")
	}
	for _, c := range opened {
		c.end(proto.PaneLink)
	}
}

// closeServed ends every served stream, telling the peer why. The
// closed events go after the events being sent, off the caller (the
// Manager's Reload, under its lock): a reset can take a while to write.
func (m *trMux) closeServed(reason string) {
	m.mu.Lock()
	ss := m.served
	m.served = map[uint32]*trServed{}
	m.closes++
	m.mu.Unlock()
	for _, sv := range ss {
		go sv.stop(reason)
	}
}

func (m *trMux) closed() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closes
}

// The far end: a stream this server serves.

type trServed struct {
	m   *trMux
	id  uint32
	src TranscriptSource

	// mu orders the events: a closed event the switch sends goes after
	// the one being sent, and nothing after it.
	mu   sync.Mutex
	done bool
}

// serve starts stream id from src. since is m.closed() from before the
// switches allowed the stream (paneMux.serve).
func (m *trMux) serve(id uint32, src TranscriptSource, since int) error {
	sv := &trServed{m: m, id: id, src: src}
	m.mu.Lock()
	switch {
	case m.ended:
		m.mu.Unlock()
		return errNotUp
	case m.closes != since:
		m.mu.Unlock()
		return errConversationsOff
	case m.served[id] != nil:
		m.mu.Unlock()
		return proto.Errorf(proto.ErrBadParams, "stream %d is open", id)
	case len(m.served) >= maxTranscriptStreams:
		m.mu.Unlock()
		return proto.Errorf(proto.ErrRefused, "at most %d conversations at once", maxTranscriptStreams)
	}
	m.served[id] = sv
	m.mu.Unlock()
	// Its first events may reach the near end before the answer: the
	// near end has the stream open from before it asked.
	go sv.run()
	return nil
}

// run sends the source's events until it ends.
func (sv *trServed) run() {
	sv.src.Run(sv.send)
	sv.m.mu.Lock()
	if sv.m.served[sv.id] == sv {
		delete(sv.m.served, sv.id)
	}
	sv.m.mu.Unlock()
	sv.stop("")
}

// send writes ev as part frames and a last one.
func (sv *trServed) send(ev proto.TranscriptEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if sv.done {
		return errNotUp
	}
	err = sv.write(b)
	if ev.Event == proto.EventTranscriptClosed {
		sv.done = true
	}
	return err
}

// write sends b, an event's JSON; mu held.
func (sv *trServed) write(b []byte) error {
	for {
		n := min(len(b), frameChunk)
		typ := byte(frameTrPart)
		if n == len(b) {
			typ = frameTrLast
		}
		if err := sv.m.s.sendFrame(sv.id, typ, b[:n]); err != nil {
			return err
		}
		if b = b[n:]; len(b) == 0 {
			return nil
		}
	}
}

// stop ends the stream, after a closed event for reason unless it is ""
// or one went.
func (sv *trServed) stop(reason string) {
	sv.mu.Lock()
	if !sv.done && reason != "" {
		b, _ := json.Marshal(proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: reason})
		sv.write(b)
	}
	sv.done = true
	sv.mu.Unlock()
	sv.src.Close()
}

// The near end: a stream this server opened.

// TranscriptConn is a transcript stream this server opened on a paired
// server.
type TranscriptConn struct {
	m    *trMux
	id   uint32
	sink TranscriptSink
	// Result is peer.transcript's answer, cleaned.
	Result proto.PeerTranscriptResult

	// buf gathers an event's pieces, on the link's reader.
	buf []byte

	mu     sync.Mutex
	closed bool
}

// open opens a stream on session p.Session of the peer, events to sink.
func (m *trMux) open(ctx context.Context, p proto.PeerTranscriptParams, sink TranscriptSink) (*TranscriptConn, error) {
	c := &TranscriptConn{m: m, sink: sink}
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
	if err := m.s.call(ctx, proto.MethodPeerTranscript, p, &c.Result); err != nil {
		m.forget(c)
		return nil, err
	}
	c.Result = cleanTranscriptResult(c.Result)
	return c, nil
}

func (m *trMux) forget(c *TranscriptConn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened[c.id] == c {
		delete(m.opened, c.id)
		return true
	}
	return false
}

func (c *TranscriptConn) frame(typ byte, payload []byte) {
	if len(c.buf)+len(payload) > maxTranscriptEvent {
		// More than any event the far server sends: the stream ends.
		c.buf = nil
		if c.m.forget(c) {
			c.m.s.sendFrame(c.id, frameTrDetach, nil)
		}
		c.end(proto.PaneExited)
		return
	}
	c.buf = append(c.buf, payload...)
	if typ != frameTrLast {
		return
	}
	b := c.buf
	c.buf = nil
	var ev proto.TranscriptEvent
	if json.Unmarshal(b, &ev) != nil {
		return
	}
	ev, ok := cleanTranscriptEvent(ev)
	if !ok {
		return
	}
	if ev.Event == proto.EventTranscriptClosed {
		c.m.forget(c)
		c.end(ev.Reason)
		return
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if !closed {
		c.sink.Event(ev)
	}
}

// end tells the sink the stream is over, once.
func (c *TranscriptConn) end(reason string) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	c.sink.Event(proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: reason})
}

// Ack tells the far server n items were taken.
func (c *TranscriptConn) Ack(n int) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed || n <= 0 {
		return nil
	}
	return c.m.s.sendFrame(c.id, frameTrAck, binary.BigEndian.AppendUint32(nil, uint32(n)))
}

// Close detaches; the sink hears nothing more.
func (c *TranscriptConn) Close() {
	open := c.m.forget(c)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if open {
		c.m.s.sendFrame(c.id, frameTrDetach, nil)
	}
}

// transcript answers a paired server's peer.transcript: only while this
// server's pane switch for it is view or write and its conversations
// switch is on, and only for a shared project's coordinator or thread
// (TranscriptHost).
func (d *dispatcher) transcript(ctx context.Context, raw json.RawMessage) (any, error) {
	var p proto.PeerTranscriptParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.transcript: %v", err)
	}
	if p.Project == "" || p.Session == "" || p.Stream == 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "peer.transcript needs a project, a session and a stream")
	}
	s := sessionOf(ctx)
	if s == nil {
		return nil, errNotUp
	}
	since := s.transcripts.closed()
	if !d.conversations() {
		return nil, errConversationsOff
	}
	src, res, err := d.transcripts.OpenTranscript(d.caller, p.Project, p.Session)
	if err != nil {
		return nil, err
	}
	if err := s.transcripts.serve(p.Stream, src, since); err != nil {
		src.Close()
		return nil, err
	}
	res.Input = d.level() == proto.PanesType
	return res, nil
}

// conversations says whether the peer may read conversations now: the
// pane switch is view or write and the conversations switch is on.
//
// TODO(T772): session.file (a file a session's transcript names, SPEC
// §3.3) doesn't cross yet. Across a link it would be its own peer
// request (peer.session-file {project, session, ref}) on this
// dispatcher, refused unless conversations() holds and the host checks
// the project is shared and the session is its coordinator's or a
// thread's (as TranscriptHost.OpenTranscript), streamed over frames as
// the files of §16.13 are; the near server's session.file on
// "<alias>/<session>" would relay it, as serveRemoteTranscript does.
func (d *dispatcher) conversations() bool {
	lv := d.level()
	return (lv == proto.PanesView || lv == proto.PanesType) && d.convOn != nil && d.convOn()
}

var errConversationsOff = proto.Errorf(proto.ErrConversationsOff, "this server doesn't share conversations with yours")

// What a paired server's conversation says is still only its word.

// transcriptKinds are the item kinds a near end passes on.
var transcriptKinds = []string{proto.ItemPrompt, proto.ItemText, proto.ItemTool, proto.ItemResult, proto.ItemNote, proto.ItemTodos}

// cleanTranscriptResult is a peer.transcript answer with its strings
// cleaned.
func cleanTranscriptResult(r proto.PeerTranscriptResult) proto.PeerTranscriptResult {
	w := cleanWatch(proto.PeerWatch{Sessions: []proto.PeerSession{r.Session}})
	r.Session = proto.PeerSession{}
	if len(w.Sessions) == 1 {
		r.Session = w.Sessions[0]
	}
	r.Status = cleanTranscriptStatus(r.Status)
	r.Window = min(max(r.Window, 1), proto.TranscriptWindow)
	return r
}

func cleanTranscriptStatus(st proto.TranscriptStatus) proto.TranscriptStatus {
	return proto.TranscriptStatus{State: word(peerWord, st.State), Reason: textsafe.Line(st.Reason, 200),
		Agent: textsafe.Line(st.Agent, 40), Version: textsafe.Line(st.Version, 40)}
}

// cleanTranscriptEvent is a far server's event with every string
// cleaned and bounded as this server's own are (proto.TranscriptItem),
// and the items of a kind it doesn't know dropped; false for an event
// it doesn't know.
func cleanTranscriptEvent(ev proto.TranscriptEvent) (proto.TranscriptEvent, bool) {
	switch ev.Event {
	case proto.EventTranscriptReset, proto.EventTranscriptItems, proto.EventTranscriptStatus:
	case proto.EventTranscriptClosed:
		switch ev.Reason {
		case proto.PaneExited, proto.PaneNotShared, proto.PaneOff, proto.PaneStopping, proto.ConversationsOff:
		default:
			ev.Reason = proto.PaneExited
		}
		return proto.TranscriptEvent{Event: ev.Event, Reason: ev.Reason}, true
	default:
		return ev, false
	}
	out := proto.TranscriptEvent{Event: ev.Event, Skipped: max(ev.Skipped, 0)}
	if ev.Status != nil {
		st := cleanTranscriptStatus(*ev.Status)
		out.Status = &st
	}
	for _, it := range ev.Items[:min(len(ev.Items), proto.TranscriptWindow)] {
		if !slices.Contains(transcriptKinds, it.Kind) {
			continue
		}
		c := proto.TranscriptItem{Kind: it.Kind, Truncated: it.Truncated, Error: it.Error, Lines: max(it.Lines, 0), At: it.At,
			Tool: textsafe.Line(it.Tool, 80), Call: word(peerWord, it.Call)}
		switch it.Kind {
		case proto.ItemTool, proto.ItemResult:
			c.Text = textsafe.Line(it.Text, proto.TranscriptSummary)
		default:
			c.Text = textsafe.Cut(textsafe.Text(it.Text), proto.TranscriptItemText)
		}
		if it.From == proto.FromTM || it.From == proto.FromCoordinator {
			c.From = it.From
		}
		if it.Kind == proto.ItemResult {
			c.Answers = cleanAnswers(it.Answers)
		}
		for _, t := range it.Todos[:min(len(it.Todos), proto.TranscriptTodos)] {
			c.Todos = append(c.Todos, proto.TranscriptTodo{Text: textsafe.Line(t.Text, proto.TranscriptSummary), Status: word(peerWord, t.Status)})
		}
		out.Items = append(out.Items, c)
	}
	return out, true
}

// cleanAnswers are a far server's answered questions, cleaned and
// bounded as this server's own are (proto.TranscriptAnswer).
func cleanAnswers(in []proto.TranscriptAnswer) []proto.TranscriptAnswer {
	var out []proto.TranscriptAnswer
	line := func(s string) string { return textsafe.Line(s, proto.TranscriptAnswerText) }
	for _, a := range in[:min(len(in), proto.TranscriptAnswers)] {
		c := proto.TranscriptAnswer{Header: line(a.Header), Question: line(a.Question), Typed: line(a.Typed), Notes: line(a.Notes)}
		for _, p := range a.Picked[:min(len(a.Picked), proto.TranscriptPicked)] {
			if p = line(p); p != "" {
				c.Picked = append(c.Picked, p)
			}
		}
		out = append(out, c)
	}
	return out
}
