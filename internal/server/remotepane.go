package server

// Remote panes, this server's side (docs/SPEC.md §16.12): a console shows
// a paired server's coordinator or thread as the session "<alias>/<id>".
// The console never talks to the link: it attaches here as to any
// session, to a proxy pane (session.Proxy) this server keeps of the far
// one, fed by a stream on the link (peer.attach). One stream serves every
// console that shows the pane; it closes remoteLinger after the last
// leaves. A link that drops freezes the pane and drops keys until the
// stream is back; the pane then repaints.

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// Remote pane timing: how long a stream waits for its first repaint, how
// long it outlives its last console, and how often a dropped one tries
// again.
var (
	remoteOpenWait = 10 * time.Second
	remoteLinger   = 10 * time.Second
	remoteRetry    = time.Second
)

// remoteID splits a remote pane's id, "<alias>/<session>"; ok is false
// for this server's own sessions, whose ids have no "/".
func remoteID(id string) (alias, sid string, ok bool) {
	alias, sid, ok = strings.Cut(id, "/")
	return alias, sid, ok && alias != "" && sid != ""
}

// remotes are the remote panes this server shows.
type remotes struct {
	s    *Server
	mu   sync.Mutex
	byID map[string]*remotePane
}

func newRemotes(s *Server) *remotes { return &remotes{s: s, byID: map[string]*remotePane{}} }

// remotePane is one paired server's pane, shown here.
type remotePane struct {
	r     *remotes
	id    string
	alias string
	proxy *session.Proxy
	// connect is held while a stream opens.
	connect sync.Mutex

	mu sync.Mutex
	// sid is the session's id there now; project, role and thread say
	// what it is, to find it again after that server restarts.
	sid, project, role, thread string
	info                       proto.PeerSession
	conn                       *peer.PaneConn
	input                      bool
	// down: the link dropped; the pane is frozen until a stream is back.
	down    bool
	viewers int
	linger  *time.Timer
	closed  bool
}

// remoteSession is the paired server's session as the mirror has it.
type remoteSession struct {
	peer    proto.PeerMirror
	project string
	s       proto.PeerSession
}

// find looks session sid of the paired server alias up in the mirror;
// with sid gone, the project's coordinator (role) or thread (thread)
// now. ok is false when the mirror has none.
func (r *remotes) find(alias, sid, project, role, thread string) (pm proto.PeerMirror, rs remoteSession, peerOK, ok bool) {
	for _, p := range r.s.mirror.List().Peers {
		if p.Alias != alias {
			continue
		}
		pm, peerOK = p, true
		for _, pr := range p.Projects {
			for _, s := range pr.Sessions {
				if s.ID == sid || s.Project == project && project != "" && s.Role == role &&
					(role == proto.RoleCoordinator || s.Thread == thread && thread != "") {
					if s.Role == proto.RoleCoordinator || s.Role == proto.RoleThread {
						return pm, remoteSession{peer: p, project: pr.Project, s: s}, true, true
					}
				}
			}
		}
	}
	return pm, rs, peerOK, false
}

// alive says whether id is a remote pane a view may show: one open here,
// or one the mirror lists.
func (r *remotes) alive(id string) bool {
	alias, sid, ok := remoteID(id)
	if !ok {
		return false
	}
	r.mu.Lock()
	_, open := r.byID[id]
	r.mu.Unlock()
	if open {
		return true
	}
	_, _, _, found := r.find(alias, sid, "", "", "")
	return found
}

// open is the remote pane id, its stream opened now unless it is up.
func (r *remotes) open(id string) (*remotePane, *proto.Error) {
	alias, sid, ok := remoteID(id)
	if !ok {
		return nil, proto.Errorf(proto.ErrUnknownSession, "no session %q", id)
	}
	r.mu.Lock()
	rp := r.byID[id]
	if rp == nil {
		rp = &remotePane{r: r, id: id, alias: alias, sid: sid, proxy: session.NewProxy(id, r.s.log.Printf)}
		r.byID[id] = rp
	}
	r.mu.Unlock()
	if perr := rp.open(); perr != nil {
		rp.mu.Lock()
		fresh := !rp.closed && rp.viewers == 0
		rp.mu.Unlock()
		select {
		case <-rp.proxy.Ready():
		default:
			if fresh {
				// Nothing showed yet: nothing to keep.
				r.drop(rp, "")
			}
		}
		return nil, perr
	}
	return rp, nil
}

// drop forgets rp and closes its proxy with reason ("" closes quietly).
func (r *remotes) drop(rp *remotePane, reason string) {
	r.mu.Lock()
	if r.byID[rp.id] == rp {
		delete(r.byID, rp.id)
	}
	r.mu.Unlock()
	rp.mu.Lock()
	rp.closed = true
	conn := rp.conn
	rp.conn = nil
	if rp.linger != nil {
		rp.linger.Stop()
	}
	rp.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	rp.proxy.Close(reason)
}

