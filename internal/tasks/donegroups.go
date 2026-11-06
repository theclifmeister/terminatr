package tasks

import (
	"slices"
	"strings"
	"time"
)

// How much of Done is open (design board-library, B3).
const (
	// DoneOpenDays are the newest days with work that get a heading of
	// their own; empty days don't count.
	DoneOpenDays = 7
	// DoneOpenRows is the most rows the open days show together.
	DoneOpenRows = 30
	// doneWeeks are the calendar weeks, this one included, that older
	// work folds by; what is older than them folds by month.
	doneWeeks = 5
	// DoneUndated is the "day" of a done task nothing dates, as a group's
	// From and To say it.
	DoneUndated = "none"
)

// The kinds of DoneGroup.
const (
	DoneDayGroup     = "day"
	DoneWeekGroup    = "week"
	DoneMonthGroup   = "month"
	DoneUndatedGroup = "undated"
)

// DoneGroup is one heading of Done: a day, or a fold of older work. It
// covers From to To, days inclusive; Count of its tasks are done there
// and Archived of them are off the board. An open group has its first
// Tasks; Next is the offset to read more from, 0 when they are all there.
type DoneGroup struct {
	Kind     string
	Title    string
	From, To string
	Count    int
	Archived int
	Open     bool
	Tasks    []*Task
	Next     int
	// Releases are the release lines among its rows, newest first
	// (PlaceReleases).
	Releases []ReleaseLine
}

// ReleaseLine is a release's line in Done (design board-library, B4): it
// marks when, and says nothing of which tasks went in. Repo is "" where
// the repo goes without saying. Time is At in words, as its group's
// heading leaves it to say. Offset is how many of the group's rows are
// newer: the line goes before the row of that index, whatever part of
// them a client has read.
type ReleaseLine struct {
	Repo    string
	Part    string
	Version string
	Tag     string
	At      time.Time
	Time    string
	Offset  int
}

// Label is what the line calls the release: "site 2.4.0", "api 1.2", "0.82.1".
func (l ReleaseLine) Label() string {
	return strings.Join(slices.DeleteFunc([]string{l.Repo, l.Part, l.Version}, func(s string) bool { return s == "" }), " ")
}

// PlaceReleases puts lines into groups, Done's headings of done, at each
// one's time as of now: among the rows of the group whose days hold it. A
// day with no work has no heading, so a line of such a day goes at the top
// of the next older group and says its day, and one older than all the work
// ends the oldest group. What nothing dates has no time to put a line by,
// and with no dated group there is nowhere to put one. Lines of one time
// keep the order they came in.
func PlaceReleases(groups []DoneGroup, done []*Task, lines []ReleaseLine, now time.Time) {
	loc := now.Location()
	dated := slices.IndexFunc(groups, func(g DoneGroup) bool { return g.Kind == DoneUndatedGroup })
	if dated < 0 {
		dated = len(groups)
	}
	if dated == 0 {
		return
	}
	lines = slices.Clone(lines)
	slices.SortStableFunc(lines, func(a, b ReleaseLine) int { return b.At.Compare(a.At) })
	rows := make([][]*Task, dated)
	for _, l := range lines {
		day := l.At.In(loc).Format(time.DateOnly)
		i := slices.IndexFunc(groups[:dated], func(g DoneGroup) bool { return g.From <= day })
		g := dated - 1
		if i >= 0 {
			g = i
		}
		if rows[g] == nil {
			rows[g], _, _ = PageDone(done, loc, groups[g].From, groups[g].To, 0, 0)
		}
		switch {
		case day > groups[g].To:
			l.Offset = 0
		case day < groups[g].From:
			l.Offset = len(rows[g])
		default:
			l.Offset = newerThan(rows[g], l.At)
		}
		l.Time = lineTime(l.At.In(loc), groups[g], now)
		groups[g].Releases = append(groups[g].Releases, l)
	}
}

// newerThan is how many of rows, newest first, finished after at, up to the
// first that didn't. A row with only a day lists after that day's timed
// rows (NewestDoneFirst), so a line goes before it.
func newerThan(rows []*Task, at time.Time) int {
	for i, t := range rows {
		when, err := time.Parse(time.RFC3339, t.DoneKey())
		if err != nil || !when.After(at) {
			return i
		}
	}
	return len(rows)
}

