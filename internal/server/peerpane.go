package server

// This server's panes for paired servers (docs/SPEC.md §16.12): a shared
// project's coordinator and threads, watched through peer.attach while
// the pane switch for that peer is view or type, and typed into while it
// is type. The peer's keys are the human's on the far console, but the
// caller stays peer:<alias> everywhere else.

import (
	"slices"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/emu"
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

// takeoverLogged is how much of the input that raised a takeover the
// log keeps.
const takeoverLogged = 32

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
// keys do (docs/SPEC.md §4), in this server's words. Only a person's
// typing counts, a key, a paste or a click that reaches the program: the
// console's reports (focus, mouse release, motion and wheel) and the
// terminal's replies, which the session drops, don't (emu.NextInput).
func (p *paneSource) Input(b []byte) error {
	p.mu.Lock()
	tell := p.info.Role == proto.RoleThread && !p.told && emu.Typed(emu.ForProgram(b, p.sess.Modes()))
	if tell {
		p.told = true
	}
	p.mu.Unlock()
	if tell {
		go p.takeover(string(b[:min(len(b), takeoverLogged)]))
	}
	return p.sess.Input(b)
}

// takeover tells the thread's coordinator; the log keeps the start of
// the input that raised it, typed.
func (p *paneSource) takeover(typed string) {
	p.s.log.Printf("session %s: takeover from %s on %q", p.info.ID, p.by, typed)
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
