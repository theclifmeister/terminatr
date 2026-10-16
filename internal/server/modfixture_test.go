package server

import (
	"log"
	"os"
	"path/filepath"
	"testing"
)

// modServer is a bare Server with mod id installed from manifest (and an
// empty main.js), enabled with grants (the inside of a TOML array) for
// project demo. With builtins off the bundled Memory, Tasks and Ticker
// mods are disabled, so a test sees only mod id; with it on they keep
// their defaults.
func modServer(t *testing.T, id, manifest, grants string, builtins bool) *Server {
	t.Helper()
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": manifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[mods." + id + "]\nenabled = true\ngrants = [" + grants + "]\nprojects = [\"demo\"]\n"
	if !builtins {
		cfg += "[mods.memory-tab]\nenabled = false\n[mods.tasks-tab]\nenabled = false\n[mods.ticker]\nenabled = false\n"
	}
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	newWatchProject(t)
	return &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
}
