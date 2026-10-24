package project

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
)

// writeFront replaces PROJECT.md's front matter with front, as a
// coordinator (or an older tm) could.
func writeFront(t *testing.T, p *Project, front string) {
	t.Helper()
	if err := os.WriteFile(p.Path("PROJECT.md"), []byte("+++\n"+front+"+++\n# Instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func reposOf(t *testing.T, slug string) []string {
	t.Helper()
	p, err := Open(slug)
	if err != nil {
		t.Fatal(err)
	}
	return p.Repos
}

// TestReposInConfig (T335): the repo list is config.toml's, the human's
// file; once the lists have moved, a repos key a coordinator writes into
// PROJECT.md approves nothing.
func TestReposInConfig(t *testing.T) {
	dir := setup(t)
	a, b := t.TempDir(), t.TempDir()
	p, err := New(Options{Slug: "demo", Repos: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p.Path("PROJECT.md")); strings.Contains(string(data), "repos") {
		t.Fatalf("PROJECT.md has repos:\n%s", data)
	}
	cfg, _ := config.Load()
	if got, set := cfg.Repos("demo"); !set || !slices.Equal(got, []string{a}) {
		t.Fatalf("config.toml repos %v %v", got, set)
	}
	if _, err := MoveRepos(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "repos-moved")); err != nil {
		t.Fatal("no mark:", err)
	}
	writeFront(t, p, "goal = \"x\"\nrepos = ["+strconv.Quote(b)+"]\n")
	if got := reposOf(t, "demo"); !slices.Equal(got, []string{a}) {
		t.Fatalf("repos after PROJECT.md's edit: %v", got)
	}
	// A move again changes nothing: the mark stands.
	if moved, err := MoveRepos(time.Now()); err != nil || len(moved) != 0 {
		t.Fatalf("second move: %v %v", moved, err)
	}
	if got := reposOf(t, "demo"); !slices.Equal(got, []string{a}) {
		t.Fatalf("repos after a second move: %v", got)
	}

	// Add and remove change config.toml; the last one gone leaves an
	// empty list, not PROJECT.md's.
	if changed, err := p.SetRepo(b, true); err != nil || !changed {
		t.Fatalf("add: %v %v", changed, err)
	}
	if got := reposOf(t, "demo"); !slices.Equal(got, []string{a, b}) {
		t.Fatalf("after add: %v", got)
	}
	if changed, err := p.SetRepo(b, true); err != nil || changed {
		t.Fatalf("add again: %v %v", changed, err)
	}
	for _, r := range []string{a, b} {
		if changed, err := p.SetRepo(r, false); err != nil || !changed {
			t.Fatalf("remove %s: %v %v", r, changed, err)
		}
	}
	cfg, _ = config.Load()
	if got, set := cfg.Repos("demo"); !set || len(got) != 0 {
		t.Fatalf("emptied: %v %v", got, set)
	}
	if got := reposOf(t, "demo"); len(got) != 0 {
		t.Fatalf("after removing all: %v", got)
	}
}

// TestMoveRepos (T335): an older tm's lists move from PROJECT.md to
// config.toml once, journaled, the key leaving PROJECT.md; until then
// they count as before. A list config.toml has already wins.
func TestMoveRepos(t *testing.T) {
	setup(t)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	old, err := New(Options{Slug: "old"})
	if err != nil {
		t.Fatal(err)
	}
	writeFront(t, old, "goal = \"g\"\nrepos = ["+strconv.Quote(a)+", \"relative/path\"]\ncreated = 2026-01-02T03:04:05Z\n")
	set, err := New(Options{Slug: "set", Repos: []string{b}})
	if err != nil {
		t.Fatal(err)
	}
	writeFront(t, set, "repos = ["+strconv.Quote(c)+"]\n")
	if _, err := New(Options{Slug: "none"}); err != nil {
		t.Fatal(err)
	}

	// Before the move: PROJECT.md's list counts where config.toml has
	// none, absolute paths only.
	if got := reposOf(t, "old"); !slices.Equal(got, []string{a}) {
		t.Fatalf("old before: %v", got)
	}
	if got := reposOf(t, "set"); !slices.Equal(got, []string{b}) {
		t.Fatalf("set before: %v", got)
	}

	moved, err := MoveRepos(time.Now())
	if err != nil || !slices.Equal(moved, []string{"old"}) {
		t.Fatalf("moved %v %v", moved, err)
	}
	cfg, _ := config.Load()
	for slug, want := range map[string][]string{"old": {a}, "set": {b}} {
		if got, _ := cfg.Repos(slug); !slices.Equal(got, want) {
			t.Errorf("%s: config.toml repos %v, want %v", slug, got, want)
		}
		data, _ := os.ReadFile(filepath.Join(filepath.Dir(old.Dir), slug, "PROJECT.md"))
		if strings.Contains(string(data), "repos") {
			t.Errorf("%s: PROJECT.md keeps repos:\n%s", slug, data)
		}
	}
	if data, _ := os.ReadFile(old.Path("PROJECT.md")); !strings.Contains(string(data), `goal: "g"`) {
		t.Errorf("old: PROJECT.md lost its goal:\n%s", data)
	}
	if _, set := cfg.Repos("none"); set {
		t.Error("none: a list was written")
	}
	if j, _ := os.ReadFile(old.Path("JOURNAL.md")); !strings.Contains(string(j), "ticker project.repo.move old to config.toml: "+a) {
		t.Errorf("journal:\n%s", j)
	}

	// After it, PROJECT.md's key is never read again.
	none, _ := Open("none")
	writeFront(t, none, "repos = ["+strconv.Quote(a)+"]\n")
	if got := reposOf(t, "none"); len(got) != 0 {
		t.Fatalf("none after the move: %v", got)
	}
}

// TestTrashDropsRepos (T335): a deleted project's list goes with its
// other settings, so a new project of the slug approves nothing it
// didn't name; the journal line keeps what it was.
func TestTrashDropsRepos(t *testing.T) {
	setup(t)
	a := t.TempDir()
	p, err := New(Options{Slug: "demo", Repos: []string{a}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MoveRepos(time.Now()); err != nil {
		t.Fatal(err)
	}
	dst, err := p.Trash(human, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if j, _ := os.ReadFile(filepath.Join(dst, "JOURNAL.md")); !strings.Contains(string(j), "its repos were "+a) {
		t.Errorf("journal:\n%s", j)
	}
	cfg, _ := config.Load()
	if _, set := cfg.Repos("demo"); set {
		t.Fatal("config.toml keeps the deleted project's repos")
	}
	if _, err := New(Options{Slug: "demo"}); err != nil {
		t.Fatal(err)
	}
	if got := reposOf(t, "demo"); len(got) != 0 {
		t.Fatalf("new demo: %v", got)
	}
}

// TestMoveReposFailsClosed (T335): a list that can't move still gets the
// mark, so PROJECT.md's key, which a coordinator could rewrite, never
// counts afterwards.
func TestMoveReposFailsClosed(t *testing.T) {
	setup(t)
	a := t.TempDir()
	p, err := New(Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	writeFront(t, p, "repos = \"not a list\"\n")
	if _, err := MoveRepos(time.Now()); err == nil || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("move: %v", err)
	}
	if !reposMoved() {
		t.Fatal("no mark after a failed move")
	}
	writeFront(t, p, "repos = ["+strconv.Quote(a)+"]\n")
	if got := reposOf(t, "demo"); len(got) != 0 {
		t.Fatalf("PROJECT.md's list counts: %v", got)
	}
}
