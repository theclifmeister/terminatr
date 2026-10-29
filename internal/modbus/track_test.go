package modbus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// drain is the events queued in s, after its mod.ready, as "name data".
// drainEvents is every event s has queued, mod.ready aside.
func drainEvents(t *testing.T, s *Sub) []Event {
	t.Helper()
	var out []Event
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		ev, err := s.Next(ctx)
		cancel()
		if err != nil {
			return out
		}
		if ev.Name != ModReady {
			out = append(out, ev)
		}
	}
}

func drain(t *testing.T, s *Sub) []string {
	t.Helper()
	var out []string
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		ev, err := s.Next(ctx)
		cancel()
		if err != nil {
			return out
		}
		if ev.Name != ModReady {
			out = append(out, fmt.Sprintf("%s %+v", ev.Name, ev.Data))
		}
	}
}

func state() *ProjectState {
	return &ProjectState{
		Tasks: map[string]Task{
			"T9":  {Project: "p", ID: "T9", Title: "nine", To: "open"},
			"T10": {Project: "p", ID: "T10", Title: "ten", To: "started", StepsTotal: 2, Thread: "t-0001"},
		},
		Threads: map[string]ThreadSnap{
			"t-0001": {Record: "running", Reports: 0,
				Life:  ThreadLife{Project: "p", Thread: "t-0001", Task: "T10", Agent: "claude"},
				State: Thread{Project: "p", Thread: "t-0001", Task: "T10", State: "working"}},
		},
		Inbox: map[string]Item{"i1": {Project: "p", ID: "i1", Kind: "report", Subject: "t-0001"}},
		PRs:   map[string]PR{},
	}
}

func TestTrackerProject(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all})
	var asked []string
	tr := NewTracker(b, func(project, thread string) *Report {
		asked = append(asked, project+"/"+thread)
		return &Report{Project: project, Thread: thread, Sections: ReportSections{Report: "did it"}}
	})
	tr.Project("p", state())
	if got := drain(t, s); len(got) != 0 {
		t.Fatalf("baseline published %v", got)
	}

	cur := state()
	t10 := cur.Tasks["T10"]
	t10.To, t10.StepsDone = "review", 2
	cur.Tasks["T10"] = t10
	cur.Tasks["T11"] = Task{Project: "p", ID: "T11", Title: "new", To: "open"}
	th := cur.Threads["t-0001"]
	th.State.State, th.Reports = "idle", 1
	cur.Threads["t-0001"] = th
	cur.Threads["t-0002"] = ThreadSnap{Record: "running",
		Life:  ThreadLife{Project: "p", Thread: "t-0002", Task: "T11", Agent: "codex"},
		State: Thread{Project: "p", Thread: "t-0002", Task: "T11", State: "working"}}
	cur.PRs["t-0001"] = PR{Project: "p", Thread: "t-0001", Number: 7, State: "OPEN", Checks: "pending"}
	cur.Inbox["i2"] = Item{Project: "p", ID: "i2", Kind: "report", Subject: "t-0002"}
	delete(cur.Inbox, "i1") // handled: no event
	tr.Project("p", cur)
	want := []string{
		"thread.started {Project:p Thread:t-0002 Task:T11 Agent:codex Model:}",
		"task.changed {Project:p ID:T10 Title:ten From:started To:review StepsDone:2 StepsTotal:2 Thread:t-0001}",
		"task.changed {Project:p ID:T11 Title:new From: To:open StepsDone:0 StepsTotal:0 Thread:}",
		"thread.state {Project:p Thread:t-0001 Task:T10 State:idle Reason: NeedsYou:}",
		"thread.state {Project:p Thread:t-0002 Task:T11 State:working Reason: NeedsYou:}",
		"report.stored {Project:p Thread:t-0001 Task: Report:1 Sections:{Report:did it Next:[] Check:[] Remember:[]} PR:}",
		"pr.changed {Project:p Thread:t-0001 Number:7 URL: State:OPEN Checks:pending Review: Behind:false Conflict:false}",
		"inbox.item {Project:p ID:i2 Kind:report Subject:t-0002 Summary: NeedsUser:false Message:}",
	}
	got := drain(t, s)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events:\n%q\nwant\n%q", got, want)
	}
	if fmt.Sprint(asked) != "[p/t-0001]" {
		t.Fatalf("reports read %v", asked)
	}

	// Unchanged: nothing. Resolved: thread.resolved, not thread.state.
	tr.Project("p", cur)
	next := *cur
	next.Threads = map[string]ThreadSnap{"t-0002": cur.Threads["t-0002"]}
	r := cur.Threads["t-0001"]
	r.Record, r.State.State = "resolved", "resolved"
	next.Threads["t-0001"] = r
	tr.Project("p", &next)
	want = []string{"thread.resolved {Project:p Thread:t-0001 Task:T10 Agent:claude Model:}"}
	if got := drain(t, s); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events %q, want %q", got, want)
	}

	// After a Reset the next state is a baseline again.
	tr.Reset()
	tr.Project("p", state())
	if got := drain(t, s); len(got) != 0 {
		t.Fatalf("after reset %v", got)
	}
}

