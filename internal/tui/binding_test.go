package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// press is a stable EventAction for action id on target, as a mod's
// view hands the console.
func boundPress(id string, target map[string]string, stable bool) *widget.Event {
	return &widget.Event{Kind: widget.EventAction, Widget: "tasks", Row: "r", Action: &widget.Action{ID: id, Target: target}, Stable: stable}
}

// pressed runs a press through the console as the program would: the
// live board is loaded, then the binding is checked and run.
func pressed(m *dash, ev *widget.Event) tea.Cmd {
	cmd := m.pressBinding("lab", ev)
	if cmd == nil {
		return nil
	}
	_, next := m.Update(cmd())
	return next
}

// TestBinding: a mod's bound action runs only from a stable press, after
// the console re-checks it against the live board, through the core's
// own confirmation naming the target from live state; the mod hears of
// it as action.done.
func TestBinding(t *testing.T) {
	src, m := popupData(t)
	alpha := func(task string) map[string]string { return map[string]string{"project": "alpha", "task": task} }

	// A press on a row that just changed counts for nothing.
	if cmd := m.pressBinding("lab", boundPress("task.delegate", alpha("T2"), false)); cmd != nil || !strings.Contains(m.msg, "just changed") {
		t.Fatalf("unstable press: %v %q", cmd, m.msg)
	}

	// D on T2 (ready): the core asks, naming T2 by the board's title,
	// whatever the mod's row said.
	drain(t, m, pressed(m, boundPress("task.delegate", alpha("T2"), true)))
	cv, ok := m.top().(*confirmView)
	if !ok || cv.question != "Delegate T2 to the coordinator? It starts a thread for T2 Ship it." {
		t.Fatalf("delegate opened %T %+v", m.top(), cv)
	}
	if len(src.done) != 0 || len(src.delegated) != 0 {
		t.Fatalf("acted before the human said yes: %v %v", src.done, src.delegated)
	}
	drain(t, m, keyPress(m, "y"))
	if len(src.delegated) != 1 || src.delegated[0] != "alpha T2" || len(src.done) != 1 || src.done[0] != "lab task.delegate alpha/T2 true" {
		t.Fatalf("after y: %v %v", src.delegated, src.done)
	}

	// n: nothing runs, and the mod hears nothing.
	drain(t, m, pressed(m, boundPress("task.delegate", alpha("T2"), true)))
	if _, ok := m.top().(*confirmView); ok {
		keyPress(m, "n")
	}

	// A stale or lying row: accept on a started task, a task that
	// doesn't exist, an action no mod may bind.
	for _, c := range []struct {
		id     string
		target map[string]string
		why    string
	}{
		{"task.accept", alpha("T1"), "T1 is started, not in review"},
		{"task.accept", alpha("T9"), "alpha has no task T9"},
		{"task.delegate", map[string]string{"project": "nope", "task": "T1"}, "no project nope"},
	} {
		src.done = nil
		drain(t, m, pressed(m, boundPress(c.id, c.target, true)))
		if _, ok := m.top().(*confirmView); ok {
			t.Fatalf("%s %v asked", c.id, c.target)
		}
		if !strings.Contains(m.msg, "not available: "+c.why) || len(src.done) != 1 || !strings.HasSuffix(src.done[0], " false") {
			t.Errorf("%s %v: %q, done %v", c.id, c.target, m.msg, src.done)
		}
	}
	// A project the mod doesn't run for: refused before anything runs,
	// and the mod hears nothing.
	src.done = nil
	drain(t, m, pressed(m, boundPress("project.activate", map[string]string{"project": "beta"}, true)))
	if _, ok := m.top().(*confirmView); ok || !strings.Contains(m.msg, "doesn't run for project beta") || len(src.done) != 0 {
		t.Errorf("out of reach: %T %q %v", m.top(), m.msg, src.done)
	}
	src.done = nil
	drain(t, m, pressed(m, boundPress("settings.open", nil, true)))
	if !strings.Contains(m.msg, "not an action a mod may bind") || len(src.done) != 0 {
		t.Errorf("settings.open: %q, done %v", m.msg, src.done)
	}
	if len(src.asked) != 0 || len(src.delegated) != 1 {
		t.Errorf("a refused press acted: %v %v", src.asked, src.delegated)
	}

	// Navigation needs no confirmation: attach to a running session.
	src.done = nil
	cmd := pressed(m, boundPress("session.attach", map[string]string{"session": "s-9"}, true))
	msg, _ := cmd().(actionMsg)
	if msg.attach != "s-9" || len(src.done) != 1 || src.done[0] != "lab session.attach /s-9 true" {
		t.Errorf("attach: %+v, done %v", msg, src.done)
	}
}

