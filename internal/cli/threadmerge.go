package cli

// tm thread merge (docs/SPEC.md §9): the one way a coordinator merges a
// thread's PR. The policy lives here, not in prose: the project must let
// the coordinator merge, the PR's checks must be green on its current
// head and the host must call it mergeable. One look, one answer.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
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
	// The PR is found by the thread's own branch; the report's PR line
	// only has to agree with it.
	host := pickHost(r.Repo, p.CodeHost())
	if host.Kind() == codehost.NoKind {
		// No host was asked: "no PR" would be a guess.
		return mergeRefusal("no-code-host", "tm can't tell where the PRs of thread %s's repo %s live: its origin remote is missing, git can't read it, or it is no GitHub or Azure DevOps URL (git config --get remote.origin.url); the user checks it, or sets code_host in PROJECT.md",
			id, textsafe.Line(r.Repo, 200))
	}
	pr, err := host.PR(r.Repo, codehost.Ref{Branch: r.Branch})
	switch {
	case errors.Is(err, codehost.ErrNoRef):
		return mergeRefusal("no-pr", "thread %s has no branch to look its pull request up by", id)
	case errors.Is(err, codehost.ErrNoPR):
		return mergeRefusal("no-pr", "thread %s has no pull request yet: %s has none from its branch %s", id, hostWhere(host), r.Branch)
	case err != nil:
		return err
	}
	if pr.Branch != r.Branch || !codehost.InRepo(r.Repo, host, pr) {
		return mergeRefusal("not-thread-pr", "%s is not from thread %s's branch in its repo", pr.Ref(), id)
	}
	if reportPR != "" && !samePR(host, reportPR, pr) {
		return mergeRefusal("not-thread-pr", "thread %s's report names another PR than %s, the one of its branch", id, pr.Ref())
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
	method := safety.MergeMethod
	if err := host.Merge(r.Repo, pr, method); err != nil {
		var me *codehost.MethodError
		if errors.As(err, &me) {
			return methodRefusal(pr, me)
		}
		return mergeRefusal("merge-failed", "%s was not merged: %s", pr.Ref(), textsafe.Line(err.Error(), 300))
	}
	head := pr.Head[:7]
	if err := p.Journal(e.Caller, "thread.merge", id, fmt.Sprintf("%s merged at %s (%s)", pr.Ref(), head, methodWords[method])); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "%s merged %s (%s, head %s, checks green)\n", id, pr.Ref(), methodWords[method], head)
	return nil
}

// methodWords name the merge methods to the user.
var methodWords = map[string]string{config.MethodMerge: "merge commit", config.MethodSquash: "squash", config.MethodRebase: "rebase"}

// methodRefusal is merge-method-not-allowed: the host refused the
// project's merge method, with the methods it allows when it said, and
// the setting that picks another.
func methodRefusal(pr codehost.PR, me *codehost.MethodError) error {
	allows := ""
	if len(me.Allowed) > 0 {
		words := make([]string, len(me.Allowed))
		for i, m := range me.Allowed {
			words[i] = methodWords[m]
		}
		allows = " (it allows: " + strings.Join(words, ", ") + ")"
	}
	return mergeRefusal("merge-method-not-allowed", "%s was not merged: its repo does not allow merging by %s%s; the user picks another merge method in the Settings tab (merge_method in config.toml)",
		pr.Ref(), methodWords[me.Method], allows)
}

// samePR says whether the report's PR URL names pr: one of the host's
// PR URLs of this repo with pr's number. Any github.com PR URL parses,
// so GitHub's must be pr's URL itself.
func samePR(host codehost.Host, reportPR string, pr codehost.PR) bool {
	n, ok := host.ParsePRURL(reportPR)
	if !ok || n != pr.Number {
		return false
	}
	return host.Kind() != codehost.GitHubKind || strings.EqualFold(reportPR, pr.URL)
}

// hostWhere names the host a PR was asked of, and for Azure DevOps the
// repo, which comes from the origin remote, not from the thread.
func hostWhere(host codehost.Host) string {
	switch host.Kind() {
	case codehost.GitHubKind:
		return "GitHub"
	case codehost.AzureKind:
		if a, ok := host.(codehost.Azure); ok && a.Target.OrgURL != "" {
			return textsafe.Line("Azure DevOps ("+a.Target.OrgURL+"/"+a.Target.Project+"/_git/"+a.Target.Repo+")", 300)
		}
		return "Azure DevOps"
	}
	return "its code host"
}

func baseOr(b string) string {
	if b == "" {
		return "its base branch"
	}
	return b
}