func TestTrackerSessions(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{})
	tr := NewTracker(b, nil)
	a := Session{Session: "s-1", Project: "p", Role: "coordinator", Agent: "claude", State: "idle", Context: 10}
	tr.Sessions([]Session{a})
	if got := drain(t, s); len(got) != 0 {
		t.Fatalf("baseline %v", got)
	}
	a.Context = 40
	c := Session{Session: "s-2", Role: "shell", Context: -1}
	tr.Sessions([]Session{a, c})
	tr.Sessions([]Session{a, c})
	tr.Sessions([]Session{c})
	want := []string{
		"session.state {Session:s-1 Project:p Thread: Role:coordinator Agent:claude State:idle Reason: Context:40 Tokens:0 CostUSD:0 Uncosted:0}",
		"session.state {Session:s-2 Project: Thread: Role:shell Agent: State: Reason: Context:-1 Tokens:0 CostUSD:0 Uncosted:0}",
		"session.state {Session:s-1 Project:p Thread: Role:coordinator Agent:claude State:exited Reason: Context:40 Tokens:0 CostUSD:0 Uncosted:0}",
	}
	if got := drain(t, s); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events:\n%q\nwant\n%q", got, want)
	}
}

// TestTrackerBoard: tasks.changed follows the board's digest, whatever
// changed in it (a title, an ask handled), once, after the tasks' own
// events.
func TestTrackerBoard(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all})
	tr := NewTracker(b, nil)
	cur := state()
	cur.Board = "a"
	tr.Project("p", cur)
	next := *cur
	next.Board = "b" // a title edited: no task.changed, but tasks.changed
	tr.Project("p", &next)
	tr.Project("p", &next)
	want := []string{"tasks.changed {Project:p}"}
	if got := drain(t, s); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events %q, want %q", got, want)
	}
}

// TestTrackerDesigns: designs.changed follows the designs' digest once
// both sides have one: "" (no mod listened, or it couldn't be read) is
// a baseline, not a change.
func TestTrackerDesigns(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all})
	tr := NewTracker(b, nil)
	cur := state()
	tr.Project("p", cur)
	for _, d := range []string{"a", "a", "b", "", "c", "d"} {
		next := *cur
		next.Designs = d
		tr.Project("p", &next)
	}
	want := []string{"designs.changed {Project:p}", "designs.changed {Project:p}"}
	if got := drain(t, s); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events %q, want %q", got, want)
	}
}