// TestBoundHints: a view's hints for the cursor's row are the core's
// labels and the console's keys.
func TestBoundHints(t *testing.T) {
	w, err := widget.Decode([]byte(`{"kind":"list","id":"tasks","rows":[{"id":"T2","spans":[{"text":"T2"}],
		"actions":[{"id":"task.delegate","target":{"project":"alpha","task":"T2"}},{"id":"task.show","target":{"project":"alpha","task":"T2"}},{"id":"project.activate","target":{"project":"alpha"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	v := widget.NewView()
	v.SetBindKeys(BindKeys)
	v.Set(w)
	if got := boundHints(v); got != "D delegate · enter show · space activate / deactivate" {
		t.Errorf("hints %q", got)
	}
}

// TestBindingClearAnswer: session.clear and question.answer run only
// after tm's own confirmation, which names the session and, for an
// answer, the live question and option; an option the live menu doesn't
// have is not available, and nothing is sent.
func TestBindingClearAnswer(t *testing.T) {
	src, m := popupData(t)
	for i := range m.data.Sessions {
		if m.data.Sessions[i].ID == "s-9" {
			m.data.Sessions[i].Question = &proto.Question{Questions: []proto.QuestionItem{
				{Question: "Which colour?", Options: []proto.QuestionOption{{Label: "Red"}, {Label: "Blue"}}},
			}}
		}
	}

	drain(t, m, pressed(m, boundPress("session.clear", map[string]string{"session": "s-9"}, true)))
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.HasPrefix(cv.question, "Clear alpha t-0003's conversation?") || len(src.cleared) != 0 {
		t.Fatalf("clear opened %T %+v, cleared %v", m.top(), cv, src.cleared)
	}
	drain(t, m, keyPress(m, "n"))
	if len(src.cleared) != 0 || len(src.done) != 0 {
		t.Fatalf("n cleared: %v %v", src.cleared, src.done)
	}
	drain(t, m, pressed(m, boundPress("session.clear", map[string]string{"session": "s-9"}, true)))
	drain(t, m, keyPress(m, "y"))
	if len(src.cleared) != 1 || src.cleared[0] != "s-9" || len(src.done) != 1 || src.done[0] != "lab session.clear /s-9 true" {
		t.Fatalf("after y: %v %v", src.cleared, src.done)
	}

	src.done = nil
	answer := func(option string) map[string]string {
		return map[string]string{"session": "s-9", "question": "Which colour?", "option": option}
	}
	drain(t, m, pressed(m, boundPress("question.answer", answer("Green"), true)))
	if _, ok := m.top().(*confirmView); ok || !strings.Contains(m.msg, `not available: that question has no option "Green"`) {
		t.Fatalf("a lying row: %T %q", m.top(), m.msg)
	}
	drain(t, m, pressed(m, boundPress("question.answer", answer("Blue"), true)))
	cv, ok = m.top().(*confirmView)
	if !ok || cv.question != `Answer alpha t-0003's question "Which colour?" with "Blue"? It goes to the agent as your answer.` {
		t.Fatalf("answer opened %T %+v", m.top(), cv)
	}
	if len(src.answers) != 0 {
		t.Fatalf("answered before yes: %v", src.answers)
	}
	drain(t, m, keyPress(m, "y"))
	if len(src.answers) != 1 || src.answers[0] != "s-9 0 Which colour? → Blue" {
		t.Fatalf("answers %v", src.answers)
	}
	if len(src.done) != 2 || !strings.HasSuffix(src.done[0], " false") || !strings.HasSuffix(src.done[1], " true") {
		t.Errorf("done %v", src.done)
	}
}
