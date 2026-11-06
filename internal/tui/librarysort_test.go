package tui

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestLibrarySortFixture: the rule both the TUI and the Mac read, equal
// sizes in the list's order whatever the filter and kind leave.
func TestLibrarySortFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/library-filter.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx libFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	if len(fx.Biggest) < 7 || len(fx.Sort) < 4 {
		t.Fatalf("fixture: %d and %d cases", len(fx.Biggest), len(fx.Sort))
	}
	for _, c := range fx.Biggest {
		var names []string
		for _, f := range libBiggestFirst(libShown(fx.Libraries[c.Library], c.Query, c.Kind)) {
			names = append(names, f.Name)
		}
		if !slices.Equal(names, c.Names) {
			t.Errorf("%s %q %q: %v, want %v", c.Library, c.Query, c.Kind, names, c.Names)
		}
	}
	for _, c := range fx.Sort {
		if got := libShowsSort(make([]thread.LibFile, c.Files)); got != c.Shows {
			t.Errorf("%d files: %v", c.Files, got)
		}
	}
}

// sizedFiles is a library across every setup: a task's thread, a repo's, one
// with no task, a packed one, and a linked project's two files of one name.
func sizedFiles() []thread.LibFile {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	return []thread.LibFile{
		{Thread: "t-0001", Task: "T1", TaskStatus: "done", Title: "New work", State: "running", Name: "small.md", Size: 10, Time: day(9)},
		{Thread: "t-0001", Task: "T1", TaskStatus: "done", Title: "New work", State: "running", Name: "same-a.png", Size: 500, Time: day(8)},
		{Thread: "t-0002", Task: "T2", Title: "Older work", Repo: "api", State: "resolved", Name: "same-b.png", Size: 500, Time: day(7)},
		{Thread: "t-0003", Title: "No task here", State: "stopped", Name: "nothing.txt", Size: 0, Time: day(6)},
		{Thread: "t-0004", Task: "T4", TaskStatus: "done", Title: "Packed away", State: "archived", Name: "explainer.mp4", Size: 175_100_000, Time: day(5)},
		{Thread: "t-0004", Task: "T4", TaskStatus: "done", Title: "Packed away", State: "archived", Name: "same-c.png", Size: 500, Time: day(4)},
		{Title: "from studio/acme", Peer: "studio/acme", Message: "M-1", State: "received", Name: "brief.zip", Size: 78_700_000, Time: day(3)},
		{Title: "from studio/acme", Peer: "studio/acme", Message: "M-2", State: "received", Name: "brief.zip", Size: 12, Time: day(2)},
	}
}

// TestLibraryBiggestFirst: s lists every setup flat with its title and no
// headings or folds; packed threads' and linked projects' files among them.
func TestLibraryBiggestFirst(t *testing.T) {
	pv, m := libraryTab(t, sizedFiles())
	out := screen(m)
	if !strings.Contains(out, "253.8 MB · / filter · s biggest first") {
		t.Errorf("newest's head:\n%s", out)
	}
	keyPress(m, "s")
	if !pv.libFlat() {
		t.Fatal("s did not sort")
	}
	out = screen(m)
	if !strings.Contains(out, "biggest first · 253.8 MB · / filter · s newest first") {
		t.Errorf("biggest's head:\n%s", out)
	}
	var rows []string
	for _, r := range pv.libRows() {
		if !r.isFile() {
			t.Errorf("a fold's row in the flat list: %+v", r)
		}
		rows = append(rows, r.f.Name+"/"+libFileID(r.f))
	}
	want := []string{"explainer.mp4/t-0004", "brief.zip/M-1", "same-a.png/t-0001", "same-b.png/t-0002", "same-c.png/t-0004", "brief.zip/M-2", "small.md/t-0001", "nothing.txt/t-0003"}
	if !slices.Equal(rows, want) {
		t.Errorf("order %v, want %v", rows, want)
	}
	for _, s := range []string{"explainer.mp4", "T4  Packed away", "175.1 MB", "from studio/acme", "T2  Older work", "No task here", "0 B"} {
		if !strings.Contains(out, s) {
			t.Errorf("no %q:\n%s", s, out)
		}
	}
	for _, no := range []string{"t-0001 ·", "· archived", "show fewer"} {
		if strings.Contains(out, no) {
			t.Errorf("a heading's %q in the flat list:\n%s", no, out)
		}
	}
	keyPress(m, "s")
	if pv.libFlat() || !strings.Contains(screen(m), "t-0001 ·") {
		t.Errorf("s did not turn back:\n%s", screen(m))
	}
}

