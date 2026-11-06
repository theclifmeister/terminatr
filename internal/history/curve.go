package history

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// CurveOf is the History view's chart of h as of now, in now's time zone
// (design board-library, B6 and B16): nil before the first finish. Unlike
// the Pulse it has no threshold, so a young project gets the curve with
// what it has. named says the project has several repos, whose releases
// are then each named and never summed.
func CurveOf(h proto.History, named bool, now time.Time) *proto.HistoryCurve {
	today := now.Format(time.DateOnly)
	if len(h.Days) == 0 || h.Days[0].Day > today {
		return nil
	}
	s := spansOf(h, named, now)
	c := &proto.HistoryCurve{Unit: s.unit, Span: s.words, Range: spanRange(h.Days[0].Day, today, now), Releases: pulseReleaseWords(h.Releases, named)}
	total := 0
	for _, bar := range s.bars {
		total += bar.Done
		c.Points = append(c.Points, proto.CurvePoint{From: bar.From, To: bar.To, Label: bar.Label, Done: bar.Done, Total: total,
			Tip: fmt.Sprintf("%s, %d in all", bar.Tip, total)})
		if bar.Done > c.Best.Done {
			c.Best = proto.CurveBest{Done: bar.Done, Word: "best " + s.unit, When: bestWhen(s.unit, bar)}
		}
	}
	for repo, r := range h.Releases {
		var points []int
		for i := range s.bars {
			if s.released[i][repo] > 0 {
				points = append(points, i)
			}
		}
		if len(points) > 0 {
			t := proto.CurveTicks{Points: points}
			if named {
				t.Repo = r.Repo
			}
			c.Ticks = append(c.Ticks, t)
		}
	}
	return c
}

// bestWhen names the best span: "12 Oct", "2–8 Nov", "October 2026".
func bestWhen(unit string, bar proto.PulseBar) string {
	if unit == proto.PulseDay {
		return bar.Label
	}
	return barTitle(unit, bar.From, bar.To)
}

// spanRange is two days in words: "5 Nov", "5–6 Nov", "28 Sep – 4 Oct", each
// with its year when it isn't now's.
func spanRange(from, to string, now time.Time) string {
	f, t := dayTime(from), dayTime(to)
	word := func(d time.Time) string {
		if d.Year() != now.Year() {
			return d.Format("2 Jan 2006")
		}
		return d.Format("2 Jan")
	}
	switch {
	case from == to:
		return word(f)
	case f.Year() == t.Year() && f.Month() == t.Month():
		return fmt.Sprintf("%d–%s", f.Day(), word(t))
	}
	return word(f) + " – " + word(t)
}

// chapters are p's designs as parts of its history (design board-library,
// B15), and how many done tasks belong to one: nothing with no designs, and
// designs that can't be read are no reason to fail the numbers. A design
// with no task to build has nothing to show here.
func (x *index) chapters(p *project.Project, curve *proto.HistoryCurve, now time.Time) ([]proto.HistoryChapter, int) {
	builds, err := p.BoardDesigns()
	if err != nil {
		return nil, 0
	}
	x.mu.Lock()
	doneAt := x.doneAt
	x.mu.Unlock()
	var out []proto.HistoryChapter
	belong := 0
	for _, b := range builds {
		c := proto.HistoryChapter{Name: b.Name, Title: textsafe.Line(cmp.Or(b.Title, b.Name), maxChapterWords)}
		for _, t := range b.Build {
			if t.Status == tasks.Dropped {
				continue
			}
			c.Total++
			if t.Status != tasks.Done {
				continue
			}
			c.Done++
			day := dayOf(tasks.DoneValue(t, doneAt), now.Location())
			if day == "" {
				continue
			}
			if c.From == "" || day < c.From {
				c.From = day
			}
			c.To = max(c.To, day)
		}
		if c.Total == 0 {
			continue
		}
		belong += c.Done
		if c.From != "" {
			c.Label = spanRange(c.From, c.To, now)
			c.Flag = flagOf(curve, c.From)
		}
		out = append(out, c)
	}
	// Newest first, so one with none done yet, the work ahead, tops the list.
	slices.SortStableFunc(out, func(a, b proto.HistoryChapter) int {
		if a.To != b.To {
			if a.To == "" {
				return -1
			}
			if b.To == "" {
				return 1
			}
			return strings.Compare(b.To, a.To)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out, belong
}

// maxChapterWords cuts a design's title for a console.
const maxChapterWords = 200

// flagOf is the point of curve holding day, from 1; 0 for none.
func flagOf(curve *proto.HistoryCurve, day string) int {
	if curve == nil {
		return 0
	}
	for i, p := range curve.Points {
		if p.From <= day && day <= p.To {
			return i + 1
		}
	}
	return 0
}
