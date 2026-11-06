package server

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestModDone: $.tasks.done's rows say nothing of waits-for, though only
// part of the board is there and a done task's link looks unmet; groups
// count the archive, and a range reads it only when asked.
func TestModDone(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p := newWatchProject(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	board := "# Tasks\n\n## Backlog\n\n### T1 Open\nstatus: open · updated: 2026-11-06\n\n## Done\n\n" +
		"### T2 Done, after an open one\nstatus: done · updated: 2026-11-06\ndone_at: 2026-11-06T10:00:00Z\nafter: T1\n\n"
	if err := os.WriteFile(p.Tasks().TasksPath(), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := "# Archive\n\n### T3 Old\nstatus: done · updated: 2026-08-01\ndone_at: 2026-08-01\n"
	if err := os.WriteFile(p.Tasks().ArchivePath(), []byte(archive), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := modDone(p, modapi.DoneOptions{Groups: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || len(got.Groups) != 2 || got.Groups[0].Title != "Today" || got.Groups[1].Archived != 1 {
		t.Fatalf("groups: %+v", got)
	}
	if r := got.Groups[0].Tasks[0]; r.ID != "T2" || len(r.WaitsFor) != 0 || r.Waiters != 0 {
		t.Errorf("a done row waits: %+v", r)
	}
	if !got.Groups[1].Tasks[0].Archived {
		t.Errorf("the archive's row: %+v", got.Groups[1].Tasks[0])
	}
	if page, err := modDone(p, modapi.DoneOptions{From: "2026-08-01", To: "2026-08-01"}, now); err != nil || page.Total != 0 {
		t.Errorf("a range with no archive: %+v %v", page, err)
	}
	if page, err := modDone(p, modapi.DoneOptions{From: "2026-08-01", To: "2026-08-01", Archived: true}, now); err != nil || page.Total != 1 {
		t.Errorf("a range with the archive: %+v %v", page, err)
	}
}

// TestDoneMark: the line at the human's last action, through both of Done's
// answers: tm's words and place, nothing from git (this project has no
// repo), and the fold holding it open. A coordinator's lines say nothing.
func TestDoneMark(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p := newWatchProject(t)
	now := time.Now().UTC().Truncate(time.Second)
	stamp := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	day := 24 * time.Hour
	var board strings.Builder
	board.WriteString("# Tasks\n\n## Done\n\n")
	for i, d := range []time.Duration{0, day, 2 * day, 3 * day, 4 * day, 5 * day, 6 * day, 100 * day, 110 * day} {
		fmt.Fprintf(&board, "### T%d Done\nstatus: done · updated: %s\ndone_at: %s\n\n", i+1, now.Add(-d).Format(time.DateOnly), stamp(d))
	}
	if err := os.WriteFile(p.Tasks().TasksPath(), []byte(board.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	read := func() (TaskDone, modapi.DoneTasks) {
		t.Helper()
		got, perr := s.taskDone(proto.TaskDoneParams{Project: p.Slug, Groups: true, TZ: "UTC"})
		if perr != nil {
			t.Fatal(perr)
		}
		mods, err := modDone(p, modapi.DoneOptions{Groups: true}, now.Local())
		if err != nil {
			t.Fatal(err)
		}
		return got.(TaskDone), mods
	}
	journal := func(line string) {
		t.Helper()
		if err := mdfile.Append(p.Path("JOURNAL.md"), []byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if got, mods := read(); got.Mark != nil || mods.Mark != nil {
		t.Fatalf("a project nobody acted on has a line: %+v %+v", got.Mark, mods.Mark)
	}
	journal(stamp(time.Hour) + " coordinator task.add T1 x\n")
	if got, _ := read(); got.Mark != nil {
		t.Fatalf("coordinator lines made a line: %+v", got.Mark)
	}
	// Older than the open days, in the fold of T8 and T9.
	journal(stamp(105*day) + " human (iphone: DUO) session.prompt c-1 x\n")
	got, mods := read()
	if got.Mark == nil || mods.Mark == nil || got.Mark.Group != mods.Mark.Group || got.Mark.Rows != 1 || !strings.HasSuffix(got.Mark.Title, "on the iPhone") {
		t.Fatalf("mark %+v, mod's %+v", got.Mark, mods.Mark)
	}
	if g := got.Groups[got.Mark.Group]; !g.Open || len(g.Tasks) != g.Count {
		t.Errorf("the fold holding the line: %+v", g)
	}
	if got.Mark.At != stamp(105*day) {
		t.Errorf("at %s", got.Mark.At)
	}
	// Acted since the last task finished: no line, and the fold folds again.
	journal(now.Add(time.Minute).Format(time.RFC3339) + " human thread.takeover t-1 x\n")
	if got, _ := read(); got.Mark != nil {
		t.Errorf("nothing finished since, and a line: %+v", got.Mark)
	}
}

// TestModDoneQuery: $.tasks.done's query finds done tasks on the board and
// in the archive by the words of their id and title, as the Mac's board
// filter does, and of their status, which it doesn't; the page counts only
// what it found.
func TestModDoneQuery(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p := newWatchProject(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	board := "# Tasks\n\n## Backlog\n\n### T1 Login still open\nstatus: open · updated: 2026-11-06\n\n## Done\n\n" +
		"### T2 Fix the login\nstatus: done · updated: 2026-11-06\ndone_at: 2026-11-06T10:00:00Z\n\n" +
		"### T3 Write docs\nstatus: done · updated: 2026-11-05\ndone_at: 2026-11-05T10:00:00Z\n\n"
	if err := os.WriteFile(p.Tasks().TasksPath(), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := "# Archive\n\n### T4 Le café login\nstatus: done · updated: 2026-08-01\ndone_at: 2026-08-01\n"
	if err := os.WriteFile(p.Tasks().ArchivePath(), []byte(archive), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"LOGIN", []string{"T2", "T4"}},
		{"login fix", []string{"T2"}},
		{"cafe", []string{"T4"}},
		{"t3", []string{"T3"}},
		{"done", []string{"T2", "T3", "T4"}}, // the status, in the TUI's filter alone
		{"review", nil},
		{"done fix", []string{"T2"}},
		{"", []string{"T2", "T3", "T4"}},
	} {
		got, err := modDone(p, modapi.DoneOptions{Query: c.query, Archived: true}, now)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range got.Tasks {
			ids = append(ids, r.ID)
		}
		if !slices.Equal(ids, c.want) || got.Total != len(c.want) {
			t.Errorf("query %q: %v, total %d", c.query, ids, got.Total)
		}
	}
	page, err := modDone(p, modapi.DoneOptions{Query: "login", Archived: true, Limit: 1}, now)
	if err != nil || page.Total != 2 || page.Next != 1 || len(page.Tasks) != 1 {
		t.Errorf("a page: %+v %v", page, err)
	}
}
