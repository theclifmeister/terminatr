package modaction

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/tasks"
)

// state is a fake live state: project alpha with a board, items asking
// about some of its tasks, and running sessions.
type state struct {
	board    map[string]*tasks.Task
	asked    map[string]string
	sessions map[string]bool
	menus    map[string][]MenuQuestion
}

func (s state) Project(slug string) bool { return slug == "alpha" }
func (s state) Task(slug, ref string) *tasks.Task {
	if slug != "alpha" {
		return nil
	}
	return s.board[ref]
}
func (s state) Asked(slug, ref string) string { return s.asked[ref] }
func (s state) Session(id string) bool        { return s.sessions[id] }
func (s state) Menu(id string) []MenuQuestion { return s.menus[id] }
func (s state) Note(slug, path string) bool   { return slug == "alpha" && path == "memory/a.md" }

func live() state {
	return state{
		board: map[string]*tasks.Task{
			"T1": {ID: 1, Title: "Open", Status: tasks.Open},
			"T2": {ID: 2, Title: "Review", Status: tasks.Review},
			"T3": {ID: 3, Title: "Done", Status: tasks.Done},
			"T4": {ID: 4, Title: "Started", Status: tasks.Started},
			"T5": {ID: 5, Title: "Asked", Status: tasks.Review},
		},
		asked:    map[string]string{"T5": KindAccept},
		sessions: map[string]bool{"s-1": true, "s-3": true},
		menus: map[string][]MenuQuestion{"s-1": {
			{Question: "Which colour?", Options: []string{"Red", "Blue"}},
			{Question: "Done?", Answered: true, Options: []string{"Yes", "No"}},
			{Question: "Which fruits?", Multi: true, Options: []string{"Apple", "Pear"}},
		}},
	}
}

func task(ref string) Target { return Target{Project: "alpha", Task: ref} }

func answer(sess, q, option string) Target { return Target{Session: sess, Question: q, Option: option} }

// TestWhy: the precondition is checked against live state, for every
// action, and a target of the wrong shape is refused.
func TestWhy(t *testing.T) {
	st := live()
	for _, c := range []struct {
		id   string
		t    Target
		want string // "" available; else a part of why not
	}{
		{"task.show", task("T1"), ""},
		{"task.show", task("T9"), "has no task T9"},
		{"task.delegate", task("T1"), ""},
		{"task.delegate", task("T2"), "in review"},
		{"task.delegate", task("T4"), "started"},
		{"task.accept", task("T2"), ""},
		{"task.accept", task("T1"), "not in review"},
		{"task.accept", task("T5"), "already waiting on the coordinator"},
		{"task.send-back", task("T3"), ""},
		{"task.send-back", task("T1"), "not in review or done"},
		{"task.new", Target{Project: "alpha"}, ""},
		{"task.new", Target{Project: "beta"}, "no project beta"},
		{"task.coordinator", Target{Project: "alpha"}, ""},
		{"task.coordinator", Target{Project: "alpha", Task: "T1"}, ""},
		{"project.activate", Target{Project: "alpha"}, ""},
		{"project.activate", Target{Project: "beta"}, "no project beta"},
		{"session.attach", Target{Session: "s-1"}, ""},
		{"session.attach", Target{Session: "s-2"}, "not running"},
		{"session.clear", Target{Session: "s-1"}, ""},
		{"session.clear", Target{Session: "s-2"}, "not running"},
		{"question.answer", answer("s-1", "Which colour?", "Blue"), ""},
		{"question.answer", answer("s-2", "Which colour?", "Blue"), "not running"},
		{"question.answer", answer("s-3", "Which colour?", "Blue"), "no open question"},
		{"question.answer", answer("s-1", "Which color?", "Blue"), "doesn't ask that question"},
		{"question.answer", answer("s-1", "Which colour?", "blue"), `no option "blue"`},
		{"question.answer", answer("s-1", "Which colour?", "2"), `no option "2"`},
		{"question.answer", answer("s-1", "Done?", "Yes"), "answered already"},
		{"question.answer", answer("s-1", "Which fruits?", "Pear"), "answered in the session"},
		{"memory.edit", Target{Project: "alpha", Path: "memory/a.md"}, ""},
		{"memory.rm", Target{Project: "alpha", Path: "memory/a.md"}, ""},
		{"memory.rm", Target{Project: "alpha", Path: "memory/b.md"}, "has no note memory/b.md"},
		{"memory.edit", Target{Project: "beta", Path: "memory/a.md"}, "no project beta"},
		{"memory.new", Target{Project: "alpha"}, ""},
		{"memory.new", Target{Project: "alpha", Path: "memory/a.md"}, "takes no path"},
		{"memory.rm", Target{Project: "alpha", Path: "MEMORY.md"}, "takes a target of"},
		{"memory.rm", Target{Project: "alpha", Path: "memory/../PROJECT.md"}, "takes a target of"},
		{"memory.rm", Target{Project: "alpha", Path: "memory/sub/a.md"}, "takes a target of"},
		{"memory.rm", Target{Project: "alpha", Path: "memory/.a.md"}, "takes a target of"},
		{"memory.edit", Target{Project: "alpha"}, "takes a target of"},
		{"memory.edit", Target{Project: "alpha", Path: "memory/a.md", Task: "T1"}, "takes a target of"},
		{"task.show", Target{Project: "alpha", Task: "T1", Path: "memory/a.md"}, "takes no path"},
		// Wrong shapes.
		{"task.accept", Target{Project: "alpha", Task: "12"}, "takes a target of"},
		{"task.accept", Target{Project: "alpha", Task: "T02"}, "takes a target of"},
		{"task.accept", Target{Task: "T2"}, "takes a target of"},
		{"task.accept", Target{Project: "alpha", Task: "T2", Session: "s-1"}, "takes a target of"},
		{"session.attach", Target{Session: "s-1", Project: "alpha"}, "takes a target of"},
		{"project.activate", Target{}, "takes a target of"},
		{"session.clear", Target{Session: "s-1", Task: "T1"}, "takes a target of"},
		{"session.attach", Target{Session: "s-1", Option: "Blue"}, "takes no question or option"},
		{"task.accept", Target{Project: "alpha", Task: "T2", Question: "Which colour?"}, "takes no question or option"},
		{"question.answer", Target{Session: "s-1", Question: "Which colour?"}, "takes a target of"},
		{"question.answer", Target{Session: "s-1", Option: "Blue"}, "takes a target of"},
		{"question.answer", Target{Session: "s-1", Question: "Which colour?", Option: "Blue", Project: "alpha"}, "takes a target of"},
		// Not bindable: the core's settings, merges and the like.
		{"settings.open", Target{}, "not an action a mod may bind"},
		{"repo.remove", Target{Project: "alpha"}, "not an action a mod may bind"},
		{"thread.merge", Target{Project: "alpha"}, "not an action a mod may bind"},
	} {
		got := Why(c.id, st, c.t)
		switch {
		case c.want == "" && got != "":
			t.Errorf("%s %+v: %q, want available", c.id, c.t, got)
		case c.want != "" && !strings.Contains(got, c.want):
			t.Errorf("%s %+v: %q, want %q", c.id, c.t, got, c.want)
		}
	}
}

