package cli

// tm thread merge (docs/SPEC.md §9): the one way a coordinator merges a
// thread's PR. The policy lives here, not in prose: the project must let
// the coordinator merge, the PR's checks must be green on its current
// head and the host must call it mergeable. One look, one answer.

import (
	"errors"
	"fmt"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// pickHost is codehost.Pick; tests swap it.
var pickHost = codehost.Pick

func mergeRefusal(code, format string, a ...any) error {
	return &tasks.Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

func (e *Env) threadMerge(p *project.Project, id string) error {
	if err := e.coordinatorOnly(p, "tm thread merge"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	safety, err := cfg.Safety(p.Slug)
	if err != nil {
		return err
	}
	if safety.Merge != config.MergeCoordinator {
		return mergeRefusal("merge-not-allowed", `this project has merge = "thread": the thread merges its own PR, not the coordinator`)
	}
	r, err := thread.Load(p, id)
	if err != nil {
		return err
	}
	if r.Repo == "" {
		return mergeRefusal("no-pr", "thread %s has no repo, so no PR", id)
	}
	reportPR := ""
	if rep, _ := thread.ReadReport(p, id); rep != nil {
		reportPR = rep.PR
	}
	host := pickHost(r.Repo, p.CodeHost())
	pr, err := host.PR(r.Repo, codehost.Ref{URL: reportPR, Branch: r.Branch})
	switch {
	case errors.Is(err, codehost.ErrNoPR), errors.Is(err, codehost.ErrNoRef):
		return mergeRefusal("no-pr", "thread %s has no pull request yet (its report names none and its branch has none)", id)
	case err != nil:
		return err
	}
	switch pr.State {
	case "MERGED":
		fmt.Fprintf(e.Stdout, "%s unchanged (%s already merged)\n", id, pr.Ref())
		return nil
	case "OPEN":
	default:
		return mergeRefusal("pr-closed", "%s is %s, not open", pr.Ref(), pr.State)
	}
	switch pr.Checks {
	case "pass":
	case "fail":
		return mergeRefusal("not-green", "%s has %d failing check(s) on its current head; it is the thread's to fix", pr.Ref(), max(pr.Failed, 1))
	case "pending":
		return mergeRefusal("not-green", "%s's checks are still running on its current head; ask again when they finish", pr.Ref())
	default:
		return mergeRefusal("not-green", "%s shows no checks on its current head yet; ask again once they report", pr.Ref())
	}
	switch pr.Mergeable {
	case "MERGEABLE":
	case "CONFLICTING":
		return mergeRefusal("conflicting", "%s conflicts with %s; the thread rebases or merges the base in, pushes, and CI runs again", pr.Ref(), baseOr(pr.Base))
	default:
		return mergeRefusal("not-mergeable", "%s: the host has not said yet whether it merges cleanly; ask again in a minute", pr.Ref())
	}
	if len(pr.Head) != 40 {
		return mergeRefusal("not-mergeable", "%s: the host gave no head commit to merge", pr.Ref())
	}
	if err := host.Merge(r.Repo, pr); err != nil {
		return mergeRefusal("merge-failed", "%s was not merged: %s", pr.Ref(), oneLine(err.Error(), 300))
	}
	head := pr.Head[:7]
	if err := p.Journal(e.Caller, "thread.merge", id, fmt.Sprintf("%s merged at %s", pr.Ref(), head)); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "%s merged %s (merge commit, head %s, checks green)\n", id, pr.Ref(), head)
	return nil
}

func baseOr(b string) string {
	if b == "" {
		return "its base branch"
	}
	return b
}
