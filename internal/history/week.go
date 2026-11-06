package history

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// MaxWeeks is the longest stretch tm week reads, about ten years.
const MaxWeeks = 520

// Week is a stretch's numbers (docs/SPEC.md §5.1, Week; design
// board-library, B9 and B14), for someone to write a page from. It names
// no task or thread, so a page copied from it has no numbers of theirs.
// What a project doesn't have is left out, never said to be missing.
type Week struct {
	// Weeks is how many calendar weeks it covers, this one and those
	// before it; From is their first Monday, To is today, and FromWeek
	// and ToWeek their ISO week numbers.
	Weeks    int    `json:"weeks"`
	From     string `json:"from"`
	To       string `json:"to"`
	FromWeek int    `json:"from_week"`
	ToWeek   int    `json:"to_week"`
	// Done is how many tasks finished in it, Days the days with work,
	// oldest first.
	Done int                `json:"done"`
	Days []proto.HistoryDay `json:"days"`
	// LastFinish is the newest day a task finished, only when none did
	// in the stretch and one did before it.
	LastFinish string `json:"last_finish,omitempty"`
	// Dropped is how many of today's dropped tasks were dropped in it.
	Dropped int `json:"dropped"`
	// StillOpen is every task neither done nor dropped, today.
	StillOpen int `json:"still_open"`
	// Designs are the designs a task of which finished in it or, still
	// open, moved in it: most finished first.
	Designs []WeekDesign `json:"designs,omitempty"`
	// Releases are each repo's in it, a monorepo's by part, in the
	// project's order; never summed.
	Releases []WeekRelease `json:"releases,omitempty"`
}

// WeekDesign is what a stretch did to one design: its tasks that finished
// in it and, today, its open ones.
type WeekDesign struct {
	Design string `json:"design"`
	Title  string `json:"title,omitempty"`
	Done   int    `json:"done"`
	Open   int    `json:"open"`
}

// WeekRelease is one repo's (or monorepo part's) releases in a stretch:
// how many, and its own range, First the oldest's version and Last the
// newest's, the same with one release.
type WeekRelease struct {
	Repo  string `json:"repo"`
	Part  string `json:"part,omitempty"`
	Count int    `json:"count"`
	First string `json:"first"`
	Last  string `json:"last"`
}

// ReadWeek answers project p's last weeks calendar weeks, this one so far
// and those before it, in this machine's time zone.
func ReadWeek(p *project.Project, weeks int) (Week, error) {
	return ReadWeekIn(p, weeks, time.Now(), time.Local)
}

// ReadWeekIn is ReadWeek as of now, with days in loc.
func ReadWeekIn(p *project.Project, weeks int, now time.Time, loc *time.Location) (Week, error) {
	x := indexOf(p)
	s := stretchOf(weeks, now, loc)
	w, first, err := x.week(p, s, loc)
	if err != nil {
		return w, err
	}
	for _, r := range x.repoTags(p, first, loc) {
		w.Releases = append(w.Releases, s.releases(r)...)
	}
	return w, nil
}

// stretch is some calendar weeks, Monday to today, as the instants and
// the days (in its time zone) that bound it.
type stretch struct {
	weeks      int
	start, end time.Time // end is tomorrow's midnight
	from, to   string
}

func stretchOf(weeks int, now time.Time, loc *time.Location) stretch {
	y, m, d := now.In(loc).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	sinceMonday := (int(today.Weekday()) + 6) % 7
	start := today.AddDate(0, 0, -sinceMonday-7*(weeks-1))
	return stretch{weeks: weeks, start: start, end: today.AddDate(0, 0, 1), from: start.Format(time.DateOnly), to: today.Format(time.DateOnly)}
}

func (s stretch) hasDay(day string) bool { return s.from <= day && day <= s.to }

func (s stretch) hasTime(at time.Time) bool { return !at.Before(s.start) && at.Before(s.end) }

