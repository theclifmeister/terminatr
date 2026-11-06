package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// TestTasksTabFilterReachesDone: / on the Tasks tab finds done tasks that
// aren't drawn, on the board and in the archive, with Done folded, by the
// words of their id and title, as the Mac's board filter does; an archived
// one opens; esc folds them again.
func TestTasksTabFilterReachesDone(t *testing.T) {
	env := New(t)
	w := env.Window(100, 24)
	w.WaitFor("no sessions", wait)
	slug, _ := newProject(env, "demo")
	w.WaitFor("demo", wait)
	env.MustCLI("task", "add", "The old needle", "--project", slug)
	env.MustCLI("task", "add", "A needle on the board", "--project", slug)
	env.MustCLI("task", "add", "Something else", "--project", slug)
	env.MustCLI("task", "status", "T1", "done", "--project", slug)
	env.MustCLI("task", "status", "T2", "done", "--project", slug)
	env.MustCLI("task", "archive", "T1", "--project", slug)

	w.Type("T")
	w.WaitFor("T3", wait)
	w.WaitFor("(b shows them)", wait)
	if s := w.Screen(); strings.Contains(s, "needle") {
		t.Fatalf("done tasks listed before the filter:\n%s", s)
	}
	w.Type("/")
	w.Type("NEEDLE old")
	w.WaitFor("T1", wait)
	if s := w.Screen(); !strings.Contains(s, "The old needle") || strings.Contains(s, "A needle on the board") || strings.Contains(s, "Something else") {
		t.Fatalf("the filter's words:\n%s", s)
	}
	for range len(" old") {
		w.Key(emu.Key{Special: emu.KeyBackspace})
	}
	w.WaitFor("A needle on the board", wait)
	w.WaitFor("The old needle", wait)
	if s := w.Screen(); strings.Contains(s, "Something else") {
		t.Fatalf("a row without the word:\n%s", s)
	}
	// Enter keeps the filter, down the older one, enter shows it.
	w.Key(keyEnter)
	w.Key(keyDown)
	w.WaitFor("enter show", wait)
	// A press too soon after the row changed counts for nothing.
	shown := false
	for try := 0; try < 10 && !shown; try++ {
		w.Key(keyEnter)
		shown = Poll(time.Second, func() bool { return strings.Contains(w.Screen(), "rchived") })
	}
	if !shown {
		t.Fatalf("the archived task didn't open:\n%s", w.Screen())
	}
	w.Key(keyEsc)
	w.WaitFor("The old needle", wait)
	w.Key(keyEsc)
	w.WaitFor("(b shows them)", wait)
	if s := w.Screen(); strings.Contains(s, "needle") {
		t.Fatalf("the filter gone, done tasks still listed:\n%s", s)
	}
}
