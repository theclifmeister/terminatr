package server

// This server's panes for paired servers (docs/SPEC.md §16.12): a shared
// project's coordinator and threads, watched through peer.attach while
// the pane switch for that peer is view or type, and typed into while it
// is type. The peer's keys are the human's on the far console, but the
// caller stays peer:<alias> everywhere else.

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// paneShareCheck is how often an open remote pane checks that its
// project is still shared.
var paneShareCheck = 2 * time.Second

// peerPanes is the server as peer.PaneHost.
type peerPanes struct{ s *Server }

func (h peerPanes) OpenPane(c caller.Caller, slug, id string) (peer.PaneSource, proto.PeerSession, error) {
	if c.Kind != caller.Peer {
		return nil, proto.PeerSession{}, proto.Errorf(proto.ErrRefused, "peer.attach is a paired server's")
	}
	if !slices.Contains(sharedProjects(), slug) {
		return nil, proto.PeerSession{}, errNotShared(slug)
	}
	sess, perr := h.s.session(id)
	if perr != nil {
		return nil, proto.PeerSession{}, proto.Errorf(proto.ErrUnknownSession, "no session %q in %s", id, slug)
	}
	info := h.s.info(sess)
	if info.Project != slug || (info.Role != proto.RoleCoordinator && info.Role != proto.RoleThread) {
		// Only the project's coordinator and threads cross, and another
		// session answers as one that isn't there.
		return nil, proto.PeerSession{}, proto.Errorf(proto.ErrUnknownSession, "no session %q in %s", id, slug)
	}
	sub, err := sess.AttachRemote()
	if err != nil {
		return nil, proto.PeerSession{}, proto.Errorf(proto.ErrUnknownSession, "no session %q in %s", id, slug)
	}
	src := &paneSource{s: h.s, sess: sess, sub: sub, info: info, by: c, done: make(chan struct{})}
	go src.watchShare()
	h.s.log.Printf("session %s: paired server %s attached", id, c.Alias)
	return src, proto.PeerSessionOf(info), nil
}

// paneSource is one of this server's panes open on a paired server.
type paneSource struct {
	s    *Server
	sess *session.Session
	sub  *session.Subscriber
	info proto.SessionInfo
	by   caller.Caller

	once sync.Once
	done chan struct{}
	// told: this stream has raised its takeover.
	mu   sync.Mutex
	told bool
}

func (p *paneSource) Next() ([]byte, bool) { return p.sub.Next() }
func (p *paneSource) Resyncs() int         { return p.sub.Resyncs() }
func (p *paneSource) Resync()              { p.sub.Resync() }

// Input types keys from the peer's console: the first into a thread's
// pane on this stream tells its coordinator, as a local console's first
// keys do (docs/SPEC.md §4), in this server's words.
func (p *paneSource) Input(b []byte) error {
	p.mu.Lock()
	tell := p.info.Role == proto.RoleThread && !p.told && typing(b)
	if tell {
		p.told = true
	}
	p.mu.Unlock()
	if tell {
		go p.takeover()
	}
	return p.sess.Input(b)
}

// typing reports whether b holds something the user did: a key, a paste
// or a mouse press. The console's own reports (focus in and out, which it
// sends when a pane is shown or the window changes focus, and mouse
// release, motion and wheel) are not typing, so they raise no takeover.
func typing(b []byte) bool {
	for len(b) > 0 {
		if b[0] != 0x1b || len(b) < 3 || b[1] != '[' {
			return true
		}
		switch b[2] {
		case 'I', 'O': // focus in, focus out
			b = b[3:]
		case '<': // SGR mouse: ESC [ < Cb ; Cx ; Cy M|m
			end := 3
			for end < len(b) && b[end] != 'M' && b[end] != 'm' {
				end++
			}
			if end == len(b) {
				return true
			}
			cb, _, _ := strings.Cut(string(b[3:end]), ";")
			n, err := strconv.Atoi(cb)
			if err != nil || (b[end] == 'M' && n&(32|64) == 0) {
				return true // a press (or something we can't read)
			}
			b = b[end+1:]
		case 'M': // X10 mouse: ESC [ M Cb Cx Cy
			if len(b) < 6 {
				return true
			}
			if cb := int(b[3]) - 32; cb&3 != 3 && cb&(32|64) == 0 {
				return true
			}
			b = b[6:]
		default:
			return true
		}
	}
	return false
}

func (p *paneSource) takeover() {
	pr, err := project.Open(p.info.Project)
	if err == nil {
		err = pr.TookOver(p.by, p.info.Thread, thread.Label(pr, p.info.Thread))
	}
	if err != nil {
		p.s.log.Printf("session %s: takeover from %s: %v", p.info.ID, p.by, err)
	}
}

// Resize claims the pane's size for the peer's console that typed, as a
// local console's keys claim it.
func (p *paneSource) Resize(cols, rows uint16) error { return p.sess.RequestResize(cols, rows) }

func (p *paneSource) Close() {
	p.once.Do(func() {
		close(p.done)
		p.sub.Detach()
		p.s.log.Printf("session %s: paired server %s detached", p.info.ID, p.by.Alias)
	})
}

// watchShare ends the stream once the project isn't shared any more.
func (p *paneSource) watchShare() {
	t := time.NewTicker(paneShareCheck)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
			if !slices.Contains(sharedProjects(), p.info.Project) {
				p.sub.End(proto.PaneNotShared)
				return
			}
		}
	}
}
