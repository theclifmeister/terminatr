package server

import (
	"fmt"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestBoardWaits: a row says what its after links leave unmet, from the
// board alone: a link to a done, dropped, archived or missing task says
// nothing; a link to another project's task (a program's) waits while
// the ticker's deps.json says it is open, and only while programs are on
// and the project is in the program; the mod's list and the board carry
// the same words.
func TestBoardWaits(t *testing.T) {
	testPaths(t)
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\", \"api\"]\n")
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := program.WriteDeps("acme", program.Snapshot{States: map[string]program.Dep{
		"api/T12": {State: program.DepOpen, Status: "review"},
		"api/T3":  {State: program.DepMet, Status: "done"},
		"api/T4":  {State: program.DepGone, Status: "dropped"},
	}}); err != nil {
		t.Fatal(err)
	}
	b := &tasks.Board{Tasks: []*tasks.Task{
		{ID: 1, Status: tasks.Ready},
		{ID: 2, Status: tasks.Done},
		{ID: 3, Status: tasks.Dropped},
		{ID: 4, Status: tasks.Open, After: []int{1, 2, 3, 77 /* archived: not on the board */}, AfterX: []string{"api/T12", "api/T3", "api/T4", "api/T9"}},
	}}
	w := boardWaits(cfg, "web", b)
	if got := w.After[4]; !slices.Equal(got, []string{"T1", "api/T12"}) || w.Waiters[1] != 1 || w.Waiters[2] != 0 {
		t.Fatalf("waits %v waiters %v", w.After, w.Waiters)
	}
	// Another project: not in the program, so the links say nothing.
	if got := boardWaits(cfg, "elsewhere", b).After[4]; !slices.Equal(got, []string{"T1"}) {
		t.Errorf("outside the program: %v", got)
	}
	// Programs off: the same.
	off := *cfg
	off.Features.Programs = false
	if got := boardWaits(&off, "web", b).After[4]; !slices.Equal(got, []string{"T1"}) {
		t.Errorf("programs off: %v", got)
	}
	mt := modTasks("web", b, nil)
	if mt[3].ID != "T4" || !slices.Equal(mt[3].WaitsFor, []string{"T1", "api/T12"}) || mt[0].Waiters != 1 {
		t.Errorf("mod tasks %+v", mt)
	}
}

// TestBoardWaitsChain: a chain of 27 tasks, each after the one before:
// every task but the first waits for its predecessor, every one but the
// last is waited for, once; finishing the head frees the second.
func TestBoardWaitsChain(t *testing.T) {
	b := &tasks.Board{}
	for i := 1; i <= 27; i++ {
		tk := &tasks.Task{ID: i, Status: tasks.Open}
		if i > 1 {
			tk.After = []int{i - 1}
		}
		b.Tasks = append(b.Tasks, tk)
	}
	w := boardWaits(nil, "demo", b)
	for i := 1; i <= 27; i++ {
		wantAfter, wantWaiters := "", 1
		if i > 1 {
			wantAfter = fmt.Sprintf("after T%d", i-1)
		}
		if i == 27 {
			wantWaiters = 0
		}
		if got := tasks.WaitWords(w.After[i]); got != wantAfter || w.Waiters[i] != wantWaiters {
			t.Fatalf("T%d says %q, %d wait", i, got, w.Waiters[i])
		}
	}
	b.Tasks[0].Status = tasks.Done
	w = boardWaits(nil, "demo", b)
	if len(w.After[2]) != 0 || len(w.After[3]) != 1 || w.Waiters[1] != 0 {
		t.Errorf("head done: %v %v", w.After[2], w.After[3])
	}
}
