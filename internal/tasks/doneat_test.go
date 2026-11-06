package tasks

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const clockAt = "2026-10-04T12:00:00Z"

func addOne(t *testing.T, s *Store, nt NewTask) int {
	t.Helper()
	res, err := s.Add(human, []NewTask{nt})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := ParseRef(res[0].ID)
	if !ok {
		t.Fatalf("add: %+v", res)
	}
	return id
}

// TestDoneAtFollowsStatus: done_at is written by whichever command makes
// a task done, kept while it stays done, gone when it leaves done, and
// written anew when it comes back, with no thread involved.
func TestDoneAtFollowsStatus(t *testing.T) {
	s, _ := newStore(t)
	id := addOne(t, s, NewTask{Title: "By hand"})
	get := func() *Task {
		t.Helper()
		x, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	if got := get().DoneAt; got != "" {
		t.Fatalf("a new open task has done_at %q", got)
	}
	if _, err := s.SetStatus(human, id, Done, ""); err != nil {
		t.Fatal(err)
	}
	if got := get().DoneAt; got != clockAt {
		t.Fatalf("done: done_at %q", got)
	}
	// A note on a task already done moves nothing.
	s.Now = func() time.Time { return time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC) }
	if _, err := s.SetStatus(human, id, Done, "a later word"); err != nil {
		t.Fatal(err)
	}
	if got := get().DoneAt; got != clockAt {
		t.Fatalf("note on a done task: done_at %q", got)
	}
	// In a zone other than UTC the line is still UTC.
	s.Now = func() time.Time { return time.Date(2026, 10, 5, 1, 0, 0, 0, time.FixedZone("x", 3600)) }
	if _, err := s.SetStatus(human, id, Review, ""); err != nil {
		t.Fatal(err)
	}
	if got := get().DoneAt; got != "" {
		t.Fatalf("sent back: done_at %q", got)
	}
	if _, err := s.SetApproved(coord, id, Done, ""); err != nil {
		t.Fatal(err)
	}
	if got := get().DoneAt; got != "2026-10-05T00:00:00Z" {
		t.Fatalf("done again: done_at %q", got)
	}
	if _, err := s.SetApproved(coord, id, Dropped, "no"); err != nil {
		t.Fatal(err)
	}
	if got := get().DoneAt; got != "" {
		t.Fatalf("dropped: done_at %q", got)
	}
}

