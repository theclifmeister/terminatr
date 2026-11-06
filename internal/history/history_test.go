package history

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

func newProject(t *testing.T) *project.Project {
	t.Helper()
	t.Setenv(home.Env, t.TempDir())
	p, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	// What project.New journals is today's, not the fixture's.
	if err := os.Remove(p.Path("JOURNAL.md")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return p
}

func writeTasks(t *testing.T, p *project.Project, board, archive []*tasks.Task) {
	t.Helper()
	s := p.Tasks()
	if err := os.WriteFile(s.TasksPath(), (&tasks.Board{Tasks: board}).Render(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.ArchivePath(), (&tasks.Board{Tasks: archive}).RenderArchive(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeArchive(t *testing.T, p *project.Project, month, text string) {
	t.Helper()
	if err := os.MkdirAll(p.Path("journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	z.Write([]byte(text))
	z.Close()
	if err := os.WriteFile(p.Path("journal", month+".md.gz"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func done(id int, at string) *tasks.Task {
	return &tasks.Task{ID: id, Title: fmt.Sprintf("Task %d", id), Status: tasks.Done, DoneAt: at, Created: "2026-10-01", Updated: "2026-10-30"}
}

func read(t *testing.T, p *project.Project) proto.History {
	t.Helper()
	h, err := ReadIn(p, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func days(h proto.History) string {
	var b strings.Builder
	for _, d := range h.Days {
		fmt.Fprintf(&b, "%s=%d ", d.Day, d.Done)
	}
	return strings.TrimSpace(b.String())
}

// TestThreeTasks: a project with three tasks has its numbers, and no
// typical time, which needs 10 timed tasks.
func TestThreeTasks(t *testing.T) {
	p := newProject(t)
	writeTasks(t, p, []*tasks.Task{
		done(1, "2026-10-05T09:00:00Z"),
		{ID: 2, Title: "Open", Status: tasks.Ready, Created: "2026-10-04", Updated: "2026-10-05"},
		{ID: 3, Title: "Dropped", Status: tasks.Dropped, Created: "2026-10-05", Updated: "2026-10-05"},
	}, nil)
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n2026-10-05T08:00:00Z coordinator task.status T1 started\n2026-10-05T09:00:00Z coordinator task.status T1 done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := read(t, p)
	if h.Done != 1 || h.Open != 1 || h.Dropped != 1 || h.Timed != 1 || h.Typical != "" || h.TypicalSeconds != 0 {
		t.Errorf("counts: %+v", h)
	}
	if h.FirstRecord != "2026-10-01" { // the oldest task's created, before any line
		t.Errorf("first record: %q", h.FirstRecord)
	}
	if days(h) != "2026-10-05=1" || h.BestDay == nil || h.BestDay.Day != "2026-10-05" {
		t.Errorf("days: %+v", h)
	}
}

// TestEmptyProject: no records, no numbers, and a list rather than null.
func TestEmptyProject(t *testing.T) {
	p := newProject(t)
	writeTasks(t, p, nil, nil)
	h := read(t, p)
	if h.Days == nil || len(h.Days) != 0 || h.BestDay != nil || h.FirstRecord != "" {
		t.Errorf("%+v", h)
	}
}

// TestDays: days are in the zone asked for, a bare day stays that day,
// an undated done task falls back as the upkeep's fill does, an archived
// task counts, and of equal days the first is the best.
func TestDays(t *testing.T) {
	p := newProject(t)
	undated := done(3, "")
	undated.Updated = "2026-10-07"
	writeTasks(t, p, []*tasks.Task{done(1, "2026-10-05T23:30:00Z"), done(2, "2026-10-06"), undated},
		[]*tasks.Task{done(4, "2026-10-08T10:00:00Z"), done(5, "2026-10-08T11:00:00Z"), done(6, "2026-10-06")})
	h := read(t, p)
	if got, want := days(h), "2026-10-05=1 2026-10-06=2 2026-10-07=1 2026-10-08=2"; got != want {
		t.Errorf("days %q, want %q", got, want)
	}
	if h.Done != 6 || h.BestDay.Day != "2026-10-06" || h.BestDay.Done != 2 {
		t.Errorf("%+v", h)
	}
	east, err := ReadIn(p, time.FixedZone("east", 3600))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := days(east), "2026-10-06=3 2026-10-07=1 2026-10-08=2"; got != want {
		t.Errorf("in another zone, days %q, want %q", got, want)
	}
}

// TestTaskInBothFiles: a task caught moving to the archive counts once.
func TestTaskInBothFiles(t *testing.T) {
	p := newProject(t)
	writeTasks(t, p, []*tasks.Task{done(1, "2026-10-05")}, []*tasks.Task{done(1, "2026-10-05")})
	if h := read(t, p); h.Done != 1 || days(h) != "2026-10-05=1" {
		t.Errorf("%+v", h)
	}
}

// timedTasks are n done tasks, each taking i+1 minutes, with their lines.
func timedTasks(n int) ([]*tasks.Task, string) {
	var ts []*tasks.Task
	var lines strings.Builder
	base := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	for i := range n {
		end := base.Add(time.Duration(i+1) * time.Minute)
		ts = append(ts, done(i+1, end.Format(time.RFC3339)))
		fmt.Fprintf(&lines, "%s coordinator task.status T%d started\n", base.Format(time.RFC3339), i+1)
		fmt.Fprintf(&lines, "%s ticker task.done T%d merged (PR #%d)\n", end.Format(time.RFC3339), i+1, i+1)
	}
	return ts, lines.String()
}

// TestTypical: the typical time is the median, and shows from 10 timed
// tasks; a done task with no start in the journal isn't timed.
func TestTypical(t *testing.T) {
	p := newProject(t)
	ts, lines := timedTasks(9)
	writeTasks(t, p, append(ts, done(50, "2026-10-05")), nil)
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	if h := read(t, p); h.Timed != 9 || h.Typical != "" || h.Done != 10 {
		t.Errorf("9 timed: %+v", h)
	}
	ts, lines = timedTasks(10)
	writeTasks(t, p, ts, nil)
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	if h := read(t, p); h.Timed != 10 || h.TypicalSeconds != 330 || h.Typical != "6 minutes" {
		t.Errorf("10 timed: %+v", h)
	}
}

// TestReopened: a task that left done and came back times its last run
// from its start, and a sent-back task its whole way.
func TestReopened(t *testing.T) {
	r := runsOf([]*records{{status: []project.StatusLine{
		{At: at(1), Task: 1, Status: "started"},
		{At: at(2), Task: 1, Status: "review"},
		{At: at(3), Task: 1, Status: "started"},
		{At: at(4), Task: 1, Status: "done"},
		{At: at(5), Task: 1, Status: "done"},
		{At: at(10), Task: 2, Status: "started"},
		{At: at(11), Task: 2, Status: "done"},
		{At: at(20), Task: 2, Status: "started"},
		{At: at(21), Task: 2, Status: "done"},
	}}})
	if !r[1].start.Equal(at(1)) || !r[1].done.Equal(at(4)) {
		t.Errorf("sent back: %+v", r[1])
	}
	if !r[2].start.Equal(at(20)) || !r[2].done.Equal(at(21)) {
		t.Errorf("reopened: %+v", r[2])
	}
}

func at(h int) time.Time { return time.Date(2026, 10, 5, h, 0, 0, 0, time.UTC) }

// TestWords: minutes, hours or days.
func TestWords(t *testing.T) {
	for s, want := range map[int64]string{0: "1 minute", 45: "1 minute", 2040: "34 minutes", 89 * 60: "89 minutes", 90 * 60: "2 hours",
		3 * 3600: "3 hours", 47 * 3600: "47 hours", 48 * 3600: "2 days", 86400 * 30: "30 days"} {
		if got := Words(s); got != want {
			t.Errorf("Words(%d) = %q, want %q", s, got, want)
		}
	}
}

// TestRotatedJournal: a journal rotated into month archives gives the
// same numbers as it did whole, archives are read once, new lines are
// followed from the offset, and a line still being written waits.
func TestRotatedJournal(t *testing.T) {
	p := newProject(t)
	ts, lines := timedTasks(12)
	writeTasks(t, p, ts, nil)
	all := strings.SplitAfter(lines, "\n")
	writeArchive(t, p, "2026-09", "2026-09-01T00:00:00Z human project.new web\n")
	writeArchive(t, p, "2026-10", strings.Join(all[:10], ""))
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n"+strings.Join(all[10:], "")), 0o644); err != nil {
		t.Fatal(err)
	}
	want := read(t, p)
	if want.FirstRecord != "2026-09-01" || want.Timed != 12 || want.Typical == "" {
		t.Fatalf("across archives: %+v", want)
	}

	// Read once: the archive's bytes can change under the same stamp.
	name := p.Path("journal", "2026-10.md.gz")
	fi, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, bytes.Repeat([]byte("x"), int(fi.Size())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got.Timed != 12 {
		t.Errorf("the archive was read again: %+v", got)
	}
	writeArchive(t, p, "2026-10", strings.Join(all[:10], ""))

	// Followed: an added line counts, half a line waits.
	ts = append(ts, done(13, "2026-10-06T10:00:00Z"))
	writeTasks(t, p, ts, nil)
	f, err := os.OpenFile(p.Path("JOURNAL.md"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString("2026-10-06T09:00:00Z coordinator task.status T13 started\n2026-10-06T10:00:00Z coordinator task.sta")
	if got := read(t, p); got.Timed != 13 {
		t.Errorf("followed: %+v", got)
	}
	f.WriteString("tus T13 done\n")
	x := indexes[p.Dir]
	before := x.offset
	got := read(t, p)
	if got.Done != 13 || got.Timed != 13 || x.offset <= before {
		t.Errorf("the rest of the line: %+v, offset %d to %d", got, before, x.offset)
	}

	// The ticker's rotation: everything moves to the archives, the journal
	// is another file, and the numbers stay. A fresh index says the same.
	moved, err := p.ArchiveJournal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), 24*time.Hour)
	if err != nil || moved == 0 {
		t.Fatalf("rotate: %d %v", moved, err)
	}
	rotated := read(t, p)
	if days(rotated) != days(got) || rotated.Timed != got.Timed || rotated.TypicalSeconds != got.TypicalSeconds || rotated.FirstRecord != got.FirstRecord {
		t.Errorf("rotated %+v, was %+v", rotated, got)
	}
	indexesMu.Lock()
	delete(indexes, p.Dir)
	indexesMu.Unlock()
	if fresh := read(t, p); days(fresh) != days(got) || fresh.Timed != got.Timed || fresh.TypicalSeconds != got.TypicalSeconds {
		t.Errorf("fresh %+v, was %+v", fresh, got)
	}
}

// TestDamagedArchive: an archive cut short keeps what it holds, doesn't
// fail the read, and is read again next time.
func TestDamagedArchive(t *testing.T) {
	p := newProject(t)
	ts, lines := timedTasks(1)
	writeTasks(t, p, ts, nil)
	writeArchive(t, p, "2026-10", lines)
	name := p.Path("journal", "2026-10.md.gz")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data[:len(data)-6], 0o644); err != nil {
		t.Fatal(err)
	}
	if h := read(t, p); h.Done != 1 {
		t.Errorf("%+v", h)
	}
	writeArchive(t, p, "2026-10", lines)
	if h := read(t, p); h.Timed != 1 {
		t.Errorf("not read again: %+v", h)
	}
	os.Remove(name)
	if h := read(t, p); h.Timed != 0 {
		t.Errorf("a removed archive's lines stayed: %+v", h)
	}
}

// BenchmarkRead is a read of 30,000 journal lines already followed.
func BenchmarkRead(b *testing.B) {
	b.Setenv(home.Env, b.TempDir())
	p, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		b.Fatal(err)
	}
	var lines strings.Builder
	for i := range 30000 {
		fmt.Fprintf(&lines, "2026-10-05T08:00:00Z t-0001 task.steps.add T%d 1 a step\n", i)
	}
	os.WriteFile(p.Path("JOURNAL.md"), []byte(lines.String()), 0o644)
	for b.Loop() {
		if _, err := Read(p); err != nil {
			b.Fatal(err)
		}
	}
}
