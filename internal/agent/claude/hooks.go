package claude

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Settings that turn tm's hooks off (docs/SPEC.md §8.6, Guard): the
// command hooks are how tm sees a Claude session's state and judges its
// tool calls, so a session without them runs unguarded. tm's --settings
// file sets disableAllHooks to false, which wins over the user's and the
// folder's own settings (checked live on 2.1.306); Claude's managed
// settings win over it, and their allowManagedHooksOnly drops every hook
// but the managed ones, tm's plugin's included.

// managedSettings are where Claude reads managed settings, per OS (only
// the one for this OS exists).
var managedSettings = []string{
	"/Library/Application Support/ClaudeCode/managed-settings.json",
	"/etc/claude-code/managed-settings.json",
	`C:\Program Files\ClaudeCode\managed-settings.json`,
}

// hookSettings is what a settings file says of hooks.
type hookSettings struct {
	DisableAllHooks       bool `json:"disableAllHooks"`
	AllowManagedHooksOnly bool `json:"allowManagedHooksOnly"`
}

// HooksOff makes the Claude agent an agent.HooksWarner: what to tell
// the human when Claude's settings turn tm's hooks off, or try to, ""
// when none does. It reads the managed settings, the user's settings
// (where a link leads) and dir's .claude/settings.json and
// settings.local.json ("" dir: none).
func (a *Agent) HooksOff(home, dir string) string {
	var managed, own []string
	for _, p := range managedSettings {
		s, ok := readHookSettings(filepath.Dir(p), filepath.Base(p))
		switch {
		case ok && s.DisableAllHooks:
			managed = append(managed, p+" (disableAllHooks)")
		case ok && s.AllowManagedHooksOnly:
			managed = append(managed, p+" (allowManagedHooksOnly)")
		}
	}
	user := filepath.Join(configDir(home), "settings.json")
	if real, err := filepath.EvalSymlinks(user); err == nil {
		if s, ok := readHookSettings(filepath.Dir(real), filepath.Base(real)); ok && s.DisableAllHooks {
			own = append(own, homeName(home, user))
		}
	}
	if dir != "" {
		for _, f := range []string{"settings.json", "settings.local.json"} {
			rel := filepath.Join(".claude", f)
			if s, ok := readHookSettings(dir, rel); ok && s.DisableAllHooks {
				own = append(own, rel)
			}
		}
	}
	var w []string
	if len(managed) > 0 {
		w = append(w, fmt.Sprintf("Claude's managed settings %s turn tm's hooks off: tm can't see this session's state or guard its tool calls; only your administrator can change it", strings.Join(managed, ", ")))
	}
	if len(own) > 0 {
		w = append(w, fmt.Sprintf("Claude's settings %s set disableAllHooks: tm's own settings for its sessions set it back to false, but remove it", strings.Join(own, ", ")))
	}
	return strings.Join(w, "; ")
}

// readHookSettings reads settings file rel under root, ok false when it
// is missing, a link out of root, not a regular file, too large or not
// JSON.
func readHookSettings(root, rel string) (hookSettings, bool) {
	var s hookSettings
	b, err := readIn(root, rel, maxSettings)
	if err != nil || b == nil {
		return s, false
	}
	return s, json.Unmarshal(b, &s) == nil
}
