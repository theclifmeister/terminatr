package server

// Watching a project (project.watch, docs/SPEC.md §3.3, Watch): what the
// coordinator's /tm pane shows, sent again whenever it changes. It
// streams like session.watch, and wakes and re-reads the same way: at
// once on what the server changes itself, every watchPoll for the rest
// (the board, the inbox, thread files, the ticker's PR polls).

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"github.com/theclifmeister/terminatr/internal/codehost"
	"io"
	"net"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// projectWatchState is project slug's state now, from its files (as
// the project cache last read them), the ticker's state file and the
// server's sessions.
func (s *Server) projectWatchState(slug string) (proto.ProjectWatch, error) {
	snap, err := s.projects.get(slug, ticker.StatePath(s.opts.Paths.Sessions), s.watch.generation())
	if err != nil {
		return proto.ProjectWatch{}, err
	}
	w := snap.watch(s.list().Sessions)
	w.Cells = s.projectCells(slug)
	return w, nil
}

// ProjectWatchOf is project p's ProjectWatch, with sessions the server's
// and tickerState the ticker's state file. The TUI's coordinator info
// panel reads it too, with the sessions of its poll (T90).
func ProjectWatchOf(p *project.Project, sessions []proto.SessionInfo, tickerState string) proto.ProjectWatch {
	return readProjectSnap(p, tickerState).watch(sessions)
}

// projectSnap is what a project watch reads from files: the project's,
// the ticker's state file and the config. A ProjectWatch is built from
// it and the sessions without reading anything.
type projectSnap struct {
	p      *project.Project
	items  []project.Item
	board  *tasks.Board
	recs   []*thread.Record
	status map[string]*thread.Status // by thread id; nil for unreadable
	// reportPR is a thread's report's PR link, for the threads the
	// ticker knows no PR URL of.
	reportPR  map[string]string
	prs       map[string]ticker.PR
	timing    ticker.Timing
	questions int
	hint      int  // the config's context hint
	prPoll    int  // the project's PR poll, in seconds
	noPRHost  bool // NoPRHost
	// links are the project's links to other projects' coordinators
	// (docs/SPEC.md §7.9).
	links []proto.WatchLink
}

// readProjectSnap reads project p's files, the ticker's state file at
// tickerState and the config, each once.
func readProjectSnap(p *project.Project, tickerState string) *projectSnap {
	sn := &projectSnap{p: p, status: map[string]*thread.Status{}, reportPR: map[string]string{},
		hint: config.DefaultContextHint, prPoll: config.DefaultPRPollSeconds}
	sn.items, _ = p.Inbox()
	sn.board, _ = p.Tasks().Load()
	sn.prs, sn.timing = ticker.ReadProject(tickerState, p.Slug)
	sn.recs, _ = thread.List(p)
	for _, r := range sn.recs {
		if r.State == thread.Resolved {
			continue
		}
		sn.status[r.ID], _ = thread.ReadStatus(p, r.ID)
		if sn.prs[r.ID].URL == "" {
			if rep, _ := thread.ReadReport(p, r.ID); rep != nil {
				sn.reportPR[r.ID] = rep.PR
			}
		}
	}
	if qs, err := p.Questions(); err == nil {
		sn.questions = len(qs)
	}
	if cfg, err := config.Load(); err == nil {
		sn.hint = cfg.ContextHint
		if s, err := cfg.Safety(p.Slug); err == nil && s.PRPollSeconds > 0 {
			sn.prPoll = s.PRPollSeconds
		}
		for _, r := range link.Rows(cfg, p) {
			sn.links = append(sn.links, proto.WatchLink{Peer: r.Peer, State: r.Words(), Send: r.Send, Receive: r.Receive,
				Paused: r.Paused, Unread: r.Unread, Held: r.Held, Last: r.Last})
		}
	}
	sn.noPRHost = NoPRHost(p)
	return sn
}

