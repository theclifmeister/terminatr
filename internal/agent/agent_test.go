package agent

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/plat/ipc"
	"github.com/theclifmeister/terminatr/internal/theme"
)

func claude(t *testing.T) Agent {
	t.Helper()
	r, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := r.Get("claude")
	if !ok {
		t.Fatal("claude manifest not embedded")
	}
	return a
}

func threadSpec() LaunchSpec {
	return LaunchSpec{
		Role: RoleThread, SessionID: "s1", AgentSID: "uuid-1",
		Cwd: "/w", RuntimeDir: "/run/s1", BriefPath: "/h/.terminatr/projects/p/threads/t-0001/brief.md",
		Kickoff: "Run tm skill thread.", TMBin: "/bin/tm", Socket: "/run/tm.sock",
		Access: Access{Read: []string{"/h/.terminatr/projects/p"}, NoWrite: []string{"/h/.terminatr/projects/p"}},
	}
}

// TestHookEvents: the events of [[hooks]], [[todos]] and
// todos_snapshot.on, in that order, each once.
func TestHookEvents(t *testing.T) {
	m := &Manifest{
		Hooks:   []HookMap{{Event: "A"}, {Event: "B"}, {Event: "A"}},
		Todos:   []TodoMap{{Event: "C"}, {Event: "B"}},
		Sources: Sources{TodoSnapshot: &TodoSnapshot{On: []string{"D", "A"}}},
	}
	if got, want := m.HookEvents(), []string{"A", "B", "C", "D"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("HookEvents = %q, want %q", got, want)
	}
}

