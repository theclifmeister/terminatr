package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/gitx"
)

func TestCreateRemove(t *testing.T) {
	root := t.TempDir()
	origin, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "repo")
	gittest.Run(t, root, "init", "--bare", "-q", origin)
	gittest.Run(t, root, "clone", "-q", origin, repo)
	os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644)
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "c")
	gittest.Run(t, repo, "push", "-q", "origin", "HEAD:main")
	gittest.Run(t, repo, "remote", "set-head", "origin", "main")

	dir := filepath.Join(root, "wt", "t-0001-x")
	base, err := Create(repo, dir, "tm/demo/t-0001-x", "")
	if err != nil || base != "origin/main" {
		t.Fatalf("create: %q %v", base, err)
	}
	if !BranchExists(repo, "tm/demo/t-0001-x") {
		t.Fatal("no branch")
	}
	if d, err := GitDirs(dir); err != nil || filepath.Base(d.CommonDir) != ".git" ||
		d.GitDir != filepath.Join(d.CommonDir, "worktrees", filepath.Base(dir)) || d.RepoRoot != filepath.Dir(d.CommonDir) {
		t.Fatalf("git dirs %+v %v", d, err)
	}
	if d, err := GitDirs(repo); err != nil || d.GitDir != d.CommonDir {
		t.Fatalf("main checkout git dirs %+v %v", d, err)
	}
	os.WriteFile(filepath.Join(dir, "dirty"), []byte("x"), 0o644)
	if err := Remove(repo, dir); !errors.Is(err, ErrDirty) {
		t.Fatalf("dirty remove: %v", err)
	}
	os.Remove(filepath.Join(dir, "dirty"))
	if err := Remove(repo, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("worktree still there")
	}
	// Restore brings it back on its branch; removal by hand is pruned.
	if err := Restore(repo, dir, "tm/demo/t-0001-x"); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dir)
	if err := Remove(repo, dir); err != nil {
		t.Fatalf("remove after rm -rf: %v", err)
	}
	if PRState(repo, "tm/demo/t-0001-x") == "MERGED" {
		t.Fatal("a local repo has no merged PR")
	}
}

func TestNoOrigin(t *testing.T) {
	repo := t.TempDir()
	gittest.Run(t, repo, "init", "-q")
	gittest.Run(t, repo, "commit", "-q", "--allow-empty", "-m", "c")
	if b, err := DefaultBase(repo); err != nil || b != "HEAD" {
		t.Fatalf("%q %v", b, err)
	}
}

// TestUnsaved: uncommitted changes and commits no remote has are
// unsaved; a pushed commit, or one the merged PR's head had, is not.
func TestUnsaved(t *testing.T) {
	root := t.TempDir()
	origin, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "repo")
	gittest.Run(t, root, "init", "--bare", "-q", origin)
	gittest.Run(t, root, "clone", "-q", origin, repo)
	os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644)
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "c")
	gittest.Run(t, repo, "push", "-q", "origin", "HEAD:main")
	gittest.Run(t, repo, "fetch", "-q", "origin")
	dir := filepath.Join(root, "wt")
	if _, err := Create(repo, dir, "tm/x", "origin/main"); err != nil {
		t.Fatal(err)
	}
	check := func(pushed, want string) {
		t.Helper()
		if got, err := Unsaved(repo, dir, pushed); err != nil || got != want {
			t.Fatalf("unsaved %q %v, want %q", got, err, want)
		}
	}
	check("", "")
	os.WriteFile(filepath.Join(dir, "g"), []byte("y"), 0o644)
	check("", "uncommitted changes")
	gittest.Run(t, dir, "add", ".")
	gittest.Run(t, dir, "commit", "-q", "-m", "g")
	check("", "1 unpushed commit")
	gittest.Run(t, dir, "push", "-q", "origin", "tm/x")
	check("", "")
	// The merge deleted the remote branch: its head still counts.
	head := revParse(t, dir)
	gittest.Run(t, dir, "push", "-q", "origin", ":tm/x")
	gittest.Run(t, dir, "fetch", "-q", "--prune", "origin")
	check("", "1 unpushed commit")
	check(head, "")
	check("0123456789012345678901234567890123456789", "1 unpushed commit")
	os.RemoveAll(dir)
	check("", "")
}

