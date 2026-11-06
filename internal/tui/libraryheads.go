package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/thread"
)

// The Library's headings and folds (docs/SPEC.md §4): a heading says its
// task's status, its thread, the repository in a project with several,
// its newest file's day, its kinds and its size, all of the files the
// filter and kind leave; a group over six files folds to six rows and
// "+N more". The Mac does the same (LibraryPage in TerminatrKit):
// testdata/library-filter.json pins both.

// libFoldAt is how many rows a folded group shows.
const libFoldAt = 6

// libFold is how a group of n files lists: how many rows, how many files
// its fold hides, and whether it offers to fold again. Words in the filter
// open every fold, so what they find shows.
func libFold(n int, open bool, query string) (rows, more int, fewer bool) {
	switch {
	case n <= libFoldAt || len(libWords(query)) > 0:
		return n, 0, false
	case open:
		return n, 0, true
	}
	return libFoldAt, n - libFoldAt, false
}

// libKindNoun is one file of a kind, in a heading.
var libKindNoun = map[string]string{
	"images": "image", "pages": "page", "notes": "note", "data": "data file",
	"archives": "archive", "video": "video", "code": "code file", "other": "file",
}

// libKindWords are the kinds of files, in the chips' order: "26 images",
// "2 data files".
func libKindWords(files []thread.LibFile) []string {
	count := map[string]int{}
	for _, f := range files {
		count[libKind(f.Name)]++
	}
	var out []string
	for _, k := range libKinds {
		if n := count[k]; n > 0 {
			out = append(out, libCount(n, libKindNoun[k]))
		}
	}
	return out
}

func libCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// libTotal is the files' size.
func libTotal(files []thread.LibFile) int64 {
	var n int64
	for _, f := range files {
		n += f.Size
	}
	return n
}

// libWhere is where a group's files came from, without the day: its thread
// and repository. A linked project's group has neither, nor a seat's own.
func libWhere(f thread.LibFile) []string {
	if f.Peer != "" || f.IsSeat() {
		return nil
	}
	out := []string{f.Thread}
	if f.Repo != "" {
		out = append(out, oneLine(f.Repo))
	}
	return out
}

