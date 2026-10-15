package proto

import (
	"encoding/json"
)

// MethodToolRun runs one of a session's tools (a thread's report,
// status, steps and done, and the tools user mods offer; docs/SPEC.md
// §8.6) for `tm mcp`, which serves them to agents that speak MCP. The
// server tells the session from the peer pid, as for cli.run, and
// answers like POST /v1/tools/{name} on the mod socket.
const MethodToolRun = "tool.run"

// MethodToolList lists the tools of the caller's session, found from
// the peer pid, as GET /v1/tools on the mod socket does: no params,
// answered with ToolListResult.
const MethodToolList = "tool.list"

// ToolDef is a tool as an agent is given it: its name, description and
// input as a JSON schema (MCP's tools/list shape, and what Claude's
// $.tool.register takes).
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolListResult is the answer of tool.list and GET /v1/tools.
type ToolListResult struct {
	Tools []ToolDef `json:"tools"`
}

// ToolRunParams are the params of tool.run: the tool and its input as
// the model gave it (a JSON object).
type ToolRunParams struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolRunResult is the answer of tool.run: the tool's text, or with
// IsError why it was refused, for the model to read.
type ToolRunResult struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error,omitempty"`
}
