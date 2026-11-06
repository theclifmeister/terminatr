package tasks

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestWaits(t *testing.T) {
	b := &Board{Tasks: []*Task{
		{ID: 1, Status: Open},
		{ID: 2, Status: Done},
		{ID: 3, Status: Dropped},
		{ID: 4, Status: Ready, After: []int{1, 2, 3, 99}, AfterX: []string{"api/T12", "web/T3"}},
		{ID: 5, Status: Open, After: []int{1}},
		{ID: 6, Status: Done, After: []int{1}}, // closed: says nothing, counts nowhere
	}}
	w := b.Waits(func(ref string) bool { return ref == "api/T12" })
	if got := w.After[4]; !slices.Equal(got, []string{"T1", "api/T12"}) {
		t.Fatalf("T4 waits for %v", got)
	}
	if got := w.After[5]; !slices.Equal(got, []string{"T1"}) {
		t.Fatalf("T5 waits for %v", got)
	}
	if len(w.After[6]) != 0 || w.Waiters[1] != 2 || w.Waiters[2] != 0 || w.Waiters[3] != 0 {
		t.Fatalf("after %v waiters %v", w.After, w.Waiters)
	}
	if none := b.Waits(nil); len(none.After[4]) != 1 {
		t.Fatalf("no cross state: %v", none.After[4])
	}
}

func TestWaitsChain(t *testing.T) {
	b := &Board{}
	for i := 1; i <= 27; i++ {
		tk := &Task{ID: i, Status: Open}
		if i > 1 {
			tk.After = []int{i - 1}
		}
		b.Tasks = append(b.Tasks, tk)
	}
	w := b.Waits(nil)
	if len(w.After[1]) != 0 || w.Waiters[1] != 1 || w.Waiters[27] != 0 || fmt.Sprint(w.After[27]) != "[T26]" {
		t.Fatalf("%v %v", w.After[27], w.Waiters)
	}
	b.Tasks[0].Status = Done // the head is met: T2 says nothing, T3 still waits
	w = b.Waits(nil)
	if len(w.After[2]) != 0 || len(w.After[3]) != 1 {
		t.Fatalf("after the head is done: %v %v", w.After[2], w.After[3])
	}
}

func TestWaitWords(t *testing.T) {
	for in, want := range map[string]string{
		"":                   "",
		"T9":                 "after T9",
		"T9,api/T12":         "after T9, api/T12",
		"T9,T10,T11,api/T12": "after T9, T10 +2",
	} {
		var refs []string
		if in != "" {
			refs = strings.Split(in, ",")
		}
		if got := WaitWords(refs); got != want {
			t.Errorf("WaitWords(%q) = %q, want %q", in, got, want)
		}
	}
}
