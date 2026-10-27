package session

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestMenuHook: AskUserQuestion's PreToolUse input is the session's
// open question while the agent is blocked on it, answered questions
// marked; its PostToolUse, or another call's PreToolUse of the tool,
// replaces it, and none shows while the agent isn't blocked on it.
func TestMenuHook(t *testing.T) {
	s, err := Start(Config{
		ID: "s-menu", Role: proto.RoleThread, Project: "p",
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
	input := func(q ...string) map[string]any {
		var qs []any
		for _, x := range q {
			qs = append(qs, map[string]any{"question": x, "header": "H", "options": []any{map[string]any{"label": "A"}, map[string]any{"label": "B"}}})
		}
		return map[string]any{"questions": qs}
	}
	hook("SessionStart", map[string]any{"source": "startup"})
	hook("UserPromptSubmit", map[string]any{"prompt": "go"})
	hook("PreToolUse", map[string]any{"tool_name": "AskUserQuestion", "tool_use_id": "q1", "tool_input": input("One?", "Two?")})
	if q, _ := s.Menu(); q != nil || s.Info().Question != nil {
		t.Fatalf("a menu before the agent blocks on it: %+v", q)
	}
	hook("PermissionRequest", map[string]any{"tool_name": "AskUserQuestion", "tool_use_id": "q1"})
	q, call := s.Menu()
	if q == nil || call != "q1" || !q.Screen || len(q.Questions) != 2 || s.Info().Question == nil || s.Info().Reason != "question" {
		t.Fatalf("menu %+v %q, info %+v", q, call, s.Info())
	}
	if s.MenuAnswered("q0", 0, "A") {
		t.Fatal("another call's menu took an answer")
	}
	if !s.MenuAnswered("q1", 0, "A") {
		t.Fatal("not answered")
	}
	if q, _ = s.Menu(); !q.Questions[0].Answered || q.Questions[0].Answer != "A" || q.Questions[1].Answered {
		t.Fatalf("after an answer: %+v", q)
	}
	hook("PostToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "b1"})
	hook("PermissionRequest", map[string]any{"tool_name": "AskUserQuestion", "tool_use_id": "q1"})
	if q, _ = s.Menu(); q == nil {
		t.Fatal("another call's end took the menu down")
	}
	hook("PostToolUse", map[string]any{"tool_name": "AskUserQuestion", "tool_use_id": "q1"})
	if q, _ = s.Menu(); q != nil {
		t.Fatalf("after the call: %+v", q)
	}
}
