// Package worktree creates and removes the git worktrees threads run in
// (docs/SPEC.md §9). A worktree is a plain checkout: terminatr puts
// nothing in it, so removing it by any means loses nothing.
package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/gitx"
)

// gitTimeout bounds a git run: a fetch or a checkout of a big repo
// takes a while, but a hung one must not hold tm forever.
const gitTimeout = 10 * time.Minute

// git runs git in dir and returns its trimmed stdout.
func git(dir string, args ...string) (string, error) {
	return gitWithin(gitTimeout, dir, args...)
}

// gitWithin is git that is killed after timeout.
func gitWithin(timeout time.Duration, dir string, args ...string) (string, error) {
	return gitx.Run(context.Background(), dir, gitx.Opts{Timeout: timeout}, args...)
}

// untrusted is git in a folder an agent may write, which git is asked
// where its repository is (gitx.Opts.Untrusted).
func untrusted(dir string, args ...string) (string, error) {
	return gitx.Run(context.Background(), dir, gitx.Opts{Untrusted: true}, args...)
}

// DefaultBase is what a new worktree starts from: origin's default branch
// (origin/HEAD's target), after fetching origin. A repo without origin
// starts from its own HEAD.
func DefaultBase(repo string) (string, error) {
	if _, err := git(repo, "remote", "get-url", "origin"); err != nil {
		return "HEAD", nil
	}
	if _, err := git(repo, "fetch", "--quiet", "origin"); err != nil {
		return "", err
	}
	ref := originHead(repo, true)
	if ref == "" {
		return "", fmt.Errorf("%s: can't tell origin's default branch; pass --base", repo)
	}
	return ref, nil
}