func TestClaudeLaunch(t *testing.T) {
	a := claude(t)
	spec := threadSpec()
	l, err := a.Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude",
		"--plugin-dir", "/run/s1/claude-plugin",
		"--settings", "/run/s1/claude-settings.json",
		"--mcp-config", "/run/s1/claude-mcp.json",
		"--session-id", "uuid-1",
		"--append-system-prompt-file", "/h/.terminatr/projects/p/threads/t-0001/brief.md",
		"--", "Run tm skill thread."}
	if !reflect.DeepEqual(l.Argv, want) {
		t.Fatalf("argv\n got %q\nwant %q", l.Argv, want)
	}
	// tm mcp is the typed tools' server (§8.6, tm mcp).
	var mcp struct {
		MCPServers map[string]struct {
			Type, Command string
			Args          []string
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(l.Files["claude-mcp.json"], &mcp); err != nil {
		t.Fatalf("claude-mcp.json is not JSON: %v\n%s", err, l.Files["claude-mcp.json"])
	}
	if s := mcp.MCPServers["terminatr"]; len(mcp.MCPServers) != 1 || s.Type != "stdio" || s.Command != "/bin/tm" || !slices.Equal(s.Args, []string{"mcp"}) {
		t.Fatalf("claude-mcp.json = %+v", mcp)
	}
	if !l.Kickoff {
		t.Fatal("Kickoff unset with the kickoff in argv")
	}
	if !contains(l.Env, "CLAUDE_CODE_ENABLE_TODO_TOOLS=1") {
		t.Fatalf("env = %q: want CLAUDE_CODE_ENABLE_TODO_TOOLS=1 so current models get the todo tools", l.Env)
	}
	spec.Resume = true
	if l, err := a.Launch(spec); err != nil || l.Kickoff {
		t.Fatalf("resume: Kickoff %v, err %v", l.Kickoff, err)
	}
	spec.Resume = false
	if !contains(l.Unset, "CLAUDECODE") || contains(l.Unset, "CLAUDE_CODE_*") {
		t.Fatalf("unset = %q: want the inherited session vars, not the whole CLAUDE_CODE_ prefix", l.Unset)
	}

	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type, Command string
				Args          []string
				Async         bool
				Timeout       int
			}
		}
	}
	if err := json.Unmarshal(l.Files["claude-plugin/hooks/hooks.json"], &hooks); err != nil {
		t.Fatalf("hooks.json is not JSON: %v\n%s", err, l.Files["claude-plugin/hooks/hooks.json"])
	}
	for _, ev := range []string{"SessionStart", "PermissionRequest", "SubagentStart", "StopFailure", "SessionEnd"} {
		h := hooks.Hooks[ev]
		if len(h) != 1 || h[0].Hooks[0].Command != "/bin/tm" || !slices.Equal(h[0].Hooks[0].Args, []string{"hook", "--agent", "claude"}) || h[0].Hooks[0].Async || h[0].Hooks[0].Timeout != 5 {
			t.Fatalf("hooks.json %s = %+v; want one sync command hook with timeout 5", ev, h)
		}
	}
	// Exactly the events the manifest maps (.HookEvents): none that
	// would spawn tm hook for nothing.
	events := []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
		"PreCompact", "PermissionRequest", "Notification", "Stop", "StopFailure", "SubagentStart", "SubagentStop", "SessionEnd"}
	if got := ManifestOf(a).HookEvents(); !reflect.DeepEqual(got, events) {
		t.Fatalf("HookEvents\n got %q\nwant %q", got, events)
	}
	if got := slices.Sorted(maps.Keys(hooks.Hooks)); !reflect.DeepEqual(got, slices.Sorted(slices.Values(events))) {
		t.Fatalf("hooks.json registers %q, want %q", got, events)
	}

	settings := parseSettings(t, l.Files["claude-settings.json"])
	if want := []string{"mcp__terminatr", "Read(//h/.terminatr/projects/p/**)"}; !reflect.DeepEqual(settings.Permissions.Allow, want) {
		t.Fatalf("allow = %q, want %q", settings.Permissions.Allow, want)
	}
	if want := []string{"Edit(//h/.terminatr/projects/p/**)"}; !reflect.DeepEqual(settings.Permissions.Deny, want) {
		t.Fatalf("deny = %q, want %q (Edit rules only)", settings.Permissions.Deny, want)
	}
	// A server "terminatr" of Claude's own config: its tools get no rule.
	clash := spec
	clash.MCPClash = true
	if l, err := a.Launch(clash); err != nil {
		t.Fatal(err)
	} else if s := parseSettings(t, l.Files["claude-settings.json"]); !reflect.DeepEqual(s.Permissions.Allow, []string{"Read(//h/.terminatr/projects/p/**)"}) ||
		!reflect.DeepEqual(s.Permissions.Ask, []string{"mcp__terminatr"}) {
		t.Fatalf("clash: allow = %q, ask = %q", s.Permissions.Allow, s.Permissions.Ask)
	}
	if len(settings.Permissions.Ask) != 0 {
		t.Fatalf("ask = %q without a clash", settings.Permissions.Ask)
	}
	// A worktree's git paths: directories and files as allow rules, the
	// git dir's configuration as deny rules (docs/SPEC.md §8.6, Access).
	git := spec
	git.Access.Write = []string{"/r/.git/objects", "/r/.git/worktrees/w"}
	git.Access.WriteFiles = []string{"/r/.git/packed-refs"}
	git.Access.NoWrite = append(slices.Clone(git.Access.NoWrite), "/r/.git/hooks")
	git.Access.NoWriteFiles = []string{"/r/.git/config"}
	if l, err := a.Launch(git); err != nil {
		t.Fatal(err)
	} else {
		s := parseSettings(t, l.Files["claude-settings.json"])
		if want := []string{"mcp__terminatr", "Read(//h/.terminatr/projects/p/**)", "Edit(//r/.git/objects/**)", "Edit(//r/.git/worktrees/w/**)", "Edit(//r/.git/packed-refs)"}; !reflect.DeepEqual(s.Permissions.Allow, want) {
			t.Fatalf("git: allow = %q, want %q", s.Permissions.Allow, want)
		}
		if want := []string{"Edit(//h/.terminatr/projects/p/**)", "Edit(//r/.git/hooks/**)", "Edit(//r/.git/config)"}; !reflect.DeepEqual(s.Permissions.Deny, want) {
			t.Fatalf("git: deny = %q, want %q", s.Permissions.Deny, want)
		}
	}
	if !settings.Sandbox.Enabled || !reflect.DeepEqual(settings.Sandbox.Network.AllowUnixSockets, []string{"/run/tm.sock"}) {
		t.Fatalf("thread sandbox = %+v", settings.Sandbox)
	}
	// sandbox_escapes on (the default): Claude's own default stands, so
	// the agent may ask to run a command outside the sandbox.
	if settings.Sandbox.AllowUnsandboxedCommands != nil {
		t.Fatalf("thread sandbox sets allowUnsandboxedCommands by default: %+v", settings.Sandbox)
	}
	// Off: a thread's Bash can't leave the sandbox, where it would reach
	// the human socket (docs/SPEC.md §5.2).
	closed := spec
	closed.NoSandboxEscapes = true
	l, err = a.Launch(closed)
	if err != nil {
		t.Fatal(err)
	}
	if s := parseSettings(t, l.Files["claude-settings.json"]); s.Sandbox.AllowUnsandboxedCommands == nil || *s.Sandbox.AllowUnsandboxedCommands || !s.Sandbox.Enabled {
		t.Fatalf("thread sandbox with sandbox_escapes off = %+v", s.Sandbox)
	}
	// The project's sandbox lists: excludedCommands, and allow rules
	// after tm's; none by default.
	if settings.Sandbox.ExcludedCommands != nil {
		t.Fatalf("thread sandbox has excludedCommands by default: %+v", settings.Sandbox)
	}
	listed := closed
	if settings.Sandbox.Network.AllowLocalBinding {
		t.Fatalf("thread sandbox allows local binding by default: %+v", settings.Sandbox)
	}
	if settings.Sandbox.Network.AllowedDomains != nil {
		t.Fatalf("thread sandbox allows domains by default: %+v", settings.Sandbox)
	}
	listed.Sandbox = SandboxLists{Excluded: []string{"npx playwright *"}, Allow: []string{"Bash(npx playwright *)"}, LocalBinding: true, Domains: []string{"registry.npmjs.org"}}
	if l, err = a.Launch(listed); err != nil {
		t.Fatal(err)
	}
	if s := parseSettings(t, l.Files["claude-settings.json"]); !reflect.DeepEqual(s.Sandbox.ExcludedCommands, []string{"npx playwright *"}) ||
		!reflect.DeepEqual(s.Permissions.Allow, []string{"mcp__terminatr", "Read(//h/.terminatr/projects/p/**)", "Bash(npx playwright *)"}) ||
		s.Sandbox.AllowUnsandboxedCommands == nil || *s.Sandbox.AllowUnsandboxedCommands || !s.Sandbox.Network.AllowLocalBinding ||
		!reflect.DeepEqual(s.Sandbox.Network.AllowedDomains, []string{"registry.npmjs.org"}) ||
		!reflect.DeepEqual(s.Sandbox.Network.AllowUnixSockets, settings.Sandbox.Network.AllowUnixSockets) {
		t.Fatalf("thread with sandbox lists: %+v", s)
	}
	// Yolo (T574 case 4): no sandbox and no prompts. The sandbox is off,
	// set to false so that a project's or the user's own true doesn't
	// turn it back on, and its lists aren't written; tm's deny rules
	// still are.
	yolo := listed
	yolo.Yolo = true
	if l, err = a.Launch(yolo); err != nil {
		t.Fatal(err)
	}
	if !contains(l.Argv, "--dangerously-skip-permissions") {
		t.Fatalf("yolo argv %q: want --dangerously-skip-permissions", l.Argv)
	}
	var raw struct {
		Sandbox map[string]json.RawMessage `json:"sandbox"`
	}
	if err := json.Unmarshal(l.Files["claude-settings.json"], &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw.Sandbox["enabled"]) != "false" {
		t.Fatalf("yolo sandbox.enabled = %s, want false\n%s", raw.Sandbox["enabled"], l.Files["claude-settings.json"])
	}
	if s := parseSettings(t, l.Files["claude-settings.json"]); s.Sandbox.ExcludedCommands != nil || s.Sandbox.AllowUnsandboxedCommands != nil ||
		s.Sandbox.Network.AllowLocalBinding || s.Sandbox.Network.AllowedDomains != nil ||
		!reflect.DeepEqual(s.Permissions.Deny, settings.Permissions.Deny) {
		t.Fatalf("yolo thread settings: %+v", s)
	}
	// Protection below sandboxed: the sandbox off as under yolo, but
	// Claude's own prompts kept.
	open := listed
	open.NoSandbox = true
	if l, err = a.Launch(open); err != nil {
		t.Fatal(err)
	}
	if contains(l.Argv, "--dangerously-skip-permissions") {
		t.Fatalf("no-sandbox argv %q skips the prompts", l.Argv)
	}
	raw.Sandbox = nil
	if err := json.Unmarshal(l.Files["claude-settings.json"], &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw.Sandbox["enabled"]) != "false" {
		t.Fatalf("no-sandbox sandbox.enabled = %s, want false\n%s", raw.Sandbox["enabled"], l.Files["claude-settings.json"])
	}
	if s := parseSettings(t, l.Files["claude-settings.json"]); s.Sandbox.ExcludedCommands != nil || s.Sandbox.Network.AllowedDomains != nil ||
		!reflect.DeepEqual(s.Permissions.Deny, settings.Permissions.Deny) {
		t.Fatalf("no-sandbox thread settings: %+v", s)
	}
	if settings.StatusLine != nil {
		t.Fatal("settings must never set statusLine; it would replace the user's")
	}
	// No pane palette: Claude's theme is the user's own.
	if settings.Theme != nil {
		t.Fatalf("theme %q without a pane palette", *settings.Theme)
	}
	// A theme's pane palette colours the 16 ANSI colours: Claude's ANSI
	// theme for the scheme, when tm's own theme file isn't written (T342).
	for _, scheme := range []string{"dark", "light"} {
		pal := spec
		pal.PaneScheme = scheme
		if l, err = a.Launch(pal); err != nil {
			t.Fatal(err)
		}
		if s := parseSettings(t, l.Files["claude-settings.json"]); s.Theme == nil || *s.Theme != scheme+"-ansi" || !s.Sandbox.Enabled {
			t.Fatalf("pane scheme %s: theme %v, settings %+v", scheme, s.Theme, s)
		}
	}
	// With it, whatever the palette: tm's theme in Claude's config folder,
	// which follows the tm theme live (T345).
	for _, scheme := range []string{"", "dark"} {
		live := spec
		live.PaneScheme, live.LiveTheme = scheme, true
		if l, err = a.Launch(live); err != nil {
			t.Fatal(err)
		}
		if s := parseSettings(t, l.Files["claude-settings.json"]); s.Theme == nil || *s.Theme != "custom:terminatr" {
			t.Fatalf("live theme, pane scheme %q: settings select %v", scheme, s.Theme)
		}
	}
	for path := range l.Files {
		if strings.Contains(path, "themes/") {
			t.Fatalf("launch writes %s: the theme lives in Claude's config folder", path)
		}
	}
	m := ManifestOf(a)
	if m.Theme.Path != "themes/terminatr.json" {
		t.Fatalf("theme path %q: the settings select custom:terminatr", m.Theme.Path)
	}
	type themeData struct {
		Theme     *theme.Agent
		Base      string
		Overrides map[string]json.RawMessage
	}
	var ct struct {
		Name, Base string
		Overrides  map[string]string
	}
	// Without a pane palette: the user's own theme, as the server gives it.
	out, err := m.RenderTheme(themeData{Base: "light-daltonized", Overrides: map[string]json.RawMessage{"claude": json.RawMessage(`"#123456"`)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &ct); err != nil || ct.Name != "terminatr" || ct.Base != "light-daltonized" || len(ct.Overrides) != 1 || ct.Overrides["claude"] != "#123456" {
		t.Fatalf("user's own theme: %v %+v\n%s", err, ct, out)
	}
	// With the theme's colours: over Claude's ANSI theme, the roles and
	// the pane's colours in truecolor, none it lacks.
	th := &theme.Agent{Scheme: "light", PaneFg: "#2e3440", PaneBg: "#eceff4",
		Fg: map[string]string{"head": "", "sel": "", "sel_focus": "", "faint": "#4c566a", "accent": "#5e81ac", "title": "",
			"bad": "#bf616a", "warn": "", "good": "#a3be8c", "info": "#81a1c1", "plain": ""},
		Bg: map[string]string{"sel": "#d8dee9"}}
	if out, err = m.RenderTheme(themeData{Theme: th}); err != nil {
		t.Fatal(err)
	}
	ct.Overrides = nil
	if err := json.Unmarshal([]byte(out), &ct); err != nil {
		t.Fatalf("theme: %v\n%s", err, out)
	}
	if ct.Base != "light-ansi" || ct.Name != "terminatr" {
		t.Fatalf("theme %+v", ct)
	}
	for tok, want := range map[string]string{
		"claude": "#5e81ac", "error": "#bf616a", "success": "#a3be8c", "suggestion": "#81a1c1",
		"inactive": "#4c566a", "text": "#2e3440", "inverseText": "#eceff4", "selectionBg": "#e3e7ef",
		"userMessageBackground": "#dde0e6", "diffAdded": "#d2ded0",
	} {
		if got := ct.Overrides[tok]; got != want {
			t.Errorf("%s = %q, want %q", tok, got, want)
		}
	}
	for _, tok := range []string{"warning", "warningShimmer", "bashMessageBackgroundColor"} {
		if v, ok := ct.Overrides[tok]; ok {
			t.Errorf("%s = %q, but the theme gives no colour for it", tok, v)
		}
	}

	coord := spec
	coord.Role, coord.Access = Role("coordinator"), Access{Read: []string{"/h/.terminatr/worktrees/p"}}
	coord.NoSandboxEscapes = true                                                                                                                          // a coordinator's Bash has no sandbox to keep
	coord.Sandbox = SandboxLists{Excluded: []string{"npx *"}, Allow: []string{"Bash(npx *)"}, LocalBinding: true, Domains: []string{"registry.npmjs.org"}} // threads' only
	l, err = a.Launch(coord)
	if err != nil {
		t.Fatal(err)
	}
	settings = parseSettings(t, l.Files["claude-settings.json"])
	if settings.Sandbox.Enabled || settings.Sandbox.AllowUnsandboxedCommands != nil || settings.Sandbox.ExcludedCommands != nil || settings.Sandbox.Network.AllowLocalBinding || settings.Sandbox.Network.AllowedDomains != nil || len(settings.Permissions.Deny) != 0 ||
		!reflect.DeepEqual(settings.Permissions.Allow, []string{"mcp__terminatr", "Read(//h/.terminatr/worktrees/p/**)"}) {
		t.Fatalf("coordinator must not get the thread sandbox or deny rules: %+v", settings)
	}

	coord.Access.Commands = []string{"gh pr merge", "az repos pr update"}
	if l, err = a.Launch(coord); err != nil {
		t.Fatal(err)
	}
	settings = parseSettings(t, l.Files["claude-settings.json"])
	if want := []string{"mcp__terminatr", "Read(//h/.terminatr/worktrees/p/**)", "Bash(gh pr merge:*)", "Bash(az repos pr update:*)"}; !reflect.DeepEqual(settings.Permissions.Allow, want) {
		t.Fatalf("allow = %q, want %q", settings.Permissions.Allow, want)
	}

	spec.Resume, spec.Yolo = true, true
	l, err = a.Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"claude",
		"--plugin-dir", "/run/s1/claude-plugin",
		"--settings", "/run/s1/claude-settings.json",
		"--mcp-config", "/run/s1/claude-mcp.json",
		"--append-system-prompt-file", "/h/.terminatr/projects/p/threads/t-0001/brief.md",
		"--resume", "uuid-1", "--dangerously-skip-permissions"}
	if !reflect.DeepEqual(l.Argv, want) {
		t.Fatalf("resume argv\n got %q\nwant %q", l.Argv, want)
	}

	spec.AgentSID = ""
	if _, err := a.Launch(spec); err == nil {
		t.Fatal("resume without a session id must fail; claude --resume \"\" opens a picker")
	}
}

