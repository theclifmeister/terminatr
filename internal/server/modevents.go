package server

// The user mods' event bus (docs/SPEC.md §7.5, Events for user mods):
// the server works out what changed and publishes it to every
// subscribed mod's queue (package modbus).
//
// Events come from looking again, not from the writers: what the server
// changes itself (project commands through cli.run, agent state, session
// exits, questions) and each ticker sweep (its inbox items and PR polls)
// wake a look at once; what changes behind its back (the human's own tm
// commands) is caught by a look every modEventsPoll. A look compares
// each project and session with the last and publishes the difference.
// While no mod is subscribed nothing is looked at.

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// envModEventsPoll shortens the look for changes the server isn't told
// about (tests).
const envModEventsPoll = "TERMINATR_MOD_EVENTS_POLL"

const (
	defaultModEventsPoll = 5 * time.Second
	// modEventsGap coalesces a burst of wakes into one look.
	modEventsGap = 100 * time.Millisecond
)

// modEvents is the bus and what wakes its look.
type modEvents struct {
	bus  *modbus.Bus
	kick chan struct{}
	// hold, when set, runs before each look and the func it returns
	// after it (tests: changes made while it holds land in one look).
	hold func() (release func())
}

func newModEvents() *modEvents {
	return &modEvents{bus: modbus.New(), kick: make(chan struct{}, 1)}
}

// wake asks for a look soon. It never blocks, and is safe with the
// server's mu held, and on a nil bus (a test's bare Server).
func (e *modEvents) wake() {
	if e == nil {
		return
	}
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// SubscribeMod opens mod id's event queue (a mod host starting, or
// restarting: the old queue ends). grants are the mod's granted
// permissions, and projects says which projects it runs for (nil: all).
// Its first event is mod.ready with the projects it may read.
func (s *Server) SubscribeMod(id string, grants []string, projects func(slug string) bool) *modbus.Sub {
	read := false
	for _, g := range grants {
		read = read || g == modbus.GrantProjectRead
	}
	sub := s.modEv.bus.Subscribe(id, modbus.Options{Grants: grants, Project: projects,
		Ready: func() any { return s.modReady(read, projects) }})
	s.modViewsRestarted(id)
	s.modEv.wake()
	return sub
}

// modReady is mod.ready's snapshot: every project the mod runs for, as a
// ProjectWatch, when it may read them.
func (s *Server) modReady(read bool, projects func(string) bool) modbus.Ready {
	r := modbus.Ready{}
	if !read {
		return r
	}
	r.Projects = []proto.ProjectWatch{}
	list, err := project.List()
	if err != nil {
		s.log.Printf("mod events: %v", err)
		return r
	}
	for _, sum := range list {
		if sum.Error != "" || projects != nil && !projects(sum.Slug) {
			continue
		}
		if w, err := s.projectWatchState(sum.Slug); err == nil {
			r.Projects = append(r.Projects, w)
		}
	}
	return r
}

// runModEvents looks for changes on every wake, every poll and every
// minute (with clock.minute), until ctx ends.
func (s *Server) runModEvents(ctx context.Context) {
	e := s.modEv
	tr := modbus.NewTracker(e.bus, modReport)
	poll := cmp.Or(envDuration(envModEventsPoll), defaultModEventsPoll)
	pt := time.NewTicker(poll)
	defer pt.Stop()
	minute := time.NewTimer(untilMinute(time.Now()))
	defer minute.Stop()
	looked := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-pt.C:
		case now := <-minute.C:
			e.bus.Publish(modbus.ClockMinute, "", modbus.Clock{Now: now.Truncate(time.Minute)})
			minute.Reset(untilMinute(time.Now()))
		case <-e.kick:
			select {
			case <-ctx.Done():
				return
			case <-time.After(modEventsGap):
			}
		}
		if !e.bus.Active() {
			if looked {
				tr.Reset()
				looked = false
			}
			continue
		}
		looked = true
		release := func() {}
		if e.hold != nil {
			release = e.hold()
		}
		s.lookModEvents(tr)
		release()
	}
}

