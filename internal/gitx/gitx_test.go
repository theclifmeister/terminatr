package gitx

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/gittest"
)

var bg = context.Background()

// TestRunError: a failed git says its verb and git's last line of
// stderr, cleaned, and its exit code.
func TestRunError(t *testing.T) {
	repo, _ := gittest.Repo(t)
	_, err := Run(bg, repo, Opts{}, "-c", "x.y=z", "rev-parse", "--verify", "no-such-ref\x1b[31m")
	var e *Error
	if !errors.As(err, &e) || e.Verb != "rev-parse" || e.Code != 128 || ExitCode(err) != 128 {
		t.Fatalf("%#v", err)
	}
	if msg := err.Error(); !strings.HasPrefix(msg, "git rev-parse: ") || strings.ContainsAny(msg, "\x1b\n") {
		t.Fatalf("%q", msg)
	}
	if out, err := Run(bg, repo, Opts{OK: []int{1}}, "diff", "--no-index", "--quiet", os.DevNull, "a"); err != nil || out != "" {
		t.Fatalf("an OK exit code: %q %v", out, err)
	}
}

// TestEnv: every run has the base environment, and none of the
// variables that would tell git a repository other than its folder's.
func TestEnv(t *testing.T) {
	repo, _ := gittest.Repo(t)
	t.Setenv("GIT_DIR", t.TempDir())
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	top, err := Run(bg, repo, Opts{}, "rev-parse", "--show-toplevel")
	if err != nil || !same(t, top, repo) {
		t.Fatalf("toplevel %q %v, want %s", top, err, repo)
	}
	env := strings.Join(Env(), "\n")
	for _, kv := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"} {
		if !strings.Contains(env, kv) {
			t.Errorf("no %s", kv)
		}
	}
	if strings.Contains(env, "GIT_DIR=") || strings.Contains(env, "GIT_WORK_TREE=") {
		t.Error("a repository location got through")
	}
	if cfg, err := Run(bg, repo, Opts{Config: []string{"x.y=z"}}, "config", "x.y"); err != nil || cfg != "z" {
		t.Errorf("config %q %v", cfg, err)
	}
}

// TestOutputCap: Output cuts at Max and says so; Run refuses it.
func TestOutputCap(t *testing.T) {
	repo, _ := gittest.Repo(t)
	out, cut, err := Output(bg, repo, Opts{Max: 5}, "log", "--format=%H")
	if err != nil || !cut || len(out) != 5 {
		t.Fatalf("%q %v %v", out, cut, err)
	}
	if _, err := Run(bg, repo, Opts{Max: 5}, "log", "--format=%H"); !errors.Is(err, ErrTooLong) {
		t.Fatalf("run: %v", err)
	}
}

// TestTimeout: a git that hangs is given up on.
func TestTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script git")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	start := time.Now()
	if _, err := Run(bg, t.TempDir(), Opts{Timeout: 100 * time.Millisecond}, "log"); err == nil || !strings.Contains(err.Error(), "took over") {
		t.Fatalf("%v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %s", took)
	}
}

