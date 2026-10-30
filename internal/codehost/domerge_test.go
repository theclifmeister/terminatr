package codehost

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

const mergeHead = "0123456789abcdef0123456789abcdef01234567"

func TestGitHubMerge(t *testing.T) {
	var got []string
	g := GitHub{Run: func(_ string, args ...string) ([]byte, error) {
		if args[1] == "view" {
			return []byte("fix(merge): use the title\n"), nil
		}
		got = args
		return nil, nil
	}}
	for _, method := range MergeMethods {
		if err := g.Merge("/r", PR{Number: 12, Head: mergeHead}, method); err != nil {
			t.Fatal(err)
		}
		want := "pr merge 12 --" + method + " --match-head-commit " + mergeHead
		if method == BySquash {
			want += " --subject fix(merge): use the title (#12)"
		}
		if strings.Join(got, " ") != want {
			t.Fatalf("args %q, want %q", got, want)
		}
		for _, bad := range []string{"--admin", "--auto", "--delete-branch", "--body"} {
			if strings.Contains(strings.Join(got, " "), bad) {
				t.Errorf("args carry %s", bad)
			}
		}
	}
	got = nil
	if g.Merge("/r", PR{Number: 12}, ByMerge) == nil || got != nil {
		t.Fatalf("a PR without a head merged: %q", got)
	}
	if g.Merge("/r", PR{Number: 12, Head: mergeHead}, "auto") == nil || got != nil {
		t.Fatalf("an unknown method merged: %q", got)
	}
}

// A squash needs the PR's title: none read, none merged; a merge
// commit never asks for it.
func TestGitHubSquashNeedsTitle(t *testing.T) {
	for _, title := range []string{"", " \n"} {
		var merged bool
		g := GitHub{Run: func(_ string, args ...string) ([]byte, error) {
			merged = merged || args[1] == "merge"
			return []byte(title), nil
		}}
		if g.Merge("/r", PR{Number: 12, Head: mergeHead}, BySquash) == nil || merged {
			t.Errorf("title %q: merged=%v", title, merged)
		}
	}
	g := GitHub{Run: func(_ string, args ...string) ([]byte, error) {
		if args[1] == "view" {
			return nil, errors.New("gh: no network")
		}
		return nil, nil
	}}
	if g.Merge("/r", PR{Number: 12, Head: mergeHead}, BySquash) == nil || g.Merge("/r", PR{Number: 12, Head: mergeHead}, ByMerge) != nil {
		t.Error("a failed title read must stop a squash only")
	}
}

// T497: gh refusing the method is a MethodError with the methods the
// repo's settings allow, less the refused one; another failure isn't.
func TestGitHubMergeMethodRefused(t *testing.T) {
	for _, c := range []struct {
		method, stderr, repo string
		allowed              []string
	}{
		{ByMerge, "GraphQL: Merge commits are not allowed on this repository. (mergePullRequest)",
			`{"mergeCommitAllowed":false,"squashMergeAllowed":true,"rebaseMergeAllowed":true}`, []string{BySquash, ByRebase}},
		// A branch rule requiring linear history: the repo setting
		// still allows merge commits, so the refused one is left out.
		{ByMerge, "Repository rule violations found\n\nThis branch must not contain merge commits.",
			`{"mergeCommitAllowed":true,"squashMergeAllowed":true,"rebaseMergeAllowed":false}`, []string{BySquash}},
		{BySquash, "GraphQL: Squash merges are not allowed on this repository. (mergePullRequest)",
			`{"mergeCommitAllowed":true,"squashMergeAllowed":false,"rebaseMergeAllowed":false}`, []string{ByMerge}},
		{ByRebase, "GraphQL: Rebase merges are not allowed on this repository. (mergePullRequest)", `not json`, nil},
	} {
		g := GitHub{Run: func(_ string, args ...string) ([]byte, error) {
			if args[0] == "repo" {
				return []byte(c.repo), nil
			}
			if args[1] == "view" {
				return []byte("fix: a title"), nil
			}
			return nil, errors.New("gh pr merge: exit status 1 " + c.stderr)
		}}
		err := g.Merge("/r", PR{Number: 12, Head: mergeHead}, c.method)
		var me *MethodError
		if !errors.As(err, &me) || me.Method != c.method || !slices.Equal(me.Allowed, c.allowed) {
			t.Errorf("%s %q: %#v", c.method, c.stderr, err)
		}
	}
	g := GitHub{Run: func(string, ...string) ([]byte, error) {
		return nil, errors.New("gh pr merge: Head branch was modified. Review and try the merge again.")
	}}
	var me *MethodError
	if err := g.Merge("/r", PR{Number: 12, Head: mergeHead}, ByMerge); err == nil || errors.As(err, &me) {
		t.Fatalf("a moved head: %v", err)
	}
}

