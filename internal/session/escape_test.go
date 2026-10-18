package session

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestEscapeMod: a call the mod says leaves the sandbox (MarkEscape)
// makes a permission prompt open while it is the user's (Info.Escape),
// until that call ends (EndEscape); another call's prompt ending doesn't
// end it (parallel calls), and the turn's end ends every mark.
func TestEscapeMod(t *testing.T) {
	s := startMod(t, "", true, make(chan PromptResolution, 1))
	state := func(st agent.State, reason string) {
		t.Helper()
		if err := s.ModState(st, reason, "test"); err != nil {
			t.Fatal(err)
		}
	}
	state(agent.StateWorking, "")
	s.MarkEscape("e1")
	if s.Info().Escape {
		t.Fatal("escape while working: no prompt yet")
	}
	state(agent.StateBlocked, "permission")
	if !s.Info().Escape {
		t.Fatal("the escape's prompt is not marked")
	}
	// A parallel call's prompt was open first and is answered: the
	// escape's prompt shows next, still marked.
	state(agent.StateWorking, "")
	state(agent.StateBlocked, "permission")
	if !s.Info().Escape {
		t.Fatal("another prompt's end took the escape's mark")
	}
	s.EndEscape("e1")
	if s.Info().Escape {
		t.Fatal("the escape's call ended, its mark didn't")
	}
	state(agent.StateWorking, "")
	s.MarkEscape("e2")
	state(agent.StateIdle, "")
	state(agent.StateWorking, "")
	state(agent.StateBlocked, "permission")
	if s.Info().Escape {
		t.Fatal("a mark outlived its turn")
	}
}

// TestEscapeHook: without the mod, the guard's "escape" ask on a
// PreToolUse hook marks that call (tool_use_id) until its PostToolUse
// or PermissionDenied; another ask doesn't mark, and another call's end
// doesn't unmark. An agent whose every prompt is an escape
// (PromptsEscape) marks each.
func TestEscapeHook(t *testing.T) {
	for _, c := range []struct {
		name    string
		rule    string
		prompts bool
		want    bool
	}{
		{"escape", guard.RuleEscape, false, true},
		{"unknown", "unknown", false, false},
		{"every prompt", "", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := Start(Config{
				ID: "s-esc", Role: proto.RoleThread, Project: "p",
				Argv: []string{"/bin/sh", "-c", "exec cat"},
				Cwd:  t.TempDir(),
				Env:  []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color"},
				Cols: 80, Rows: 24,
				Logf: t.Logf,
				Agent: &AgentConfig{
					Agent: claudeLike(t), Home: t.TempDir(), PromptsEscape: c.prompts,
					Guard: func(_ string, input map[string]any) *guard.Denial {
						if c.rule == "" || input["dangerouslyDisableSandbox"] != true {
							return nil
						}
						return &guard.Denial{Rule: c.rule, Ask: true}
					},
				},
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
			hook("UserPromptSubmit", map[string]any{"prompt": "go"})
			hook("PreToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t1", "tool_input": map[string]any{"command": "ls", "dangerouslyDisableSandbox": true}})
			hook("PreToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t2", "tool_input": map[string]any{"command": "pwd"}})
			hook("PermissionRequest", map[string]any{"tool_name": "Bash", "tool_use_id": "t2"})
			hook("PostToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t2"})
			hook("PermissionRequest", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
			if info := s.Info(); info.State != "blocked" || info.Escape != c.want {
				t.Fatalf("state %s/%s, escape %v; want escape %v", info.State, info.Reason, info.Escape, c.want)
			}
			if c.prompts {
				return
			}
			hook("PostToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
			hook("PreToolUse", map[string]any{"tool_name": "Write", "tool_use_id": "t3", "tool_input": map[string]any{"file_path": "x"}})
			hook("PermissionRequest", map[string]any{"tool_name": "Write", "tool_use_id": "t3"})
			if info := s.Info(); info.State != "blocked" || info.Escape {
				t.Fatalf("after the escape's call ended: %s/%s, escape %v", info.State, info.Reason, info.Escape)
			}
		})
	}
}