func TestCheckRefFormat(t *testing.T) {
	for name, want := range map[string]bool{"main": true, "tm/x/t-1": true, "a..b": false, "-x": false, "a b": false} {
		if ok, err := CheckRefFormat(name, "--branch"); err != nil || ok != want {
			t.Errorf("%q: %v %v, want %v", name, ok, err, want)
		}
	}
	if ok, _ := CheckRefFormat("main"); ok {
		t.Error("a one-level ref without --allow-onelevel")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := CheckRefFormat("main", "--branch"); err == nil {
		t.Error("no git: no error")
	}
}

// worktreeOf adds a linked worktree of a new repo and returns both.
func worktreeOf(t *testing.T) (repo, wt string) {
	t.Helper()
	repo, run := gittest.Repo(t)
	wt = filepath.Join(t.TempDir(), "wt")
	run("worktree", "add", "-q", "-b", "tm/x", wt)
	return repo, wt
}

func TestPin(t *testing.T) {
	repo, wt := worktreeOf(t)
	tr, err := Pin(bg, repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(tr.GitDir) != "wt" || filepath.Dir(tr.GitDir) != filepath.Join(tr.CommonDir, "worktrees") || !same(t, tr.Dir, wt) {
		t.Fatalf("%+v", tr)
	}
	if b, err := Run(bg, "", Opts{Tree: tr}, "symbolic-ref", "--short", "HEAD"); err != nil || b != "tm/x" {
		t.Fatalf("pinned run: %q %v", b, err)
	}
	m, err := Pin(bg, repo, repo)
	if err != nil || m.GitDir != m.CommonDir || !same(t, m.Dir, repo) {
		t.Fatalf("main checkout: %+v %v", m, err)
	}
	other, _ := gittest.Repo(t)
	if _, err := Pin(bg, other, wt); !errors.Is(err, ErrNotPinned) {
		t.Errorf("another repo's worktree: %v", err)
	}
	if _, err := Pin(bg, repo, filepath.Join(wt, "nope")); !errors.Is(err, ErrNotPinned) {
		t.Errorf("a missing folder: %v", err)
	}
}

// TestPinRefuses: a worktree whose .git an agent rewrote is no
// worktree of the repo: a git dir of its own, a .git file naming
// another folder, or another worktree's git dir, which doesn't name it
// back.
func TestPinRefuses(t *testing.T) {
	repo, wt := worktreeOf(t)
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "tm/y", filepath.Join(t.TempDir(), "other"))
	tr, err := Pin(bg, repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	dotgit := filepath.Join(wt, ".git")
	for name, plant := range map[string]func(){
		"own git dir": func() { os.Remove(dotgit); gittest.Run(t, wt, "init", "-q") },
		"other folder": func() {
			os.WriteFile(dotgit, []byte("gitdir: "+t.TempDir()+"\n"), 0o644)
		},
		"another worktree's git dir": func() {
			os.WriteFile(dotgit, []byte("gitdir: "+filepath.Join(tr.CommonDir, "worktrees", "other")+"\n"), 0o644)
		},
		"a nested worktrees folder": func() {
			fake := filepath.Join(wt, "x", "worktrees", "wt")
			os.MkdirAll(fake, 0o755)
			os.WriteFile(filepath.Join(fake, "gitdir"), []byte(dotgit+"\n"), 0o644)
			os.WriteFile(dotgit, []byte("gitdir: "+fake+"\n"), 0o644)
		},
		"a symlink": func() {
			os.Remove(dotgit)
			os.Symlink(tr.GitDir, dotgit)
		},
	} {
		t.Run(name, func(t *testing.T) {
			os.RemoveAll(dotgit)
			os.WriteFile(dotgit, []byte("gitdir: "+tr.GitDir+"\n"), 0o644)
			plant()
			if _, err := Pin(bg, repo, wt); !errors.Is(err, ErrNotPinned) {
				t.Fatalf("pinned: %v", err)
			}
		})
	}
}

// TestPinnedRunStartsNothing: configuration an agent planted for its
// worktree never makes a pinned run start a program: a file system
// monitor in a git dir that replaced the worktree's .git (Pin refuses
// it), or in a common dir the worktree's own git dir now names, which
// the run overrules, or set from that git dir's config.worktree.
func TestPinnedRunStartsNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell command monitor")
	}
	marker := filepath.Join(t.TempDir(), "ran")
	monitor := "touch " + marker
	ran := func() bool {
		_, err := os.Stat(marker)
		os.Remove(marker)
		return err == nil
	}
	plainStatus := func(dir string) {
		cmd := exec.Command("git", "status", "--porcelain")
		cmd.Dir = dir
		cmd.Run()
	}

	t.Run("replaced .git", func(t *testing.T) {
		repo, wt := worktreeOf(t)
		os.WriteFile(filepath.Join(wt, "f"), []byte("x"), 0o644)
		os.Remove(filepath.Join(wt, ".git"))
		gittest.Run(t, wt, "init", "-q")
		gittest.Run(t, wt, "config", "core.fsmonitor", monitor)
		if plainStatus(wt); !ran() {
			t.Skip("this git runs no monitor for status")
		}
		if tr, err := Pin(bg, repo, wt); err == nil {
			Run(bg, "", Opts{Tree: tr}, "status", "--porcelain")
			t.Error("pinned a replaced .git")
		}
		if ran() {
			t.Fatal("the planted monitor ran")
		}
	})

	t.Run("redirected common dir", func(t *testing.T) {
		repo, wt := worktreeOf(t)
		tr, err := Pin(bg, repo, wt)
		if err != nil {
			t.Fatal(err)
		}
		evil := filepath.Join(t.TempDir(), "evil.git")
		gittest.Run(t, repo, "clone", "-q", "--bare", repo, evil)
		gittest.Run(t, evil, "config", "core.fsmonitor", monitor)
		os.WriteFile(filepath.Join(tr.GitDir, "commondir"), []byte(evil+"\n"), 0o644)
		os.WriteFile(filepath.Join(wt, "f"), []byte("x"), 0o644)
		if plainStatus(wt); !ran() {
			t.Skip("this git runs no monitor for status")
		}
		tr, err = Pin(bg, repo, wt)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := Run(bg, "", Opts{Tree: tr}, "status", "--porcelain"); err != nil || !strings.Contains(out, "f") {
			t.Fatalf("status %q %v", out, err)
		}
		if ran() {
			t.Fatal("the planted monitor ran")
		}
	})

	t.Run("worktree config", func(t *testing.T) {
		repo, wt := worktreeOf(t)
		gittest.Run(t, repo, "config", "extensions.worktreeConfig", "true")
		gittest.Run(t, wt, "config", "--worktree", "core.fsmonitor", monitor)
		os.WriteFile(filepath.Join(wt, "f"), []byte("x"), 0o644)
		if plainStatus(wt); !ran() {
			t.Skip("this git runs no monitor for status")
		}
		tr, err := Pin(bg, repo, wt)
		if err != nil {
			t.Fatal(err)
		}
		Run(bg, "", Opts{Tree: tr}, "status", "--porcelain")
		Run(bg, wt, Opts{Untrusted: true}, "status", "--porcelain")
		if ran() {
			t.Fatal("the planted monitor ran")
		}
	})

	t.Run("worktree config filter", func(t *testing.T) {
		repo, wt := worktreeOf(t)
		gittest.Run(t, repo, "config", "extensions.worktreeConfig", "true")
		tr, err := Pin(bg, repo, wt)
		if err != nil {
			t.Fatal(err)
		}
		attrs := filepath.Join(t.TempDir(), "attributes")
		os.WriteFile(attrs, []byte("* filter=x\n"), 0o644)
		cfg := "[core]\n\tattributesFile = " + attrs + "\n[filter \"x\"]\n\tclean = \"" + monitor + "; cat\"\n\tzzz = planted\n"
		os.WriteFile(filepath.Join(tr.GitDir, "config.worktree"), []byte(cfg), 0o644)
		// The same size: git hashes it, through the filter, to tell.
		os.WriteFile(filepath.Join(wt, "a"), []byte("b"), 0o644)
		if plainStatus(wt); !ran() {
			t.Skip("this git runs no clean filter for status")
		}
		if out, err := Run(bg, "", Opts{Tree: tr}, "status", "--porcelain"); err != nil || !strings.Contains(out, "a") {
			t.Fatalf("status %q %v", out, err)
		}
		if ran() {
			t.Fatal("the planted filter ran")
		}
		if v, err := Run(bg, "", Opts{Tree: tr}, "config", "filter.x.zzz"); err == nil {
			t.Fatalf("the run read config.worktree: %q", v)
		}
	})
}

