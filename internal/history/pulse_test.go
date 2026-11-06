package history

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

var pulseNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// doneDays is a History with n tasks done on each of days, oldest first.
func doneDays(n int, days ...string) proto.History {
	h := proto.History{}
	for _, d := range days {
		h.Days = append(h.Days, proto.HistoryDay{Day: d, Done: n})
		h.Done += n
	}
	return h
}

func TestPulseAppearsAtTenDoneAcrossSevenDays(t *testing.T) {
	cases := []struct {
		name string
		h    proto.History
		want bool
	}{
		{"nothing done", proto.History{}, false},
		{"9 done tasks", doneDays(3, "2026-10-01", "2026-10-15", "2026-11-01"), false},
		{"10 done in two days", doneDays(5, "2026-11-05", "2026-11-06"), false},
		{"10 done across six days", doneDays(5, "2026-11-01", "2026-11-06"), false},
		{"10 done across seven days", doneDays(5, "2026-10-31", "2026-11-06"), true},
		{"a day past today", doneDays(10, "2026-11-07"), false},
	}
	for _, c := range cases {
		if got := PulseOf(c.h, false, pulseNow) != nil; got != c.want {
			t.Errorf("%s: shown %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPulseBarsAreDaysThenWeeksThenMonths(t *testing.T) {
	// Up to 60 days, one bar a day, a day with no work a gap.
	p := PulseOf(doneDays(5, "2026-09-08", "2026-11-06"), false, pulseNow) // 60 days
	if p == nil || p.Unit != proto.PulseDay || len(p.Bars) != 60 || p.Span != "60 days" {
		t.Fatalf("60 days: %+v", p)
	}
	if b := p.Bars[1]; b.Done != 0 || b.Tip != "Wed 9 Sep: nothing done" {
		t.Errorf("a day with no work: %+v", b)
	}
	if b := p.Bars[0]; b.From != "2026-09-08" || b.To != b.From || b.Label != "8 Sep" || b.Tip != "Tue 8 Sep: 5 done" {
		t.Errorf("the first day: %+v", b)
	}
	if b := p.Bars[59]; b.Label != "today" {
		t.Errorf("the last day: %+v", b)
	}

	// 14 tasks over 5 months: weeks.
	h := doneDays(2, "2026-06-10", "2026-07-02", "2026-08-11", "2026-09-01", "2026-09-30", "2026-10-21", "2026-11-02")
	p = PulseOf(h, false, pulseNow)
	if p == nil || p.Unit != proto.PulseWeek || p.Span != "5 months" {
		t.Fatalf("14 tasks over 5 months: %+v", p)
	}
	if b := p.Bars[0]; b.From != "2026-06-08" || b.To != "2026-06-14" || b.Done != 2 || b.Tip != "8–14 Jun: 2 done" {
		t.Errorf("first week: %+v", b)
	}
	last := p.Bars[len(p.Bars)-1]
	if last.From != "2026-11-02" || last.To != "2026-11-08" || last.Label != "today" || last.Done != 2 {
		t.Errorf("this week: %+v", last)
	}
	sum := 0
	for i, b := range p.Bars {
		sum += b.Done
		if i > 0 && b.From != nextDay(p.Bars[i-1].To) {
			t.Errorf("bar %d doesn't follow the last: %+v", i, b)
		}
	}
	if sum != 14 {
		t.Errorf("the weeks hold %d, want 14", sum)
	}

	// A week across two months names both.
	p = PulseOf(doneDays(5, "2026-06-29", "2026-10-01"), false, pulseNow)
	if b := p.Bars[0]; b.Tip != "29 Jun – 5 Jul: 5 done" {
		t.Errorf("week across months: %+v", b)
	}

	// Past two years, months.
	p = PulseOf(doneDays(5, "2024-02-15", "2026-11-01"), false, pulseNow)
	if p == nil || p.Unit != proto.PulseMonth || p.Span != "3 years" {
		t.Fatalf("months: %+v", p)
	}
	if b := p.Bars[0]; b.From != "2024-02-01" || b.To != "2024-02-29" || b.Label != "Feb 2024" || b.Tip != "February 2024: 5 done" {
		t.Errorf("first month: %+v", b)
	}
	if len(p.Bars) != 34 {
		t.Errorf("%d months, want 34", len(p.Bars))
	}
}

func release(at string) proto.HistoryRelease {
	return proto.HistoryRelease{Tag: "v1", Version: "1", At: at}
}

func TestPulseWithoutReleasesLeavesThemOut(t *testing.T) {
	p := PulseOf(doneDays(5, "2026-10-01", "2026-11-06"), false, pulseNow)
	if p.Releases != "" {
		t.Errorf("words %q", p.Releases)
	}
	for _, b := range p.Bars {
		if b.Released || strings.Contains(b.Tip, "release") {
			t.Fatalf("%+v", b)
		}
	}
}

func TestPulseReleaseDotsAreDaysInTheZone(t *testing.T) {
	h := doneDays(5, "2026-10-01", "2026-11-06")
	h.Releases = []proto.HistoryRepo{{Repo: "app", Count: 2, Releases: []proto.HistoryRelease{
		release("2026-10-01T23:30:00Z"), // 2 Oct in Amsterdam
		release("2026-09-01T10:00:00Z"), // before the first bar
	}}}
	loc := time.FixedZone("CEST", 2*3600)
	p := PulseOf(h, false, pulseNow.In(loc))
	if p.Releases != "2 releases" {
		t.Errorf("words %q", p.Releases)
	}
	if p.Bars[0].Released || !p.Bars[1].Released {
		t.Errorf("dots: %+v %+v", p.Bars[0], p.Bars[1])
	}
	if p.Bars[1].Tip != "Fri 2 Oct: 1 release" {
		t.Errorf("a day with a release and no work: %q", p.Bars[1].Tip)
	}
}

func TestPulseSeveralReposAreNamedAndNeverSummed(t *testing.T) {
	h := doneDays(5, "2026-10-30", "2026-11-06")
	h.Releases = []proto.HistoryRepo{
		{Repo: "terminatr", Count: 128, Releases: []proto.HistoryRelease{release("2026-11-06T05:03:00Z"), release("2026-11-06T06:00:00Z")}},
		{Repo: "site", Count: 6, Releases: []proto.HistoryRelease{release("2026-11-06T07:40:00Z")}},
	}
	p := PulseOf(h, true, pulseNow)
	if p.Releases != "terminatr 128 · site 6 releases" {
		t.Errorf("words %q", p.Releases)
	}
	last := p.Bars[len(p.Bars)-1]
	if !last.Released || last.Tip != "Fri 6 Nov: 5 done, terminatr 2 · site 1 releases" {
		t.Errorf("last day: %+v", last)
	}
	// One repo of several released: still named.
	h.Releases = h.Releases[1:]
	p = PulseOf(h, true, pulseNow)
	last = p.Bars[len(p.Bars)-1]
	if p.Releases != "site 6 releases" || last.Tip != "Fri 6 Nov: 5 done, site 1 release" {
		t.Errorf("one of several: %q, %q", p.Releases, last.Tip)
	}
}
