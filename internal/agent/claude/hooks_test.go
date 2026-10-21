package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
)

// noHookSettings points HooksOff at an empty home and no managed
// settings; it returns the home.
func noHookSettings(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	old := managedSettings
	managedSettings = []string{filepath.Join(t.TempDir(), "managed-settings.json")}
	t.Cleanup(func() { managedSettings = old })
	return home
}

// TestHooksOff: managed settings that turn hooks off (disableAllHooks,
// allowManagedHooksOnly), and the user's or the folder's disableAllHooks,
// are told, each by its file; settings that leave hooks on aren't.
func TestHooksOff(t *testing.T) {
	home := noHookSettings(t)
	a := load(t).(*Agent)
	dir := t.TempDir()
	if _, ok := load(t).(agent.HooksWarner); !ok {
		t.Fatal("claude is not a HooksWarner")
	}
	write := func(p, body string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, ".claude", "settings.json"), `{"disableAllHooks": false}`)
	if w := a.HooksOff(home, dir); w != "" {
		t.Fatalf("hooks on: %q", w)
	}
	write(filepath.Join(dir, ".claude", "settings.local.json"), `{"disableAllHooks": true}`)
	write(filepath.Join(home, ".claude", "settings.json"), `{"disableAllHooks": true}`)
	w := a.HooksOff(home, dir)
	if !strings.Contains(w, "~/.claude/settings.json, .claude/settings.local.json set disableAllHooks") || strings.Contains(w, "managed") {
		t.Fatalf("own settings: %q", w)
	}
	write(managedSettings[0], `{"allowManagedHooksOnly": true}`)
	if w := a.HooksOff(home, ""); !strings.Contains(w, "(allowManagedHooksOnly) turn tm's hooks off") || !strings.Contains(w, "~/.claude/settings.json set") {
		t.Fatalf("managed: %q", w)
	}
}

// TestSettingsPinHooks: tm's own settings set disableAllHooks false.
func TestSettingsPinHooks(t *testing.T) {
	l, err := load(t).Launch(pluginSpec(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(l.Files["claude-settings.json"]), `"disableAllHooks": false`) {
		t.Fatalf("settings:\n%s", l.Files["claude-settings.json"])
	}
}