// watch is the project's ProjectWatch with sessions the server's.
func (sn *projectSnap) watch(sessions []proto.SessionInfo) proto.ProjectWatch {
	p, items, b := sn.p, sn.items, sn.board
	w := proto.ProjectWatch{Project: p.Slug, NeedsYou: []proto.WatchNeed{}, Inbox: []proto.WatchItem{},
		Threads: []proto.WatchThread{}, Ready: []proto.WatchTodo{}}
	for _, r := range project.Rows(items) {
		w.Inbox = append(w.Inbox, proto.WatchItem{ID: r.ID, Kind: r.Kind, Subject: r.Subject, Summary: r.Summary, NeedsUser: r.NeedsUser,
			Count: r.Count, Task: r.Task, What: r.What, Title: r.Title})
	}
	byThread := map[string]proto.SessionInfo{}
	for _, info := range sessions {
		if info.Project == p.Slug && info.Role == proto.RoleThread && info.Thread != "" {
			byThread[info.Thread] = info
		}
	}
	prs, timing := sn.prs, sn.timing

	// The threads, and per task the latest one working on it.
	byTask := map[string]int{} // index in w.Threads
	prOf := map[string]ticker.PR{}
	for _, r := range sn.recs {
		if r.State == thread.Resolved {
			continue
		}
		wt := proto.WatchThread{ID: r.ID, Title: r.Title, Reports: r.Reports, Done: r.Done}
		if info, ok := byThread[r.ID]; ok {
			wt.Session, wt.State, wt.Reason = info.ID, info.State, info.Reason
			wt.NeedsYou = sessionNeedsYou(info)
		}
		st := sn.status[r.ID]
		if st != nil && st.NeedsYou != "" {
			wt.NeedsYou = st.NeedsYou
		}
		if b != nil && r.TaskID() > 0 {
			if t := b.Find(r.TaskID()); t != nil {
				wt.Task = &proto.WatchTask{ID: t.Ref(), Title: t.Title, Status: string(t.Status),
					StepsDone: t.StepsDone(), StepsTotal: len(t.Steps)}
				if st != nil {
					wt.Task.Current = st.Current
				}
			}
		}
		pr := prs[r.ID]
		wt.PR, wt.PRURL, wt.PRBad, wt.PRChecks = prWords(pr), pr.URL, isPRBad(pr), pr.Checks
		if pr.Number > 0 {
			wt.PRChecked = timing.ThreadPolled[r.ID]
		}
		if wt.PRURL == "" {
			wt.PRURL = sn.reportPR[r.ID]
		}
		prOf[r.ID] = pr
		if wt.Task != nil {
			byTask[wt.Task.ID] = len(w.Threads)
		} else if wt.NeedsYou != "" {
			w.NeedsYou = append(w.NeedsYou, proto.WatchNeed{Why: proto.WhyQuestion, Title: r.Title, Thread: r.ID, Question: wt.NeedsYou})
		}
		w.Threads = append(w.Threads, wt)
	}

	if b != nil {
		for _, t := range b.Tasks {
			ref := t.Ref()
			asked := project.TaskAsked(items, ref)
			switch t.Status {
			case tasks.Open:
				continue // the backlog is not on deck
			case tasks.Ready:
				w.Ready = append(w.Ready, proto.WatchTodo{Task: ref, Title: t.Title, Status: string(t.Status), Asked: asked})
				continue
			case tasks.Done:
				continue
			}
			n := proto.WatchNeed{Task: ref, Title: t.Title, Status: string(t.Status), Asked: asked}
			if i, ok := byTask[ref]; ok {
				wt := w.Threads[i]
				pr := prOf[wt.ID]
				n.Thread, n.Question = wt.ID, wt.NeedsYou
				n.PR, n.PRURL, n.PRNumber = wt.PR, wt.PRURL, pr.Number
				n.Mergeable = isMergeable(pr)
				switch {
				case t.Status == tasks.Review:
					n.Why = proto.WhyReview
				case n.Question != "":
					n.Why = proto.WhyQuestion
				case pr.State == "OPEN" && (pr.Checks == "fail" || pr.Mergeable == "CONFLICTING"):
					n.Why = proto.WhyCI
				}
			}
			if n.Why == "" {
				switch t.Status {
				case tasks.Review:
					n.Why = proto.WhyReview
				case tasks.Blocked:
					n.Why = proto.WhyBlocked
				default:
					continue
				}
			}
			w.NeedsYou = append(w.NeedsYou, n)
		}
	}
	// Prompts held while the agent idles: a coordinator's nudges wait
	// behind them.
	now := time.Now()
	for _, info := range sessions {
		note := info.QueueNote(now)
		if info.Project != p.Slug || note == "" {
			continue
		}
		w.NeedsYou = append(w.NeedsYou, proto.WatchNeed{Why: proto.WhyQueue, Thread: info.Thread, Session: info.ID,
			Title: queueTitle(info, note)})
	}
	w.Context = contextOf(p.Slug, sessions, sn.hint)
	w.Links = sn.links
	if t := watchTicker(timing, sn.prPoll, sn.noPRHost); !t.PRChecked.IsZero() || !t.Synced.IsZero() || t.NoPRHost {
		w.Ticker = &t
	}
	w.Questions = sn.questions
	slices.SortStableFunc(w.NeedsYou, func(a, b proto.WatchNeed) int {
		if c := cmp.Compare(whyRank(a.Why), whyRank(b.Why)); c != 0 {
			return c
		}
		// A PR ready to merge goes before one that isn't.
		return cmp.Compare(boolRank(!a.Mergeable), boolRank(!b.Mergeable))
	})
	return w
}