// libDay is a day as a heading says it: the year only when it isn't this
// one.
func libDay(t time.Time) string {
	t = t.Local()
	if t.Year() == time.Now().Year() {
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// libHeading is a group's two lines for w cells: its title and its task's
// status, then the rest. When the kinds don't fit they become a count.
func libHeading(files []thread.LibFile, w int) (title, meta string) {
	f := files[0]
	head := "from " + oneLine(f.Peer)
	if f.Peer == "" {
		head = joinSp(f.Task, oneLine(f.Title))
	}
	if f.Peer == "" && f.TaskStatus != "" {
		status := oneLine(f.TaskStatus)
		_, look := stateLook(status)
		word := stateText(status)
		title = th().Head.Render(ansi.Truncate(head, max(w-ansi.StringWidth(word)-2, 8), "…")) + "  " + look.Render(word)
	} else {
		title = th().Head.Render(ansi.Truncate(head, max(w, 8), "…"))
	}
	lead := append(libWhere(f), libDay(f.Time))
	if f.State == "archived" {
		lead = append(lead, "archived")
	}
	size := sizeWords(libTotal(files))
	line := func(kinds []string) string {
		return "  " + strings.Join(append(append(append([]string{}, lead...), kinds...), size), " · ")
	}
	text := line(libKindWords(files))
	if ansi.StringWidth(text) > w {
		text = line([]string{libCount(len(files), "file")})
	}
	return title, th().Faint.Render(ansi.Truncate(text, max(w, 8), "…"))
}

// libFileLabel is a file's row for w cells: its name, or its own title with
// the name beside it, faint when it may be styled, and "· published" for a
// page with a published copy. Both stay readable in a tight row: the title
// gives way down to three fifths, then the name to what's left.
func libFileLabel(f thread.LibFile, w int, styled bool) string {
	name, tag := oneLine(f.Name), ""
	if f.Published != "" {
		tag = " · published"
	}
	nameW := ansi.StringWidth(name) + ansi.StringWidth(tag)
	if f.DocTitle == "" {
		if nameW > w {
			if room := w - ansi.StringWidth(tag); room >= 8 {
				return ansi.Truncate(name, room, "…") + tag
			}
			return fit(name, w)
		}
		return name + tag
	}
	title := oneLine(f.DocTitle)
	titleW := min(ansi.StringWidth(title), max(w-2-min(nameW, max(w*2/5, 8)), 0))
	nameRoom := w - 2 - titleW
	if titleW < 4 {
		return fit(title, w)
	}
	if room := nameRoom - ansi.StringWidth(tag); room >= 8 {
		name = ansi.Truncate(name, room, "…") + tag
	} else {
		name = ansi.Truncate(name, nameRoom, "…")
	}
	if styled {
		name = th().Faint.Render(name)
	}
	return ansi.Truncate(title, titleW, "…") + "  " + name
}

// libRow is a row under a heading: a file, or a fold's. f is the file, or
// for a fold's row the group's first.
type libRow struct {
	f     thread.LibFile
	more  int  // the row that opens the group: how many it hides
	fewer bool // the row that folds it again
}

func (r libRow) isFile() bool { return r.more == 0 && !r.fewer }

// word is a fold's row.
func (r libRow) word() string {
	if r.fewer {
		return "show fewer"
	}
	return fmt.Sprintf("+%d more", r.more)
}

// libShownGroup is a group as it lists: the files the filter and kind
// leave, which its heading sums up, and its rows.
type libShownGroup struct {
	files []thread.LibFile
	rows  []libRow
}

// libShownGroups are the listed groups, in order.
func (pv *projectView) libShownGroups() []libShownGroup {
	files := pv.libFiles()
	var out []libShownGroup
	for start := 0; start < len(files); {
		end := start + 1
		for end < len(files) && files[end].Group() == files[start].Group() {
			end++
		}
		g := libShownGroup{files: files[start:end]}
		rows, more, fewer := libFold(len(g.files), pv.libOpen[files[start].Group()], pv.libFilter)
		for _, f := range g.files[:rows] {
			g.rows = append(g.rows, libRow{f: f})
		}
		if more > 0 || fewer {
			g.rows = append(g.rows, libRow{f: files[start], more: more, fewer: fewer})
		}
		out = append(out, g)
		start = end
	}
	return out
}

// libRows are the listed rows: what the selection walks.
func (pv *projectView) libRows() []libRow {
	var out []libRow
	for _, g := range pv.libShownGroups() {
		out = append(out, g.rows...)
	}
	return out
}

// libRowOf is the row of the first file same says yes to, -1 for none.
func (pv *projectView) libRowOf(same func(thread.LibFile) bool) int {
	for i, r := range pv.libRows() {
		if r.isFile() && same(r.f) {
			return i
		}
	}
	return -1
}

func (pv *projectView) setLibOpen(group string, open bool) {
	if pv.libOpen == nil {
		pv.libOpen = map[string]bool{}
	}
	pv.libOpen[group] = open
}

// onLibFold says the selection is on a fold's row.
func (pv *projectView) onLibFold() bool {
	rows := pv.libRows()
	i := pv.sel[tabLibrary]
	return i < len(rows) && !rows[i].isFile()
}

// toggleLibFold opens or folds the group of the selected fold's row, false
// when the selection is on a file. Opening leaves it on the first file
// that came out; folding, on the group's row again.
func (pv *projectView) toggleLibFold() bool {
	if !pv.onLibFold() {
		return false
	}
	r := pv.libRows()[pv.sel[tabLibrary]]
	group := r.f.Group()
	pv.setLibOpen(group, r.more > 0)
	if r.fewer {
		for i, n := range pv.libRows() {
			if n.more > 0 && n.f.Group() == group {
				pv.sel[tabLibrary] = i
			}
		}
	}
	return true
}
