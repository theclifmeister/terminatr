package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestDaily (T719): the daily notes digest every project's journal by
// local day: each section from its actions, ids linked, titles from the
// board, never a line's detail; what needed the user at the day's end,
// tagged while it still does; days back-filled from the archives; a
// second render writes nothing.
func TestDaily(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	human := caller.Caller{Kind: caller.Human}
	demo, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := demo.Tasks().Add(human, []tasks.NewTask{{Title: "Ship <b>it</b>"}, {Title: "Review me"}, {Title: "Old"}}); err != nil {
		t.Fatal(err)
	}
	demo.Tasks().Archive(human, 3)
	other, err := project.New(project.Options{Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	// The server's zone is two hours ahead of UTC: 22:30Z on the 23rd is
	// the 24th there, 21:00Z still the 23rd.
	zone := time.FixedZone("XST", 2*3600)
	journal := func(p *project.Project, lines ...string) {
		t.Helper()
		if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n"+strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	journal(demo,
		"2026-09-10T10:00:00Z coordinator task.add T3 Old SECRET",
		"2026-09-10T11:00:00Z coordinator task.status T3 done (approved by the user)",
		"2026-10-23T21:00:00Z coordinator task.add T1 Ship it SECRET",
		"2026-10-23T22:30:00Z coordinator task.add T2 Review me",
		"2026-10-23T22:31:00Z coordinator thread.start t-0001 T1 Ship it SECRET (approved by the user)",
		"2026-10-23T22:31:01Z coordinator task.thread T1 t-0001",
		"2026-10-24T08:00:00Z t-0001 task.steps.check T1 1",
		"2026-10-24T08:00:01Z t-0001 task.steps.check T1 2",
		"2026-10-24T08:01:00Z t-0001 run.start make test",
		"2026-10-24T08:02:00Z t-0001 run.end make test exit 0",
		"2026-10-24T08:03:00Z t-0001 guard.deny t-0001 tm-files Bash: rm SECRET",
		"2026-10-24T08:04:00Z t-0001 guard.deny t-0001 tm-files Bash: rm again",
		"2026-10-24T08:05:00Z coordinator guard.deny s-12 credentials Bash: cat SECRET",
		"2026-10-24T09:00:00Z coordinator thread.merge t-0001 PR #12 merged at abc1234 (squash)",
		"2026-10-24T09:00:05Z ticker task.done T1 merged (PR #12)",
		"2026-10-24T09:10:00Z coordinator task.status T2 review: SECRET check",
		"2026-10-24T09:20:00Z coordinator ask.add Q1 T2 SECRET question",
		"2026-10-24T09:21:00Z coordinator ask.add Q2 another",
		"2026-10-24T09:22:00Z coordinator ask.done Q1",
		"2026-10-24T09:30:00Z coordinator memory.edit memory/decisions.md",
		"2026-10-24T09:31:00Z coordinator memory.rm memory/old.md",
		"2026-10-24T09:32:00Z coordinator memory.edit ../../etc/passwd",
		"2026-10-24T10:00:00Z coordinator link.send other M-3 (12 bytes)",
		"2026-10-24T10:01:00Z coordinator link.held mini/acme M-4 (9 bytes)",
		"2026-10-24T10:02:00Z ticker link.delivered other M-3",
		"2026-10-24T10:03:00Z ticker repo.fast-forward main abc..def",
		"2026-10-24T10:04:00Z coordinator thread.prompt t-0001 SECRET words",
		"2026-10-24T10:05:00Z coordinator settings.links SECRET",
		"not a line SECRET",
		// The 25th, from 22:00Z on the 24th: T2 is accepted, Q2 stays.
		"2026-10-24T22:00:00Z coordinator task.status T2 done (approved by the user)",
	)
	other.Tasks().Add(human, []tasks.NewTask{{Title: "Elsewhere"}})
	journal(other, "2026-10-24T12:00:00Z coordinator task.add T1 Elsewhere")
	// The September lines go to journal/2026-09.md.gz.
	if n, err := demo.ArchiveJournal(time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), 30*24*time.Hour); err != nil || n != 2 {
		t.Fatalf("archive: %d, %v", n, err)
	}

	now := time.Date(2026, 10, 25, 9, 0, 0, 0, zone)
	if _, err := RenderDaily(now, 0); err == nil {
		t.Fatal("0 days rendered")
	}
	w, err := RenderDaily(now, TickerDays)
	if err != nil || w != 2 {
		t.Fatalf("render: %d written, %v", w, err)
	}
	read := func(date string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "projects", "_tm", "daily", date+".md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	day := read("2026-10-24")
	for _, want := range []string{
		`date: "2026-10-24"`, "projects:\n  - \"[[demo/PROJECT|demo]]\"\n  - \"[[other/PROJECT|other]]\"\n",
		"shipped: 1", "started: 1", "needs_you: 3", "tags:\n  - \"day\"\n  - \"needs-you\"\n", `tm: "owned"`,
		"# Saturday 24 October 2026\n",
		"## [[demo/PROJECT|demo]]\n",
		"### Shipped\n\n- [[demo/tasks/T1|T1]] Ship &lt;b>it&lt;/b> · #12 · [[demo/threads/t-0001/t-0001|t-0001]]\n",
		"### Started\n\n- [[demo/tasks/T1|T1]] Ship &lt;b>it&lt;/b> → [[demo/threads/t-0001/t-0001|t-0001]]\n- 1 task added: [[demo/tasks/T2|T2]]\n",
		"### Decided\n\n- 1 question answered: Q1 ([[demo/tasks/T2|T2]])\n- memory: [[demo/memory/decisions|decisions]]\n- memory removed: old\n",
		"### Needs you\n\n- [[demo/tasks/T2|T2]] Review me · in review\n- 1 question open: Q2 (1 still)\n- [[demo/messages/@mini/acme/M-4|M-4]] to [[_peers/mini/acme|mini/acme]] held for the user, still\n",
		"### Links\n\n- [[demo/messages/other/M-3|M-3]] to [[other/PROJECT|other]]\n- [[demo/messages/@mini/acme/M-4|M-4]] held for [[_peers/mini/acme|mini/acme]]\n- 1 delivered\n",
		"### Guard\n\n- 3 denials: [[demo/threads/t-0001/t-0001|t-0001]] ×2, coordinator (tm-files, credentials)\n",
		"### Counts\n\n- 2 step changes · 1 run · 1 prompt · 1 fast-forward · 2 other\n",
		"## [[other/PROJECT|other]]\n\n### Started\n\n- 1 task added: [[other/tasks/T1|T1]]\n",
	} {
		if !strings.Contains(day, want) {
			t.Errorf("daily note has no %q:\n%s", want, day)
		}
	}
	for _, not := range []string{"SECRET", "passwd", "T2 Review me · in review, still"} {
		if strings.Contains(day, not) {
			t.Errorf("daily note has %q:\n%s", not, day)
		}
	}
	if next := read("2026-10-25"); !strings.Contains(next, "### Shipped\n\n- [[demo/tasks/T2|T2]] Review me\n") || strings.Contains(next, "other/PROJECT") {
		t.Errorf("the 25th:\n%s", next)
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "_tm", "daily", "2026-10-23.md")); err == nil {
		t.Error("a note for a day outside the days asked")
	}

	if w, err := RenderDaily(now, TickerDays); err != nil || w != 0 {
		t.Fatalf("second render: %d written, %v", w, err)
	}
	stamp := DailyStamp(now)
	mdfile.Append(demo.Path("JOURNAL.md"), []byte("2026-10-25T05:00:00Z coordinator task.add T9 x\n"))
	if DailyStamp(now) == stamp {
		t.Fatal("stamp unchanged by a journal line")
	}
	// Back-fill: the archived September day, linking the archived task,
	// the 23rd and the changed 25th; days without a line get no note.
	if w, err := RenderDaily(now, 60); err != nil || w != 3 {
		t.Fatalf("back-fill: %d written, %v", w, err)
	}
	if old := read("2026-09-10"); !strings.Contains(old, "- [[demo/tasks/archive/T3|T3]] Old\n") {
		t.Errorf("back-filled day:\n%s", old)
	}
	notes, _ := filepath.Glob(filepath.Join(dir, "projects", "_tm", "daily", "*.md"))
	if len(notes) != 4 {
		t.Errorf("daily notes %v, want 4", notes)
	}
}

// TestDailyShip: a merge and its task's done line are one entry,
// whichever comes first, and a merge without a task still shows.
func TestDailyShip(t *testing.T) {
	var d projectDay
	d.ship(shipped{thread: "t-0001", pr: 5})
	d.ship(shipped{task: "T1", thread: "t-0001"})
	d.ship(shipped{task: "T2"})
	d.ship(shipped{task: "T2", thread: "t-0002", pr: 6})
	d.ship(shipped{thread: "t-0003", pr: 7})
	want := []shipped{{"T1", "t-0001", 5}, {"T2", "t-0002", 6}, {"", "t-0003", 7}}
	if len(d.shipped) != len(want) {
		t.Fatalf("shipped %v", d.shipped)
	}
	for i := range want {
		if d.shipped[i] != want[i] {
			t.Errorf("shipped[%d] = %v, want %v", i, d.shipped[i], want[i])
		}
	}
}
