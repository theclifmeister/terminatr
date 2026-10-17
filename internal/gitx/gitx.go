// Package gitx is the one way tm runs git: a fixed base environment
// (no prompt, no optional locks, the C locale, no repository location
// inherited from tm's own environment), a timeout, a cap on the output,
// the process group killed on the way out, and an error that carries
// git's own last line of stderr.
//
// A folder an agent writes (a thread's worktree) is never trusted to say
// where its repository is: Pin derives the git dirs from the repository
// tm knows, checks the worktree's .git names them, and Run then passes
// them to git explicitly, with nothing in the folder able to make git
// run a program.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Defaults of a run.
const (
	DefaultTimeout = 2 * time.Minute
	DefaultMax     = 8 << 20
	maxStderr      = 64 << 10
)

// Opts are what a run adds to the base.
type Opts struct {
	// Timeout bounds the run (0: DefaultTimeout); ctx bounds it too.
	Timeout time.Duration
	// Max caps stdout (0: DefaultMax). Run fails on more; Output cuts.
	Max int
	// Config are key=value settings, each passed as -c before args.
	Config []string
	// Env are variables added to the base environment.
	Env []string
	// OK are exit codes besides 0 that count as success (git diff
	// --no-index answers 1 for a difference).
	OK []int
	// Tree pins the run to a worktree Pin checked: git runs in it with
	// its git dirs given, never looked up from the folder.
	Tree *Tree
	// Untrusted is a run that must look its repository up from a folder
	// an agent writes (to find out what it is): git starts no program on
	// the folder's behalf. A Tree run is always so.
	Untrusted bool
}

// Error is a git run that failed: its verb, exit code (-1 when git
// didn't exit) and git's last line of stderr, cleaned.
type Error struct {
	Verb string
	Code int
	Msg  string
	Err  error
}

func (e *Error) Error() string { return "git " + e.Verb + ": " + e.Msg }

func (e *Error) Unwrap() error { return e.Err }

// ExitCode is err's git exit code; -1 when err is no git exit.
func ExitCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return -1
}

// ErrTooLong is git output over Opts.Max, for Run.
var ErrTooLong = errors.New("output too long")

// Run runs git args in dir and returns its trimmed stdout.
func Run(ctx context.Context, dir string, o Opts, args ...string) (string, error) {
	out, cut, err := Output(ctx, dir, o, args...)
	if err == nil && cut {
		err = &Error{Verb: verb(args), Code: -1, Msg: ErrTooLong.Error(), Err: ErrTooLong}
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Output runs git args in dir and returns its stdout up to Opts.Max
// bytes, and whether it was cut there.
func Output(ctx context.Context, dir string, o Opts, args ...string) (string, bool, error) {
	timeout := orDefault(o.Timeout, DefaultTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cfg := append([]string{"protocol.ext.allow=never"}, o.Config...)
	env := Env()
	if o.Untrusted || o.Tree != nil {
		cfg = append(cfg, untrusted...)
	}
	if t := o.Tree; t != nil {
		dir = t.Dir
		env = append(env, "GIT_DIR="+t.GitDir, "GIT_COMMON_DIR="+t.CommonDir, "GIT_WORK_TREE="+t.Dir)
	}
	argv := make([]string, 0, 2*len(cfg)+len(args))
	for _, c := range cfg {
		argv = append(argv, "-c", c)
	}
	cmd := exec.CommandContext(ctx, "git", append(argv, args...)...)
	cmd.Dir = dir
	cmd.Env = append(env, o.Env...)
	stdout, stderr := &Capped{N: orDefault(o.Max, DefaultMax)}, &Capped{N: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	proc.Group(cmd)
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return "", false, &Error{Verb: verb(args), Code: -1, Msg: "took over " + timeout.String(), Err: ctx.Err()}
	case errors.As(err, &exit) && slices.Contains(o.OK, exit.ExitCode()):
	case err != nil:
		code := -1
		if exit != nil {
			code = exit.ExitCode()
		}
		return "", false, &Error{Verb: verb(args), Code: code, Msg: lastLine(stderr.Buf.String(), err), Err: err}
	}
	return stdout.Buf.String(), stdout.Over, nil
}

// untrusted is the configuration of a run in a folder an agent writes:
// no file system monitor and no hooks, the programs git would otherwise
// start on the folder's behalf. Git passes it on to the gits it runs.
var untrusted = []string{"core.fsmonitor=false", "core.hooksPath=" + os.DevNull}

// located are the variables that tell git where a repository is: none
// comes from tm's own environment (a tm started from a git hook).
var located = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX"}

// Env is the base environment of a git run: tm's, without a repository
// location, with no prompt, no optional locks (a read never contends
// with the user's own git) and the C locale.
func Env() []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.Contains(located, k)
	})
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "LC_ALL=C")
}

// verb is the git command of args, after any -c pairs.
func verb(args []string) string {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c" || a == "-C":
			i++
		case !strings.HasPrefix(a, "-"):
			return a
		}
	}
	return "run"
}

// lastLine is stderr's last non-empty line, cleaned for a terminal;
// err's text when there is none.
func lastLine(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if s := textsafe.Line(lines[len(lines)-1], 300); s != "" {
		return s
	}
	return err.Error()
}

