package modaction

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// state is a fake live state: project alpha with a board, items asking
// about some of its tasks, and running sessions.
type state struct {
	board    map[string]*tasks.Task
	asked    map[string]string
	sessions map[string]bool
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

func live() state {
	return state{
		board: map[string]*tasks.Task{
			"T1": {ID: 1, Title: "Open", Status: tasks.Open},
			"T2": {ID: 2, Title: "Review", Status: tasks.Review},
			"T3": {ID: 3, Title: "Done", Status: tasks.Done},
			"T4": {ID: 4, Title: "Started", Status: tasks.Started},
			"T5": {ID: 5, Title: "Asked", Status: tasks.Review},
		},
		asked:    map[string]string{"T5": project.KindAccept},
		sessions: map[string]bool{"s-1": true},
	}
}

func task(ref string) Target { return Target{Project: "alpha", Task: ref} }

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
		{"task.coordinator", Target{Project: "alpha"}, ""},
		{"task.coordinator", Target{Project: "alpha", Task: "T1"}, ""},
		{"project.activate", Target{Project: "alpha"}, ""},
		{"project.activate", Target{Project: "beta"}, "no project beta"},
		{"session.attach", Target{Session: "s-1"}, ""},
		{"session.attach", Target{Session: "s-2"}, "not running"},
		// Wrong shapes.
		{"task.accept", Target{Project: "alpha", Task: "12"}, "takes a target of"},
		{"task.accept", Target{Project: "alpha", Task: "T02"}, "takes a target of"},
		{"task.accept", Target{Task: "T2"}, "takes a target of"},
		{"task.accept", Target{Project: "alpha", Task: "T2", Session: "s-1"}, "takes a target of"},
		{"session.attach", Target{Session: "s-1", Project: "alpha"}, "takes a target of"},
		{"project.activate", Target{}, "takes a target of"},
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
		case OnTask, OnProject, OnSession:
		default:
			t.Errorf("%s: target kind %q", a.ID, a.On)
		}
		writes := askKind(a.ID) != "" || a.ID == "project.activate"
		if writes && !a.Confirm {
			t.Errorf("%s changes state but doesn't ask first", a.ID)
		}
	}
	if _, ok := Lookup("task.delegate"); !ok {
		t.Error("Lookup task.delegate")
	}
	if _, ok := Lookup("task.merge"); ok {
		t.Error("Lookup of an unknown id")
	}
}

// TestKinds: the ask kinds are the inbox's.
func TestKinds(t *testing.T) {
	if KindDelegate != project.KindDelegate || KindAccept != project.KindAccept || KindSendBack != project.KindSendBack {
		t.Error("the ask kinds differ from the inbox's")
	}
}

func TestTaskRef(t *testing.T) {
	for s, want := range map[string]bool{"T1": true, "T260": true, "T": false, "t1": false, "T01": false, "T1a": false, "T-1": false, "T+1": false, "T1234567890": false} {
		if TaskRef(s) != want {
			t.Errorf("TaskRef(%q) = %v", s, !want)
		}
	}
}
