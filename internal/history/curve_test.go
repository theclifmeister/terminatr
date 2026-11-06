package history

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

func curveAt(t *testing.T, today string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation(time.DateOnly, today, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return at.Add(12 * time.Hour)
}

// Nothing finished yet: nothing to draw.
func TestCurveNeedsAFinish(t *testing.T) {
	if c := CurveOf(proto.History{Open: 3, Days: []proto.HistoryDay{}}, false, curveAt(t, "2026-11-06")); c != nil {
		t.Errorf("%+v", c)
	}
}

// A project with 3 tasks gets the curve with what it has, where the Pulse
// stays hidden: its points, a running total and the best day.
func TestCurveOfAYoungProject(t *testing.T) {
	h := proto.History{Done: 3, Days: []proto.HistoryDay{{Day: "2026-11-05", Done: 1}, {Day: "2026-11-06", Done: 2}}}
	now := curveAt(t, "2026-11-06")
	if PulseOf(h, false, now) != nil {
		t.Errorf("the Pulse appears at 10 done across 7 days")
	}
	c := CurveOf(h, false, now)
	if c == nil || c.Unit != proto.PulseDay || c.Span != "2 days" || c.Range != "5–6 Nov" || len(c.Points) != 2 {
		t.Fatalf("%+v", c)
	}
	if c.Points[1].Total != 3 || c.Points[1].Done != 2 || c.Points[1].Tip != "Fri 6 Nov: 2 done, 3 in all" {
		t.Errorf("%+v", c.Points[1])
	}
	if c.Best != (proto.CurveBest{Done: 2, Word: "best day", When: "6 Nov"}) {
		t.Errorf("%+v", c.Best)
	}
	if c.Ticks != nil {
		t.Errorf("no releases, no ticks: %+v", c.Ticks)
	}
}

// Over two years a quiet project goes by month, its best span a month, its
// range with years, and an empty month keeps its place at the same total.
func TestCurveOfAQuietProjectByMonth(t *testing.T) {
	h := proto.History{Done: 6, Days: []proto.HistoryDay{{Day: "2024-03-10", Done: 2}, {Day: "2024-03-20", Done: 1}, {Day: "2026-11-02", Done: 3}}}
	c := CurveOf(h, false, curveAt(t, "2026-11-06"))
	if c == nil || c.Unit != proto.PulseMonth || c.Range != "10 Mar 2024 – 6 Nov" {
		t.Fatalf("%+v", c)
	}
	if n := len(c.Points); n != 33 || c.Points[0].From != "2024-03-01" || c.Points[n-1].Total != 6 {
		t.Fatalf("%d points: %+v", n, c.Points)
	}
	if c.Points[1].Done != 0 || c.Points[1].Total != 3 {
		t.Errorf("a gap keeps the total: %+v", c.Points[1])
	}
	if c.Best.Word != "best month" || c.Best.When != "March 2024" || c.Best.Done != 3 {
		t.Errorf("%+v", c.Best)
	}
}

// Past 60 days, by week.
func TestCurveByWeek(t *testing.T) {
	h := proto.History{Done: 3, Days: []proto.HistoryDay{{Day: "2026-06-01", Done: 1}, {Day: "2026-11-03", Done: 2}}}
	c := CurveOf(h, false, curveAt(t, "2026-11-06"))
	if c == nil || c.Unit != proto.PulseWeek || c.Best.Word != "best week" || c.Best.When != "2–8 Nov" {
		t.Fatalf("%+v", c)
	}
}

// Each repo has its own ticks, named, and a repo with none in the span has no
// row.
func TestCurveTicksPerRepo(t *testing.T) {
	h := proto.History{Done: 3, Days: []proto.HistoryDay{{Day: "2026-11-04", Done: 1}, {Day: "2026-11-06", Done: 2}},
		Releases: []proto.HistoryRepo{
			{Repo: "app", Count: 2, Releases: []proto.HistoryRelease{{At: "2026-11-04T10:00:00Z"}, {At: "2026-11-06T10:00:00Z"}}},
			{Repo: "site", Count: 1, Releases: []proto.HistoryRelease{{At: "2026-11-05T10:00:00Z"}}},
			{Repo: "old", Count: 1, Releases: []proto.HistoryRelease{{At: "2026-10-01T10:00:00Z"}}},
		}}
	c := CurveOf(h, true, curveAt(t, "2026-11-06"))
	if c == nil || len(c.Ticks) != 2 {
		t.Fatalf("%+v", c)
	}
	if c.Ticks[0].Repo != "app" || len(c.Ticks[0].Points) != 2 || c.Ticks[0].Points[1] != 2 || c.Ticks[1].Repo != "site" || c.Ticks[1].Points[0] != 1 {
		t.Errorf("%+v", c.Ticks)
	}
	if c.Points[0].Tip != "Wed 4 Nov: 1 done, app 1 release, 1 in all" {
		t.Errorf("%q", c.Points[0].Tip)
	}
}

// The Pulse's last bar says today; a curve's point says its date, for an axis.
func TestPulseAndCurveLabels(t *testing.T) {
	h := proto.History{Done: 10, Days: []proto.HistoryDay{{Day: "2026-10-30", Done: 5}, {Day: "2026-11-06", Done: 5}}}
	now := curveAt(t, "2026-11-06")
	p := PulseOf(h, false, now)
	c := CurveOf(h, false, now)
	if p == nil || p.Bars[len(p.Bars)-1].Label != "today" || c.Points[len(c.Points)-1].Label != "6 Nov" {
		t.Errorf("%+v %+v", p, c)
	}
}

func TestSpanRange(t *testing.T) {
	now := curveAt(t, "2026-11-06")
	for _, c := range []struct{ from, to, want string }{
		{"2026-11-05", "2026-11-05", "5 Nov"},
		{"2026-11-05", "2026-11-06", "5–6 Nov"},
		{"2026-09-28", "2026-10-04", "28 Sep – 4 Oct"},
		{"2025-12-28", "2026-01-03", "28 Dec 2025 – 3 Jan"},
		{"2025-03-02", "2025-03-04", "2–4 Mar 2025"},
	} {
		if got := spanRange(c.from, c.to, now); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.from, c.to, got, c.want)
		}
	}
}

