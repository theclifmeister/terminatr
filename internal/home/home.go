// Package home locates terminatr's state directory, ~/.terminatr by
// default (docs/SPEC.md §5.1). TERMINATR_HOME moves all of it, which is
// how tests run without touching the real home.
package home

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// Env is the variable that overrides the state directory.
const Env = "TERMINATR_HOME"

// Dir returns the absolute state directory: $TERMINATR_HOME, or
// ~/.terminatr. It does not create it.
func Dir() (string, error) {
	if d := os.Getenv(Env); d != "" {
		d, err := filepath.Abs(d)
		if err == nil && testing.Testing() && IsReal(d) {
			// A tm session hands its children TERMINATR_HOME=~/.terminatr,
			// so a thread running go test inherits the live home (T472).
			panic("home: a test resolved the real state directory " + d + " through " + Env + "; set it to t.TempDir()")
		}
		return d, err
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

// IsReal reports whether dir is the account's own ~/.terminatr, the
// live state a test must never touch, whatever $HOME says.
func IsReal(dir string) bool {
	real, ok := realUserHome()
	if !ok {
		return false
	}
	want := filepath.Join(real, ".terminatr")
	if filepath.Clean(dir) == want {
		return true
	}
	a, err1 := filepath.EvalSymlinks(dir)
	b, err2 := filepath.EvalSymlinks(want)
	return err1 == nil && err2 == nil && a == b
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

// SearchDir is <home>/state/search, each project's search index in
// <slug>/ (internal/search): derived, rebuilt at will.
func SearchDir() (string, error) { return sub(filepath.Join("state", "search")) }

// ModCacheDir is <home>/cache/mods, each code mod's bundle and what its
// last host said (the mod supervisor's, never the mod's).
func ModCacheDir() (string, error) { return sub(filepath.Join("cache", "mods")) }

// ModLogsDir is <home>/logs/mods, each mod's <id>.log.
func ModLogsDir() (string, error) { return sub(filepath.Join("logs", "mods")) }

// LogsDir is <home>/logs: the server's, the service's, the consoles'
// and (in mods/) each mod's log.
func LogsDir() (string, error) { return sub("logs") }

// ThreadTempRoot is the folder that holds every thread's own temp folder
// (ThreadTempDir): /tmp/tm-<uid> by its real path, on Windows <TEMP>\tm.
// Short on purpose: Claude Code keeps its sandbox's TMPDIR in
// $CLAUDE_CODE_TMPDIR/claude-<uid> only while that is at most 44 bytes
// (AF_UNIX socket paths), and falls back to the shared /tmp/claude-<uid>
// otherwise (2.1.302), so a folder under the home would be too long.
func ThreadTempRoot() string {
	if os.Getuid() < 0 {
		return filepath.Join(os.TempDir(), "tm")
	}
	tmp := "/tmp"
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real // /private/tmp on macOS: the path sandboxes match
	}
	return filepath.Join(tmp, "tm-"+strconv.Itoa(os.Getuid()))
}

// ThreadTempDir is a thread's own temp folder (T587): its agent's
// TMPDIR, where Claude Code keeps the thread's scratchpads and task
// output, so no two sessions share one. In ThreadTempRoot, named by a
// hash of the home, the project and the thread, so every home's threads
// (tests, scratch servers) keep their own. It fails for a slug or id
// that isn't a single plain name.
func ThreadTempDir(slug, id string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return ThreadTempDirOf(d, slug, id)
}

// ThreadTempDirOf is ThreadTempDir for the state directory dir.
func ThreadTempDirOf(dir, slug, id string) (string, error) {
	for _, n := range []string{slug, id} {
		if n == "" || n == "." || n == ".." || filepath.Base(n) != n || n != filepath.Clean(n) {
			return "", errors.New("thread temp folder: bad project or thread name " + n)
		}
	}
	sum := sha256.Sum256([]byte(dir + "\x00" + slug + "\x00" + id))
	return filepath.Join(ThreadTempRoot(), hex.EncodeToString(sum[:5])), nil
}
