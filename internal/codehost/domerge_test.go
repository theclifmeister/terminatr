package codehost

import (
	"strings"
	"testing"
)

const mergeHead = "0123456789abcdef0123456789abcdef01234567"

func TestGitHubMerge(t *testing.T) {
	var got []string
	g := GitHub{Run: func(_ string, args ...string) ([]byte, error) { got = args; return nil, nil }}
	if err := g.Merge("/r", PR{Number: 12, Head: mergeHead}); err != nil {
		t.Fatal(err)
	}
	if want := "pr merge 12 --merge --match-head-commit " + mergeHead; strings.Join(got, " ") != want {
		t.Fatalf("args %q, want %q", got, want)
	}
	for _, bad := range []string{"--squash", "--rebase", "--admin", "--auto"} {
		if strings.Contains(strings.Join(got, " "), bad) {
			t.Errorf("args carry %s", bad)
		}
	}
	got = nil
	if g.Merge("/r", PR{Number: 12}) == nil || got != nil {
		t.Fatalf("a PR without a head merged: %q", got)
	}
}

func TestAzureMerge(t *testing.T) {
	var got []string
	a := Azure{Target: Target{Kind: AzureKind, OrgURL: "https://dev.azure.com/o", Project: "p", Repo: "r"},
		Run: func(_ string, args ...string) ([]byte, error) { got = args; return []byte("{}"), nil }}
	if err := a.Merge("/r", PR{Number: 5, Head: mergeHead}); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(got, " ")
	for _, w := range []string{"rest --method patch", "/pullrequests/5?", `"status":"completed"`, `"commitId":"` + mergeHead + `"`, `"mergeStrategy":"noFastForward"`, `"deleteSourceBranch":false`} {
		if !strings.Contains(all, w) {
			t.Errorf("args lack %q: %s", w, all)
		}
	}
	if strings.Contains(all, "squash") || strings.Contains(all, "rebase") {
		t.Errorf("args: %s", all)
	}
}