// TestPinnedRunGitDir: a linked worktree's pinned run takes refs from
// the repository's common dir, whatever its own git dir's commondir
// names, and its HEAD and index from its own git dir; the git dir tm
// makes for the run is gone after it.
func TestPinnedRunGitDir(t *testing.T) {
	repo, wt := worktreeOf(t)
	tr, err := Pin(bg, repo, wt)
	if err != nil {
		t.Fatal(err)
	}
	want := gittest.Run(t, wt, "rev-parse", "HEAD")
	empty := filepath.Join(t.TempDir(), "empty.git")
	gittest.Run(t, repo, "init", "-q", "--bare", empty)
	os.WriteFile(filepath.Join(tr.GitDir, "commondir"), []byte(empty+"\n"), 0o644)
	if got, err := Run(bg, "", Opts{Tree: tr}, "rev-parse", "HEAD"); err != nil || got != want {
		t.Fatalf("HEAD %q %v, want %s", got, err, want)
	}
	if b, err := Run(bg, "", Opts{Tree: tr}, "for-each-ref", "--format=%(refname)", "refs/heads/tm/x"); err != nil || b != "refs/heads/tm/x" {
		t.Fatalf("branch %q %v", b, err)
	}

	git := func(args ...string) {
		gittest.Run(t, wt, append([]string{"--git-dir", tr.GitDir, "--work-tree", wt}, args...)...)
	}
	os.WriteFile(filepath.Join(wt, "b"), []byte("b"), 0o644)
	git("add", "b")
	if out, err := Run(bg, "", Opts{Tree: tr}, "status", "--porcelain"); err != nil || out != "A  b" {
		t.Fatalf("status %q %v", out, err)
	}
	git("update-index", "--split-index")
	if out, err := Run(bg, "", Opts{Tree: tr}, "status", "--porcelain"); err != nil || out != "A  b" {
		t.Fatalf("split index: status %q %v", out, err)
	}

	// Made in tm's state dir, never the temp folder, and gone after.
	h := t.TempDir()
	t.Setenv("TERMINATR_HOME", h)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if _, err := Run(bg, "", Opts{Tree: tr}, "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	if es, err := os.ReadDir(RunsDir(h)); err != nil || len(es) != 0 {
		t.Errorf("runs dir: %v %v", es, err)
	}
	if es, _ := os.ReadDir(tmp); len(es) != 0 {
		t.Errorf("in the temp folder: %v", es)
	}

	// Shared index files: regular files only, and a few.
	sh, _ := filepath.Glob(filepath.Join(tr.GitDir, "sharedindex.*"))
	if len(sh) == 0 {
		t.Fatal("no shared index file")
	}
	extra := filepath.Join(tr.GitDir, "sharedindex.0000")
	os.Symlink(sh[0], extra)
	if _, err := Run(bg, "", Opts{Tree: tr}, "status"); err == nil {
		t.Error("a linked shared index file was taken")
	}
	os.Remove(extra)
	for i := range maxSharedIndex {
		os.WriteFile(filepath.Join(tr.GitDir, "sharedindex.extra"+strconv.Itoa(i)), nil, 0o644)
	}
	if _, err := Run(bg, "", Opts{Tree: tr}, "status"); err == nil {
		t.Errorf("over %d shared index files were taken", maxSharedIndex)
	}

	// The index must be a file.
	index := filepath.Join(tr.GitDir, "index")
	os.Rename(index, index+".real")
	os.Symlink(index+".real", index)
	if _, err := Run(bg, "", Opts{Tree: tr}, "status"); err == nil {
		t.Error("a linked index was taken")
	}

	os.WriteFile(filepath.Join(tr.GitDir, "HEAD"), []byte("ref: refs/heads/tm/x\nmore\n"), 0o644)
	if _, err := Run(bg, "", Opts{Tree: tr}, "status"); err == nil {
		t.Error("a HEAD of two lines was taken")
	}
}

