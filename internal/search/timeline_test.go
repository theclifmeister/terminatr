package search

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func chunkIDs(cs []Chunk) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestTimeline(t *testing.T) {
	dir := fixture(t)
	x := Open(dir, t.TempDir())
	tl, err := x.Timeline("t-0007/followup.1", 8, 2, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// Its task's and thread's chunks, by time: the journal's task.add
	// and thread.start, the task (updated that day), then the report
	// line and the second follow-up.
	if got, want := chunkIDs(tl.Before), []string{"j:20261030T090000Z", "j:20261030T090000Z-2", "T12"}; !slices.Equal(got, want) {
		t.Errorf("before: %q, want %q", got, want)
	}
	if got, want := chunkIDs(tl.After), []string{"j:20261031T110000Z", "t-0007/followup.2"}; !slices.Equal(got, want) {
		t.Errorf("after: %q, want %q", got, want)
	}
	// The coordinator sees the inbox's items of the thread too.
	tl, err = x.Timeline("t-0007/followup.1", 8, 0, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(tl.Before); !slices.Contains(got, "inbox:20261030T090000Z-blocked-t-0007") {
		t.Errorf("coordinator's before: %q", got)
	}
	// A note has no task or thread: the project's journal around it.
	tl, err = x.Timeline("note:decisions#decided", 2, 2, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := chunkIDs(tl.Before), []string{"j:20261030T090000Z-2", "j:20261031T110000Z"}; !slices.Equal(got, want) || len(tl.After) != 0 {
		t.Errorf("note: before %q (want %q), after %q", got, want, chunkIDs(tl.After))
	}
	// A thread is told no such chunk for the coordinator's.
	if _, err := x.Timeline("inbox:20261031T100000Z-report-t-0007", 2, 2, false, false); !errors.Is(err, ErrNoChunk) {
		t.Errorf("a thread's timeline of the inbox: %v", err)
	}
	if _, err := x.Timeline("T999", 2, 2, false, true); !errors.Is(err, ErrNoChunk) {
		t.Errorf("unknown id: %v", err)
	}
}

func TestChildren(t *testing.T) {
	dir := fixture(t)
	x := Open(dir, t.TempDir())
	got, err := x.Children("t-0007", false)
	if err != nil {
		t.Fatal(err)
	}
	ids := chunkIDs(got)
	for _, want := range []string{"t-0007/brief", "t-0007/followup.1", "t-0007/report.1#report", "t-0007/report.2#next", "lib:t-0007/notes.md"} {
		if !slices.Contains(ids, want) {
			t.Errorf("t-0007's chunks %q lack %s", ids, want)
		}
	}
	for _, id := range ids {
		if strings.HasPrefix(id, "j:") || strings.HasPrefix(id, "inbox:") {
			t.Errorf("t-0007's chunks for a thread hold %s", id)
		}
	}
	got, err = x.Children("T12", false)
	if err != nil {
		t.Fatal(err)
	}
	if ids := chunkIDs(got); !slices.Contains(ids, "T12") || !slices.Contains(ids, "t-0007/task") {
		t.Errorf("T12's chunks: %q", ids)
	}
	got, err = x.Children("t-0007", true)
	if err != nil {
		t.Fatal(err)
	}
	if ids := chunkIDs(got); !slices.Contains(ids, "inbox:20261031T100000Z-report-t-0007") {
		t.Errorf("t-0007's chunks for the coordinator: %q", ids)
	}
}

func TestGroupFoldsJournalLines(t *testing.T) {
	h := func(id, kind, task, thread string) Hit {
		return Hit{Chunk: Chunk{ID: id, Kind: kind, Task: task, Thread: thread}}
	}
	hits := []Hit{
		h("j:1", KindJournal, "T1", ""), // best of T1: stays
		h("T1", KindTask, "T1", "t-0001"),
		h("j:2", KindJournal, "T1", ""),               // folds into j:1
		h("j:3", KindJournal, "", "t-0001"),           // folds into T1, by its thread
		h("j:4", KindJournal, "", ""),                 // of neither: stays
		h("t-0002/brief", KindThread, "T2", "t-0002"), // not a journal line: stays
		h("j:5", KindJournal, "T2", "t-0002"),         // folds into t-0002/brief
	}
	out := group(hits)
	var got []string
	for _, o := range out {
		got = append(got, o.ID)
	}
	if want := []string{"j:1", "T1", "j:4", "t-0002/brief"}; !slices.Equal(got, want) {
		t.Fatalf("grouped: %q, want %q", got, want)
	}
	if out[0].More != 1 || out[1].More != 1 || out[2].More != 0 || out[3].More != 1 {
		t.Errorf("more: %d %d %d %d", out[0].More, out[1].More, out[2].More, out[3].More)
	}
}

func TestFreshSkipsTheWalk(t *testing.T) {
	dir := fixture(t)
	x := Open(dir, t.TempDir())
	now := time.Now()
	x.now = func() time.Time { return now }
	x.SetFresh(time.Second)
	if got := search(t, x, Query{Words: "damson"}); len(got) != 0 {
		t.Fatalf("damson before: %q", got)
	}
	write(t, dir, "memory/plums.md", "## Decided\n- Damson plums only.\n")
	if got := search(t, x, Query{Words: "damson"}); len(got) != 0 {
		t.Errorf("a query within a second of the walk walked again: %q", got)
	}
	now = now.Add(time.Second)
	if got := search(t, x, Query{Words: "damson"}); !slices.Equal(got, []string{"note:plums#decided"}) {
		t.Errorf("a second later: %q", got)
	}
	// Update always walks.
	write(t, dir, "memory/figs.md", "## Decided\n- Figs too.\n")
	if st, err := x.Update(); err != nil || st.Read != 1 {
		t.Errorf("update: read %d, %v", st.Read, err)
	}
}

func TestTextOfALargeTasksFile(t *testing.T) {
	dir := fixture(t)
	var b strings.Builder
	b.WriteString("# Tasks\n\n## Backlog\n\n### T40 Rhubarb crumble\nstatus: open · created: 2026-10-31 · updated: 2026-10-31\n\nBake it.\n\n")
	for n := 41; b.Len() <= MaxFile; n++ {
		fmt.Fprintf(&b, "### T%d Filler\nstatus: open\n\n%s\n\n", n, strings.Repeat("x", 100))
	}
	big := b.String()
	if len(big) <= MaxFile {
		t.Fatalf("TASKS.md of %d bytes isn't over MaxFile", len(big))
	}
	write(t, dir, "TASKS.md", big)
	x := Open(dir, t.TempDir())
	_, text, err := x.Text("T40", false)
	if err != nil || !strings.Contains(text, "Rhubarb crumble") {
		t.Fatalf("T40 of a TASKS.md over MaxFile: %q, %v", text, err)
	}
}
