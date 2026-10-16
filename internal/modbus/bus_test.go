package modbus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mod"
)

func next(t *testing.T, s *Sub) Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ev, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return ev
}

func empty(t *testing.T, s *Sub) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if ev, err := s.Next(ctx); err == nil {
		t.Fatalf("unexpected event %s %+v", ev.Name, ev.Data)
	}
}

var all = []string{mod.PermProjectRead, mod.PermViews}

func TestReadyFirstThenInOrder(t *testing.T) {
	b := New()
	built := 0
	s := b.Subscribe("m", Options{Grants: all, Ready: func() any { built++; return "snap" }})
	b.Publish(TaskChanged, "p", 1)
	b.Publish(InboxItem, "p", 2)
	if built != 0 {
		t.Fatal("snapshot built before delivery")
	}
	ev := next(t, s)
	if ev.Name != ModReady || ev.Data != "snap" || built != 1 {
		t.Fatalf("first = %+v, built %d", ev, built)
	}
	a, c := next(t, s), next(t, s)
	if a.Data != 1 || c.Data != 2 || a.Seq >= c.Seq || ev.Seq <= c.Seq {
		t.Fatalf("order: ready %d, %d %v, %d %v", ev.Seq, a.Seq, a.Data, c.Seq, c.Data)
	}
	empty(t, s)
}

func TestQueueBoundDropsOldest(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all})
	for i := range QueueSize + 5 {
		b.Publish(ClockMinute, "", i)
	}
	if s.Len() != QueueSize {
		t.Fatalf("len %d", s.Len())
	}
	if ev := next(t, s); ev.Name != ModReady || ev.Dropped != 0 {
		t.Fatalf("ready = %+v", ev)
	}
	ev := next(t, s)
	if ev.Data != 5 || ev.Dropped != 5 {
		t.Fatalf("first after drops: data %v dropped %d", ev.Data, ev.Dropped)
	}
	if ev := next(t, s); ev.Dropped != 0 {
		t.Fatalf("dropped count given twice: %d", ev.Dropped)
	}
}

func TestGrantsEventsProjects(t *testing.T) {
	b := New()
	none := b.Subscribe("none", Options{})
	some := b.Subscribe("some", Options{Grants: all, Events: []string{TaskChanged, ClockMinute},
		Project: func(slug string) bool { return slug == "a" }})
	next(t, none)
	next(t, some)
	b.Publish(TaskChanged, "a", "task a")
	b.Publish(TaskChanged, "b", "task b")
	b.Publish(InboxItem, "a", "item")
	b.Publish(SessionState, "b", "session b")
	b.Publish(ViewOpened, "a", "view")
	b.Publish(ClockMinute, "", "tick")
	var got []any
	for range 2 {
		got = append(got, next(t, some).Data)
	}
	empty(t, some)
	if fmt.Sprint(got) != "[task a tick]" {
		t.Fatalf("some got %v", got)
	}
	got = nil
	for range 2 {
		got = append(got, next(t, none).Data)
	}
	empty(t, none)
	if fmt.Sprint(got) != "[session b tick]" {
		t.Fatalf("none got %v", got)
	}
}

// TestWants: a source asks whether any mod would get an event: one
// that listens to it, has its grant and runs for its project.
func TestWants(t *testing.T) {
	b := New()
	if b.Wants(ThreadChanges, "a") {
		t.Fatal("no mod, yet wanted")
	}
	b.Subscribe("nogrant", Options{Events: []string{ThreadChanges}})
	b.Subscribe("other", Options{Grants: all, Events: []string{TaskChanged}})
	if b.Wants(ThreadChanges, "a") {
		t.Fatal("wanted without the grant or the event")
	}
	s := b.Subscribe("some", Options{Grants: all, Events: []string{ThreadChanges},
		Project: func(slug string) bool { return slug == "a" }})
	if !b.Wants(ThreadChanges, "a") || b.Wants(ThreadChanges, "b") {
		t.Fatal("wants follows the events and projects")
	}
	s.Close()
	b.Subscribe("all", Options{Grants: all})
	if !b.Wants(ThreadChanges, "b") {
		t.Fatal("a mod listening to everything wants it")
	}
}

func TestResubscribeStartsOver(t *testing.T) {
	b := New()
	old := b.Subscribe("m", Options{Grants: all})
	next(t, old)
	b.Publish(TaskChanged, "p", "missed")
	s := b.Subscribe("m", Options{Grants: all})
	if _, err := old.Next(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("old queue: %v", err)
	}
	old.Close() // must not drop the new queue
	if !b.Active() {
		t.Fatal("new queue gone with the old")
	}
	if ev := next(t, s); ev.Name != ModReady {
		t.Fatalf("first = %s", ev.Name)
	}
	empty(t, s) // no replay
	s.Close()
	if b.Active() {
		t.Fatal("still active")
	}
}

func TestDeliverLimitAndGone(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all})
	b.Publish(ClockMinute, "", "slow")
	b.Publish(ClockMinute, "", "fails")
	b.Publish(ClockMinute, "", "ok")
	b.Publish(ClockMinute, "", "gone")
	b.Publish(ClockMinute, "", "after")
	var mu sync.Mutex
	var logs, seen []string
	logf := func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(f, a...))
	}
	h := func(ctx context.Context, ev Event) error {
		seen = append(seen, fmt.Sprint(ev.Data))
		switch ev.Data {
		case "slow":
			<-ctx.Done()
			return ctx.Err()
		case "fails":
			return errors.New("boom")
		case "gone":
			return fmt.Errorf("host exited: %w", ErrGone)
		}
		return nil
	}
	err := deliver(context.Background(), s, h, logf, 30*time.Millisecond)
	if !errors.Is(err, ErrGone) {
		t.Fatalf("deliver: %v", err)
	}
	if fmt.Sprint(seen) != "[<nil> slow fails ok gone]" {
		t.Fatalf("seen %v", seen)
	}
	if len(logs) != 2 || !strings.Contains(logs[0], "clock.minute handler ran past 30ms; cancelled") || !strings.Contains(logs[1], "boom") {
		t.Fatalf("logs %q", logs)
	}
	if s.Len() != 1 {
		t.Fatalf("left %d", s.Len())
	}
}

func TestDeliverEndsWithContext(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Deliver(ctx, s, func(context.Context, Event) error { return nil }, t.Logf) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("deliver: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("deliver kept running")
	}
}

func TestWaitAndSetEvents(t *testing.T) {
	b := New()
	s := b.Subscribe("m", Options{Grants: all, Events: []string{ClockMinute}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := s.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait with only mod.ready pending = %v, want the deadline", err)
	}
	cancel()
	b.Publish(TaskChanged, "p", 1) // filtered out
	s.SetEvents([]string{TaskChanged})
	b.Publish(ClockMinute, "", 2) // filtered out now
	done := make(chan error, 1)
	go func() { done <- s.Wait(context.Background()) }()
	b.Publish(TaskChanged, "p", 3)
	if err := <-done; err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if ev := next(t, s); ev.Name != ModReady {
		t.Fatalf("first %s, want mod.ready", ev.Name)
	}
	if ev := next(t, s); ev.Name != TaskChanged || ev.Data != 3 {
		t.Fatalf("got %s %v, want task.changed 3", ev.Name, ev.Data)
	}
	empty(t, s)
	s.Close()
	if err := s.Wait(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait on a closed queue = %v", err)
	}
}
