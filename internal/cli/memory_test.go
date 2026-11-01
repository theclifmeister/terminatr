package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestMemoryFind: tm memory find lists the notes that match words and
// paths, best first, for the coordinator and the project's threads;
// tm memory template prints a note with the schema.
func TestMemoryFind(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	os.MkdirAll(p.Path("memory"), 0o755)
	os.WriteFile(p.Path("memory", "retries.md"), []byte("+++\ntitle = \"Retries stay at 4\"\ntouches = [\"internal/ticker/\"]\nstanding = true\nsource = \"T481\"\n+++\n## Decided\nKeep 4 for the gh poller.\n"), 0o644)
	os.WriteFile(p.Path("memory", "old.md"), []byte("# Old free-form note\n\nThe gh poller sweeps.\n"), 0o644)

	out := h.ok(thr, "memory", "find", "retries", "poller", "--path", "internal/ticker/pr.go", "--project", "demo")
	for _, want := range []string{"memory/retries.md  Retries stay at 4  (score 15.2: retries, poller, internal/ticker/pr.go)", "touches internal/ticker/ · standing · from T481", p.Dir} {
		if !strings.Contains(out, want) {
			t.Errorf("find lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old.md") {
		t.Errorf("find shows a note under the score:\n%s", out)
	}
	if out := h.ok(coord, "memory", "find", "gh", "poller", "sweeps", "--project", "demo"); !strings.Contains(out, "memory/old.md  Old free-form note") {
		t.Errorf("a free-form note isn't found:\n%s", out)
	}
	// A note for the coordinator only is never shown to a thread.
	os.WriteFile(p.Path("memory", "budget.md"), []byte("+++\ntitle = \"Poller budget\"\naudience = \"coordinator\"\n+++\nThe poller's budget.\n"), 0o644)
	if out := h.ok(thr, "memory", "find", "poller", "budget", "--project", "demo"); strings.Contains(out, "budget.md") {
		t.Errorf("a thread is shown the coordinator's note:\n%s", out)
	}
	if out := h.ok(coord, "memory", "find", "poller", "budget", "--project", "demo"); !strings.Contains(out, "memory/budget.md  Poller budget") {
		t.Errorf("the coordinator isn't shown its note:\n%s", out)
	}
	if out := h.ok(coord, "memory", "find", "nothing", "here", "--project", "demo"); !strings.Contains(out, "no note of demo's memory matches") {
		t.Errorf("no match:\n%s", out)
	}
	var res struct {
		Hits []project.Hit `json:"hits"`
	}
	if err := json.Unmarshal([]byte(h.ok(human, "memory", "find", "retries", "free", "--limit", "1", "--json", "--project", "demo")), &res); err != nil || len(res.Hits) != 1 {
		t.Fatalf("json: %v %+v", err, res)
	}

	h.expect(2, "", coord, "memory", "find", "--project", "demo")
	h.expect(2, "", coord, "memory", "find", "the", "--project", "demo")
	h.expect(2, "", coord, "memory", "find", "poller", "--limit", "0", "--project", "demo")
	h.expect(1, "other-project", caller.Caller{Kind: caller.Thread, Project: "other", Thread: "t-0001"}, "memory", "find", "poller", "--project", "demo")

	if out := h.ok(thr, "memory", "template"); out != project.NoteTemplate {
		t.Errorf("template:\n%s", out)
	}
}

// TestMemoryCheck: tm memory check flags a thread's diff that changes a
// note's decided value or adds what it ruled out, and tm report adds
// the warnings to its ## Check without refusing.
func TestMemoryCheck(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	repo, rec := threadWithWorktree(t, h, "demo")
	p, _ := project.Open("demo")
	os.MkdirAll(filepath.Join(repo, "internal", "ticker"), 0o755)
	os.WriteFile(filepath.Join(repo, "internal", "ticker", "pr.go"), []byte("package ticker\n\nconst retries = 4\n"), 0o644)
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "ticker")
	gittest.Run(t, rec.Worktree, "reset", "-q", "--hard", gittest.Run(t, repo, "rev-parse", "HEAD"))

	if out := h.ok(thr, "memory", "check", "--project", "demo"); !strings.Contains(out, "no note of demo's memory speaks against t-0001's diff") {
		t.Errorf("no notes:\n%s", out)
	}
	os.MkdirAll(p.Path("memory"), 0o755)
	os.WriteFile(p.Path("memory", "retries.md"), []byte("+++\ntitle = \"Retries stay at 4\"\ntouches = [\"internal/ticker/\"]\n+++\n## Decided\nKeep 4 for the gh poller.\n## Ruled out\nExponential backoff.\n"), 0o644)
	os.WriteFile(filepath.Join(rec.Worktree, "internal", "ticker", "pr.go"), []byte("package ticker\n\nconst retries = 6\n\nvar exponentialBackoff = true\n"), 0o644)

	os.WriteFile(p.Path("memory", "budget.md"), []byte("+++\ntitle = \"Retry budget\"\ntouches = [\"internal/ticker/\"]\naudience = \"coordinator\"\n+++\n## Decided\nA budget of 4.\n"), 0o644)
	want := []string{
		"internal/ticker/pr.go changes 4 to 6; memory/retries.md (Retries stay at 4) decided 4",
		"internal/ticker/pr.go adds exponential, backoff; memory/retries.md (Retries stay at 4) ruled it out",
	}
	out := h.ok(thr, "memory", "check", "--project", "demo")
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("check lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "budget.md") {
		t.Errorf("a thread's check shows the coordinator's note:\n%s", out)
	}
	var res struct {
		Conflicts []project.Conflict `json:"conflicts"`
	}
	// The coordinator's check of the thread has its own note too.
	if err := json.Unmarshal([]byte(h.ok(coord, "memory", "check", "--thread", rec.ID, "--json", "--project", "demo")), &res); err != nil || len(res.Conflicts) != 3 {
		t.Fatalf("json: %v %+v", err, res)
	}
	h.expect(2, "", coord, "memory", "check", "--project", "demo")
	h.expect(1, "coordinator-only", thr, "memory", "check", "--thread", "t-0002", "--project", "demo")
	h.expect(2, "", thr, "memory", "find", "x", "--thread", "t-0001", "--project", "demo")

	code, out, errs := h.run(thr, "## Report\nDone.\n## Next\nMerge it\n## Check\nRun it.\n", "report", "--project", "demo")
	if code != 0 {
		t.Fatalf("report: %d %s", code, errs)
	}
	stored, _ := os.ReadFile(thread.Path(p, rec.ID, "REPORT.md"))
	if strings.Contains(out, "budget.md") || strings.Contains(string(stored), "budget.md") {
		t.Errorf("a thread's report has the coordinator's note:\n%s\n%s", out, stored)
	}
	for _, w := range want {
		if !strings.Contains(out, "Memory check: "+w) || !strings.Contains(string(stored), "\nMemory check: "+w+"\n") {
			t.Errorf("report lacks %q:\n%s\n%s", w, out, stored)
		}
	}
	if !strings.Contains(string(stored), "## Check\nRun it.\nMemory check: ") {
		t.Errorf("warnings not after the thread's own ## Check:\n%s", stored)
	}
}

// TestContextThread: tm context --thread previews the memory notes a
// thread's brief carries, for a task before its thread starts.
func TestContextThread(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "task", "add", "Tune the gh poller retries", "--project", "demo")
	p, _ := project.Open("demo")
	os.MkdirAll(p.Path("memory"), 0o755)
	os.WriteFile(p.Path("memory", "retries.md"), []byte("+++\ntitle = \"Retries stay at 4\"\n+++\n## Decided\nKeep 4 for the gh poller.\n"), 0o644)
	os.WriteFile(p.Path("memory", "rules.md"), []byte("+++\ntitle = \"House rules\"\nstanding = true\n+++\nBe brief.\n"), 0o644)

	out := h.ok(coord, "context", "--thread", "T1", "--project", "demo")
	for _, want := range []string{"Memory notes for T1's new thread: 1 standing, 1 matched (of at most 3).", "### House rules (memory/rules.md; standing)",
		"### Retries stay at 4 (memory/retries.md; matched: poller, retries)", "Keep 4 for the gh poller."} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
	var res struct {
		Memory struct {
			Notes []struct {
				Standing bool `json:"standing"`
			} `json:"notes"`
		} `json:"memory"`
	}
	if err := json.Unmarshal([]byte(h.ok(thr, "context", "--thread", "T1", "--json", "--project", "demo")), &res); err != nil || len(res.Memory.Notes) != 2 || !res.Memory.Notes[0].Standing {
		t.Fatalf("json: %v %+v", err, res)
	}
	os.Remove(p.Path("memory", "rules.md"))
	os.Remove(p.Path("memory", "retries.md"))
	if out := h.ok(coord, "context", "--thread", "T1", "--project", "demo"); !strings.Contains(out, "carries no memory notes") {
		t.Errorf("without notes:\n%s", out)
	}
	h.expect(2, "", coord, "context", "--thread", "nope", "--project", "demo")
	h.expect(1, "", coord, "context", "--thread", "t-0009", "--project", "demo")
}

