package search

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
)

// fixture writes a small project: every kind of source.
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"CONTEXT.md":                                    "# Context\n\nState of things.\n\n## Needs you\n\nPick a relay provider for the peers.\n",
		"PROJECT.md":                                    "---\nslug: demo\n---\n# Demo\n\n## Test VMs\n\nThe Windows VM is shared; take the lock.\n",
		"memory/decisions.md":                           "---\ntitle: \"Product decisions\"\ntouches:\n  - \"internal/agent/\"\n---\n## Decided\n- Own Go pane host; libghostty-vt via cgo.\n\n## Ruled out\n- SQLite for memory search.\n",
		"memory/secret.md":                              "---\ntitle: \"Coordinator practice\"\naudience: coordinator\n---\n## Decided\n- Batch the zebra reviews on Fridays.\n",
		"TASKS.md":                                      "---\nnext_id: 30\n---\n# Tasks\n\n## In motion\n\n### T12 Flaky TestColors on macOS\nstatus: started · thread: [[demo/threads/t-0007/t-0007|t-0007]] · created: 2026-10-30 · updated: 2026-10-31\n\nThe OSC 11 query times out under load.\n\n## Backlog\n\n### T13 Search the history\nstatus: open · created: 2026-10-31 · updated: 2026-10-31\n\nAn inverted index with BM25.\n",
		"tasks/ARCHIVE.md":                              "# Archive\n\n### T3 Old spike on tarballs\nstatus: done · created: 2026-10-01 · updated: 2026-10-02\n\nPacked threads into tarballs.\n",
		"designs/recall.md":                             "---\nname: \"recall\"\ntask: \"[[demo/tasks/T13|T13]]\"\ntm: \"owned\"\n---\n# Agent recall\n\n## Answers\n\n1. Q4: own project only\n",
		"designs/recall.canvas":                         "{}",
		"threads/t-0007/thread.toml":                    "id = \"t-0007\"\ntitle = \"Fix the flaky colors test\"\ntask = \"T12\"\n",
		"threads/t-0007/brief.md":                       "# Thread t-0007 of project demo\n\nYou work on the colors test.\n",
		"threads/t-0007/task.md":                        "# T12 Flaky TestColors\n\nThe query times out.\n\n## Follow-up 2026-10-31T10:00:00Z\n\nAlso check the marmalade path.\n\n## Follow-up 2026-10-31T12:00:00Z\n\nAnd the quince path.\n",
		"threads/t-0007/reports/1.md":                   "## Report\n\nFirst try: raised nothing.\n",
		"threads/t-0007/REPORT.md":                      "## Report\n\nFound the race in the OSC reader.\n\n## Next\nMerge it\n",
		"threads/t-0007/STATUS.md":                      "status file, not indexed: pomegranate\n",
		"threads/t-0007/t-0007.md":                      "derived note, not indexed: pomegranate\n",
		"threads/t-0007/library/notes.md":               "# Notes\n\nThe gooseberry measurements.\n",
		"threads/t-0007/library/page.html":              "<html><style>.x{color:red}</style><body><h1>Design</h1><p>Elderflower &amp; lime</p><script>var kiwi=1</script></body></html>",
		"threads/t-0007/library/shot.png":               "\x89PNG pomegranate",
		"inbox/20261031T100000Z-report-t-0007.md":       "Report from t-0007 about the tangerine.\n",
		"inbox/done/20261030T090000Z-blocked-t-0007.md": "Blocked on the apricot.\n",
		"messages/peer/1.md":                            "link message: pomegranate\n",
		"JOURNAL.md":                                    "# Journal\n\n2026-10-30T09:00:00Z coordinator task.add T12 Flaky TestColors on macOS\n2026-10-30T09:00:00Z coordinator thread.start t-0007 T12 Fix the flaky colors test\n2026-10-31T11:00:00Z t-0007 thread.report t-0007 found the race\n",
	}
	for name, text := range files {
		write(t, dir, name, text)
	}
	return dir
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// touch sets a file's time ahead, so the index sees it changed even
// within the file system's time resolution.
func touch(t *testing.T, dir, name string, d time.Duration) {
	t.Helper()
	at := time.Now().Add(d)
	if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(name)), at, at); err != nil {
		t.Fatal(err)
	}
}

