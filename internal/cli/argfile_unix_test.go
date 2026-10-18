//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
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
	tempRoots = func(*Env) ([]string, []string, bool) {
		return []string{tmp}, []string{filepath.Join(tmp, "private")}, false
	}
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
	// A private folder of the agent's temp folder, its sandbox's to keep,
	// as a path or through a relative link beside it.
	os.MkdirAll(filepath.Join(tmp, "private"), 0o700)
	os.WriteFile(filepath.Join(tmp, "private", "diff"), []byte(report), 0o600)
	os.Symlink(filepath.Join("private", "diff"), filepath.Join(tmp, "plink"))
	h.expect(1, "outside-roots", thr, "report", "--file", filepath.Join(tmp, "private", "diff"))
	h.expect(1, "outside-roots", thr, "report", "--file", filepath.Join(tmp, "plink"))
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

// TestAgentScratchpad: where the agent's temp folder is shared by all
// its sessions (Claude Code's), a thread may name files only in its own
// scratchpad (<temp>/<its worktree, every non-alphanumeric a "-">/
// <session>/scratchpad): a regular file loose in the temp folder (Claude's TMPDIR), but not through a link or a second name, not another
// working folder's (in any spelling), through no symlink at all, not as
// an adopted thread; a refusal lists the roots it may name.
func TestAgentScratchpad(t *testing.T) {
	h := newHarness(t)
	tmp := t.TempDir()
	prev := tempRoots
	tempRoots = func(*Env) ([]string, []string, bool) { return []string{tmp}, nil, true }
	t.Cleanup(func() { tempRoots = prev })
	h.ok(human, "project", "new", "demo")
	h.env["TERMINATR_PROJECT"] = "demo"
	p, _ := project.Open("demo")
	wts, err := home.WorktreesDir()
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(wts, "demo", "t-0001-fix")
	adoptedWt := filepath.Join(wts, "demo", "t-0002-adopted")
	for _, d := range []string{wt, adoptedWt} {
		os.MkdirAll(d, 0o755)
	}
	if _, err := thread.Create(p, thread.Record{Title: "Fix it", State: thread.Running, Worktree: wt}); err != nil {
		t.Fatal(err)
	}
	if _, err := thread.Create(p, thread.Record{Title: "Adopted", State: thread.Running, Worktree: adoptedWt, Adopted: true}); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(tmp, scratchName(wt), "sess", "scratchpad")
	theirsName := "-Users-someone--terminatr-projects-demo"
	theirs := filepath.Join(tmp, theirsName, "sess", "tasks")
	adoptedPad := filepath.Join(tmp, scratchName(adoptedWt), "sess", "scratchpad")
	for _, d := range []string{mine, theirs, adoptedPad} {
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "chart.png"), []byte("png"), 0o644)
	}
	os.WriteFile(filepath.Join(tmp, "loose.png"), []byte("png"), 0o644)
	os.Symlink("loose.png", filepath.Join(tmp, "loose-link.png"))
	os.WriteFile(filepath.Join(tmp, "hard-src.png"), []byte("png"), 0o644)
	os.Link(filepath.Join(tmp, "hard-src.png"), filepath.Join(tmp, "loose-hard.png"))
	rel, _ := filepath.Rel(mine, filepath.Join(theirs, "chart.png"))
	os.Symlink(rel, filepath.Join(mine, "steal.png"))
	os.Symlink(filepath.Join(theirs, "chart.png"), filepath.Join(mine, "abs.png"))
	// A link to a file in the same scratchpad is a link all the same,
	// and so is a link in a folder on the way.
	os.Symlink("chart.png", filepath.Join(mine, "same.png"))
	os.Mkdir(filepath.Join(mine, "sub"), 0o755)
	os.WriteFile(filepath.Join(mine, "sub", "in.png"), []byte("png"), 0o644)
	os.Symlink("sub", filepath.Join(mine, "subln"))
	report := "## Report\nDone.\n\n## Next\n- Merge it\n"
	h.cwd = wt
	stored := func(name string) bool {
		_, err := os.Stat(thread.Path(p, "t-0001", "library", name))
		return err == nil
	}
	if got := scratchName("/Users/a/.terminatr/wt"); got != "-Users-a--terminatr-wt" {
		t.Errorf("scratchName = %q", got)
	}
	// Windows: Claude Code's names start with the drive.
	if got := scratchName(`C:\Users\a\.terminatr\wt`); got != "C--Users-a--terminatr-wt" {
		t.Errorf("scratchName of a Windows path = %q", got)
	}
	if code, _, errs := h.run(thr, report, "report", "--attach", filepath.Join(mine, "chart.png"), "--attach", filepath.Join(mine, "sub", "in.png"), "--attach", filepath.Join(tmp, "loose.png")); code != 0 || !stored("chart.png") || !stored("in.png") || !stored("loose.png") {
		t.Fatalf("report with files in its own scratchpad and loose in the temp folder: exit %d, %s", code, errs)
	}
	refused := []string{
		filepath.Join(theirs, "chart.png"),                                            // another working folder's
		filepath.Join(tmp, strings.ToLower(theirsName), "sess", "tasks", "chart.png"), // a case variant
		filepath.Join(tmp, strings.ToUpper(scratchName(wt)), "sess", "scratchpad", "chart.png"),
		filepath.Join(mine, "steal.png"), filepath.Join(mine, "abs.png"), filepath.Join(mine, "same.png"), filepath.Join(mine, "subln", "in.png"),
		filepath.Join(adoptedPad, "chart.png"), // not its own
	}
	for _, a := range refused {
		code, _, errs := h.run(thr, report, "report", "--attach", a)
		if code == 0 {
			t.Errorf("report --attach %s: exit %d, %s", a, code, errs)
		}
	}
	for _, n := range []string{"loose-link.png", "loose-hard.png", "steal.png", "abs.png", "same.png"} {
		if stored(n) {
			t.Errorf("%s was stored", n)
		}
	}
	code, _, errs := h.run(thr, report, "report", "--attach", filepath.Join(os.TempDir(), "nope"))
	if code == 0 || !strings.Contains(errs, wt) || !strings.Contains(errs, scratchName(wt)) || strings.Contains(errs, theirsName) {
		t.Errorf("the refusal doesn't list the roots: exit %d, %s", code, errs)
	}

	// The scratch folder planted as a link to another's, or gone: refused.
	root := filepath.Join(tmp, scratchName(wt))
	os.Rename(root, root+".x")
	os.Symlink(filepath.Join(tmp, theirsName), root)
	for _, a := range []string{filepath.Join(root, "sess", "tasks", "chart.png"), filepath.Join(root, "sess", "scratchpad", "chart.png")} {
		if code, _, errs := h.run(thr, report, "report", "--attach", a); code == 0 {
			t.Errorf("a scratch folder that is a link to another's, report --attach %s: exit %d, %s", a, code, errs)
		}
	}
	os.Remove(root)
	if code, _, errs := h.run(thr, report, "report", "--attach", filepath.Join(root, "sess", "scratchpad", "chart.png")); code == 0 {
		t.Errorf("a missing scratch folder: exit %d, %s", code, errs)
	}

	// An adopted thread's worktree is the user's own checkout: its
	// scratch folder is shared with the user's sessions, so none.
	h.cwd = adoptedWt
	code, _, errs = h.run(caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0002"}, report, "report", "--attach", filepath.Join(adoptedPad, "chart.png"))
	if code == 0 || !strings.Contains(errs, "adopted") {
		t.Errorf("an adopted thread's scratchpad: exit %d, %s", code, errs)
	}
}

