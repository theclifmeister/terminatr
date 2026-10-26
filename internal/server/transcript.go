package server

// transcript.watch (docs/SPEC.md §3.3, Transcript): a session's
// conversation as typed items, read from the file its agent's
// [jsonl_tail] follows through the manifest's [transcript] table. The
// human's only, and never a paired server's (peer.Exporter doesn't
// carry it). Bounded: a reset reads at most transcriptBacklogRead of the
// file's end and sends the last proto.TranscriptBacklog items, and the
// stream reads on only while fewer than its window of items are unacked:
// the file is the backlog, so a slow client costs the server an offset.

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// maxTranscripts bounds the transcript.watch streams open at once.
const maxTranscripts = 32

const (
	// transcriptBacklogRead is how much of the file's end a reset reads.
	transcriptBacklogRead = 4 << 20
	// transcriptRead bounds one read of what was appended, and so the
	// longest line read; a longer one (a tool result holding a large
	// file) is skipped, as the tailer skips it.
	transcriptRead = 8 << 20
	// driftUnits is how many of the latest blocks the drift check looks
	// at: the stream is unmapped while driftMin of them, and a quarter
	// at least, are ones the table doesn't know.
	driftUnits = 64
	driftMin   = 3
)

// transcriptWindow and transcriptPoll are variables for tests.
var (
	transcriptWindow = proto.TranscriptWindow
	transcriptPoll   = 500 * time.Millisecond
)

func (s *Server) serveTranscript(c net.Conn, br *bufio.Reader, req proto.Request) {
	var p proto.TranscriptWatchParams
	if perr := decodeParams(req.Params, &p); perr != nil {
		refuse(c, req, perr)
		return
	}
	if p.Session == "" {
		refuse(c, req, proto.Errorf(proto.ErrBadParams, "transcript.watch needs a session"))
		return
	}
	sess, perr := s.session(p.Session)
	if perr != nil {
		refuse(c, req, perr)
		return
	}
	if n := s.transcripts.Add(1); n > maxTranscripts {
		s.transcripts.Add(-1)
		refuse(c, req, proto.Errorf(proto.ErrRefused, "at most %d transcripts at once", maxTranscripts))
		return
	}
	defer s.transcripts.Add(-1)
	w := &transcriptWatch{s: s, sess: sess, c: c, window: transcriptWindow, wake: make(chan struct{}, 1)}
	if a := sess.Agent(); a != nil {
		w.agent = a.Name()
		if m := agent.ManifestOf(a); m != nil {
			w.tr = m.Transcript
		}
	}
	w.root, w.path = sess.Transcript()
	st := w.status()
	if !answer(c, req, proto.TranscriptWatchResult{Session: s.info(sess), Status: st, Window: w.window}) {
		return
	}
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			var in proto.TranscriptInput
			if proto.ReadLine(br, &in) != nil || in.Detach {
				return
			}
			if in.Ack > 0 {
				w.acked(in.Ack)
			}
		}
	}()
	w.run(gone)
}

// transcriptWatch is one transcript.watch stream. Only run's goroutine
// touches it, but for inflight.
type transcriptWatch struct {
	s     *Server
	sess  *session.Session
	c     net.Conn
	tr    *agent.Transcript // nil: unmapped
	agent string

	root, path string
	off        int64
	skip       bool // off is inside a line too long to read
	version    string
	units      []bool // the latest blocks: true for one the table doesn't know
	todos      []agent.Todo
	sent       proto.TranscriptStatus

	window   int
	wake     chan struct{}
	mu       sync.Mutex
	inflight int
}

// run sends the conversation until the client leaves (gone), the
// session ends or a write fails.
func (w *transcriptWatch) run(gone <-chan struct{}) {
	t := time.NewTicker(transcriptPoll)
	defer t.Stop()
	if !w.reset() {
		return
	}
	for {
		select {
		case <-gone:
			return
		case <-w.sess.Done():
			reason := proto.PaneExited
			if w.s.isStopping() {
				reason = proto.PaneStopping
			}
			proto.WriteLine(w.c, proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: reason})
			return
		case <-t.C:
		case <-w.wake:
		}
		if !w.look() {
			return
		}
	}
}

// look sends what changed: a reset for another file, the lines appended,
// the todo list, the status. False when a write failed.
func (w *transcriptWatch) look() bool {
	if w.tr == nil {
		return true
	}
	if root, path := w.sess.Transcript(); root != w.root || path != w.path {
		w.root, w.path = root, path
		return w.reset()
	}
	items, ok := w.follow()
	if !ok {
		return w.reset()
	}
	items = append(items, w.todosChanged()...)
	if len(items) > 0 {
		w.took(len(items))
		if proto.WriteLine(w.c, proto.TranscriptEvent{Event: proto.EventTranscriptItems, Items: items}) != nil {
			return false
		}
	}
	if st := w.status(); st != w.sent {
		w.sent = st
		return proto.WriteLine(w.c, proto.TranscriptEvent{Event: proto.EventTranscriptStatus, Status: &st}) == nil
	}
	return true
}