func ids(x *Index) []string {
	var out []string
	for id, c := range x.byID {
		if !c.dead {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func search(t *testing.T, x *Index, q Query) []string {
	t.Helper()
	r, err := x.Search(q)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, h := range r.Hits {
		out = append(out, h.ID)
	}
	return out
}

func TestChunkIDs(t *testing.T) {
	dir := fixture(t)
	x := Open(dir, t.TempDir())
	st, err := x.Update()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Errors) > 0 {
		t.Fatalf("errors: %v", st.Errors)
	}
	want := []string{
		"T12", "T13", "T3",
		"context", "context#needs-you",
		"design:recall#answers",
		"inbox:20261030T090000Z-blocked-t-0007", "inbox:20261031T100000Z-report-t-0007",
		"j:20261030T090000Z", "j:20261030T090000Z-2", "j:20261031T110000Z",
		"lib:t-0007/notes.md", "lib:t-0007/page.html",
		"note:decisions#decided", "note:decisions#ruled-out", "note:secret#decided",
		"project#test-vms",
		"t-0007/brief",
		"t-0007/followup.1", "t-0007/followup.2",
		"t-0007/report.1#report", "t-0007/report.2#next", "t-0007/report.2#report",
		"t-0007/task",
	}
	if got := ids(x); !slices.Equal(got, want) {
		t.Errorf("ids:\n got %q\nwant %q", got, want)
	}
	c := x.byID["T12"]
	if c.Kind != KindTask || c.Thread != "t-0007" || c.Time.Format("2006-01-02") != "2026-10-31" || c.Title != "T12 Flaky TestColors on macOS" {
		t.Errorf("T12 = %+v", c.Chunk)
	}
	if _, text, _ := x.Text("T12", false); strings.Contains(text, "Backlog") || !strings.Contains(text, "OSC 11") {
		t.Errorf("T12's text runs into the next group: %q", text)
	}
	if c := x.byID["t-0007/followup.2"]; c.Task != "T12" || c.Time != time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) {
		t.Errorf("followup.2 = %+v", c.Chunk)
	}
	if c := x.byID["j:20261030T090000Z-2"]; c.Task != "T12" || c.Thread != "t-0007" || c.Kind != KindJournal {
		t.Errorf("journal line = %+v", c.Chunk)
	}
	if c := x.byID["design:recall#answers"]; c.Task != "T13" {
		t.Errorf("design = %+v", c.Chunk)
	}
	if !x.byID["note:secret#decided"].Coordinator || x.byID["note:decisions#decided"].Coordinator {
		t.Error("a note's audience isn't kept")
	}
	if _, text, err := x.Text("lib:t-0007/page.html", false); err != nil || strings.Contains(text, "kiwi") || strings.Contains(text, "color:red") || !strings.Contains(text, "Elderflower & lime") {
		t.Errorf("html text = %q, %v", text, err)
	}
}

func TestSearchRanksAndScopes(t *testing.T) {
	dir := fixture(t)
	x := Open(dir, t.TempDir())
	if got := search(t, x, Query{Words: "flaky colors", Coordinator: true, Limit: 4}); !slices.Contains(got, "T12") || !slices.Contains(got, "t-0007/task") {
		t.Errorf("flaky colors: %q, want T12 and t-0007/task at the top", got)
	}
	// What is never indexed.
	if got := search(t, x, Query{Words: "pomegranate", Coordinator: true}); len(got) > 0 {
		t.Errorf("pomegranate found in %q", got)
	}
	// The inbox and coordinator notes are the coordinator's only.
	for _, w := range []string{"tangerine", "apricot", "zebra"} {
		if got := search(t, x, Query{Words: w, Coordinator: true}); len(got) != 1 {
			t.Errorf("coordinator %s: %q", w, got)
		}
		if got := search(t, x, Query{Words: w}); len(got) != 0 {
			t.Errorf("thread %s: %q", w, got)
		}
	}
	if _, _, err := x.Text("inbox:20261031T100000Z-report-t-0007", false); !errors.Is(err, ErrNoChunk) {
		t.Errorf("a thread reads the inbox: %v", err)
	}
	// Filters.
	if got := search(t, x, Query{Words: "macos", Kinds: []string{KindJournal}}); !slices.Equal(got, []string{"j:20261030T090000Z"}) {
		t.Errorf("--in journal: %q", got)
	}
	if got := search(t, x, Query{Words: "path", Thread: "t-0007", Since: time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC)}); !slices.Equal(got, []string{"t-0007/followup.2"}) {
		t.Errorf("--thread --since: %q", got)
	}
	if got := search(t, x, Query{Words: "the and of"}); len(got) != 0 {
		t.Errorf("stop words matched %q", got)
	}
}

