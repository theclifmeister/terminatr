package server

// transcript.watch (docs/SPEC.md §3.3, Transcript): a session's
// conversation as typed items, read from the file its agent's
// [jsonl_tail] follows through the manifest's [transcript] table. The
// human's only; a paired server reads the same stream only through
// peer.transcript, gated (peertranscript.go, §16.14). Bounded: a reset reads at most transcriptBacklogRead of the
// file's end and sends the last proto.TranscriptBacklog items, and the
// stream reads on only while fewer than its window of items are unacked:
// the file is the backlog, so a slow client costs the server an offset.

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
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
	if _, _, ok := remoteID(p.Session); ok {
		s.serveRemoteTranscript(c, br, req, s.remoteCanon(p.Session))
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
	w := s.newTranscriptWatch(sess, func(ev proto.TranscriptEvent) error { return proto.WriteLine(c, ev) })
	if p.Tail > 0 {
		w.tail = min(p.Tail, proto.TranscriptBacklog)
	}
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

// newTranscriptWatch is a stream of sess's conversation, its events to
// send.
func (s *Server) newTranscriptWatch(sess *session.Session, send func(proto.TranscriptEvent) error) *transcriptWatch {
	w := &transcriptWatch{s: s, sess: sess, send: send, window: transcriptWindow, tail: proto.TranscriptBacklog, wake: make(chan struct{}, 1)}
	if a := sess.Agent(); a != nil {
		w.agent = a.Name()
		if m := agent.ManifestOf(a); m != nil {
			w.tr = m.Transcript
		}
	}
	w.root, w.path = sess.Transcript()
	return w
}

// transcriptWatch is one transcript.watch stream. Only run's goroutine
// touches it, but for inflight.
type transcriptWatch struct {
	s     *Server
	sess  *session.Session
	send  func(proto.TranscriptEvent) error
	tr    *agent.Transcript // nil: unmapped
	agent string
	// shared, when set, is asked every paneShareCheck whether the
	// stream may go on: "" yes, else the closed reason (a paired
	// server's, whose project may stop being shared).
	shared func() string

	root, path string
	off        int64
	skip       bool // off is inside a line too long to read
	version    string
	units      []bool // the latest blocks: true for one the table doesn't know
	todos      []agent.Todo
	sent       proto.TranscriptStatus

	window int
	// tail is how many of the last items a reset sends.
	tail     int
	wake     chan struct{}
	mu       sync.Mutex
	inflight int
}

// run sends the conversation until the client leaves (gone), the
// session ends or a write fails.
func (w *transcriptWatch) run(gone <-chan struct{}) {
	t := time.NewTicker(transcriptPoll)
	defer t.Stop()
	var share <-chan time.Time
	if w.shared != nil {
		st := time.NewTicker(paneShareCheck)
		defer st.Stop()
		share = st.C
	}
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
			w.send(proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: reason})
			return
		case <-share:
			if reason := w.shared(); reason != "" {
				w.send(proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: reason})
				return
			}
			continue
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
		for _, run := range splitItems(items, proto.TranscriptEventBytes) {
			if w.send(proto.TranscriptEvent{Event: proto.EventTranscriptItems, Items: run}) != nil {
				return false
			}
		}
	}
	if st := w.status(); st != w.sent {
		w.sent = st
		return w.send(proto.TranscriptEvent{Event: proto.EventTranscriptStatus, Status: &st}) == nil
	}
	return true
}

// reset sends the conversation from the start: the last items of the
// file's end, and the todo list.
func (w *transcriptWatch) reset() bool {
	w.off, w.skip, w.version, w.units, w.todos = 0, false, "", nil, nil
	var items []proto.TranscriptItem
	var at []itemAt
	var from int64
	if w.tr != nil && w.path != "" {
		items, at, from = w.backlog()
	}
	var todos []proto.TranscriptItem
	if w.tr != nil {
		todos = w.todosChanged()
	}
	skipped := max(0, len(items)-w.tail)
	skipped += fitFrom(items[skipped:], proto.TranscriptEventBytes-itemsBytes(todos))
	items = items[skipped:]
	before := ""
	switch {
	case skipped > 0:
		before = at[skipped].String()
	case from > 0:
		before = itemAt{off: from}.String()
	}
	items = append(items, todos...)
	st := w.status()
	w.sent = st
	w.took(len(items))
	return w.send(proto.TranscriptEvent{Event: proto.EventTranscriptReset, Items: items, Skipped: skipped, Before: before, Status: &st}) == nil
}

// itemAt is where an item is in the transcript: the n-th item of the
// line at off. As a cursor (Before) it means the items before it.
type itemAt struct {
	off int64
	n   int
}

func (a itemAt) String() string { return strconv.FormatInt(a.off, 10) + "." + strconv.Itoa(a.n) }

// parseItemAt reads a cursor back.
func parseItemAt(s string) (itemAt, bool) {
	o, n, ok := strings.Cut(s, ".")
	off, err1 := strconv.ParseInt(o, 10, 64)
	i, err2 := strconv.Atoi(n)
	return itemAt{off, i}, ok && err1 == nil && err2 == nil && off >= 0 && i >= 0
}

// backlog reads the items of the file's last transcriptBacklogRead
// bytes, and sets off after them: the items, where each is, and where
// the first whole line read starts (0: the file's start).
func (w *transcriptWatch) backlog() ([]proto.TranscriptItem, []itemAt, int64) {
	f, fi, err := session.OpenTranscript(w.root, w.path)
	if err != nil {
		return nil, nil, 0
	}
	defer f.Close()
	items, at, from, end := w.readItems(f, max(0, fi.Size()-transcriptBacklogRead), fi.Size(), nil)
	w.off = end
	return items, at, from
}

