package session

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestEscapeMod: a call the mod says leaves the sandbox (MarkEscape)
// makes the permission prompt it brings the user's (Info.Escape) until
// that prompt ends; the next prompt is anyone's again, and a turn that
// ends without one drops the mark.
func TestEscapeMod(t *testing.T) {
	s := startMod(t, "", true, make(chan PromptResolution, 1))
	state := func(st agent.State, reason string) {
		t.Helper()
		if err := s.ModState(st, reason, "test"); err != nil {
			t.Fatal(err)
		}
	}
	state(agent.StateWorking, "")
	s.MarkEscape()
	if s.Info().Escape {
		t.Fatal("escape while working: no prompt yet")
	}
	state(agent.StateBlocked, "permission")
	if !s.Info().Escape {
		t.Fatal("the escape's prompt is not marked")
	}
	state(agent.StateWorking, "")
	state(agent.StateBlocked, "permission")
	if s.Info().Escape {
		t.Fatal("the next prompt is still marked an escape")
	}
	state(agent.StateWorking, "")
	s.MarkEscape()
	state(agent.StateIdle, "")
	state(agent.StateWorking, "")
	state(agent.StateBlocked, "permission")
	if s.Info().Escape {
		t.Fatal("a mark outlived its turn")
	}
}

// TestEscapeHook: without the mod, the guard's "escape" ask on a
// PreToolUse hook marks the prompt that follows; another ask doesn't.
// An agent whose every prompt is an escape (PromptsEscape) marks each.
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
					Guard: func(string, map[string]any) *guard.Denial {
						if c.rule == "" {
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
			for _, ev := range []struct {
				name    string
				payload map[string]any
			}{
				{"SessionStart", map[string]any{"source": "startup"}},
				{"UserPromptSubmit", map[string]any{"prompt": "go"}},
				{"PreToolUse", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "ls", "dangerouslyDisableSandbox": true}}},
				{"PermissionRequest", map[string]any{"tool_name": "Bash"}},
			} {
				if _, err := s.Hook(ev.name, ev.payload); err != nil {
					t.Fatal(err)
				}
			}
			if info := s.Info(); info.State != "blocked" || info.Escape != c.want {
				t.Fatalf("state %s/%s, escape %v; want escape %v", info.State, info.Reason, info.Escape, c.want)
			}
		})
	}
}