type claudeSettings struct {
	Permissions struct{ Allow, Ask, Deny []string }
	Sandbox     struct {
		Enabled                  bool
		AllowUnsandboxedCommands *bool
		ExcludedCommands         []string
		Filesystem               *struct{ DenyRead, AllowRead []string }
		Network                  struct {
			AllowUnixSockets  []string
			AllowLocalBinding bool
			AllowedDomains    []string
		}
	}
	StatusLine any     `json:"statusLine"`
	Theme      *string `json:"theme"`
}

func parseSettings(t *testing.T, b []byte) claudeSettings {
	t.Helper()
	var s claudeSettings
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("claude-settings.json is not JSON: %v\n%s", err, b)
	}
	return s
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// states returns the state signals of an event as "state/reason" strings.
func states(t *testing.T, a Agent, event string, payload map[string]any) []string {
	t.Helper()
	sigs, _, err := a.Hook(HookEvent{Event: event, Payload: payload}, HookEnv{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range sigs {
		if s.State != "" {
			out = append(out, string(s.State)+"/"+s.Reason)
		}
	}
	return out
}

func TestClaudeHookStates(t *testing.T) {
	a := claude(t)
	cases := []struct {
		event   string
		payload map[string]any
		want    []string
	}{
		{"UserPromptSubmit", nil, []string{"working/"}},
		{"PermissionRequest", map[string]any{"tool_name": "Bash"}, []string{"blocked/permission"}},
		{"PermissionRequest", map[string]any{"tool_name": "AskUserQuestion"}, []string{"blocked/question"}},
		{"Notification", map[string]any{"notification_type": "idle_prompt"}, []string{"idle/"}},
		{"Stop", nil, []string{"idle/"}},
		{"Stop", map[string]any{"agent_id": "sub-1"}, nil},
		{"SessionEnd", map[string]any{"reason": "clear"}, nil},
		{"SessionEnd", map[string]any{"reason": "prompt_input_exit"}, []string{"exited/"}},
		{"SessionEnd", map[string]any{"reason": "other"}, []string{"exited/"}},
	}
	for _, c := range cases {
		if got := states(t, a, c.event, c.payload); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %v -> %q, want %q", c.event, c.payload, got, c.want)
		}
	}
}

func TestClaudeSessionIDAndCounters(t *testing.T) {
	a := claude(t)
	sigs, _, _ := a.Hook(HookEvent{Event: "UserPromptSubmit", Payload: map[string]any{"session_id": "new-after-clear"}}, HookEnv{})
	if len(sigs) == 0 || sigs[0].AgentSID != "new-after-clear" {
		t.Fatalf("session id must be read from every event, got %+v", sigs)
	}

	counter := func(event string, payload map[string]any) string {
		sigs, _, _ := a.Hook(HookEvent{Event: event, Payload: payload}, HookEnv{})
		for _, s := range sigs {
			if s.Counter != "" {
				return s.Counter + ":" + s.CounterKey
			}
		}
		return ""
	}
	if got := counter("SubagentStart", map[string]any{"agent_id": "a1", "agent_type": "general-purpose"}); got != "+bg:a1" {
		t.Fatalf("SubagentStart -> %q", got)
	}
	if got := counter("SubagentStop", map[string]any{"agent_id": "a1", "agent_type": "general-purpose"}); got != "-bg:a1" {
		t.Fatalf("SubagentStop -> %q", got)
	}
	if got := counter("SubagentStop", map[string]any{"agent_id": "suggest"}); got != "" {
		t.Fatalf("the prompt-suggestion side agent (no agent_type) must be ignored, got %q", got)
	}
}

func TestClaudeContextResponse(t *testing.T) {
	a := claude(t)
	_, res, err := a.Hook(HookEvent{Event: "SessionStart", Payload: map[string]any{"session_id": "x", "source": "clear"}},
		HookEnv{Context: func() ([]byte, error) { return []byte("# Context\n\"quoted\""), nil }})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		HookSpecificOutput struct{ AdditionalContext string } `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		t.Fatalf("SessionStart response is not JSON: %v\n%s", err, res.Stdout)
	}
	if out.HookSpecificOutput.AdditionalContext != "# Context\n\"quoted\"" {
		t.Fatalf("additionalContext = %q", out.HookSpecificOutput.AdditionalContext)
	}
}

// TestClaudeGuardResponse: PreToolUse answers the
// guard's refusal as a deny, judged once; an allowed call gets no output.
func TestClaudeGuardResponse(t *testing.T) {
	a := claude(t)
	var calls []string
	env := HookEnv{Guard: func(tool string, input map[string]any) *guard.Denial {
		calls = append(calls, tool+" "+input["command"].(string))
		if input["command"] == "gh pr merge 1" {
			return &guard.Denial{Rule: "merge", Message: `terminatr guard (merge): "no"`, Summary: "gh pr merge"}
		}
		return nil
	}}
	call := func(cmd string) []byte {
		_, res, err := a.Hook(HookEvent{Event: "PreToolUse", Payload: map[string]any{
			"session_id": "x", "tool_name": "Bash", "tool_input": map[string]any{"command": cmd}}}, env)
		if err != nil {
			t.Fatal(err)
		}
		return res.Stdout
	}
	var out struct {
		HookSpecificOutput struct {
			HookEventName, PermissionDecision, PermissionDecisionReason string
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(call("gh pr merge 1"), &out); err != nil {
		t.Fatal(err)
	}
	if h := out.HookSpecificOutput; h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || h.PermissionDecisionReason != `terminatr guard (merge): "no"` {
		t.Errorf("deny = %+v", h)
	}
	if got := call("gh pr view 1"); len(got) != 0 {
		t.Errorf("allowed call answered %q", got)
	}
	if want := []string{"Bash gh pr merge 1", "Bash gh pr view 1"}; !slices.Equal(calls, want) {
		t.Errorf("judged %q, want %q", calls, want)
	}
	// Without a guard (the mod judges), nothing is answered.
	_, res, _ := a.Hook(HookEvent{Event: "PreToolUse", Payload: map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr merge 1"}}}, HookEnv{})
	if len(res.Stdout) != 0 {
		t.Errorf("no guard answered %q", res.Stdout)
	}
}

// TestGuardFields: .Guard reads the tool call from the [hook]
// tool_field and input_field, tool_name and tool_input by default.
func TestGuardFields(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n" +
		"[[hooks]]\nevent = \"before\"\nrespond = '{{with .Guard}}{{.Rule}}{{end}}'\n"
	for _, c := range []struct {
		hook    string
		payload map[string]any
	}{
		{"", map[string]any{"tool_name": "bash", "tool_input": map[string]any{"command": "x"}}},
		{"[hook]\ntool_field = \"tool\"\ninput_field = \"args\"\n", map[string]any{"tool": "bash", "args": map[string]any{"command": "x"}}},
	} {
		m, err := ParseManifest([]byte(base + c.hook))
		if err != nil {
			t.Fatal(err)
		}
		var judged string
		env := HookEnv{Guard: func(tool string, input map[string]any) *guard.Denial {
			judged = tool + " " + input["command"].(string)
			return &guard.Denial{Rule: "merge"}
		}}
		_, res, err := FromManifest(m).Hook(HookEvent{Event: "before", Payload: c.payload}, env)
		if err != nil {
			t.Fatal(err)
		}
		if judged != "bash x" || string(res.Stdout) != "merge" {
			t.Errorf("%q: judged %q, answered %q", c.hook, judged, res.Stdout)
		}
	}
}

// todo applies one hook event to list and returns the result.
func todo(t *testing.T, a Agent, list []Todo, event string, payload map[string]any) []Todo {
	t.Helper()
	sigs, _, err := a.Hook(HookEvent{Event: event, Payload: payload}, HookEnv{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sigs {
		if s.Todo != nil {
			return ApplyTodo(list, *s.Todo)
		}
	}
	return list
}

func TestClaudeTodos(t *testing.T) {
	a := claude(t)
	create := func(id, subject string) map[string]any {
		return map[string]any{
			"tool_name":     "TaskCreate",
			"tool_input":    map[string]any{"subject": subject, "description": "…", "activeForm": subject + "ing"},
			"tool_response": map[string]any{"task": map[string]any{"id": id, "subject": subject}},
		}
	}
	update := func(id, status string) map[string]any {
		return map[string]any{
			"tool_name":  "TaskUpdate",
			"tool_input": map[string]any{"taskId": id, "status": status},
		}
	}

	var list []Todo
	list = todo(t, a, list, "PostToolUse", create("1", "Test"))
	list = todo(t, a, list, "PostToolUse", create("2", "Fix"))
	list = todo(t, a, list, "PostToolUse", update("1", "completed"))
	list = todo(t, a, list, "PostToolUse", update("2", "in_progress"))
	want := []Todo{
		{ID: "1", Text: "Test", ActiveText: "Testing", Status: TodoCompleted},
		{ID: "2", Text: "Fix", ActiveText: "Fixing", Status: TodoInProgress},
	}
	if !reflect.DeepEqual(list, want) {
		t.Fatalf("after diffs:\n got %+v\nwant %+v", list, want)
	}

	list = todo(t, a, list, "PostToolUse", update("1", "deleted"))
	if len(list) != 1 || list[0].ID != "2" {
		t.Fatalf("deleted must remove the item, got %+v", list)
	}

	list = todo(t, a, list, "SessionStart", map[string]any{"source": "compact"})
	if len(list) != 1 {
		t.Fatalf("compaction keeps the list, got %+v", list)
	}
	list = todo(t, a, list, "SessionStart", map[string]any{"source": "clear"})
	if list == nil || len(list) != 0 {
		t.Fatalf("/clear starts a new list, got %+v", list)
	}

	list = todo(t, a, list, "PostToolUse", map[string]any{
		"tool_name": "TodoWrite",
		"tool_input": map[string]any{"todos": []any{
			map[string]any{"content": "A", "status": "completed", "activeForm": "Doing A"},
			map[string]any{"content": "B", "status": "pending", "activeForm": "Doing B"},
		}},
	})
	if len(list) != 2 || list[1].Text != "B" || list[0].Status != TodoCompleted {
		t.Fatalf("TodoWrite replaces the list, got %+v", list)
	}
}

func TestStatusFile(t *testing.T) {
	src := claude(t).Sources()
	f := src.StatusFile
	path, err := f.PathFor(SourceVars{Home: "/h", PID: 4242, ConfigDir: "/h/.claude"})
	if err != nil || path != "/h/.claude/sessions/4242.json" {
		t.Fatalf("path = %q, %v", path, err)
	}

	r, err := f.Read([]byte(`{"pid":4242,"sessionId":"s9","status":"waiting","waitingFor":"permission prompt",
		"version":"2.1.289","messagingSocketPath":"/tmp/cc-socks/x.sock"}`), src)
	if err != nil {
		t.Fatal(err)
	}
	if r.Signal.State != StateBlocked || r.Signal.Reason != "permission" || r.Signal.AgentSID != "s9" ||
		r.Fields["messaging_socket"] != "/tmp/cc-socks/x.sock" {
		t.Fatalf("reading = %+v", r)
	}

	// Remote Control: bridgeSessionId is null while off (as observed on
	// 2.1.289), a session id while on.
	if !ManifestOf(claude(t)).ObservesRemote() {
		t.Fatal("claude's status file should say whether remote control is on")
	}
	rc := RemoteControlOf(claude(t)).StatusField
	for body, on := range map[string]bool{
		`{"status":"idle","version":"2.1.289","bridgeSessionId":null}`:         false,
		`{"status":"idle","version":"2.1.289"}`:                                false,
		`{"status":"idle","version":"2.1.289","bridgeSessionId":"session_01"}`: true,
	} {
		r, err := f.Read([]byte(body), src)
		if err != nil || (r.Fields[rc] != "") != on {
			t.Fatalf("%s: fields %v, %v; want remote control %v", body, r.Fields, err, on)
		}
	}

	// "shell": idle at the prompt with a background shell running (seen
	// on the terminatr coordinator, T43). Unknown, it distrusted the
	// whole file, the socket and remote control with it.
	r, err = f.Read([]byte(`{"status":"shell","version":"2.1.289","messagingSocketPath":"/tmp/x.sock","bridgeSessionId":"session_01"}`), src)
	if err != nil || r.Signal.State != StateIdle || r.Fields[rc] == "" {
		t.Fatalf("shell: %+v %v", r, err)
	}

	// No version gate (docs/SPEC.md §8.8): a newer Claude's file is read
	// as long as what it says is known.
	if r, err := f.Read([]byte(`{"status":"idle","version":"3.0.0"}`), src); err != nil || r.Signal.State != StateIdle {
		t.Fatalf("a newer version must be read: %+v %v", r, err)
	}
	// A manifest that pins tested_versions still refuses the others.
	pinned := *src
	pinned.TestedVersions = []string{"2.1."}
	if _, err := f.Read([]byte(`{"status":"idle","version":"3.0.0"}`), &pinned); !errors.Is(err, ErrUntestedVersion) {
		t.Fatalf("tested_versions must still refuse an untested version, got %v", err)
	}
	if _, err := f.Read([]byte(`{"status":"thinking","version":"2.1.300"}`), src); err == nil {
		t.Fatal("an unknown status must be refused")
	}
}

func TestJSONLTail(t *testing.T) {
	tail := claude(t).Sources().JSONLTail
	cases := []struct {
		line string
		want State
		ok   bool
	}{
		{`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`, StateIdle, true},
		{`{"type":"user","message":{"role":"user","content":"[Request interrupted by user]"}}`, StateIdle, true},
		{`{"type":"system","subtype":"turn_duration","durationMs":1200}`, StateIdle, true},
		{`{"type":"user","message":{"role":"user","content":"hello"}}`, "", false},
		{`not json`, "", false},
	}
	for _, c := range cases {
		sig, ok := tail.Line([]byte(c.line))
		if ok != c.ok || sig.State != c.want {
			t.Errorf("%s -> %v %v, want %v %v", c.line, sig.State, ok, c.want, c.ok)
		}
	}
}

func TestTodoSnapshot(t *testing.T) {
	snap := claude(t).Sources().TodoSnapshot
	dir, _ := snap.DirFor(SourceVars{Home: "/h", AgentSID: "s9", ConfigDir: "/h/.claude"})
	if dir != "/h/.claude/tasks/s9" {
		t.Fatalf("dir = %q", dir)
	}
	got := snap.Parse(map[string][]byte{
		"10.json":        []byte(`{"id":"10","subject":"ten","status":"pending"}`),
		"2.json":         []byte(`{"id":"2","subject":"two","status":"in_progress"}`),
		".highwatermark": []byte(`10`),
		"bad.json":       []byte(`{`),
	})
	want := []Todo{{ID: "2", Text: "two", Status: TodoInProgress}, {ID: "10", Text: "ten", Status: TodoPending}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
}

func TestHookTrim(t *testing.T) {
	trim := claude(t).Sources().Hook
	long := strings.Repeat("é", 400) // 800 bytes
	got := trim.Trim(map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "ls"}, "tool_response": "big", "prompt": long,
	})
	if _, ok := got["tool_input"]; ok {
		t.Fatal("tool_input must be dropped for ordinary tools")
	}
	if p := got["prompt"].(string); len(p) > 500 || !strings.HasPrefix(long, p) {
		t.Fatalf("prompt must be cut to <=500 bytes on a rune boundary, got %d bytes", len(p))
	}
	got = trim.Trim(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "TaskUpdate", "tool_input": map[string]any{"taskId": "1"}})
	if _, ok := got["tool_input"]; !ok {
		t.Fatal("tool_input must be kept for TaskUpdate")
	}
}

// [hook] event_field names the event field, which trimming always keeps,
// and [[hooks]] timeout is checked at load.
func TestHookWireFormat(t *testing.T) {
	if f := claude(t).Sources().Hook.EventField(); f != "hook_event_name" {
		t.Fatalf("default event field %q", f)
	}
	trim := HookTrim{Keep: []string{"a"}, Event: "type"}
	if got := trim.Trim(map[string]any{"type": "start", "a": 1, "b": 2}); got["type"] != "start" || got["b"] != nil {
		t.Fatalf("trim with event_field = %v", got)
	}
	for _, name := range []string{"claude", "codex"} {
		data, _ := builtin.ReadFile("manifests/" + name + ".toml")
		m, err := ParseManifest(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range m.Hooks {
			if h.Respond == "" || h.Command != "" {
				continue // tm hook waits only on the entries that answer; a command answers at once
			}
			want := ""
			switch h.Event {
			case "SessionStart":
				want = HookTimeoutContext
			case "PreToolUse":
				want = HookTimeoutGuard // the guard's verdict, failing closed
			}
			if h.Timeout != want {
				t.Errorf("%s %s: timeout %q, want %q", name, h.Event, h.Timeout, want)
			}
		}
	}
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[[hooks]]\nevent = \"E\"\n"
	for in, ok := range map[string]bool{
		"respond = \"x\"\ntimeout = \"context\"\n":  true,
		"respond = \"x\"\ntimeout = \"response\"\n": true,
		"respond = \"x\"\ntimeout = \"3s\"\n":       false,
		"timeout = \"context\"\n":                   false,
	} {
		if _, err := ParseManifest([]byte(base + in)); (err == nil) != ok {
			t.Errorf("%q: err %v", in, err)
		}
	}
}

func TestFilterEnv(t *testing.T) {
	env := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=x", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_MESSAGING_TOKEN=t"}
	l, err := claude(t).Launch(threadSpec())
	if err != nil {
		t.Fatal(err)
	}
	got := FilterEnv(env, l.Unset)
	want := []string{"PATH=/bin", "CLAUDE_CODE_USE_BEDROCK=1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterEnv = %q, want %q", got, want)
	}
}

func TestMatchPatterns(t *testing.T) {
	p := map[string]any{"a": "x", "n": map[string]any{"id": float64(3)}, "e": ""}
	cases := []struct {
		want map[string]string
		ok   bool
	}{
		{map[string]string{"a": "x"}, true},
		{map[string]string{"a": "!x"}, false},
		{map[string]string{"a": "!y"}, true},
		{map[string]string{"a": "*"}, true},
		{map[string]string{"e": "*"}, false},
		{map[string]string{"missing": "!*"}, true},
		{map[string]string{"n.id": "3"}, true},
	}
	for _, c := range cases {
		if got := matches(c.want, p); got != c.ok {
			t.Errorf("matches(%v) = %v, want %v", c.want, got, c.ok)
		}
	}
}

func TestUserManifest(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("myagent.toml", `manifest_version = 1
name = "myagent"
[identify]
argv0 = ["myagent"]
[launch]
command = "myagent"
kickoff_args = ["{{.Kickoff}}"]
[[hooks]]
event = "busy"
state = "working"
`)
	write("wrong.toml", "manifest_version = 1\nname = \"other\"\n[launch]\ncommand = \"x\"\n")
	write("badrule.toml", "manifest_version = 1\nname = \"badrule\"\n[launch]\ncommand = \"x\"\n[[rules]]\nid = \"r\"\nstate = \"idle\"\nregex = \"(\"\n")

	r, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "wrong.toml") || !strings.Contains(err.Error(), "badrule.toml") {
		t.Fatalf("want errors for wrong.toml and badrule.toml, got %v", err)
	}
	a, ok := r.Get("myagent")
	if !ok {
		t.Fatal("user manifest not loaded")
	}
	if got, _ := r.Identify(ProcessInfo{Argv: []string{"/usr/local/bin/myagent"}}); got != a {
		t.Fatal("Identify failed")
	}
	if _, ok := r.Get("claude"); !ok {
		t.Fatal("built-ins must still load next to user manifests")
	}
}

func TestManifestScreenResize(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n"
	for _, c := range []struct {
		screen string
		follow bool
		ok     bool
	}{
		{"", true, true},
		{"[screen]\nresize = \"follow\"\n", true, true},
		{"[screen]\nresize = \"explicit\"\n", false, true},
		{"[screen]\nresize = \"never\"\n", false, false},
	} {
		m, err := ParseManifest([]byte(base + c.screen))
		if (err == nil) != c.ok {
			t.Errorf("%q: err = %v, want ok %v", c.screen, err, c.ok)
			continue
		}
		if err == nil && FollowsTyping(FromManifest(m)) != c.follow {
			t.Errorf("%q: FollowsTyping = %v", c.screen, !c.follow)
		}
	}
	if !FollowsTyping(nil) {
		t.Error("a pane without an agent must follow typing")
	}
}

func TestBuiltinUnsetEnv(t *testing.T) {
	got := FilterEnv([]string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=x", "CLAUDE_CODE_USE_BEDROCK=1", "PATH=/bin"}, BuiltinUnsetEnv())
	if len(got) != 2 || got[0] != "CLAUDE_CODE_USE_BEDROCK=1" || got[1] != "PATH=/bin" {
		t.Fatalf("FilterEnv with the built-in unset list = %q", got)
	}
}

func TestTodoSnapshotHeal(t *testing.T) {
	snap := &TodoSnapshot{}
	mirror := []Todo{{ID: "1", Text: "a", ActiveText: "Doing a", Status: TodoInProgress}, {ID: "2", Text: "b", Status: TodoPending}}
	got := snap.Heal(mirror, []Todo{{ID: "1", Text: "a", Status: TodoCompleted}, {ID: "3", Text: "c", Status: TodoPending}})
	want := []Todo{{ID: "1", Text: "a", ActiveText: "Doing a", Status: TodoCompleted}, {ID: "3", Text: "c", Status: TodoPending}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Heal = %+v", got)
	}
}

func TestManifestRemoteControl(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\nkickoff_args = [\"--\", \"{{.Kickoff}}\"]\n"
	for _, c := range []struct {
		rc        string
		supported bool
		ok        bool
	}{
		{"", false, true},
		{"[remote_control]\nargs = [\"--rc\", \"{{.RemoteName}}\"]\n", true, true},
		{"[remote_control]\nargs = [\"--rc\"]\nenable = \"/rc {{.RemoteName}}\"\ndisable = \"/rc off\"\n", true, true},
		{"[remote_control]\nenable = \"/rc\"\n", false, false},
		{"[remote_control]\nargs = [\"--rc\"]\nname = \"x\"\n", false, false},
		{"[remote_control]\nargs = [\"--rc\"]\ndisable = \"/rc\"\ndisable_dialog = { contains = \"Disconnect\", keys = \"\\r\", done = \"Disconnected\" }\n", true, true},
		{"[remote_control]\nargs = [\"--rc\"]\ndisable_dialog = { contains = \"Disconnect\", keys = \"\\r\", done = \"x\" }\n", false, false},
		{"[remote_control]\nargs = [\"--rc\"]\ndisable = \"/rc\"\ndisable_dialog = { contains = \"Disconnect\" }\n", false, false},
		{"[remote_control]\nargs = [\"--rc\"]\nstatus_field = \"rc\"\n", true, true}, // no status file: unobserved
	} {
		m, err := ParseManifest([]byte(base + c.rc))
		if (err == nil) != c.ok {
			t.Errorf("%q: err = %v, want ok %v", c.rc, err, c.ok)
			continue
		}
		if err == nil && (RemoteControlOf(FromManifest(m)) != nil) != c.supported {
			t.Errorf("%q: supported = %v", c.rc, !c.supported)
		}
	}

	m, err := ParseManifest([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromManifest(m).Launch(LaunchSpec{RemoteControl: true}); err == nil || !strings.Contains(err.Error(), "no remote control") {
		t.Fatalf("remote control on an agent without it: err = %v", err)
	}
}

func TestClaudeRemoteControl(t *testing.T) {
	a := claude(t)
	spec := threadSpec()
	spec.Role, spec.RemoteControl, spec.RemoteName = Role("coordinator"), true, "demo"
	l, err := a.Launch(spec)
	if err != nil {
		t.Fatal(err)
	}
	// The flag takes an optional value, so the name follows it and the
	// kickoff still comes last, after "--".
	n := len(l.Argv)
	if got := l.Argv[n-4:]; !reflect.DeepEqual(got, []string{"--remote-control", "demo", "--", "Run tm skill thread."}) {
		t.Fatalf("argv tail = %q", got)
	}
	spec.Resume = true
	if l, err = a.Launch(spec); err != nil || !contains(l.Argv, "--remote-control") || !contains(l.Argv, "--resume") {
		t.Fatalf("resumed with remote control: %q %v", l.Argv, err)
	}
	rc := RemoteControlOf(a)
	if rc == nil {
		t.Fatal("claude has remote control")
	}
	if s, err := rc.Text(true, spec); err != nil || s != "/remote-control demo" {
		t.Fatalf("enable text = %q %v", s, err)
	}
	// Off is the same command, which opens a menu: a resumed
	// conversation would reconnect, so a restart can't turn it off.
	if s, err := rc.Text(false, spec); err != nil || s != "/remote-control" {
		t.Fatalf("disable text = %q %v", s, err)
	}
	if d := rc.DisableDialog; d == nil || d.Keys != "\x1b[A\x1b[A\r" || d.Contains == "" || d.Done == "" {
		t.Fatalf("disable dialog = %+v", d)
	}
}

func TestManifestAnswer(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n" +
		"[[rules]]\nid = \"q\"\nstate = \"blocked\"\nreason = \"question\"\nregion = \"screen\"\ncontains = [\"Enter to select\"]\n" +
		"[[rules]]\nid = \"idle\"\nstate = \"idle\"\nregion = \"screen\"\ncontains = [\"❯\"]\n"
	for _, c := range []struct {
		answer string
		has    bool
		ok     bool
	}{
		{"", false, true},
		{"[answer]\nrule = \"q\"\n", true, true},
		{"[answer]\nrule = \"q\"\ntext_option = \"Type something\"\nsubmit = \"\\r\"\n", true, true},
		{"[answer]\nrule = \"q\"\ntext_option = \"Type something\"\n", false, false},
		{"[answer]\nrule = \"q\"\ntext_option = \"Type something\"\ntext_key = \"\\t\"\nsubmit = \"\\r\"\n", true, true},
		{"[answer]\nrule = \"q\"\ntext_key = \"\\t\"\n", false, false},
		{"[answer]\nrule = \"q\"\ntext_option = \"x\"\ntext_focus = \"j\"\nsubmit = \"\\r\"\n", true, true},
		{"[answer]\nrule = \"q\"\ntext_focus = \"j\"\n", false, false},
		{"[answer]\nrule = \"idle\"\n", false, false},
		{"[answer]\nrule = \"nope\"\n", false, false},
		{"[answer]\ntext_option = \"x\"\nsubmit = \"\\r\"\n", false, false},
	} {
		m, err := ParseManifest([]byte(base + c.answer))
		if (err == nil) != c.ok {
			t.Errorf("%q: err = %v, want ok %v", c.answer, err, c.ok)
			continue
		}
		if err == nil && (AnswerOf(FromManifest(m)) != nil) != c.has {
			t.Errorf("%q: has answer = %v", c.answer, !c.has)
		}
	}
	r, _ := Load("")
	a, _ := r.Get("claude")
	if ans := AnswerOf(a); ans == nil || ans.Rule != "blocked-question" || ans.TextOption == "" {
		t.Fatalf("claude answer %+v", ans)
	}
}

func TestManifestListModels(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n"
	const args = "model_args = [\"--model\", \"{{.Model}}\"]\n"
	const lister = "[list_models]\nargs = [\"list\"]\n[list_models.models]\nreply = { id = \"1\" }\npath = \"models\"\nname = \"id\"\n"
	for _, c := range []struct {
		toml string
		has  bool
		ok   bool
	}{
		{"", false, true},
		{args + lister, true, true},
		{lister, false, false}, // no model_args
		{args + "[list_models]\nargs = [\"list\"]\n[list_models.models]\npath = \"models\"\nname = \"id\"\n", false, false}, // no reply
		{args + lister + "timeout_seconds = 500\n", false, false},                                                           // under [list_models.models]: unknown
		{args + lister + "[list_models.account]\npath = \"a\"\n", false, false},                                             // account without reply
		{args + "[[models]]\nname = \"big\"\nabout = \"older manifests still load\"\n", false, true},                        // ignored
	} {
		m, err := ParseManifest([]byte(base + c.toml))
		if (err == nil) != c.ok {
			t.Errorf("%q: err = %v, want ok %v", c.toml, err, c.ok)
			continue
		}
		if err == nil && (m.ListModels != nil) != c.has {
			t.Errorf("%q: has lister %v", c.toml, m.ListModels != nil)
		}
	}
	reg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	// Both built-in agents ask, and pass a model only when one is chosen:
	// tm ships no model list, and no default of its own.
	for name, flag := range map[string]string{"claude": "--model", "codex": "-m"} {
		a, _ := reg.Get(name)
		m := ManifestOf(a)
		if m.ListModels == nil || len(m.LegacyModels) > 0 {
			t.Errorf("%s: lister %v, [[models]] %d", name, m.ListModels != nil, len(m.LegacyModels))
		}
		spec := LaunchSpec{Role: RoleThread, Cwd: "/w", RuntimeDir: "/r", AgentSID: "x"}
		if l, err := a.Launch(spec); err != nil || slices.Contains(l.Argv, flag) {
			t.Errorf("%s without a model: %v %q", name, err, l.Argv)
		}
		spec.Model = "alpha-1"
		if l, err := a.Launch(spec); err != nil || !strings.Contains(strings.Join(l.Argv, " "), flag+" alpha-1") {
			t.Errorf("%s with a model: %v %q", name, err, l.Argv)
		}
	}
}

// TestRuleKeys: only a blocked rule may answer its dialog with keys.
func TestRuleKeys(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[[rules]]\nid = \"r\"\nregion = \"screen\"\ncontains = [\"x\"]\nkeys = \"3\"\n"
	if _, err := ParseManifest([]byte(base + "state = \"blocked\"\n")); err != nil {
		t.Errorf("blocked rule with keys: %v", err)
	}
	if _, err := ParseManifest([]byte(base + "state = \"idle\"\n")); err == nil {
		t.Error("an idle rule with keys passed")
	}
}

// TestManifestGuard: [guard] paths are relative to home or under a
// variable, left out while it is unset; every known agent's secrets
// count, a user manifest replacing a built-in one doesn't drop them, and
// the writable folders are the session agent's own.
func TestManifestGuard(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[guard]\n"
	for _, c := range []struct {
		guard string
		ok    bool
	}{
		{`secrets = [".a/key", "~/.a/token", "$A_HOME/key"]`, true},
		{`writable = [".a/plans"]`, true},
		{`secrets = ["/etc/a/key"]`, false},
		{`secrets = ["../x"]`, false},
		{`writable = ["$A_HOME/../x"]`, false},
		{`secrets = [""]`, false},
		{`escape_rule = "nope"`, false},
	} {
		if _, err := ParseManifest([]byte(base + c.guard + "\n")); (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok %v", c.guard, err, c.ok)
		}
	}
	env := map[string]string{"A_HOME": "/ah"}
	got := GuardPaths([]string{".a/key", "~/.a/token", "$A_HOME/key", "$UNSET/key"}, "/h", func(k string) string { return env[k] })
	if want := []string{"/h/.a/key", "/h/.a/token", "/ah/key"}; !slices.Equal(got, want) {
		t.Errorf("GuardPaths = %q, want %q", got, want)
	}
	if got := GuardPaths([]string{".a/key", "$A_HOME/key"}, "", func(k string) string { return env[k] }); !slices.Equal(got, []string{"/ah/key"}) {
		t.Errorf("GuardPaths without a home = %q", got)
	}

	env = map[string]string{"CODEX_HOME": "/ch"}
	getenv := func(k string) string { return env[k] }
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude.toml"), []byte("manifest_version = 1\nname = \"claude\"\n[launch]\ncommand = \"claude\"\n[guard]\nwritable = [\".my/plans\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*Registry{nil, reg} {
		s := GuardSecrets(r, "/h", getenv)
		for _, want := range []string{"/h/.claude/.credentials.json", "/h/.codex/auth.json", "/ch/auth.json"} {
			if !slices.Contains(s, want) {
				t.Errorf("GuardSecrets(%v) = %q, lacks %s", r != nil, s, want)
			}
		}
	}
	if got := GuardWritable(nil, "claude", "/h", getenv); !slices.Equal(got, []string{"/h/.claude/plans", "/h/.claude/projects"}) {
		t.Errorf("built-in claude writable = %q", got)
	}
	if got := GuardWritable(reg, "claude", "/h", getenv); !slices.Equal(got, []string{"/h/.my/plans"}) {
		t.Errorf("user claude writable = %q", got)
	}
	if got := GuardWritable(reg, "codex", "/h", getenv); got != nil {
		t.Errorf("codex writable = %q", got)
	}
}

// TestManifestGuardTools: [guard.tools] gives each tool a known kind and
// the input fields it reads; GuardTools is the session agent's own.
func TestManifestGuardTools(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[guard.tools]\n"
	for _, c := range []struct {
		tools string
		ok    bool
	}{
		{`bash = { kind = "shell", fields = ["command"] }` + "\n" + `edit = { kind = "write", fields = ["filePath"] }`, true},
		{`apply = { kind = "patch", fields = ["patch", "input"] }` + "\n" + `glob = { kind = "glob", fields = ["pattern", "path"] }` + "\n" + `read = { kind = "read", fields = ["filePath"] }`, true},
		{`bash = { kind = "exec", fields = ["command"] }`, false},
		{`bash = { kind = "shell" }`, false},
		{`bash = { kind = "shell", fields = [""] }`, false},
		{`bash = { kind = "shell", fields = ["command"], args = ["x"] }`, false},
	} {
		if _, err := ParseManifest([]byte(base + c.tools + "\n")); (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok %v", c.tools, err, c.ok)
		}
	}
	claude := GuardTools(nil, "claude")
	if claude["Bash"].Kind != guard.KindShell || claude["NotebookEdit"].Kind != guard.KindWrite || claude["Glob"].Kind != guard.KindGlob {
		t.Errorf("built-in claude tools = %v", claude)
	}
	if codex := GuardTools(nil, "codex"); codex["apply_patch"].Kind != guard.KindPatch || len(codex) != 2 {
		t.Errorf("built-in codex tools = %v", codex)
	}
	if GuardTools(nil, "nope") != nil {
		t.Error("an unknown agent has tools")
	}
}

// TestGuardToolVectors: the built-in manifests' [guard.tools] are the
// guard's test vectors' (internal/guard/testdata/vectors.json).
func TestGuardToolVectors(t *testing.T) {
	b, err := os.ReadFile("../guard/testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Tools map[string]guard.Tool `json:"tools"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		tools := GuardTools(nil, name)
		if len(tools) == 0 {
			t.Errorf("%s: no [guard.tools]", name)
		}
		for tool, x := range tools {
			got, ok := v.Tools[tool]
			if name != "claude" {
				// The escape field is Claude's (dangerouslyDisableSandbox):
				// Codex asks to leave its sandbox through its own prompt.
				got.Escape = ""
			}
			if !ok || !reflect.DeepEqual(got, x) {
				t.Errorf("%s %s: manifest %+v, vectors %+v", name, tool, x, got)
			}
		}
	}
}

// TestModAgents: an enabled mod's agents/<name>.toml loads as if in the
// user's agents folder; the user's own file of that name wins; a mod
// that is off adds nothing; a link out of the mod's folder, or a
// manifest with a character that doesn't show, is refused.
func TestModAgents(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TERMINATR_HOME", root)
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := func(name, cmd string) string {
		return "manifest_version = 1\nname = \"" + name + "\"\n[launch]\ncommand = \"" + cmd + "\"\n"
	}
	modToml := func(id string) string {
		return "id = \"" + id + "\"\nname = \"" + id + "\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nagents = \"agents\"\n"
	}
	write("mods/pi/mod.toml", modToml("pi"))
	write("mods/pi/agents/pi.toml", manifest("pi", "pi-from-mod"))
	write("mods/pi/agents/aider.toml", manifest("aider", "aider-from-mod"))
	write("mods/off/mod.toml", modToml("off"))
	write("mods/off/agents/off.toml", manifest("off", "off"))
	write("mods/pi/agents/hid.toml", manifest("hid", "hid\u200b"))
	write("agents/aider.toml", manifest("aider", "aider-mine"))
	write("config.toml", "[mods.pi]\nenabled = true\n")
	outside := filepath.Join(t.TempDir(), "evil.toml")
	os.WriteFile(outside, []byte(manifest("evil", "evil")), 0o644)
	if err := os.Symlink(outside, filepath.Join(root, "mods/pi/agents/evil.toml")); err != nil {
		t.Fatal(err)
	}

	r, err := Load(filepath.Join(root, "agents"))
	if err == nil || !strings.Contains(err.Error(), "mod pi: evil.toml: leads out") {
		t.Fatalf("link out: %v", err)
	}
	if !strings.Contains(err.Error(), "mod pi: hid.toml: line 4: holds a character that doesn't show (U+200B)") {
		t.Fatalf("hidden character: %v", err)
	}
	if _, ok := r.Get("pi"); !ok || !strings.Contains(r.Source["pi"], filepath.Join("mods", "pi", "agents")) {
		t.Fatalf("pi from the mod: %v", r.Source)
	}
	if !strings.HasSuffix(r.Source["aider"], filepath.Join("agents", "aider.toml")) || strings.Contains(r.Source["aider"], "mods") {
		t.Fatalf("the user's aider: %s", r.Source["aider"])
	}
	for _, n := range []string{"off", "evil", "hid"} {
		if _, ok := r.Get(n); ok {
			t.Fatalf("%s loaded", n)
		}
	}
}

// TestNoHumanSocket: no embedded agent's launch, in any role, grants
// the human socket (ipc.HumanName) or the run dir it shares with the
// server's: each names the server socket alone, so a sandboxed agent,
// and whatever it leaves running, can't reach the human-only methods
// (docs/SPEC.md §5.2, §11.1).
func TestNoHumanSocket(t *testing.T) {
	r, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	os.WriteFile(brief, []byte("Brief."), 0o600)
	const run, sock = "/tmrun", "/tmrun/tm.sock"
	for _, name := range r.Names() {
		a, _ := r.Get(name)
		for _, role := range []Role{RoleThread, Role("coordinator")} {
			spec := threadSpec()
			spec.Role, spec.RuntimeDir, spec.BriefPath, spec.Socket = role, dir, brief, sock
			l, err := a.Launch(spec)
			if err != nil {
				t.Fatalf("%s %s: %v", name, role, err)
			}
			all := append(append(slices.Clone(l.Argv), l.Env...), slices.Collect(maps.Keys(l.Files))...)
			for _, f := range l.Files {
				all = append(all, string(f))
			}
			for _, s := range all {
				if strings.Contains(s, ipc.HumanName) || strings.Count(s, run) != strings.Count(s, sock) {
					t.Errorf("%s %s launch grants more than the server socket: %.300q", name, role, s)
				}
			}
		}
	}
}

// TestManifestInterrupt: interrupt_paste is before or after, and needs a
// key; the built-in agents name theirs (T569).
func TestManifestInterrupt(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[inject]\n"
	for in, ok := range map[string]bool{
		"":                          true,
		"interrupt = \"\\u001b\"\n": true,
		"interrupt = \"\\u001b\"\ninterrupt_paste = \"before\"\n": true,
		"interrupt = \"\\u001b\"\ninterrupt_paste = \"after\"\n":  true,
		"interrupt = \"\\u001b\"\ninterrupt_paste = \"later\"\n":  false,
		"interrupt_paste = \"after\"\n":                           false,
	} {
		if _, err := ParseManifest([]byte(base + in)); (err == nil) != ok {
			t.Errorf("%q: err %v", in, err)
		}
	}
	for name, paste := range map[string]string{"claude": InterruptPasteBefore, "codex": InterruptPasteAfter} {
		b, _ := Builtin(name)
		m, err := ParseManifest(b)
		if err != nil {
			t.Fatal(err)
		}
		if m.Inject.Interrupt != "\x1b" || m.Inject.InterruptPaste != paste {
			t.Errorf("%s: interrupt %q, paste %q", name, m.Inject.Interrupt, m.Inject.InterruptPaste)
		}
	}
}

// TestClearsContext: /clear and a compaction forget the context use,
// for Claude (SessionEnd clear, SessionStart compact) and Codex
// (SessionStart clear and compact); other ends and starts don't.
func TestClearsContext(t *testing.T) {
	for _, c := range []struct {
		agent, event, field, value string
		want                       bool
	}{
		{"claude", "SessionEnd", "reason", "clear", true},
		{"claude", "SessionEnd", "reason", "logout", false},
		{"claude", "SessionStart", "source", "compact", true},
		{"claude", "SessionStart", "source", "startup", false},
		{"claude", "SessionStart", "source", "resume", false},
		{"codex", "SessionStart", "source", "clear", true},
		{"codex", "SessionStart", "source", "compact", true},
		{"codex", "SessionStart", "source", "startup", false},
	} {
		data, _ := builtin.ReadFile("manifests/" + c.agent + ".toml")
		m, err := ParseManifest(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := ClearsContext(FromManifest(m), c.event, map[string]any{c.field: c.value}); got != c.want {
			t.Errorf("%s %s %s=%s: %v, want %v", c.agent, c.event, c.field, c.value, got, c.want)
		}
	}
}

// TestHookCommand: a UserPromptSubmit with /saveclear or /answer runs
// the session command and blocks the prompt with what came of it, the
// session idle again; when the command didn't run, the prompt goes on
// with no answer.
func TestHookCommand(t *testing.T) {
	a := claude(t)
	for _, prompt := range []string{"/saveclear", "/terminatr:saveclear", "/answer", "/terminatr:answer"} {
		var ran []string
		env := HookEnv{Command: func(name string) string { ran = append(ran, name); return "Asked: " + name }}
		sigs, res, err := a.Hook(HookEvent{Event: "UserPromptSubmit", Payload: map[string]any{"prompt": prompt}}, env)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.TrimPrefix(strings.TrimPrefix(prompt, "/"), "terminatr:")
		var out struct{ Decision, Reason string }
		if json.Unmarshal(res.Stdout, &out) != nil || out.Decision != "block" || out.Reason != "Asked: "+want || !slices.Equal(ran, []string{want}) {
			t.Errorf("%s: %s, ran %v", prompt, res.Stdout, ran)
		}
		if len(sigs) == 0 || sigs[len(sigs)-1].State != StateIdle {
			t.Errorf("%s: signals %+v, want idle last", prompt, sigs)
		}
		// No answer (a session that can't run it): the
		// prompt goes on, and the session is working, not idle.
		for _, env := range []HookEnv{{Command: func(string) string { return "" }}, {}} {
			sigs, res, _ = a.Hook(HookEvent{Event: "UserPromptSubmit", Payload: map[string]any{"prompt": prompt}}, env)
			if len(res.Stdout) != 0 {
				t.Errorf("%s not run: %s", prompt, res.Stdout)
			}
			if len(sigs) == 0 || sigs[len(sigs)-1].State != StateWorking {
				t.Errorf("%s not run: signals %+v, want working last", prompt, sigs)
			}
		}
	}
	var ran bool
	_, res, _ := a.Hook(HookEvent{Event: "UserPromptSubmit", Payload: map[string]any{"prompt": "/saveclear now"}},
		HookEnv{Command: func(string) string { ran = true; return "x" }})
	if ran || len(res.Stdout) != 0 {
		t.Errorf("another prompt ran the command: %s", res.Stdout)
	}
}

// TestHookCommandValidated: a [[hooks]] command is saveclear or answer,
// with a respond to say what came of it.
func TestHookCommandValidated(t *testing.T) {
	const base = "manifest_version = 1\nname = \"a\"\n[launch]\ncommand = \"a\"\n[[hooks]]\nevent = \"E\"\n"
	for in, ok := range map[string]bool{
		"command = \"saveclear\"\nrespond = \"{{.Command}}\"\n": true,
		"command = \"answer\"\nrespond = \"{{.Command}}\"\n":    true,
		"command = \"saveclear\"\n":                             false,
		"command = \"rm\"\nrespond = \"{{.Command}}\"\n":        false,
	} {
		if _, err := ParseManifest([]byte(base + in)); (err == nil) != ok {
			t.Errorf("%q: err %v, want ok %v", in, err, ok)
		}
	}
}

// TestGuardIgnoresNoFields: a PreToolUse whatever fields it carries (a
// subagent's agent_id, which Claude's ignore_fields drops from state)
// is judged and answered with the refusal, for Claude and Codex alike;
// only its state signal is dropped.
func TestGuardIgnoresNoFields(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		data, _ := builtin.ReadFile("manifests/" + name + ".toml")
		m, err := ParseManifest(data)
		if err != nil {
			t.Fatal(err)
		}
		a := FromManifest(m)
		judged := 0
		env := HookEnv{Guard: func(tool string, input map[string]any) *guard.Denial {
			judged++
			return &guard.Denial{Rule: "merge", Message: "no merging"}
		}}
		payload := map[string]any{"hook_event_name": "PreToolUse", "agent_id": "sub-1", "session_id": "s", "tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr merge 1"}}
		sigs, res, err := a.Hook(HookEvent{Event: "PreToolUse", Payload: payload}, env)
		if err != nil || judged != 1 || !strings.Contains(string(res.Stdout), `"permissionDecision":"deny"`) {
			t.Fatalf("%s: judged %d, stdout %q, %v", name, judged, res.Stdout, err)
		}
		ignored := slices.Contains(m.IgnoreFields, "agent_id")
		for _, s := range sigs {
			if ignored && s.State != "" {
				t.Errorf("%s: an ignored event set state %+v", name, s)
			}
		}
		if out := FailClosed(a, "PreToolUse", payload); !strings.Contains(string(out), "tm couldn't check this call") {
			t.Errorf("%s: fail closed with agent_id: %q", name, out)
		}
	}
}