// week is the stretch's numbers without its releases, and the project's
// first record, which they need.
func (x *index) week(p *project.Project, s stretch, loc *time.Location) (Week, string, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	all, sources, first, err := x.load(p)
	if err != nil {
		return Week{}, "", err
	}
	runs := runsOf(sources)
	h := numbers(all, runs, first, loc)
	_, fromWeek := s.start.ISOWeek()
	_, toWeek := s.end.AddDate(0, 0, -1).ISOWeek()
	w := Week{Weeks: s.weeks, From: s.from, To: s.to, FromWeek: fromWeek, ToWeek: toWeek, Days: []proto.HistoryDay{}, StillOpen: h.Open}
	var last string
	for _, d := range h.Days {
		switch {
		case s.hasDay(d.Day):
			w.Days = append(w.Days, d)
			w.Done += d.Done
		case d.Day < s.from:
			last = d.Day
		}
	}
	if w.Done == 0 {
		w.LastFinish = last
	}
	moved, droppedAt := movesIn(sources, s)
	done := doneTimes(runs)
	designs := map[string]*WeekDesign{}
	touched := map[string]bool{}
	seen := map[int]bool{}
	for _, t := range all {
		if seen[t.ID] {
			continue // moved to the archive between the two reads
		}
		seen[t.ID] = true
		d := designs[t.Design]
		if d == nil {
			d = &WeekDesign{Design: t.Design}
			designs[t.Design] = d
		}
		switch t.Status {
		case tasks.Done:
			if s.hasDay(dayOf(tasks.DoneValue(t, done), loc)) {
				d.Done++
				touched[t.Design] = true
			}
		case tasks.Dropped:
			at, ok := droppedAt[t.ID]
			if ok && s.hasTime(at) || !ok && s.hasDay(dayOf(t.Updated, loc)) {
				w.Dropped++
			}
		default:
			d.Open++
			if moved[t.ID] {
				touched[t.Design] = true
			}
		}
	}
	delete(touched, "") // tasks of no design
	w.Designs = weekDesigns(p, designs, touched)
	return w, h.FirstRecord, nil
}

// movesIn reads the status lines: the tasks that moved in s, to anything
// but done or dropped, and when each task was last dropped.
func movesIn(sources []*records, s stretch) (moved map[int]bool, droppedAt map[int]time.Time) {
	moved, droppedAt = map[int]bool{}, map[int]time.Time{}
	for _, src := range sources {
		for _, l := range src.status {
			switch l.Status {
			case string(tasks.Dropped):
				droppedAt[l.Task] = l.At
			case string(tasks.Done):
			default:
				if s.hasTime(l.At) {
					moved[l.Task] = true
				}
			}
		}
	}
	return moved, droppedAt
}

// weekDesigns lists the touched designs, with their titles when the
// designs can be read: most finished first, then by name.
func weekDesigns(p *project.Project, designs map[string]*WeekDesign, touched map[string]bool) []WeekDesign {
	if len(touched) == 0 {
		return nil
	}
	titles := map[string]string{}
	if recorded, err := p.Designs(); err == nil {
		for _, d := range recorded {
			titles[d.Name] = textsafe.Line(d.Title, maxTagWords)
		}
	}
	var out []WeekDesign
	for _, name := range slices.Sorted(maps.Keys(touched)) {
		d := *designs[name]
		d.Title = titles[name]
		out = append(out, d)
	}
	slices.SortStableFunc(out, func(a, b WeekDesign) int { return cmp.Compare(b.Done, a.Done) })
	return out
}

// releases are repo r's in s: one row for each part, each with its own
// range. r's releases are oldest first.
func (s stretch) releases(r taggedRepo) []WeekRelease {
	rows := map[string]*WeekRelease{}
	for _, rel := range r.releases {
		if !s.hasTime(rel.At) {
			continue
		}
		version := textsafe.Line(rel.Version, maxTagWords)
		row := rows[rel.Part]
		if row == nil {
			row = &WeekRelease{Repo: r.name, Part: textsafe.Line(rel.Part, maxTagWords), First: version}
			rows[rel.Part] = row
		}
		row.Count++
		row.Last = version
	}
	var out []WeekRelease
	for _, part := range slices.Sorted(maps.Keys(rows)) {
		out = append(out, *rows[part])
	}
	return out
}