// queueTitle names a held queue: "1 queued prompt for the coordinator,
// held 3m0s: prompt box not empty".
func queueTitle(info proto.SessionInfo, note string) string {
	s := ""
	if info.Queued != 1 {
		s = "s"
	}
	return fmt.Sprintf("%d queued prompt%s for the %s, %s", info.Queued, s, cmp.Or(info.Role, "session"), note)
}

func whyRank(why string) int {
	return slices.Index([]string{proto.WhyQueue, proto.WhyReview, proto.WhyQuestion, proto.WhyCI, proto.WhyBlocked}, why)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// openQuestion is the first unanswered question of the menu open in the
// agent, "" for none.
func openQuestion(q *proto.Question) string {
	if q == nil {
		return ""
	}
	for _, it := range q.Questions {
		if !it.Answered {
			return it.Question
		}
	}
	return ""
}

// prWords is pr in the ticker's words, with its base when it is behind
// it, as session.watch says it; "" for no PR.
func prWords(pr ticker.PR) string {
	s := pr.Summary()
	if s != "" && pr.State == "OPEN" && pr.MergeState == "BEHIND" {
		s += ", behind " + cmp.Or(pr.Base, "its base")
	}
	return s
}

// isPRBad reports an open PR the thread has to act on.
func isPRBad(pr ticker.PR) bool {
	return pr.State == "OPEN" && (pr.Checks == "fail" || pr.Mergeable == "CONFLICTING" ||
		pr.Review == "CHANGES_REQUESTED" || pr.MergeState == "BEHIND")
}

// isMergeable reports an open PR with its checks passed (or none), no
// conflicts and no changes requested: the coordinator may merge it once
// the user says so.
func isMergeable(pr ticker.PR) bool {
	return pr.State == "OPEN" && (pr.Checks == "pass" || pr.Checks == "") &&
		pr.Mergeable != "CONFLICTING" && pr.Review != "CHANGES_REQUESTED"
}

// serveProjectWatch answers project.watch and then streams the
// project's state: a project.changed line for every new one, until the
// client hangs up or the server stops.
func (s *Server) serveProjectWatch(c net.Conn, br *bufio.Reader, req proto.Request) {
	var p proto.ProjectWatchParams
	resp := proto.Response{ID: req.ID}
	if perr := decodeParams(req.Params, &p); perr != nil {
		resp.Error = perr
		writeJSONLine(c, resp)
		return
	}
	wake := s.watch.add()
	defer s.watch.remove(wake)
	w, err := s.projectWatchState(p.Project)
	if err != nil {
		resp.Error = proto.Errorf(proto.ErrBadParams, "%v", err)
		writeJSONLine(c, resp)
		return
	}
	resp.Result, _ = json.Marshal(w)
	if err := writeJSONLine(c, resp); err != nil {
		return
	}
	last := resp.Result
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, br)
		close(gone)
	}()
	poll := envDuration(envWatchPoll)
	if poll <= 0 {
		poll = defaultWatchPoll
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-gone:
			return
		case <-wake:
		case <-t.C:
		}
		if s.isStopping() {
			return
		}
		next, err := s.projectWatchState(p.Project)
		if err != nil {
			continue // a project being renamed or moved: try again
		}
		b, _ := json.Marshal(next)
		if bytes.Equal(b, last) {
			continue
		}
		last = b
		if err := writeJSONLine(c, proto.ProjectWatchEvent{Event: proto.EventProjectChanged, Watch: next}); err != nil {
			return
		}
	}
}

