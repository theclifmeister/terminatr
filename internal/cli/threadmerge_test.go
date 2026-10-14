package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
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
}

func (f *fakeHost) Kind() string { return codehost.GitHubKind }
func (f *fakeHost) ParsePRURL(s string) (int, bool) {
	return codehost.GitHub{}.ParsePRURL(s)
}
func (f *fakeHost) PR(_ string, ref codehost.Ref) (codehost.PR, error) {
	f.asked = append(f.asked, ref)
	return f.pr, f.err
}
func (f *fakeHost) Merge(_ string, pr codehost.PR) error {
	f.merged = append(f.merged, pr)
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
	git(t, repo, "remote", "add", "origin", "git@github.com:acme/shop.git")
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
		if len(fh.merged) != 0 && code != "merge-failed" {
			t.Fatalf("%s: merged anyway", code)
		}
	}
	refuse("no-pr", func(f *fakeHost) { f.err = codehost.ErrNoPR })
	refuse("no-pr", func(f *fakeHost) { f.err = codehost.ErrNoRef })
	refuse("pr-closed", func(f *fakeHost) { f.pr.State = "CLOSED" })
	refuse("not-green", func(f *fakeHost) { f.pr.Checks, f.pr.Failed = "fail", 2 })
	refuse("not-green", func(f *fakeHost) { f.pr.Checks = "pending" })
	refuse("not-green", func(f *fakeHost) { f.pr.Checks = "" })
	refuse("conflicting", func(f *fakeHost) { f.pr.Mergeable = "CONFLICTING" })
	refuse("not-mergeable", func(f *fakeHost) { f.pr.Mergeable = "UNKNOWN" })
	refuse("not-mergeable", func(f *fakeHost) { f.pr.Head = "" })
	refuse("merge-failed", func(f *fakeHost) { f.mergeErr = errors.New("head moved") })
	// T378: only the PR of the thread's own branch, in its own repo.
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.Branch = "someone-else" })
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.Branch = "" })
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.URL = "https://github.com/other/shop/pull/7" })
	refuse("not-thread-pr", func(f *fakeHost) { f.pr.URL = "" })

	*fh = fakeHost{pr: codehost.PR{Number: 7, URL: pr7URL, State: "MERGED", Branch: rec.Branch}}
	if out := h.ok(coord, args()...); !strings.Contains(out, "already merged") || len(fh.merged) != 0 {
		t.Fatalf("merged PR: %q", out)
	}

	*fh = fakeHost{pr: greenPR()}
	out := h.ok(coord, args()...)
	if !strings.Contains(out, "merged PR #7") || len(fh.merged) != 1 || fh.merged[0].Head != head40 {
		t.Fatalf("merge: %q %+v", out, fh.merged)
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

	// merge = thread: the coordinator doesn't merge.
	cfg := filepath.Join(h.root, "config.toml")
	os.WriteFile(cfg, []byte("[projects.demo]\nmerge = \"thread\"\n"), 0o644)
	*fh = fakeHost{pr: greenPR()}
	h.expect(1, "merge-not-allowed", coord, args()...)
	if len(fh.merged) != 0 {
		t.Fatal("merged with merge = thread")
	}
}