// closeAll closes every remote pane: the server is stopping.
func (r *remotes) closeAll() {
	r.mu.Lock()
	all := make([]*remotePane, 0, len(r.byID))
	for _, rp := range r.byID {
		all = append(all, rp)
	}
	r.mu.Unlock()
	for _, rp := range all {
		r.drop(rp, "tm server stopping")
	}
}

// open opens the stream unless it is up, and waits for the pane's first
// repaint.
func (rp *remotePane) open() *proto.Error {
	rp.connect.Lock()
	defer rp.connect.Unlock()
	rp.mu.Lock()
	up, closed := rp.conn != nil, rp.closed
	sid, project, role, thread := rp.sid, rp.project, rp.role, rp.thread
	rp.mu.Unlock()
	if closed {
		return proto.Errorf(proto.ErrUnknownSession, "no session %q", rp.id)
	}
	if !up {
		pm, rs, peerOK, ok := rp.r.find(rp.alias, sid, project, role, thread)
		switch {
		case !peerOK:
			return proto.Errorf(proto.ErrUnknownSession, "no paired server %q", rp.alias)
		case pm.State != proto.LinkUp:
			return proto.Errorf(proto.ErrRefused, "%s is offline", rp.alias)
		case !pm.Pane:
			return proto.Errorf(proto.ErrRefused, "%s runs tm %s, which has no remote panes; update it to open them", rp.alias, pm.Version)
		case !ok:
			return proto.Errorf(proto.ErrUnknownSession, "no session %s on %s", sid, rp.alias)
		}
		cols, rows := rp.proxy.Size()
		ctx, cancel := context.WithTimeout(context.Background(), remoteOpenWait)
		conn, err := rp.r.s.peers.OpenPane(ctx, rp.alias, proto.PeerAttachParams{Project: rs.project, Session: rs.s.ID, Cols: cols, Rows: rows}, rp)
		cancel()
		if err != nil {
			return rp.openError(err)
		}
		rp.mu.Lock()
		if rp.closed {
			rp.mu.Unlock()
			conn.Close()
			return proto.Errorf(proto.ErrUnknownSession, "no session %q", rp.id)
		}
		rp.conn, rp.input, rp.down = conn, conn.Result.Input, false
		rp.sid, rp.project, rp.role, rp.thread, rp.info = rs.s.ID, rs.project, rs.s.Role, rs.s.Thread, conn.Result.Session
		rp.mu.Unlock()
		rp.r.s.log.Printf("remote pane %s: open (%s)", rp.id, map[bool]string{true: "type", false: "view"}[conn.Result.Input])
	}
	select {
	case <-rp.proxy.Ready():
		return nil
	case <-time.After(remoteOpenWait):
		return proto.Errorf(proto.ErrRefused, "%s didn't send the pane in time", rp.alias)
	}
}

// openError is a failed peer.attach in this server's words.
func (rp *remotePane) openError(err error) *proto.Error {
	var pe *proto.Error
	switch {
	case errors.Is(err, peer.ErrNoPanes):
		return proto.Errorf(proto.ErrRefused, "%s's tm has no remote panes; update it to open them", rp.alias)
	case errors.As(err, &pe) && pe.Code == proto.ErrPanesOff:
		return proto.Errorf(proto.ErrPanesOff, "%s doesn't share panes with this server: on %s, tm peer panes <this server> view, or type", rp.alias, rp.alias)
	case errors.As(err, &pe) && pe.Code == proto.ErrNotShared:
		return proto.Errorf(proto.ErrNotShared, "%s doesn't share that project any more", rp.alias)
	case errors.As(err, &pe):
		return pe
	}
	return proto.Errorf(proto.ErrRefused, "%s: %v", rp.alias, err)
}

// The pane as the stream's sink.

func (rp *remotePane) Repaint(cols, rows uint16, vt []byte) { rp.proxy.Repaint(cols, rows, vt) }
func (rp *remotePane) Output(p []byte)                      { rp.proxy.Output(p) }
func (rp *remotePane) Resize(cols, rows uint16)             { rp.proxy.Resize(cols, rows) }

// Closed: the stream ended. A dropped link freezes the pane and tries
// again while a console shows it; anything else ends the pane.
func (rp *remotePane) Closed(reason string) {
	rp.mu.Lock()
	rp.conn = nil
	closed, viewers := rp.closed, rp.viewers
	if reason == proto.PaneLink {
		rp.down = true
	}
	rp.mu.Unlock()
	if closed {
		return
	}
	if reason == proto.PaneLink {
		rp.r.s.log.Printf("remote pane %s: the link dropped", rp.id)
		if viewers > 0 {
			go rp.reconnect()
		}
		return
	}
	rp.end(reason)
}

// end closes the pane for good, in this server's words, and takes it out
// of the views.
func (rp *remotePane) end(reason string) {
	words := map[string]string{
		proto.PaneExited:    "session exited on " + rp.alias,
		proto.PaneNotShared: rp.alias + " stopped sharing " + rp.project,
		proto.PaneOff:       rp.alias + " turned its panes off for this server",
		"gone":              "the session on " + rp.alias + " is gone",
	}[reason]
	if words == "" {
		words = "the pane on " + rp.alias + " closed"
	}
	rp.r.s.log.Printf("remote pane %s: %s", rp.id, words)
	rp.r.drop(rp, words)
	rp.r.s.views.sessionGone(rp.id)
}

