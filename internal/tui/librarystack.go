package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/thread"
)

// The Library's stacks (docs/SPEC.md §4): two images of one thread that are
// one picture twice list as one row with a switch. The rule goes by names
// alone: same extension, the rest of the name identical, -dark/-light as the
// ending or before/after at the start or the end, and exactly two files. A
// wrong pair costs little: the switch shows both and the heading counts both.
// The Mac does the same (LibraryPage in TerminatrKit):
// testdata/library-filter.json pins both.

// libStackPairs are the words that pair, and whether each may also start a
// name: dark-matter.png is no variant of anything, before-action.png is.
var libStackPairs = []struct {
	words [2]string
	start bool
}{
	{[2]string{"dark", "light"}, false},
	{[2]string{"before", "after"}, true},
}

// libEntry is a row's worth of files: one, or a stack's two in list order.
type libEntry struct {
	f     thread.LibFile
	other *thread.LibFile
	words [2]string // f's word and other's
	title string    // the name without the word: mac-conv.png
}

// libSlot is what a file has to share with the other half of its pair.
type libSlot struct {
	pair      int
	start     bool
	rest, ext string
}

// libSlotOf is one way a file could be half of a pair.
type libSlotOf struct {
	libSlot
	word string
}

// libEqualASCII says whether s is word (lower case), in either case. Bytes,
// so a Unicode letter that folds to an ASCII one isn't the word; Swift's
// rule reads the same bytes.
func libEqualASCII(s, word string) bool {
	if len(s) != len(word) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != word[i] {
			return false
		}
	}
	return true
}

// libSlots are the ways a file's name could be half of a pair: none unless
// it's an image of a thread. A linked project's files come from no thread.
func libSlots(f thread.LibFile) []libSlotOf {
	i := strings.LastIndexByte(f.Name, '.')
	if f.Peer != "" || i <= 0 || libKind(f.Name) != "images" {
		return nil
	}
	stem, ext := f.Name[:i], strings.ToLower(f.Name[i:])
	var out []libSlotOf
	for p, pair := range libStackPairs {
		for _, w := range pair.words {
			n := len(stem) - len(w)
			switch {
			case n < 0:
			case n == 0:
				if libEqualASCII(stem, w) { // the whole name: dark.png
					out = append(out, libSlotOf{libSlot{p, false, "", ext}, w})
				}
			default:
				if stem[n-1] == '-' && libEqualASCII(stem[n:], w) {
					out = append(out, libSlotOf{libSlot{p, false, stem[:n-1], ext}, w})
				}
				if pair.start && stem[len(w)] == '-' && libEqualASCII(stem[:len(w)], w) {
					out = append(out, libSlotOf{libSlot{p, true, stem[len(w)+1:], ext}, w})
				}
			}
		}
	}
	return out
}

// libEntries are a group's files as rows, in their order. A pair takes the
// place of its first file. A slot with anything but one file of each word
// pairs nothing, and a file two pairs want stays in neither: both are more
// than two files of one name.
func libEntries(files []thread.LibFile) []libEntry {
	type half struct {
		at   int
		word string
	}
	slots := map[libSlot][]half{}
	var order []libSlot
	for i, f := range files {
		for _, s := range libSlots(f) {
			if slots[s.libSlot] == nil {
				order = append(order, s.libSlot)
			}
			slots[s.libSlot] = append(slots[s.libSlot], half{i, s.word})
		}
	}
	wanted := map[int]int{}
	var pairs []libSlot
	for _, s := range order {
		h := slots[s]
		if len(h) == 2 && h[0].word != h[1].word {
			pairs = append(pairs, s)
			wanted[h[0].at]++
			wanted[h[1].at]++
		}
	}
	partner := map[int]int{}
	entries := map[int]libEntry{}
	for _, s := range pairs {
		h := slots[s]
		a, b := h[0], h[1]
		if wanted[a.at] != 1 || wanted[b.at] != 1 {
			continue
		}
		other := files[b.at]
		title := s.rest + files[a.at].Name[strings.LastIndexByte(files[a.at].Name, '.'):]
		if s.rest == "" {
			title = files[a.at].Name
		}
		entries[a.at] = libEntry{f: files[a.at], other: &other, words: [2]string{a.word, b.word}, title: title}
		partner[b.at] = a.at
	}
	out := make([]libEntry, 0, len(files))
	for i, f := range files {
		if _, second := partner[i]; second {
			continue
		}
		e, ok := entries[i]
		if !ok {
			e = libEntry{f: f, title: f.Name}
		}
		out = append(out, e)
	}
	return out
}