// readItems maps the whole lines of f between start and stop (a cut
// first line skipped when start isn't 0), the items before before only
// when it is set. It returns the items, where each is, where the first
// whole line starts, and where the last ends.
func (w *transcriptWatch) readItems(f io.ReaderAt, start, stop int64, before *itemAt) ([]proto.TranscriptItem, []itemAt, int64, int64) {
	data := make([]byte, stop-start)
	n, _ := f.ReadAt(data, start)
	data = data[:n]
	if start > 0 {
		// The first line is likely cut: from the next.
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return nil, nil, start + int64(n), start + int64(n)
		}
		data, start = data[i+1:], start+int64(i)+1
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, nil, start, start
	}
	var items []proto.TranscriptItem
	var at []itemAt
	off := start
	for line := range bytes.SplitSeq(data[:end], []byte{'\n'}) {
		if before != nil && off > before.off {
			break
		}
		got := w.mapLine(line, off)
		if before != nil && off == before.off {
			got = got[:min(len(got), before.n)]
		}
		for i := range got {
			at = append(at, itemAt{off, i})
		}
		items = append(items, got...)
		off += int64(len(line)) + 1
	}
	return items, at, start, start + int64(end) + 1
}

// transcriptPage is transcript.page's: the items of p.Session's
// conversation before the cursor p.Before.
func (s *Server) transcriptPage(p proto.TranscriptPageParams, _ callCtx) (any, *proto.Error) {
	if _, _, ok := remoteID(p.Session); ok {
		return nil, proto.Errorf(proto.ErrRefused, "transcript.page doesn't read a paired server's conversation yet")
	}
	before, ok := parseItemAt(p.Before)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "before %q isn't a reset's or a page's cursor", p.Before)
	}
	limit := proto.TranscriptBacklog
	if p.Limit > 0 {
		limit = min(p.Limit, limit)
	}
	sess, perr := s.session(p.Session)
	if perr != nil {
		return nil, perr
	}
	w := s.newTranscriptWatch(sess, nil)
	res := proto.TranscriptPageResult{Items: []proto.TranscriptItem{}}
	if w.tr == nil || w.path == "" {
		return res, nil
	}
	f, fi, err := session.OpenTranscript(w.root, w.path)
	if err != nil {
		return res, nil
	}
	defer f.Close()
	if before.off > fi.Size() {
		return nil, proto.Errorf(proto.ErrBadParams, "before %q is past the transcript's end: it was replaced; reset", p.Before)
	}
	// The cursor's own line, when some of its items come before it.
	stop := before.off
	if before.n > 0 {
		var line [64 << 10]byte
		for stop < fi.Size() {
			n, _ := f.ReadAt(line[:min(int64(len(line)), fi.Size()-stop)], stop)
			if i := bytes.IndexByte(line[:n], '\n'); i >= 0 {
				stop += int64(i) + 1
				break
			}
			if n == 0 {
				break
			}
			stop += int64(n)
		}
	}
	start := max(0, before.off-transcriptBacklogRead)
	items, at, from, _ := w.readItems(f, start, stop, &before)
	cut := max(0, len(items)-limit)
	cut += fitFrom(items[cut:], proto.TranscriptEventBytes)
	res.Items = items[cut:]
	switch {
	case cut > 0:
		res.Before = at[cut].String()
	case from > 0 && len(items) > 0:
		res.Before = at[0].String()
	case start > 0:
		// nothing whole in the window (a line longer than it): before it
		res.Before = itemAt{off: start}.String()
	}
	return res, nil
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
		items = append(items, w.mapLine(line, off)...)
		off += int64(len(line)) + 1
	}
	w.off = off
	return items, true
}

// mapLine is one line's items, the line at off, keeping its version and
// its blocks for the drift check. Their files' refs name the line
// (fileRef).
func (w *transcriptWatch) mapLine(line []byte, off int64) []proto.TranscriptItem {
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
	ref := ""
	for i := range l.Items {
		it := &l.Items[i]
		if it.Kind == proto.ItemPrompt {
			it.From = w.s.sent.from(w.sess.ID(), it.Text)
		}
		for j := range it.Files {
			if ref == "" {
				ref = lineRef(line, off)
			}
			it.Files[j].Ref = ref + it.Files[j].Ref
		}
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

// itemBytes is it's size in JSON, with the comma after it.
func itemBytes(it proto.TranscriptItem) int {
	b, _ := json.Marshal(it)
	return len(b) + 1
}

// itemsBytes is the size of items in JSON, commas included.
func itemsBytes(items []proto.TranscriptItem) int {
	n := 0
	for _, it := range items {
		n += itemBytes(it)
	}
	return n
}

// fitFrom is where the last of items that fit in budget bytes of JSON
// start: 0 when all fit. The last item always goes (an item is bounded,
// TranscriptItemText), so a reset or page is never empty for it.
func fitFrom(items []proto.TranscriptItem, budget int) int {
	n := 0
	for i := len(items) - 1; i >= 0; i-- {
		n += itemBytes(items[i])
		if n > budget && i < len(items)-1 {
			return i + 1
		}
	}
	return 0
}

// splitItems cuts items into runs of at most budget bytes of JSON each,
// in order (an item over it alone in its run).
func splitItems(items []proto.TranscriptItem, budget int) [][]proto.TranscriptItem {
	var runs [][]proto.TranscriptItem
	start, n := 0, 0
	for i, it := range items {
		b := itemBytes(it)
		if n+b > budget && i > start {
			runs = append(runs, items[start:i])
			start, n = i, 0
		}
		n += b
	}
	return append(runs, items[start:])
}
