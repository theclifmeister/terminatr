package claude

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// Claude's own settings files (T574 §3.1): Claude merges the user's
// settings.json and the folder's .claude/settings.json and
// settings.local.json (where "Yes, don't ask again" lands) with tm's
// --settings file. tm doesn't write them; it lists their rules in a
// thread's effective access, read-only.

// maxSettings bounds one settings file read.
const maxSettings = 1 << 20

// ownSettings is what a settings file says that bears on access.
type ownSettings struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	} `json:"permissions"`
	Sandbox struct {
		ExcludedCommands []string `json:"excludedCommands"`
	} `json:"sandbox"`
}

// OwnRules lists the allow, deny and excluded-command rules of the
// user's settings and dir's, in that order; a file missing or not JSON
// gives none.
func (a *Agent) OwnRules(home, dir string) []agent.OwnRule {
	user := filepath.Join(configDir(home), "settings.json")
	files := []struct{ path, name string }{{user, homeName(home, user)}}
	if dir != "" {
		for _, f := range []string{"settings.json", "settings.local.json"} {
			files = append(files, struct{ path, name string }{filepath.Join(dir, ".claude", f), filepath.Join(".claude", f)})
		}
	}
	var out []agent.OwnRule
	for _, f := range files {
		s, ok := readSettings(f.path)
		if !ok {
			continue
		}
		add := func(kind string, vals []string) {
			for _, v := range vals {
				out = append(out, agent.OwnRule{Kind: kind, Value: v, File: f.name})
			}
		}
		add("allow", s.Permissions.Allow)
		add("deny", s.Permissions.Deny)
		add("excluded", s.Sandbox.ExcludedCommands)
	}
	return out
}

// homeName is path with home as ~.
func homeName(home, path string) string {
	if rel, err := filepath.Rel(home, path); err == nil && home != "" && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
}

// readSettings reads one settings file, ok false when it is missing,
// too large or not JSON.
func readSettings(path string) (ownSettings, bool) {
	var s ownSettings
	f, err := os.Open(path)
	if err != nil {
		return s, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxSettings+1))
	if err != nil || len(b) > maxSettings {
		return s, false
	}
	return s, json.Unmarshal(b, &s) == nil
}