// libShowKey is a stack's place in pv.libShow, which file its row shows: the
// first unless v switched it. Kept by name, so a reload doesn't lose it.
func libShowKey(e libEntry) string { return e.f.Group() + "\x00" + e.f.Name }

// libRowFor is an entry's row, showing the file the switch is on.
func (pv *projectView) libRowFor(e libEntry) libRow {
	r := libRow{f: e.f}
	if e.other != nil {
		stack := e
		r.stack = &stack
		if pv.libShow[libShowKey(e)] == e.other.Name {
			r.f = *e.other
		}
	}
	return r
}

// shownWord and otherWord are the switch's words for the file showing and
// for the other one; otherFile is the file the switch goes to.
func (r libRow) shownWord() string {
	if r.stack.f.Name == r.f.Name {
		return r.stack.words[0]
	}
	return r.stack.words[1]
}

func (r libRow) otherWord() string {
	if r.stack.f.Name == r.f.Name {
		return r.stack.words[1]
	}
	return r.stack.words[0]
}

func (r libRow) otherFile() thread.LibFile {
	if r.stack.f.Name == r.f.Name {
		return *r.stack.other
	}
	return r.stack.f
}

// switchLib turns a stack's row to its other file.
func (pv *projectView) switchLib(r libRow) {
	if pv.libShow == nil {
		pv.libShow = map[string]string{}
	}
	pv.libShow[libShowKey(*r.stack)] = r.otherFile().Name
}

const (
	libSizeCols = 9  // a row's size
	libTitleMin = 12 // the least of a stack's name beside its switch
)

// libSwitchCols is how wide a row's switch is: "before after".
const libSwitchCols = 12

// libSwitchSpan is where a row's switch lies in a list w cells wide, when the
// list has room for it beside a name.
func libSwitchSpan(w int) (from, to int, ok bool) {
	to = w - libSizeCols - 2
	from = to - libSwitchCols
	return from, to, from >= 2+libTitleMin+2
}

// libStackRow is the selected row when it's a stack.
func (pv *projectView) libStackRow() (libRow, bool) {
	rows := pv.libRows()
	if i := pv.sel[tabLibrary]; i < len(rows) && rows[i].stack != nil {
		return rows[i], true
	}
	return libRow{}, false
}

// libStackOf is the stack's row that shows f, if any.
func (pv *projectView) libStackOf(f thread.LibFile) (libRow, bool) {
	for _, r := range pv.libRows() {
		if r.stack != nil && r.f.Group() == f.Group() && r.f.Name == f.Name {
			return r, true
		}
	}
	return libRow{}, false
}

// libStackLine is a stack's row for w cells: its name, the switch with the
// showing word lit, and the showing file's size. Too narrow for the switch, it
// is the showing file's own row, and v still switches.
func libStackLine(r libRow, w int, selected bool) string {
	size := fmt.Sprintf("%*s", libSizeCols, sizeWords(r.f.Size))
	from, _, ok := libSwitchSpan(w)
	if !ok {
		l := "  " + fit(oneLine(r.f.Name), max(w-libSizeCols-4, 4)) + "  " + size
		if selected {
			return th().Sel.Render(fit(l, w))
		}
		return fit(l, w) + reset
	}
	plain, lit, dim := th().Sel, th().Sel.Bold(true), th().Sel.Faint(true)
	if !selected {
		plain, lit, dim = th().Plain, th().Accent, th().Faint
	}
	words := lit.Render(r.stack.words[0]) + plain.Render(" ") + dim.Render(r.stack.words[1])
	if r.shownWord() != r.stack.words[0] {
		words = dim.Render(r.stack.words[0]) + plain.Render(" ") + lit.Render(r.stack.words[1])
	}
	pad := libSwitchCols - len(r.stack.words[0]) - 1 - len(r.stack.words[1])
	line := plain.Render("  "+fit(oneLine(r.stack.title), from-4)+"  ") + words +
		plain.Render(strings.Repeat(" ", pad)+"  "+size)
	if rest := w - ansi.StringWidth(line); rest > 0 {
		line += plain.Render(strings.Repeat(" ", rest))
	}
	return line + reset
}
