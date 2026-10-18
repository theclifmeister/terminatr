package cli

// tm thread resolve: the session, worktree and branches of a finished
// thread.

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

func (e *Env) threadResolve(p *project.Project, id string, discard bool) error {
	r, err := thread.Load(p, id)
	if err != nil {
		return err
	}
	if r.State == thread.Resolved && !(discard && leftover(r)) {
		fmt.Fprintf(e.Stdout, "%s unchanged (already resolved)\n", id)
		return nil
	}
	if discard {
		if err := discardable(p, r); err != nil {
			return err
		}
	}
	if _, live := e.sessionOf(r); live {
		if err := e.call(proto.MethodSessionStop, proto.SessionIDParams{ID: r.Session}, nil); err != nil {
			return err
		}
	}
	var did []string
	switch {
	case r.Checkout:
		// Adopted in the repository's own checkout: not tm's to remove,
		// and its branch is the checkout's (docs/SPEC.md §9, Adopt).
		did = append(did, "kept checkout "+r.Worktree+" (adopted; tm removes only worktrees)")
		more, gone := otherBranches(p, r)
		did = append(did, more...)
		for _, d := range gone {
			if err := p.Journal(e.Caller, "branch.delete", id, d); err != nil {
				return err
			}
		}
	case r.Repo != "" && !followRepo(p, r):
		// The repository is gone and no repo of the project has the
		// branch: there is nothing git could do (docs/SPEC.md §9).
		did = append(did, "repo "+r.Repo+" is gone")
		if _, err := os.Stat(r.Worktree); err == nil {
			did = append(did, "kept worktree "+r.Worktree+" (no repo to remove it from)")
		} else {
			did = append(did, "worktree was already gone")
		}
	case r.Repo != "":
		_, statErr := os.Stat(r.Worktree)
		remove := worktree.Remove
		if discard {
			remove = worktree.RemoveForce
		}
		err := remove(r.Repo, r.Worktree)
		switch {
		case errors.Is(err, worktree.ErrDirty):
			did = append(did, "kept worktree "+r.Worktree+" (uncommitted changes)")
		case err != nil:
			did = append(did, "kept worktree "+r.Worktree+" ("+textsafe.Line(err.Error(), 120)+")")
		case statErr != nil:
			did = append(did, "worktree was already gone")
		default:
			did = append(did, "removed worktree "+r.Worktree)
		}
		more, deleted := branchCleanup(p, r, discard)
		did = append(did, more...)
		more, gone := otherBranches(p, r)
		did, deleted = append(did, more...), append(deleted, gone...)
		for _, d := range deleted {
			if err := p.Journal(e.Caller, "branch.delete", id, d); err != nil {
				return err
			}
		}
	case r.Adopted:
		did = append(did, "kept folder "+r.Worktree+" (adopted)")
	default:
		if err := os.Remove(r.Worktree); err == nil || errors.Is(err, os.ErrNotExist) {
			did = append(did, "removed folder "+r.Worktree)
		} else {
			did = append(did, "kept folder "+r.Worktree+" (not empty)")
		}
	}
	// Its own temp folder (T587): scratchpads and task output, nothing
	// handed in (a report's attachments are copied to the library).
	if d, err := home.ThreadTempDir(p.Slug, id); err == nil {
		if _, err := os.Lstat(d); err == nil {
			if err := os.RemoveAll(d); err != nil {
				did = append(did, "kept temp folder "+d+" ("+textsafe.Line(err.Error(), 120)+")")
			} else {
				did = append(did, "removed temp folder "+d)
			}
		}
	}
	if _, err := thread.Update(p, id, func(x *thread.Record) error {
		x.State, x.Repo = thread.Resolved, r.Repo
		if x.ResolvedAt.IsZero() {
			x.ResolvedAt = time.Now().UTC().Truncate(time.Second)
		}
		return nil
	}); err != nil {
		return err
	}
	summary := thread.Label(p, id) + " resolved: " + strings.Join(did, "; ")
	if _, err := p.AddItem("thread-resolved", id, summary, false); err != nil {
		return err
	}
	if err := p.Journal(e.Caller, "thread.resolve", id, strings.Join(did, "; ")); err != nil {
		return err
	}
	fmt.Fprintln(e.Stdout, shown(summary))
	return nil
}

// branchCleanup deletes the thread's own branch when nothing would be
// lost: with discard (discardable found no commit that is nowhere
// else), after a merged PR whose commits git finds on the remote, or
// when its commits are all on the default branch. It returns what it
// did, for the resolve item, and the deleted branches, for the journal.
func branchCleanup(p *project.Project, r *thread.Record, discard bool) (did, deleted []string) {
	if r.Branch == "" || !worktree.BranchExists(r.Repo, r.Branch) {
		return nil, nil
	}
	// del deletes the branch and records what happened, whatever the why.
	del := func(why string) {
		if err := worktree.DeleteBranch(r.Repo, r.Branch); err != nil {
			did = append(did, "kept branch "+r.Branch+" ("+textsafe.Line(err.Error(), 120)+")")
			return
		}
		did = append(did, "deleted branch "+r.Branch+" ("+why+")")
		deleted = append(deleted, r.Branch+" ("+why+")")
	}
	if discard {
		del("discarded")
		return did, deleted
	}
	host := pickHost(r.Repo, p.CodeHost())
	switch st := host.PRState(r.Repo, r.Branch); st {
	case "MERGED":
		// The host's word deletes nothing by itself: commits made
		// after the merge and pushed nowhere would be lost, so git
		// must find the branch's commits on the remote.
		if unsaved, err := worktree.BranchUnsaved(r.Repo, r.Branch); err != nil || unsaved != "" {
			why := unsaved
			if err != nil {
				why = textsafe.Line(err.Error(), 120)
			}
			did = append(did, "kept branch "+r.Branch+" (PR merged, but "+why+")")
			break
		}
		del("PR merged")
	default:
		// No merged PR (a repo without remote or gh, or one merged
		// by hand): a branch whose commits are all on the default
		// branch loses nothing.
		if base := worktree.MergedBase(r.Repo, r.Branch); base != "" {
			del("merged into " + base)
		} else if st == "" {
			did = append(did, "kept branch "+r.Branch+" (no merged PR found)")
		} else {
			did = append(did, "kept branch "+r.Branch+" (PR "+strings.ToLower(st)+")")
		}
	}
	return did, deleted
}

