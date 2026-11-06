package e2e

import (
	"fmt"
	"strings"
	"testing"
)

// TestSmokeTaskWaits: the project popup's Tasks tab says "after T4"
// muted on a task whose link is unmet and "1 wait" on the task they wait
// for (a chain of 27 tasks, the first done and the 5th dropped: the
// tasks after them say nothing), and nothing for a link to a task that
// is not there; read from the after field alone.
func TestSmokeTaskWaits(t *testing.T) {
	env := New(t)
	env.FakeClaude()
	slug, _ := newProject(env, "demo")
	for i := 1; i <= 27; i++ {
		env.MustCLI("task", "add", fmt.Sprintf("Step %d", i), "--status", "ready", "--project", slug)
		if i > 1 {
			env.MustCLI("task", "edit", fmt.Sprintf("T%d", i), "--after", fmt.Sprintf("T%d", i-1), "--project", slug)
		}
	}
	env.MustCLI("task", "status", "T1", "done", "--project", slug)
	env.MustCLI("task", "status", "T5", "dropped", "--project", slug)

	w := env.Window(120, 50)
	w.WaitFor("SESSIONS", wait)
	w.Type("A")
	w.WaitFor("1 overview", wait)
	w.Type("3")
	w.WaitFor("ON DECK", wait)
	sc := w.WaitFor("after T26", wait)
	row := func(n int) string {
		for _, line := range strings.Split(sc, "\n") {
			if strings.Contains(line, fmt.Sprintf("Step %d ", n)) {
				return line
			}
		}
		t.Fatalf("no row for T%d:\n%s", n, sc)
		return ""
	}
	// T2 follows the done T1 and T6 the dropped T5: nothing to say of
	// the link, though each is waited for. T4's waiter is dropped.
	for n, want := range map[int][2]bool{2: {false, true}, 3: {true, true}, 4: {true, false}, 6: {false, true}, 27: {true, false}} {
		l := row(n)
		if got := strings.Contains(l, "after T"+fmt.Sprint(n-1)); got != want[0] {
			t.Errorf("T%d says its link: %v, want %v: %q", n, got, want[0], l)
		}
		if got := strings.Contains(l, "1 wait"); got != want[1] {
			t.Errorf("T%d says it is waited for: %v, want %v: %q", n, got, want[1], l)
		}
	}
	w.Golden("popup-tasks-waits.txt", popupMasks...)
}