func same(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// TestOneRunner: tm runs git only through this package (tests and the
// test helpers aside), so a hardening of it covers every run.
func TestOneRunner(t *testing.T) {
	re := regexp.MustCompile(`exec\.Command(Context)?\([^)]*"git"`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir() && (rel == "internal/gitx" || rel == "internal/gittest" || rel == "internal/e2e" || strings.HasPrefix(d.Name(), ".")) && rel != ".":
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go"):
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if re.Match(b) {
			t.Errorf("%s runs git itself; use gitx", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidBranch(t *testing.T) {
	long := strings.Repeat("a", MaxBranch)
	for b, want := range map[string]bool{
		"main": true, "tm/x/t-1-fix": true, "users/sam.v2_x": true, long: true,
		"": false, long + "a": false, "-x": false, "/x": false, "a+b": false, "a b": false,
		"a..b": false, "a//b": false, "a/": false, "a.": false, "a/.b": false, ".a": false,
		"a.lock": false, "a.lock/b": false, "HEAD": false, "a@{b": false, "a~1": false,
	} {
		if got := ValidBranch(b); got != want {
			t.Errorf("ValidBranch(%q) = %v, want %v", b, got, want)
		}
		// Whatever tm takes, git takes.
		if ValidBranch(b) {
			if ok, err := CheckRefFormat(b, "--branch"); err != nil || !ok {
				t.Errorf("git refuses %q, ValidBranch takes it (%v)", b, err)
			}
		}
	}
}
