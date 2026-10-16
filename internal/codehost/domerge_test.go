package codehost

import (
	"errors"
	"slices"
	"strings"
	"testing"
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
			return []byte("{}"), nil
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
	}
	if all := strings.Join(got, " "); strings.Contains(all, "mergeCommitMessage") {
		t.Errorf("a rebase carries a message: %s", all)
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
