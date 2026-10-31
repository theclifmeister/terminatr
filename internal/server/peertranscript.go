package server

// This server's conversations for paired servers (docs/SPEC.md §16.14):
// a shared project's coordinator and threads, read through
// peer.transcript while the pane switch for that peer is view or write
// and its conversations switch is on (the link checks both). What
// crosses is exactly what this server's own app reads with
// transcript.watch: the items of the agent's transcript, and nothing
// from the project folder, its uploads or anywhere else.

import (
	"bufio"
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// peerTranscripts is the server as peer.TranscriptHost.
type peerTranscripts struct{ s *Server }

func (h peerTranscripts) OpenTranscript(c caller.Caller, slug, id string) (peer.TranscriptSource, proto.PeerTranscriptResult, error) {
	if c.Kind != caller.Peer {
		return nil, proto.PeerTranscriptResult{}, proto.Errorf(proto.ErrRefused, "peer.transcript is a paired server's")
	}
	if !slices.Contains(sharedProjects(), slug) {
		return nil, proto.PeerTranscriptResult{}, errNotShared(slug)
	}
	sess, perr := h.s.session(id)
	if perr != nil {
		return nil, proto.PeerTranscriptResult{}, proto.Errorf(proto.ErrUnknownSession, "no session %q in %s", id, slug)
	}
	info := h.s.info(sess)
	if info.Project != slug || (info.Role != proto.RoleCoordinator && info.Role != proto.RoleThread) {
		// Only the project's coordinator and threads cross, and another
		// session answers as one that isn't there (as OpenPane).
		return nil, proto.PeerTranscriptResult{}, proto.Errorf(proto.ErrUnknownSession, "no session %q in %s", id, slug)
	}
	if n := h.s.transcripts.Add(1); n > maxTranscripts {
		h.s.transcripts.Add(-1)
		return nil, proto.PeerTranscriptResult{}, proto.Errorf(proto.ErrRefused, "at most %d transcripts at once", maxTranscripts)
	}
	src := &transcriptSource{s: h.s, info: info, by: c, gone: make(chan struct{})}
	src.w = h.s.newTranscriptWatch(sess, nil)
	src.w.shared = func() string {
		if !slices.Contains(sharedProjects(), slug) {
			return proto.PaneNotShared
		}
		return ""
	}
	h.s.log.Printf("session %s: paired server %s reads its conversation", id, c.Alias)
	return src, proto.PeerTranscriptResult{Session: proto.PeerSessionOf(info), Status: src.w.status(), Window: src.w.window}, nil
}

// transcriptSource is one of this server's conversations open on a
// paired server.
type transcriptSource struct {
	s    *Server
	w    *transcriptWatch
	info proto.SessionInfo
	by   caller.Caller

	once sync.Once
	gone chan struct{}
}

func (t *transcriptSource) Run(send func(proto.TranscriptEvent) error) {
	t.w.send = send
	t.w.run(t.gone)
}

func (t *transcriptSource) Ack(n int) { t.w.acked(n) }

func (t *transcriptSource) Close() {
	t.once.Do(func() {
		close(t.gone)
		t.s.transcripts.Add(-1)
		t.s.log.Printf("session %s: paired server %s stopped reading its conversation", t.info.ID, t.by.Alias)
	})
}

// A paired server's conversations, this server's side (§16.14): the app
// reads "<alias>/<session>" with transcript.watch as it reads a session
// here, and this server relays the stream from the link (peer.transcript),
// the app's acks back to it. A link that drops opens the stream again
// once it is back, which starts with a reset; anything else ends it,
// with a fixed word the app puts in its own words: the far server's
// (proto.PaneExited, PaneNotShared, PaneOff, ConversationsOff,
// PaneStopping).

// remoteTranscriptQueue bounds the events waiting for the app: the far
// server sends at most its window past the app's acks, so more is a
// broken peer.
const remoteTranscriptQueue = 4 * proto.TranscriptWindow

// serveRemoteTranscript is serveTranscript for a paired server's session.
// session.file on "<alias>/<session>" is refused for now (T772): the
// hook for it is at peer's dispatcher.conversations.
func (s *Server) serveRemoteTranscript(c net.Conn, br *bufio.Reader, req proto.Request, id string) {
	alias, sid, _ := remoteID(id)
	if n := s.transcripts.Add(1); n > maxTranscripts {
		s.transcripts.Add(-1)
		refuse(c, req, proto.Errorf(proto.ErrRefused, "at most %d transcripts at once", maxTranscripts))
		return
	}
	defer s.transcripts.Add(-1)
	r := &remoteTranscript{s: s, id: id, alias: alias, sid: sid, q: &trQueue{wake: make(chan struct{}, 1)}}
	if perr := r.open(); perr != nil {
		refuse(c, req, perr)
		return
	}
	res := r.conn.Result
	info := remoteSessionInfo(id, alias, res.Session, !res.Input)
	if !answer(c, req, proto.TranscriptWatchResult{Session: info, Status: res.Status, Window: res.Window}) {
		r.close()
		return
	}
	s.log.Printf("remote conversation %s: open (%s)", id, map[bool]string{true: "write", false: "view"}[res.Input])
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			var in proto.TranscriptInput
			if proto.ReadLine(br, &in) != nil || in.Detach {
				return
			}
			if in.Ack > 0 {
				r.ack(in.Ack)
			}
		}
	}()
	reason := r.relay(c, gone)
	r.close()
	s.log.Printf("remote conversation %s: closed (%s)", id, reason)
}

