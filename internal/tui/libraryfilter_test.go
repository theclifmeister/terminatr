package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/thread"
)

// libFixture is testdata/library-filter.json, which the Swift tests read
// too (LibraryPage in TerminatrKit): both apps and the TUI agree.
type libFixture struct {
	Kinds     map[string]string           `json:"kinds"`
	Libraries map[string][]thread.LibFile `json:"libraries"`
	Cases     []struct {
		Library string   `json:"library"`
		Query   string   `json:"query"`
		Kind    string   `json:"kind"`
		Names   []string `json:"names"`
		Chips   [][]any  `json:"chips"`
	} `json:"cases"`
	// Order is the order the Library lists a library's files in: a seat's
	// own first, a program's projects as they came.
	Order []struct {
		Library string   `json:"library"`
		Names   []string `json:"names"`
	} `json:"order"`
	Filter []struct {
		Files int    `json:"files"`
		Query string `json:"query"`
		Shows bool   `json:"shows"`
	} `json:"filter"`
}

func TestLibraryFilterFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/library-filter.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx libFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	for name, kind := range fx.Kinds {
		if got := libKind(name); got != kind {
			t.Errorf("kind of %q: %s, want %s", name, got, kind)
		}
	}
	for _, c := range fx.Cases {
		id := fmt.Sprintf("%s, %q, %q", c.Library, c.Query, c.Kind)
		names := []string{}
		for _, f := range libShown(fx.Libraries[c.Library], c.Query, c.Kind) {
			names = append(names, f.Name)
		}
		if !slices.Equal(names, c.Names) {
			t.Errorf("%s: shows %v, want %v", id, names, c.Names)
		}
		var got, want []string
		for _, ch := range libChips(fx.Libraries[c.Library], c.Query, c.Kind) {
			got = append(got, fmt.Sprintf("%s %d", ch.kind, ch.n))
		}
		for _, ch := range c.Chips {
			want = append(want, fmt.Sprintf("%s %.0f", ch[0], ch[1]))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: chips %v, want %v", id, got, want)
		}
	}
	for _, c := range fx.Order {
		names := []string{}
		for _, f := range libraryOrder(fx.Libraries[c.Library]) {
			names = append(names, f.Name)
		}
		if !slices.Equal(names, c.Names) {
			t.Errorf("%s: order %v, want %v", c.Library, names, c.Names)
		}
	}
	for _, c := range fx.Filter {
		if got := libShowsFilter(make([]thread.LibFile, c.Files), c.Query); got != c.Shows {
			t.Errorf("%d files, %q: shows the filter %v", c.Files, c.Query, got)
		}
	}
}

// libraryTab opens a Library tab over files.
func libraryTab(t *testing.T, files []thread.LibFile) (*projectView, *dash) {
	t.Helper()
	src, m := popupData(t)
	src.library = files
	feed(m, keyPress(m, "A"))
	pv := m.top().(*projectView)
	keyPress(m, "8")
	if pv.tab != tabLibrary {
		t.Fatalf("tab %d", pv.tab)
	}
	return pv, m
}

// manyFiles is a library of 8 files of three kinds.
func manyFiles() []thread.LibFile {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	var out []thread.LibFile
	for i, n := range []string{"a.png", "b.png", "c.png", "d.png", "e.png", "one.md"} {
		out = append(out, thread.LibFile{Thread: "t-0002", Task: "T2", Title: "Screens", State: "running", Name: n, Size: 10, Time: day(20 - i)})
	}
	out = append(out, thread.LibFile{Thread: "t-0001", Task: "T1", Title: "Data", State: "running", Name: "x.json", Size: 10, Time: day(5)})
	out = append(out, thread.LibFile{Thread: "t-0001", Task: "T1", Title: "Data", State: "running", Name: "y.json", Size: 10, Time: day(4)})
	return out
}

