package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// weekNow is Friday of ISO week 45, 2026: the stretch starts Monday 2 Nov.
var weekNow = time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)

func readWeek(t *testing.T, p *project.Project, weeks int) Week {
	t.Helper()
	w, err := ReadWeekIn(p, weeks, weekNow, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func writeJournal(t *testing.T, p *project.Project, lines string) {
	t.Helper()
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n"+lines), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dayCounts(w Week) string {
	var out []string
	for _, d := range w.Days {
		out = append(out, fmt.Sprintf("%s:%d", d.Day, d.Done))
	}
	return strings.Join(out, " ")
}

func TestWeekBounds(t *testing.T) {
	for _, c := range []struct {
		now              time.Time
		weeks            int
		from, to         string
		fromWeek, toWeek int
	}{
		{weekNow, 1, "2026-11-02", "2026-11-06", 45, 45},
		{weekNow, 4, "2026-10-12", "2026-11-06", 42, 45},
		{time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), 1, "2026-11-02", "2026-11-02", 45, 45},   // a Monday
		{time.Date(2026, 11, 8, 23, 59, 0, 0, time.UTC), 1, "2026-11-02", "2026-11-08", 45, 45}, // its Sunday
		{time.Date(2027, 1, 6, 12, 0, 0, 0, time.UTC), 2, "2026-12-28", "2027-01-06", 53, 1},    // over a new year
	} {
		s := stretchOf(c.weeks, c.now, time.UTC)
		_, fw := s.start.ISOWeek()
		_, tw := s.end.AddDate(0, 0, -1).ISOWeek()
		if s.from != c.from || s.to != c.to || fw != c.fromWeek || tw != c.toWeek {
			t.Errorf("%v %d weeks: %s to %s, weeks %d to %d", c.now, c.weeks, s.from, s.to, fw, tw)
		}
	}
	// Midnight is the zone's: Monday 00:30 in Berlin is still Sunday in UTC.
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip(err)
	}
	s := stretchOf(1, time.Date(2026, 11, 8, 23, 30, 0, 0, time.UTC), berlin)
	if s.from != "2026-11-09" || s.to != "2026-11-09" {
		t.Errorf("Berlin: %s to %s", s.from, s.to)
	}
}

// TestWeekNumbers: done per day inside the stretch only, by the task's
// finish, on the board and in the archive, with still open as of today.
func TestWeekNumbers(t *testing.T) {
	p := newProject(t)
	writeTasks(t, p, []*tasks.Task{
		done(1, "2026-11-02T09:00:00Z"), done(2, "2026-11-03T09:00:00Z"), done(3, "2026-11-03"),
		done(4, "2026-11-01T23:00:00Z"), // Sunday, last week
		{ID: 5, Title: "open", Status: tasks.Started, Created: "2026-10-01", Updated: "2026-11-01"},
	}, []*tasks.Task{done(6, "2026-11-06T09:00:00Z")})
	w := readWeek(t, p, 1)
	if got, want := dayCounts(w), "2026-11-02:1 2026-11-03:2 2026-11-06:1"; got != want {
		t.Errorf("days %q, want %q", got, want)
	}
	if w.Done != 4 || w.StillOpen != 1 || w.Dropped != 0 || w.LastFinish != "" || w.Weeks != 1 || w.FromWeek != 45 || w.ToWeek != 45 {
		t.Errorf("numbers: %+v", w)
	}
	if w4 := readWeek(t, p, 4); w4.Done != 5 || w4.From != "2026-10-12" || w4.FromWeek != 42 {
		t.Errorf("four weeks: %+v", w4)
	}
}

// TestWeekEmpty: nothing finished gives no days and the last finish; a
// project that never finished a task has none to give.
func TestWeekEmpty(t *testing.T) {
	p := newProject(t)
	w := readWeek(t, p, 1)
	if w.Done != 0 || w.Days == nil || len(w.Days) != 0 || w.LastFinish != "" || w.Designs != nil || w.Releases != nil {
		t.Errorf("no records: %+v", w)
	}
	writeTasks(t, p, []*tasks.Task{done(1, "2026-10-19T09:00:00Z"), done(2, "2026-10-12T09:00:00Z")}, nil)
	w = readWeek(t, p, 1)
	if w.Done != 0 || len(w.Days) != 0 || w.LastFinish != "2026-10-19" {
		t.Errorf("a quiet week: %+v", w)
	}
	if w = readWeek(t, p, 4); w.Done != 2 || w.LastFinish != "" {
		t.Errorf("a stretch with finishes has no last finish: %+v", w)
	}
}

// TestWeekOnlyDropped: dropping tasks isn't finishing them or touching
// their design, whether the journal or the task says when.
func TestWeekOnlyDropped(t *testing.T) {
	p := newProject(t)
	writeTasks(t, p, []*tasks.Task{
		done(1, "2026-10-19T09:00:00Z"),
		{ID: 2, Title: "a", Status: tasks.Dropped, Design: "site", Created: "2026-10-01", Updated: "2026-11-03"},
		{ID: 3, Title: "b", Status: tasks.Dropped, Design: "site", Created: "2026-10-01", Updated: "2026-11-05"},
		{ID: 4, Title: "old", Status: tasks.Dropped, Created: "2026-10-01", Updated: "2026-10-01"},
	}, nil)
	writeJournal(t, p, "2026-11-03T08:00:00Z coordinator task.status T2 started\n"+
		"2026-11-03T09:00:00Z coordinator task.status T2 dropped\n")
	w := readWeek(t, p, 1)
	if w.Done != 0 || len(w.Days) != 0 || w.Dropped != 2 || w.StillOpen != 0 || w.LastFinish != "2026-10-19" || w.Designs != nil {
		t.Errorf("only dropped: %+v", w)
	}
}