// untilMinute is how long from now to the next whole minute.
func untilMinute(now time.Time) time.Duration {
	return now.Truncate(time.Minute).Add(time.Minute).Sub(now)
}

// lookModEvents gives the tracker the sessions and every project as they
// are now.
func (s *Server) lookModEvents(tr *modbus.Tracker) {
	sessions := s.list().Sessions
	ss := make([]modbus.Session, 0, len(sessions))
	for _, info := range sessions {
		n := modbus.Session{Session: info.ID, Project: info.Project, Thread: info.Thread, Role: info.Role,
			Agent: info.Agent, State: info.State, Reason: info.Reason, Context: info.ContextPercent()}
		n.Tokens, n.CostUSD = sessionUsage(info)
		ss = append(ss, n)
	}
	tr.Sessions(ss)
	tr.Questions(sessionQuestions(sessions))
	list, err := project.List()
	if err != nil {
		s.log.Printf("mod events: %v", err)
		return
	}
	statePath := ticker.StatePath(s.opts.Paths.Sessions)
	for _, sum := range list {
		if sum.Error != "" {
			continue
		}
		p, err := project.Open(sum.Slug)
		if err != nil {
			continue
		}
		tr.Project(p.Slug, projectState(p, sessions, statePath))
	}
}

// sessionUsage is what session info's thread has used so far: its
// record's totals while the session is the thread's; none for a session
// that is no thread's.
func sessionUsage(info proto.SessionInfo) (int64, float64) {
	if info.Role != proto.RoleThread || info.Project == "" || info.Thread == "" {
		return 0, 0
	}
	p, err := project.Open(info.Project)
	if err != nil {
		return 0, 0
	}
	r, err := thread.Load(p, info.Thread)
	if err != nil || r.Session != info.ID {
		return 0, 0
	}
	return r.Usage.Tokens(), r.Usage.CostUSD
}

// sessionQuestions are the questions open on the sessions of a project:
// each one's first unanswered question.
func sessionQuestions(sessions []proto.SessionInfo) []modbus.Question {
	var out []modbus.Question
	for _, info := range sessions {
		if info.Project == "" || info.Question == nil {
			continue
		}
		for _, it := range info.Question.Questions {
			if it.Answered {
				continue
			}
			a := &modbus.Ask{Question: it.Question, Header: it.Header, MultiSelect: it.MultiSelect, Options: []modbus.AskOption{}}
			for _, o := range it.Options {
				a.Options = append(a.Options, modbus.AskOption{Label: o.Label, Description: o.Description})
			}
			out = append(out, modbus.Question{Session: info.ID, Project: info.Project, Thread: info.Thread, Role: info.Role, Question: a})
			break
		}
	}
	return out
}

// questionNoText is a thread's needs_you when its session is blocked on
// a question whose text tm doesn't have (no Agent mod to report the
// menu).
const questionNoText = "a question in its session"

// sessionNeedsYou is what a thread's session waits on the human for: its
// open menu's question, or questionNoText when it is blocked on a
// question all the same. The menu's text comes from the Agent mod; the
// hooks say a question blocks the session without it, and that needs
// the human just as much (T301 L2).
func sessionNeedsYou(info proto.SessionInfo) string {
	if q := openQuestion(info.Question); q != "" {
		return q
	}
	if info.State == string(agent.StateBlocked) && info.Reason == "question" {
		return questionNoText
	}
	return ""
}

