package history

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Pulse's thresholds (design board-library, B16), rules on what the
// project has, so nobody has to set them.
const (
	// pulseDone and pulseDays: it appears at 10 done tasks across 7 days,
	// counted from the first day with work to today.
	pulseDone = 10
	pulseDays = 7
	// pulseDayBars: bars are days up to 60 days, then weeks, and from
	// pulseWeekBars days, two years, months.
	pulseDayBars  = 60
	pulseWeekBars = 730
)

// PulseOf is the Pulse of h as of now, in now's time zone: nil before it
// appears. named says the project has several repos, whose releases are then each named
// and never summed. Only releases in the span count for a dot.
func PulseOf(h proto.History, named bool, now time.Time) *proto.HistoryPulse {
	today := now.Format(time.DateOnly)
	if h.Done < pulseDone || len(h.Days) == 0 || h.Days[0].Day > today {
		return nil
	}
	if daysBetween(h.Days[0].Day, today)+1 < pulseDays {
		return nil
	}
	s := spansOf(h, named, now)
	p := &proto.HistoryPulse{Unit: s.unit, Span: s.words, Releases: pulseReleaseWords(h.Releases, named), Bars: s.bars}
	if last := &p.Bars[len(p.Bars)-1]; last.To >= today {
		last.Label = "today"
	}
	return p
}

// spans are a project's bars, from the first with work to the one holding
// today, and what each repo released in each.
type spans struct {
	unit, words string
	bars        []proto.PulseBar
	released    [][]int // by bar, by repo
}

// spansOf is the spans of h, which has a day with work and none after now.
// A bar stands for every day, week or month, so a span with no work is a gap.
func spansOf(h proto.History, named bool, now time.Time) spans {
	today := now.Format(time.DateOnly)
	first := h.Days[0].Day
	span := daysBetween(first, today) + 1
	done := make(map[string]int, len(h.Days))
	for _, d := range h.Days {
		done[d.Day] = d.Done
	}
	released := releaseDays(h.Releases, now.Location())

	s := spans{words: spanWords(span)}
	switch {
	case span <= pulseDayBars:
		s.unit = proto.PulseDay
	case span <= pulseWeekBars:
		s.unit, first = proto.PulseWeek, mondayOf(first)
	default:
		s.unit, first = proto.PulseMonth, first[:len("2006-01")]+"-01"
	}
	for from := first; from <= today; from = nextDay(endOf(s.unit, from)) {
		bar := proto.PulseBar{From: from, To: endOf(s.unit, from), Label: barLabel(s.unit, from)}
		counts := make([]int, len(h.Releases))
		for d := bar.From; d <= bar.To && d <= today; d = nextDay(d) {
			bar.Done += done[d]
			for repo, n := range released[d] {
				counts[repo] += n
			}
		}
		for _, n := range counts {
			bar.Released = bar.Released || n > 0
		}
		bar.Tip = barTip(s.unit, bar, h.Releases, counts, named)
		s.bars = append(s.bars, bar)
		s.released = append(s.released, counts)
	}
	return s
}

// releaseDays counts each repo's releases by day, its index in repos.
func releaseDays(repos []proto.HistoryRepo, loc *time.Location) map[string]map[int]int {
	out := map[string]map[int]int{}
	for i, r := range repos {
		for _, x := range r.Releases {
			at, err := time.Parse(time.RFC3339, x.At)
			if err != nil {
				continue
			}
			d := at.In(loc).Format(time.DateOnly)
			if out[d] == nil {
				out[d] = map[int]int{}
			}
			out[d][i]++
		}
	}
	return out
}

// ReleaseWords lists each repo's count, a monorepo's by part, never a
// total: "terminatr 128 · site 6", "mono api 3 · mono web 2".
func ReleaseWords(repos []proto.HistoryRepo) string {
	var words []string
	for _, r := range repos {
		if len(r.Parts) == 0 {
			words = append(words, fmt.Sprintf("%s %d", r.Repo, r.Count))
		}
		for _, p := range r.Parts {
			words = append(words, strings.TrimSpace(fmt.Sprintf("%s %s", r.Repo, p.Part))+fmt.Sprintf(" %d", p.Count))
		}
	}
	return strings.Join(words, " · ")
}