// TestMemoryShowList: tm memory show prints notes in full and tm memory
// list MEMORY.md's line for each; a thread is never shown a note for
// the coordinator.
func TestMemoryShowList(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	if out := h.ok(thr, "memory", "list", "--project", "demo"); !strings.Contains(out, "demo has no memory notes") {
		t.Errorf("empty list:\n%s", out)
	}
	os.MkdirAll(p.Path("memory"), 0o755)
	retries := "---\ntitle: \"Retries stay at 4\"\ntouches:\n  - \"internal/ticker/\"\n---\n## Decided\nKeep 4 for the gh poller.\n"
	os.WriteFile(p.Path("memory", "retries.md"), []byte(retries), 0o644)
	os.WriteFile(p.Path("memory", "budget.md"), []byte("---\ntitle: \"Poller budget\"\naudience: \"coordinator\"\n---\nThe poller's budget.\n"), 0o644)

	if out := h.ok(thr, "memory", "show", "retries", "--project", "demo"); out != retries {
		t.Errorf("show:\n%q", out)
	}
	out := h.ok(coord, "memory", "show", "memory/retries.md", "budget.md", "retries", "--project", "demo")
	if want := "==> memory/retries.md <==\n" + retries + "\n==> memory/budget.md <==\n"; !strings.HasPrefix(out, want) || strings.Count(out, "==>") != 2 {
		t.Errorf("show of two:\n%s", out)
	}
	h.expect(1, "no-note", thr, "memory", "show", "budget", "--project", "demo")
	h.expect(1, "no-note", coord, "memory", "show", "nothing", "--project", "demo")
	h.expect(1, "not-memory", coord, "memory", "show", "../PROJECT.md", "--project", "demo")
	h.expect(2, "", coord, "memory", "show", "--project", "demo")
	h.expect(2, "", coord, "memory", "list", "x", "--project", "demo")

	line := "- [Retries stay at 4](memory/retries.md): Keep 4 for the gh poller. (touches: internal/ticker/)"
	out = h.ok(thr, "memory", "list", "--project", "demo")
	if !strings.Contains(out, line) || strings.Contains(out, "budget") {
		t.Errorf("thread's list:\n%s", out)
	}
	out = h.ok(coord, "memory", "list", "--project", "demo")
	if !strings.Contains(out, line) || !strings.Contains(out, "- [Poller budget](memory/budget.md): The poller's budget.") {
		t.Errorf("coordinator's list:\n%s", out)
	}
	var res struct {
		Notes []struct {
			Path, Title, Line string
		} `json:"notes"`
	}
	if err := json.Unmarshal([]byte(h.ok(thr, "memory", "list", "--json", "--project", "demo")), &res); err != nil || len(res.Notes) != 1 || res.Notes[0].Line != line {
		t.Fatalf("json: %v %+v", err, res)
	}
}
