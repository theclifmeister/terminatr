package thread

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
)

// TestLeftovers: what a plain folder holds that wasn't handed in.
func TestLeftovers(t *testing.T) {
	dir := t.TempDir()
	check := func(want string) {
		t.Helper()
		if got, err := Leftovers(dir); err != nil || got != want {
			t.Fatalf("Leftovers = %q, %v; want %q", got, err, want)
		}
	}
	check("")
	os.MkdirAll(filepath.Join(dir, "out"), 0o755)
	check("empty folders")
	os.WriteFile(filepath.Join(dir, "out", "a.md"), nil, 0o644)
	check("1 file not handed in")
	os.WriteFile(filepath.Join(dir, "b.md"), nil, 0o644)
	check("2 files not handed in")
	if got, err := Leftovers(filepath.Join(dir, "gone")); err != nil || got != "" {
		t.Fatalf("a missing folder: %q, %v", got, err)
	}
}

// TestPlainBrief: a thread without a repo is told it has no git, no
// branch and no PR, and how its output is kept; one with a repo isn't.
func TestPlainBrief(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := WorktreeDir("demo", "t-0001", "Notes")
	plain, _ := Create(p, Record{Title: "Notes", Worktree: wt})
	if err := OwnFolder("demo", plain); err != nil {
		t.Fatalf("its own folder: %v", err)
	}
	if err := OwnFolder("demo", &Record{ID: "t-0002", Worktree: wt}); err == nil {
		t.Fatal("another thread's folder passed")
	}
	brief, _, err := WriteBrief(p, plain, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(brief)
	for _, w := range []string{"## No git", "no git, no branch, no pull request", "tm report --attach FILE", "removes the folder\nonly if it is empty"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("brief lacks %q:\n%s", w, b)
		}
	}
	if s := ResetContext(p, plain.ID, ""); !strings.Contains(s, "a plain folder: no git") {
		t.Errorf("reset context:\n%s", s)
	}
	withRepo, _ := Create(p, Record{Title: "Fix", Repo: "/src/web", Branch: "tm/demo/t-0002-fix"})
	brief, _, _ = WriteBrief(p, withRepo, false)
	if b, _ := os.ReadFile(brief); strings.Contains(string(b), "No git") {
		t.Errorf("a repo thread's brief says no git:\n%s", b)
	}
}
