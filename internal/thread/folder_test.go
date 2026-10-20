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

// TestLoadChecksID: a record whose id isn't the one it is loaded under
// (an edited thread.toml) is refused, so nothing checked against its id
// trusts it.
func TestLoadChecksID(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := Create(p, Record{Title: "Notes"})
	path := Path(p, r.ID, "thread.toml")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(b), `id = "t-0001"`, `id = "t-0002"`, 1)), 0o644)
	if _, err := Load(p, r.ID); err == nil || !strings.Contains(err.Error(), `id "t-0002" is not t-0001`) {
		t.Fatalf("Load = %v", err)
	}
}

// TestListSkipsMismatchedID: one record whose id isn't its folder's is
// skipped, so the others still list.
func TestListSkipsMismatchedID(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Create(p, Record{Title: "A"})
	b, _ := Create(p, Record{Title: "B"})
	path := Path(p, a.ID, "thread.toml")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(data), `id = "`+a.ID+`"`, `id = "t-0099"`, 1)), 0o644)
	recs, err := List(p)
	if err != nil || len(recs) != 1 || recs[0].ID != b.ID {
		t.Fatalf("List = %v, %v", recs, err)
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
	if err := OwnFolder("demo", plain, nil); err != nil {
		t.Fatalf("its own folder: %v", err)
	}
	if err := OwnFolder("demo", &Record{ID: "t-0002", Worktree: wt}, nil); err == nil {
		t.Fatal("another thread's folder passed")
	}
	// A .git in it makes it a checkout, whatever the record says.
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /src/web/.git/worktrees/x\n"), 0o644)
	if err := OwnFolder("demo", plain, nil); err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("a folder with a .git: %v", err)
	}
	os.Remove(filepath.Join(wt, ".git"))
	// Another record naming it: not this thread's alone.
	if err := OwnFolder("demo", plain, []*Record{plain, {ID: "t-0009", Worktree: wt}}); err == nil || !strings.Contains(err.Error(), "t-0009") {
		t.Fatalf("a folder another record names: %v", err)
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
