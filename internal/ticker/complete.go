package ticker

// Completing tasks by the project's setting (docs/SPEC.md §6.4, §7.5):
// with complete_tasks "merged", a task in review is marked done once its
// thread's PR merged. The setting is the user's standing acceptance; the
// coordinator gets a task-done item, and a send-back reopens the task.

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// KindTaskDone is the kind of the entry for a task the ticker marked done
// by complete_tasks.
const KindTaskDone = "task-done"

// mergeMemo is a thread's merged PR: kept past the thread's resolve, so
// a task still in review later still finds the merge commit.
type mergeMemo struct {
	Repo  string `json:"repo"`
	PR    int    `json:"pr"`
	Merge string `json:"merge"`
}

// rememberMerge keeps thread r's PR merge commit for completeTasks.
func (t *Ticker) rememberMerge(p *project.Project, r *thread.Record, pr PR) {
	if pr.State != "MERGED" || !oidRE.MatchString(pr.Merge) || r.Repo == "" || pr.Number <= 0 {
		return
	}
	pm := t.projectMemo(p.Slug)
	if pm.Merges == nil {
		pm.Merges = map[string]*mergeMemo{}
	}
	pm.Merges[r.ID] = &mergeMemo{Repo: r.Repo, PR: pr.Number, Merge: pr.Merge}
}

// mergeOf is thread id's PR merge: as the ticker saw it, else from the
// PR in its report, by its repo's history (a PR that merged before tm
// kept merges) or, when git names no commit for it (a squash or rebase
// merge), by the host's word on the PR of a resolved thread (an open
// one's PR polls see it). nil when there is none.
func (t *Ticker) mergeOf(p *project.Project, pm *projectMemo, id string) *mergeMemo {
	if mm := pm.Merges[id]; mm != nil {
		return mm
	}
	if !thread.ValidID(id) {
		return nil
	}
	r, err := thread.Load(p, id)
	if err != nil || r.Repo == "" {
		return nil
	}
	rep, _ := thread.ReadReport(p, id)
	if rep == nil {
		return nil
	}
	// Only a URL of the repo's own host counts: a GitHub URL in an
	// Azure DevOps repo's report names no PR there.
	host := t.host(p, r.Repo)
	n, _ := host.ParsePRURL(rep.PR)
	if n <= 0 {
		return nil
	}
	if merge := host.MergeCommit(r.Repo, n); oidRE.MatchString(merge) {
		return &mergeMemo{Repo: r.Repo, PR: n, Merge: merge}
	}
	if r.State != thread.Resolved {
		return nil // its PR polls remember the merge (rememberMerge)
	}
	pr, err := host.PR(r.Repo, codehost.Ref{Number: n})
	if err != nil || pr.Number != n || pr.Branch != r.Branch || pr.State != "MERGED" || !oidRE.MatchString(pr.Merge) {
		return nil
	}
	mm := &mergeMemo{Repo: r.Repo, PR: n, Merge: pr.Merge}
	if pm.Merges == nil {
		pm.Merges = map[string]*mergeMemo{}
	}
	pm.Merges[id] = mm // asked once
	return mm
}

// PRNumber is the number of a GitHub or Azure DevOps pull request's URL,
// or a GitLab merge request's, 0 for none.
func PRNumber(url string) int {
	n, _, _ := codehost.ParsePRURL(url)
	return n
}

// byUser: a task the user owns is theirs to accept.
func byUser(owner string) bool {
	return strings.EqualFold(strings.TrimSpace(owner), "me")
}

// completeTasks marks done each task in review whose thread's PR merged
// (complete_tasks "merged"), once per merge commit: a task the user sent back
// after that stays open until a new PR ships. Tasks without a PR, and
// tasks the user owns, are left to the user.
func (t *Ticker) completeTasks(p *project.Project, safety config.Safety) {
	pm := t.projectMemo(p.Slug)
	board, err := p.Tasks().Load()
	if err != nil {
		return
	}
	// Forget what no task still needs.
	live, refs := map[string]bool{}, map[string]bool{}
	for _, tk := range board.Tasks {
		refs[tk.Ref()] = true
		if !tk.Status.Closed() && tk.Thread != "" {
			live[tk.Thread] = true
		}
	}
	for id := range pm.Merges {
		if !live[id] {
			delete(pm.Merges, id)
		}
	}
	for ref := range pm.Completed {
		if !refs[ref] {
			delete(pm.Completed, ref)
		}
	}
	if safety.CompleteTasks != config.CompleteMerged {
		return
	}
	for _, tk := range board.Tasks {
		if tk.Status != tasks.Review || tk.Thread == "" || byUser(tk.Owner) {
			continue
		}
		mm := t.mergeOf(p, pm, tk.Thread)
		if mm == nil || pm.Completed[tk.Ref()] == mm.Merge {
			continue
		}
		why := fmt.Sprintf("merged (PR #%d)", mm.PR)
		res, err := p.Tasks().CompleteBySetting(caller.Caller{Kind: caller.Ticker}, tk.ID, why)
		if err != nil {
			t.o.Log.Printf("ticker: %s: complete %s: %v", p.Slug, tk.Ref(), err)
			continue
		}
		if !res.Changed {
			continue
		}
		if pm.Completed == nil {
			pm.Completed = map[string]string{}
		}
		pm.Completed[tk.Ref()] = mm.Merge
		t.item(p, KindTaskDone, tk.Ref(), fmt.Sprintf("%s is done: %s, as the user's setting says (complete tasks when merged); the user can still send it back",
			thread.TaskLabel(p, tk.Ref()), why), false)
	}
}
