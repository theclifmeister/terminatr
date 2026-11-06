package server

import (
	"os"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modapi"
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
