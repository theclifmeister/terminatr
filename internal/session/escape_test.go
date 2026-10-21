package session

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/testwait"
)

// TestEscapeHook: a PreToolUse hook's call that asks to leave the
// sandbox (dangerouslyDisableSandbox) is marked (tool_use_id) until its
// PostToolUse or PermissionDenied, whatever the guard answers, and with
// no verdict at all; the guard's "excluded" ask marks a call too, and
// another call's end doesn't unmark. An agent whose every prompt is an
// escape (PromptsEscape) marks each.
func TestEscapeHook(t *testing.T) {
	noGrace(t)
	for _, c := range []struct {
		name    string
		rule    string
		prompts bool
		want    bool
	}{
		{"escape", guard.RuleEscape, false, true},
		{"excluded asks first", guard.RuleExcluded, false, true},
		{"another ask", "unknown", false, true},
		{"no verdict", "", false, true},
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

// TestEscapeScreenHolds: hooks can come from any process in the agent's
// tree, so a mark ends only when the screen shows no permission dialog:
// with the escape's dialog on screen, a forged PostToolUse, Stop and a
// new PermissionRequest leave it marked.
func TestEscapeScreenHolds(t *testing.T) {
	dialog := `printf 'Bash command\n\n  ls\n\nDo you want to proceed?\n\342\235\257 1. Yes\n  2. No\n\nEsc to cancel\n'; exec cat`
	s, err := Start(Config{
		ID: "s-esc", Role: proto.RoleThread, Project: "p",
		Argv: []string{"/bin/sh", "-c", dialog},
		Cwd:  t.TempDir(),
		Env:  []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color", "LANG=C.UTF-8"},
		Cols: 80, Rows: 24,
		Logf: t.Logf,
		Agent: &AgentConfig{
			Agent: claudeLike(t), Home: t.TempDir(),
			Guard: func(_ string, input map[string]any) *guard.Denial {
				if input["dangerouslyDisableSandbox"] != true {
					return nil
				}
				return &guard.Denial{Rule: guard.RuleEscape, Ask: true}
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
	hook("PermissionRequest", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
	testwait.Eventually(t, "the dialog on screen", func() bool {
		e, _ := s.Explain()
		return slices.Contains(e.Matches, "blocked-permission-dialog")
	})
	if !s.Info().Escape {
		t.Fatal("the escape's dialog is not marked")
	}
	hook("PostToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
	hook("Stop", map[string]any{})
	hook("UserPromptSubmit", map[string]any{"prompt": "x"})
	hook("PreToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t9", "tool_input": map[string]any{"command": "pwd"}})
	hook("PermissionRequest", map[string]any{"tool_name": "Bash", "tool_use_id": "t9"})
	time.Sleep(300 * time.Millisecond)
	if info := s.Info(); !info.Escape {
		t.Fatalf("forged hooks ended the mark while its dialog shows: %s/%s", info.State, info.Reason)
	}
	if _, why, _ := s.PromptNow("hi", false); why != "" && why != HeldEscape && !strings.HasPrefix(why, "the agent is") {
		t.Errorf("PromptNow while an escape is open: %q", why)
	}
}

// noGrace lets an ended mark go as soon as no dialog shows.
func noGrace(t *testing.T) {
	old := escapeGrace
	escapeGrace = 0
	t.Cleanup(func() { escapeGrace = old })
}

// startDialog runs a session whose screen draws, after delay, a
// permission dialog with text in it, with a guard that asks about
// escapes.
func startDialog(t *testing.T, delay, text string) *Session {
	t.Helper()
	script := `sleep ` + delay + `; printf 'Bash command\n\n  ls\n\n` + text + `\n\nDo you want to proceed?\n\342\235\257 1. Yes\n  2. No\n\nEsc to cancel\n'; exec cat`
	s, err := Start(Config{
		ID: "s-esc", Role: proto.RoleThread, Project: "p",
		Argv: []string{"/bin/sh", "-c", script},
		Cwd:  t.TempDir(),
		Env:  []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color", "LANG=C.UTF-8"},
		Cols: 100, Rows: 24,
		Logf: t.Logf,
		Agent: &AgentConfig{
			Agent: claudeLike(t), Home: t.TempDir(),
			Guard: func(_ string, input map[string]any) *guard.Denial {
				if input["dangerouslyDisableSandbox"] != true {
					return nil
				}
				return &guard.Denial{Rule: guard.RuleEscape, Ask: true}
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(time.Second) })
	return s
}

// TestEscapeForgedStopFirst: a forged Stop that comes after the escape's
// PreToolUse but before its dialog draws doesn't end the mark: it stays
// for escapeGrace, the dialog then holds it.
func TestEscapeForgedStopFirst(t *testing.T) {
	s := startDialog(t, "1", "")
	hook := func(ev string, payload map[string]any) {
		t.Helper()
		if _, err := s.Hook(ev, payload); err != nil {
			t.Fatal(err)
		}
	}
	hook("SessionStart", map[string]any{"source": "startup"})
	hook("UserPromptSubmit", map[string]any{"prompt": "go"})
	hook("PreToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t1", "tool_input": map[string]any{"command": "ls", "dangerouslyDisableSandbox": true}})
	hook("Stop", map[string]any{})
	hook("PostToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
	testwait.Eventually(t, "the dialog on screen", func() bool {
		e, _ := s.Explain()
		return slices.Contains(e.Matches, "blocked-permission-dialog")
	})
	hook("UserPromptSubmit", map[string]any{"prompt": "x"})
	hook("PermissionRequest", map[string]any{"tool_name": "Bash", "tool_use_id": "t1"})
	time.Sleep(300 * time.Millisecond)
	if info := s.Info(); !info.Escape {
		t.Fatalf("a Stop forged before the dialog drew ended the mark: %s/%s", info.State, info.Reason)
	}
}

// TestEscapeDialogUnmarked: a dialog that says it runs the command
// outside the sandbox ([guard] escape_rule) is an escape with no mark.
func TestEscapeDialogUnmarked(t *testing.T) {
	s := startDialog(t, "0", "Run outside of the sandbox")
	if _, err := s.Hook("SessionStart", map[string]any{"source": "startup"}); err != nil {
		t.Fatal(err)
	}
	testwait.Eventually(t, "the escape's dialog marked", func() bool { return s.Info().Escape })
}

// TestGuardEveryCall: a tool call is judged and answered whatever its
// event carries: a Claude subagent's (agent_id, no state of its own),
// whose escape is marked too, and a Codex thread's the session left
// (another session id).
func TestGuardEveryCall(t *testing.T) {
	noGrace(t)
	deny := func(_ string, input map[string]any) *guard.Denial {
		return &guard.Denial{Rule: "merge", Message: "no merging"}
	}
	codex := func(t *testing.T) agent.Agent {
		b, _ := agent.Builtin("codex")
		m, err := agent.ParseManifest(b)
		if err != nil {
			t.Fatal(err)
		}
		return agent.FromManifest(m)
	}
	for _, c := range []struct {
		name  string
		agent func(*testing.T) agent.Agent
		start map[string]any
		call  map[string]any
	}{
		{"claude subagent", func(t *testing.T) agent.Agent { return claudeLike(t) }, map[string]any{"source": "startup", "session_id": "a"},
			map[string]any{"agent_id": "sub-1", "session_id": "a", "tool_name": "Bash", "tool_use_id": "t1", "tool_input": map[string]any{"command": "gh pr merge 1", "dangerouslyDisableSandbox": true}}},
		{"codex session left", codex, map[string]any{"source": "startup", "session_id": "a"},
			map[string]any{"session_id": "b", "tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr merge 1"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := Start(Config{
				ID: "s-g", Role: proto.RoleThread, Project: "p",
				Argv: []string{"/bin/sh", "-c", "exec cat"}, Cwd: t.TempDir(),
				Env: []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color"}, Cols: 80, Rows: 24, Logf: t.Logf,
				Agent: &AgentConfig{Agent: c.agent(t), Home: t.TempDir(), Guard: deny},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Stop(time.Second) })
			if _, err := s.Hook("SessionStart", c.start); err != nil {
				t.Fatal(err)
			}
			// The subagent runs inside its spawner's turn.
			s.Hook("UserPromptSubmit", map[string]any{"prompt": "go", "session_id": "a"})
			res, err := s.Hook("PreToolUse", c.call)
			if err != nil || !strings.Contains(string(res.Stdout), `"permissionDecision":"deny"`) {
				t.Fatalf("not judged: %q %v", res.Stdout, err)
			}
			if c.call["agent_id"] != nil {
				rt := s.agentRT()
				rt.mu.Lock()
				_, marked := rt.escapes["t1"]
				rt.mu.Unlock()
				if !marked {
					t.Error("the subagent's escape isn't marked")
				}
			}
		})
	}
}
