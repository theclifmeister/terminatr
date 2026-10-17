package codehost

// Merging a PR (docs/SPEC.md §9, tm thread merge): one call, by the
// project's merge method, guarded by the head the checks were read on.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
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
// applies to, and the source branch kept. Azure DevOps finishes the
// completion a moment after the call answers. A strategy a branch policy
// refuses is a MethodError; Azure DevOps doesn't say which it allows.
// A squash's commit message is the PR's title and " (!N)" (Azure
// DevOps' PR link), as completionOptions.mergeCommitMessage; a merge
// commit keeps Azure DevOps' own message.
func (a Azure) Merge(repo string, pr PR, method string) error {
	if err := checkMerge("azure devops", pr, method); err != nil {
		return err
	}
	opts := map[string]any{"mergeStrategy": azStrategy[method], "deleteSourceBranch": false}
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
		subject, err := squashSubject(p.Title, pr.Number, "!")
		if err != nil {
			return fmt.Errorf("azure devops: %w", err)
		}
		opts["mergeCommitMessage"] = subject
	}
	body, _ := json.Marshal(map[string]any{
		"status":                "completed",
		"lastMergeSourceCommit": map[string]string{"commitId": pr.Head},
		"completionOptions":     opts,
	})
	args := []string{"rest", "--method", "patch", "--resource", azResource, "--url", a.prURL(pr.Number),
		"--headers", "Content-Type=application/json", "--body", string(body)}
	if !TrustedAzureURL(a.prURL(pr.Number)) {
		return errUntrustedURL
	}
	_, err := a.az(repo, a.withSub(args)...)
	if err != nil && azMethodRE.MatchString(err.Error()) {
		return &MethodError{Method: method, Err: err}
	}
	return err
}

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