func TestAzureMerge(t *testing.T) {
	var got []string
	a := Azure{Target: Target{Kind: AzureKind, OrgURL: "https://dev.azure.com/o", Project: "p", Repo: "r"},
		Run: func(_ string, args ...string) ([]byte, error) {
			if !slices.Contains(args, "patch") {
				return []byte(`{"title":"fix(merge): use\nthe title"}`), nil
			}
			got = args
			return []byte(azCompleted), nil
		}}
	for method, strategy := range map[string]string{ByMerge: "noFastForward", BySquash: "squash", ByRebase: "rebase"} {
		if err := a.Merge("/r", PR{Number: 5, Head: mergeHead}, method); err != nil {
			t.Fatal(err)
		}
		all := strings.Join(got, " ")
		for _, w := range []string{"rest --method patch", "/pullrequests/5?", `"status":"completed"`, `"commitId":"` + mergeHead + `"`, `"mergeStrategy":"` + strategy + `"`, `"deleteSourceBranch":false`} {
			if !strings.Contains(all, w) {
				t.Errorf("%s: args lack %q: %s", method, w, all)
			}
		}
		if method == ByRebase && strings.Contains(all, "mergeCommitMessage") {
			t.Errorf("a rebase carries a message: %s", all)
		}
	}
	for _, method := range []string{ByMerge, BySquash} {
		if err := a.Merge("/r", PR{Number: 5, Head: mergeHead}, method); err != nil {
			t.Fatal(err)
		}
		all := strings.Join(got, " ")
		if has := strings.Contains(all, `"mergeCommitMessage":"fix(merge): use the title (!5)"`); has != (method == BySquash) {
			t.Errorf("%s: message in %s", method, all)
		}
	}
	got = nil
	if a.Merge("/r", PR{Number: 5, Head: mergeHead}, "rebaseMerge") == nil || got != nil {
		t.Fatalf("an unknown method merged: %q", got)
	}
	a.Run = func(string, ...string) ([]byte, error) {
		return nil, errors.New("az rest: Bad Request({\"message\":\"The merge strategy NoFastForward is not allowed by policy on this branch.\"})")
	}
	var me *MethodError
	if err := a.Merge("/r", PR{Number: 5, Head: mergeHead}, ByMerge); !errors.As(err, &me) || me.Method != ByMerge || me.Allowed != nil {
		t.Fatalf("a refused strategy: %#v", err)
	}
}

// azCompleted is a PR's answer once Azure DevOps completed it.
const azCompleted = `{"pullRequestId":5,"status":"completed","mergeStatus":"succeeded","lastMergeSourceCommit":{"commitId":"` + mergeHead +
	`"},"lastMergeCommit":{"commitId":"1111111111111111111111111111111111111111"}}`

