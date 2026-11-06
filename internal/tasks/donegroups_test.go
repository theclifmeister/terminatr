package tasks

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// doneNow is a Friday; its week starts on Monday 2 November.
var doneNow = time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)

// doneOn are n done tasks finished on day, ids from *id on.
func doneOn(id *int, day string, n int) []*Task {
	var out []*Task
	for i := range n {
		*id++
		out = append(out, &Task{ID: *id, Status: Done, DoneAt: fmt.Sprintf("%sT%02d:%02d:00Z", day, 8+i/60, i%60)})
	}
	return out
}

// newest sorts tasks as Store.Done does.
func newest(ts []*Task) []*Task { return NewestDoneFirst(ts) }

// titles are the groups' titles with their counts, "(open)" on an open one.
func titles(gs []DoneGroup) string {
	var out []string
	for _, g := range gs {
		s := fmt.Sprintf("%s %d", g.Title, g.Count)
		if g.Open {
			s += " open"
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

// checkInvariants: at most 30 rows open and 7 headings by day, each task
// in the one group whose range holds it, the ranges apart and each read
// back through PageDone.
func checkInvariants(t *testing.T, done []*Task, gs []DoneGroup) {
	t.Helper()
	rows, days, total := 0, 0, 0
	seen := map[int]bool{}
	for i, g := range gs {
		if g.Count == 0 {
			t.Errorf("%s has no tasks and a heading", g.Title)
		}
		rows += len(g.Tasks)
		total += g.Count
		if g.Kind == DoneDayGroup {
			days++
		}
		if i > 0 && g.Kind != DoneUndatedGroup && g.To >= gs[i-1].From {
			t.Errorf("%s (%s to %s) reaches into %s (%s)", g.Title, g.From, g.To, gs[i-1].Title, gs[i-1].From)
		}
		page, n, next := PageDone(done, doneNow.Location(), g.From, g.To, 0, 0)
		if n != g.Count || next != 0 || len(page) != g.Count {
			t.Errorf("%s holds %d, its range reads %d", g.Title, g.Count, n)
		}
		for _, x := range page {
			if seen[x.ID] {
				t.Errorf("T%d is in two groups", x.ID)
			}
			seen[x.ID] = true
		}
	}
	if rows > DoneOpenRows {
		t.Errorf("%d rows open", rows)
	}
	if days > DoneOpenDays {
		t.Errorf("%d days headed", days)
	}
	if total != len(done) {
		t.Errorf("the groups hold %d of %d", total, len(done))
	}
}

func TestGroupDoneThreeTasks(t *testing.T) {
	id := 0
	done := newest(slices.Concat(doneOn(&id, "2026-11-06", 2), doneOn(&id, "2026-11-05", 1)))
	gs := GroupDone(done, doneNow, 0)
	if got := titles(gs); got != "Today 2 open, Yesterday 1 open" {
		t.Errorf("3 tasks: %s", got)
	}
	checkInvariants(t, done, gs)
	if gs := GroupDone(nil, doneNow, 0); len(gs) != 0 {
		t.Errorf("no tasks: %+v", gs)
	}
}

func TestGroupDoneThousandOverThreeMonths(t *testing.T) {
	id := 0
	var all []*Task
	day := doneNow
	for len(all) < 1000 {
		all = append(all, doneOn(&id, day.Format(time.DateOnly), min(11, 1000-len(all)))...)
		day = day.AddDate(0, 0, -1)
	}
	done := newest(all)
	gs := GroupDone(done, doneNow, 0)
	checkInvariants(t, done, gs)
	// Two days fit the 30 rows, the rest of the 7 stay as closed days;
	// weeks back to 5 Oct, months before it.
	want := "Today 11 open, Yesterday 11 open, Wed 4 Nov 11, Tue 3 Nov 11, Mon 2 Nov 11, Sun 1 Nov 11, Sat 31 Oct 11, " +
		"26–30 Oct 55, 19–25 Oct 77, 12–18 Oct 77, 5–11 Oct 77, October 44, September 330, August "
	if got := titles(gs); !strings.HasPrefix(got, want) {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// Paging a fold reads every row once, newest first.
	var g DoneGroup
	for _, x := range gs {
		if x.Title == "September" {
			g = x
		}
	}
	var read []*Task
	for offset := 0; ; {
		page, total, next := PageDone(done, time.UTC, g.From, g.To, offset, 50)
		if total != g.Count {
			t.Fatalf("total %d", total)
		}
		read = append(read, page...)
		if next == 0 {
			break
		}
		offset = next
	}
	if len(read) != g.Count || !slices.IsSortedFunc(read, func(a, b *Task) int { return strings.Compare(b.DoneKey(), a.DoneKey()) }) {
		t.Errorf("read %d of %d, in order?", len(read), g.Count)
	}
}

// A quiet project: 14 tasks in five months. Only days with work get a
// heading, and the older ones fold by month with no empty week between.
func TestGroupDoneQuietProject(t *testing.T) {
	id := 0
	var all []*Task
	for _, d := range []string{"2026-11-02", "2026-10-21", "2026-10-02", "2026-09-24", "2026-09-10", "2026-08-30", "2026-08-20",
		"2026-08-10", "2026-08-01", "2026-07-20", "2026-07-02", "2026-06-20", "2026-06-10", "2026-06-02"} {
		all = append(all, doneOn(&id, d, 1)...)
	}
	done := newest(all)
	gs := GroupDone(done, doneNow, 0)
	checkInvariants(t, done, gs)
	want := "Mon 2 Nov 1 open, Wed 21 Oct 1 open, Fri 2 Oct 1 open, Thu 24 Sep 1 open, Thu 10 Sep 1 open, Sun 30 Aug 1 open, Thu 20 Aug 1 open, " +
		"August 2, July 2, June 3"
	if got := titles(gs); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// The August fold ends the day before the oldest headed day.
	if g := gs[7]; g.Kind != DoneMonthGroup || g.From != "2026-08-01" || g.To != "2026-08-19" {
		t.Errorf("August: %+v", g)
	}
}

func TestGroupDoneDayOfTwoHundred(t *testing.T) {
	id := 0
	done := newest(slices.Concat(doneOn(&id, "2026-11-06", 200), doneOn(&id, "2026-11-05", 3)))
	gs := GroupDone(done, doneNow, 0)
	checkInvariants(t, done, gs)
	if g := gs[0]; !g.Open || len(g.Tasks) != 30 || g.Next != 30 || g.Count != 200 {
		t.Errorf("today: open %v, %d rows, next %d", g.Open, len(g.Tasks), g.Next)
	}
	if g := gs[1]; g.Open || len(g.Tasks) != 0 {
		t.Errorf("yesterday opened with no room: %+v", g)
	}
	page, total, next := PageDone(done, time.UTC, "2026-11-06", "2026-11-06", 30, 50)
	if len(page) != 50 || total != 200 || next != 80 || page[0].ID != gs[0].Tasks[29].ID-1 {
		t.Errorf("the next page: %d rows, %d, next %d", len(page), total, next)
	}
	if page, _, next := PageDone(done, time.UTC, "2026-11-06", "2026-11-06", 500, 50); len(page) != 0 || next != 0 {
		t.Errorf("past the end: %d rows, next %d", len(page), next)
	}
}

// TestGroupDoneDays: the day is the viewer's for a time and as recorded
// for a day; a day-only and an undated task; archived ones are counted.
func TestGroupDoneDays(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*3600)
	late := &Task{ID: 1, Status: Done, DoneAt: "2026-11-05T20:00:00Z"} // 6 Nov in Tokyo
	if got := DoneDay(late, time.UTC); got != "2026-11-05" {
		t.Errorf("UTC: %s", got)
	}
	if got := DoneDay(late, tokyo); got != "2026-11-06" {
		t.Errorf("Tokyo: %s", got)
	}
	day := &Task{ID: 2, Status: Done, DoneAt: "2026-11-04", Archived: true}
	if got := DoneDay(day, tokyo); got != "2026-11-04" {
		t.Errorf("a day stays: %s", got)
	}
	updated := &Task{ID: 3, Status: Done, Updated: "2026-11-03"}
	if got := DoneDay(updated, tokyo); got != "2026-11-03" {
		t.Errorf("updated: %s", got)
	}
	none := &Task{ID: 4, Status: Done}
	if got := DoneDay(none, tokyo); got != DoneUndated {
		t.Errorf("none: %s", got)
	}
	done := newest([]*Task{late, day, updated, none})
	gs := GroupDone(done, doneNow, 0)
	checkInvariants(t, done, gs)
	if got := titles(gs); got != "Yesterday 1 open, Wed 4 Nov 1 open, Tue 3 Nov 1 open, Undated 1" {
		t.Errorf("%s", got)
	}
	if gs[1].Archived != 1 || gs[0].Archived != 0 {
		t.Errorf("archived: %+v", gs)
	}
	if page, n, _ := PageDone(done, time.UTC, DoneUndated, DoneUndated, 0, 0); n != 1 || page[0].ID != 4 {
		t.Errorf("undated reads %d", n)
	}
}

// TestGroupDoneReveal: the fold holding a task opens as far as it.
func TestGroupDoneReveal(t *testing.T) {
	id := 0
	var all []*Task
	for i := range 8 {
		all = append(all, doneOn(&id, doneNow.AddDate(0, 0, -i).Format(time.DateOnly), 1)...)
	}
	old := doneOn(&id, "2026-08-10", 40)
	done := newest(append(all, old...))
	if gs := GroupDone(done, doneNow, 0); gs[len(gs)-1].Open {
		t.Fatalf("August opened by itself")
	}
	gs := GroupDone(done, doneNow, old[0].ID) // the day's first, so its last row
	g := gs[len(gs)-1]
	if !g.Open || g.Title != "August" || len(g.Tasks) != 40 || g.Tasks[39].ID != old[0].ID || g.Next != 0 {
		t.Errorf("revealed: %s open %v, %d rows, next %d", g.Title, g.Open, len(g.Tasks), g.Next)
	}
	if gs := GroupDone(done, doneNow, 9999); titles(gs) != titles(GroupDone(done, doneNow, 0)) {
		t.Errorf("no such task: %s", titles(gs))
	}
}

func TestDoneTitles(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		{"2026-10-19", "2026-10-25", "19–25 Oct"},
		{"2026-10-26", "2026-11-01", "26 Oct – 1 Nov"},
		{"2026-11-03", "2026-11-03", "3 Nov"},
		{"2025-12-29", "2026-01-04", "29 Dec 2025 – 4 Jan 2026"},
		{"2025-10-13", "2025-10-19", "13–19 Oct 2025"},
	} {
		if got := weekTitle(c.from, c.to, "2026-11-06"); got != c.want {
			t.Errorf("%s..%s: %q, want %q", c.from, c.to, got, c.want)
		}
	}
	if got := monthTitle("2025-09-01", "2026-11-06"); got != "September 2025" {
		t.Error(got)
	}
	if got := dayTitle("2025-09-01", "2026-11-06"); got != "Mon 1 Sep 2025" {
		t.Error(got)
	}
	if got := weekStart("2026-11-01"); got != "2026-10-26" { // a Sunday
		t.Error(got)
	}
	if got := weekStart("2026-11-02"); got != "2026-11-02" { // a Monday
		t.Error(got)
	}
}

// TestStoreDone: the archive's tasks only when asked, dropped ones never.
func TestStoreDone(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Add(human, []NewTask{{Title: "On the board", Status: "done"}, {Title: "Archived", Status: "done"},
		{Title: "Dropped", Status: "dropped"}, {Title: "Open"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(human, 2); err != nil {
		t.Fatal(err)
	}
	board, err := s.Done(false)
	if err != nil || len(board) != 1 || board[0].ID != 1 {
		t.Errorf("the board's: %v %v", board, err)
	}
	all, err := s.Done(true)
	if err != nil || len(all) != 2 || !slices.ContainsFunc(all, func(x *Task) bool { return x.ID == 2 && x.Archived }) {
		t.Errorf("with the archive: %v %v", all, err)
	}
}
