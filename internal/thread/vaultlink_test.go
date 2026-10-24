package thread

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
)

func linksOn(t *testing.T) {
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
}

// TestStatusLinks (T717): STATUS.md links its thread and task; read
// back, they are the ids.
func TestStatusLinks(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	linksOn(t)
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := Create(p, Record{Title: "x", Task: "T4", State: Running})
	st, err := UpdateStatus(p, r.ID, func(st *Status) error { st.Activity, st.NeedsYou = "a", "which?"; return nil })
	if err != nil || st.Thread != r.ID || st.Task != "T4" {
		t.Fatalf("%+v %v", st, err)
	}
	data, _ := os.ReadFile(Path(p, r.ID, "STATUS.md"))
	for _, w := range []string{`thread: "[[demo/threads/` + r.ID + `/` + r.ID + `|` + r.ID + `]]"`, `task: "[[demo/tasks/T4|T4]]"`, `tm: "owned"`, "tags:\n  - \"needs-you\""} {
		if !strings.Contains(string(data), w) {
			t.Errorf("no %s in\n%s", w, data)
		}
	}
	back, err := ReadStatus(p, r.ID)
	if err != nil || back.Thread != r.ID || back.Task != "T4" || back.Activity != "a" {
		t.Fatalf("%+v %v", back, err)
	}
}

// TestBriefLinks (T717): brief.md links the task, the notes it carries
// and the follow-ups file; without links it reads as before.
func TestBriefLinks(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	writeNote(t, p, "standing.md", "---\ntitle: \"Never touch prod\"\nstanding: true\n---\n## Decided\nStaging only.\n")
	r, _ := Create(p, Record{Title: "x", Task: "T4", Worktree: "/wt/t-0001"})
	brief, _, err := WriteBrief(p, r, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(brief)
	if !strings.Contains(string(b), "working on task T4.") || !strings.Contains(string(b), "(memory/standing.md; standing)") ||
		!strings.Contains(string(b), "Follow-ups from the coordinator are appended to") || strings.Contains(string(b), "[[") {
		t.Errorf("brief without links:\n%s", b)
	}
	linksOn(t)
	if brief, _, err = WriteBrief(p, r, false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(brief)
	for _, w := range []string{"working on task [[demo/tasks/T4|T4]].",
		"### Never touch prod ([[demo/memory/standing|memory/standing.md]]; standing)",
		"Follow-ups from the coordinator ([[demo/threads/" + r.ID + "/task|task.md]]) are appended to " + Path(p, r.ID, "task.md")} {
		if !strings.Contains(string(b), w) {
			t.Errorf("no %q in\n%s", w, b)
		}
	}
}
