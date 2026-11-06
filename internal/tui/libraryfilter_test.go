package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	Heads []struct {
		Library  string   `json:"library"`
		Group    string   `json:"group"`
		Status   string   `json:"status"`
		Where    []string `json:"where"`
		Archived bool     `json:"archived"`
		Kinds    []string `json:"kinds"`
		Bytes    int64    `json:"bytes"`
	} `json:"heads"`
	Fold []struct {
		Files int    `json:"files"`
		Open  bool   `json:"open"`
		Query string `json:"query"`
		Rows  int    `json:"rows"`
		More  int    `json:"more"`
		Fewer bool   `json:"fewer"`
	} `json:"fold"`
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
	for _, c := range fx.Heads {
		var files []thread.LibFile
		for _, f := range fx.Libraries[c.Library] {
			if f.Group() == c.Group {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			t.Fatalf("%s has no group %s", c.Library, c.Group)
		}
		f := files[0]
		if f.TaskStatus != c.Status || f.State == "archived" != c.Archived || !slices.Equal(libWhere(f), c.Where) ||
			!slices.Equal(libKindWords(files), c.Kinds) || libTotal(files) != c.Bytes {
			t.Errorf("%s/%s: %q %q %v %v %d, want %+v", c.Library, c.Group, f.TaskStatus, f.State, libWhere(f), libKindWords(files), libTotal(files), c)
		}
	}
	for _, c := range fx.Fold {
		if rows, more, fewer := libFold(c.Files, c.Open, c.Query); rows != c.Rows || more != c.More || fewer != c.Fewer {
			t.Errorf("fold %+v: %d %d %v", c, rows, more, fewer)
		}
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

// groupOf is a thread's n files of one kind, newest first.
func groupOf(id, task, status, title, repo, state string, n int, at time.Time) []thread.LibFile {
	var out []thread.LibFile
	for i := range n {
		out = append(out, thread.LibFile{Thread: id, Task: task, TaskStatus: status, Title: title, Repo: repo, State: state,
			Name: fmt.Sprintf("%s-%02d.png", id, i), Size: 1000, Time: at.Add(-time.Duration(i) * time.Minute)})
	}
	return out
}

// TestLibraryHeadings: each setup's heading says what it has and leaves out
// what it hasn't.
func TestLibraryHeadings(t *testing.T) {
	at := time.Date(time.Now().Year(), 3, 15, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name        string
		files       []thread.LibFile
		has, hasNot []string
	}{
		{"plain folder, no repo", groupOf("t-0001", "T1", "started", "Plain", "", "running", 1, at),
			[]string{"T1  Plain", "started", "t-0001 · 15 Mar · 1 image · 1.0 KB"}, []string{"· 1 image · 1 image"}},
		{"thread with no task", groupOf("t-0002", "", "", "Quick look", "", "stopped", 2, at),
			[]string{"Quick look", "t-0002 · 15 Mar · 2 images · 2.0 KB"}, []string{"started", "done"}},
		{"several repos: named", groupOf("t-0003", "T3", "review", "Backend", "api", "running", 1, at),
			[]string{"review", "t-0003 · api · 15 Mar · 1 image"}, nil},
		{"linked project: no status, no thread", []thread.LibFile{
			{Peer: "studio/acme", Message: "M-1", Title: "from studio/acme", State: "received", Name: "brief.pdf", Size: 5, Time: at},
			{Peer: "studio/acme", Message: "M-2", Title: "from studio/acme", State: "received", Name: "more.pdf", Size: 5, Time: at}},
			[]string{"from studio/acme", "15 Mar · 2 pages · 10 B"}, []string{"t-0", "received"}},
		{"archived thread", groupOf("t-0004", "T4", "done", "Old", "", "archived", 1, at),
			[]string{"T4  Old", "done", "t-0004 · 15 Mar · archived · 1 image"}, nil},
		{"last year's", groupOf("t-0005", "T5", "done", "Old", "", "running", 1, at.AddDate(-1, 0, 0)),
			[]string{fmt.Sprintf("15 Mar %d", at.Year()-1)}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			title, meta := libHeading(c.files, 100)
			out := ansi.Strip(title) + "\n" + ansi.Strip(meta)
			for _, want := range c.has {
				if !strings.Contains(out, want) {
					t.Errorf("lacks %q:\n%s", want, out)
				}
			}
			for _, not := range c.hasNot {
				if strings.Contains(out, not) {
					t.Errorf("has %q:\n%s", not, out)
				}
			}
		})
	}
	// Too narrow for every kind: a count, and the size stays.
	files := []thread.LibFile{
		{Thread: "t-0001", Title: "T", Name: "a.png", Size: 5, Time: at}, {Thread: "t-0001", Title: "T", Name: "b.md", Size: 5, Time: at},
		{Thread: "t-0001", Title: "T", Name: "c.json", Size: 5, Time: at}, {Thread: "t-0001", Title: "T", Name: "d.zip", Size: 5, Time: at},
	}
	if _, meta := libHeading(files, 44); !strings.Contains(ansi.Strip(meta), "t-0001 · 15 Mar · 4 files · 20 B") {
		t.Errorf("narrow heading: %q", ansi.Strip(meta))
	}
	if _, meta := libHeading(files, 100); !strings.Contains(ansi.Strip(meta), "1 image · 1 note · 1 data file · 1 archive · 20 B") {
		t.Errorf("wide heading: %q", ansi.Strip(meta))
	}
	if got := sizeWords(1_440_000_000); got != "1.44 GB" {
		t.Errorf("a library's total: %s", got)
	}
}

// TestLibraryFold: a group of 37 folds to six rows and +31 more, enter
// opens it and folds it again; a group of one has no fold; the heading
// counts every file; a click works the row too.
func TestLibraryFold(t *testing.T) {
	at := time.Date(time.Now().Year(), 3, 15, 12, 0, 0, 0, time.UTC)
	files := append(groupOf("t-0037", "T37", "done", "Many", "", "running", 37, at), groupOf("t-0001", "T1", "started", "One", "", "running", 1, at.Add(-time.Hour))...)
	pv, m := libraryTab(t, files)
	out := screen(m)
	for _, want := range []string{"t-0037 · 15 Mar · 37 images · 37.0 KB", "+31 more", "t-0037-05.png", "t-0001 · 15 Mar · 1 image · 1.0 KB", "t-0001-00.png"} {
		if !strings.Contains(out, want) {
			t.Errorf("folded lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "t-0037-06.png") {
		t.Errorf("a folded file shows:\n%s", out)
	}
	if rows := pv.libRows(); len(rows) != 8 || rows[6].more != 31 {
		t.Fatalf("rows: %d", len(rows))
	}
	for range 6 {
		keyPress(m, "down")
	}
	if _, ok := pv.selLib(); ok || !pv.onLibFold() {
		t.Fatal("the fold's row isn't selected")
	}
	if out := screen(m); !strings.Contains(out, "enter open or fold the group") {
		t.Errorf("keys:\n%s", out)
	}
	keyPress(m, "X") // nothing to delete on it
	if m.top() != pv {
		t.Fatalf("X on a fold's row: %T", m.top())
	}
	keyPress(m, "enter")
	if f, ok := pv.selLib(); !ok || f.Name != "t-0037-06.png" {
		t.Fatalf("opened: on %+v %v", f, ok)
	}
	if rows := pv.libRows(); len(rows) != 39 || !rows[37].fewer {
		t.Fatalf("open rows: %d", len(rows))
	}
	pv.sel[tabLibrary] = 37
	keyPress(m, "enter")
	if rows := pv.libRows(); len(rows) != 8 || pv.sel[tabLibrary] != 6 {
		t.Fatalf("folded again: %d rows, on %d", len(rows), pv.sel[tabLibrary])
	}
	pv.click(m, 6, 0, false)
	if len(pv.libRows()) != 39 {
		t.Errorf("a click didn't open the group")
	}
	pv.setLibOpen("t-0037", false)

	// Words open the fold, and the heading sums what they leave.
	pv.narrowLib("t-0037-1", "")
	if rows := pv.libRows(); len(rows) != 10 || !rows[0].isFile() {
		t.Fatalf("filtered: %d rows", len(rows))
	}
	if out := screen(m); !strings.Contains(out, "10 images · 10.0 KB") || strings.Contains(out, "more") {
		t.Errorf("filtered:\n%s", out)
	}
	// A file selected past the fold when the words go keeps its place.
	pv.sel[tabLibrary] = 9 // t-0037-19
	pv.narrowLib("", "")
	if f, ok := pv.selLib(); !ok || f.Name != "t-0037-19.png" {
		t.Errorf("after the filter: on %+v %v", f, ok)
	}
}

// TestLibraryFoldReload: the selection counts rows, not files, so when a
// reload leaves fewer rows than files it still lands on one.
func TestLibraryFoldReload(t *testing.T) {
	at := time.Date(time.Now().Year(), 3, 15, 12, 0, 0, 0, time.UTC)
	many := groupOf("t-0037", "T37", "done", "Many", "", "running", 37, at)
	few := groupOf("t-0001", "T1", "started", "Few", "", "running", 3, at.Add(-time.Hour))
	src, m := popupData(t)
	src.library = append(append([]thread.LibFile{}, many...), few...)
	feed(m, keyPress(m, "A"))
	pv := m.top().(*projectView)
	keyPress(m, "8")
	pv.sel[tabLibrary] = len(pv.libRows()) - 1
	if f, ok := pv.selLib(); !ok || f.Thread != "t-0001" {
		t.Fatalf("selected %+v %v", f, ok)
	}
	// The board didn't load: only the library is new, as it reloads.
	m.Update(boardMsg{slug: pv.slug, err: errors.New("no board"), libOK: true, library: many})
	if got := len(pv.libRows()); got != 7 || pv.sel[tabLibrary] != 6 || !pv.onLibFold() {
		t.Errorf("after the reload: %d rows, on %d", got, pv.sel[tabLibrary])
	}
}
