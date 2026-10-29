package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestDesignsTab: tab 0 lists each design with its status, title,
// build, design task, open questions, answers and page; enter opens one
// with its answers, questions and build in after order; enter on a
// question has the coordinator open its questions, on a build task
// shows it (its design and after), on an archived one says so.
func TestDesignsTab(t *testing.T) {
	src, m := popupData(t)
	b := src.board.Tasks
	b[0].Design, b[1].Design = "vault", "vault"
	archived := &tasks.Task{ID: 9, Title: "Old groundwork", Status: tasks.Done, Design: "vault"}
	b[0].After = []int{9}
	src.designs = []project.DesignBuild{
		{Design: project.Design{Name: "net", Title: ""}},
		{Design: project.Design{Name: "vault", Title: "Obsidian vault", Task: "T2", Artifact: "https://example.com/vault",
			Answers: []string{"YAML front matter", "One note per task"}},
			DesignTask: b[1], Build: []*tasks.Task{b[0], archived},
			Questions: []project.Question{{ID: "Q3", Task: "T2", Header: "Canvas", Question: "Columns by depth?"}}},
	}
	feed(m, keyPress(m, "A"))
	pv := m.top().(*projectView)
	keyPress(m, "0")
	if pv.tab != tabDesigns {
		t.Fatalf("0: tab %d", pv.tab)
	}
	out := screen(m)
	for _, want := range []string{"0 designs", "designing", "net", "no build tasks", "building", "Obsidian vault", "1/2 built",
		"design task T2 ready · 1 open question · 2 answers · https://example.com/vault", "0 answers", "enter open"} {
		if !strings.Contains(out, want) {
			t.Errorf("designs tab lacks %q:\n%s", want, out)
		}
	}
	keyPress(m, "down")
	keyPress(m, "enter")
	dv, ok := m.top().(*designView)
	if !ok {
		t.Fatalf("enter opened %T", m.top())
	}
	out = screen(m)
	for _, want := range []string{"Design · alpha", "building · vault", "T2 Ship it · ready", "ANSWERS 2", "1. YAML front matter",
		"2. One note per task", "OPEN QUESTIONS 1", "Q3 Canvas: Columns by depth?", "BUILD 1/2 built", "T1    Write the README",
		"after T9", "T9    Old groundwork", "enter show the task"} {
		if !strings.Contains(out, want) {
			t.Errorf("design lacks %q:\n%s", want, out)
		}
	}
	// After order: T9 before T1, which comes after it.
	if a, c := strings.Index(out, "T9    Old"), strings.Index(out, "T1    Write"); a < 0 || a > c {
		t.Errorf("build not in after order:\n%s", out)
	}
	// The question: the coordinator opens its questions.
	keyPress(m, "down")
	if out := screen(m); !strings.Contains(out, "enter answer in the coordinator") {
		t.Errorf("question's keys:\n%s", out)
	}
	drain(t, m, keyPress(m, "enter"))
	if !slices.Equal(src.answered, []string{"alpha"}) {
		t.Errorf("answered %v", src.answered)
	}
	// T9, archived, is first in the build: it says so.
	keyPress(m, "down")
	keyPress(m, "enter")
	if m.top() != dv || !strings.Contains(m.msg, "T9 is archived") {
		t.Errorf("archived: %T %q", m.top(), m.msg)
	}
	// T1 shows, with its design and after.
	keyPress(m, "down")
	keyPress(m, "enter")
	if _, ok := m.top().(*taskView); !ok {
		t.Fatalf("enter on T1 opened %T", m.top())
	}
	out = screen(m)
	for _, want := range []string{"design    vault", "after     T9"} {
		if !strings.Contains(out, want) {
			t.Errorf("task view lacks %q:\n%s", want, out)
		}
	}
	keyPress(m, "esc")
	keyPress(m, "esc")
	if m.top() != pv {
		t.Fatalf("esc twice: %T", m.top())
	}
}

// TestDesignsTabEmpty: without designs the tab says how one comes.
func TestDesignsTabEmpty(t *testing.T) {
	_, m := popupData(t)
	feed(m, keyPress(m, "A"))
	keyPress(m, "0")
	if out := screen(m); !strings.Contains(out, "no designs") || strings.Contains(out, "enter open") {
		t.Errorf("empty designs tab:\n%s", out)
	}
}
