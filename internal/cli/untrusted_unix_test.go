//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestThreadWordsAreText: what a thread says of itself (its activity, its
// question) and the names of the files it attaches reach the console and
// the human's terminal, so they are kept as plain text: the words are
// cleaned, and a file name that isn't text is refused.
func TestThreadWordsAreText(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env["TERMINATR_PROJECT"] = "demo"
	p, _ := project.Open("demo")
	wt := t.TempDir()
	if _, err := thread.Create(p, thread.Record{Title: "Fix it", State: thread.Running, Worktree: wt}); err != nil {
		t.Fatal(err)
	}
	h.cwd = wt
	h.ok(thr, "status", "--activity", "build\u202e\x1b]52;c;eA==\aing", "--needs-you", "ok?\u200b\x1b[2J")
	st, err := thread.ReadStatus(p, "t-0001")
	if err != nil || st.Activity != "build]52;c;eA==ing" || st.NeedsYou != "ok?[2J" {
		t.Fatalf("status %+v %v", st, err)
	}

	bad := "plan\x1b]8;;https://example.net\x1b\\.md"
	os.WriteFile(filepath.Join(wt, bad), []byte("# Plan\n"), 0o644)
	report := "## Report\nDone.\n\n## Next\n- Merge it\n"
	if code, _, errs := h.run(thr, report, "report", "--attach", bad); code == 0 || !strings.Contains(errs, "one line of plain text") {
		t.Fatalf("attached %q: %d %s", bad, code, errs)
	}
	if _, err := os.Stat(thread.Path(p, "t-0001", "library", bad)); err == nil {
		t.Fatal("stored")
	}
}