// projectState is project p's state for the tracker, with sessions the
// server's and tickerState the ticker's state file.
func projectState(p *project.Project, sessions []proto.SessionInfo, tickerState string) *modbus.ProjectState {
	st := &modbus.ProjectState{Tasks: map[string]modbus.Task{}, Threads: map[string]modbus.ThreadSnap{},
		Inbox: map[string]modbus.Item{}, PRs: map[string]modbus.PR{}}
	items, itemsErr := p.Inbox()
	if b, _ := p.Tasks().Load(); b != nil {
		for _, t := range b.Tasks {
			st.Tasks[t.Ref()] = modbus.Task{Project: p.Slug, ID: t.Ref(), Title: t.Title, To: string(t.Status),
				StepsDone: t.StepsDone(), StepsTotal: len(t.Steps), Thread: t.Thread}
		}
		// What $.tasks.list shows, as a digest (tasks.changed).
		js, _ := json.Marshal(modTasks(b, items))
		sum := sha256.Sum256(js)
		st.Board = string(sum[:])
	}
	byThread := map[string]proto.SessionInfo{}
	for _, info := range sessions {
		if info.Project == p.Slug && info.Role == proto.RoleThread && info.Thread != "" {
			byThread[info.Thread] = info
		}
	}
	recs, _ := thread.List(p)
	for _, r := range recs {
		ts := modbus.ThreadSnap{Record: r.State, Reports: r.Reports,
			Life:  modbus.ThreadLife{Project: p.Slug, Thread: r.ID, Task: r.Task, Agent: r.Agent, Model: r.Model},
			State: modbus.Thread{Project: p.Slug, Thread: r.ID, Task: r.Task, State: r.State}}
		if r.State != thread.Resolved {
			if info, ok := byThread[r.ID]; ok && info.State != "" {
				ts.State.State, ts.State.Reason = info.State, info.Reason
				ts.State.NeedsYou = sessionNeedsYou(info)
			}
			if sst, _ := thread.ReadStatus(p, r.ID); sst != nil && sst.NeedsYou != "" {
				ts.State.NeedsYou = sst.NeedsYou
			}
		}
		st.Threads[r.ID] = ts
	}
	if itemsErr == nil {
		for _, it := range items {
			st.Inbox[it.ID] = modbus.Item{Project: p.Slug, ID: it.ID, Kind: it.Kind, Subject: it.Subject,
				Summary: it.Summary, NeedsUser: it.NeedsUser, Message: it.Message}
		}
	}
	// The ticker's times are read on every look; the event's ticker,
	// which asks git for the repos' hosts, only when they changed.
	timing := ticker.ReadTiming(tickerState, p.Slug)
	st.Ticker = modbus.TickerTimes{PRChecked: timing.PRPolled, Synced: timing.Synced, GHFailing: timing.GHFailing}
	st.TickerWatch = func() proto.WatchTicker { return watchTicker(p, timing) }
	for id, pr := range ticker.PRs(tickerState, p.Slug) {
		st.PRs[id] = modbus.PR{Project: p.Slug, Thread: id, Number: pr.Number, URL: pr.URL, State: pr.State,
			Checks: pr.Checks, Review: pr.Review, Behind: pr.MergeState == "BEHIND", Conflict: pr.Mergeable == "CONFLICTING"}
	}
	return st
}

// modReport is thread id's newest report for report.stored; nil when
// it can't be read.
func modReport(slug, id string) *modbus.Report {
	p, err := project.Open(slug)
	if err != nil {
		return nil
	}
	r, err := thread.Load(p, id)
	if err != nil {
		return nil
	}
	rep, err := thread.ReadReport(p, id)
	if err != nil || rep == nil {
		return nil
	}
	// The ## Report section, not the whole file (T301 B2); a report
	// edited by hand out of shape gives its text.
	body := thread.Section(rep.Text, "Report")
	if body == "" && !strings.Contains(rep.Text, "## Report") {
		body = strings.TrimSpace(rep.Text)
	}
	return &modbus.Report{Project: slug, Thread: id, Task: r.Task, PR: rep.PR, Report: r.Reports,
		Sections: modbus.ReportSections{Report: body, Next: rep.Next, Check: rep.Check, Remember: rep.Remember}}
}