func TestIncremental(t *testing.T) {
	dir := fixture(t)
	idx := t.TempDir()
	x := Open(dir, idx)
	st, err := x.Update()
	if err != nil {
		t.Fatal(err)
	}
	files := st.Files
	if st, _ = x.Update(); st.Read != 0 || st.Removed != 0 {
		t.Errorf("unchanged: read %d, removed %d", st.Read, st.Removed)
	}
	// A changed file is read again, alone.
	write(t, dir, "memory/decisions.md", "---\ntitle: \"Product decisions\"\n---\n## Decided\n- Damson plums only.\n")
	touch(t, dir, "memory/decisions.md", time.Second)
	if st, _ = x.Update(); st.Read != 1 {
		t.Errorf("changed: read %d", st.Read)
	}
	if got := search(t, x, Query{Words: "damson"}); !slices.Equal(got, []string{"note:decisions#decided"}) {
		t.Errorf("damson: %q", got)
	}
	if got := search(t, x, Query{Words: "sqlite"}); len(got) != 0 {
		t.Errorf("the old text is still found: %q", got)
	}
	// A new report moves REPORT.md to reports/2.md.
	if err := os.Rename(filepath.Join(dir, "threads/t-0007/REPORT.md"), filepath.Join(dir, "threads/t-0007/reports/2.md")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "threads/t-0007/REPORT.md", "## Report\n\nMerged; the blackcurrant fix.\n")
	if got := search(t, x, Query{Words: "blackcurrant"}); !slices.Equal(got, []string{"t-0007/report.3#report"}) {
		t.Errorf("new report: %q", got)
	}
	if got := search(t, x, Query{Words: "race OSC reader"}); len(got) == 0 || got[0] != "t-0007/report.2#report" {
		t.Errorf("moved report: %q", got)
	}
	// A file gone is dropped.
	if err := os.Remove(filepath.Join(dir, "threads/t-0007/library/notes.md")); err != nil {
		t.Fatal(err)
	}
	if st, _ = x.Update(); st.Removed != 1 || st.Files != files+1-1 {
		t.Errorf("removed %d, files %d (was %d)", st.Removed, st.Files, files)
	}
	if got := search(t, x, Query{Words: "gooseberry"}); len(got) != 0 {
		t.Errorf("gone file found: %q", got)
	}
	// Saved and loaded again: nothing to read.
	if err := x.Close(); err != nil {
		t.Fatal(err)
	}
	x = Open(dir, idx)
	if st, _ = x.Update(); st.Read != 0 || st.Removed != 0 {
		t.Errorf("after reload: read %d, removed %d", st.Read, st.Removed)
	}
	if got := search(t, x, Query{Words: "blackcurrant"}); !slices.Equal(got, []string{"t-0007/report.3#report"}) {
		t.Errorf("after reload: %q", got)
	}
}

