package e2e

// Claude without the agent mod (T646): what the mod alone gave a Claude
// thread before, the typed tools and the usage, from tm mcp and the
// session's transcript, as Codex has them.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestClaudeWithoutMod: a Claude thread with the mod off (the default)
// gets tm mcp as its MCP server, whose tools/list holds the thread's
// typed tools under Claude's names and whose call runs as the thread;
// its turns' tokens and its context use come from its transcript (the
// fake writes Claude 2.1.306's usage lines), the window from the model's
// id; /clear and /compact forget the context use until the next turn
// reports it.
func TestClaudeWithoutMod(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	env.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)
	start := env.WaitFake("start", agentWait, nil)
	if cfgs, _ := start["mcp_config"].([]any); len(cfgs) != 1 {
		t.Fatalf("launch without --mcp-config: %v", start["argv"])
	}

	env.Prompt(th, "run mcp-status")
	rec := env.WaitFake("mcp", agentWait, nil)
	var tools []string
	for _, x := range rec["tools"].([]any) {
		tools = append(tools, x.(string))
	}
	for _, want := range []string{"mcp__terminatr__report", "mcp__terminatr__status", "mcp__terminatr__steps", "mcp__terminatr__done"} {
		if !slices.Contains(tools, want) {
			t.Errorf("tools/list lacks %s: %v", want, tools)
		}
	}
	if rec.Str("error") != "" || rec["is_error"] != false {
		t.Fatalf("status through tm mcp: %v", rec)
	}
	if !Poll(agentWait, func() bool {
		b, _ := os.ReadFile(filepath.Join(projDir, "threads", "t-0001", "STATUS.md"))
		return strings.Contains(string(b), "via tm mcp")
	}) {
		b, _ := os.ReadFile(filepath.Join(projDir, "threads", "t-0001", "STATUS.md"))
		t.Errorf("the call didn't set the thread's status:\n%s", b)
	}
	env.WaitState(th, "idle", agentWait)

	// Usage: the turns' tokens in the thread's totals, the context use
	// in its session, in the opus window.
	var show struct {
		Thread struct {
			Usage struct{ Turns, Input, Output int64 }
		}
	}
	if !Poll(agentWait, func() bool {
		json.Unmarshal([]byte(env.MustCLI("thread", "show", "t-0001", "--project", "demo", "--json")), &show)
		return show.Thread.Usage.Turns >= 2 && show.Thread.Usage.Output > 0
	}) {
		t.Fatalf("thread usage: %+v", show.Thread.Usage)
	}
	if u := show.Thread.Usage; u.Input == 0 || u.Output%50 != 0 {
		t.Errorf("thread usage: %+v", u)
	}
	context := func() (int64, int64) {
		i, _ := env.Info(th)
		return i.Context, i.ContextWindow
	}
	var used int64
	if !Poll(agentWait, func() bool { used, _ = context(); return used > 20000 }) {
		t.Fatalf("no context use: %d", used)
	}
	if _, w := context(); w != 1_000_000 {
		t.Errorf("context window %d, want the opus model's 1M", w)
	}

	// /clear forgets it; the next turn reports the new conversation's.
	env.Prompt(th, "/clear")
	env.WaitFake("slash", agentWait, func(r FakeRecord) bool { return r.Str("text") == "/clear" })
	if !Poll(agentWait, func() bool { n, _ := context(); return n == 0 }) {
		n, _ := context()
		t.Fatalf("context use after /clear: %d", n)
	}
	env.WaitState(th, "idle", agentWait)
	env.Prompt(th, "hello")
	var after int64
	if !Poll(agentWait, func() bool { after, _ = context(); return after > 0 }) {
		t.Fatal("no context use after /clear's next turn")
	}
	if after >= used {
		t.Errorf("context after /clear %d, before %d", after, used)
	}

	// /compact too.
	env.WaitState(th, "idle", agentWait)
	env.Prompt(th, "/compact")
	env.WaitFake("slash", agentWait, func(r FakeRecord) bool { return r.Str("text") == "/compact" })
	if !Poll(agentWait, func() bool { n, _ := context(); return n == 0 }) {
		n, _ := context()
		t.Errorf("context use after /compact: %d", n)
	}
}

