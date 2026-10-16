package changes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/gittest"
)

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo is a clone of a bare origin whose main has one commit, with
// origin/HEAD set: what a project's repo looks like.
func repo(t *testing.T) (root, repo string) {
	t.Helper()
	root = t.TempDir()
	origin := filepath.Join(root, "origin.git")
	repo = filepath.Join(root, "repo")
	gittest.Run(t, root, "init", "--bare", "-q", origin)
	gittest.Run(t, root, "clone", "-q", origin, repo)
	write(t, repo, "keep.txt", "a\nb\nc\n")
	write(t, repo, "old.txt", "one\ntwo\nthree\nfour\nfive\n")
	write(t, repo, "gone.txt", "x\n")
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "c")
	gittest.Run(t, repo, "push", "-q", "origin", "HEAD:main")
	gittest.Run(t, repo, "remote", "set-head", "origin", "main")
	return root, repo
}

func byPath(cs *Changeset) map[string]File {
	m := map[string]File{}
	for _, f := range cs.Files {
		m[f.Path] = f
	}
	return m
}

// TestStartedThread: a worktree started from origin/main, with a pushed
// commit, an unpushed one and uncommitted work, puts each file in its
// newest bucket, counts lines, and reads one file's patch.
func TestStartedThread(t *testing.T) {
	root, repo := repo(t)
	wt := filepath.Join(root, "wt")
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "tm/p/t-0001-x", wt, "origin/main")
	write(t, wt, "pushed.go", "package p\n")
	gittest.Run(t, wt, "add", ".")
	gittest.Run(t, wt, "commit", "-q", "-m", "pushed")
	gittest.Run(t, wt, "push", "-q", "origin", "HEAD:tm/p/t-0001-x")
	gittest.Run(t, wt, "mv", "old.txt", "renamed.txt")
	gittest.Run(t, wt, "rm", "-q", "gone.txt")
	gittest.Run(t, wt, "commit", "-q", "-m", "unpushed")
	write(t, wt, "keep.txt", "a\nB\nc\nd\n")
	write(t, wt, "new/untracked.txt", "1\n2\n3")
	write(t, wt, "bin.dat", "\x00\x01\x02")

	cs, err := Read(context.Background(), wt, Options{Repo: repo, Base: "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if cs.State != StateDirty || cs.Mode != ModeBase || cs.Branch != "tm/p/t-0001-x" || cs.Base != "origin/main" ||
		cs.Ahead != 2 || cs.Unpushed != 1 || !cs.Pushed || cs.Head == "" || cs.Truncated {
		t.Fatalf("changeset %+v", cs)
	}
	files := byPath(cs)
	want := map[string]File{
		"keep.txt":          {Path: "keep.txt", Status: "M", Add: 2, Del: 1, Bucket: Uncommitted},
		"new/untracked.txt": {Path: "new/untracked.txt", Status: "?", Add: 3, Bucket: Uncommitted},
		"bin.dat":           {Path: "bin.dat", Status: "?", Binary: true, Bucket: Uncommitted},
		"renamed.txt":       {Path: "renamed.txt", Old: "old.txt", Status: "R", Bucket: Unpushed},
		"gone.txt":          {Path: "gone.txt", Status: "D", Del: 1, Bucket: Unpushed},
		"pushed.go":         {Path: "pushed.go", Status: "A", Add: 1, Bucket: Pushed},
	}
	if len(files) != len(want) {
		t.Fatalf("files %+v", cs.Files)
	}
	for p, w := range want {
		if files[p] != w {
			t.Errorf("%s: %+v, want %+v", p, files[p], w)
		}
	}
	if cs.Totals != (Count{Files: 6, Add: 6, Del: 2}) || cs.Buckets.Uncommitted != (Count{3, 5, 1}) ||
		cs.Buckets.Unpushed != (Count{2, 0, 1}) || cs.Buckets.Pushed != (Count{1, 1, 0}) {
		t.Fatalf("totals %+v buckets %+v", cs.Totals, cs.Buckets)
	}

	// Uncommitted: against HEAD only.
	cs, err = Read(context.Background(), wt, Options{Repo: repo, Base: "origin/main", Mode: ModeUncommitted, Path: "keep.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Files) != 3 || cs.Totals != (Count{3, 5, 1}) || cs.Ahead != 2 {
		t.Fatalf("uncommitted %+v", cs)
	}
	if !strings.Contains(cs.Patch, "-b\n+B\n c\n+d\n") || strings.Contains(cs.Patch, "\x1b") || cs.PatchTruncated {
		t.Fatalf("patch %q", cs.Patch)
	}
	// An untracked file's patch adds every line.
	cs, _ = Read(context.Background(), wt, Options{Repo: repo, Path: "new/untracked.txt"})
	if !strings.Contains(cs.Patch, "+1\n+2\n+3") {
		t.Fatalf("untracked patch %q", cs.Patch)
	}
	// A renamed file's patch shows the rename.
	cs, _ = Read(context.Background(), wt, Options{Repo: repo, Path: "renamed.txt"})
	if !strings.Contains(cs.Patch, "rename from old.txt") {
		t.Fatalf("rename patch %q", cs.Patch)
	}
	if _, err := Read(context.Background(), wt, Options{Repo: repo, Mode: "all"}); err != ErrMode {
		t.Fatalf("bad mode: %v", err)
	}
}

// TestCustomBase: a thread started with --base counts from that base,
// and a branch never pushed has all its commits unpushed.
func TestCustomBase(t *testing.T) {
	root, repo := repo(t)
	gittest.Run(t, repo, "checkout", "-q", "-b", "develop")
	write(t, repo, "dev.txt", "d\n")
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "dev")
	gittest.Run(t, repo, "push", "-q", "origin", "develop")
	gittest.Run(t, repo, "checkout", "-q", "main")
	wt := filepath.Join(root, "wt")
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "tm/p/t-0002-y", wt, "origin/develop")
	write(t, wt, "y.txt", "y\n")
	gittest.Run(t, wt, "add", ".")
	gittest.Run(t, wt, "commit", "-q", "-m", "y")

	cs, err := Read(context.Background(), wt, Options{Repo: repo, Base: "origin/develop"})
	if err != nil {
		t.Fatal(err)
	}
	if cs.State != StateClean || cs.Base != "origin/develop" || cs.Ahead != 1 || cs.Unpushed != 1 || cs.Pushed ||
		len(cs.Files) != 1 || cs.Files[0].Path != "y.txt" || cs.Files[0].Bucket != Unpushed {
		t.Fatalf("changeset %+v", cs)
	}
	// Against origin's default branch the base's own commit shows too.
	cs, _ = Read(context.Background(), wt, Options{Repo: repo})
	if cs.Base != "origin/main" || cs.Ahead != 2 || len(cs.Files) != 2 {
		t.Fatalf("default base %+v", cs)
	}
}