// remoteTranscript is one relayed conversation.
type remoteTranscript struct {
	s     *Server
	id    string
	alias string
	q     *trQueue

	mu sync.Mutex
	// sid is the session's id there now; project, role and thread say
	// what it is, to find it again after that server restarts.
	sid, project, role, thread string
	conn                       *peer.TranscriptConn
}

// open opens the stream on the link.
func (r *remoteTranscript) open() *proto.Error {
	r.mu.Lock()
	sid, project, role, thread := r.sid, r.project, r.role, r.thread
	r.mu.Unlock()
	pm, rs, peerOK, ok := r.s.remotes.find(r.alias, sid, project, role, thread)
	switch {
	case !peerOK:
		return proto.Errorf(proto.ErrUnknownSession, "no paired server %q", r.alias)
	case pm.State != proto.LinkUp:
		return proto.Errorf(proto.ErrRefused, "%s is offline", r.alias)
	case !pm.Transcript:
		return errNoRemoteTranscripts(r.alias, pm.Version)
	case !ok:
		return proto.Errorf(proto.ErrUnknownSession, "no session %s on %s", sid, r.alias)
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteOpenWait)
	conn, err := r.s.peers.OpenTranscript(ctx, r.alias, proto.PeerTranscriptParams{Project: rs.project, Session: rs.s.ID}, r.q)
	cancel()
	if err != nil {
		return r.openError(err, pm.Version)
	}
	r.mu.Lock()
	r.conn = conn
	r.sid, r.project, r.role, r.thread = rs.s.ID, rs.project, rs.s.Role, rs.s.Thread
	r.mu.Unlock()
	return nil
}

func errNoRemoteTranscripts(alias, version string) *proto.Error {
	return proto.Errorf(proto.ErrRefused, "%s runs tm %s, which doesn't share conversations; update tm there to read them here", alias, version)
}

// openError is a failed peer.transcript in this server's words.
func (r *remoteTranscript) openError(err error, version string) *proto.Error {
	var pe *proto.Error
	switch {
	case errors.Is(err, peer.ErrNoTranscripts):
		return errNoRemoteTranscripts(r.alias, version)
	case errors.As(err, &pe) && pe.Code == proto.ErrConversationsOff:
		return proto.Errorf(proto.ErrConversationsOff, "%s doesn't share conversations with this server", r.alias)
	case errors.As(err, &pe) && pe.Code == proto.ErrNotShared:
		return proto.Errorf(proto.ErrNotShared, "%s doesn't share that project any more", r.alias)
	case errors.As(err, &pe):
		return pe
	}
	return proto.Errorf(proto.ErrRefused, "%s: %v", r.alias, err)
}

func (r *remoteTranscript) ack(n int) {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn != nil {
		conn.Ack(n)
	}
}

