package session

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestTurnSince: the session's turn starts when the agent goes working,
// keeps its start through a permission prompt within the turn, and ends
// when the agent idles; the next turn starts anew.
func TestTurnSince(t *testing.T) {
	s, err := Start(Config{
		ID: "s-turn", Role: proto.RoleThread, Project: "p",
		Argv: []string{"/bin/sh", "-c", "exec cat"},
		Cwd:  t.TempDir(),
		Env:  []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color"},
		Cols: 80, Rows: 24,
		Logf:  t.Logf,
		Agent: &AgentConfig{Agent: claudeLike(t), Home: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(time.Second) })
	hook := func(ev string, payload map[string]any) {
		t.Helper()
		if _, err := s.Hook(ev, payload); err != nil {
			t.Fatal(err)
		}
	}
	hook("SessionStart", map[string]any{"source": "startup"})
	if info := s.Info(); !info.TurnSince.IsZero() {
		t.Fatalf("idle at start: turn since %v", info.TurnSince)
	}
	before := time.Now()
	hook("UserPromptSubmit", map[string]any{"prompt": "go"})
	info := s.Info()
	if info.State != "working" || info.TurnSince.Before(before.Add(-time.Second)) || info.TurnSince.After(time.Now()) {
		t.Fatalf("working: state %s, turn since %v (prompt at %v)", info.State, info.TurnSince, before)
	}
	start := info.TurnSince
	hook("PreToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t1", "tool_input": map[string]any{"command": "ls"}})
	hook("PermissionRequest", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
	if info := s.Info(); info.State != "blocked" || !info.TurnSince.Equal(start) {
		t.Fatalf("blocked within the turn: state %s, turn since %v, want %v", info.State, info.TurnSince, start)
	}
	hook("PostToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
	if info := s.Info(); info.State != "working" || !info.TurnSince.Equal(start) {
		t.Fatalf("working again: state %s, turn since %v, want %v", info.State, info.TurnSince, start)
	}
	hook("Stop", map[string]any{})
	if info := s.Info(); info.State != "idle" || !info.TurnSince.IsZero() {
		t.Fatalf("idle: state %s, turn since %v", info.State, info.TurnSince)
	}
	time.Sleep(10 * time.Millisecond)
	hook("UserPromptSubmit", map[string]any{"prompt": "again"})
	if info := s.Info(); info.State != "working" || !info.TurnSince.After(start) {
		t.Fatalf("next turn: state %s, turn since %v, after %v", info.State, info.TurnSince, start)
	}
}
