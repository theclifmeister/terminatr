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

// threadRecord is thread session r's record, which only tm writes, when
// it has a repo and a worktree; nil otherwise.
func threadRecord(r SessionRecord) *thread.Record {
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
	return rec
}

// mainGitDir is a main checkout's git dir, <repo>/.git by its recorded
// path, when that is a folder; git is not asked, as the agent writes
// the checkout. "" otherwise.
func mainGitDir(repo string) string {
	dotgit := filepath.Join(realPath(repo), ".git")
	if fi, err := os.Lstat(dotgit); err != nil || !fi.IsDir() {
		return ""
	}
	return dotgit
}

// threadTree is the checkout thread session r works in, with its git
// dirs, by the thread's record: its worktree must be r's cwd and a
// checkout of its repo (gitx.Pin). Nil for a session of no thread, a
// record with no repo (older ones), or when the folder isn't that
// checkout: its git dirs then get no grant (docs/SPEC.md §5.2). What
// the worktree's .git file says is never enough, as the agent writes
// it.
//
// A main checkout (Checkout, an adopted thread in the repository's own
// folder) has nothing outside the agent's reach to check its git dir
// against: it is mainGitDir.
func threadTree(r SessionRecord) *gitx.Tree {
	rec := threadRecord(r)
	if rec == nil {
		return nil
	}
	cwd := realPath(r.Cwd)
	if realPath(rec.Worktree) != cwd {
		return nil
	}
	if rec.Checkout {
		dotgit := mainGitDir(rec.Repo)
		if dotgit == "" || realPath(rec.Repo) != cwd {
			return nil
		}
		return &gitx.Tree{Dir: cwd, GitDir: dotgit, CommonDir: dotgit}
	}
	ctx, cancel := context.WithTimeout(context.Background(), threadGitTimeout)
	defer cancel()
	t, err := gitx.Pin(ctx, rec.Repo, rec.Worktree)
	if err != nil || t.Dir != cwd {
		return nil
	}
	return t
}

// threadGitConfig are the paths of thread session r's repository whose
// writes the guard refuses: what git runs or reads as configuration in
// the git dir of the record's repo (gitConfig), and in each worktree's
// git dir there (ownConfig). The worktree isn't asked, so a thread that
// changes its .git mid-session keeps them all.
func threadGitConfig(r SessionRecord) []string {
	rec := threadRecord(r)
	if rec == nil {
		return nil
	}
	common := ""
	if rec.Checkout {
		common = mainGitDir(rec.Repo)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), threadGitTimeout)
		defer cancel()
		common, _ = gitx.CommonDir(ctx, rec.Repo)
	}
	if common == "" {
		return nil
	}
	dirs, files := gitConfig(common)
	out := append(dirs, files...)
	wts := filepath.Join(common, "worktrees")
	if es, err := os.ReadDir(wts); err == nil {
		for _, e := range es {
			if e.IsDir() {
				out = append(out, ownConfig(filepath.Join(wts, e.Name()))...)
			}
		}
	}
	return out
}

// repoRoot is the main checkout's folder of git dir common: its parent,
// or common itself for a bare repo (worktree.Dirs).
func repoRoot(common string) string {
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return common
}
