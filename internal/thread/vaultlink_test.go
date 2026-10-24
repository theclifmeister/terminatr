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
	st, err := UpdateStatus(p, r.ID, func(st *Status) error { st.Activity = "a"; return nil })
	if err != nil || st.Thread != r.ID || st.Task != "T4" {
		t.Fatalf("%+v %v", st, err)
	}
	data, _ := os.ReadFile(Path(p, r.ID, "STATUS.md"))
	for _, w := range []string{`thread: "[[demo/threads/` + r.ID + `/` + r.ID + `|` + r.ID + `]]"`, `task: "[[demo/tasks/T4|T4]]"`} {
		if !strings.Contains(string(data), w) {
			t.Errorf("no %s in\n%s", w, data)
		}
	}
	back, err := ReadStatus(p, r.ID)
	if err != nil || back.Thread != r.ID || back.Task != "T4" || back.Activity != "a" {
		t.Fatalf("%+v %v", back, err)
	}
}
