package codehost

// Merging a PR (docs/SPEC.md §9, tm thread merge): one call, a regular
// merge commit, guarded by the head the checks were read on.

import (
	"encoding/json"
	"errors"
	"strconv"
)

// Merge asks gh to merge the PR with a merge commit, which GitHub
// refuses when the head moved since pr was read. The branch is kept.
func (g GitHub) Merge(repo string, pr PR) error {
	if pr.Number <= 0 || !oidRE.MatchString(pr.Head) {
		return errors.New("github: no PR number or head to merge")
	}
	_, err := g.gh(repo, "pr", "merge", strconv.Itoa(pr.Number), "--merge", "--match-head-commit", pr.Head)
	return err
}

// Merge completes the PR through the REST API (az rest, as the reads
// are): status completed with a no-fast-forward merge (a merge commit),
// the head read as the commit it applies to, and the source branch kept.
// Azure DevOps finishes the completion a moment after the call answers.
func (a Azure) Merge(repo string, pr PR) error {
	if pr.Number <= 0 || !oidRE.MatchString(pr.Head) {
		return errors.New("azure devops: no PR number or head to merge")
	}
	body, _ := json.Marshal(map[string]any{
		"status":                "completed",
		"lastMergeSourceCommit": map[string]string{"commitId": pr.Head},
		"completionOptions":     map[string]any{"mergeStrategy": "noFastForward", "deleteSourceBranch": false},
	})
	args := []string{"rest", "--method", "patch", "--resource", azResource, "--url", a.prURL(pr.Number),
		"--headers", "Content-Type=application/json", "--body", string(body)}
	if sub, ok := azSubs.Load(a.Target.OrgURL); ok {
		args = append(args, "--subscription", sub.(string))
	}
	_, err := a.az(repo, args...)
	return err
}
