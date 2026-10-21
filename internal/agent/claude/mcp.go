package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// tm's MCP server (docs/SPEC.md §8.6, tm mcp): without the mod, the
// launch gives Claude tm mcp as the server "terminatr" (claude.toml's
// claude-mcp.json), whose tools are the mcp__terminatr__<name> the
// briefs name, and an mcp__terminatr rule that runs them without a
// prompt. A server of the user's or the repo's with that name would
// stand in for tm's, and the rule would let its tools run unasked. The
// check fails closed: a file it can't read counts as one that has it.

// mcpServer is the name claude.toml gives tm mcp.
const mcpServer = "terminatr"

// maxConfig bounds the read of ~/.claude.json, which keeps every
// folder Claude ran in.
const maxConfig = 64 << 20

// MCPClash makes the Claude agent an agent.MCPClasher: what the launch
// tells the human when Claude's own config names a server "terminatr",
// or may (a file that exists but can't be read, is too large, leads out
// of its folder through a link or isn't JSON), "" when none does. It
// reads the user's servers and dir's local ones (~/.claude.json, where
// a link leads, its mcpServers and projects[dir].mcpServers) and dir's
// .mcp.json, and names an allow rule of the user's or dir's own
// settings that names the tools. The ask rule tm's settings then give
// holds over any allow rule, the user's auto mode and
// --dangerously-skip-permissions (checked on 2.1.306), so a yolo
// session is told it asks too.
func (a *Agent) MCPClash(home, dir string, yolo bool) string {
	var found, unchecked []string
	cfg := configPath(home)
	name := homeName(home, cfg)
	if b, err := readUserConfig(cfg); err != nil {
		unchecked = append(unchecked, fmt.Sprintf("%s (%v)", name, err))
	} else if b != nil {
		var keys []string
		if dir != "" {
			keys = append(keys, projectKey(dir))
			if r, err := filepath.EvalSymlinks(dir); err == nil && r != dir {
				keys = append(keys, projectKey(r))
			}
		}
		user, local, err := userServers(b, keys)
		switch {
		case err != nil:
			unchecked = append(unchecked, fmt.Sprintf("%s (%v)", name, err))
		case user:
			found = append(found, name)
		}
		if local != "" {
			found = append(found, fmt.Sprintf("%s (projects[%q])", name, local))
		}
	}
	if dir != "" {
		b, err := readIn(dir, ".mcp.json", maxSettings)
		if err == nil && b != nil {
			var has bool
			var top map[string]json.RawMessage
			if err = json.Unmarshal(b, &top); err == nil {
				has, err = hasServer(top["mcpServers"])
			}
			if has {
				found = append(found, ".mcp.json")
			}
		}
		if err != nil {
			unchecked = append(unchecked, fmt.Sprintf(".mcp.json (%v)", err))
		}
	}
	if len(found)+len(unchecked) == 0 {
		return ""
	}
	var w []string
	if len(found) > 0 {
		w = append(w, fmt.Sprintf("Claude's own MCP server %q (%s) has the name of tm's", mcpServer, strings.Join(found, ", ")))
	}
	if len(unchecked) > 0 {
		w = append(w, fmt.Sprintf("Claude's MCP servers in %s couldn't be checked for one named %q", strings.Join(unchecked, ", "), mcpServer))
	}
	msg := strings.Join(w, "; ") + fmt.Sprintf(": tm's tools (mcp__%s__*) ask before they run; rename that server to run them unasked", mcpServer)
	var own []string
	for _, r := range a.OwnRules(home, dir) {
		if r.Kind == "allow" && namesServer(r.Value) {
			own = append(own, fmt.Sprintf("%s in %s", r.Value, r.File))
		}
	}
	if len(own) > 0 {
		msg += fmt.Sprintf(". Your own allow rule %s names that server's tools too: tm's ask rule comes first, but remove it", strings.Join(own, ", "))
	}
	if yolo {
		msg += ". This session runs without prompts, but these calls still stop and ask"
	}
	return msg
}

// namesServer reports whether a permission rule names tm's server or
// one of its tools.
func namesServer(rule string) bool {
	p := "mcp__" + mcpServer
	return rule == p || strings.HasPrefix(rule, p+"__")
}

// userServers reads ~/.claude.json: whether its mcpServers has tm's
// name, and the first of keys whose projects entry's mcpServers has it.
// Each level is decoded on its own, so another folder's entry, however
// malformed, hides nothing; a level that matters and isn't an object
// is an error.
func userServers(b []byte, keys []string) (user bool, local string, err error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return false, "", err
	}
	if user, err = hasServer(top["mcpServers"]); err != nil {
		return false, "", fmt.Errorf("mcpServers: %w", err)
	}
	var projects map[string]json.RawMessage
	if err := unmarshalSet(top["projects"], &projects); err != nil {
		return user, "", fmt.Errorf("projects: %w", err)
	}
	for _, k := range keys {
		var entry map[string]json.RawMessage
		if err := unmarshalSet(projects[k], &entry); err != nil {
			return user, "", fmt.Errorf("projects[%q]: %w", k, err)
		}
		has, err := hasServer(entry["mcpServers"])
		if err != nil {
			return user, "", fmt.Errorf("projects[%q].mcpServers: %w", k, err)
		}
		if has {
			return user, k, nil
		}
	}
	return user, "", nil
}

// hasServer reports whether the mcpServers object raw has tm's name;
// none (missing or null) hasn't.
func hasServer(raw json.RawMessage) (bool, error) {
	var servers map[string]json.RawMessage
	if err := unmarshalSet(raw, &servers); err != nil {
		return false, err
	}
	_, ok := servers[mcpServer]
	return ok, nil
}

// unmarshalSet decodes raw into v, leaving v as it is for a value not
// set (missing or null).
func unmarshalSet(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// readUserConfig reads Claude's global config where a link leads, as
// OwnRules reads the user's settings: nil, nil when it doesn't exist.
func readUserConfig(cfg string) ([]byte, error) {
	real, err := filepath.EvalSymlinks(cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readIn(filepath.Dir(real), filepath.Base(real), maxConfig)
}

// readIn reads file rel under root, without a leading UTF-8 BOM: nil,
// nil when it doesn't exist; an error when it exists but can't be read,
// leads out of root through a link, isn't a regular file (a FIFO would
// block the read) or is over max bytes.
func readIn(root, rel string, max int64) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := fsx.OpenInNoBlock(r, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("over %d bytes", max)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("over %d bytes", max)
	}
	return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), nil
}
