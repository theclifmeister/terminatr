package modbus

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// drain is the events queued in s, after its mod.ready, as "name data".
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
		"inbox.item {Project:p ID:i2 Kind:report Subject:t-0002 Summary: NeedsUser:false}",
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
		"session.state {Session:s-1 Project:p Thread: Role:coordinator Agent:claude State:idle Reason: Context:40}",
		"session.state {Session:s-2 Project: Thread: Role:shell Agent: State: Reason: Context:-1}",
		"session.state {Session:s-1 Project:p Thread: Role:coordinator Agent:claude State:exited Reason: Context:40}",
	}
	if got := drain(t, s); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events:\n%q\nwant\n%q", got, want)
	}
}