// lineTime is a line's time in words: the time alone under its own day's
// heading, with its day anywhere else.
func lineTime(at time.Time, g DoneGroup, now time.Time) string {
	day := at.Format(time.DateOnly)
	if g.Kind == DoneDayGroup && g.From == day {
		return at.Format("15:04")
	}
	if day == now.Format(time.DateOnly) {
		return "today " + at.Format("15:04")
	}
	if at.Year() != now.Year() {
		return at.Format("2 Jan 2006 15:04")
	}
	return at.Format("2 Jan 15:04")
}

// Done are the store's done tasks, newest first (NewestDoneFirst): the
// board's, and the archive's when archive is true.
func (s *Store) Done(archive bool) ([]*Task, error) {
	b, err := s.Load()
	if err != nil {
		return nil, err
	}
	all := b.Tasks
	if archive {
		a, err := s.LoadArchive()
		if err != nil {
			return nil, err
		}
		all = append(slices.Clone(all), a.Tasks...)
	}
	out := make([]*Task, 0, len(all))
	for _, t := range all {
		if t.Status == Done {
			out = append(out, t)
		}
	}
	return NewestDoneFirst(out), nil
}

// DoneDay is the day done task t finished in loc: a time's day there, or
// the day recorded when only that is known, else the day it last
// changed; DoneUndated when nothing dates it.
func DoneDay(t *Task, loc *time.Location) string {
	key := t.DoneKey()
	if at, err := time.Parse(time.RFC3339, key); err == nil {
		return at.In(loc).Format(time.DateOnly)
	}
	if _, err := time.Parse(time.DateOnly, key); err == nil {
		return key
	}
	return DoneUndated
}

// GroupDone heads done, tasks newest first (Store.Done), as of now, in
// now's time zone: the newest DoneOpenDays days with work, one heading
// each, then older work by week, then by month, then what nothing dates.
// A span with no work has no heading. The days open from the newest
// while DoneOpenRows has room for them all; the newest always opens, up
// to DoneOpenRows of it. The group holding task reveal (0: none) opens
// too, as far as that task.
func GroupDone(done []*Task, now time.Time, reveal int) []DoneGroup {
	loc := now.Location()
	today := now.Format(time.DateOnly)
	byDay := map[string][]*Task{}
	var days []string
	for _, t := range done {
		d := DoneDay(t, loc)
		if byDay[d] == nil && d != DoneUndated {
			days = append(days, d)
		}
		byDay[d] = append(byDay[d], t)
	}
	slices.Sort(days)
	slices.Reverse(days)

	var out []DoneGroup
	room, opening := DoneOpenRows, true
	oldestOpen := ""
	for i := 0; i < min(DoneOpenDays, len(days)); i++ {
		d := days[i]
		g := DoneGroup{Kind: DoneDayGroup, Title: dayTitle(d, today), From: d, To: d}
		ts := byDay[d]
		if opening && (i == 0 || len(ts) <= room) {
			g.Open = true
			n := min(len(ts), room)
			room -= n
			g.Tasks = ts[:n]
			if n < len(ts) {
				g.Next = n
			}
		} else {
			opening = false
		}
		g.count(ts)
		out = append(out, g)
		oldestOpen = d
	}
	// Older work: by week from the first day of the oldest week counted,
	// by month before it. What's left of a span the headed days reach
	// into ends the day before them.
	cutoff := dayAdd(weekStart(today), -7*(doneWeeks-1))
	var last *DoneGroup
	for _, d := range days[min(DoneOpenDays, len(days)):] {
		from, to, kind := "", "", DoneWeekGroup
		if d >= cutoff {
			from = weekStart(d)
			to = dayAdd(from, 6)
		} else {
			kind = DoneMonthGroup
			from, to = monthSpan(d)
			if to >= cutoff {
				to = dayAdd(cutoff, -1)
			}
		}
		if to >= oldestOpen {
			to = dayAdd(oldestOpen, -1)
		}
		if last == nil || last.From != from || last.Kind != kind {
			out = append(out, DoneGroup{Kind: kind, From: from, To: to})
			last = &out[len(out)-1]
			if kind == DoneWeekGroup {
				last.Title = weekTitle(from, to, today)
			} else {
				last.Title = monthTitle(from, today)
			}
		}
		last.count(byDay[d])
	}
	if ts := byDay[DoneUndated]; len(ts) > 0 {
		g := DoneGroup{Kind: DoneUndatedGroup, Title: "Undated", From: DoneUndated, To: DoneUndated}
		g.count(ts)
		out = append(out, g)
	}
	if reveal != 0 {
		revealIn(out, done, reveal, loc)
	}
	return out
}