// TestDoneAtOtherWays: filing a task done, and the ticker's completion.
func TestDoneAtOtherWays(t *testing.T) {
	s, _ := newStore(t)
	filed := addOne(t, s, NewTask{Title: "Filed done", Status: "done"})
	merged := addOne(t, s, NewTask{Title: "Merged", Status: "review"})
	if _, err := s.CompleteBySetting(human, merged, "merged (PR #1)"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{filed, merged} {
		if x, _ := s.Get(id); x.DoneAt != clockAt {
			t.Errorf("T%d: done_at %q", id, x.DoneAt)
		}
	}
}

// TestDoneAtThroughArchive: an archived task keeps its time, there and
// back, and coming back isn't finishing again.
func TestDoneAtThroughArchive(t *testing.T) {
	s, _ := newStore(t)
	id := addOne(t, s, NewTask{Title: "Old", Status: "done"})
	s.Now = func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := s.Archive(human, id); err != nil {
		t.Fatal(err)
	}
	if x, _ := s.Get(id); !x.Archived || x.DoneAt != clockAt {
		t.Fatalf("archived: %+v", x)
	}
	if _, err := s.Unarchive(human, id); err != nil {
		t.Fatal(err)
	}
	if x, _ := s.Get(id); x.Archived || x.DoneAt != clockAt {
		t.Fatalf("unarchived: %+v", x)
	}
}

// TestDoneAtLine: the line sits after the priority and parts lines, reads
// back whole with the notes untouched, and an older tm's rewrite (the
// line as the notes' first, after a blank) still reads.
func TestDoneAtLine(t *testing.T) {
	tk := &Task{ID: 3, Title: "x", Status: Done, Priority: Must, Parts: []string{"a/T1"}, DoneAt: clockAt, Notes: "Why.", Steps: []Step{{N: 1, Text: "s", Done: true}}}
	b := &Board{NextID: 4, Tasks: []*Task{tk}}
	out := string(b.Render())
	if !strings.Contains(out, "priority: must\nparts: a/T1\ndone_at: "+clockAt+"\n\nWhy.\n") {
		t.Fatalf("written:\n%s", out)
	}
	back, err := Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	got := back.Find(3)
	if got.DoneAt != clockAt || got.Priority != Must || len(got.Parts) != 1 || got.Notes != "Why." || len(got.Steps) != 1 {
		t.Fatalf("read back: %+v", got)
	}
	old := "# Tasks\n\n## Done\n\n### T3 x\nstatus: done\npriority: must\n\ndone_at: 2026-10-04\n\nWhy.\n"
	back, err = Parse([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Find(3); got.DoneAt != "2026-10-04" || got.Priority != Must || got.Notes != "Why." {
		t.Fatalf("an older tm's rewrite: %+v", got)
	}
	// Anything else on that line is the notes'.
	back, err = Parse([]byte("# Tasks\n\n## Done\n\n### T3 x\nstatus: done\n\ndone_at: yesterday\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Find(3); got.DoneAt != "" || got.Notes != "done_at: yesterday" {
		t.Fatalf("not a date: %+v", got)
	}
	if _, err := checkNotes("done_at: 2026-10-04\nmore"); err == nil {
		t.Error("notes that would read as done_at were taken")
	}
}

func TestNewestDoneFirst(t *testing.T) {
	ts := []*Task{
		{ID: 1, Status: Ready},
		{ID: 2, Status: Done, Priority: Must, DoneAt: "2026-10-01T10:00:00Z"},
		{ID: 3, Status: Done, DoneAt: "2026-10-03T09:00:00Z"},
		{ID: 4, Status: Done, DoneAt: "2026-10-03"},                        // the day only: under that day's times
		{ID: 5, Status: Done, Updated: "2026-10-02"},                       // nothing dates it but updated
		{ID: 6, Status: Done, DoneAt: "2026-10-03T09:00:00Z"},              // the same time: highest id first
		{ID: 7, Status: Dropped, Updated: "2026-11-01"},                    // stays after done
		{ID: 8, Status: Done},                                              // no date at all: last
		{ID: 9, Status: Done, Updated: "2026-10-09", DoneAt: "2026-09-01"}, // done_at beats updated
	}
	var ids []int
	for _, x := range NewestDoneFirst(ts) {
		ids = append(ids, x.ID)
	}
	if want := []int{1, 6, 3, 4, 5, 2, 9, 8, 7}; !slices.Equal(ids, want) {
		t.Errorf("order %v, want %v", ids, want)
	}
	if ts[1].ID != 2 {
		t.Error("the list given was changed")
	}
}

func TestDoneLabel(t *testing.T) {
	zone := time.FixedZone("x", 3600)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, zone)
	for at, want := range map[string]string{
		"":                     "",
		"soon":                 "",
		"2026-11-06T09:10:44Z": "10:10",
		"2026-11-05T23:30:00Z": "00:30", // today here, though yesterday in UTC
		"2026-11-05T09:10:44Z": "5 Nov 10:10",
		"2025-11-06T09:10:44Z": "6 Nov 2025 10:10",
		"2026-10-05":           "5 Oct",
		"2025-10-05":           "5 Oct 2025",
		"2026-11-06":           "6 Nov",
	} {
		if got := DoneLabel(at, now); got != want {
			t.Errorf("DoneLabel(%q) = %q, want %q", at, got, want)
		}
	}
}

// TestFillDoneAt: journal time, else the done note's day, else the
// updated day, on the board and in the archive, touching nothing else;
// a filled board isn't written again.
func TestFillDoneAt(t *testing.T) {
	s, _ := newStore(t)
	if err := os.WriteFile(s.TasksPath(), []byte(`# Tasks

## Done

### T1 Journaled
status: done · updated: 2026-10-30

done (2026-10-02): went out

### T2 Noted
status: done · updated: 2026-10-30

done (2026-09-01): first
send back
done (2026-10-02): second

### T3 Only updated
status: done · created: 2026-10-01 · updated: 2026-10-30

### T4 Nothing at all
status: done

### T5 Already
status: done · updated: 2026-10-30
done_at: 2026-10-07T01:02:03Z

## Backlog

### T6 Open
status: open · updated: 2026-10-30
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Dir+"/tasks", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ArchivePath(), []byte("# Archive\n\n### T7 Archived\nstatus: done · updated: 2026-10-01\n\n### T8 Archived, dropped\nstatus: dropped · updated: 2026-10-01\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := s.FillDoneAt(map[int]string{1: "2026-10-03T04:05:06Z", 6: "2026-10-03T04:05:06Z", 7: "2026-09-30T00:00:00Z"})
	if err != nil || n != 4 {
		t.Fatalf("filled %d, %v", n, err)
	}
	for id, want := range map[int]string{1: "2026-10-03T04:05:06Z", 2: "2026-10-02", 3: "2026-10-30", 4: "", 5: "2026-10-07T01:02:03Z", 6: "", 7: "2026-09-30T00:00:00Z", 8: ""} {
		x, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if x.DoneAt != want {
			t.Errorf("T%d: done_at %q, want %q", id, x.DoneAt, want)
		}
		if id == 1 && x.Updated != "2026-10-30" {
			t.Errorf("T1: updated moved to %q", x.Updated)
		}
	}
	info, _ := os.Stat(s.TasksPath())
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(s.TasksPath(), old, old); err != nil {
		t.Fatal(err)
	}
	if n, err := s.FillDoneAt(nil); err != nil || n != 0 {
		t.Fatalf("again: %d, %v", n, err)
	}
	if after, _ := os.Stat(s.TasksPath()); !after.ModTime().Equal(old) {
		t.Errorf("a board with nothing to fill was written (%v, was %v)", after.ModTime(), info.ModTime())
	}
}
