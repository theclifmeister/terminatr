package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// mcpStep is a call to a tool of the MCP server the --mcp-config file
// names, as Claude makes it: it starts the server (stdio, the config's
// command and args, Claude's environment), initializes, lists the tools
// and calls st.Tool, the server's own name for it, with st.Input. It
// logs an "mcp" record: the tools listed (as the model sees them,
// mcp__<server>__<name>), the call's text and is_error, or the error.
func (a *app) mcpStep(ctx context.Context, st step) {
	rec := map[string]any{"tool": st.Tool}
	defer func() { a.log("mcp", rec) }()
	if len(a.opts.mcpConfigs) == 0 {
		rec["error"] = "no --mcp-config"
		return
	}
	b, err := os.ReadFile(a.opts.mcpConfigs[0])
	if err != nil {
		rec["error"] = err.Error()
		return
	}
	var cfg struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		rec["error"] = err.Error()
		return
	}
	var tools []string
	for name, srv := range cfg.MCPServers {
		rec["server"] = name
		listed, text, isErr, err := mcpCall(ctx, a.cwd, srv.Command, srv.Args, st.Tool, orEmpty(st.Input))
		if err != nil {
			rec["error"] = err.Error()
			return
		}
		for _, t := range listed {
			tools = append(tools, "mcp__"+name+"__"+t)
		}
		rec["text"], rec["is_error"] = text, isErr
		break
	}
	rec["tools"] = tools
}

// mcpCall runs one MCP session on the server command: initialize,
// tools/list, tools/call name with input.
func mcpCall(ctx context.Context, dir, command string, args []string, name string, input map[string]any) (tools []string, text string, isErr bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", false, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", false, err
	}
	defer func() {
		in.Close()
		cmd.Wait()
	}()
	enc, dec := json.NewEncoder(in), bufio.NewScanner(out)
	dec.Buffer(make([]byte, 64<<10), 4<<20)
	id := 0
	call := func(method string, params any, res any) error {
		id++
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return err
		}
		if !dec.Scan() {
			return fmt.Errorf("%s: no answer: %v", method, dec.Err())
		}
		var msg struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(dec.Bytes(), &msg); err != nil {
			return err
		}
		if msg.Error != nil {
			return fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		return json.Unmarshal(msg.Result, res)
	}
	var init map[string]any
	if err := call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "fakeagent", "version": "0"}}, &init); err != nil {
		return nil, "", false, err
	}
	enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := call("tools/list", map[string]any{}, &list); err != nil {
		return nil, "", false, err
	}
	for _, t := range list.Tools {
		tools = append(tools, t.Name)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := call("tools/call", map[string]any{"name": name, "arguments": input}, &res); err != nil {
		return tools, "", false, err
	}
	if len(res.Content) > 0 {
		text = res.Content[0].Text
	}
	return tools, text, res.IsError, nil
}