// count adds ts to g's counts.
func (g *DoneGroup) count(ts []*Task) {
	g.Count += len(ts)
	for _, t := range ts {
		if t.Archived {
			g.Archived++
		}
	}
}

// revealIn opens the group of out that holds task id, its rows reaching
// that task.
func revealIn(out []DoneGroup, done []*Task, id int, loc *time.Location) {
	i := slices.IndexFunc(done, func(t *Task) bool { return t.ID == id })
	if i < 0 {
		return
	}
	d := DoneDay(done[i], loc)
	for g := range out {
		if d < out[g].From || d > out[g].To {
			continue
		}
		rows, _, _ := PageDone(done, loc, out[g].From, out[g].To, 0, 0)
		at := slices.IndexFunc(rows, func(t *Task) bool { return t.ID == id })
		if at < len(out[g].Tasks) {
			return
		}
		n := max(at+1, min(DoneOpenRows, len(rows)))
		out[g].Open, out[g].Tasks, out[g].Next = true, rows[:n], 0
		if n < len(rows) {
			out[g].Next = n
		}
		return
	}
}

// PageDone is the page of done, tasks newest first, that finished from
// day from to day to in loc, both inclusive and "" for no bound: limit
// rows from offset (limit 0: all of them), how many the range holds, and
// the offset to read on from, 0 at the end.
func PageDone(done []*Task, loc *time.Location, from, to string, offset, limit int) (page []*Task, total, next int) {
	in := make([]*Task, 0, len(done))
	for _, t := range done {
		d := DoneDay(t, loc)
		if (from == "" || d >= from) && (to == "" || d <= to) {
			in = append(in, t)
		}
	}
	total = len(in)
	offset = max(offset, 0)
	if offset >= total {
		return []*Task{}, total, 0
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
		next = end
	}
	return in[offset:end], total, next
}

func dayOf(d string) time.Time {
	t, _ := time.Parse(time.DateOnly, d)
	return t
}

func dayAdd(d string, n int) string {
	return dayOf(d).AddDate(0, 0, n).Format(time.DateOnly)
}

// weekStart is the Monday of day d's week.
func weekStart(d string) string {
	return dayAdd(d, -(int(dayOf(d).Weekday())+6)%7)
}

// monthSpan are the first and last days of day d's month.
func monthSpan(d string) (first, last string) {
	t := dayOf(d)
	f := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return f.Format(time.DateOnly), f.AddDate(0, 1, -1).Format(time.DateOnly)
}

// dayTitle is day d's heading: Today, Yesterday or "Wed 4 Nov", the year
// too for another year's.
func dayTitle(d, today string) string {
	switch d {
	case today:
		return "Today"
	case dayAdd(today, -1):
		return "Yesterday"
	}
	if d[:4] != today[:4] {
		return dayOf(d).Format("Mon 2 Jan 2006")
	}
	return dayOf(d).Format("Mon 2 Jan")
}

// weekTitle is a week fold's heading from its days: "19–25 Oct",
// "26 Oct – 1 Nov", "3 Nov" for one day, the year too for another year's.
func weekTitle(from, to, today string) string {
	f, t := dayOf(from), dayOf(to)
	year := ""
	if to[:4] != today[:4] {
		year = t.Format(" 2006")
	}
	switch {
	case from == to:
		return t.Format("2 Jan") + year
	case f.Year() != t.Year():
		return f.Format("2 Jan 2006") + " – " + t.Format("2 Jan 2006")
	case f.Month() != t.Month():
		return f.Format("2 Jan") + " – " + t.Format("2 Jan") + year
	}
	return f.Format("2") + "–" + t.Format("2 Jan") + year
}

// monthTitle is a month fold's heading: "September", the year too for
// another year's.
func monthTitle(from, today string) string {
	if from[:4] != today[:4] {
		return dayOf(from).Format("January 2006")
	}
	return dayOf(from).Format("January")
}
