package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// runAll runs cmd and the batch it may be, feeding action messages back.
func runAll(m *dash, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runAll(m, c)
		}
	case actionMsg:
		m.Update(msg)
	}
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
	runAll(m, pressed(m, boundPress("task.delegate", alpha("T2"), true)))
	cv, ok := m.top().(*confirmView)
	if !ok || cv.question != "Delegate T2 to the coordinator? It starts a thread for T2 Ship it." {
		t.Fatalf("delegate opened %T %+v", m.top(), cv)
	}
	if len(src.done) != 0 || len(src.delegated) != 0 {
		t.Fatalf("acted before the human said yes: %v %v", src.done, src.delegated)
	}
	run(m, keyPress(m, "y"))
	if len(src.delegated) != 1 || src.delegated[0] != "alpha T2" || len(src.done) != 1 || src.done[0] != "lab task.delegate alpha/T2 true" {
		t.Fatalf("after y: %v %v", src.delegated, src.done)
	}

	// n: nothing runs, and the mod hears nothing.
	runAll(m, pressed(m, boundPress("task.delegate", alpha("T2"), true)))
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
		runAll(m, pressed(m, boundPress(c.id, c.target, true)))
		if _, ok := m.top().(*confirmView); ok {
			t.Fatalf("%s %v asked", c.id, c.target)
		}
		if !strings.Contains(m.msg, "not available: "+c.why) || len(src.done) != 1 || !strings.HasSuffix(src.done[0], " false") {
			t.Errorf("%s %v: %q, done %v", c.id, c.target, m.msg, src.done)
		}
	}
	src.done = nil
	runAll(m, pressed(m, boundPress("settings.open", nil, true)))
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
		"actions":[{"id":"task.delegate","target":{"project":"alpha","task":"T2"}},{"id":"task.show","target":{"project":"alpha","task":"T2"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	v := widget.NewView()
	v.SetBindKeys(BindKeys)
	v.Set(w)
	if got := boundHints(v); got != "D delegate · enter show" {
		t.Errorf("hints %q", got)
	}
}