// TestAdopted: a thread adopted in a repository's main checkout, with
// no base recorded and no origin, counts from the checked-out branch:
// its uncommitted work only.
func TestAdopted(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")
	write(t, dir, "a.txt", "a\n")
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-q", "-m", "c")
	write(t, dir, "a.txt", "a\nb\n")
	cs, err := Read(context.Background(), dir, Options{Repo: dir})
	if err != nil {
		t.Fatal(err)
	}
	if cs.State != StateDirty || cs.Branch != "main" || cs.Base != "main" || cs.Ahead != 0 || cs.Pushed ||
		len(cs.Files) != 1 || cs.Files[0] != (File{Path: "a.txt", Status: "M", Add: 1, Bucket: Uncommitted}) {
		t.Fatalf("changeset %+v", cs)
	}
}

// TestNone: a thread without a repo, a missing worktree, and a folder
// inside another repository have no changes.
func TestNone(t *testing.T) {
	_, repo := repo(t)
	sub := filepath.Join(repo, "sub")
	os.MkdirAll(sub, 0o755)
	for _, c := range []struct{ dir, repo string }{
		{t.TempDir(), ""},
		{filepath.Join(t.TempDir(), "gone"), repo},
		{sub, repo},
	} {
		cs, err := Read(context.Background(), c.dir, Options{Repo: c.repo})
		if err != nil || cs.State != StateNone || len(cs.Files) != 0 || cs.Files == nil {
			t.Errorf("%s: %+v %v", c.dir, cs, err)
		}
	}
}

// TestUnborn: before the first commit every file is uncommitted.
func TestUnborn(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")
	write(t, dir, "staged.txt", "s\n")
	gittest.Run(t, dir, "add", ".")
	write(t, dir, "loose.txt", "l\nm\n")
	cs, err := Read(context.Background(), dir, Options{Repo: dir})
	if err != nil {
		t.Fatal(err)
	}
	f := byPath(cs)
	if cs.Head != "" || cs.State != StateDirty || f["staged.txt"].Status != "A" || f["staged.txt"].Add != 1 ||
		f["loose.txt"].Status != "?" || f["loose.txt"].Add != 2 {
		t.Fatalf("changeset %+v", cs)
	}
}

// TestCleanedPaths: a file name with an escape sequence is listed
// without it.
func TestCleanedPaths(t *testing.T) {
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-q")
	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "c")
	write(t, dir, "a\x1b[31mred.txt", "x\n")
	cs, err := Read(context.Background(), dir, Options{Repo: dir})
	if err != nil || len(cs.Files) != 1 || cs.Files[0].Path != "a[31mred.txt" {
		t.Fatalf("%+v %v", cs, err)
	}
}

// TestFingerprint: the fingerprint moves with an edit, a commit and a
// push, and stays put otherwise; none without a repo.
func TestFingerprint(t *testing.T) {
	root, repo := repo(t)
	wt := filepath.Join(root, "wt")
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "tm/p/t-0001-x", wt, "origin/main")
	ctx := context.Background()
	seen := map[string]bool{}
	step := func(name string) {
		t.Helper()
		fp := Fingerprint(ctx, wt, repo)
		if fp == "" || seen[fp] {
			t.Fatalf("%s: fingerprint %q seen before", name, fp)
		}
		if again := Fingerprint(ctx, wt, repo); again != fp {
			t.Fatalf("%s: unstable %q %q", name, fp, again)
		}
		seen[fp] = true
	}
	step("clean")
	write(t, wt, "a.txt", "a\n")
	step("untracked")
	write(t, wt, "a.txt", "a\nb\n")
	step("edited")
	gittest.Run(t, wt, "add", ".")
	gittest.Run(t, wt, "commit", "-q", "-m", "a")
	step("committed")
	write(t, wt, "a.txt", "a\nb\nc\n")
	step("modified")
	write(t, wt, "a.txt", "a\nb\nc\nd\n")
	step("modified again")
	gittest.Run(t, wt, "mv", "a.txt", "b.txt")
	step("renamed")
	gittest.Run(t, wt, "commit", "-q", "-am", "b")
	step("committed again")
	gittest.Run(t, wt, "push", "-q", "origin", "HEAD:tm/p/t-0001-x")
	step("pushed")
	if Fingerprint(ctx, wt, "") != "" || Fingerprint(ctx, filepath.Join(root, "gone"), repo) != "" {
		t.Fatal("a fingerprint without a repo or worktree")
	}
}
