package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestRender (T718): with the vault on, each task, archived task and
// thread gets its note, with its properties, links and the thread's
// guarded words; a second render writes nothing, a change rewrites the
// one note, and archiving or unarchiving a task keeps its note's path
// (T729).
func TestRender(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	if On() {
		t.Fatal("on without the mark")
	}
	os.MkdirAll(filepath.Join(dir, "state"), 0o700)
	os.WriteFile(filepath.Join(dir, "state", "vault"), nil, 0o600)
	if !On() {
		t.Fatal("not on with the mark")
	}
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	s := p.Tasks()
	if _, err := s.Add(human, []tasks.NewTask{
		{Title: "Design it", Status: "done"},
		{Title: "Build <b>it</b>", Status: "review", Notes: "After T1, see t-0001 and `T1`.\n<img src=x>", Steps: []string{"one", "two"}},
		{Title: "Old", Status: "done"},
	}); err != nil {
		t.Fatal(err)
	}
	design := "network-view"
	s.SetPlan(human, 2, &design, []string{"T1"})
	r, _ := thread.Create(p, thread.Record{Title: "Build it", Task: "T2", State: thread.Running, Agent: "claude", Branch: "tm/demo/x"})
	s.SetThread(human, 2, r.ID)
	os.WriteFile(thread.Path(p, r.ID, "brief.md"), []byte("# Brief\n"), 0o644)
	report := "PR: https://github.com/o/r/pull/7\n## Report\n<img src=\"https://x.y/a.png\">\n## Next\nMerge\n"
	if _, err := thread.StoreReport(p, r.ID, report, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	// A report stored before the guard, as it was.
	os.WriteFile(thread.Path(p, r.ID, "REPORT.md"), []byte(report), 0o644)
	if _, err := s.Archive(human, 3); err != nil {
		t.Fatal(err)
	}

	// Four notes, and T2's design's note and canvas.
	w, rm, err := Render(p)
	if err != nil || w != 6 || rm != 0 {
		t.Fatalf("render: %d written, %d removed, %v", w, rm, err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(p.Path(filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	task := read("tasks/T2.md")
	for _, want := range []string{
		`id: "T2"`, `project: "[[demo/PROJECT|demo]]"`, `status: "review"`, `group: "Needs you"`,
		`thread: "[[demo/threads/` + r.ID + `/` + r.ID + `|` + r.ID + `]]"`, `pr: "https://github.com/o/r/pull/7"`,
		`design: "[[demo/designs/network-view|network-view]]"`, "after:\n  - \"[[demo/tasks/T1|T1]]\"",
		"steps: 2", "steps_done: 0", "tags:\n  - \"task\"\n  - \"needs-you\"", `tm: "owned"`, "cssclasses:\n  - \"tm-owned\"",
		"# T2 Build &lt;b>it&lt;/b>\n",
		"After [[demo/tasks/T1|T1]], see [[demo/threads/t-0001/t-0001|t-0001]] and `T1`.\n&lt;img src=x>",
		"## Steps\n\n- [ ] one\n- [ ] two\n",
		"## Thread\n\n![[demo/threads/" + r.ID + "/" + r.ID + "#Report]]\n",
	} {
		if !strings.Contains(task, want) {
			t.Errorf("task note has no %q:\n%s", want, task)
		}
	}
	th := read("threads/" + r.ID + "/" + r.ID + ".md")
	for _, want := range []string{
		`task: "[[demo/tasks/T2|T2]]"`, `state: "running"`, `agent: "claude"`, `branch: "tm/demo/x"`, `pr: "https://github.com/o/r/pull/7"`,
		"reports: 1", "tags:\n  - \"thread\"",
		"- [[demo/threads/" + r.ID + "/brief|brief]]", "- [[demo/threads/" + r.ID + "/REPORT|report 1]] (latest)",
		"## Report\n\n> [!quote] Report 1 from " + r.ID + " · the thread's words\n> PR: https://github.com/o/r/pull/7\n> ## Report\n> &lt;img",
	} {
		if !strings.Contains(th, want) {
			t.Errorf("thread note has no %q:\n%s", want, th)
		}
	}
	if old := read("tasks/T3.md"); !strings.Contains(old, "archived: true") {
		t.Errorf("archived note:\n%s", old)
	}

	if w, rm, err := Render(p); err != nil || w != 0 || rm != 0 {
		t.Fatalf("second render: %d written, %d removed, %v", w, rm, err)
	}
	stamp := Stamp(p)
	s.StepCheck(human, 2, 1, true)
	if Stamp(p) == stamp {
		t.Fatal("stamp unchanged by a step")
	}
	if w, _, err := Render(p); err != nil || w != 1 || !strings.Contains(read("tasks/T2.md"), "- [x] one") {
		t.Fatalf("after a step: %d written, %v", w, err)
	}
	s.Archive(human, 1)
	if w, rm, err := Render(p); err != nil || w != 1 || rm != 0 || !strings.Contains(read("tasks/T1.md"), "archived: true") {
		t.Fatalf("after archiving: %d written, %d removed, %v", w, rm, err)
	}
	if _, err := os.Stat(p.Path("tasks", "archive")); err == nil {
		t.Fatal("an archive folder of notes")
	}
	s.Unarchive(human, 1)
	if w, rm, err := Render(p); err != nil || w != 1 || rm != 0 || strings.Contains(read("tasks/T1.md"), "archived:") {
		t.Fatalf("after unarchiving: %d written, %d removed, %v", w, rm, err)
	}
	// A note someone removed is written again.
	os.Remove(p.Path("tasks", "T2.md"))
	if w, _, _ := Render(p); w != 1 {
		t.Fatalf("removed note: %d written", w)
	}
	// An archived task is off the board: its note is in no board group,
	// Backlog or Needs you, whatever its status (T872).
	s.Add(human, []tasks.NewTask{{Title: "Never mind"}, {Title: "Stuck", Status: "blocked"}, {Title: "Not this", Status: "dropped"}})
	s.Archive(human, 4)
	s.Archive(human, 5)
	if _, _, err := Render(p); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"T4", "T5"} {
		if n := read("tasks/" + ref + ".md"); !strings.Contains(n, `group: "Archived"`) || strings.Contains(n, "needs-you") {
			t.Errorf("archived %s:\n%s", ref, n)
		}
	}
	if n := read("tasks/T6.md"); !strings.Contains(n, `status: "dropped"`) || !strings.Contains(n, `group: "Dropped"`) {
		t.Errorf("dropped:\n%s", n)
	}
}

// TestRenderOldArchive (T729): an archived task's note where a tm before
// T729 put it, tasks/archive/T<n>.md, is removed with its folder, and
// the note is at tasks/T<n>.md; the links to it then resolve.
func TestRenderOldArchive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	os.MkdirAll(filepath.Join(dir, "state"), 0o700)
	os.WriteFile(filepath.Join(dir, "state", "vault"), nil, 0o600)
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	s := p.Tasks()
	s.Add(human, []tasks.NewTask{{Title: "Old", Status: "done"}})
	s.Archive(human, 1)
	os.MkdirAll(p.Path("tasks", "archive"), 0o755)
	os.WriteFile(p.Path("tasks", "archive", "T1.md"), []byte("old\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "state", "vault.json"), []byte(`{"files": {"demo/tasks/archive/T1.md": "x"}}`), 0o600)
	os.MkdirAll(p.Path("memory"), 0o755)
	os.WriteFile(p.Path("memory", "x.md"), []byte("See [[demo/tasks/T1|T1]].\n"), 0o644)
	if w, rm, err := Render(p); err != nil || w != 1 || rm != 1 {
		t.Fatalf("render: %d written, %d removed, %v", w, rm, err)
	}
	if _, err := os.Stat(p.Path("tasks", "archive")); err == nil {
		t.Fatal("old archive folder left")
	}
	if b, err := os.ReadFile(p.Path("tasks", "T1.md")); err != nil || !strings.Contains(string(b), "archived: true") {
		t.Fatalf("note: %v\n%s", err, b)
	}
	if broken, err := Links(); err != nil || len(broken) != 0 {
		t.Fatalf("doctor: %v %+v", err, broken)
	}
}
