package modbus

import (
	"maps"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// ProjectState is what the events of a project are worked out from: the
// server reads it from the project's files, the ticker's state and its
// sessions, and the Tracker publishes what changed since the last one.
type ProjectState struct {
	Tasks map[string]Task // by id, To the status
	// Board is all $.tasks.list shows of the board, the asks too, as a
	// digest: tasks.changed when it differs.
	Board   string
	Threads map[string]ThreadSnap
	Inbox   map[string]Item // the unhandled items, by id
	PRs     map[string]PR   // by thread, only threads with a PR
	// Ticker is when the ticker last polled PRs and synced, and whether
	// its polls fail: ticker.changed when it differs, with TickerWatch's
	// ticker (nil: none).
	Ticker      TickerTimes
	TickerWatch func() proto.WatchTicker
}

// TickerTimes are what of a project's ticker ticker.changed follows.
type TickerTimes struct {
	PRChecked, Synced time.Time
	GHFailing         bool
}

func (a TickerTimes) same(b TickerTimes) bool {
	return a.PRChecked.Equal(b.PRChecked) && a.Synced.Equal(b.Synced) && a.GHFailing == b.GHFailing
}

// ThreadSnap is one thread in a ProjectState.
type ThreadSnap struct {
	State   Thread
	Life    ThreadLife
	Record  string // its record's state: running, stopped, resolved
	Reports int    // reports stored
	// Changes is its worktree's changes while its session is live and
	// a mod listens to thread.changes; nil otherwise.
	Changes *Changes
}

// Tracker turns successive states into events. Its first sight of a
// project or a session publishes nothing: it is what mod.ready's
// snapshot already shows.
type Tracker struct {
	bus      *Bus
	projects map[string]*ProjectState
	sessions map[string]Session
	primed   bool // sessions seen once: a new one is news
	// questions are the sessions' open questions, primedQ that they
	// were seen once.
	questions map[string]Question
	primedQ   bool
	// report reads thread's newest report; nil when it can't.
	report func(project, thread string) *Report
}

// NewTracker publishes to bus; report reads a thread's newest report.
func NewTracker(bus *Bus, report func(project, thread string) *Report) *Tracker {
	return &Tracker{bus: bus, projects: map[string]*ProjectState{}, sessions: map[string]Session{}, questions: map[string]Question{}, report: report}
}

// Reset forgets what was seen: the next states are baselines again (no
// mod listened meanwhile).
func (t *Tracker) Reset() {
	clear(t.projects)
	clear(t.sessions)
	clear(t.questions)
	t.primed, t.primedQ = false, false
}

// Project publishes what changed in project slug since its last state.
// Events of one project go out in the order of the design's table:
// threads started, tasks (each task, then the board), thread states, thread changes, reports, PRs, the ticker,
// inbox items, threads resolved; each kind in id order.
func (t *Tracker) Project(slug string, cur *ProjectState) {
	old := t.projects[slug]
	t.projects[slug] = cur
	if old == nil {
		return
	}
	pub := func(name string, data any) { t.bus.Publish(name, slug, data) }
	threads := slices.Sorted(maps.Keys(cur.Threads))
	for _, id := range threads {
		if _, ok := old.Threads[id]; !ok {
			pub(ThreadStarted, cur.Threads[id].Life)
		}
	}
	for _, id := range slices.SortedFunc(maps.Keys(cur.Tasks), byTaskID) {
		n := cur.Tasks[id]
		o, ok := old.Tasks[id]
		if ok && o.To == n.To && o.StepsDone == n.StepsDone && o.StepsTotal == n.StepsTotal {
			continue
		}
		n.From = o.To
		pub(TaskChanged, n)
	}
	if cur.Board != old.Board {
		pub(TasksChanged, Tasks{Project: slug})
	}
	for _, id := range threads {
		n, o := cur.Threads[id], old.Threads[id]
		if n.State != o.State && n.Record != "resolved" {
			pub(ThreadState, n.State)
		}
	}
	for _, id := range threads {
		n, o := cur.Threads[id].Changes, old.Threads[id].Changes
		if n != nil && (o == nil || *n != *o) {
			pub(ThreadChanges, *n)
		}
	}
	for _, id := range threads {
		n, o := cur.Threads[id], old.Threads[id]
		if n.Reports > o.Reports && t.report != nil {
			if r := t.report(slug, id); r != nil {
				r.Report = n.Reports
				pub(ReportStored, *r)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(cur.PRs)) {
		if n, o := cur.PRs[id], old.PRs[id]; n != o {
			pub(PRChanged, n)
		}
	}
	if !cur.Ticker.same(old.Ticker) && cur.TickerWatch != nil {
		pub(TickerChanged, Ticker{Project: slug, Ticker: cur.TickerWatch()})
	}
	for _, id := range slices.Sorted(maps.Keys(cur.Inbox)) {
		if _, ok := old.Inbox[id]; !ok {
			pub(InboxItem, cur.Inbox[id])
		}
	}
	for _, id := range threads {
		n := cur.Threads[id]
		if o, ok := old.Threads[id]; ok && n.Record == "resolved" && o.Record != "resolved" {
			pub(ThreadResolved, n.Life)
		}
	}
}

// Sessions publishes session.state for each session that started (after
// the first call), whose state, reason or context use changed, and once
// for each that ended.
func (t *Tracker) Sessions(cur []Session) {
	seen := map[string]bool{}
	for _, n := range cur {
		seen[n.Session] = true
		o, ok := t.sessions[n.Session]
		t.sessions[n.Session] = n
		if ok && o != n || !ok && t.primed {
			t.bus.Publish(SessionState, n.Project, n)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(t.sessions)) {
		if seen[id] {
			continue
		}
		o := t.sessions[id]
		delete(t.sessions, id)
		o.State, o.Reason = "exited", ""
		t.bus.Publish(SessionState, o.Project, o)
	}
	t.primed = true
}

// Questions publishes session.question for each session in a project
// whose open question changed: one opened (after the first call),
// another replaced it, or it was answered or went, with the session.
// cur holds the sessions with a question open.
func (t *Tracker) Questions(cur []Question) {
	seen := map[string]bool{}
	for _, n := range cur {
		seen[n.Session] = true
		o, ok := t.questions[n.Session]
		t.questions[n.Session] = n
		if ok && !sameAsk(o.Question, n.Question) || !ok && t.primedQ {
			t.bus.Publish(SessionQuestion, n.Project, n)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(t.questions)) {
		if seen[id] {
			continue
		}
		o := t.questions[id]
		delete(t.questions, id)
		o.Question = nil
		t.bus.Publish(SessionQuestion, o.Project, o)
	}
	t.primedQ = true
}

func sameAsk(a, b *Ask) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Question == b.Question && a.Header == b.Header && a.MultiSelect == b.MultiSelect && slices.Equal(a.Options, b.Options)
}

// byTaskID orders "T9" before "T10".
func byTaskID(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
