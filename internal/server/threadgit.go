package server

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// threadGitTimeout bounds the git run that finds a thread's repository.
const threadGitTimeout = 30 * time.Second

// threadTree is the checkout thread session r works in, with its git
// dirs, by the thread's record, which only tm writes: its worktree must
// be r's cwd and a checkout of its repo (gitx.Pin). Nil for a session
// of no thread, or when the folder isn't that checkout: its git dirs
// then get no grant (docs/SPEC.md §5.2). What the worktree's .git file
// says is never enough, as the agent writes it.
//
// A main checkout (Checkout, an adopted thread in the repository's own
// folder) has nothing outside the agent's reach to check its git dir
// against: it is <repo>/.git by the recorded path, a real folder, and
// git is not asked.
func threadTree(r SessionRecord) *gitx.Tree {
	if r.Role != proto.RoleThread || r.Project == "" || r.Thread == "" || r.Cwd == "" {
		return nil
	}
	p, err := project.Open(r.Project)
	if err != nil {
		return nil
	}
	rec, err := thread.Load(p, r.Thread)
	if err != nil || rec.Repo == "" || rec.Worktree == "" {
		return nil
	}
	cwd := realPath(r.Cwd)
	if realPath(rec.Worktree) != cwd {
		return nil
	}
	if rec.Checkout {
		top := realPath(rec.Repo)
		dotgit := filepath.Join(top, ".git")
		if fi, err := os.Lstat(dotgit); top != cwd || err != nil || !fi.IsDir() {
			return nil
		}
		return &gitx.Tree{Dir: top, GitDir: dotgit, CommonDir: dotgit}
	}
	ctx, cancel := context.WithTimeout(context.Background(), threadGitTimeout)
	defer cancel()
	t, err := gitx.Pin(ctx, rec.Repo, rec.Worktree)
	if err != nil || t.Dir != cwd {
		return nil
	}
	return t
}

// repoRoot is the main checkout's folder of git dir common: its parent,
// or common itself for a bare repo (worktree.Dirs).
func repoRoot(common string) string {
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return common
}
