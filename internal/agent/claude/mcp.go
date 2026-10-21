package claude

import (
	"fmt"
	"path/filepath"
	"strings"
)

// tm's MCP server (docs/SPEC.md §8.6, tm mcp): without the mod, the
// launch gives Claude tm mcp as the server "terminatr" (claude.toml's
// claude-mcp.json), whose tools are the mcp__terminatr__<name> the
// briefs name, and an mcp__terminatr rule that runs them without a
// prompt. A server of the user's or the repo's with that name would
// stand in for tm's, and the rule would let its tools run unasked.

// mcpServer is the name claude.toml gives tm mcp.
const mcpServer = "terminatr"

// maxConfig bounds the read of ~/.claude.json, which keeps every
// folder Claude ran in.
const maxConfig = 64 << 20

// MCPClash makes the Claude agent an agent.MCPClasher: what the launch
// tells the human when Claude's own config names a server "terminatr",
// "" when none does. It reads the user's servers and dir's local ones
// (~/.claude.json's mcpServers and projects[dir].mcpServers) and dir's
// .mcp.json; a file missing or not JSON names none.
func (a *Agent) MCPClash(home, dir string) string {
	var in []string
	cfg := configPath(home)
	var user struct {
		MCPServers map[string]any `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	if readJSON(filepath.Dir(cfg), filepath.Base(cfg), maxConfig, &user) {
		if _, ok := user.MCPServers[mcpServer]; ok {
			in = append(in, homeName(home, cfg))
		}
		dirs := []string{projectKey(dir)}
		if r, err := filepath.EvalSymlinks(dir); err == nil && r != dir {
			dirs = append(dirs, projectKey(r))
		}
		for _, d := range dirs {
			if _, ok := user.Projects[d].MCPServers[mcpServer]; ok && dir != "" {
				in = append(in, fmt.Sprintf("%s (projects[%q])", homeName(home, cfg), d))
				break
			}
		}
	}
	var project struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if dir != "" && readJSON(dir, ".mcp.json", maxSettings, &project) {
		if _, ok := project.MCPServers[mcpServer]; ok {
			in = append(in, ".mcp.json")
		}
	}
	if len(in) == 0 {
		return ""
	}
	return fmt.Sprintf("Claude's own MCP server %q (%s) has the name of tm's: tm's tools (mcp__%s__*) now ask before they run; rename that server to run them unasked",
		mcpServer, strings.Join(in, ", "), mcpServer)
}
