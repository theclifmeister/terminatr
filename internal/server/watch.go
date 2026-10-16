package server

// Watching a session (session.watch, docs/SPEC.md §3.3, Watch): the
// session's state, its thread's task and PR and its project's waiting
// work, sent again whenever it changes. It streams like view.subscribe:
// the answer, then one line per new state on the same connection.
//
// What the server changes itself (agent state, session exits, project
// commands run through cli.run) wakes the watchers at once; what changes
// behind its back (the human's own tm commands, the ticker's PR polls,
// the prompt queue) is caught by a re-read every watchPoll.

import (
	"bufio"
	"cmp"
	"net"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// envWatchPoll shortens the re-read of a watched session (tests).
const envWatchPoll = "TERMINATR_WATCH_POLL"

// defaultWatchPoll is how often a watch re-reads what the server isn't
// told about.
const defaultWatchPoll = time.Second

// watchers wakes every watch when the server changes something.
type watchers struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
	gen  uint64 // wakes so far
}

// generation counts the wakes so far: a cache built before the last
// one is stale.
func (w *watchers) generation() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gen
}

func (w *watchers) add() chan struct{} {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.subs == nil {
		w.subs = map[chan struct{}]struct{}{}
	}
	w.subs[ch] = struct{}{}
	return ch
}

func (w *watchers) remove(ch chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.subs, ch)
}

// wake tells every watch to look again. It never blocks, and is safe
// with the server's mu held.
func (w *watchers) wake() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gen++
	for ch := range w.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// watchState is session id's state now; ok is false once it has ended.
func (s *Server) watchState(id string) (proto.Watch, bool) {
	sess, perr := s.session(id)
	if perr != nil {
		return proto.Watch{}, false
	}
	return watchFunc(s.info(sess), ticker.StatePath(s.opts.Paths.Sessions)), true
}

// watchFunc is watchOf; tests replace it before a server starts.
var watchFunc = watchOf

// watchOf is the Watch of the session info describes, from its project's
// files and the ticker's state file at tickerState.
func watchOf(info proto.SessionInfo, tickerState string) proto.Watch {
	w := proto.Watch{
		Session: proto.WatchSession{ID: info.ID, Role: info.Role, Agent: info.Agent, Project: info.Project,
			Thread: info.Thread, State: info.State, Reason: info.Reason},
		Queued: info.Queued,
	}
	if info.Project == "" {
		return w
	}
	p, err := project.Open(info.Project)
	if err != nil {
		return w
	}
	b, _ := p.Tasks().Load()
	if b != nil {
		for _, t := range b.Tasks {
			if tasks.GroupOf(t.Status) == tasks.NeedsYou {
				w.NeedsYou++
			}
		}
	}
	if items, err := p.Inbox(); err == nil {
		w.Inbox = len(items)
	}
	if info.Role == proto.RoleCoordinator {
		if qs, err := p.Questions(); err == nil {
			w.Questions = len(qs)
		}
	}
	if info.Role != proto.RoleThread || !thread.ValidID(info.Thread) {
		return w
	}
	st, _ := thread.ReadStatus(p, info.Thread)
	if st != nil {
		w.Session.NeedsYou = st.NeedsYou
	}
	if rec, err := thread.Load(p, info.Thread); err == nil && b != nil && rec.TaskID() > 0 {
		if t := b.Find(rec.TaskID()); t != nil {
			w.Task = &proto.WatchTask{ID: t.Ref(), Title: t.Title, Status: string(t.Status),
				StepsDone: t.StepsDone(), StepsTotal: len(t.Steps)}
			if st != nil {
				w.Task.Current = st.Current
			}
		}
	}
	pr := ticker.PRs(tickerState, info.Project)[info.Thread]
	w.PR, w.PRURL = pr.Summary(), pr.URL
	if w.PR != "" && pr.State == "OPEN" && pr.MergeState == "BEHIND" {
		w.PR += ", behind " + cmp.Or(pr.Base, "its base")
	}
	if w.PRURL == "" {
		if rep, _ := thread.ReadReport(p, info.Thread); rep != nil {
			w.PRURL = rep.PR
		}
	}
	return w
}

func (s *Server) isStopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

// serveWatch answers session.watch and then streams the session's state:
// a watch.changed line for every new one, until the client hangs up or
// the session ends, whose last line has state "exited". A stopping
// server just hangs up.
func (s *Server) serveWatch(c net.Conn, br *bufio.Reader, req proto.Request) {
	var p proto.SessionIDParams
	if perr := decodeParams(req.Params, &p); perr != nil {
		refuse(c, req, perr)
		return
	}
	// Read before the answer: once it is out, the caller may change it.
	poll := watchPollEvery()
	// Join before the first read, so no change between them is missed.
	wake := s.watch.add()
	defer s.watch.remove(wake)
	w, ok := s.watchState(p.ID)
	if !ok {
		refuse(c, req, proto.Errorf(proto.ErrUnknownSession, "no session %q", p.ID))
		return
	}
	if !answer(c, req, w) {
		return
	}
	var last lastSent
	last.changed(w)
	pollStream(c, clientGone(br), wake, poll, func() (any, bool) {
		next, alive := s.watchState(p.ID)
		if !alive && s.isStopping() {
			// Not ended: the next server resumes it under its id.
			return nil, true
		}
		if !alive {
			w.Session.State, w.Session.Reason = string(agent.StateExited), ""
			return proto.WatchEvent{Event: proto.EventWatchChanged, Watch: w}, true
		}
		if !last.changed(next) {
			return nil, false
		}
		w = next
		return proto.WatchEvent{Event: proto.EventWatchChanged, Watch: w}, false
	})
}