// TestClaudeWithModNoMCP: with the mod on, the mod registers the typed
// tools and reports the usage itself, so the launch names no MCP server
// and the transcript's usage isn't counted (the fake runs no mod: its
// thread reports none).
func TestClaudeWithModNoMCP(t *testing.T) {
	env, _, _ := threadEnv(t)
	writeConfig(t, env, "[agent_mod]\nenabled = true\n")
	env.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)
	start := env.WaitFake("start", agentWait, nil)
	argv, _ := json.Marshal(start["argv"])
	plugin := pluginDir(t, start)
	if _, err := os.Stat(filepath.Join(plugin, "hooks", "register.ts")); err != nil {
		t.Fatalf("the mod isn't loaded (%v): %s", err, argv)
	}
	if cfgs, _ := start["mcp_config"].([]any); len(cfgs) != 0 {
		t.Fatalf("the mod and tm mcp both: %s", argv)
	}
	env.Prompt(th, "hello")
	env.WaitState(th, "idle", agentWait)
	time.Sleep(time.Second) // the tailer's ticks
	var show struct {
		Thread struct{ Usage struct{ Turns, Output int64 } }
	}
	json.Unmarshal([]byte(env.MustCLI("thread", "show", "t-0001", "--project", "demo", "--json")), &show)
	if u := show.Thread.Usage; u.Output != 0 || u.Turns != 0 {
		t.Errorf("usage counted from the transcript beside the mod: %+v", u)
	}
	if i, _ := env.Info(th); i.Context != 0 {
		t.Errorf("context from the transcript beside the mod: %d", i.Context)
	}
}

// TestCoordinatorSlashCommandsWithoutMod: a coordinator's /saveclear and
// /answer without the mod are the plugin's commands; the prompt never
// reaches the model: the server runs tm clear's and tm ask open's work
// and the hook blocks it with what came of it, the coordinator idle
// again. A thread gets neither command.
func TestCoordinatorSlashCommandsWithoutMod(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	coord := env.StartAgent("claude", projDir, "--role", "coordinator", "--project", "demo")
	env.WaitState(coord, "idle", agentWait)
	plugin := pluginDir(t, env.WaitFake("start", agentWait, nil))
	for _, name := range []string{"saveclear", "answer"} {
		if _, err := os.Stat(filepath.Join(plugin, "commands", name+".md")); err != nil {
			t.Errorf("no /%s: %v", name, err)
		}
	}

	blocked := func(prompt string) string {
		t.Helper()
		n := len(env.FakeRecords("blocked"))
		env.Prompt(coord, prompt)
		var r FakeRecord
		if !Poll(agentWait, func() bool {
			b := env.FakeRecords("blocked")
			if len(b) > n {
				r = b[n]
			}
			return r != nil
		}) {
			t.Fatalf("%s wasn't blocked; prompts %v", prompt, env.FakeRecords("prompt"))
		}
		env.WaitState(coord, "idle", agentWait)
		return r.Str("reason")
	}
	if got := blocked("/answer"); got != "Not asked: project demo has no open questions" {
		t.Errorf("/answer without questions: %q", got)
	}
	add := exec.Command(env.Bin, "ask", "add", "Which repo?", "--option", "Main", "--option", "Site", "--project", "demo")
	add.Env = append(env.Vars, "TERMINATR_SESSION="+coord.ID)
	if b, err := add.CombinedOutput(); err != nil {
		t.Fatalf("ask add: %v\n%s", err, b)
	}
	if got := blocked("/answer"); got != "Asked: your questions open in a dialog once the coordinator is free" {
		t.Errorf("/answer: %q", got)
	}
	env.WaitFake("prompt", agentWait, func(r FakeRecord) bool {
		return strings.HasPrefix(r.Str("text"), "[tm] answer questions:")
	})
	env.WaitState(coord, "idle", agentWait)
	if got := blocked("/saveclear"); got != "Asked: the coordinator saves what it learned, then its conversation is cleared once it is idle" {
		t.Errorf("/saveclear: %q", got)
	}
	env.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)
	starts := env.FakeRecords("start")
	plugin = pluginDir(t, starts[len(starts)-1])
	if _, err := os.Stat(filepath.Join(plugin, "commands")); err == nil {
		t.Errorf("a thread got the coordinator's commands")
	}
}

// pluginDir is the --plugin-dir of a fake's start record.
func pluginDir(t *testing.T, start FakeRecord) string {
	t.Helper()
	argv, _ := start["argv"].([]any)
	for i, a := range argv {
		if a == "--plugin-dir" && i+1 < len(argv) {
			p, _ := argv[i+1].(string)
			return p
		}
	}
	t.Fatalf("no --plugin-dir in %v", argv)
	return ""
}
