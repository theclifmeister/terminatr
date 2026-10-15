package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/version"
)

// tm mcp (docs/SPEC.md §8.6, Mods): a session's tools (a thread's
// report, status, steps and done, and the tools enabled user mods offer
// the session) as an MCP server on stdio, for agents without the Claude
// mod that speak MCP, such as Codex. It is a thin bridge: tools/list asks
// the server with tool.list and each call goes to it as tool.run; the
// server tells the session from the process tree (the agent started this
// process, so it is inside the session) and answers as the mod socket's
// GET /v1/tools and POST /v1/tools/{name} would. When the server can't
// be asked, the list is a thread's own tools.

const mcpProtocol = "2025-06-18"

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// runMCP serves MCP on in/out until in closes. list gives the tools,
// call runs one.
func runMCP(in io.Reader, out io.Writer, list func() []proto.ToolDef, call func(name string, input json.RawMessage) (text string, isErr bool)) error {
	enc := json.NewEncoder(out)
	reply := func(id json.RawMessage, result any, e *rpcErr) {
		m := map[string]any{"jsonrpc": "2.0", "id": id}
		if e != nil {
			m["error"] = e
		} else {
			m["result"] = result
		}
		enc.Encode(m)
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var m rpcMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			reply(json.RawMessage("null"), nil, &rpcErr{-32700, "parse error"})
			continue
		}
		if len(m.ID) == 0 { // a notification
			continue
		}
		switch m.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(m.Params, &p)
			ver := mcpProtocol
			if p.ProtocolVersion != "" {
				ver = p.ProtocolVersion
			}
			reply(m.ID, map[string]any{
				"protocolVersion": ver,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "terminatr", "version": version.Version},
			}, nil)
		case "ping":
			reply(m.ID, map[string]any{}, nil)
		case "tools/list":
			reply(m.ID, map[string]any{"tools": list()}, nil)
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(m.Params, &p); err != nil || p.Name == "" {
				reply(m.ID, nil, &rpcErr{-32602, "tools/call wants a name"})
				continue
			}
			if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
				p.Arguments = json.RawMessage("{}")
			}
			text, isErr := call(p.Name, p.Arguments)
			reply(m.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": isErr,
			}, nil)
		default:
			reply(m.ID, nil, &rpcErr{-32601, "method not found: " + m.Method})
		}
	}
	return sc.Err()
}

// runMCPCmd is tm mcp.
func runMCPCmd(e *Env, args []string) error {
	if len(args) > 0 {
		return usagef("usage: tm mcp   (an MCP server on stdio: a thread's tools)")
	}
	ask := func(method string, params, res any) error {
		p, err := client.ResolvePaths()
		if err != nil {
			return err
		}
		c, err := client.Connect(p, false)
		if err != nil {
			return err
		}
		defer c.Close()
		return c.Call(method, params, res)
	}
	list := func() []proto.ToolDef {
		var res proto.ToolListResult
		if err := ask(proto.MethodToolList, struct{}{}, &res); err != nil {
			return server.ThreadTools
		}
		return res.Tools
	}
	call := func(name string, input json.RawMessage) (string, bool) {
		var res proto.ToolRunResult
		if err := ask(proto.MethodToolRun, proto.ToolRunParams{Name: name, Input: input}, &res); err != nil {
			return fmt.Sprintf("tm server: %v", err), true
		}
		if res.Text == "" && !res.IsError {
			res.Text = "done"
		}
		return res.Text, res.IsError
	}
	return runMCP(e.Stdin, e.Stdout, list, call)
}
