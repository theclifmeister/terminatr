package codehost

// Merging a PR (docs/SPEC.md §9, tm thread merge): one call, by the
// project's merge method, guarded by the head the checks were read on.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The merge methods, merge_method's values (config.MergeMethods): a
// merge commit, a squash commit, or the PR's commits rebased onto the
// base without one.
const (
	ByMerge  = "merge"
	BySquash = "squash"
	ByRebase = "rebase"
)

// MergeMethods are the methods in their order of mention.
var MergeMethods = []string{ByMerge, BySquash, ByRebase}

// MethodError is the host's refusal of the merge method: the repo's
// settings or branch rules don't allow it. Allowed are the methods the
// host says it allows, nil when it doesn't say.
type MethodError struct {
	Method  string
	Allowed []string
	Err     error
}

func (e *MethodError) Error() string { return e.Err.Error() }
func (e *MethodError) Unwrap() error { return e.Err }

// checkMerge refuses what no host is asked to merge: no number, no
// head, or a method tm doesn't know.
func checkMerge(who string, pr PR, method string) error {
	if pr.Number <= 0 || !oidRE.MatchString(pr.Head) {
		return fmt.Errorf("%s: no PR number or head to merge", who)
	}
	if !slices.Contains(MergeMethods, method) {
		return fmt.Errorf("%s: unknown merge method %q", who, method)
	}
	return nil
}

// ghMethodRE is gh's refusal of a merge method: the repo setting's
// "Merge commits are not allowed on this repository" (squash merges,
// rebase merges alike), or a branch rule that requires linear history.
var ghMethodRE = regexp.MustCompile(`(?i)(merge commits|squash merges|rebase merges) are not allowed|must not contain merge commits|linear history`)

// Merge asks gh to merge the PR by method (--merge, --squash or
// --rebase), which GitHub refuses when the head moved since pr was read.
// The branch is kept. A squash gets the PR's title and number as its
// subject ("title (#12)", as GitHub writes it when it takes the title
// itself; the repo's default may take the commit's message instead), read
// from the PR now; the body is left to GitHub. A refused method is a MethodError naming the methods the repo
// allows, as gh repo view says (less the refused one when a branch rule
// refused it).
func (g GitHub) Merge(repo string, pr PR, method string) error {
	if err := checkMerge("github", pr, method); err != nil {
		return err
	}
	args := []string{"pr", "merge", strconv.Itoa(pr.Number), "--" + method, "--match-head-commit", pr.Head}
	if method == BySquash {
		out, err := g.gh(repo, "pr", "view", strconv.Itoa(pr.Number), "--json", "title", "--jq", ".title")
		if err != nil {
			return err
		}
		subject, err := squashSubject(string(out), pr.Number, "#")
		if err != nil {
			return fmt.Errorf("github: %w", err)
		}
		args = append(args, "--subject", subject)
	}
	_, err := g.gh(repo, args...)
	if err == nil || !ghMethodRE.MatchString(err.Error()) {
		return err
	}
	return &MethodError{Method: method, Allowed: g.allowedMethods(repo, method), Err: err}
}

// squashSubject is a squash commit's subject: the PR's title on one
// line, then " (<mark>N)". An empty title is an error: the host would
// write its own.
func squashSubject(title string, n int, mark string) (string, error) {
	title = strings.Join(strings.FieldsFunc(title, func(r rune) bool { return unicode.IsControl(r) }), " ")
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return "", errors.New("the PR has no title to squash with")
	}
	return fmt.Sprintf("%s (%s%d)", title, mark, n), nil
}

// allowedMethods are the methods the repo's settings allow, less
// refused (which a branch rule refuses though the settings allow it);
// nil when gh can't say.
func (g GitHub) allowedMethods(repo, refused string) []string {
	b, err := g.gh(repo, "repo", "view", "--json", "mergeCommitAllowed,squashMergeAllowed,rebaseMergeAllowed")
	if err != nil {
		return nil
	}
	var r struct {
		Merge  *bool `json:"mergeCommitAllowed"`
		Squash *bool `json:"squashMergeAllowed"`
		Rebase *bool `json:"rebaseMergeAllowed"`
	}
	if json.Unmarshal(b, &r) != nil || r.Merge == nil || r.Squash == nil || r.Rebase == nil {
		return nil
	}
	allowed := []string{}
	for i, ok := range []bool{*r.Merge, *r.Squash, *r.Rebase} {
		if ok && MergeMethods[i] != refused {
			allowed = append(allowed, MergeMethods[i])
		}
	}
	return allowed
}

// azStrategy is completionOptions.mergeStrategy for each method.
var azStrategy = map[string]string{ByMerge: "noFastForward", BySquash: "squash", ByRebase: "rebase"}