// TestGuardLocatedCwd: a call that leaves an optional location out
// (Claude's Grep path) is judged at the agent's own folder, the hook
// payload's cwd, which [hook] keep keeps; without an absolute cwd it
// goes to the guard as it came (the guard takes the session's folder),
// and a location given is never changed.
func TestGuardLocatedCwd(t *testing.T) {
	a := claude(t)
	if !slices.Contains(ManifestOf(a).Hook.Keep, ManifestOf(a).Hook.CwdField()) {
		t.Fatal("claude.toml's [hook] keep drops cwd")
	}
	var got map[string]any
	env := HookEnv{Guard: func(_ string, input map[string]any) *guard.Denial { got = input; return nil }}
	for _, c := range []struct {
		cwd   any
		input map[string]any
		want  any
	}{
		{"/home/u/agentdir", map[string]any{"pattern": "x"}, "/home/u/agentdir"},
		{"/home/u/agentdir", map[string]any{"pattern": "x", "path": "/elsewhere"}, "/elsewhere"},
		{"/home/u/agentdir", map[string]any{"pattern": "x", "path": ""}, "/home/u/agentdir"},
		{"relative", map[string]any{"pattern": "x"}, nil},
		{nil, map[string]any{"pattern": "x"}, nil},
	} {
		got = nil
		p := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Grep", "tool_input": c.input}
		if c.cwd != nil {
			p["cwd"] = c.cwd
		}
		if _, _, err := a.Hook(HookEvent{Event: "PreToolUse", Payload: p}, env); err != nil {
			t.Fatal(err)
		}
		if got["path"] != c.want {
			t.Errorf("cwd %v, input %v: judged %v, want path %v", c.cwd, c.input, got, c.want)
		}
		if c.want != "/elsewhere" {
			if v, ok := c.input["path"]; ok && v != "" {
				t.Errorf("the call's own input was changed: %v", c.input)
			}
		}
	}
}