// T878: Merge returns only once Azure DevOps shows the PR completed; a
// PR still active, a sign-in page, a failed merge or a failed call is
// an error saying why, never a merge.
func TestAzureMergeConfirms(t *testing.T) {
	oldWait, oldPoll := azMergeWait, azMergePoll
	azMergeWait, azMergePoll = 50*time.Millisecond, time.Millisecond
	t.Cleanup(func() { azMergeWait, azMergePoll = oldWait, oldPoll; azVia.Clear() })
	const active = `{"pullRequestId":5,"status":"active","mergeStatus":"succeeded","lastMergeSourceCommit":{"commitId":"` + mergeHead +
		`"},"repository":{"project":{"id":"b2e006cc-8463-426e-a1b2-26f75f2e224c"}}}`
	const policies = `{"value":[{"status":"approved","configuration":{"isBlocking":true,"type":{"displayName":"Build"}}},` +
		`{"status":"queued","configuration":{"isBlocking":true,"type":{"displayName":"Minimum number of reviewers"}}},` +
		`{"status":"queued","configuration":{"isBlocking":false,"type":{"displayName":"Comment requirements"}}}]}`
	type answer struct {
		out string
		err error
	}
	run := func(patch answer, reads ...string) (Azure, *[]string) {
		var calls []string
		return Azure{Target: Target{Kind: AzureKind, OrgURL: "https://dev.azure.com/o", Project: "p", Repo: "r"},
			Getenv: func(string) string { return "" },
			Run: func(_ string, args ...string) ([]byte, error) {
				all := strings.Join(args, " ")
				calls = append(calls, all)
				switch {
				case slices.Contains(args, "patch") || strings.HasPrefix(all, "repos pr update"):
					return []byte(patch.out), patch.err
				case strings.Contains(all, "/policy/evaluations"):
					return []byte(policies), nil
				}
				if len(reads) == 0 {
					return []byte(active), nil
				}
				r := reads[0]
				reads = reads[1:]
				return []byte(r), nil
			}}, &calls
	}
	pr := PR{Number: 5, Head: mergeHead}

	// Completed at once, or a read later.
	if a, _ := run(answer{out: azCompleted}); a.Merge("/r", pr, ByMerge) != nil {
		t.Fatal("completed: not merged")
	}
	a, calls := run(answer{out: active}, active, azCompleted)
	if err := a.Merge("/r", pr, ByMerge); err != nil || len(*calls) != 3 {
		t.Fatalf("completed later: %v, %q", err, *calls)
	}

	// Still active: the reason names the blocking policies not approved.
	var nm *NotMergedError
	a, _ = run(answer{out: active})
	err := a.Merge("/r", pr, ByMerge)
	if !errors.As(err, &nm) || !strings.Contains(nm.Why, "still shows it active (mergeStatus succeeded)") ||
		!strings.Contains(nm.Why, "blocking policies not approved: Minimum number of reviewers queued") || strings.Contains(nm.Why, "Comment") || strings.Contains(nm.Why, "Build") {
		t.Fatalf("still active: %v", err)
	}
	// Azure DevOps' own failure message.
	a, _ = run(answer{out: strings.Replace(active, `"mergeStatus":"succeeded"`, `"mergeStatus":"failure","mergeFailureMessage":"TF401192: the target branch moved"`, 1)})
	if err := a.Merge("/r", pr, ByMerge); !errors.As(err, &nm) || !strings.Contains(nm.Why, "could not merge it (mergeStatus failure): TF401192") {
		t.Fatalf("merge failure: %v", err)
	}
	// The call fails: its error, no merge.
	a, _ = run(answer{err: azError(errors.New("az rest: Bad Request({\"message\":\"TF401181: The pull request cannot be completed\"})"))})
	if err := a.Merge("/r", pr, ByMerge); err == nil || !strings.Contains(err.Error(), "TF401181") {
		t.Fatalf("API failure: %v", err)
	}
	// Something not a PR in answer.
	a, _ = run(answer{out: "{}"})
	if err := a.Merge("/r", pr, ByMerge); !errors.As(err, &nm) || !strings.Contains(nm.Why, "no PR tm can read") {
		t.Fatalf("empty answer: %v", err)
	}

	// The sign-in page (al0ft's: az rest exits 0 with it) is no merge:
	// the extension completes it then, and is asked first from then on.
	var ext []string
	a = Azure{Target: Target{Kind: AzureKind, OrgURL: "https://dev.azure.com/o", Project: "p", Repo: "r"},
		Getenv: func(string) string { return "" },
		Run: func(_ string, args ...string) ([]byte, error) {
			all := strings.Join(args, " ")
			switch {
			case slices.Contains(args, "patch"):
				return []byte("<!DOCTYPE html><html>Sign in</html>"), nil
			case strings.HasPrefix(all, "repos pr update"):
				ext = append(ext, all)
				return []byte(azCompleted), nil
			}
			return nil, errors.New("unexpected " + all)
		}}
	if err := a.Merge("/r", pr, BySquash); err == nil || ext != nil {
		// A squash reads the title first, which fails here.
		t.Fatalf("squash without a title: %v %q", err, ext)
	}
	if err := a.Merge("/r", pr, ByMerge); err != nil || len(ext) != 1 || !a.UsesExtension() ||
		!strings.Contains(ext[0], "repos pr update --org https://dev.azure.com/o --id 5 --status completed --delete-source-branch false --squash false") {
		t.Fatalf("sign-in page: %v %q", err, ext)
	}
	if err := a.Merge("/r", pr, ByRebase); err == nil || !strings.Contains(err.Error(), "can't complete a PR by rebase") {
		t.Fatalf("rebase through the extension: %v", err)
	}
	azVia.Clear()
	// The sign-in page with no extension to fall back on: az rest's
	// reason, not a merge.
	a.Run = func(_ string, args ...string) ([]byte, error) {
		if slices.Contains(args, "patch") {
			return []byte("<html>Sign in</html>"), nil
		}
		return nil, azError(exec.ErrNotFound)
	}
	if err := a.Merge("/r", pr, ByMerge); err == nil || !strings.Contains(err.Error(), "sign-in page") {
		t.Fatalf("sign-in page, no extension: %v", err)
	}
}