func typeInto(m *dash, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// TestLibraryFilterKeys: / types into the filter, whose every key is the
// filter's (the tabs' digits too); enter keeps it, esc drops it, and esc
// with the filter kept clears it before it closes the popup.
func TestLibraryFilterKeys(t *testing.T) {
	pv, m := libraryTab(t, manyFiles())
	out := screen(m)
	for _, want := range []string{" all 8 ", " images 5 ", " notes 1 ", " data 2 ", "t kinds", "/ filter"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	keyPress(m, "down")
	keyPress(m, "/")
	if !pv.libTyping {
		t.Fatal("/ didn't start the filter")
	}
	typeInto(m, "T1")
	if pv.tab != tabLibrary || pv.libFilter != "T1" {
		t.Fatalf("typed: tab %d, filter %q", pv.tab, pv.libFilter)
	}
	out = screen(m)
	if !strings.Contains(out, "x.json") || strings.Contains(out, "a.png") {
		t.Errorf("filter T1:\n%s", out)
	}
	if !strings.Contains(out, "esc clear") {
		t.Errorf("typing's keys:\n%s", out)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if pv.libFilter != "T" {
		t.Errorf("backspace: %q", pv.libFilter)
	}
	typeInto(m, "1")
	keyPress(m, "enter")
	if pv.libTyping || pv.libFilter != "T1" {
		t.Fatalf("enter: typing %v, filter %q", pv.libTyping, pv.libFilter)
	}
	if out := screen(m); !strings.Contains(out, "2 of 8") {
		t.Errorf("kept filter:\n%s", out)
	}
	// The kept filter: X now acts on what shows.
	if f, ok := pv.selLib(); !ok || f.Thread != "t-0001" {
		t.Errorf("selected %+v", f)
	}
	keyPress(m, "esc")
	if m.top() != pv || pv.libFilter != "" {
		t.Fatalf("esc: %T, filter %q", m.top(), pv.libFilter)
	}
	// esc while typing drops the words and stays.
	keyPress(m, "/")
	typeInto(m, "nothing")
	if out := screen(m); !strings.Contains(out, "nothing matches: esc clears") {
		t.Errorf("no match:\n%s", out)
	}
	keyPress(m, "enter")
	if cmd := keyPress(m, "enter"); cmd != nil {
		t.Errorf("enter on no file: %v", cmd)
	}
	keyPress(m, "esc")
	if out := screen(m); !strings.Contains(out, "a.png") {
		t.Errorf("after esc:\n%s", out)
	}
	keyPress(m, "esc")
	if m.top() != nil {
		t.Errorf("esc with nothing to clear: %T", m.top())
	}
}

// TestLibraryKindKeys: t steps through the shown kinds and round to all;
// the selection stays on its file while it still shows.
func TestLibraryKindKeys(t *testing.T) {
	pv, m := libraryTab(t, manyFiles())
	for _, want := range []struct {
		kind string
		n    int
	}{{"images", 5}, {"notes", 1}, {"data", 2}, {"", 8}} {
		keyPress(m, "t")
		if pv.libPick != want.kind || len(pv.libFiles()) != want.n {
			t.Fatalf("t: %q, %d files; want %v", pv.libPick, len(pv.libFiles()), want)
		}
	}
	keyPress(m, "t")
	if out := screen(m); strings.Contains(out, "x.json") || !strings.Contains(out, "a.png") {
		t.Errorf("images:\n%s", out)
	}
	keyPress(m, "down")
	if f, _ := pv.selLib(); f.Name != "b.png" {
		t.Fatalf("selected %q", f.Name)
	}
	keyPress(m, "t") // notes: b.png doesn't show there
	if f, _ := pv.selLib(); f.Name != "one.md" {
		t.Errorf("selected %q", f.Name)
	}
	keyPress(m, "t")
	keyPress(m, "t") // all: the first, as the selection went with notes' file
	if pv.libPick != "" {
		t.Fatalf("pick %q", pv.libPick)
	}
	keyPress(m, "t")
	keyPress(m, "esc") // lets go of the kind first
	if m.top() != pv || pv.libPick != "" {
		t.Fatalf("esc: %T, pick %q", m.top(), pv.libPick)
	}
}

// TestLibraryChipClick: the mouse reaches what the keys do: a chip picks
// its kind, the filter's line starts typing.
func TestLibraryChipClick(t *testing.T) {
	pv, m := libraryTab(t, manyFiles())
	x, y := at(t, m, " data 2 ")
	mouseAt(t, m, tea.MouseLeft, x+1, y)
	if pv.libPick != "data" {
		t.Fatalf("click on data: %q", pv.libPick)
	}
	x, y = at(t, m, " all 8 ")
	mouseAt(t, m, tea.MouseLeft, x+1, y)
	if pv.libPick != "" {
		t.Fatalf("click on all: %q", pv.libPick)
	}
	x, y = at(t, m, "/ filter")
	mouseAt(t, m, tea.MouseLeft, x, y)
	if !pv.libTyping {
		t.Fatal("click on the filter: not typing")
	}
	x, y = at(t, m, "a.png")
	mouseAt(t, m, tea.MouseLeft, x, y)
	if pv.libTyping {
		t.Fatal("a click on a file left the filter typing")
	}
}

// TestLibraryChipsWrap: narrow, every chip stays in reach.
func TestLibraryChipsWrap(t *testing.T) {
	var files []thread.LibFile
	for _, n := range []string{"a.png", "b.html", "c.md", "d.json", "e.zip", "f.mp4", "g.go", "h.bin"} {
		files = append(files, thread.LibFile{Thread: "t-0001", Title: "All", Name: n, Size: 1})
	}
	pv := &projectView{library: files, libOK: true}
	ls := pv.libChipLines(30)
	if len(ls) < 2 {
		t.Fatalf("%d lines", len(ls))
	}
	n := 0
	for _, l := range ls {
		n += len(l.spans)
		for _, s := range l.spans {
			if s.to > 30 {
				t.Errorf("chip %q runs to %d", s.kind, s.to)
			}
		}
	}
	if n != 9 {
		t.Errorf("%d chips", n)
	}
}

// TestLibraryQuietLibraries: no files, one file, a library of only notes:
// no chips and no filter, and / and t do nothing.
func TestLibraryQuietLibraries(t *testing.T) {
	for name, files := range map[string][]thread.LibFile{
		"no files": nil,
		"one file": {{Thread: "t-0001", Task: "T1", Title: "Only", Name: "one.png", Size: 1}},
		"notes":    {{Thread: "t-0001", Title: "A", Name: "a.md", Size: 1}, {Thread: "t-0001", Title: "A", Name: "b.txt", Size: 1}},
	} {
		pv, m := libraryTab(t, files)
		out := screen(m)
		for _, no := range []string{"t kinds", "/ filter", " all "} {
			if strings.Contains(out, no) {
				t.Errorf("%s: shows %q:\n%s", name, no, out)
			}
		}
		keyPress(m, "/")
		keyPress(m, "t")
		if pv.libTyping || pv.libPick != "" {
			t.Errorf("%s: / or t did something", name)
		}
		if name == "no files" && !strings.Contains(out, "no files: a thread's report attaches them") {
			t.Errorf("no files:\n%s", out)
		}
	}
}

// TestLibraryLinkedAndRepos: a linked project's file and a repository are
// found by name, and a deleted last file of the picked kind lets go of it.
func TestLibraryLinkedAndRepos(t *testing.T) {
	files := append(manyFiles(), thread.LibFile{Peer: "studio/acme", Message: "M-1", Title: "from studio/acme", State: "received", Name: "brief.pdf", Size: 1})
	files[0].Repo = "site"
	pv, m := libraryTab(t, files)
	keyPress(m, "/")
	typeInto(m, "studio")
	if fs := pv.libFiles(); len(fs) != 1 || fs[0].Name != "brief.pdf" {
		t.Errorf("by linked project: %v", fs)
	}
	if out := screen(m); !strings.Contains(out, "from studio/acme") {
		t.Errorf("linked group:\n%s", out)
	}
	keyPress(m, "esc")
	keyPress(m, "/")
	typeInto(m, "site")
	if fs := pv.libFiles(); len(fs) != 1 || fs[0].Name != "a.png" {
		t.Errorf("by repo: %v", fs)
	}
	keyPress(m, "esc")
	pv.libPick = "pages"
	pv.library = pv.library[:len(pv.library)-1] // brief.pdf was deleted
	if got := len(pv.libFiles()); got != 8 {
		t.Errorf("after the kind's last file went: %d files", got)
	}
}