func TestJournalReadOn(t *testing.T) {
	dir := fixture(t)
	x := Open(dir, t.TempDir())
	if _, err := x.Update(); err != nil {
		t.Fatal(err)
	}
	seg := x.segs["journal"]
	before := len(seg.chunks)
	f, err := os.OpenFile(filepath.Join(dir, "JOURNAL.md"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A whole line in the same second as the last, and half a line.
	if _, err := f.WriteString("2026-10-31T11:00:00Z t-0007 task.steps.check T12 1 nectarine\n2026-10-31T11:00:05Z t-0007 half"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	touch(t, dir, "JOURNAL.md", time.Second)
	if _, err := x.Update(); err != nil {
		t.Fatal(err)
	}
	if got := len(seg.chunks); got != before+1 {
		t.Errorf("chunks %d, want %d: the old lines were read again or the half line taken", got, before+1)
	}
	if got := search(t, x, Query{Words: "nectarine"}); !slices.Equal(got, []string{"j:20261031T110000Z-2"}) {
		t.Errorf("appended line: %q", got)
	}
	f, _ = os.OpenFile(filepath.Join(dir, "JOURNAL.md"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(" line done\n")
	f.Close()
	touch(t, dir, "JOURNAL.md", 2*time.Second)
	if got := search(t, x, Query{Words: "half"}); !slices.Equal(got, []string{"j:20261031T110005Z"}) {
		t.Errorf("finished line: %q", got)
	}
	if seg.dead != 0 {
		t.Errorf("reading on dropped %d chunks", seg.dead)
	}

	// Retention moves the old lines to journal/2026-10.md.gz: the file
	// is read again whole, the archive once, the ids kept.
	p := &project.Project{Slug: "demo", Dir: dir}
	n, err := p.ArchiveJournal(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), 0)
	if err != nil || n != 2 {
		t.Fatalf("ArchiveJournal = %d, %v", n, err)
	}
	st, err := x.Update()
	if err != nil {
		t.Fatal(err)
	}
	if st.Read != 2 {
		t.Errorf("read %d after retention, want JOURNAL.md and the archive", st.Read)
	}
	if got := search(t, x, Query{Words: "macos", Kinds: []string{KindJournal}}); !slices.Equal(got, []string{"j:20261030T090000Z"}) {
		t.Errorf("archived line: %q", got)
	}
	if c, text, err := x.Text("j:20261030T090000Z-2", false); err != nil || c.Path != "journal/2026-10.md.gz" || !strings.HasSuffix(text, "Fix the flaky colors test") {
		t.Errorf("archived text = %+v %q %v", c, text, err)
	}
	if st, _ := x.Update(); st.Read != 0 {
		t.Errorf("archive read again: %d", st.Read)
	}
}

func TestArchives(t *testing.T) {
	dir := fixture(t)
	x := Open(dir, t.TempDir())
	if _, err := x.Update(); err != nil {
		t.Fatal(err)
	}
	// The thread is archived: its folder becomes a tarball, its ids stay.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	root := filepath.Join(dir, "threads", "t-0007")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(filepath.Join(dir, "threads"), p)
		tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: time.Now()})
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	tw.Close()
	zw.Close()
	write(t, dir, "threads/archive/t-0007.tar.gz", buf.String())
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	st, err := x.Update()
	if err != nil || len(st.Errors) > 0 {
		t.Fatal(err, st.Errors)
	}
	if got := search(t, x, Query{Words: "quince"}); !slices.Equal(got, []string{"t-0007/followup.2"}) {
		t.Errorf("archived follow-up: %q", got)
	}
	if got := search(t, x, Query{Words: "gooseberry"}); !slices.Equal(got, []string{"lib:t-0007/notes.md"}) {
		t.Errorf("archived library: %q", got)
	}
	if got := search(t, x, Query{Words: "race OSC reader"}); len(got) == 0 || got[0] != "t-0007/report.2#report" {
		t.Errorf("archived report: %q", got)
	}
	c, text, err := x.Text("t-0007/followup.1", false)
	if err != nil || c.Member != "t-0007/task.md" || !strings.Contains(text, "marmalade") || c.Task != "T12" {
		t.Errorf("archived text = %+v %q %v", c, text, err)
	}
	if st, _ := x.Update(); st.Read != 0 {
		t.Errorf("archive read again: %d", st.Read)
	}

	// The handled inbox item goes to inbox/done/2026-10.tar.gz.
	old := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(dir, "inbox/done/20261030T090000Z-blocked-t-0007.md"), old, old)
	p := &project.Project{Slug: "demo", Dir: dir}
	if n, err := p.ArchiveInbox(time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), time.Hour); err != nil || n != 1 {
		t.Fatalf("ArchiveInbox = %d, %v", n, err)
	}
	if got := search(t, x, Query{Words: "apricot", Coordinator: true}); !slices.Equal(got, []string{"inbox:20261030T090000Z-blocked-t-0007"}) {
		t.Errorf("archived inbox item: %q", got)
	}
	if got := search(t, x, Query{Words: "apricot"}); len(got) != 0 {
		t.Errorf("thread sees the archived inbox: %q", got)
	}
}