// reset sends the conversation from the start: the last items of the
// file's end, and the todo list.
func (w *transcriptWatch) reset() bool {
	w.off, w.skip, w.version, w.units, w.todos = 0, false, "", nil, nil
	var items []proto.TranscriptItem
	if w.tr != nil && w.path != "" {
		items = w.backlog()
	}
	skipped := max(0, len(items)-proto.TranscriptBacklog)
	items = items[skipped:]
	if w.tr != nil {
		items = append(items, w.todosChanged()...)
	}
	st := w.status()
	w.sent = st
	w.took(len(items))
	return proto.WriteLine(w.c, proto.TranscriptEvent{Event: proto.EventTranscriptReset, Items: items, Skipped: skipped, Status: &st}) == nil
}

// backlog reads the items of the file's last transcriptBacklogRead
// bytes, and sets off after them.
func (w *transcriptWatch) backlog() []proto.TranscriptItem {
	f, fi, err := session.OpenTranscript(w.root, w.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	start := max(0, fi.Size()-transcriptBacklogRead)
	data := make([]byte, fi.Size()-start)
	n, _ := f.ReadAt(data, start)
	data = data[:n]
	if start > 0 {
		// The first line is likely cut: from the next.
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			w.off = start + int64(n)
			return nil
		}
		data, start = data[i+1:], start+int64(i)+1
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		w.off = start
		return nil
	}
	var items []proto.TranscriptItem
	for line := range bytes.SplitSeq(data[:end], []byte{'\n'}) {
		items = append(items, w.mapLine(line)...)
	}
	w.off = start + int64(end) + 1
	return items
}

// follow reads the whole lines appended since off, while the window
// has room; false when the file shrank (truncated or replaced).
func (w *transcriptWatch) follow() ([]proto.TranscriptItem, bool) {
	if w.path == "" || w.room() <= 0 {
		return nil, true
	}
	f, fi, err := session.OpenTranscript(w.root, w.path)
	if err != nil {
		return nil, true
	}
	defer f.Close()
	if fi.Size() < w.off {
		return nil, false
	}
	if fi.Size() == w.off {
		return nil, true
	}
	data := make([]byte, min(fi.Size()-w.off, transcriptRead))
	n, _ := f.ReadAt(data, w.off)
	data = data[:n]
	start := 0
	if w.skip {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			w.off += int64(n)
			return nil, true
		}
		start, w.skip = i+1, false
	}
	lines := data[start:]
	end := bytes.LastIndexByte(lines, '\n')
	if end < 0 {
		if len(lines) == transcriptRead {
			w.off, w.skip = w.off+int64(n), true
		} else {
			w.off += int64(start)
		}
		return nil, true
	}
	off := w.off + int64(start)
	var items []proto.TranscriptItem
	for line := range bytes.SplitSeq(lines[:end], []byte{'\n'}) {
		if len(items) >= w.room() {
			break // the rest waits for acks
		}
		items = append(items, w.mapLine(line)...)
		off += int64(len(line)) + 1
	}
	w.off = off
	return items, true
}

// mapLine is one line's items, keeping its version and its blocks for
// the drift check.
func (w *transcriptWatch) mapLine(line []byte) []proto.TranscriptItem {
	l := w.tr.Map(line)
	if l.Version != "" {
		w.version = l.Version
	}
	for i := range l.Units {
		w.units = append(w.units, i < l.Unknown)
	}
	if over := len(w.units) - driftUnits; over > 0 {
		w.units = w.units[over:]
	}
	return l.Items
}

// todosChanged is a todos item when the agent's list changed since the
// last one sent.
func (w *transcriptWatch) todosChanged() []proto.TranscriptItem {
	st, ok := w.sess.AgentState()
	if !ok || slices.Equal(st.Todos, w.todos) {
		return nil
	}
	w.todos = slices.Clone(st.Todos)
	return []proto.TranscriptItem{agent.TranscriptTodos(st.Todos)}
}

// status is the stream's status now.
func (w *transcriptWatch) status() proto.TranscriptStatus {
	st := proto.TranscriptStatus{State: proto.TranscriptMapped, Agent: w.agent, Version: w.version}
	who := cmp.Or(w.agent, "this session")
	switch {
	case w.tr == nil:
		st.State, st.Reason = proto.TranscriptUnmapped, fmt.Sprintf("tm has no conversation view for %s", who)
	case w.path == "":
		st.State, st.Reason = proto.TranscriptWaiting, fmt.Sprintf("%s hasn't named its transcript yet", who)
	case !w.tr.TranscriptVersion(w.version):
		st.State, st.Reason = proto.TranscriptUnmapped, fmt.Sprintf("tm's conversation view wasn't checked on %s %s", who, w.version)
	case w.drifted():
		st.State, st.Reason = proto.TranscriptUnmapped, fmt.Sprintf("%s's transcript has lines tm can't read: its format changed", who)
	}
	return st
}

// drifted reports whether many of the latest blocks are ones the table
// doesn't know.
func (w *transcriptWatch) drifted() bool {
	n := 0
	for _, u := range w.units {
		if u {
			n++
		}
	}
	return n >= driftMin && n*4 >= len(w.units)
}

func (w *transcriptWatch) room() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.window - w.inflight
}

func (w *transcriptWatch) took(n int) {
	w.mu.Lock()
	w.inflight += n
	w.mu.Unlock()
}

// acked takes n items off the window, and looks again at once.
func (w *transcriptWatch) acked(n int) {
	w.mu.Lock()
	w.inflight = max(0, w.inflight-n)
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