func revParse(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:40])
}

// TestMergedBase: a branch whose commits are all on origin's default
// branch, or on the checked-out branch of a repo without origin, is
// merged; a squash-merged or unmerged one is not.
func TestMergedBase(t *testing.T) {
	commit := func(dir, name string) {
		t.Helper()
		os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644)
		gittest.Run(t, dir, "add", ".")
		gittest.Run(t, dir, "commit", "-q", "-m", name)
	}
	check := func(repo, branch, want string) {
		t.Helper()
		if got := MergedBase(repo, branch); got != want {
			t.Fatalf("MergedBase(%s) = %q, want %q", branch, got, want)
		}
	}

	// No remote: fast-forward, merge commit, squash, unmerged.
	repo := t.TempDir()
	gittest.Run(t, repo, "init", "-q")
	commit(repo, "a")
	for _, b := range []string{"ff", "merge", "squash", "open"} {
		gittest.Run(t, repo, "checkout", "-q", "-b", "tm/x/"+b, "main")
		commit(repo, b)
	}
	gittest.Run(t, repo, "checkout", "-q", "main")
	gittest.Run(t, repo, "merge", "-q", "--ff-only", "tm/x/ff")
	gittest.Run(t, repo, "merge", "-q", "--no-ff", "-m", "m", "tm/x/merge")
	gittest.Run(t, repo, "merge", "-q", "--squash", "tm/x/squash")
	gittest.Run(t, repo, "commit", "-q", "-m", "squash")
	check(repo, "tm/x/ff", "main")
	check(repo, "tm/x/merge", "main")
	check(repo, "tm/x/squash", "")
	check(repo, "tm/x/open", "")
	// The base is the checked-out branch, which isn't its own base.
	gittest.Run(t, repo, "checkout", "-q", "tm/x/open")
	check(repo, "tm/x/open", "")
	check(repo, "tm/x/ff", "")

	// With origin: merged on the remote (as last fetched) while the local
	// main is stale; a merge nobody fetched yet is not seen.
	root := t.TempDir()
	origin, clone, other := filepath.Join(root, "origin.git"), filepath.Join(root, "repo"), filepath.Join(root, "other")
	gittest.Run(t, root, "init", "--bare", "-q", origin)
	gittest.Run(t, root, "clone", "-q", origin, clone)
	commit(clone, "a")
	gittest.Run(t, clone, "push", "-q", "origin", "HEAD:main")
	gittest.Run(t, clone, "remote", "set-head", "origin", "main")
	gittest.Run(t, clone, "checkout", "-q", "-b", "tm/x/pr")
	commit(clone, "pr")
	gittest.Run(t, clone, "push", "-q", "origin", "tm/x/pr")
	gittest.Run(t, clone, "checkout", "-q", "main")
	gittest.Run(t, root, "clone", "-q", origin, other)
	gittest.Run(t, other, "merge", "-q", "--no-ff", "-m", "Merge PR", "origin/tm/x/pr")
	gittest.Run(t, other, "push", "-q", "origin", "main")
	check(clone, "tm/x/pr", "")
	gittest.Run(t, clone, "fetch", "-q", "origin")
	check(clone, "tm/x/pr", "origin/main")
}

func TestMergeCommit(t *testing.T) {
	repo := t.TempDir()
	gittest.Run(t, repo, "init", "-q")
	commit := func(name string) {
		t.Helper()
		os.WriteFile(filepath.Join(repo, name), []byte(name), 0o644)
		gittest.Run(t, repo, "add", ".")
		gittest.Run(t, repo, "commit", "-q", "-m", name)
	}
	commit("a")
	for _, n := range []string{"61", "610"} {
		gittest.Run(t, repo, "checkout", "-q", "-b", "pr"+n, "main")
		commit("f" + n)
		gittest.Run(t, repo, "checkout", "-q", "main")
		gittest.Run(t, repo, "merge", "-q", "--no-ff", "-m", "Merge pull request #"+n+" from o/pr"+n, "pr"+n)
	}
	m61, m610 := MergeCommit(repo, 61), MergeCommit(repo, 610)
	if m61 == "" || m610 == "" || m61 == m610 || MergeCommit(repo, 6) != "" || MergeCommit(repo, 0) != "" {
		t.Fatalf("MergeCommit: %q %q", m61, m610)
	}
}