// ProjectWatchStream is a project watch (project.watch).
type ProjectWatchStream struct{ c *Client }

// WatchProject watches project slug and returns the stream and the
// project's state now.
func WatchProject(p Paths, slug string) (*ProjectWatchStream, proto.ProjectWatch, error) {
	c, err := Dial(p, proto.KindControl)
	if err != nil {
		return nil, proto.ProjectWatch{}, err
	}
	var w proto.ProjectWatch
	if err := c.Call(proto.MethodProjectWatch, proto.ProjectWatchParams{Project: slug}, &w); err != nil {
		c.Close()
		return nil, proto.ProjectWatch{}, err
	}
	return &ProjectWatchStream{c: c}, w, nil
}

// Next blocks until the project's next state.
func (s *ProjectWatchStream) Next() (proto.ProjectWatch, error) {
	for {
		var ev proto.ProjectWatchEvent
		if err := readJSONLine(s.c.br, &ev); err != nil {
			return proto.ProjectWatch{}, err
		}
		if ev.Event == proto.EventProjectChanged {
			return ev.Watch, nil
		}
	}
}

// Close ends the watch.
func (s *ProjectWatchStream) Close() error { return s.c.Close() }

// contextOf is the context use of project slug's coordinator, nil when
// none runs or none reported a turn; hint is the config's ContextHint.
func contextOf(slug string, sessions []proto.SessionInfo, hint int) *proto.WatchContext {
	for _, info := range sessions {
		if info.Project != slug || info.Role != proto.RoleCoordinator {
			continue
		}
		pct := info.ContextPercent()
		if pct < 0 {
			return nil
		}
		return &proto.WatchContext{Tokens: info.Context, Window: info.ContextWindow, Percent: pct,
			Threshold: hint, Hint: hint > 0 && pct >= hint}
	}
	return nil
}

// projectTicker is project p's ticker as its dashboard shows it, with
// its poll interval from the config.
func projectTicker(p *project.Project, timing ticker.Timing) proto.WatchTicker {
	secs := config.DefaultPRPollSeconds
	if cfg, err := config.Load(); err == nil {
		if s, err := cfg.Safety(p.Slug); err == nil && s.PRPollSeconds > 0 {
			secs = s.PRPollSeconds
		}
	}
	return watchTicker(timing, secs, NoPRHost(p))
}

// watchTicker is a project's ticker as its dashboard shows it, from the
// ticker's timing: its times, its poll interval (secs), whether its PR
// host fails or it has none (noHost).
func watchTicker(timing ticker.Timing, secs int, noHost bool) proto.WatchTicker {
	return proto.WatchTicker{PRChecked: timing.PRPolled, Synced: timing.Synced, PRPollSeconds: secs, GHFailing: timing.GHFailing && !noHost, NoPRHost: noHost}
}

// NoPRHost is whether p has repos and none has a PR host to poll (no
// origin, or one that is neither GitHub nor Azure DevOps): the ticker
// has nothing to ask, so the pane says so instead of "ok" or "failing",
// and nothing warns of a failing PR host.
func NoPRHost(p *project.Project) bool {
	if len(p.Repos) == 0 {
		return false
	}
	for _, r := range p.Repos {
		if codehost.Detect(r, p.CodeHost()).Kind != codehost.NoKind {
			return false
		}
	}
	return true
}