// TestTable: ids are unique and well formed, each takes a known target,
// and every action that writes asks first.
func TestTable(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range Actions {
		if seen[a.ID] {
			t.Errorf("%s twice", a.ID)
		}
		seen[a.ID] = true
		if a.Label == "" || a.Key == "" || a.Precondition == "" {
			t.Errorf("%s: label, key and precondition are all needed", a.ID)
		}
		switch a.On {
		case OnTask, OnProject, OnSession, OnQuestion, OnNote:
		default:
			t.Errorf("%s: target kind %q", a.ID, a.On)
		}
		writes := askKind(a.ID) != "" || a.ID == "project.activate" || a.ID == "memory.rm" || a.Consent != ""
		if writes && !a.Confirm {
			t.Errorf("%s changes state but doesn't ask first", a.ID)
		}
		if a.On == OnQuestion && a.Consent == "" {
			t.Errorf("%s answers for the human but needs no consent", a.ID)
		}
	}
	if _, ok := Lookup("task.delegate"); !ok {
		t.Error("Lookup task.delegate")
	}
	if _, ok := Lookup("task.merge"); ok {
		t.Error("Lookup of an unknown id")
	}
}

// TestAnswer: the index of the question answered.
func TestAnswer(t *testing.T) {
	menu := live().menus["s-1"]
	if i, why := Answer(menu, "Which colour?", "Red"); i != 0 || why != "" {
		t.Errorf("Answer = %d, %q", i, why)
	}
	if i, why := Answer(menu, "Which colour?", "Green"); i != -1 || why == "" {
		t.Errorf("Answer of no option = %d, %q", i, why)
	}
}

func TestTaskRef(t *testing.T) {
	for s, want := range map[string]bool{"T1": true, "T260": true, "T": false, "t1": false, "T01": false, "T1a": false, "T-1": false, "T+1": false, "T1234567890": false} {
		if TaskRef(s) != want {
			t.Errorf("TaskRef(%q) = %v", s, !want)
		}
	}
}
