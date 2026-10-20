package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// fakeHost is a GitHub that answers PR with pr (or err) and records what
// PR was asked and what Merge got.
type fakeHost struct {
	codehost.NoHost
	pr       codehost.PR
	err      error
	mergeErr error
	asked    []codehost.Ref
	merged   []codehost.PR
	methods  []string
}

func (f *fakeHost) Kind() string { return codehost.GitHubKind }
func (f *fakeHost) ParsePRURL(s string) (int, bool) {
	return codehost.GitHub{}.ParsePRURL(s)
}
func (f *fakeHost) PR(_ string, ref codehost.Ref) (codehost.PR, error) {
	f.asked = append(f.asked, ref)
	return f.pr, f.err
}
func (f *fakeHost) Merge(_ string, pr codehost.PR, method string) error {
	f.merged = append(f.merged, pr)
	f.methods = append(f.methods, method)
	return f.mergeErr
}

const (
	head40 = "0123456789abcdef0123456789abcdef01234567"
	pr7URL = "https://github.com/acme/shop/pull/7"
)

// greenPRBranch is the thread's branch the green PR comes from.
var greenPRBranch string

func greenPR() codehost.PR {
	return codehost.PR{Number: 7, URL: pr7URL, State: "OPEN", Checks: "pass", Mergeable: "MERGEABLE", Head: head40, Base: "main", Branch: greenPRBranch}
}

