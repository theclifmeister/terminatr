package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
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
	for _, want := range []string{"memory/retries.md  Retries stay at 4  (score 8: retries, poller, internal/ticker/pr.go)", "touches internal/ticker/ · standing · from T481", p.Dir} {
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
	h.expect(2, "", coord, "memory", "find", "gh", "--limit", "0", "--project", "demo")
	h.expect(1, "other-project", caller.Caller{Kind: caller.Thread, Project: "other", Thread: "t-0001"}, "memory", "find", "gh", "--project", "demo")

	if out := h.ok(thr, "memory", "template"); out != project.NoteTemplate {
		t.Errorf("template:\n%s", out)
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
		"### Retries stay at 4 (memory/retries.md; matched: gh, poller, retries)", "Keep 4 for the gh poller."} {
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