// pulseReleaseWords is the stat line's words of the releases: "128
// releases", or with several repos each one's, "terminatr 128 · site 6
// releases"; "" with none, where they are left out.
func pulseReleaseWords(repos []proto.HistoryRepo, named bool) string {
	if len(repos) == 0 {
		return ""
	}
	if named {
		total := 0
		for _, r := range repos {
			total += r.Count
		}
		return ReleaseWords(repos) + releaseWord(total)
	}
	return fmt.Sprintf("%d%s", repos[0].Count, releaseWord(repos[0].Count))
}

// releaseWord follows a count in words, so "site 1 release" reads right; a
// total is only ever asked of it here, never shown.
func releaseWord(n int) string {
	if n == 1 {
		return " release"
	}
	return " releases"
}

// barTip is what a bar says of itself: "Mon 5 Oct: 28 done, 8 releases",
// with several repos "Mon 5 Oct: 28 done, terminatr 2 · site 1 releases".
func barTip(unit string, bar proto.PulseBar, repos []proto.HistoryRepo, counts []int, named bool) string {
	var parts, each []string
	if bar.Done > 0 {
		parts = append(parts, fmt.Sprintf("%d done", bar.Done))
	}
	single := 0 // only ever to say "release" or "releases"
	for i, n := range counts {
		if n > 0 {
			each = append(each, fmt.Sprintf("%s %d", repos[i].Repo, n))
			single += n
		}
	}
	switch {
	case len(each) == 0:
	case named:
		parts = append(parts, strings.Join(each, " · ")+releaseWord(single))
	default:
		parts = append(parts, fmt.Sprintf("%d%s", single, releaseWord(single)))
	}
	if len(parts) == 0 {
		parts = []string{"nothing done"}
	}
	return barTitle(unit, bar.From, bar.To) + ": " + strings.Join(parts, ", ")
}

// barTitle names a bar's span: "Mon 5 Oct", "2–8 Nov", "28 Sep – 4 Oct",
// "October 2026".
func barTitle(unit, from, to string) string {
	f, t := dayTime(from), dayTime(to)
	switch unit {
	case proto.PulseWeek:
		if f.Month() == t.Month() {
			return fmt.Sprintf("%d–%s", f.Day(), t.Format("2 Jan"))
		}
		return f.Format("2 Jan") + " – " + t.Format("2 Jan")
	case proto.PulseMonth:
		return f.Format("January 2006")
	}
	return f.Format("Mon 2 Jan")
}

// barLabel is a bar's name on the band's axis: "5 Oct", or "Oct 2026" for
// a month.
func barLabel(unit, from string) string {
	if unit == proto.PulseMonth {
		return dayTime(from).Format("Jan 2006")
	}
	return dayTime(from).Format("2 Jan")
}

// spanWords says how long the work has run: "33 days", "5 months", "3
// years".
func spanWords(days int) string {
	plural := func(n int, what string) string {
		n = max(n, 1)
		if n == 1 {
			return "1 " + what
		}
		return fmt.Sprintf("%d %ss", n, what)
	}
	switch {
	case days <= pulseDayBars:
		return plural(days, "day")
	case days <= pulseWeekBars:
		return plural(cmp.Or(int(float64(days)/30.44+0.5), 1), "month")
	}
	return plural(int(float64(days)/365.25+0.5), "year")
}

func dayTime(d string) time.Time {
	t, _ := time.Parse(time.DateOnly, d)
	return t
}

func nextDay(d string) string {
	return dayTime(d).AddDate(0, 0, 1).Format(time.DateOnly)
}

func daysBetween(from, to string) int {
	return int(dayTime(to).Sub(dayTime(from)).Hours() / 24)
}

// mondayOf is the Monday of d's week, as Done's weeks start
// (tasks.GroupDone).
func mondayOf(d string) string {
	t := dayTime(d)
	return t.AddDate(0, 0, -(int(t.Weekday())+6)%7).Format(time.DateOnly)
}

// endOf is the last day of the bar starting at from.
func endOf(unit, from string) string {
	t := dayTime(from)
	switch unit {
	case proto.PulseWeek:
		t = t.AddDate(0, 0, 6)
	case proto.PulseMonth:
		t = t.AddDate(0, 1, -1)
	}
	return t.Format(time.DateOnly)
}