func (r *remoteTranscript) close() {
	r.mu.Lock()
	conn := r.conn
	r.conn = nil
	r.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// relay writes the stream's events to the app until it ends, the app
// leaves (gone) or a write fails; it returns why.
func (r *remoteTranscript) relay(c net.Conn, gone <-chan struct{}) string {
	for {
		ev, ok := r.q.next(gone)
		if !ok {
			return "the app left"
		}
		if ev.Event == proto.EventTranscriptClosed && ev.Reason == proto.PaneLink {
			r.s.log.Printf("remote conversation %s: the link dropped", r.id)
			if ev = r.reopen(gone); ev.Event == "" {
				continue
			}
		}
		for _, ev := range r.fit(ev) {
			if proto.WriteLine(c, ev) != nil {
				return "write failed"
			}
		}
		if ev.Event == proto.EventTranscriptClosed {
			return ev.Reason
		}
	}
}

// fit is ev in events of at most proto.TranscriptEventBytes of items
// each, as this server's own streams send them, whatever the paired
// server sent (an older tm's reset may be larger): items split, a
// reset's earliest items skipped, which this server acks for the app.
func (r *remoteTranscript) fit(ev proto.TranscriptEvent) []proto.TranscriptEvent {
	switch ev.Event {
	case proto.EventTranscriptItems:
		runs := splitItems(ev.Items, proto.TranscriptEventBytes)
		out := make([]proto.TranscriptEvent, len(runs))
		for i, run := range runs {
			out[i] = proto.TranscriptEvent{Event: ev.Event, Items: run}
		}
		return out
	case proto.EventTranscriptReset:
		if cut := fitFrom(ev.Items, proto.TranscriptEventBytes); cut > 0 {
			ev.Items, ev.Skipped = ev.Items[cut:], ev.Skipped+cut
			r.ack(cut)
		}
	}
	return []proto.TranscriptEvent{ev}
}

// reopen opens the stream again once the link is back: a zero event when
// it did, else the closed event that ends the app's stream.
func (r *remoteTranscript) reopen(gone <-chan struct{}) proto.TranscriptEvent {
	r.close()
	for {
		select {
		case <-gone:
			return proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: proto.PaneLink}
		case <-time.After(remoteRetry):
		}
		perr := r.open()
		if perr == nil {
			r.s.log.Printf("remote conversation %s: back", r.id)
			return proto.TranscriptEvent{}
		}
		reason := ""
		switch {
		case perr.Code == proto.ErrConversationsOff:
			reason = proto.ConversationsOff
		case perr.Code == proto.ErrNotShared:
			reason = proto.PaneNotShared
		case perr.Code == proto.ErrUnknownSession && strings.HasPrefix(perr.Message, "no session "):
			reason = proto.PaneExited
		}
		if reason != "" {
			return proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: reason}
		}
	}
}

// trQueue holds a relayed stream's events for the app's writer: the
// link's reader puts, and mustn't wait.
type trQueue struct {
	mu     sync.Mutex
	events []proto.TranscriptEvent
	items  int
	over   bool
	wake   chan struct{}
}

// Event takes an event from the link.
func (q *trQueue) Event(ev proto.TranscriptEvent) {
	q.mu.Lock()
	switch {
	case q.over:
	case q.items+len(ev.Items) > remoteTranscriptQueue:
		q.over = true
		q.events = append(q.events, proto.TranscriptEvent{Event: proto.EventTranscriptClosed, Reason: proto.PaneExited})
	default:
		q.items += len(ev.Items)
		q.events = append(q.events, ev)
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// next is the next event; false once gone closed.
func (q *trQueue) next(gone <-chan struct{}) (proto.TranscriptEvent, bool) {
	for {
		q.mu.Lock()
		if len(q.events) > 0 {
			ev := q.events[0]
			q.events = q.events[1:]
			q.items -= len(ev.Items)
			if ev.Event == proto.EventTranscriptClosed && ev.Reason == proto.PaneLink {
				// The stream opened again starts afresh.
				q.over = false
			}
			q.mu.Unlock()
			return ev, true
		}
		q.mu.Unlock()
		select {
		case <-gone:
			return proto.TranscriptEvent{}, false
		case <-q.wake:
		}
	}
}