func orDefault[T int | time.Duration](v, def T) T {
	if v > 0 {
		return v
	}
	return def
}

// Capped keeps the first N bytes written to it; Over says more came.
type Capped struct {
	Buf  bytes.Buffer
	N    int
	Over bool
}

func (c *Capped) Write(p []byte) (int, error) {
	if room := c.N - c.Buf.Len(); room < len(p) {
		c.Over = true
		c.Buf.Write(p[:max(room, 0)])
	} else {
		c.Buf.Write(p)
	}
	return len(p), nil
}

// MaxBranch is the longest branch name tm takes.
const MaxBranch = 100

// ValidBranch reports whether b is a branch name tm creates, shows and
// passes to git: at most MaxBranch characters from A-Z a-z 0-9 _ . / -,
// not starting with '-' or '/', and one git check-ref-format --branch
// accepts (no "..", "//", component starting with '.' or ending in
// ".lock", trailing '/' or '.', nor "HEAD"). It is the one rule for a
// branch name, checked without running git.
func ValidBranch(b string) bool {
	if b == "" || len(b) > MaxBranch || b == "HEAD" || b[0] == '-' || b[0] == '/' {
		return false
	}
	for i := 0; i < len(b); i++ {
		c := b[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '/' || c == '-') {
			return false
		}
	}
	if strings.Contains(b, "..") || strings.HasSuffix(b, ".") {
		return false
	}
	for _, part := range strings.Split(b, "/") {
		if part == "" || part[0] == '.' || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// CheckRefFormat reports whether git check-ref-format accepts name with
// flags (--branch, --allow-onelevel); err when git can't be run.
func CheckRefFormat(name string, flags ...string) (bool, error) {
	_, err := Run(context.Background(), "", Opts{Timeout: 10 * time.Second}, append(append([]string{"check-ref-format"}, flags...), name)...)
	if err == nil {
		return true, nil
	}
	if ExitCode(err) > 0 {
		return false, nil
	}
	return false, fmt.Errorf("can't run git: %w", err)
}

// Tree is a worktree with the git dirs it was checked against.
type Tree struct {
	Dir       string // the worktree's top folder
	GitDir    string // its own git dir: <common>/worktrees/<name>, or common for the main checkout
	CommonDir string // the repository's git dir
}

// ErrNotPinned means a folder is not a worktree of the repository, by
// the repository's own records: its .git is missing, or names another
// git dir, or the git dir it names doesn't name it back.
var ErrNotPinned = errors.New("not a worktree of the repository")

// Pin checks that dir is a checkout of repo and returns its git dirs,
// for Opts.Tree. The repository's git dir comes from repo, which tm
// knows; dir's .git, which an agent writes, must name it, or one of its
// worktrees' git dirs that names dir back. What git would otherwise read
// from the worktree's git dir about where the repository is
// (commondir) is overruled by the run.
func Pin(ctx context.Context, repo, dir string) (*Tree, error) {
	if repo == "" || dir == "" {
		return nil, ErrNotPinned
	}
	common, err := CommonDir(ctx, repo)
	if err != nil {
		return nil, err
	}
	top, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotPinned)
	}
	dotgit := filepath.Join(top, ".git")
	fi, err := os.Lstat(dotgit)
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s: %w", dir, ErrNotPinned)
	case fi.IsDir():
		// The main checkout: its .git is the repository's git dir.
		if dotgit != common {
			return nil, fmt.Errorf("%s: %w", dir, ErrNotPinned)
		}
		return &Tree{Dir: top, GitDir: common, CommonDir: common}, nil
	case !fi.Mode().IsRegular():
		return nil, fmt.Errorf("%s: %w", dir, ErrNotPinned)
	}
	own, ok := gitfile(dotgit, top)
	if !ok || filepath.Dir(own) != filepath.Join(common, "worktrees") {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotPinned)
	}
	if fi, err := os.Lstat(own); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotPinned)
	}
	if back, ok := gitfile(filepath.Join(own, "gitdir"), own); !ok || back != dotgit {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotPinned)
	}
	return &Tree{Dir: top, GitDir: own, CommonDir: common}, nil
}

// CommonDir is repo's git dir, the one its worktrees share, by its real
// path.
func CommonDir(ctx context.Context, repo string) (string, error) {
	out, err := Run(ctx, repo, Opts{Timeout: 30 * time.Second}, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(out)
}

// gitfile is the path a .git file (gitdir: <path>) or a worktree git
// dir's gitdir file (<path>) names, by its real path; a relative one is
// from base.
func gitfile(path, base string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 4096 {
		return "", false
	}
	s := strings.TrimSpace(string(b))
	s = strings.TrimPrefix(s, "gitdir: ")
	if s == "" || strings.ContainsAny(s, "\n\x00") {
		return "", false
	}
	if !filepath.IsAbs(s) {
		s = filepath.Join(base, s)
	}
	// The .git file itself may be gone from the gitdir file's point of
	// view: resolve the folder, keep the name.
	d, err := filepath.EvalSymlinks(filepath.Dir(s))
	if err != nil {
		return "", false
	}
	return filepath.Join(d, filepath.Base(s)), true
}
