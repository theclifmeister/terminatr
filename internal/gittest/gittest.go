// Package gittest runs git in tests with a fixed identity and config, so a
// test does not depend on the machine's own git configuration.
package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// config is passed to every command: an identity, main as the initial
// branch, and no signing (a machine whose global config signs commits would
// otherwise fail the first commit).
var config = []string{
	"-c", "user.name=t",
	"-c", "user.email=t@t",
	"-c", "init.defaultBranch=main",
	"-c", "commit.gpgsign=false",
	"-c", "tag.gpgsign=false",
}

// Run runs git with args in dir and returns its trimmed combined output. A
// failure fails the test.
func Run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append(append([]string{}, config...), args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// In returns Run bound to dir.
func In(t testing.TB, dir string) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		return Run(t, dir, args...)
	}
}

// Repo makes a new repository on main with one commit and returns its
// directory and a runner for it.
func Repo(t testing.TB) (string, func(args ...string) string) {
	t.Helper()
	dir := t.TempDir()
	run := In(t, dir)
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "a")
	return dir, run
}