// TestThreadTempRoots (T587): a thread launched with its own temp folder
// may name files in its agent's temp folder there, never in the shared
// /tmp/claude-<uid> its sandbox can't read; one launched before has the
// shared folder still.
func TestThreadTempRoots(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	if _, err := thread.Create(p, thread.Record{Title: "Fix it", State: thread.Running, Worktree: t.TempDir(), Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	e := &Env{Caller: thr, InServer: true}
	shared := "/tmp/claude-" + strconv.Itoa(os.Getuid())
	roots, _, scratch := tempRoots(e)
	if !slices.Contains(roots, shared) || !scratch {
		t.Fatalf("before its own temp folder: roots %q scratch %v, want %s", roots, scratch, shared)
	}
	own, err := home.ThreadTempDir("demo", "t-0001")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(own) })
	roots, _, scratch = tempRoots(e)
	if want := []string{filepath.Join(own, "claude-"+strconv.Itoa(os.Getuid()))}; !slices.Equal(roots, want) || !scratch {
		t.Errorf("with its own temp folder: roots %q scratch %v, want %q", roots, scratch, want)
	}
	// Another thread's caller doesn't get this one's.
	other := &Env{Caller: caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0002"}, InServer: true}
	if r, _, _ := tempRoots(other); slices.ContainsFunc(r, func(d string) bool { return strings.HasPrefix(d, own) }) {
		t.Errorf("t-0002 may name t-0001's temp folder: %q", r)
	}
}

// TestResolveThreadTemp (T587): resolve removes a thread's own temp
// folder only when it and its folder in /tmp are this user's private
// folders; else it keeps it and says why.
func TestResolveThreadTemp(t *testing.T) {
	for _, open := range []bool{false, true} {
		h := newHarness(t)
		h.ok(human, "project", "new", "demo")
		_, rec := threadWithWorktree(t, h, "demo")
		d, err := home.ThreadTempDir("demo", rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Join(d, "claude-1"), 0o700)
		t.Cleanup(func() { os.RemoveAll(d) })
		if open {
			os.Chmod(d, 0o755)
		}
		out := h.ok(coord, "thread", "resolve", rec.ID, "--project", "demo")
		_, statErr := os.Stat(d)
		if open && (statErr != nil || !strings.Contains(out, "kept temp folder "+d)) {
			t.Errorf("a folder open to others: %q, %v", out, statErr)
		}
		if !open && (statErr == nil || !strings.Contains(out, "removed temp folder "+d)) {
			t.Errorf("own private folder: %q, %v", out, statErr)
		}
	}
}