// TestTrackerQuestions: session.question once a menu opens on a session
// after the baseline, when another question replaces it, and once it
// goes (no question); the same question again publishes nothing.
// ticker.changed follows the ticker's times and its failing flag: after
// a PR poll, after a sync, when gh_failing flips; not when nothing moved.
// The event's ticker is made only when it is published.
func TestTrackerTicker(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all})
	tr := NewTracker(b, nil)
	t0 := time.Date(2026, 10, 13, 14, 0, 0, 0, time.UTC)
	made := 0
	look := func(tt TickerTimes) {
		st := state()
		st.Ticker = tt
		st.TickerWatch = func() proto.WatchTicker {
			made++
			return proto.WatchTicker{PRChecked: tt.PRChecked, Synced: tt.Synced, PRPollSeconds: 120, GHFailing: tt.GHFailing}
		}
		tr.Project("p", st)
	}
	look(TickerTimes{})
	for _, tc := range []struct {
		name string
		tt   TickerTimes
		want string
	}{
		{"poll", TickerTimes{PRChecked: t0}, "ticker.changed {Project:p Ticker:{PRChecked:2026-10-13 14:00:00 +0000 UTC Synced:0001-01-01 00:00:00 +0000 UTC PRPollSeconds:120 GHFailing:false NoPRHost:false}}"},
		{"unchanged", TickerTimes{PRChecked: t0.In(time.FixedZone("x", 3600))}, ""},
		{"sync", TickerTimes{PRChecked: t0, Synced: t0.Add(time.Minute)}, "ticker.changed {Project:p Ticker:{PRChecked:2026-10-13 14:00:00 +0000 UTC Synced:2026-10-13 14:01:00 +0000 UTC PRPollSeconds:120 GHFailing:false NoPRHost:false}}"},
		{"failing", TickerTimes{PRChecked: t0, Synced: t0.Add(time.Minute), GHFailing: true}, "ticker.changed {Project:p Ticker:{PRChecked:2026-10-13 14:00:00 +0000 UTC Synced:2026-10-13 14:01:00 +0000 UTC PRPollSeconds:120 GHFailing:true NoPRHost:false}}"},
		{"fixed", TickerTimes{PRChecked: t0.Add(2 * time.Minute), Synced: t0.Add(time.Minute)}, "ticker.changed {Project:p Ticker:{PRChecked:2026-10-13 14:02:00 +0000 UTC Synced:2026-10-13 14:01:00 +0000 UTC PRPollSeconds:120 GHFailing:false NoPRHost:false}}"},
	} {
		look(tc.tt)
		if got := fmt.Sprint(drain(t, s)); got != fmt.Sprint(nonEmpty(tc.want)) {
			t.Errorf("%s: %s, want %s", tc.name, got, nonEmpty(tc.want))
		}
	}
	if made != 4 {
		t.Errorf("the ticker was made %d times, want 4: once per event", made)
	}
}

// TestTrackerChanges: thread.changes when a thread's changes appear
// (its session went live) or differ; not when they are the same, nor
// when they go (its session ended).
func TestTrackerChanges(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all})
	tr := NewTracker(b, nil)
	look := func(c *Changes) {
		st := state()
		ts := st.Threads["t-0001"]
		ts.Changes = c
		st.Threads["t-0001"] = ts
		tr.Project("p", st)
	}
	c := Changes{Project: "p", Thread: "t-0001", Head: "abc", Dirty: true, Files: 1, Add: 2}
	look(&c)
	if got := drain(t, s); len(got) != 0 {
		t.Fatalf("baseline %v", got)
	}
	same := c
	look(&same)
	c2 := c
	c2.Dirty, c2.Ahead, c2.Unpushed = false, 1, 1
	look(&c2)
	look(nil)
	look(&c2)
	want := []string{
		"thread.changes {Project:p Thread:t-0001 Head:abc Branch: Dirty:false Ahead:1 Unpushed:1 Pushed:false Files:1 Add:2 Del:0}",
		"thread.changes {Project:p Thread:t-0001 Head:abc Branch: Dirty:false Ahead:1 Unpushed:1 Pushed:false Files:1 Add:2 Del:0}",
	}
	if got := drain(t, s); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func TestTrackerQuestions(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: []string{mod.PermProjectRead}})
	tr := NewTracker(b, nil)
	ask := func(q string) *Ask {
		return &Ask{Question: q, Options: []AskOption{{Label: "Yes"}, {Label: "No"}}}
	}
	old := Question{Session: "s-1", Project: "p", Role: "coordinator", Question: ask("Before?")}
	tr.Questions([]Question{old})
	if got := drain(t, s); len(got) != 0 {
		t.Fatalf("baseline %v", got)
	}
	a := Question{Session: "s-2", Project: "p", Thread: "t-0001", Role: "thread", Question: ask("Ship it?")}
	tr.Questions([]Question{old, a})
	tr.Questions([]Question{old, a})
	a.Question = ask("Really?")
	tr.Questions([]Question{old, a})
	tr.Questions([]Question{old})
	var got []string
	for _, e := range drainEvents(t, s) {
		q := e.Data.(Question)
		w := "none"
		if q.Question != nil {
			w = q.Question.Question
		}
		got = append(got, e.Name+" "+q.Session+" "+w)
	}
	want := []string{"session.question s-2 Ship it?", "session.question s-2 Really?", "session.question s-2 none"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events %q, want %q", got, want)
	}
	// Without project_read, none of it.
	n := b.Subscribe("n", Options{})
	tr.Questions([]Question{old, a})
	if got := drain(t, n); len(got) != 0 {
		t.Errorf("without project_read: %v", got)
	}
}