func designed(id int, design string, status tasks.Status, at string) *tasks.Task {
	return &tasks.Task{ID: id, Title: fmt.Sprintf("Task %d", id), Status: status, DoneAt: at, Design: design, Created: "2026-11-01", Updated: "2026-11-01"}
}

// Chapters are the designs with a task to build, newest first with those not
// begun on top, dropped tasks counting as neither; a chapter's flag is the
// point its first task finished in.
func TestChapters(t *testing.T) {
	p := newProject(t)
	writeTasks(t, p, []*tasks.Task{
		designed(1, "older", tasks.Done, "2026-11-02T10:00:00Z"),
		designed(2, "newer", tasks.Done, "2026-11-03T10:00:00Z"),
		designed(3, "newer", tasks.Done, "2026-11-04T10:00:00Z"),
		designed(4, "newer", tasks.Started, ""),
		designed(5, "newer", tasks.Dropped, ""),
		designed(6, "ahead", tasks.Open, ""),
		designed(7, "ahead", tasks.Open, ""),
		designed(8, "gone", tasks.Dropped, ""),
		done(9, "2026-11-04T11:00:00Z"),
	}, nil)
	h, err := ReadIn(p, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if h.ChapterDone != 3 || h.Done != 4 {
		t.Fatalf("%d of %d belong to one", h.ChapterDone, h.Done)
	}
	var names []string
	for _, c := range h.Chapters {
		names = append(names, c.Name)
	}
	if want := []string{"ahead", "newer", "older"}; !slices.Equal(names, want) {
		t.Fatalf("%v, want %v", names, want)
	}
	ahead, newer := h.Chapters[0], h.Chapters[1]
	if ahead.Done != 0 || ahead.Total != 2 || ahead.Label != "" || ahead.Flag != 0 {
		t.Errorf("all its tasks open, no dates and no flag: %+v", ahead)
	}
	if newer.Done != 2 || newer.Total != 3 || newer.From != "2026-11-03" || newer.To != "2026-11-04" || newer.Label != "3–4 Nov" {
		t.Errorf("%+v", newer)
	}
	if h.Curve == nil || h.Curve.Points[newer.Flag-1].From != "2026-11-03" {
		t.Errorf("the flag %d of %+v", newer.Flag, h.Curve)
	}
}

// A project with no designs has no chapters, and a paired server gets none.
func TestNoChapters(t *testing.T) {
	p := newProject(t)
	writeTasks(t, p, []*tasks.Task{done(1, "2026-11-04T11:00:00Z")}, nil)
	h, err := ReadIn(p, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if h.Chapters != nil || h.ChapterDone != 0 || h.Curve == nil {
		t.Errorf("%+v", h)
	}
	writeTasks(t, p, []*tasks.Task{designed(1, "web", tasks.Done, "2026-11-04T11:00:00Z")}, nil)
	r, err := Records(p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Curve != nil || r.Chapters != nil {
		t.Errorf("a paired server's: %+v", r)
	}
}