// reconnect opens the stream again once the link is back, while a
// console shows the pane.
func (rp *remotePane) reconnect() {
	for {
		time.Sleep(remoteRetry)
		rp.mu.Lock()
		stop := rp.closed || rp.viewers == 0 || rp.conn != nil
		rp.mu.Unlock()
		if stop {
			return
		}
		perr := rp.open()
		switch {
		case perr == nil:
			rp.r.s.log.Printf("remote pane %s: back", rp.id)
			return
		case perr.Code == proto.ErrUnknownSession && strings.HasPrefix(perr.Message, "no session "):
			rp.end("gone")
			return
		case perr.Code == proto.ErrPanesOff:
			rp.end(proto.PaneOff)
			return
		case perr.Code == proto.ErrNotShared:
			rp.end(proto.PaneNotShared)
			return
		}
	}
}

// attached counts a console in; detached counts it out, and the last out
// closes the stream after remoteLinger.
func (rp *remotePane) attached() {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.viewers++
	if rp.linger != nil {
		rp.linger.Stop()
		rp.linger = nil
	}
}

func (rp *remotePane) detached() {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.viewers--
	if rp.viewers > 0 || rp.closed {
		return
	}
	rp.linger = time.AfterFunc(remoteLinger, func() {
		rp.mu.Lock()
		idle := rp.viewers == 0 && !rp.closed
		rp.mu.Unlock()
		if idle {
			rp.r.s.log.Printf("remote pane %s: closed, nobody shows it", rp.id)
			rp.r.drop(rp, "")
		}
	})
}

// keys types p into the far pane: only while the stream may type and is
// up. Keys while the link is down are dropped, never kept for later.
func (rp *remotePane) keys(p []byte) {
	rp.mu.Lock()
	conn, input := rp.conn, rp.input
	rp.mu.Unlock()
	if conn != nil && input {
		conn.Input(p)
	}
}

// size claims the far pane's size for the console that typed; off the
// views' lock, which holds it.
func (rp *remotePane) size(cols, rows uint16) {
	rp.mu.Lock()
	conn, input := rp.conn, rp.input
	rp.mu.Unlock()
	if conn != nil && input {
		go conn.Size(cols, rows)
	}
}

// sessionInfo is the pane as a SessionInfo for the console.
func (rp *remotePane) sessionInfo() proto.SessionInfo {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	s := rp.info
	cols, rows := rp.proxy.Size()
	return proto.SessionInfo{ID: rp.id, Role: s.Role, Agent: s.Agent, Project: s.Project, Thread: s.Thread, Task: s.Task,
		Cols: cols, Rows: rows, Created: s.Created, State: s.State, Reason: s.Reason, TodosDone: s.TodosDone,
		TodosTotal: s.TodosTotal, Current: s.Current, Model: s.Model, Context: s.Context, ContextWindow: s.ContextWindow,
		Server: rp.alias, ViewOnly: !rp.input}
}

// serveRemoteAttach is serveAttach for a remote pane: the console gets
// the proxy's snapshot and stream; its keys go to the far pane.
func (s *Server) serveRemoteAttach(c net.Conn, br *bufio.Reader, id string) {
	rp, perr := s.remotes.open(id)
	if perr != nil {
		writeJSONLine(c, proto.AttachReply{Error: perr})
		return
	}
	sub, err := rp.proxy.Attach()
	if err != nil {
		writeJSONLine(c, proto.AttachReply{Error: proto.Errorf(proto.ErrUnknownSession, "the pane on %s closed", rp.alias)})
		return
	}
	rp.attached()
	defer rp.detached()
	info := rp.sessionInfo()
	if err := writeJSONLine(c, proto.AttachReply{Attached: &info}); err != nil {
		sub.Detach()
		return
	}
	s.log.Printf("remote pane %s: client attached", id)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer c.Close()
		for {
			b, ok := sub.Next()
			if !ok {
				return
			}
			if _, err := c.Write(b); err != nil {
				return
			}
		}
	}()
	reason := "eof"
	var buf []byte
loop:
	for {
		typ, payload, err := proto.ReadFrame(br, buf)
		if err != nil {
			break
		}
		buf = payload
		switch typ {
		case proto.FrameInput:
			rp.keys(payload)
		case proto.FrameDigestReq:
			rp.proxy.RequestDigest(sub)
		case proto.FrameDetach:
			reason = "detach"
			break loop
		}
		// Colours stay the far server's (docs/SPEC.md §16.12).
	}
	sub.Detach()
	<-writerDone
	s.log.Printf("remote pane %s: client left (%s)", id, reason)
}

// remoteRefusal refuses a session.* method on a remote pane: only keys
// cross (docs/SPEC.md §16.12).
func remoteRefusal(id string) *proto.Error {
	alias, _, _ := remoteID(id)
	return proto.Errorf(proto.ErrRefused, "not on %s's panes: open it on %s", alias, alias)
}
