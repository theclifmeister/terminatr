package claude

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// pluginDir is where the manifest's launch.files put the plugin, inside
// the runtime dir.
const pluginDir = "claude-plugin"

func pluginSpec(dir string) agent.LaunchSpec {
	return agent.LaunchSpec{Role: agent.RoleThread, SessionID: "s-1", AgentSID: "abc", Cwd: dir,
		RuntimeDir: dir, TMBin: "/usr/local/bin/tm", Socket: "/tmp/tm.sock"}
}

// TestLaunchPlugin: the plugin folder holds plugin.json and hooks.json
// with a command hook for each event the manifest maps, and the typed
// tools come from tm mcp, the MCP server terminatr, with its allow rule.
func TestLaunchPlugin(t *testing.T) {
	l, err := load(t).Launch(pluginSpec(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	var hooks struct {
		Modules []string                   `json:"modules"`
		Hooks   map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(l.Files["claude-plugin/hooks/hooks.json"], &hooks); err != nil {
		t.Fatalf("hooks.json: %v", err)
	}
	var plugin map[string]string
	if err := json.Unmarshal(l.Files["claude-plugin/.claude-plugin/plugin.json"], &plugin); err != nil {
		t.Fatalf("plugin.json: %v", err)
	}
	if plugin["name"] != "terminatr" || plugin["types"] != "" || len(hooks.Modules) != 0 {
		t.Fatalf("plugin %v, modules %v", plugin, hooks.Modules)
	}
	if len(hooks.Hooks) != 13 {
		t.Fatalf("%d command hooks, want 13", len(hooks.Hooks))
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "SubagentStart", "SubagentStop", "SessionEnd"} {
		if hooks.Hooks[ev] == nil {
			t.Fatalf("no %s hook", ev)
		}
	}
	var settings struct {
		Permissions struct{ Allow []string } `json:"permissions"`
	}
	if err := json.Unmarshal(l.Files["claude-settings.json"], &settings); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if i := slices.Index(l.Argv, "--mcp-config"); i < 0 || !strings.HasSuffix(l.Argv[i+1], "/claude-mcp.json") || l.Files["claude-mcp.json"] == nil {
		t.Fatalf("argv %q: no tm mcp", l.Argv)
	}
	if !slices.Contains(settings.Permissions.Allow, "mcp__terminatr") {
		t.Fatalf("mcp__terminatr not allowed: %v", settings.Permissions.Allow)
	}
}

// writePlugin renders the plugin a session gets into dir and returns
// the plugin folder.
func writePlugin(t *testing.T, dir string) string {
	t.Helper()
	l, err := load(t).Launch(pluginSpec(dir))
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range l.Files {
		f := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, pluginDir)
}

// TestPlugin runs `claude plugin validate` on the plugin exactly as a
// session gets it. It needs the Claude Code CLI, so it runs only with
// TERMINATR_CLAUDE_PLUGIN_CHECK=1 (the weekly workflow); `claude` is
// then required.
func TestPlugin(t *testing.T) {
	if os.Getenv("TERMINATR_CLAUDE_PLUGIN_CHECK") != "1" {
		t.Skip("set TERMINATR_CLAUDE_PLUGIN_CHECK=1 to run claude plugin validate")
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	dir := writePlugin(t, t.TempDir())
	out, err := exec.Command(claude, "plugin", "validate", dir).CombinedOutput()
	t.Logf("claude plugin validate:\n%s", out)
	if err != nil {
		t.Fatalf("claude plugin validate: %v", err)
	}
}

// TestSlashCommands: a coordinator's /saveclear and /answer are plain
// plugin commands; other roles get neither.
func TestSlashCommands(t *testing.T) {
	a := load(t)
	for _, c := range []struct {
		role agent.Role
		want bool
	}{
		{"coordinator", true},
		{agent.RoleThread, false},
	} {
		spec := pluginSpec(t.TempDir())
		spec.Role = c.role
		l, err := a.Launch(spec)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"saveclear", "answer"} {
			b, ok := l.Files["claude-plugin/commands/"+name+".md"]
			if ok != c.want {
				t.Errorf("%s: commands/%s.md written %v", c.role, name, ok)
			}
			if ok && (!strings.HasPrefix(string(b), "---\ndescription: ") || !strings.Contains(string(b), "tm ")) {
				t.Errorf("commands/%s.md:\n%s", name, b)
			}
		}
	}
}
