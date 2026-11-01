package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestTaskPriority: the Tasks tab lists each section by priority, the
// priority in a column of its own; P on a task picks must, should,
// could or none, which sets it as the human and moves the row; the
// shown task names it, and P works there too.
func TestTaskPriority(t *testing.T) {
	src, m := popupData(t)
	src.board.Tasks = append(src.board.Tasks,
		&tasks.Task{ID: 3, Title: "Plan the beta", Status: tasks.Ready, Priority: tasks.Could},
		&tasks.Task{ID: 4, Title: "Fix the crash", Status: tasks.Ready, Priority: tasks.Must})
	tasksTabNow(m, src)
	feed(m, keyPress(m, "T"))
	out := screen(m)
	order := func(out string) []int {
		var at []int
		for _, s := range []string{"Write the README", "Fix the crash", "Plan the beta", "Ship it"} {
			at = append(at, strings.Index(out, s))
		}
		return at
	}
	if at := order(out); !slices.IsSorted(at) || at[0] < 0 {
		t.Fatalf("not by priority within the group (%v):\n%s", at, out)
	}
	if l := lineWith(out, "Fix the crash"); !strings.Contains(l, "must") {
		t.Fatalf("no marker: %q", l)
	}
	for range 3 {
		keyPress(m, "down")
	}
	if c := tasksCursor(m, src); c != "T2" {
		t.Fatalf("cursor on %q", c)
	}
	pressKey(t, m, "P")
	mv, ok := m.top().(*menuView)
	if !ok || mv.title != "Priority · T2 Ship it" || len(mv.items) != 4 || mv.items[3].label != "none ✓" {
		t.Fatalf("P opened %T %+v", m.top(), m.top())
	}
	act(t, m, src, "enter") // must, the first
	if !slices.Equal(src.priorities, []string{"alpha T2 must"}) || m.msg != "T2 is must now" {
		t.Fatalf("set %v, msg %q", src.priorities, m.msg)
	}
	// T2 is must now, before T4 (must too) by id.
	out = screen(m)
	if a, b := strings.Index(out, "Ship it"), strings.Index(out, "Fix the crash"); a < 0 || a > b {
		t.Fatalf("T2 didn't move up:\n%s", out)
	}
	// The shown task names its priority, and P works there.
	for tasksCursor(m, src) != "T4" {
		keyPress(m, "down")
	}
	stillRows(t) // the rows just moved
	pressKey(t, m, "enter")
	if out := screen(m); !strings.Contains(out, "priority") || !strings.Contains(lineWith(out, "priority"), "must") || !strings.Contains(out, "D delegate · P priority · esc back") {
		t.Fatalf("shown task:\n%s", out)
	}
	pressKey(t, m, "P")
	keyPress(m, "down")
	keyPress(m, "down")
	act(t, m, src, "enter") // could
	if len(src.priorities) != 2 || src.priorities[1] != "alpha T4 could" {
		t.Fatalf("set from the shown task: %v", src.priorities)
	}
}