// leftover says whether a resolved thread still has a worktree or branch
// of tm's own for resolve --discard to clean up.
func leftover(r *thread.Record) bool {
	if r.Checkout || r.Adopted || r.Repo == "" {
		return false
	}
	if _, err := os.Stat(r.Worktree); err == nil {
		return true
	}
	return r.Branch != "" && worktree.BranchExists(r.Repo, r.Branch)
}

// discardable says whether resolve --discard may throw the thread's
// worktree and branch away: only a worktree of tm's own, whose branch
// holds no commit that is on no remote (in a repo without remotes, not
// merged into another branch). Uncommitted changes are what it discards.
func discardable(p *project.Project, r *thread.Record) error {
	refuse := func(format string, a ...any) error {
		return &tasks.Error{Code: "discard-refused", Msg: fmt.Sprintf(format, a...)}
	}
	switch {
	case r.Checkout || r.Adopted:
		return refuse("thread %s is adopted: its checkout is the user's, not tm's to remove", r.ID)
	case r.Repo == "":
		return refuse("thread %s has no worktree to discard; resolve it", r.ID)
	case !followRepo(p, r):
		return refuse("repo %s is gone; resolve the thread without --discard", r.Repo)
	}
	unsaved, err := worktree.BranchUnsaved(r.Repo, r.Branch)
	if err != nil {
		return refuse("can't tell whether branch %s has unsaved work (%s); nothing discarded", r.Branch, textsafe.Line(err.Error(), 120))
	}
	if unsaved != "" {
		return refuse("%s: unsaved work; push it or ask the thread to, then resolve without --discard", unsaved)
	}
	return nil
}

// followRepo copes with a thread whose repository folder is gone: when
// one of the project's repos has the thread's branch, the repo was moved
// there, so r.Repo becomes it and the worktree is reconnected to it (git
// worktree repair). It reports false when no repo is left to use.
func followRepo(p *project.Project, r *thread.Record) bool {
	if _, err := os.Stat(r.Repo); err == nil {
		return true
	}
	if r.Branch == "" {
		return false
	}
	for _, repo := range p.Repos {
		if repo == r.Repo || !worktree.BranchExists(repo, r.Branch) {
			continue
		}
		r.Repo = repo
		if _, err := os.Stat(r.Worktree); err == nil {
			worktree.Repair(repo, r.Worktree)
		}
		return true
	}
	return false
}

// otherBranches cleans up the thread's branches other than its own: the
// head branches of the PRs its reports named, and local branches named
// tm/<slug>/<id>-… (docs/SPEC.md §9). Each is deleted with git branch
// -d, and only when worktree.PruneBranch finds nothing would be lost; an
// open PR's branch is kept. It returns what it did, for the resolve
// item, and the deleted branches, for the journal.
func otherBranches(p *project.Project, r *thread.Record) (did, deleted []string) {
	prOf := map[string]string{} // branch → its PR, "PR #12 merged", or ""
	open := map[string]bool{}
	host := codehost.Pick(r.Repo, p.CodeHost())
	for _, url := range thread.ReportPRs(p, r.ID) {
		st, n, head := host.PRHead(r.Repo, url)
		if head == "" || head == r.Branch || !worktree.BranchExists(r.Repo, head) {
			continue
		}
		prOf[head] = fmt.Sprintf("PR #%d %s", n, strings.ToLower(st))
		open[head] = open[head] || st == "OPEN"
	}
	for _, b := range worktree.ThreadBranches(r.Repo, p.Slug, r.ID) {
		if _, ok := prOf[b]; !ok && b != r.Branch {
			prOf[b] = ""
		}
	}
	branches := make([]string, 0, len(prOf))
	for b := range prOf {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	for _, b := range branches {
		pr := prOf[b]
		if open[b] {
			did = append(did, "kept branch "+b+" ("+pr+")")
			continue
		}
		ok, how := worktree.PruneBranch(r.Repo, b)
		if !ok {
			if pr != "" {
				how = pr + ", but " + how
			}
			did = append(did, "kept branch "+b+" ("+textsafe.Line(how, 120)+")")
			continue
		}
		how = "merged into " + how
		if pr != "" {
			how = pr + ", " + how
		}
		did = append(did, "deleted branch "+b+" ("+how+")")
		deleted = append(deleted, b+" ("+how+")")
	}
	return did, deleted
}