// originHead is origin's default branch as last fetched ("origin/main"),
// "" when origin/HEAD isn't set. origin/HEAD isn't set in every clone:
// with ask, an unset one is first asked of the remote (git remote
// set-head origin --auto), which needs the network.
func originHead(repo string, ask bool) string {
	ref, err := git(repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if (err != nil || ref == "") && ask {
		git(repo, "remote", "set-head", "origin", "--auto")
		ref, err = git(repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	}
	if err != nil {
		return ""
	}
	return ref
}

// headBranch is the branch checked out in repo, "" when HEAD is detached.
func headBranch(repo string) string {
	ref, err := git(repo, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return ref
}

// Create adds a worktree at dir on a new branch from base. base "" means
// DefaultBase. It returns the base used.
func Create(repo, dir, branch, base string) (string, error) {
	if base != "" && !ValidBase(base) {
		return "", fmt.Errorf("base %q is not a branch, tag or commit name", base)
	}
	if base == "" {
		var err error
		if base, err = DefaultBase(repo); err != nil {
			return "", err
		}
	} else if _, err := git(repo, "remote", "get-url", "origin"); err == nil {
		git(repo, "fetch", "--quiet", "origin")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if err := addWorktree(repo, dir, "-b", branch, "--", dir, base); err != nil {
		return "", err
	}
	return base, nil
}

// addWorktree is git worktree add args, with the checkout of dir as a
// run gitx.Pin checked. The git dir git makes for dir is in the common
// dir, which a running thread's sandbox may write: the gits worktree add
// runs there (and its checkout) would follow a commondir put there to
// another repository's configuration. So git adds the worktree with no
// checkout and starts no hook or monitor; the checkout reads only the
// git dirs tm recorded.
func addWorktree(repo, dir string, args ...string) error {
	ctx := context.Background()
	if _, err := gitx.Run(ctx, repo, gitx.Opts{Timeout: gitTimeout, Untrusted: true},
		append([]string{"worktree", "add", "--quiet", "--no-checkout"}, args...)...); err != nil {
		return err
	}
	t, err := gitx.Pin(ctx, repo, dir)
	if err != nil {
		return err
	}
	_, err = gitx.Run(ctx, "", gitx.Opts{Timeout: gitTimeout, Tree: t}, "read-tree", "--reset", "-u", "HEAD")
	return err
}

// Restore re-adds a worktree removed by hand, on its existing branch.
func Restore(repo, dir, branch string) error {
	if !ValidBranch(branch) {
		return fmt.Errorf("branch %q is not a branch name", branch)
	}
	git(repo, "worktree", "prune")
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	return addWorktree(repo, dir, "--", dir, branch)
}

// ErrDirty means a worktree has changes and was kept.
var ErrDirty = errors.New("worktree has uncommitted changes")

// ErrNoRepo means the repository folder is gone (moved or deleted).
var ErrNoRepo = errors.New("repository folder is gone")

// Remove removes a worktree without force: a dirty one is kept and
// reported with ErrDirty. A worktree that is already gone is pruned. A
// repo that is gone gives ErrNoRepo, and nothing is touched. A folder
// that isn't repo's worktree by repo's records (gitx.ErrNotPinned) is
// kept.
func Remove(repo, dir string) error {
	if _, err := os.Stat(repo); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", repo, ErrNoRepo)
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		_, err := git(repo, "worktree", "prune")
		return err
	}
	switch dirty, err := Dirty(repo, dir); {
	case err != nil:
		return err
	case dirty:
		return ErrDirty
	}
	var flags []string
	if worktreeConfig(repo) {
		// Git's own check for changes would read dir's config.worktree:
		// Dirty's pinned one stands for it.
		flags = append(flags, "--force")
	}
	_, err := removeWorktree(repo, dir, flags...)
	return err
}

// worktreeConfig reports whether repo's git reads each worktree's own
// config.worktree (extensions.worktreeConfig); true when its config
// can't be read.
func worktreeConfig(repo string) bool {
	ctx := context.Background()
	common, err := gitx.CommonDir(ctx, repo)
	if err != nil {
		return true
	}
	out, err := gitx.Run(ctx, common, gitx.Opts{OK: []int{1}},
		"config", "--file", filepath.Join(common, "config"), "--type=bool", "--get", "extensions.worktreeConfig")
	return err != nil || out == "true"
}

// Dirty reports whether repo's worktree dir has uncommitted changes.
// Git runs in dir with the git dirs gitx.Pin checked, never the ones
// dir's own .git names: dir is the agent's to write. A folder that isn't
// repo's worktree by repo's records fails with gitx.ErrNotPinned.
func Dirty(repo, dir string) (bool, error) {
	ctx := context.Background()
	t, err := gitx.Pin(ctx, repo, dir)
	if err != nil {
		return false, err
	}
	out, err := gitx.Run(ctx, "", gitx.Opts{Tree: t}, statusArgs...)
	return out != "", err
}

// statusArgs is the check for changes of Dirty and Unsaved: every
// untracked file, and submodules' changes whatever .gitmodules says
// (its ignore setting is the worktree's to write).
var statusArgs = []string{"status", "--porcelain", "--ignore-submodules=none", "--untracked-files=all"}

// removeWorktree is git worktree remove of dir. Its own check for
// changes runs git status in dir: it is given the repo's git dir, not
// the one dir's git dir names, and no monitor or hooks.
func removeWorktree(repo, dir string, flags ...string) (string, error) {
	common, err := gitx.CommonDir(context.Background(), repo)
	if err != nil {
		return "", err
	}
	return gitx.Run(context.Background(), repo, gitx.Opts{Timeout: gitTimeout, Untrusted: true,
		Env: []string{"GIT_COMMON_DIR=" + common}},
		append(append([]string{"worktree", "remove"}, flags...), "--", dir)...)
}

// RemoveForce removes a worktree with its uncommitted changes (git
// worktree remove --force): resolve --discard, after BranchUnsaved found
// no commit that is nowhere else. A worktree already gone is pruned.
func RemoveForce(repo, dir string) error {
	if _, err := os.Stat(repo); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", repo, ErrNoRepo)
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		_, err := git(repo, "worktree", "prune")
		return err
	}
	_, err := removeWorktree(repo, dir, "--force")
	return err
}

// Repair reconnects repo with its linked worktrees at dirs after either
// side moved (git worktree repair): the worktrees' .git files and the
// repo's records of them then name each other's current paths.
func Repair(repo string, dirs ...string) error {
	if len(dirs) == 0 {
		return nil
	}
	_, err := git(repo, append([]string{"worktree", "repair"}, dirs...)...)
	return err
}

// Reconnect tells the repo of the linked worktree at dir that the
// worktree now lives at dir, after the worktree alone moved (git
// worktree repair, run in it).
func Reconnect(dir string) error {
	_, err := untrusted(dir, "worktree", "repair")
	return err
}

// Dirs are the git directories of a checkout.
type Dirs struct {
	// GitDir is the checkout's own git dir: <repo>/.git/worktrees/<name>
	// for a linked worktree (its HEAD and index), the repo's .git for the
	// main checkout.
	GitDir string
	// CommonDir is where the commits go: the main repo's .git. A
	// sandboxed thread must be able to write both.
	CommonDir string
	// RepoRoot is the main checkout's folder, the common dir's parent;
	// the common dir itself for a bare repo.
	RepoRoot string
}

// GitDirs returns the git directories of the checkout dir is in, as
// absolute paths.
func GitDirs(dir string) (Dirs, error) {
	out, err := untrusted(dir, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
	if err != nil {
		return Dirs{}, err
	}
	gd, cd, ok := strings.Cut(out, "\n")
	if !ok || !filepath.IsAbs(gd) || !filepath.IsAbs(cd) {
		return Dirs{}, fmt.Errorf("git rev-parse: unexpected output %q", out)
	}
	d := Dirs{GitDir: gd, CommonDir: cd, RepoRoot: cd}
	if filepath.Base(cd) == ".git" {
		d.RepoRoot = filepath.Dir(cd)
	}
	return d, nil
}

// PRState is the state of the PR whose head is branch, from repo's code
// host (codehost.Host.PRState): "MERGED", "OPEN", "CLOSED", or "" when
// there is none or the host can't tell.
func PRState(repo, branch string) string {
	return codehost.Pick(repo, codehost.Config{}).PRState(repo, branch)
}

// DeleteBranch deletes a local branch. Callers check first that its PR
// is merged or MergedBase found it; a squash merge leaves it unmerged in
// git's eyes, hence -D.
func DeleteBranch(repo, branch string) error {
	_, err := git(repo, "branch", "-D", branch)
	return err
}

// MergedBase is the branch that branch is merged into, for resolve:
// origin's default branch as last fetched, else the repo's checked-out
// branch. "" when it is merged into neither. No fetch: a merge that
// only the remote knows about is left to the PR check.
func MergedBase(repo, branch string) string {
	var bases []string
	if ref := originHead(repo, false); ref != "" {
		bases = append(bases, ref)
	}
	if ref := headBranch(repo); ref != "" && ref != branch {
		bases = append(bases, ref)
	}
	for _, b := range bases {
		if _, err := git(repo, "merge-base", "--is-ancestor", "refs/heads/"+branch, b); err == nil {
			return b
		}
	}
	return ""
}

// BranchExists reports whether the local branch exists.
func BranchExists(repo, branch string) bool {
	_, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// BranchUnsaved says what a resolved thread's branch holds that is
// nowhere else: "<n> unpushed commit(s) on <branch>", or "" for nothing
// (no such branch, or all its commits on a remote; in a repo without
// remotes, merged into another branch).
func BranchUnsaved(repo, branch string) (string, error) {
	if repo == "" || branch == "" || !BranchExists(repo, branch) {
		return "", nil
	}
	remotes, err := git(repo, "remote")
	if err != nil {
		return "", err
	}
	if remotes == "" {
		if MergedBase(repo, branch) != "" {
			return "", nil
		}
		return "unmerged commits on " + branch, nil
	}
	n, err := git(repo, "rev-list", "--count", "refs/heads/"+branch, "--not", "--remotes")
	if err != nil {
		return "", err
	}
	switch n {
	case "0":
		return "", nil
	case "1":
		return "1 unpushed commit on " + branch, nil
	}
	return n + " unpushed commits on " + branch, nil
}

// Unsaved says what removing a worktree would lose: "uncommitted
// changes", "<n> unpushed commit(s)", or "" for nothing. Commits count as
// pushed when a remote-tracking branch, or pushed (a commit the remote
// had, e.g. a merged PR's head, whose branch the merge deleted), has
// them. A repo without remotes has nowhere to push: its commits stay on
// the branch, which resolve keeps. A worktree already gone loses
// nothing. Git runs in dir with repo's git dirs (gitx.Pin): a folder
// that isn't repo's worktree by repo's records is an error.
func Unsaved(repo, dir, pushed string) (string, error) {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	ctx := context.Background()
	t, err := gitx.Pin(ctx, repo, dir)
	if err != nil {
		return "", err
	}
	git := func(args ...string) (string, error) {
		return gitx.Run(ctx, "", gitx.Opts{Tree: t}, args...)
	}
	out, err := git(statusArgs...)
	if err != nil {
		return "", err
	}
	if out != "" {
		return "uncommitted changes", nil
	}
	if remotes, err := git("remote"); err != nil || remotes == "" {
		return "", err
	}
	args := []string{"rev-list", "--count", "HEAD", "--not", "--remotes"}
	if pushed != "" {
		if _, err := git("cat-file", "-e", pushed+"^{commit}"); err == nil {
			args = append(args, pushed)
		}
	}
	n, err := git(args...)
	if err != nil {
		return "", err
	}
	switch n {
	case "0":
		return "", nil
	case "1":
		return "1 unpushed commit", nil
	}
	return n + " unpushed commits", nil
}

// MergeCommit is the commit that merged pull request n into the
// default branch as last fetched (origin's, else the checked-out
// branch), by git alone (codehost.Host.MergeCommit). "" when there is
// none: not merged, squashed, or not fetched yet. No fetch.
func MergeCommit(repo string, n int) string {
	return codehost.Pick(repo, codehost.Config{}).MergeCommit(repo, n)
}

// Place is where a directory sits in git, for a thread adopted in it
// (docs/SPEC.md §9, Adopt).
type Place struct {
	Top    string // the checkout's top directory
	Repo   string // the repository's main checkout
	Branch string // the branch checked out; "" when HEAD is detached
	Linked bool   // Top is a linked worktree, not the main checkout
}

// Locate tells where dir sits in git; ok is false outside a repository
// (or in a bare one).
func Locate(dir string) (pl Place, ok bool) {
	out, err := untrusted(dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		return Place{}, false
	}
	f := strings.Split(out, "\n")
	if len(f) != 3 || f[0] == "" {
		return Place{}, false
	}
	gitDir, common := filepath.Clean(f[1]), filepath.Clean(f[2])
	pl = Place{Top: filepath.Clean(f[0]), Linked: gitDir != common}
	pl.Repo = pl.Top
	if pl.Linked {
		if filepath.Base(common) != ".git" {
			return Place{}, false // a bare repository's worktree: no main checkout
		}
		pl.Repo = filepath.Dir(common)
	}
	pl.Branch, _ = untrusted(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	return pl, true
}
