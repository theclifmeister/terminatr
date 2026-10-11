package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/codehost"
)

// fakeHost answers PR with pr (or err) and records what Merge got.
type fakeHost struct {
	codehost.NoHost
	pr       codehost.PR
	err      error
	mergeErr error
	merged   []codehost.PR
}

func (f *fakeHost) PR(string, codehost.Ref) (codehost.PR, error) { return f.pr, f.err }
func (f *fakeHost) Merge(_ string, pr codehost.PR) error {
	f.merged = append(f.merged, pr)
	return f.mergeErr
}

const head40 = "0123456789abcdef0123456789abcdef01234567"

func greenPR() codehost.PR {
	return codehost.PR{Number: 7, State: "OPEN", Checks: "pass", Mergeable: "MERGEABLE", Head: head40, Base: "main"}
}

// T239: tm thread merge merges only a green, mergeable, open PR, for the
// coordinator, in a project where the coordinator merges; every other
// case is a refusal with a code and no host call.
func TestThreadMerge(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	_, rec := threadWithWorktree(t, h, "demo")
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

	*fh = fakeHost{pr: codehost.PR{Number: 7, State: "MERGED"}}
	if out := h.ok(coord, args()...); !strings.Contains(out, "already merged") || len(fh.merged) != 0 {
		t.Fatalf("merged PR: %q", out)
	}

	*fh = fakeHost{pr: greenPR()}
	out := h.ok(coord, args()...)
	if !strings.Contains(out, "merged PR #7") || len(fh.merged) != 1 || fh.merged[0].Head != head40 {
		t.Fatalf("merge: %q %+v", out, fh.merged)
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
