// Package home locates terminatr's state directory, ~/.terminatr by
// default (docs/SPEC.md §5.1). TERMINATR_HOME moves all of it, which is
// how tests run without touching the real home.
package home

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// Env is the variable that overrides the state directory.
const Env = "TERMINATR_HOME"

// Dir returns the absolute state directory: $TERMINATR_HOME, or
// ~/.terminatr. It does not create it.
func Dir() (string, error) {
	if d := os.Getenv(Env); d != "" {
		return filepath.Abs(d)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if h == "" {
		return "", errors.New("no home directory; set " + Env)
	}
	d := filepath.Join(h, ".terminatr")
	if testing.Testing() {
		// A test binary that falls through to the user's real home would
		// read and write their live state (models cache, config, links).
		// Tests set TERMINATR_HOME (or HOME) to a temp dir.
		if real, ok := realUserHome(); ok && h == real {
			panic("home: a test resolved the real state directory " + d + "; set " + Env + " to t.TempDir()")
		}
	}
	return d, nil
}

// realUserHome is the account's home from the password database, which
// $HOME in a test cannot move: a test that sets HOME to a temp dir is
// isolated, one that leaves it is not.
func realUserHome() (string, bool) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		return "", false
	}
	return u.HomeDir, true
}

// ProjectsDir is <home>/projects.
func ProjectsDir() (string, error) { return sub("projects") }

// WorktreesDir is <home>/worktrees.
func WorktreesDir() (string, error) { return sub("worktrees") }

func sub(name string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

// TrashDir is <home>/.trash, where tm project delete moves a project.
func TrashDir() (string, error) { return sub(".trash") }

// AgentsDir is <home>/agents, the user's agent manifests.
func AgentsDir() (string, error) { return sub("agents") }

// ModsDir is <home>/mods, the installed user mods, one folder per mod id
// (mods design §8).
func ModsDir() (string, error) { return sub("mods") }

// ModStateDir is <home>/state/mods, each mod's own data and store.
func ModStateDir() (string, error) { return sub(filepath.Join("state", "mods")) }

// ModCacheDir is <home>/cache/mods, each code mod's bundle and what its
// last host said (the mod supervisor's, never the mod's).
func ModCacheDir() (string, error) { return sub(filepath.Join("cache", "mods")) }

// ModLogsDir is <home>/logs/mods, each mod's <id>.log.
func ModLogsDir() (string, error) { return sub(filepath.Join("logs", "mods")) }