func TestValidBase(t *testing.T) {
	for _, b := range []string{"main", "origin/main", "v1.2.3", "feature/x", "0123456789abcdef0123456789abcdef01234567"} {
		if !ValidBase(b) {
			t.Errorf("ValidBase(%q) = false", b)
		}
	}
	for _, b := range []string{"", "-x", "--force", "--orphan", "a b", "a\nb", "a..b", "a~1", "x:y", "a[b", "@{u}"} {
		if ValidBase(b) {
			t.Errorf("ValidBase(%q) = true", b)
		}
	}
}

// TestRestoreBranchNoOption: a branch that reads as an option is
// refused before git runs, and the branch it would have reset keeps
// its commits; a real one is checked out again.
func TestRestoreBranchNoOption(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	gittest.Run(t, root, "init", "-q", repo)
	gittest.Run(t, repo, "commit", "-q", "--allow-empty", "-m", "a")
	gittest.Run(t, repo, "branch", "keep")
	gittest.Run(t, repo, "checkout", "-q", "keep")
	gittest.Run(t, repo, "commit", "-q", "--allow-empty", "-m", "b")
	tip := gittest.Run(t, repo, "rev-parse", "keep")
	gittest.Run(t, repo, "checkout", "-q", "-")
	if err := Restore(repo, filepath.Join(root, "wt"), "-Bkeep"); err == nil {
		t.Fatal("option branch restored")
	}
	if got := gittest.Run(t, repo, "rev-parse", "keep"); got != tip {
		t.Fatalf("keep moved: %s, was %s", got, tip)
	}
	if err := Restore(repo, filepath.Join(root, "wt"), "keep"); err != nil {
		t.Fatal(err)
	}
}

