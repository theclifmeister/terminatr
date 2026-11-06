package tui

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/thread"
)

// The Library's biggest-first sort (docs/SPEC.md §4): s turns the tab from
// its groups, newest first, to one flat list of the files the filter and
// kind leave, biggest first, each with its task's title. Equal sizes keep
// the list's order, newest first, so a reload doesn't shuffle them. The Mac
// does the same (LibraryPage in TerminatrKit):
// testdata/library-filter.json pins both.

// libShowsSort says whether a library has anything to sort: two files.
func libShowsSort(files []thread.LibFile) bool { return len(files) >= 2 }

// libBiggestFirst are files by size, the biggest first.
func libBiggestFirst(files []thread.LibFile) []thread.LibFile {
	out := slices.Clone(files)
	slices.SortStableFunc(out, func(a, b thread.LibFile) int { return cmp.Compare(b.Size, a.Size) })
	return out
}

// libFlat says the tab lists biggest first: asked for, and for as long as
// the library has two files.
func (pv *projectView) libFlat() bool { return pv.libBiggest && libShowsSort(pv.library) }

// sortLib turns the sort; the selection stays on its file.
func (pv *projectView) sortLib(biggest bool) {
	at, ok := pv.selLib()
	pv.libBiggest = biggest
	pv.keepLibSel(at, ok)
}

// libSource is what a file came with: its task and title, its thread when
// it has neither, or the linked project that sent it.
func libSource(f thread.LibFile) string {
	if f.Peer != "" {
		return "from " + oneLine(f.Peer)
	}
	if s := joinSp(f.Task, oneLine(f.Title)); s != "" {
		return s
	}
	return f.Thread
}

// libSizeCells is a row's size column.
const libSizeCells = 9

// libSizeWords is a row's size, right-aligned in its column.
func libSizeWords(n int64) string { return fmt.Sprintf("%*s", libSizeCells, sizeWords(n)) }

// libRowRoom is the cells a row has for its name and title, less its indent,
// gaps and size.
func libRowRoom(w int) int { return w - libSizeCells - 4 }

// libFlatName is the flat list's name column for w cells: the longest name,
// up to three fifths of the room, so the titles line up.
func libFlatName(rows []libRow, w int) int {
	n := 0
	for _, r := range rows {
		n = max(n, ansi.StringWidth(oneLine(r.f.Name)))
	}
	return min(n, libRowRoom(w)*3/5)
}

// libFlatRow is a file's row in the flat list: its name, the title in the
// cells left (none when under eight) and its size. plain leaves the title
// unstyled, for a selected row.
func libFlatRow(f thread.LibFile, nameW, w int, plain bool) string {
	room := libRowRoom(w)
	title := room - nameW - 2
	if title < 8 {
		return "  " + fit(oneLine(f.Name), max(room, 4)) + "  " + libSizeWords(f.Size)
	}
	source := fit(libSource(f), title)
	if !plain {
		source = th().Faint.Render(source)
	}
	return "  " + fit(oneLine(f.Name), nameW) + "  " + source + "  " + libSizeWords(f.Size)
}

// libFilterLine is the head's line under the chips, "" for none: the
// total, the filter's words or its key, and the sort's key. sortAt is the
// column the sort's key starts in, -1 for none. Typing, it is the filter
// alone.
func (pv *projectView) libFilterLine(w int) (line string, sortAt int) {
	if pv.libTyping {
		return th().Accent.Render("/") + " " + oneLine(pv.libFilter) + th().Accent.Render("▏"), -1
	}
	sorts := libShowsSort(pv.library)
	if pv.libFilter != "" {
		n := len(libShown(pv.library, pv.libFilter, ""))
		line = th().Faint.Render("/ ") + oneLine(pv.libFilter) + th().Faint.Render(fmt.Sprintf(" · %d of %d · esc clears", n, len(pv.library)))
	} else if sorts || libShowsFilter(pv.library, pv.libFilter) {
		lead := ""
		if pv.libFlat() {
			lead = "biggest first · "
		}
		lead += sizeWords(libTotal(libShown(pv.library, "", pv.libPick)))
		if libShowsFilter(pv.library, "") {
			lead += " · / filter"
		}
		line = th().Faint.Render(lead)
	}
	if !sorts {
		return line, -1
	}
	word := "s biggest first"
	if pv.libFlat() {
		word = "s newest first"
	}
	sortAt = ansi.StringWidth(line) + ansi.StringWidth(" · ")
	if sortAt+len(word) > w {
		return line, -1
	}
	return line + th().Faint.Render(" · "+word), sortAt
}
