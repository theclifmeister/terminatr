package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/theclifmeister/terminatr/internal/gittest"
)

func noShellOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell command filter")
	}
}

// planted is a clean filter that leaves a mark when it runs, and the mark's
// check, which forgets it. A test needs a same-size edit, which git hashes
// through the filter to tell, and a control: the filter running for plain git.
func planted(t *testing.T) (clean string, ran func() bool) {
	t.Helper()
	noShellOnWindows(t)
	marker := filepath.Join(t.TempDir(), "ran")
	return "touch " + marker + "; cat", func() bool {
		_, err := os.Stat(marker)
		os.Remove(marker)
		return err == nil
	}
}

// plainGit is a git run the way nothing in tm runs it, to prove what was
// planted runs when not stopped: without it a pass would mean nothing.
func plainGit(dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Run()
}

// TestPinnedRunRepoFilter: a clean filter in the repository's own config
// doesn't run for a pinned read.
func TestPinnedRunRepoFilter(t *testing.T) {
	repo, wt := worktreeOf(t)
	clean, ran := planted(t)
	os.WriteFile(filepath.Join(wt, ".gitattributes"), []byte("* filter=evil\n"), 0o644)
	gittest.Run(t, repo, "config", "filter.evil.clean", clean)
	// The same size: git hashes it, through the filter, to tell.
	os.WriteFile(filepath.Join(wt, "a"), []byte("b"), 0o644)
	if plainGit(wt, "status", "--porcelain"); !ran() {
		t.Fatal("control: the filter didn't run for a plain status")
	}
	tr, err := Pin(bg, repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"status", "--porcelain"},
		{"status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all"},
		{"diff", "--no-ext-diff", "--no-textconv", "--numstat", "HEAD"},
	} {
		if _, err := Run(bg, "", Opts{Tree: tr}, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
		if ran() {
			t.Errorf("%v ran the repo's filter", args)
		}
	}
}

// TestPinnedRunSubmoduleFilter: a submodule's folder is the agent's, its
// .git/config too, and git asks a dirty one for its status.
func TestPinnedRunSubmoduleFilter(t *testing.T) {
	repo, wt := worktreeOf(t)
	clean, ran := planted(t)
	sub := filepath.Join(wt, "sub")
	os.MkdirAll(sub, 0o755)
	g := gittest.In(t, sub)
	g("init", "-q")
	g("config", "user.email", "a@b.c")
	g("config", "user.name", "a")
	os.WriteFile(filepath.Join(sub, ".gitattributes"), []byte("* filter=evil\n"), 0o644)
	os.WriteFile(filepath.Join(sub, "f"), []byte("x\n"), 0o644)
	g("add", ".")
	g("commit", "-qm", "s")
	head := g("rev-parse", "HEAD")
	gittest.Run(t, wt, "update-index", "--add", "--cacheinfo", "160000,"+head+",sub")
	g("config", "filter.evil.clean", clean)
	os.WriteFile(filepath.Join(sub, "f"), []byte("y\n"), 0o644)
	if plainGit(wt, "status", "--porcelain"); !ran() {
		t.Fatal("control: the submodule's filter didn't run for a plain status")
	}
	tr, err := Pin(bg, repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all"},
		{"diff", "--no-ext-diff", "--no-textconv", "--numstat", "HEAD"},
	} {
		os.WriteFile(filepath.Join(sub, "f"), []byte("z\n"), 0o644)
		if _, err := Run(bg, "", Opts{Tree: tr}, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
		if ran() {
			t.Errorf("%v ran the submodule's filter", args)
		}
	}
}

// TestPinnedRunFetchesNothing: a missing object in a partial clone doesn't
// make a read start the transport the repository's config names. A
// checkout still may.
func TestPinnedRunFetchesNothing(t *testing.T) {
	noShellOnWindows(t)
	repo, wt := worktreeOf(t)
	marker := filepath.Join(t.TempDir(), "ran")
	gittest.Run(t, repo, "config", "core.repositoryformatversion", "1")
	gittest.Run(t, repo, "config", "extensions.partialClone", "origin")
	gittest.Run(t, repo, "config", "remote.origin.url", "ssh://example.invalid/r")
	gittest.Run(t, repo, "config", "remote.origin.promisor", "true")
	gittest.Run(t, repo, "config", "core.sshCommand", "touch "+marker+"; false")
	blob := gittest.Run(t, wt, "rev-parse", "HEAD:a")
	if err := os.Remove(filepath.Join(repo, ".git", "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	// A different file against a blob that's missing: only its text tells
	// the lines.
	os.WriteFile(filepath.Join(wt, "a"), []byte("other\n"), 0o644)
	if plainGit(wt, "diff", "--numstat", "HEAD"); !fileExists(marker) {
		t.Fatal("control: a plain diff didn't start the transport")
	}
	os.Remove(marker)
	tr, err := Pin(bg, repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	Run(bg, "", Opts{Tree: tr}, "diff", "--numstat", "HEAD")
	if fileExists(marker) {
		t.Fatal("a pinned read started the transport")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// TestRepoFilterScope: a read turns off what the repository's own config
// names, however spelled, and what it includes, and leaves the user's own
// filters; a required one doesn't fail the read.
func TestRepoFilterScope(t *testing.T) {
	repo, wt := worktreeOf(t)
	clean, ran := planted(t)
	inc := filepath.Join(t.TempDir(), "inc")
	os.WriteFile(inc, []byte("[filter \"in=cluded.x\"]\n\tclean = "+clean+"\n\trequired = true\n"), 0o644)
	gittest.Run(t, repo, "config", "include.path", inc)
	global := filepath.Join(t.TempDir(), "gitconfig")
	userClean, userRan := planted(t)
	os.WriteFile(global, []byte("[filter \"user\"]\n\tclean = "+userClean+"\n"), 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	tr, err := Pin(bg, repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.filters) != 1 || tr.filters[0] != "in=cluded.x" {
		t.Fatalf("filters %q", tr.filters)
	}
	for _, f := range []string{"in=cluded.x", "user"} {
		os.WriteFile(filepath.Join(wt, ".gitattributes"), []byte("a filter="+f+"\n"), 0o644)
		os.WriteFile(filepath.Join(wt, "a"), []byte("b"), 0o644)
		if _, err := Run(bg, "", Opts{Tree: tr}, "status", "--porcelain"); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		os.WriteFile(filepath.Join(wt, "a"), []byte("a"), 0o644)
	}
	if ran() {
		t.Error("a filter of the repo's include ran")
	}
	if !userRan() {
		t.Error("the user's own filter didn't run")
	}
}