// TestPlantedGitDir: a worktree whose .git its agent replaced with a
// repository of its own, set to run a file system monitor, is not
// read, nor removed, and the monitor never runs.
func TestPlantedGitDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell command monitor")
	}
	repo, _ := gittest.Repo(t)
	dir := filepath.Join(t.TempDir(), "wt")
	if _, err := Create(repo, dir, "tm/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)
	if dirty, err := Dirty(repo, dir); err != nil || !dirty {
		t.Fatalf("before: %v %v", dirty, err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	os.Remove(filepath.Join(dir, ".git"))
	gittest.Run(t, dir, "init", "-q")
	gittest.Run(t, dir, "config", "core.fsmonitor", "touch "+marker)
	if _, err := Unsaved(repo, dir, ""); !errors.Is(err, gitx.ErrNotPinned) {
		t.Errorf("unsaved: %v", err)
	}
	if _, err := Dirty(repo, dir); !errors.Is(err, gitx.ErrNotPinned) {
		t.Errorf("dirty: %v", err)
	}
	if err := Remove(repo, dir); !errors.Is(err, gitx.ErrNotPinned) {
		t.Errorf("remove: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("removed: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the planted monitor ran")
	}
}

// TestRemoveRedirectedCommonDir: git worktree remove checks the
// worktree for changes with git status; a common dir the worktree's
// own git dir was made to name, set to run a monitor, is not read.
func TestRemoveRedirectedCommonDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell command monitor")
	}
	repo, _ := gittest.Repo(t)
	dir := filepath.Join(t.TempDir(), "wt")
	if _, err := Create(repo, dir, "tm/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	d, err := GitDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	evil := filepath.Join(t.TempDir(), "evil.git")
	gittest.Run(t, repo, "clone", "-q", "--bare", repo, evil)
	gittest.Run(t, evil, "config", "core.fsmonitor", "touch "+marker)
	os.WriteFile(filepath.Join(d.GitDir, "commondir"), []byte(evil+"\n"), 0o644)
	if err := Remove(repo, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the planted monitor ran")
	}
}

// TestDirtySubmodule: a submodule's changes count, whatever the
// worktree's .gitmodules says to ignore.
func TestDirtySubmodule(t *testing.T) {
	sub, _ := gittest.Repo(t)
	repo, _ := gittest.Repo(t)
	dir := filepath.Join(t.TempDir(), "wt")
	if _, err := Create(repo, dir, "tm/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	run := gittest.In(t, dir)
	run("-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	run("config", "--file", ".gitmodules", "submodule.sub.ignore", "all")
	run("add", ".gitmodules")
	run("commit", "-q", "-m", "sub")
	if dirty, err := Dirty(repo, dir); err != nil || dirty {
		t.Fatalf("clean: %v %v", dirty, err)
	}
	os.WriteFile(filepath.Join(dir, "sub", "a"), []byte("changed"), 0o644)
	if out := run("status", "--porcelain"); out != "" {
		t.Logf("plain status sees it anyway: %q", out)
	}
	if dirty, err := Dirty(repo, dir); err != nil || !dirty {
		t.Fatalf("dirty submodule: %v %v", dirty, err)
	}
	if err := Remove(repo, dir); !errors.Is(err, ErrDirty) {
		t.Fatalf("remove: %v", err)
	}
}

// TestRemoveWorktreeConfig:with extensions.worktreeConfig on, a clean
// filter set in the worktree's own config.worktree never runs as it is
// checked for changes and removed; a dirty one is still kept.
func TestRemoveWorktreeConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell command filter")
	}
	repo, run := gittest.Repo(t)
	run("config", "extensions.worktreeConfig", "true")
	dir := filepath.Join(t.TempDir(), "wt")
	if _, err := Create(repo, dir, "tm/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	d, err := GitDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	attrs := filepath.Join(t.TempDir(), "attributes")
	os.WriteFile(attrs, []byte("* filter=x\n"), 0o644)
	cfg := "[core]\n\tattributesFile = " + attrs + "\n[filter \"x\"]\n\tclean = \"touch " + marker + "; cat\"\n"
	os.WriteFile(filepath.Join(d.GitDir, "config.worktree"), []byte(cfg), 0o644)
	// Rewritten in place at the same size: git hashes it to tell.
	os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)
	if err := Remove(repo, dir); !errors.Is(err, ErrDirty) {
		t.Fatalf("dirty: %v", err)
	}
	os.Remove(filepath.Join(dir, "f"))
	if err := Remove(repo, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("kept: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the planted filter ran")
	}
}

// TestCreateStartsNothing: adding a worktree checks it out but runs
// none of the repo's hooks (worktree add's own gits run in a git dir a
// running thread may write).
func TestCreateStartsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks")
	}
	repo, _ := gittest.Repo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	for _, h := range []string{"post-checkout", "reference-transaction"} {
		os.WriteFile(filepath.Join(repo, ".git", "hooks", h), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	}
	dir := filepath.Join(t.TempDir(), "wt")
	if _, err := Create(repo, dir, "tm/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "a")); err != nil || string(b) != "a" {
		t.Fatalf("not checked out: %q %v", b, err)
	}
	if dirty, err := Dirty(repo, dir); err != nil || dirty {
		t.Fatalf("dirty after create: %v %v", dirty, err)
	}
	if got := gittest.Run(t, dir, "symbolic-ref", "HEAD"); got != "refs/heads/tm/x" {
		t.Fatalf("HEAD %q", got)
	}
	if err := Remove(repo, dir); err != nil {
		t.Fatal(err)
	}
	if err := Restore(repo, dir, "tm/x"); err != nil {
		t.Fatal(err)
	}
	if dirty, err := Dirty(repo, dir); err != nil || dirty {
		t.Fatalf("dirty after restore: %v %v", dirty, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a hook ran")
	}
}
