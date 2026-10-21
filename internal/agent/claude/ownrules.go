package claude

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
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
// user's settings and dir's, in that order; a file missing, not a
// regular file or not JSON gives none, and so does one of dir's that
// is a link out of dir or an absolute link (a thread writes its
// worktree; os.Root follows relative links that stay inside only).
func (a *Agent) OwnRules(home, dir string) []agent.OwnRule {
	var out []agent.OwnRule
	add := func(s ownSettings, name string) {
		for _, l := range []struct {
			kind string
			vals []string
		}{{"allow", s.Permissions.Allow}, {"deny", s.Permissions.Deny}, {"excluded", s.Sandbox.ExcludedCommands}} {
			for _, v := range l.vals {
				out = append(out, agent.OwnRule{Kind: l.kind, Value: v, File: name})
			}
		}
	}
	// The user's own file may be a link (to their dotfiles): it is read
	// where it leads.
	user := filepath.Join(configDir(home), "settings.json")
	if real, err := filepath.EvalSymlinks(user); err == nil {
		if s, ok := readSettings(filepath.Dir(real), filepath.Base(real)); ok {
			add(s, homeName(home, user))
		}
	}
	if dir != "" {
		for _, f := range []string{"settings.json", "settings.local.json"} {
			rel := filepath.Join(".claude", f)
			if s, ok := readSettings(dir, rel); ok {
				add(s, rel)
			}
		}
	}
	return out
}

// OwnFiles are the files OwnRules reads: the user's settings (and where
// a link leads) and dir's two.
func (a *Agent) OwnFiles(home, dir string) []string {
	user := filepath.Join(configDir(home), "settings.json")
	out := []string{user}
	if real, err := filepath.EvalSymlinks(user); err == nil && real != user {
		out = append(out, real)
	}
	if dir != "" {
		out = append(out, filepath.Join(dir, ".claude", "settings.json"), filepath.Join(dir, ".claude", "settings.local.json"))
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

// readSettings reads settings file rel under root, ok false when it is
// missing, a link out of root, not a regular file (a FIFO would block
// the read), too large or not JSON.
func readSettings(root, rel string) (ownSettings, bool) {
	var s ownSettings
	return s, readJSON(root, rel, maxSettings, &s)
}

// readJSON decodes JSON file rel under root into v, false as for
// readSettings, with max bytes at most.
func readJSON(root, rel string, max int64, v any) bool {
	r, err := os.OpenRoot(root)
	if err != nil {
		return false
	}
	defer r.Close()
	f, err := fsx.OpenInNoBlock(r, rel)
	if err != nil {
		return false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || fi.Size() > max {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		return false
	}
	return json.Unmarshal(b, v) == nil
}
