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

// TestPlaceReleases: a line goes at its time among its day's rows, whatever
// a client has read; lines of one minute keep their order; a day with no
// work has no heading, so its line tops the next older group and says its
// day; older than all the work ends the oldest group.
func TestPlaceReleases(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	done := NewestDoneFirst([]*Task{
		{ID: 1, Status: Done, DoneAt: "2026-11-06T10:00:00Z"},
		{ID: 2, Status: Done, DoneAt: "2026-11-06T08:00:00Z"},
		{ID: 3, Status: Done, DoneAt: "2026-11-06"},
		{ID: 4, Status: Done, DoneAt: "2026-11-04T09:00:00Z"},
	})
	lines := []ReleaseLine{
		{Repo: "a", Version: "1.0", At: at("2026-10-01T09:00:00Z")}, // older than all the work
		{Repo: "a", Version: "1.1", At: at("2026-11-05T09:00:00Z")}, // a day with no work
		{Repo: "a", Version: "1.2", At: at("2026-11-06T09:00:00Z")},
		{Repo: "b", Part: "api", Version: "2.0", At: at("2026-11-06T09:00:00Z")}, // the same minute
		{Repo: "b", Version: "2.1", At: at("2026-11-06T11:00:00Z")},
	}
	groups := GroupDone(done, now, 0)
	PlaceReleases(groups, done, lines, now)
	type want struct {
		label, time string
		offset      int
	}
	check := func(g DoneGroup, ws ...want) {
		t.Helper()
		var got []want
		for _, l := range g.Releases {
			got = append(got, want{l.Label(), l.Time, l.Offset})
		}
		if !slices.Equal(got, ws) {
			t.Errorf("%s: %+v, want %+v", g.Title, got, ws)
		}
	}
	check(groups[0], want{"b 2.1", "11:00", 0}, want{"a 1.2", "09:00", 1}, want{"b api 2.0", "09:00", 1})
	check(groups[1], want{"a 1.1", "5 Nov 09:00", 0}, want{"a 1.0", "1 Oct 09:00", 1})

	// A release today with no work today says today.
	yesterday := []*Task{{ID: 9, Status: Done, DoneAt: "2026-11-05T09:00:00Z"}}
	g := GroupDone(yesterday, now, 0)
	PlaceReleases(g, yesterday, []ReleaseLine{{Version: "3.0", At: at("2026-11-06T07:00:00Z")}}, now)
	if l := g[0].Releases[0]; l.Offset != 0 || l.Time != "today 07:00" || l.Label() != "3.0" {
		t.Errorf("a line of a day with no heading: %+v", l)
	}
	// Nothing to read a line against, and nothing dated: no lines.
	PlaceReleases(nil, nil, lines, now)
	undated := []*Task{{ID: 9, Status: Done}}
	g = GroupDone(undated, now, 0)
	PlaceReleases(g, undated, lines, now)
	if len(g[0].Releases) != 0 {
		t.Errorf("an undated group took lines: %+v", g[0])
	}
}

// A task finished at a time, and the human's action, on doneNow's days.
func doneAt(id int, at string) *Task { return &Task{ID: id, Status: Done, DoneAt: at} }

