package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/thread"
)

const libPublishedURL = "https://claude.ai/artifact/abc"

func titledFiles() []thread.LibFile {
	return []thread.LibFile{
		{Thread: "t-0001", Task: "T1", Title: "A design", Name: "design.html", DocTitle: "Who May Run tm", Published: libPublishedURL, Size: 57000},
		{Thread: "t-0001", Task: "T1", Title: "A design", Name: "REPORT.md", DocTitle: "Report", Size: 300},
		{Thread: "t-0002", Task: "T2", Title: "Plain", Name: "plain.md", Size: 10},
		{Thread: "t-0003", Title: "No task", Repo: "api", Name: "page.html", Size: 10},
	}
}

// TestLibFileLabel: a title goes with the file's name, which never leaves
// the row while it fits a word; every width stays inside its cells.
func TestLibFileLabel(t *testing.T) {
	f := thread.LibFile{Name: "design.html", DocTitle: "Who May Run tm", Published: libPublishedURL}
	if got := ansi.Strip(libFileLabel(f, 60, true)); got != "Who May Run tm  design.html · published" {
		t.Errorf("wide: %q", got)
	}
	if got := ansi.Strip(libFileLabel(thread.LibFile{Name: "notes.md"}, 60, true)); got != "notes.md" {
		t.Errorf("no title: %q", got)
	}
	if got := libFileLabel(thread.LibFile{Name: "x.html", Published: libPublishedURL}, 60, false); got != "x.html · published" {
		t.Errorf("published, no title: %q", got)
	}
	if got := libFileLabel(f, 60, false); strings.Contains(got, "\x1b") {
		t.Errorf("a selected row is drawn plain: %q", got)
	}
	long := thread.LibFile{Name: strings.Repeat("n", 90) + ".md", DocTitle: strings.Repeat("t", 200), Published: libPublishedURL}
	for _, w := range []int{4, 8, 12, 20, 40, 80, 120} {
		for _, c := range []thread.LibFile{f, long, {Name: long.Name}, {Name: long.Name, Published: libPublishedURL}} {
			got := libFileLabel(c, w, true)
			if n := ansi.StringWidth(got); n > w {
				t.Errorf("width %d: %d cells: %q", w, n, ansi.Strip(got))
			}
			if c.DocTitle != "" && w >= 20 && !strings.Contains(ansi.Strip(got), "n") && c.Name == long.Name {
				t.Errorf("width %d: the file's name went: %q", w, ansi.Strip(got))
			}
		}
	}
	if got := ansi.Strip(libFileLabel(long, 80, true)); !strings.HasSuffix(got, " · published") || !strings.HasPrefix(got, "ttt") {
		t.Errorf("long: %q", got)
	}
}

// TestLibraryTitleRows: the tab's rows and hints, a project with no
// designs and a plain thread among them, and the link's key.
func TestLibraryTitleRows(t *testing.T) {
	pv, m := libraryTab(t, titledFiles())
	lines, _, _ := pv.libraryLines(90)
	text := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"Who May Run tm  design.html · published", "Report  REPORT.md", "plain.md", "page.html"} {
		if !strings.Contains(text, want) {
			t.Errorf("rows lack %q:\n%s", want, text)
		}
	}
	for _, l := range strings.Split(text, "\n") {
		if !strings.Contains(l, "t-000") && (strings.HasSuffix(l, " B") || strings.HasSuffix(l, " KB")) && ansi.StringWidth(l) != 90 {
			t.Errorf("size not at the row's end: %q", l)
		}
	}
	if strings.Contains(text, libPublishedURL) {
		t.Errorf("the address shows:\n%s", text)
	}

	var opened []string
	old := openURL
	openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	t.Cleanup(func() { openURL = old })

	pv.sel[tabLibrary] = 0
	if f, ok := pv.selLib(); !ok || f.Name != "design.html" {
		t.Fatalf("selected %+v", f)
	}
	if cmd := keyPress(m, "p"); cmd != nil {
		cmd()
	}
	if len(opened) != 1 || opened[0] != libPublishedURL {
		t.Errorf("p opened %v", opened)
	}
	opened = nil
	pv.sel[tabLibrary] = 1
	if cmd := keyPress(m, "p"); cmd != nil {
		cmd()
	}
	if len(opened) != 0 || !strings.Contains(m.msg, "no published page") {
		t.Errorf("no link: opened %v, said %q", opened, m.msg)
	}
}

// TestLibraryTitleFilter: words find a file by its own title, not by the
// address of its page.
func TestLibraryTitleFilter(t *testing.T) {
	files := titledFiles()
	if got := libShown(files, "may run", ""); len(got) != 1 || got[0].Name != "design.html" {
		t.Errorf("by title: %+v", got)
	}
	if got := libShown(files, "artifact", ""); len(got) != 0 {
		t.Errorf("by address: %+v", got)
	}
}

// TestLibFlatRowTitle: biggest first, a file's own title comes before what
// it came with; without one the row is as it was.
func TestLibFlatRowTitle(t *testing.T) {
	f := thread.LibFile{Thread: "t-0001", Task: "T1", Title: "A design", Name: "design.html", DocTitle: "Who May Run tm", Size: 57000}
	got := ansi.Strip(libFlatRow(f, 12, 90, true))
	if !strings.Contains(got, "design.html") || !strings.Contains(got, "Who May Run tm · T1") {
		t.Errorf("titled: %q", got)
	}
	f.DocTitle = ""
	if got := ansi.Strip(libFlatRow(f, 12, 90, true)); strings.Contains(got, "·") {
		t.Errorf("untitled: %q", got)
	}
}