// azMethodRE is Azure DevOps' refusal of a merge strategy, which a
// branch policy limits ("Limit merge types").
var azMethodRE = regexp.MustCompile(`(?i)merge (strategy|type)[^.]*not (allowed|permitted)|not (allowed|permitted)[^.]*merge (strategy|type)`)

// Merge completes the PR through the REST API (az rest, as the reads
// are): status completed by the method's merge strategy (noFastForward
// for a merge commit, squash, rebase), the head read as the commit it
// applies to, and the source branch kept. A strategy a branch policy
// refuses is a MethodError; Azure DevOps doesn't say which it allows.
// A squash's commit message is the PR's title and " (!N)" (Azure
// DevOps' PR link), as completionOptions.mergeCommitMessage; a merge
// commit keeps Azure DevOps' own message.
//
// Where az rest can't sign in (its sign-in page, which az passes on
// with exit 0, once read as success: T878), the azure-devops extension
// completes it (az repos pr update), at the head Azure DevOps has then,
// which tm read a moment ago. Azure DevOps finishes a completion after
// the call answers, or not at all (a policy pending, a reviewer
// missing), so the PR is read again until it is completed, for at most
// azMergeWait: one still active then is a *NotMergedError saying why.
func (a Azure) Merge(repo string, pr PR, method string) error {
	if err := checkMerge("azure devops", pr, method); err != nil {
		return err
	}
	subject := ""
	if method == BySquash {
		out, err := a.get(repo, a.prURL(pr.Number))
		if err != nil {
			return err
		}
		var p struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(out, &p); err != nil {
			return fmt.Errorf("azure devops: PR %d: %w", pr.Number, err)
		}
		if subject, err = squashSubject(p.Title, pr.Number, "!"); err != nil {
			return fmt.Errorf("azure devops: %w", err)
		}
	}
	var out []byte
	var err error
	ext := a.UsesExtension()
	if !ext {
		out, err = a.completeByREST(repo, pr, method, subject)
		ext = err != nil && authFailure(err)
	}
	if ext {
		var eerr error
		if out, eerr = a.completeByExtension(repo, pr, method, subject); eerr == nil {
			azVia.Store(a.Target.OrgURL, viaExt)
		}
		if err == nil || eerr == nil || !authFailure(eerr) {
			err = eerr
		}
	}
	if err != nil {
		if azMethodRE.MatchString(err.Error()) {
			return &MethodError{Method: method, Err: err}
		}
		return err
	}
	return a.awaitCompleted(repo, pr, out)
}

// completeByREST is the PATCH of status completed through az rest.
func (a Azure) completeByREST(repo string, pr PR, method, subject string) ([]byte, error) {
	u := a.prURL(pr.Number)
	if !a.trusted(u) {
		return nil, errUntrustedURL
	}
	opts := map[string]any{"mergeStrategy": azStrategy[method], "deleteSourceBranch": false}
	if subject != "" {
		opts["mergeCommitMessage"] = subject
	}
	body, _ := json.Marshal(map[string]any{
		"status":                "completed",
		"lastMergeSourceCommit": map[string]string{"commitId": pr.Head},
		"completionOptions":     opts,
	})
	out, err := a.az(repo, a.withSub([]string{"rest", "--method", "patch", "--resource", azResource, "--url", u,
		"--headers", "Content-Type=application/json", "Accept=application/json", "--body", string(body)})...)
	if err == nil && signInPage(out) {
		return nil, &CLIError{CLI: "az", Err: fmt.Errorf("az rest PATCH %s: Azure DevOps answered with its sign-in page (HTTP 203), not JSON", u),
			Problem: errSignInPage, auth: true,
			Advice: "the user installs the azure-devops extension (az extension add --name azure-devops), which tm asks when az rest can't sign in, or checks az login in a terminal (tm doctor)"}
	}
	return out, err
}

// completeByExtension completes the PR through az repos pr update,
// which takes the PR's head as Azure DevOps has it and has no rebase.
func (a Azure) completeByExtension(repo string, pr PR, method, subject string) ([]byte, error) {
	if !a.trusted(a.Target.OrgURL) {
		return nil, errUntrustedURL
	}
	if method == ByRebase {
		return nil, azError(errors.New("az repos pr update (the azure-devops extension, which tm uses where az rest can't sign in) can't complete a PR by rebase"))
	}
	args := []string{"repos", "pr", "update", "--org", a.Target.OrgURL, "--id", strconv.Itoa(pr.Number),
		"--status", "completed", "--delete-source-branch", "false", "--squash", strconv.FormatBool(method == BySquash)}
	if subject != "" {
		args = append(args, "--merge-commit-message", subject)
	}
	out, err := a.az(repo, args...)
	if err == nil && signInPage(out) {
		return nil, azError(fmt.Errorf("az repos pr update %d: Azure DevOps answered with its sign-in page", pr.Number))
	}
	return out, err
}

