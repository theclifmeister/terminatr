package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

type canvasFile struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

func readCanvas(t *testing.T, p *project.Project, name string) canvasFile {
	t.Helper()
	b, err := os.ReadFile(p.Path("designs", name+".canvas"))
	if err != nil {
		t.Fatal(err)
	}
	var c canvasFile
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("canvas: %v\n%s", err, b)
	}
	return c
}

func (c canvasFile) node(id string) map[string]any {
	for _, n := range c.Nodes {
		if n["id"] == id {
			return n
		}
	}
	return nil
}

func (c canvasFile) edge(id string) map[string]any {
	for _, e := range c.Edges {
		if e["id"] == id {
			return e
		}
	}
	return nil
}

func writeCanvas(t *testing.T, p *project.Project, name string, c canvasFile) {
	t.Helper()
	b, _ := json.Marshal(c)
	if err := os.WriteFile(p.Path("designs", name+".canvas"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func vaultProject(t *testing.T) *project.Project {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestDesign (T723): a design's note has its page, design task, answers,
// open questions and build; its canvas has the build in columns by
// after depth, coloured by status, with edges along after.
func TestDesign(t *testing.T) {
	p := vaultProject(t)
	human := caller.Caller{Kind: caller.Human}
	s := p.Tasks()
	s.Add(human, []tasks.NewTask{
		{Title: "Design: the <b>view</b>", Status: "review"},
		{Title: "A1", Status: "done"},
		{Title: "A2", Status: "started"},
		{Title: "B", Status: "blocked"},
		{Title: "C", Status: "open"},
		{Title: "Other"},
	})
	name := "net-view"
	for id, after := range map[int][]string{1: nil, 2: nil, 3: {"T2"}, 4: {"T3", "T2"}, 5: {"T6"}} {
		s.SetPlan(human, id, &name, after)
	}
	title, task, url := "Network view", "T1", "https://claude.ai/artifact/abc"
	answers := []string{"Yes, see T3", "No <img src=x>"}
	if _, _, err := p.SetDesign(name, project.DesignEdit{Title: &title, Task: &task, Artifact: &url, Answers: &answers}); err != nil {
		t.Fatal(err)
	}
	q, _ := p.AddQuestion(project.Question{Task: "T1", Question: "Which colour for T4?", Options: []project.QuestionOption{{Label: "Red"}, {Label: "Blue"}}})
	p.AddQuestion(project.Question{Task: "T6", Question: "Not this design's", Options: []project.QuestionOption{{Label: "a"}, {Label: "b"}}})

	if _, _, err := Render(p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.Path("designs", name+".md"))
	if err != nil {
		t.Fatal(err)
	}
	note := string(b)
	for _, want := range []string{
		`name: "net-view"`, `title: "Network view"`, `status: "building"`, `task: "[[demo/tasks/T1|T1]]"`, `task_status: "review"`,
		`artifact: "https://claude.ai/artifact/abc"`, "tasks: 4", "tasks_done: 1", "answers: 2", "open_questions: 1",
		`canvas: "[[demo/designs/net-view.canvas|net-view.canvas]]"`, "tags:\n  - \"design\"\n  - \"needs-you\"", `tm: "owned"`,
		"# Network view\n", "Design task [[demo/tasks/T1|T1]] · review\n[Design page](https://claude.ai/artifact/abc)\n",
		"## Answers\n\n1. Yes, see [[demo/tasks/T3|T3]]\n2. No &lt;img src=x>\n",
		"## Open questions\n\n- " + q.ID + ": Which colour for [[demo/tasks/T4|T4]]?\n",
		"## Build\n\n1 of 4 done.\n\n- [[demo/tasks/T2|T2]] A1 · done\n- [[demo/tasks/T5|T5]] C · open · after [[demo/tasks/T6|T6]]\n- [[demo/tasks/T3|T3]] A2 · started · after [[demo/tasks/T2|T2]]\n- [[demo/tasks/T4|T4]] B · blocked",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("design note has no %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "Not this design's") {
		t.Errorf("another task's question in the note:\n%s", note)
	}

	c := readCanvas(t, p, name)
	if c.Nodes[0]["id"] != "tm-group" || c.Nodes[0]["type"] != "group" || c.Nodes[0]["label"] != "Network view (T1) · decided 2/3" {
		t.Errorf("group: %v", c.Nodes[0])
	}
	if n := c.node("tm-note"); n == nil || n["file"] != "demo/designs/net-view.md" || n["x"] != 0.0 {
		t.Errorf("note node: %v", n)
	}
	if n := c.node("tm-artifact"); n == nil || n["type"] != "link" || n["url"] != url {
		t.Errorf("artifact node: %v", n)
	}
	if n := c.node("tm-" + q.ID); n == nil || n["type"] != "text" || !strings.Contains(n["text"].(string), "[[demo/tasks/T4|T4]]") {
		t.Errorf("question node: %v", n)
	}
	if c.node("tm-T1") != nil || c.node("tm-T6") != nil {
		t.Error("the design task, or a task of no design, has a node")
	}
	for id, want := range map[string][3]any{
		"tm-T2": {440.0, 0.0, "4"}, "tm-T5": {440.0, 160.0, nil},
		"tm-T3": {800.0, 0.0, "5"}, "tm-T4": {1160.0, 0.0, "1"},
	} {
		n := c.node(id)
		if n == nil || n["x"] != want[0] || n["y"] != want[1] || n["color"] != want[2] || n["file"] != "demo/tasks/"+id[3:]+".md" {
			t.Errorf("%s: %v, want x, y, colour %v", id, n, want)
		}
	}
	for _, id := range []string{"tm-e-note-T2", "tm-e-note-T5", "tm-e-T2-T3", "tm-e-T3-T4", "tm-e-T2-T4"} {
		if c.edge(id) == nil {
			t.Errorf("no edge %s: %v", id, c.Edges)
		}
	}
	if len(c.Edges) != 5 {
		t.Errorf("edges: %v", c.Edges)
	}
	if w, _, _ := Render(p); w != 0 {
		t.Errorf("second render wrote %d", w)
	}
}

// TestDesignCanvasArchived (T872): an archived build task has no node
// on the canvas, and a task after it hangs off the note; a dropped one
// is purple, and counts as neither done nor left to build.
func TestDesignCanvasArchived(t *testing.T) {
	p := vaultProject(t)
	human := caller.Caller{Kind: caller.Human}
	s := p.Tasks()
	s.Add(human, []tasks.NewTask{{Title: "A", Status: "done"}, {Title: "B", Status: "dropped"}, {Title: "C", Status: "done"}})
	name := "net-view"
	for id, after := range map[int][]string{1: nil, 2: nil, 3: {"T1"}} {
		s.SetPlan(human, id, &name, after)
	}
	s.Archive(human, 1)
	if _, _, err := Render(p); err != nil {
		t.Fatal(err)
	}
	c := readCanvas(t, p, name)
	if c.node("tm-T1") != nil {
		t.Errorf("archived T1 has a node: %v", c.Nodes)
	}
	if n := c.node("tm-T2"); n == nil || n["color"] != "6" {
		t.Errorf("dropped T2: %v", n)
	}
	if c.edge("tm-e-note-T3") == nil || c.edge("tm-e-T1-T3") != nil {
		t.Errorf("edges: %v", c.Edges)
	}
	b, _ := os.ReadFile(p.Path("designs", name+".md"))
	if note := string(b); !strings.Contains(note, `status: "done"`) || !strings.Contains(note, "tasks_done: 2") || !strings.Contains(note, "[[demo/tasks/T1|T1]] A · done") {
		t.Errorf("design note:\n%s", note)
	}

	// An archived design's note says so, and leaves Needs you even with
	// an open question.
	title, task := "Net", "T3"
	p.SetDesign(name, project.DesignEdit{Title: &title, Task: &task})
	p.AddQuestion(project.Question{Task: "T3", Question: "Still?", Options: []project.QuestionOption{{Label: "a"}, {Label: "b"}}})
	if _, changed, err := p.ArchiveDesign(name, true); err != nil || !changed {
		t.Fatalf("archive: %v %v", changed, err)
	}
	if _, _, err := Render(p); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p.Path("designs", name+".md"))
	if note := string(b); !strings.Contains(note, "archived: true") || strings.Contains(note, "needs-you") || !strings.Contains(note, "open_questions: 1") {
		t.Errorf("archived design note:\n%s", note)
	}
}

// TestDesignCanvasKeeps (T723): tm keeps where the human put its nodes
// and the human's own nodes and edges, takes over a file node of theirs
// that is one of its tasks, and removes only its own.
func TestDesignCanvasKeeps(t *testing.T) {
	p := vaultProject(t)
	human := caller.Caller{Kind: caller.Human}
	s := p.Tasks()
	s.Add(human, []tasks.NewTask{{Title: "A"}, {Title: "B"}, {Title: "C"}})
	name := "plan"
	s.SetPlan(human, 1, &name, nil)
	s.SetPlan(human, 2, &name, []string{"T1"})
	if _, _, err := Render(p); err != nil {
		t.Fatal(err)
	}
	c := readCanvas(t, p, name)
	// The human moves the group and T2, adds a card, an arrow to T2 and
	// a node of their own for T3's note, and drags T3 in later.
	group, t2 := c.node("tm-group"), c.node("tm-T2")
	group["x"], group["y"] = 960.0, 1960.0
	t2["x"], t2["y"], t2["width"] = 5000.0, 5000.0, 333.0
	c.Nodes = append(c.Nodes,
		map[string]any{"id": "abc", "type": "text", "text": "mine #for-tm", "x": -500, "y": 0, "width": 100, "height": 50},
		map[string]any{"id": "f3", "type": "file", "file": "demo/tasks/T3.md", "x": 7000, "y": 7000, "width": 200, "height": 100},
	)
	c.Edges = append(c.Edges,
		map[string]any{"id": "e1", "fromNode": "abc", "toNode": "tm-T2"},
		map[string]any{"id": "e2", "fromNode": "abc", "toNode": "f3"},
	)
	c.Nodes[1]["color"] = "6" // tm recolours its own
	writeCanvas(t, p, name, c)

	s.SetStatus(human, 2, tasks.Done, "")
	s.SetPlan(human, 3, &name, []string{"T2"})
	if w, _, err := Render(p); err != nil || w == 0 {
		t.Fatalf("render: %d, %v", w, err)
	}
	c = readCanvas(t, p, name)
	if g := c.node("tm-group"); g["x"] != 960.0 || g["y"] != 1960.0 {
		t.Errorf("group moved back: %v", g)
	}
	if n := c.node("tm-T2"); n["x"] != 5000.0 || n["y"] != 5000.0 || n["width"] != 333.0 || n["color"] != "4" {
		t.Errorf("T2: %v", n)
	}
	if n := c.node("tm-T1"); n["color"] != nil {
		t.Errorf("T1 keeps the human's colour: %v", n)
	}
	if n := c.node("abc"); n == nil || n["text"] != "mine #for-tm" {
		t.Errorf("the human's card: %v", n)
	}
	if c.node("f3") != nil {
		t.Error("T3's file node kept its own id")
	}
	if n := c.node("tm-T3"); n == nil || n["x"] != 7000.0 || n["file"] != "demo/tasks/T3.md" {
		t.Errorf("T3 taken over: %v", n)
	}
	if e := c.edge("e1"); e == nil || e["toNode"] != "tm-T2" {
		t.Errorf("the human's arrow: %v", e)
	}
	if e := c.edge("e2"); e == nil || e["toNode"] != "tm-T3" {
		t.Errorf("arrow to the taken-over node: %v", e)
	}
	if c.edge("tm-e-T2-T3") == nil {
		t.Errorf("no tm edge T2 → T3: %v", c.Edges)
	}

	// T2 leaves the design: its node goes, and every arrow to it.
	none := ""
	s.SetPlan(human, 2, &none, nil)
	Render(p)
	c = readCanvas(t, p, name)
	if c.node("tm-T2") != nil || c.edge("e1") != nil || c.edge("tm-e-T2-T3") != nil {
		t.Errorf("T2 left: %v %v", c.Nodes, c.Edges)
	}
	if c.node("abc") == nil || c.edge("e2") == nil {
		t.Error("the human's card or arrow went")
	}
	// A new node lands where the layout puts it (T2 still comes after
	// T1), moved with the group.
	s.SetPlan(human, 2, &name, nil)
	Render(p)
	c = readCanvas(t, p, name)
	if n := c.node("tm-T2"); n == nil || n["x"] != 800.0+1000 || n["y"] != 0.0+2000 {
		t.Errorf("new T2: %v", n)
	}

	// A canvas tm can't read is left alone; so is a note tm didn't write.
	os.WriteFile(p.Path("designs", name+".canvas"), []byte("{not json"), 0o644)
	os.WriteFile(p.Path("designs", "other.md"), []byte("# Mine\n"), 0o644)
	s.SetPlan(human, 3, new("other"), nil)
	if _, _, err := Render(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p.Path("designs", name+".canvas")); string(b) != "{not json" {
		t.Errorf("broken canvas rewritten: %s", b)
	}
	if b, _ := os.ReadFile(p.Path("designs", "other.md")); string(b) != "# Mine\n" {
		t.Errorf("someone's note rewritten: %s", b)
	}
	if _, err := os.Stat(p.Path("designs", "other.canvas")); err != nil {
		t.Errorf("no canvas beside someone's note: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Path("designs"), name+".md")); err != nil {
		t.Errorf("a design's note removed: %v", err)
	}
}