func actedAt(t *testing.T, at string) time.Time {
	t.Helper()
	a, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestMarkDone: where the line stands, what it says, and which group that
// opens. All from task records and the journal's time: every setup, git or
// not, is this same call.
func TestMarkDone(t *testing.T) {
	today := []*Task{doneAt(9, "2026-11-06T14:00:00Z"), doneAt(8, "2026-11-06T09:00:00Z")}
	yesterday := []*Task{doneAt(7, "2026-11-05T23:39:00Z"), doneAt(6, "2026-11-05T22:53:00Z")}
	// Seven days with work, so that October and September fold.
	id := 100
	var days []*Task
	for d := range 7 {
		days = append(days, doneOn(&id, dayAdd("2026-11-06", -d), 1)...)
	}
	october := []*Task{doneAt(5, "2026-10-21T12:00:00Z"), doneAt(4, "2026-10-19T12:00:00Z")}
	september := []*Task{doneAt(3, "2026-09-10T12:00:00Z"), doneAt(2, "2026-09-02T12:00:00Z")}
	quiet := []*Task{doneAt(9, "2026-11-01T10:00:00Z"), doneAt(8, "2026-07-01T10:00:00Z"), doneAt(7, "2026-03-01T10:00:00Z")}
	undated := &Task{ID: 1, Status: Done}
	cat := func(ts ...[]*Task) []*Task { return slices.Concat(ts...) }
	for _, tc := range []struct {
		name, at, where string
		done            []*Task
		title           string // "" for no line
		group, rows     int
		between         bool
		opens           string // the title of the group that must be open
	}{
		{name: "between two rows of a day", at: "2026-11-05T23:14:00Z", where: "on the iPhone", done: cat(today, yesterday),
			title: "you last acted at 23:14 on the iPhone", group: 1, rows: 1, opens: "Yesterday"},
		{name: "this morning, in the app", at: "2026-11-06T10:00:00Z", where: "in the app", done: today,
			title: "you last acted at 10:00 in the app", group: 0, rows: 1, opens: "Today"},
		{name: "nothing finished since", at: "2026-11-06T14:00:00Z", done: today},
		{name: "a task at the same second is not since", at: "2026-11-06T14:00:01Z", done: today},
		{name: "no done work", at: "2026-11-06T14:00:00Z"},
		{name: "below all of a day's rows", at: "2026-11-05T01:00:00Z", done: cat(today, yesterday),
			title: "you last acted at 01:00", group: 1, rows: 2, opens: "Yesterday"},
		{name: "a day older than the open days: the fold opens", at: "2026-10-20T08:00:00Z", done: cat(days, october, september),
			title: "you last acted 20 Oct 08:00", group: 7, rows: 1, opens: "19–25 Oct"},
		{name: "in a month's fold: the fold opens", at: "2026-09-05T08:00:00Z", done: cat(days, october, september),
			title: "you last acted 5 Sep 08:00", group: 8, rows: 1, opens: "September"},
		{name: "months between tasks: between two headings", at: "2026-05-01T08:00:00Z", done: quiet,
			title: "you last acted 1 May 08:00", group: 2, between: true},
		{name: "older than all the work: below every heading", at: "2026-01-01T08:00:00Z", done: quiet,
			title: "you last acted 1 Jan 08:00", group: 3, between: true},
		{name: "another year's says so", at: "2025-12-31T08:00:00Z", done: quiet,
			title: "you last acted 31 Dec 2025 08:00", group: 3, between: true},
		{name: "above what nothing dates", at: "2026-01-01T08:00:00Z", done: cat(quiet, []*Task{undated}),
			title: "you last acted 1 Jan 08:00", group: 3, between: true},
		{name: "only undated work: nothing finished since", at: "2026-01-01T08:00:00Z", done: []*Task{undated}},
		{name: "a task known by its day alone, later", at: "2026-11-05T08:00:00Z", done: []*Task{{ID: 2, Status: Done, DoneAt: "2026-11-06"}, doneAt(1, "2026-11-05T07:00:00Z")},
			title: "you last acted at 08:00", group: 1, rows: 0},
		{name: "a task known by its day alone, that day: not since", at: "2026-11-06T08:00:00Z", done: []*Task{{ID: 2, Status: Done, DoneAt: "2026-11-06"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done := newest(tc.done)
			gs := GroupDone(done, doneNow, 0)
			m := MarkDone(gs, done, actedAt(t, tc.at), tc.where, doneNow)
			if tc.title == "" {
				if m != nil {
					t.Fatalf("a line: %+v", m)
				}
				return
			}
			if m == nil {
				t.Fatalf("no line, groups %s", titles(gs))
			}
			if m.Title != tc.title || m.Group != tc.group || m.Rows != tc.rows || m.Between != tc.between {
				t.Errorf("mark %+v, groups %s", m, titles(gs))
			}
			if tc.opens != "" {
				g := gs[m.Group]
				if g.Title != tc.opens || !g.Open {
					t.Errorf("opens %q open=%v, want %q; groups %s", g.Title, g.Open, tc.opens, titles(gs))
				}
				if len(g.Tasks) < min(m.Rows+1, g.Count) {
					t.Errorf("read %d rows of %d, the line below %d", len(g.Tasks), g.Count, m.Rows)
				}
			}
		})
	}
}

// TestMarkDoneInLocalTime: the day, the time and the group are the viewer's.
func TestMarkDoneInLocalTime(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*3600)
	now := doneNow.In(tokyo) // 00:00 on Saturday 7 November there
	done := newest([]*Task{doneAt(2, "2026-11-06T14:00:00Z"), doneAt(1, "2026-11-06T09:00:00Z")})
	gs := GroupDone(done, now, 0)
	m := MarkDone(gs, done, actedAt(t, "2026-11-06T10:00:00Z"), "", now)
	if m == nil || m.Title != "you last acted at 19:00" || gs[m.Group].Title != "Yesterday" || m.Rows != 1 {
		t.Fatalf("%+v, groups %s", m, titles(gs))
	}
}

// TestMarkDoneDeepInAFold: a line below more rows than a page still has
// them read, and a fold it opens says what is left to read.
func TestMarkDoneDeepInAFold(t *testing.T) {
	id := 0
	var done []*Task
	for d := range 7 {
		done = append(done, doneOn(&id, dayAdd("2026-11-06", -d), 1)...)
	}
	done = append(done, doneOn(&id, "2026-09-10", 50)...) // 08:00 to 08:49
	done = newest(done)
	gs := GroupDone(done, doneNow, 0)
	m := MarkDone(gs, done, actedAt(t, "2026-09-10T08:04:30Z"), "", doneNow)
	if m == nil || m.Rows != 45 {
		t.Fatalf("%+v", m)
	}
	g := gs[m.Group]
	if !g.Open || len(g.Tasks) != 46 || g.Next != 46 {
		t.Fatalf("open %v, %d rows, next %d", g.Open, len(g.Tasks), g.Next)
	}
}