// TestWeekDesigns: a design is touched by a task that finished in the
// stretch or, still open, moved in it; its open tasks count whenever they
// moved, and a design nothing touched, or none at all, adds nothing.
func TestWeekDesigns(t *testing.T) {
	p := newProject(t)
	title := "Board and Library"
	if _, _, err := p.SetDesign("board", project.DesignEdit{Title: &title}); err != nil {
		t.Fatal(err)
	}
	d := func(tk *tasks.Task, design string) *tasks.Task { tk.Design = design; return tk }
	open := func(id int) *tasks.Task {
		return &tasks.Task{ID: id, Title: "open", Status: tasks.Started, Created: "2026-10-01", Updated: "2026-10-01"}
	}
	writeTasks(t, p, []*tasks.Task{
		d(done(1, "2026-11-02T09:00:00Z"), "board"), d(done(2, "2026-11-03T09:00:00Z"), "board"), d(open(3), "board"),
		d(open(4), "moved"),
		d(done(5, "2026-11-04T09:00:00Z"), "small"),
		d(done(6, "2026-10-04T09:00:00Z"), "untouched"), d(open(7), "untouched"),
		done(8, "2026-11-05T09:00:00Z"),
	}, nil)
	writeJournal(t, p, "2026-11-04T08:00:00Z coordinator task.status T4 started\n"+
		"2026-10-20T08:00:00Z coordinator task.status T7 started\n")
	w := readWeek(t, p, 1)
	want := []WeekDesign{{Design: "board", Title: title, Done: 2, Open: 1}, {Design: "small", Done: 1}, {Design: "moved", Open: 1}}
	if fmt.Sprint(w.Designs) != fmt.Sprint(want) {
		t.Errorf("designs %+v, want %+v", w.Designs, want)
	}
	if w.Done != 4 || w.StillOpen != 3 {
		t.Errorf("numbers: %+v", w)
	}

	plain := newProject(t)
	writeTasks(t, plain, []*tasks.Task{done(1, "2026-11-02T09:00:00Z")}, nil)
	data, err := json.Marshal(readWeek(t, plain, 1))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "designs") {
		t.Errorf("no designs, but the numbers say: %s", data)
	}
}

// TestWeekReleases: each repo keeps its own range in the stretch, the
// same version in two stays in both, a monorepo's parts each have theirs,
// nothing adds them up, and releases before the stretch stay out.
func TestWeekReleases(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	web, _ := gittest.Repo(t)
	site, _ := gittest.Repo(t)
	mono, _ := gittest.Repo(t)
	quiet, _ := gittest.Repo(t)
	none, _ := gittest.Repo(t)
	tagAt(t, web, "2026-10-20T12:00:00Z", "v0.9.0")
	tagAt(t, web, "2026-11-02T12:00:00Z", "v1.0.0")
	tagAt(t, web, "2026-11-04T12:00:00Z", "v1.1.0")
	tagAt(t, web, "2026-11-05T12:00:00Z", "v1.2.0")
	tagAt(t, site, "2026-11-03T12:00:00Z", "v1.0.0")
	tagAt(t, mono, "2026-11-03T12:00:00Z", "api/v1.2")
	tagAt(t, mono, "2026-11-04T12:00:00Z", "api/v1.3", "web/v2.0")
	tagAt(t, quiet, "2026-10-20T12:00:00Z", "v3.0.0")
	p := releaseProject(t, web, site, mono, quiet, none)
	w := readWeek(t, p, 1)
	got, err := json.Marshal(w.Releases)
	if err != nil {
		t.Fatal(err)
	}
	wantRows := []WeekRelease{
		{Repo: filepath.Base(web), Count: 3, First: "1.0.0", Last: "1.2.0"},
		{Repo: filepath.Base(site), Count: 1, First: "1.0.0", Last: "1.0.0"},
		{Repo: filepath.Base(mono), Part: "api", Count: 2, First: "1.2", Last: "1.3"},
		{Repo: filepath.Base(mono), Part: "web", Count: 1, First: "2.0", Last: "2.0"},
	}
	want, _ := json.Marshal(wantRows)
	if string(got) != string(want) {
		t.Errorf("releases\n%s\nwant\n%s", got, want)
	}
	if err := config.SetProject(p.Slug, "release_tags", []string{config.ReleaseTagsOff}); err != nil {
		t.Fatal(err)
	}
	if w := readWeek(t, p, 1); w.Releases != nil {
		t.Errorf("release_tags none: %+v", w.Releases)
	}
}

// TestWeekPlainFolderCallsNoGit: a plain folder runs no git for a week.
func TestWeekPlainFolderCallsNoGit(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	log := spyGit(t)
	if w := readWeek(t, releaseProject(t), 1); w.Releases != nil {
		t.Errorf("releases: %+v", w.Releases)
	}
	if data, err := os.ReadFile(log); err == nil {
		t.Errorf("git ran: %s", data)
	}
}
