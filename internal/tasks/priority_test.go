package tasks

import (
	"slices"
	"strings"
	"testing"
)

func TestParsePriority(t *testing.T) {
	for in, want := range map[string]Priority{"must": Must, " Should ": Should, "could": Could, "none": NoPriority, "": NoPriority} {
		if got, ok := ParsePriority(in); !ok || got != want {
			t.Errorf("ParsePriority(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := ParsePriority("high"); ok {
		t.Error("high accepted")
	}
	if Must.Rank() >= Should.Rank() || Should.Rank() >= Could.Rank() || Could.Rank() >= NoPriority.Rank() {
		t.Error("ranks out of order")
	}
}

// The priority is a line straight after the metadata line, so older tm,
// which refuses an unknown metadata key, reads it as the notes' first
// line; a board that older tm wrote back (a blank line before it) reads
// the same.
func TestPriorityRoundTrip(t *testing.T) {
	b := &Board{NextID: 5, Tasks: []*Task{
		{ID: 1, Title: "None", Status: Ready},
		{ID: 2, Title: "Could", Status: Ready, Priority: Could, Notes: "why"},
		{ID: 3, Title: "Must", Status: Ready, Priority: Must, Steps: []Step{{N: 1, Text: "a"}}},
		{ID: 4, Title: "Should", Status: Ready, Priority: Should},
	}}
	out := string(b.Render())
	want := "### T3 Must\nstatus: ready\npriority: must\n\n- [ ] a\n\n### T4 Should\nstatus: ready\npriority: should\n\n### T2 Could\nstatus: ready\npriority: could\n\nwhy\n\n### T1 None\nstatus: ready\n"
	if !strings.Contains(out, want) {
		t.Fatalf("render:\n%s", out)
	}
	b2, err := Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if string(b2.Render()) != out {
		t.Fatalf("not stable:\n%s", b2.Render())
	}
	if got := b2.Find(2); got.Priority != Could || got.Notes != "why" {
		t.Fatalf("T2 = %+v", got)
	}
	// As older tm writes it back: the line is the notes' first.
	old := "### T2 Could\nstatus: ready\n\npriority: could\n\nwhy\n"
	b3, err := Parse([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if got := b3.Find(2); got.Priority != Could || got.Notes != "why" {
		t.Fatalf("older form: T2 = %+v", got)
	}
}

func TestSortByPriorityWithinGroups(t *testing.T) {
	b := &Board{Tasks: []*Task{
		{ID: 1, Status: Ready},
		{ID: 2, Status: Ready, Priority: Could},
		{ID: 3, Status: Started},
		{ID: 4, Status: Ready, Priority: Must},
		{ID: 5, Status: Open, Priority: Must},
		{ID: 6, Status: Ready, Priority: Must},
	}}
	b.Sort()
	var ids []int
	for _, t := range b.Tasks {
		ids = append(ids, t.ID)
	}
	if want := []int{3, 4, 6, 2, 1, 5}; !slices.Equal(ids, want) {
		t.Fatalf("order %v, want %v", ids, want)
	}
}

func TestSetPriority(t *testing.T) {
	s, ev := newStore(t)
	mustAdd(t, s, human, NewTask{Title: "A", Priority: "should"}, NewTask{Title: "B"})
	if r := mustAdd(t, s, human, NewTask{Title: "C", Priority: "urgent"}); r[0].Code != "invalid-priority" {
		t.Fatalf("add urgent = %+v", r)
	}
	if got, _ := s.Get(1); got.Priority != Should {
		t.Fatalf("T1 = %+v", got)
	}
	res, err := s.SetPriority(coord, 2, "must")
	if err != nil || !res.Changed || res.Task.Priority != Must {
		t.Fatalf("set = %+v, %v", res, err)
	}
	if res, err := s.SetPriority(coord, 2, "must"); err != nil || res.Changed {
		t.Fatalf("again = %+v, %v", res, err)
	}
	if _, err := s.SetPriority(thread, 2, "could"); code(err) != "coordinator-only" {
		t.Fatalf("thread = %v", err)
	}
	if _, err := s.SetPriority(human, 2, "high"); code(err) != "invalid-priority" {
		t.Fatalf("high = %v", err)
	}
	if res, err := s.SetPriority(human, 2, "none"); err != nil || res.Task.Priority != NoPriority {
		t.Fatalf("none = %+v, %v", res, err)
	}
	if !slices.Contains(ev.journal, "coordinator task.priority T2 must") || !slices.Contains(ev.journal, "human task.priority T2 none") {
		t.Fatalf("journal %q", ev.journal)
	}
	if _, err := s.Edit(human, 1, nil, new("priority: must\nmore"), nil, nil); code(err) != "invalid-notes" {
		t.Fatalf("notes = %v", err)
	}
}

func TestLinesAndDetailShowPriority(t *testing.T) {
	ts := []*Task{{ID: 1, Title: "A", Status: Ready, Priority: Must}, {ID: 2, Title: "B", Status: Open}}
	l := Lines(ts)
	if l[0] != "T1   ready    must  A" || l[1] != "T2   open           B" {
		t.Fatalf("lines %q", l)
	}
	if l := Lines(ts[1:]); l[0] != "T2   open     B" {
		t.Fatalf("no priority column: %q", l)
	}
	if d := Detail(ts[0]); !strings.Contains(d, "status: ready · priority: must") {
		t.Fatalf("detail %q", d)
	}
	if j := ToJSON(ts[0]); j.Priority != Must {
		t.Fatalf("json %+v", j)
	}
}
