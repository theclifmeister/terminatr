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
	if w := c.MCPClash(home, dir); w != "" {
		t.Fatalf("no config: %q", w)
	}
	os.WriteFile(cfg, []byte(`{"mcpServers": {"github": {}}, "projects": {"/other": {"mcpServers": {"terminatr": {}}}}}`), 0o600)
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers": {"terminatr-x": {}}}`), 0o600)
	if w := c.MCPClash(home, dir); w != "" {
		t.Fatalf("other servers: %q", w)
	}
	for _, tc := range []struct{ user, project, want string }{
		{`{"mcpServers": {"terminatr": {}}}`, `{}`, "(~/.claude.json)"},
		{`{"projects": {"` + filepath.ToSlash(dir) + `": {"mcpServers": {"terminatr": {}}}}}`, `{}`, `projects[`},
		{`{}`, `{"mcpServers": {"terminatr": {"command": "x"}}}`, "(.mcp.json)"},
	} {
		os.WriteFile(cfg, []byte(tc.user), 0o600)
		os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(tc.project), 0o600)
		if w := c.MCPClash(home, dir); !strings.Contains(w, tc.want) || !strings.Contains(w, `"terminatr"`) {
			t.Errorf("%s %s: %q, want %s", tc.user, tc.project, w, tc.want)
		}
	}
}