// T239: tm thread merge merges only a green, mergeable, open PR, for the
// coordinator, in a project where the coordinator merges; every other
// case is a refusal with a code and no host call.
func TestThreadMerge(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	repo, rec := threadWithWorktree(t, h, "demo")
	gittest.Run(t, repo, "remote", "add", "origin", "git@github.com:acme/shop.git")
	greenPRBranch = rec.Branch
	t.Cleanup(func() { greenPRBranch = "" })
	fh := &fakeHost{pr: greenPR()}
	old := pickHost
	pickHost = func(string, codehost.Config) codehost.Host { return fh }
	t.Cleanup(func() { pickHost = old })

	args := func(a ...string) []string {
		return append([]string{"thread", "merge", rec.ID, "--project", "demo"}, a...)
	}
	h.expect(1, "coordinator-only", thr, args()...)
	if len(fh.merged) != 0 {
		t.Fatal("a thread merged")
	}

	refuse := func(code string, mut func(*fakeHost)) {
		t.Helper()
		*fh = fakeHost{pr: greenPR()}
		mut(fh)
		h.expect(1, code, coord, args()...)
		if len(fh.merged) != 0 && fh.mergeErr == nil {
			t.Fatalf("%s: merged anyway", code)
		}
	}
	refuse("no-pr", func(f *fakeHost) { f.err = codehost.ErrNoPR })
	refuse("no-pr", func(f *fakeHost) { f.err = codehost.ErrNoRef })
	// T629: the refusal names the host and the branch it asked.
	refuse("has none from its branch "+rec.Branch, func(f *fakeHost) { f.err = codehost.ErrNoPR })
	refuse("pr-closed", func(f *fakeHost) { f.pr.State = "CLOSED" })
	refuse("not-green", func(f *fakeHost) { f.pr.Checks, f.pr.Failed = "fail", 2 })
	refuse("not-green", func(f *fakeHost) { f.pr.Checks = "pending" })
	refuse("not-green", func(f *fakeHost) { f.pr.Checks = "" })
	refuse("conflicting", func(f *fakeHost) { f.pr.Mergeable = "CONFLICTING" })
	refuse("not-mergeable", func(f *fakeHost) { f.pr.Mergeable = "UNKNOWN" })
	refuse("not-mergeable", func(f *fakeHost) { f.pr.Head = "" })
	refuse("merge-failed", func(f *fakeHost) { f.mergeErr = errors.New("head moved") })
	// T497: the host refusing the method is its own refusal, naming
	// what the repo allows and the setting.
	refuse("merge-method-not-allowed", func(f *fakeHost) {
		f.mergeErr = &codehost.MethodError{Method: "merge", Allowed: []string{"squash", "rebase"}, Err: errors.New("Merge commits are not allowed on this repository")}
	})
	refuse("does not allow merging by merge commit (it allows: squash, rebase); the user picks another merge method in the Settings tab (merge_method", func(f *fakeHost) {
		f.mergeErr = &codehost.MethodError{Method: "merge", Allowed: []string{"squash", "rebase"}, Err: errors.New("x")}
	})
	// T378: only the PR of the thread's own branch, in its own repo.
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.Branch = "someone-else" })
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.Branch = "" })
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.URL = "https://github.com/other/shop/pull/7" })
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.URL = "" })

	// T629: a repo tm finds no code host for (its origin unreadable,
	// missing or of no known host) asks no host, so it is not "no PR".
	pickHost = func(string, codehost.Config) codehost.Host { return codehost.NoHost{} }
	h.expect(1, "no-code-host", coord, args()...)
	// Azure DevOps answering no PR for the branch names the repo asked.
	var asked []string
	pickHost = func(string, codehost.Config) codehost.Host {
		return codehost.Azure{
			Target: codehost.Target{Kind: codehost.AzureKind, OrgURL: "https://dev.azure.com/acme", Project: "Shop", Repo: "web"},
			Getenv: func(string) string { return "" },
			Run: func(_ string, a ...string) ([]byte, error) {
				asked = append(asked, strings.Join(a, " "))
				return []byte(`{"count":0,"value":[]}`), nil
			},
		}
	}
	h.expect(1, "no-pr: thread "+rec.ID+" has no pull request yet: Azure DevOps (https://dev.azure.com/acme/Shop/_git/web) has none from its branch "+rec.Branch, coord, args()...)
	if len(asked) != 1 || !strings.Contains(asked[0], "searchCriteria.sourceRefName=refs%2Fheads%2F"+strings.ReplaceAll(rec.Branch, "/", "%2F")+"&") {
		t.Fatalf("asked %q", asked)
	}
	pickHost = func(string, codehost.Config) codehost.Host { return fh }

	*fh = fakeHost{pr: codehost.PR{Number: 7, URL: pr7URL, State: "MERGED", Branch: rec.Branch}}
	if out := h.ok(coord, args()...); !strings.Contains(out, "already merged") || len(fh.merged) != 0 {
		t.Fatalf("merged PR: %q", out)
	}

	*fh = fakeHost{pr: greenPR()}
	out := h.ok(coord, args()...)
	if !strings.Contains(out, "merged PR #7 (merge commit, head 0123456") || len(fh.merged) != 1 || fh.merged[0].Head != head40 || fh.methods[0] != "merge" {
		t.Fatalf("merge: %q %+v %q", out, fh.merged, fh.methods)
	}

	// The report's PR line is a hint that must agree with the branch's
	// PR; the PR is looked up by the branch alone.
	p, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	report := func(url string) {
		t.Helper()
		if _, err := thread.StoreReport(p, rec.ID, "PR: "+url+"\n## Report\nDone.\n## Next\n- Merge it\n", nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, other := range []string{"https://github.com/acme/shop/pull/8", "https://github.com/other/shop/pull/7"} {
		report(other)
		*fh = fakeHost{pr: greenPR()}
		h.expect(1, "not-thread-pr", coord, args()...)
		if len(fh.merged) != 0 || len(fh.asked) != 1 || fh.asked[0] != (codehost.Ref{Branch: rec.Branch}) {
			t.Fatalf("report naming %s: merged %+v, asked %+v", other, fh.merged, fh.asked)
		}
	}
	report(pr7URL)
	*fh = fakeHost{pr: greenPR()}
	if out := h.ok(coord, args()...); !strings.Contains(out, "merged PR #7") || len(fh.merged) != 1 {
		t.Fatalf("own PR in the report: %q %+v", out, fh.merged)
	}

	// merge_method picks the method, from the project's table or
	// [defaults], and the success line names it.
	cfg := filepath.Join(h.root, "config.toml")
	for conf, want := range map[string]string{
		"[projects.demo]\nmerge_method = \"squash\"\n":                                       "squash",
		"[defaults]\nmerge_method = \"rebase\"\n":                                            "rebase",
		"[defaults]\nmerge_method = \"rebase\"\n[projects.demo]\nmerge_method = \"merge\"\n": "merge commit",
	} {
		os.WriteFile(cfg, []byte(conf), 0o644)
		*fh = fakeHost{pr: greenPR()}
		out := h.ok(coord, args()...)
		if !strings.Contains(out, "merged PR #7 ("+want+", head") || len(fh.methods) != 1 || methodWords[fh.methods[0]] != want {
			t.Fatalf("%s: %q %q", conf, out, fh.methods)
		}
	}

	// merge = thread: the coordinator doesn't merge.
	os.WriteFile(cfg, []byte("[projects.demo]\nmerge = \"thread\"\n"), 0o644)
	*fh = fakeHost{pr: greenPR()}
	h.expect(1, "merge-not-allowed", coord, args()...)
	if len(fh.merged) != 0 {
		t.Fatal("merged with merge = thread")
	}
}

// prStateHost answers every PRState with state.
type prStateHost struct {
	codehost.NoHost
	state string
}

func (p prStateHost) PRState(string, string) string { return p.state }

// BUG-01: a merged PR on a built-in host keeps the branch when it has a
// commit pushed nowhere, and deletes it when the remote has everything.
func TestResolveMergedKeepsUnpushed(t *testing.T) {
	old := pickHost
	pickHost = func(string, codehost.Config) codehost.Host { return prStateHost{state: "MERGED"} }
	t.Cleanup(func() { pickHost = old })
	for _, pushed := range []bool{false, true} {
		h := newHarness(t)
		h.ok(human, "project", "new", "demo")
		repo, rec := threadWithWorktree(t, h, "demo")
		bare := filepath.Join(t.TempDir(), "shop.git")
		gittest.Run(t, repo, "init", "-q", "--bare", bare)
		gittest.Run(t, repo, "remote", "add", "origin", bare)
		gittest.Run(t, repo, "push", "-q", "origin", "HEAD")
		os.WriteFile(filepath.Join(rec.Worktree, "late.md"), []byte("late"), 0o644)
		gittest.Run(t, rec.Worktree, "add", ".")
		gittest.Run(t, rec.Worktree, "commit", "-q", "-m", "after the merge")
		if pushed {
			gittest.Run(t, rec.Worktree, "push", "-q", "origin", rec.Branch)
		}
		out := h.ok(coord, "thread", "resolve", rec.ID, "--project", "demo")
		exists := worktree.BranchExists(repo, rec.Branch)
		switch {
		case !pushed && (!exists || !strings.Contains(out, "kept branch "+rec.Branch+" (PR merged, but 1 unpushed commit")):
			t.Errorf("unpushed: exists=%v %q", exists, out)
		case pushed && (exists || !strings.Contains(out, "deleted branch "+rec.Branch+" (PR merged)")):
			t.Errorf("pushed: exists=%v %q", exists, out)
		}
	}
}
