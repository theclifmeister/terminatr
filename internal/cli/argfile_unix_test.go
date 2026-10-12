//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestAgentFiles: an agent's command runs in the server with the human's
// rights, so the files it names (--attach, --file, --notes-file) open only
// in its worktree (a thread), its project's folder (a coordinator) or a
// temp folder, never through a link or ".." out of them. The human's open
// anywhere.
func TestAgentFiles(t *testing.T) {
	h := newHarness(t)
	tmp := t.TempDir()
	prev := tempRoots
	tempRoots = func(*Env) []string { return []string{tmp} }
	t.Cleanup(func() { tempRoots = prev })
	h.ok(human, "project", "new", "demo")
	h.env["TERMINATR_PROJECT"] = "demo"
	p, _ := project.Open("demo")
	wt := t.TempDir()
	if _, err := thread.Create(p, thread.Record{Title: "Fix it", State: thread.Running, Worktree: wt}); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "id_rsa")
	os.WriteFile(secret, []byte("KEY"), 0o600)
	report := "## Report\nDone.\n\n## Next\n- Merge it\n"
	os.WriteFile(filepath.Join(wt, "plan.md"), []byte("# Plan\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "report.md"), []byte(report), 0o644)
	os.WriteFile(filepath.Join(tmp, "chart.png"), []byte("png"), 0o644)
	os.Symlink(secret, filepath.Join(wt, "link"))
	os.Symlink(filepath.Dir(secret), filepath.Join(wt, "dir"))
	h.cwd = wt

	stored := func(name string) bool {
		_, err := os.Stat(thread.Path(p, "t-0001", "library", name))
		return err == nil
	}
	for _, a := range []string{secret, "link", "dir/id_rsa", filepath.Join(wt, "..", filepath.Base(filepath.Dir(secret)), "id_rsa")} {
		if code, _, errs := h.run(thr, report, "report", "--attach", a); code == 0 || stored("id_rsa") || stored("link") {
			t.Errorf("report --attach %s from a thread: exit %d, %s", a, code, errs)
		}
	}
	h.expect(1, "outside-roots", thr, "report", "--file", secret)
	// A FIFO would hold the server; a hard link names a file the agent
	// may not read.
	if err := syscall.Mkfifo(filepath.Join(wt, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.expect(1, "invalid-file", thr, "report", "--file", "fifo")
	if code, _, errs := h.run(thr, report, "report", "--attach", "fifo"); code == 0 || !strings.Contains(errs, "not a regular file") {
		t.Errorf("report --attach of a FIFO: exit %d, %s", code, errs)
	}
	if err := os.Link(secret, filepath.Join(wt, "hard")); err != nil {
		t.Fatal(err)
	}
	h.expect(1, "invalid-file", thr, "report", "--file", "hard")
	if code, _, errs := h.run(thr, report, "report", "--attach", "plan.md", "--attach", filepath.Join(tmp, "chart.png")); code != 0 || !stored("plan.md") || !stored("chart.png") {
		t.Fatalf("report with attachments in the worktree and temp: exit %d, %s", code, errs)
	}
	h.ok(thr, "report", "--file", "report.md")

	// A coordinator's files: its project's folder and temp, not elsewhere.
	h.ok(coord, "task", "add", "First")
	notes := p.Path("uploads", "notes.md")
	os.WriteFile(notes, []byte("the notes"), 0o644)
	h.ok(coord, "task", "edit", "T1", "--notes-file", notes)
	h.expect(1, "outside-roots", coord, "task", "edit", "T1", "--notes-file", secret)
	if data, _ := os.ReadFile(p.Path("TASKS.md")); strings.Contains(string(data), "KEY") {
		t.Fatal("a coordinator copied a file from outside its roots into TASKS.md")
	}

	// The human's open anywhere.
	h.ok(human, "task", "edit", "T1", "--notes-file", secret)
}

// TestAgentRootsNoHome: a thread's worktree root never holds the home
// folder (thread.toml is the coordinator's to write; an adopted thread's
// worktree is wherever its agent ran).
func TestAgentRootsNoHome(t *testing.T) {
	h, _ := os.UserHomeDir()
	if !holdsHome(h) || !holdsHome(filepath.Dir(h)) || !holdsHome("/") {
		t.Error("the home folder or a folder holding it is a root")
	}
	if holdsHome(filepath.Join(h, "src", "app")) || holdsHome(t.TempDir()) {
		t.Error("a folder in the home, or a temp folder, holds the home")
	}
}
