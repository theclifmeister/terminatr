package server

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// TestModEvents: a subscribed mod gets mod.ready with its projects, then
// what changes in their files and the ticker's state, after a wake or
// the poll; a mod without project_read gets no project events.
func TestModEvents(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(envModEventsPoll, "50ms")
	pr := newWatchProject(t)
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Work", Status: "started", Steps: []string{"a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p}, log: log.New(&testWriter{t: t}, "server: ", 0),
		sessions: map[string]*session.Session{}, modEv: newModEvents()}
	// The changes below must land in one look, whose events have a set
	// order: a look between two of them (the poll, a wake, the minute)
	// splits them, and the order with them (T283). The test holds the
	// looks off while it makes them, and learns when one has run.
	var looking sync.RWMutex
	looks := make(chan struct{}, 1)
	s.modEv.hold = func() func() {
		looking.RLock()
		return func() {
			looking.RUnlock()
			select {
			case looks <- struct{}{}:
			default:
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.runModEvents(ctx)

	reader := s.SubscribeMod("reader", []string{modbus.GrantProjectRead}, nil)
	blind := s.SubscribeMod("blind", nil, nil)
	next := func(sub *modbus.Sub) modbus.Event {
		t.Helper()
		c, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		ev, err := sub.Next(c)
		if err != nil {
			t.Fatalf("%s: %v", sub.Mod(), err)
		}
		return ev
	}
	ev := next(reader)
	if ev.Name != modbus.ModReady {
		t.Fatalf("first event %s", ev.Name)
	}
	if r := ev.Data.(modbus.Ready); len(r.Projects) != 1 || r.Projects[0].Project != "demo" {
		t.Fatalf("ready %+v", r)
	}
	if r := next(blind).Data.(modbus.Ready); r.Projects != nil {
		t.Fatalf("blind ready has projects: %+v", r)
	}
	// Let a look take its baseline: hold runs only while a mod is
	// subscribed, so any look it signals has one.
	<-looks

	looking.Lock()
	if _, err := pr.Tasks().StepCheck(human, 1, 1, true); err != nil {
		t.Fatal(err)
	}
	var rec *thread.Record
	rec, err = thread.Create(pr, thread.Record{Title: "work", Task: "T1", Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(p.Sessions), 0o700)
	if err := os.WriteFile(ticker.StatePath(p.Sessions), []byte(`{"threads": {"demo/`+rec.ID+`": {"pr": {"number": 4, "state": "OPEN", "checks": "fail", "merge_state": "BEHIND"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.AddItem("report", rec.ID, "t-0001 reported", false); err != nil {
		t.Fatal(err)
	}
	s.modEv.wake()
	looking.Unlock()

	var names []string
	var task modbus.Task
	var prEv modbus.PR
	for len(names) < 5 {
		ev := next(reader)
		names = append(names, ev.Name)
		switch d := ev.Data.(type) {
		case modbus.Task:
			task = d
		case modbus.PR:
			prEv = d
		}
	}
	want := []string{modbus.ThreadStarted, modbus.TaskChanged, modbus.ThreadState, modbus.PRChanged, modbus.InboxItem}
	if len(names) != len(want) {
		t.Fatalf("events %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("events %v, want %v", names, want)
		}
	}
	if task.ID != "T1" || task.From != "started" || task.To != "started" || task.StepsDone != 1 || task.StepsTotal != 2 {
		t.Fatalf("task.changed %+v", task)
	}
	if prEv.Number != 4 || prEv.Checks != "fail" || !prEv.Behind || prEv.Thread != rec.ID {
		t.Fatalf("pr.changed %+v", prEv)
	}

	// The blind mod got none of it, but does get the clock and sessions.
	c, done := context.WithTimeout(ctx, 100*time.Millisecond)
	defer done()
	if ev, err := blind.Next(c); err == nil {
		t.Fatalf("blind got %s", ev.Name)
	}
}

func TestUntilMinute(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 30, 15, 0, time.UTC)
	if d := untilMinute(now); d != 45*time.Second {
		t.Fatalf("untilMinute = %v", d)
	}
}

// TestSessionUsage: session.state carries what a thread's session has
// used so far, from its thread's record; none for a coordinator, or for
// a session the thread no longer runs.
func TestSessionUsage(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	pr := newWatchProject(t)
	rec, err := thread.Create(pr, thread.Record{Title: "work", Task: "T1", Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := thread.Update(pr, rec.ID, func(r *thread.Record) error {
		r.Session = "s-3"
		r.Usage = thread.Usage{Turns: 2, Input: 1000, Output: 500, CacheRead: 8500, CostUSD: 0.42}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		info   proto.SessionInfo
		tokens int64
		cost   float64
	}{
		{proto.SessionInfo{ID: "s-3", Project: "demo", Thread: rec.ID, Role: proto.RoleThread}, 10000, 0.42},
		{proto.SessionInfo{ID: "s-9", Project: "demo", Thread: rec.ID, Role: proto.RoleThread}, 0, 0},
		{proto.SessionInfo{ID: "s-3", Project: "demo", Role: proto.RoleCoordinator}, 0, 0},
	} {
		if tokens, cost := sessionUsage(c.info); tokens != c.tokens || cost != c.cost {
			t.Errorf("%+v: %d tokens, $%v; want %d, $%v", c.info, tokens, cost, c.tokens, c.cost)
		}
	}
}

// TestModReport: report.get and report.stored give the report's number
// and its ## Report section, not the whole file (T301 B2).
func TestModReport(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	pr := newWatchProject(t)
	rec, err := thread.Create(pr, thread.Record{Title: "work", Task: "T1", Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"## Report\nfirst\n## Next\n- go\n", "PR: https://github.com/o/r/pull/3\n## Report\nFixed the login.\n\nTwo lines.\n## Next\n- merge it\n## Check\nrun it\n"} {
		if _, err := thread.StoreReport(pr, rec.ID, text, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	r := modReport("demo", rec.ID)
	if r == nil || r.Report != 2 || r.Sections.Report != "Fixed the login.\n\nTwo lines." || r.PR != "https://github.com/o/r/pull/3" ||
		len(r.Sections.Next) != 1 || r.Sections.Next[0] != "merge it" || len(r.Sections.Check) != 1 || r.Task != "T1" {
		t.Fatalf("report %+v", r)
	}
}