func TestReindexAndFormat(t *testing.T) {
	dir := fixture(t)
	idx := t.TempDir()
	x := Open(dir, idx)
	st, err := x.Reindex()
	if err != nil {
		t.Fatal(err)
	}
	if st.Read != st.Files || st.Files == 0 {
		t.Errorf("reindex read %d of %d", st.Read, st.Files)
	}
	if st, _ = x.Reindex(); st.Read != st.Files {
		t.Errorf("second reindex read %d of %d", st.Read, st.Files)
	}
	if len(ids(x)) != st.Chunks {
		t.Errorf("%d ids, %d chunks: a reindex kept old chunks", len(ids(x)), st.Chunks)
	}
	// A segment of another format, or that doesn't decode, is rebuilt.
	x.Close()
	if err := writeGob(filepath.Join(idx, "tasks.gob.gz"), segDisk{Format: Format + 1, Name: "tasks"}); err != nil {
		t.Fatal(err)
	}
	write(t, idx, "designs.gob.gz", "not gzip")
	x = Open(dir, idx)
	st, err = x.Update()
	if err != nil {
		t.Fatal(err)
	}
	if st.Read != 3 { // TASKS.md, ARCHIVE.md, designs/recall.md
		t.Errorf("read %d after a bad segment, want 3", st.Read)
	}
	if got := search(t, x, Query{Words: "tarballs"}); !slices.Equal(got, []string{"T3"}) {
		t.Errorf("rebuilt segment: %q", got)
	}
}

func TestChunkSplitsLongSections(t *testing.T) {
	long := "## Big\n\n" + strings.Repeat("A paragraph of words that goes on.\n\n", 200)
	ps := markdown(long, 0, 3, "note:x", "X", Chunk{})
	if len(ps) < 3 {
		t.Fatalf("%d pieces", len(ps))
	}
	end := 0
	for i, p := range ps {
		if p.Len > MaxChunk || p.Off != end {
			t.Errorf("piece %d: off %d len %d (end %d)", i, p.Off, p.Len, end)
		}
		end = p.Off + p.Len
	}
	if end != len(long) || ps[0].ID != "note:x#big" || ps[1].ID != "note:x#big-2" {
		t.Errorf("end %d of %d, ids %s %s", end, len(long), ps[0].ID, ps[1].ID)
	}
	// A heading inside a code fence doesn't split.
	fenced := "# Title\n\n```\n# not a heading\n```\n"
	if ps := markdown(fenced, 0, 3, "b", "", Chunk{}); len(ps) != 1 {
		t.Errorf("fenced: %d pieces", len(ps))
	}
}
