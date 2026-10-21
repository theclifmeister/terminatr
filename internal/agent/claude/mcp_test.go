package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// TestMCPClash: a server "terminatr" in the user's servers, the
// folder's local ones or its .mcp.json is named in the launch's
// warning; others, or none, give none.
func TestMCPClash(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	c, ok := load(t).(agent.MCPClasher)
	if !ok {
		t.Fatal("the claude agent is no MCPClasher")
	}
	home, dir := t.TempDir(), t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	cfg := filepath.Join(home, ".claude.json")
	if w := c.MCPClash(home, dir, false); w != "" {
		t.Fatalf("no config: %q", w)
	}
	os.WriteFile(cfg, []byte(`{"mcpServers": {"github": {}}, "projects": {"/other": {"mcpServers": {"terminatr": {}}}}}`), 0o600)
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers": {"terminatr-x": {}}}`), 0o600)
	if w := c.MCPClash(home, dir, false); w != "" {
		t.Fatalf("other servers: %q", w)
	}
	for _, tc := range []struct{ user, project, want string }{
		{`{"mcpServers": {"terminatr": {}}}`, `{}`, "(~/.claude.json)"},
		{`{"projects": {"` + filepath.ToSlash(dir) + `": {"mcpServers": {"terminatr": {}}}}}`, `{}`, `projects[`},
		{`{}`, `{"mcpServers": {"terminatr": {"command": "x"}}}`, "(.mcp.json)"},
	} {
		os.WriteFile(cfg, []byte(tc.user), 0o600)
		os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(tc.project), 0o600)
		if w := c.MCPClash(home, dir, false); !strings.Contains(w, tc.want) || !strings.Contains(w, `"terminatr"`) {
			t.Errorf("%s %s: %q, want %s", tc.user, tc.project, w, tc.want)
		}
	}
}

// TestMCPClashFailsClosed: a file that exists but can't be checked
// counts as one with the name, and the warning says so; one malformed
// folder entry hides no user server; a BOM and a linked ~/.claude.json
// are read; an allow rule of the user's own is named.
func TestMCPClashFailsClosed(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	c := load(t).(agent.MCPClasher)
	home, dir := t.TempDir(), t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	cfg := filepath.Join(home, ".claude.json")
	mcp := filepath.Join(dir, ".mcp.json")
	reset := func() {
		os.Remove(cfg)
		os.Remove(mcp)
		os.RemoveAll(filepath.Join(home, ".claude"))
	}
	clash := func(name, want string) {
		t.Helper()
		if w := c.MCPClash(home, dir, false); !strings.Contains(w, want) {
			t.Errorf("%s: %q, want %q in it", name, w, want)
		}
	}

	reset()
	os.WriteFile(cfg, []byte(`{"mcpServers": {"terminatr": `), 0o600)
	clash("unparsable", "couldn't be checked")

	reset()
	os.WriteFile(cfg, []byte(`{"mcpServers": ["terminatr"]}`), 0o600)
	clash("mcpServers not an object", "couldn't be checked")

	reset()
	os.WriteFile(cfg, []byte(`{"mcpServers": {"terminatr": {}}, "projects": {"/other": 7, "/x": {"mcpServers": 1}}}`), 0o600)
	clash("malformed siblings", "(~/.claude.json) has the name")

	reset()
	os.WriteFile(cfg, []byte("\xef\xbb\xbf"+`{"mcpServers": {"terminatr": {}}}`), 0o600)
	clash("BOM", "has the name")

	reset()
	dots := filepath.Join(t.TempDir(), "claude.json")
	os.WriteFile(dots, []byte(`{"mcpServers": {"terminatr": {}}}`), 0o600)
	if err := os.Symlink(dots, cfg); err != nil {
		t.Fatal(err)
	}
	clash("linked config", "has the name")

	reset()
	if err := os.Symlink(dots, mcp); err != nil {
		t.Fatal(err)
	}
	clash(".mcp.json linked out of the folder", ".mcp.json (")

	reset()
	os.WriteFile(mcp, append([]byte(`{"x": "`), append(make([]byte, maxSettings), '"', '}')...), 0o600)
	clash("oversize", "over ")

	if os.Getuid() != 0 {
		reset()
		os.WriteFile(cfg, []byte(`{}`), 0o000)
		clash("unreadable", "couldn't be checked")
	}

	reset()
	os.WriteFile(mcp, []byte(`{"mcpServers": {"terminatr": {}}}`), 0o600)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"permissions": {"allow": ["mcp__terminatr__report", "mcp__terminatrx"]}}`), 0o600)
	w := c.MCPClash(home, dir, false)
	if !strings.Contains(w, "mcp__terminatr__report in ~/.claude/settings.json") || strings.Contains(w, "mcp__terminatrx") {
		t.Errorf("own allow rule: %q", w)
	}

	if w := c.MCPClash(home, dir, true); !strings.Contains(w, "still stop and ask") {
		t.Errorf("yolo: %q", w)
	}

	reset()
	if w := c.MCPClash(home, dir, true); w != "" {
		t.Errorf("nothing there: %q", w)
	}
}
