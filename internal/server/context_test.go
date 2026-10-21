package server

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

func TestContextWindow(t *testing.T) {
	for _, c := range []struct {
		model  string
		tokens int64
		want   int64
	}{
		{"claude-sonnet-5-5", 90_000, windowStandard},
		{"claude-sonnet-5-5[1m]", 90_000, windowLong},
		{"claude-opus-5-5", 250_000, windowLong}, // more than 200k proves the long window
	} {
		if got := contextWindow(c.model, c.tokens); got != c.want {
			t.Errorf("%s %d: %d, want %d", c.model, c.tokens, got, c.want)
		}
	}
}

// TestContextAlertPerCrossing: a coordinator's context rising past
// [ui] context_hint rings once; it rings again only after a fall below.
func TestContextAlertPerCrossing(t *testing.T) {
	h := t.TempDir()
	t.Setenv(home.Env, h)
	s := &Server{records: map[string]SessionRecord{
		"s-1": {ID: "s-1", Role: proto.RoleCoordinator},
		"s-2": {ID: "s-2", Role: proto.RoleThread},
	}}
	steps := []struct {
		id     string
		tokens int64
		alerts uint64
	}{
		{"s-1", 50_000, 0},  // 25%
		{"s-1", 90_000, 1},  // 45%: crossed
		{"s-1", 120_000, 1}, // still above
		{"s-1", 20_000, 1},  // a /clear
		{"s-1", 85_000, 2},  // crossed again
		{"s-2", 150_000, 2}, // a thread's context never rings
	}
	for i, st := range steps {
		s.setContext(st.id, "claude-x", st.tokens, 0)
		if got := s.alerts.Load(); got != st.alerts {
			t.Fatalf("step %d: %d alerts, want %d", i, got, st.alerts)
		}
	}
	// Never.
	os.WriteFile(filepath.Join(h, "config.toml"), []byte("[ui]\ncontext_hint = 0\n"), 0o600)
	s.setContext("s-1", "claude-x", 10_000, 0)
	s.setContext("s-1", "claude-x", 190_000, 0)
	if got := s.alerts.Load(); got != 2 {
		t.Fatalf("with the hint off: %d alerts", got)
	}
}

func TestContextOf(t *testing.T) {
	t.Setenv(home.Env, t.TempDir())
	sessions := []proto.SessionInfo{
		{ID: "s-1", Role: proto.RoleCoordinator, Project: "demo", Context: 84_000, ContextWindow: 200_000},
		{ID: "s-2", Role: proto.RoleCoordinator, Project: "other"},
	}
	c := contextOf("demo", sessions, config.DefaultContextHint)
	if c == nil || c.Percent != 42 || c.Threshold != 40 || !c.Hint || c.Tokens != 84_000 || c.Window != 200_000 {
		t.Fatalf("demo: %+v", c)
	}
	sessions[0].Context = 20_000
	if c := contextOf("demo", sessions, config.DefaultContextHint); c == nil || c.Percent != 10 || c.Hint {
		t.Fatalf("low: %+v", c)
	}
	if c := contextOf("other", sessions, config.DefaultContextHint); c != nil {
		t.Fatalf("no report yet: %+v", c)
	}
	if c := contextOf("none", sessions, config.DefaultContextHint); c != nil {
		t.Fatalf("no coordinator: %+v", c)
	}
}

// TestContextAgentWindow: the window the agent gives wins over the guess
// from the model's id; one smaller than the context is ignored.
func TestContextAgentWindow(t *testing.T) {
	t.Setenv(home.Env, t.TempDir())
	s := &Server{records: map[string]SessionRecord{"s-1": {ID: "s-1", Role: proto.RoleThread}}}
	get := func() ctxUse { v, _ := s.ctxOf.Load("s-1"); return v.(ctxUse) }
	s.setContext("s-1", "claude-x", 300_000, 1_000_000)
	if c := get(); c.window != 1_000_000 {
		t.Fatalf("window %d", c.window)
	}
	s.setContext("s-1", "claude-x", 50_000, 400_000)
	if c := get(); c.window != 400_000 {
		t.Fatalf("agent's window not taken: %d", c.window)
	}
	s.setContext("s-1", "claude-x", 50_000, 10)
	if c := get(); c.window != windowStandard {
		t.Fatalf("a window below the context: %d", c.window)
	}
}

// TestAgentUsage: usage the agent's JSONL file reports goes to the
// thread's totals and the session's context use; a turn-only report
// leaves the context alone.
func TestAgentUsage(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := thread.Create(p, thread.Record{Title: "T", Session: "s-1", State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{
		"s-1": {ID: "s-1", Role: proto.RoleThread, Project: p.Slug, Thread: r.ID},
	}}
	s.agentUsage("s-1", agent.Usage{Input: 1080, CacheRead: 11776, Output: 302, Context: 12856, ContextWindow: 258400, Key: "25402"})
	s.agentUsage("s-1", agent.Usage{Turns: 1})
	got, _ := thread.Load(p, r.ID)
	if want := (thread.Usage{Turns: 1, Input: 1080, CacheRead: 11776, Output: 302, Uncosted: 13158}); got.Usage != want {
		t.Fatalf("usage %+v, want %+v", got.Usage, want)
	}
	if v, ok := s.ctxOf.Load("s-1"); !ok || v.(ctxUse) != (ctxUse{12856, 258400}) {
		t.Fatalf("context %+v %v", v, ok)
	}
}