// TestLibrarySortKeepsSelection: the selection stays on its file both ways,
// even with a same-named file of another message or under a fold.
func TestLibrarySortKeepsSelection(t *testing.T) {
	files := sizedFiles()
	for i := range 8 {
		files = append(files, thread.LibFile{Thread: "t-0005", Task: "T5", Title: "Long", Name: string(rune('a'+i)) + ".png", Size: int64(100 - i), Time: time.Date(2026, 10, 1, 0, i, 0, 0, time.UTC)})
	}
	files = append(files, thread.LibFile{Thread: "t-0005", Task: "T5", Title: "Long", Name: "huge.png", Size: 900_000_000, Time: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
	pv, m := libraryTab(t, files)
	keyPress(m, "s")
	first, ok := pv.selLib()
	if !ok {
		t.Fatal("no selection")
	}
	// The selection stayed on the newest file; go to the biggest.
	if first.Name != "small.md" {
		t.Errorf("s moved the selection to %s", first.Name)
	}
	pv.sel[tabLibrary] = 0
	if f, _ := pv.selLib(); f.Name != "huge.png" {
		t.Fatalf("first row %s", f.Name)
	}
	// huge.png is under its group's fold in the groups: it opens.
	keyPress(m, "s")
	if f, ok := pv.selLib(); !ok || f.Name != "huge.png" || !pv.libOpen["t-0005"] {
		t.Errorf("after s: selection %+v, open %v", f, pv.libOpen)
	}
	keyPress(m, "s")
	for i, r := range pv.libRows() {
		if libFileID(r.f) == "M-2" {
			pv.sel[tabLibrary] = i
		}
	}
	keyPress(m, "s")
	if f, ok := pv.selLib(); !ok || libFileID(f) != "M-2" {
		t.Errorf("newest: a same-named file took the selection: %+v", f)
	}
	keyPress(m, "s")
	if f, ok := pv.selLib(); !ok || libFileID(f) != "M-2" {
		t.Errorf("biggest: a same-named file took the selection: %+v", f)
	}
}

// TestLibrarySortNarrows: filter and kind keep working over the flat list,
// esc clears them and leaves the sort, typing s is a letter, and the delete
// keys still act on the selected file.
func TestLibrarySortNarrows(t *testing.T) {
	pv, m := libraryTab(t, sizedFiles())
	keyPress(m, "s")
	keyPress(m, "/")
	keyPress(m, "s")
	if pv.libFilter != "s" || !pv.libFlat() {
		t.Fatalf("typing s: filter %q, flat %v", pv.libFilter, pv.libFlat())
	}
	keyPress(m, "enter")
	keyPress(m, "t")
	if pv.libPick == "" || !pv.libFlat() {
		t.Errorf("t: pick %q, flat %v", pv.libPick, pv.libFlat())
	}
	keyPress(m, "esc")
	if pv.libFilter != "" || pv.libPick != "" || !pv.libFlat() {
		t.Errorf("esc: %q %q %v", pv.libFilter, pv.libPick, pv.libFlat())
	}
	if _, ok := m.top().(*projectView); !ok {
		t.Fatalf("esc with a filter kept left %T", m.top())
	}
	pv.sel[tabLibrary] = 0
	keyPress(m, "X")
	if out := screen(m); !strings.Contains(out, "explainer.mp4") || !strings.Contains(out, "of thread t-0004") {
		t.Errorf("X asks about the wrong file:\n%s", out)
	}
	keyPress(m, "esc")
	pv.narrowLib("nothing like it", "")
	if out := screen(m); !strings.Contains(out, "nothing matches: esc clears") {
		t.Errorf("nothing left:\n%s", out)
	}
}

// TestLibraryQuietSort: one file or none has no sort, and two have it with
// no filter yet; a library down to one file lists as it always did.
func TestLibraryQuietSort(t *testing.T) {
	one := []thread.LibFile{{Thread: "t-0001", Task: "T1", Title: "Only", Name: "one.png", Size: 1}}
	for name, files := range map[string][]thread.LibFile{"none": nil, "one": one} {
		pv, m := libraryTab(t, files)
		keyPress(m, "s")
		if pv.libBiggest || strings.Contains(screen(m), "biggest first") {
			t.Errorf("%s: sorts:\n%s", name, screen(m))
		}
	}
	two := append(slices.Clone(one), thread.LibFile{Thread: "t-0001", Task: "T1", Title: "Only", Name: "two.png", Size: 2})
	pv, m := libraryTab(t, two)
	out := screen(m)
	if !strings.Contains(out, "s biggest first") || strings.Contains(out, "/ filter") {
		t.Errorf("two files:\n%s", out)
	}
	keyPress(m, "s")
	pv.library = one
	if pv.libFlat() || !strings.Contains(screen(m), "t-0001 ·") {
		t.Errorf("one file left still lists flat:\n%s", screen(m))
	}
}

// TestLibrarySortClick: a click on the sort's words turns it, on the rest
// of the line it filters, and with no filter yet it does nothing.
func TestLibrarySortClick(t *testing.T) {
	pv, m := libraryTab(t, sizedFiles())
	w := m.inner(viewWidth)
	line, at := pv.libFilterLine(w)
	if at < 0 {
		t.Fatalf("no sort key at %d cells", w)
	}
	if got := ansi.Strip(line); !strings.HasPrefix(string([]rune(got)[at:]), "s biggest first") {
		t.Errorf("column %d of %q", at, got)
	}
	pv.click(m, libFilterHit, 0, false)
	if !pv.libTyping {
		t.Error("a click on the filter's words did not filter")
	}
	pv.click(m, libFilterHit, at+2, false)
	if pv.libFlat() {
		t.Error("typing, a click on the line turned the sort")
	}
	pv.libTyping = false
	pv.click(m, libFilterHit, at+2, false)
	if !pv.libFlat() || pv.libTyping {
		t.Errorf("a click on the sort's words: flat %v, typing %v", pv.libFlat(), pv.libTyping)
	}
	pv, m = libraryTab(t, sizedFiles()[:2])
	pv.click(m, libFilterHit, 0, false)
	if pv.libTyping {
		t.Error("under 7 files a click on the total opened a filter")
	}
}

// TestLibraryFlatRowFits: at any width a flat row holds its size and fits;
// the title goes first when there's no room.
func TestLibraryFlatRowFits(t *testing.T) {
	f := thread.LibFile{Thread: "t-0001", Task: "T1", Title: strings.Repeat("title ", 40), Name: strings.Repeat("n", 90) + ".png", Size: 1_440_000_000}
	rows := []libRow{{f: f}}
	for _, w := range []int{20, 30, 40, 60, 80, 120, 200} {
		l := libFlatRow(f, libFlatName(rows, w), w, true)
		if n := ansi.StringWidth(l); n > w {
			t.Errorf("%d cells: %d: %q", w, n, l)
		}
		if !strings.Contains(l, "1.44 GB") {
			t.Errorf("%d cells: no size: %q", w, l)
		}
		if w >= 60 && !strings.Contains(l, "T1  title") {
			t.Errorf("%d cells: no title: %q", w, l)
		}
	}
}