// azMergeWait is how long Merge waits for Azure DevOps to complete a
// PR, reading it every azMergePoll; variables for tests.
var (
	azMergeWait = 30 * time.Second
	azMergePoll = 2 * time.Second
)

// azCompletion is the part of a PR's answer that says how its
// completion went.
type azCompletion struct {
	ID                    int       `json:"pullRequestId"`
	Status                string    `json:"status"`
	MergeStatus           string    `json:"mergeStatus"`
	MergeFailureMessage   string    `json:"mergeFailureMessage"`
	LastMergeSourceCommit *azCommit `json:"lastMergeSourceCommit"`
	LastMergeCommit       *azCommit `json:"lastMergeCommit"`
	Repository            struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	} `json:"repository"`
}

// awaitCompleted reads the PR (first from answer, the completion's own)
// until Azure DevOps says it is completed with a merge commit: nil
// then; a *NotMergedError when it is abandoned, its merge failed, or
// azMergeWait passes with it still active.
func (a Azure) awaitCompleted(repo string, pr PR, answer []byte) error {
	deadline := time.Now().Add(azMergeWait)
	out := answer
	for {
		var c azCompletion
		if err := json.Unmarshal(out, &c); err != nil || c.ID != pr.Number {
			return &NotMergedError{Why: "Azure DevOps answered the completion with no PR tm can read: " + clip(clean(string(out)), 200)}
		}
		switch {
		case c.Status == "completed" && c.LastMergeCommit != nil && oidRE.MatchString(c.LastMergeCommit.CommitID):
			return nil
		case c.Status == "abandoned":
			return &NotMergedError{Why: "Azure DevOps shows it abandoned"}
		case c.Status == "active" && (c.MergeStatus == "failure" || c.MergeStatus == "conflicts") && c.MergeFailureMessage != "":
			return &NotMergedError{Why: "Azure DevOps could not merge it (mergeStatus " + c.MergeStatus + "): " + clip(clean(c.MergeFailureMessage), 200)}
		}
		if !time.Now().Before(deadline) {
			return &NotMergedError{Why: a.stillActive(repo, c)}
		}
		time.Sleep(azMergePoll)
		next, err := a.get(repo, a.prURL(pr.Number))
		if err != nil {
			return &NotMergedError{Why: "Azure DevOps took the completion, but reading the PR again failed: " + clip(clean(err.Error()), 200)}
		}
		out = next
	}
}

// stillActive says why a PR Azure DevOps was asked to complete is still
// active: its status and mergeStatus, and the blocking policies not yet
// approved, as Azure DevOps names their types.
func (a Azure) stillActive(repo string, c azCompletion) string {
	why := fmt.Sprintf("Azure DevOps still shows it %s (mergeStatus %s) after %s", clip(clean(c.Status), 20), clip(clean(c.MergeStatus), 20), azMergeWait)
	if m := clip(clean(c.MergeFailureMessage), 200); m != "" {
		why += ": " + m
	}
	pid := c.Repository.Project.ID
	if !guidRE.MatchString(pid) {
		return why
	}
	b, err := a.get(repo, a.projectURL()+"/_apis/policy/evaluations?artifactId="+
		url.QueryEscape("vstfs:///CodeReview/CodeReviewId/"+pid+"/"+strconv.Itoa(c.ID))+"&api-version=7.1-preview.1")
	if err != nil {
		return why
	}
	var pending []string
	for _, raw := range azValues(b) {
		var p azPolicy
		if json.Unmarshal(raw, &p) != nil || p.Configuration.IsBlocking != nil && !*p.Configuration.IsBlocking ||
			p.Configuration.IsEnabled != nil && !*p.Configuration.IsEnabled {
			continue
		}
		switch p.Status {
		case "approved", "notApplicable":
			continue
		}
		pending = append(pending, fmt.Sprintf("%s %s", clip(clean(p.Configuration.Type.DisplayName), 60), clip(clean(p.Status), 20)))
	}
	if len(pending) > 0 {
		why += "; blocking policies not approved: " + strings.Join(pending, ", ")
	}
	return why
}

// NotMergedError is a merge the host took but did not carry out, or
// can't be seen to have: Why says what the host shows instead.
type NotMergedError struct{ Why string }

func (e *NotMergedError) Error() string { return "not merged: " + e.Why }

// methodRefused is a mod's merge answer that refuses the method.
type methodRefused struct {
	Refused string   `json:"refused"`
	Allowed []string `json:"allowed"`
}

// knownMethods keeps the methods tm knows, in their order, from a mod's
// list; nil for none.
func knownMethods(list []string) []string {
	var out []string
	for _, m := range MergeMethods {
		if slices.ContainsFunc(list, func(s string) bool { return strings.EqualFold(s, m) }) {
			out = append(out, m)
		}
	}
	return out
}

var errMethodRefused = errors.New("the code host mod does not merge by this method")
