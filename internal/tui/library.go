package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/thumb"
)

// The project popup's Library tab (docs/SPEC.md §4): the files every
// thread of the project attached to its reports, the resolved and
// archived threads' too, grouped by task and thread, newest first, and a
// group per linked project for the files it sent (§16.13). enter reads
// one in the popup, o opens one in this computer's viewer, an image shows
// in half-blocks (imagepreview.go), p opens a page's published copy; X deletes the selected file and D the
// files of its thread, each after y; / filters and t steps through the
// kinds (libraryfilter.go). A group's heading says its task's status, kinds
// and size, and a long group folds (libraryheads.go). Two images that are one
// picture twice list as one row, and v switches it (librarystack.go). A file
// shows by its own title, when it has one, with its name beside it
// (libFileLabel) and its size, never a path.

// libraryOrder are the library's files grouped by thread (or linked
// project), the seat's own group first, then the group with the newest
// file first and each group's files newest first. A program's list keeps
// its projects in the order they came, each ordered so.
func libraryOrder(files []thread.LibFile) []thread.LibFile {
	var ids []string
	by := map[string][]thread.LibFile{}
	places := map[string]int{}
	for _, f := range files { // newest first already
		if _, ok := by[f.Group()]; !ok {
			ids = append(ids, f.Group())
		}
		by[f.Group()] = append(by[f.Group()], f)
		if _, ok := places[libPlace(f)]; !ok {
			places[libPlace(f)] = len(places)
		}
	}
	slices.SortStableFunc(ids, func(a, b string) int {
		fa, fb := by[a][0], by[b][0]
		if d := places[libPlace(fa)] - places[libPlace(fb)]; d != 0 {
			return d
		}
		switch {
		case fa.IsSeat() && !fb.IsSeat():
			return -1
		case fb.IsSeat() && !fa.IsSeat():
			return 1
		}
		return 0
	})
	out := make([]thread.LibFile, 0, len(files))
	for _, id := range ids {
		out = append(out, by[id]...)
	}
	return out
}

// libFiles are the Library tab's files the filter and kind leave, in the
// order they are listed.
func (pv *projectView) libFiles() []thread.LibFile {
	return libraryOrder(libShown(pv.library, pv.libFilter, pv.libPick))
}

// narrowLib sets the filter and the kind; the selection stays on its file
// while that still shows, else it goes to the first. A stack the filter leaves
// one file of is that file's row.
func (pv *projectView) narrowLib(query, pick string) {
	at, ok := pv.selLib()
	pv.libFilter, pv.libPick = query, pick
	pv.sel[tabLibrary] = 0
	if ok {
		same := func(f thread.LibFile) bool { return f.Group() == at.Group() && f.Name == at.Name }
		if slices.IndexFunc(pv.libFiles(), same) >= 0 && pv.libRowOf(same) < 0 {
			pv.setLibOpen(at.Group(), true) // the file is under its group's fold
		}
		pv.sel[tabLibrary] = max(pv.libRowOf(same), 0)
	}
}

// libraryTyping: the keyboard is in the filter. Enter keeps the words,
// esc drops them.
func (pv *projectView) libraryTyping(k tea.KeyPressMsg) {
	switch k.String() {
	case "enter":
		pv.libTyping = false
	case "esc":
		pv.libTyping = false
		pv.narrowLib("", pv.libPick)
	case "backspace":
		if r := []rune(pv.libFilter); len(r) > 0 {
			pv.narrowLib(string(r[:len(r)-1]), pv.libPick)
		}
	default:
		if t := textsafe.Clean(k.Text, false); t != "" && len(pv.libFilter)+len(t) <= proto.MaxFilesQuery {
			pv.narrowLib(pv.libFilter+t, pv.libPick)
		}
	}
}

// libFilterHit and libChipHit are clicks on the Library's head: the
// filter's line, and a line of chips (libChipHit less the line; the
// column picks the chip).
const (
	libFilterHit = -3
	libChipHit   = -4
)

// libChipLine is a line of chips: where each one starts and ends.
type libChipLine struct {
	text  string
	spans []libChipSpan
}

type libChipSpan struct {
	from, to int
	kind     string
}

// libChipLines are the chips laid out on lines of w cells, the picked one
// marked: every kind stays in reach at any width.
func (pv *projectView) libChipLines(w int) []libChipLine {
	return libChipLinesOf(pv.library, pv.libFilter, pv.libPick, w)
}

// libChipLinesOf are files' chips under the filter and the kind, laid out
// on lines of w cells.
func libChipLinesOf(files []thread.LibFile, query, kind string, w int) []libChipLine {
	chips := libChips(files, query, kind)
	if chips == nil {
		return nil
	}
	pick := libPicked(files, kind)
	var out []libChipLine
	cur := libChipLine{}
	x := 0
	add := func(word, styled string, kind string, chip bool) {
		n := ansi.StringWidth(word)
		if x > 0 && x+n > w {
			out = append(out, cur)
			cur, x = libChipLine{}, 0
		}
		cur.text += styled
		if chip {
			cur.spans = append(cur.spans, libChipSpan{x, x + n, kind})
		}
		x += n
		if chip {
			cur.text += "  "
			x += 2
		}
	}
	for _, c := range chips {
		word := c.kind
		if word == "" {
			word = "all"
		}
		word = fmt.Sprintf(" %s %d ", word, c.n)
		if c.kind == pick {
			add(word, th().Sel.Render(word), c.kind, true)
		} else {
			add(word, word, c.kind, true)
		}
	}
	add("t kinds", th().Faint.Render("t kinds"), "", false)
	return append(out, cur)
}

// libraryHead are the lines above the list, which stay when it scrolls:
// the chips and the filter, when they show; and what a click on each picks.
func (pv *projectView) libraryHead(w int) ([]string, []int) {
	if !pv.libOK || pv.libErr != nil {
		return nil, nil
	}
	var head []string
	var hits []int
	for i, l := range pv.libChipLines(w) {
		head, hits = append(head, l.text), append(hits, libChipHit-i)
	}
	switch {
	case pv.libTyping:
		head = append(head, th().Accent.Render("/")+" "+oneLine(pv.libFilter)+th().Accent.Render("▏"))
	case pv.libFilter != "":
		n := len(libShown(pv.library, pv.libFilter, ""))
		head = append(head, th().Faint.Render("/ ")+oneLine(pv.libFilter)+th().Faint.Render(fmt.Sprintf(" · %d of %d · esc clears", n, len(pv.library))))
	case libShowsFilter(pv.library, pv.libFilter):
		head = append(head, th().Faint.Render("/ filter"))
	}
	if len(head) > len(hits) {
		hits = append(hits, libFilterHit)
	}
	if len(head) > 0 {
		head, hits = append(head, ""), append(hits, noHit)
	}
	return head, hits
}

// clickLibChip picks the chip at column col of the head's chip line.
func (pv *projectView) clickLibChip(w, line, col int) {
	ls := pv.libChipLines(w)
	if line >= len(ls) {
		return
	}
	for _, s := range ls[line].spans {
		if col >= s.from && col < s.to {
			pv.narrowLib(pv.libFilter, s.kind)
		}
	}
}

// selLib is the selected file, false for none (or a fold's row).
func (pv *projectView) selLib() (thread.LibFile, bool) {
	rows := pv.libRows()
	if i := pv.sel[tabLibrary]; i < len(rows) && rows[i].isFile() {
		return rows[i].f, true
	}
	return thread.LibFile{}, false
}

// libGroup is the title line of a thread's files: task, title, thread
// and the date of its newest file; of a linked project's, the project
// and how many (n); of a seat's own, whose they are and how many.
func libGroup(f thread.LibFile, n, w int) string {
	if f.Peer != "" || f.IsSeat() {
		rest := fmt.Sprintf(" · %d files", n)
		if n == 1 {
			rest = " · 1 file"
		}
		head := "from " + f.Peer
		if f.IsSeat() {
			head = oneLine(f.Title)
		}
		return th().Head.Render(ansi.Truncate(head, max(w-ansi.StringWidth(rest), 8), "…")) + th().Faint.Render(rest)
	}
	rest := " · " + f.Thread + " · " + f.Time.Local().Format("2006-01-02")
	if f.State == "archived" {
		rest += " · archived"
	}
	head := joinSp(f.Task, oneLine(f.Title))
	return th().Head.Render(ansi.Truncate(head, max(w-ansi.StringWidth(rest), 8), "…")) + th().Faint.Render(rest)
}

// sizeWords is a byte count in words: 812 B, 3.4 KB, 1.2 MB, 1.44 GB.
func sizeWords(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1f KB", float64(n)/1000)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1f MB", float64(n)/1_000_000)
	}
	return fmt.Sprintf("%.2f GB", float64(n)/1_000_000_000)
}

// libraryLines are the tab's lines: its sel line and each line's file.
func (pv *projectView) libraryLines(w int) ([]string, int, []int) {
	switch {
	case pv.libErr != nil:
		return []string{th().Bad.Render("error: " + oneLine(pv.libErr.Error()))}, -1, nil
	case !pv.libOK:
		return []string{th().Faint.Render("loading…")}, -1, nil
	}
	if len(pv.library) == 0 {
		return []string{th().Faint.Render("no files: a thread's report attaches them (tm report --attach), the coordinator keeps its own (tm library add)")}, -1, nil
	}
	groups := pv.libShownGroups()
	if len(groups) == 0 {
		return []string{th().Faint.Render("nothing matches: esc clears")}, -1, nil
	}
	var out []string
	var hits []int
	sel := -1
	i := 0
	for _, g := range groups {
		if len(out) > 0 {
			out, hits = append(out, ""), append(hits, noHit)
		}
		title, meta := libHeading(g.files, w)
		out, hits = append(out, title, meta), append(hits, noHit, noHit)
		for _, r := range g.rows {
			l := "  " + r.word()
			if r.isFile() {
				size := fmt.Sprintf("%*s", libSizeCols, sizeWords(r.f.Size))
				room := max(w-len(size)-4, 4)
				l = "  " + fit(libFileLabel(r.f, room, i != pv.sel[tabLibrary]), room) + "  " + size
			}
			switch {
			case r.stack != nil:
				if i == pv.sel[tabLibrary] {
					sel = len(out)
				}
				l = libStackLine(r, w, i == pv.sel[tabLibrary])
			case i == pv.sel[tabLibrary]:
				sel = len(out)
				l = th().Sel.Render(fit(l, w))
			case r.isFile():
				l = fit(l, w) + reset
			default:
				l = th().Faint.Render(fit(l, w))
			}
			out, hits = append(out, l), append(hits, i)
			i++
		}
	}
	return out, sel, hits
}

// libraryKey: / filters and t steps through the kinds; v switches a stack;
// enter reads the selected file, X deletes it and D its thread's files, each
// after y.
func (pv *projectView) libraryKey(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "/":
		pv.libTyping = libShowsFilter(pv.library, pv.libFilter)
		return nil
	case "t":
		if chips := libChips(pv.library, pv.libFilter, pv.libPick); chips != nil {
			pv.narrowLib(pv.libFilter, libNextKind(chips, libPicked(pv.library, pv.libPick)))
		}
		return nil
	case "v":
		if r, ok := pv.libStackRow(); ok {
			pv.switchLib(r)
		}
		return nil
	}
	if k.String() == "enter" && pv.toggleLibFold() {
		return nil
	}
	f, ok := pv.selLib()
	if !ok {
		return nil
	}
	switch k.String() {
	case "enter":
		return m.openLibFile(pv, f)
	case "o":
		return m.openLibCopy(pv.slug, libFileID(f), f.Name)
	case "p":
		return m.openLibPublished(f)
	case "X":
		if f.Peer != "" {
			m.confirm("Delete a file", "Delete "+oneLine(f.Name)+" ("+sizeWords(f.Size)+"), which "+f.Peer+" sent with "+f.Message+"? It can't be brought back.", func() tea.Cmd {
				return m.libRemove(pv.slug, f.Message, f.Name, "deleted "+oneLine(f.Name))
			})
			break
		}
		whose := "thread " + f.Thread
		if f.IsSeat() {
			whose = "the coordinator's files"
		}
		m.confirm("Delete a library file", "Delete "+oneLine(f.Name)+" ("+sizeWords(f.Size)+") of "+whose+"? It can't be brought back.", func() tea.Cmd {
			return m.libRemove(pv.slug, f.Thread, f.Name, "deleted "+oneLine(f.Name))
		})
	case "D":
		if f.Peer != "" {
			m.msg = "X deletes a linked project's files one at a time"
			break
		}
		n := 0
		for _, g := range pv.library {
			if g.Thread == f.Thread {
				n++
			}
		}
		whose, short := "thread "+f.Thread, f.Thread
		if f.IsSeat() {
			whose, short = "the coordinator's files", "the coordinator's"
		}
		m.confirm("Delete a thread's library", fmt.Sprintf("Delete all %d library files of %s? They can't be brought back.", n, whose), func() tea.Cmd {
			return m.libRemove(pv.slug, f.Thread, "", fmt.Sprintf("deleted %d files of %s", n, short))
		})
	}
	return nil
}

func (m *dash) libRemove(slug, id, name, msg string) tea.Cmd {
	src := m.src
	// The action reloads the popup, library included.
	return m.act(func() actionMsg {
		if _, err := src.LibraryRemove(slug, id, name); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: msg}
	})
}

// libMax is how much of a file the viewer reads.
const libMax = 1 << 20

// textView is read-only text over the popup, scrollable; it opens at
// its end when bottom is set (a log). esc closes it, and q too when
// quit is set.
type textView struct {
	title, body, note string
	scroll            int
	bottom, quit      bool

	// wrapped is body hard-wrapped at wrapW (for wrapBody): wrapping a
	// large file is done once per width, not on every key and frame.
	wrapped []string
	wrapW   int
	wrapOf  string

	// diff is body with a file's changes laid over it (filesdiff.go), nil
	// for plain text. changeAt is the first change's wrapped line, and seek
	// says the viewer hasn't opened there yet.
	diff     []diffLine
	changeAt int
	seek     bool

	// image is an image's cells in place of text (imagepreview.go), and
	// open, on a file o opens, opens it in this computer's viewer.
	image *imagePreview
	open  func(m *dash) tea.Cmd

	// switchTo, on a stack's file, reads its other file instead, and
	// switchWord says which that is.
	switchTo   func(m *dash) tea.Cmd
	switchWord string

	// keep, on a paired server's file, looks whether it may still show: ""
	// while it may, else the words to put in its place. kept is when it
	// last looked. Nil for all else (viewerWatch).
	keep func() string
	kept time.Time
}

// viewerGoneMsg says tv's file may no longer show.
type viewerGoneMsg struct {
	tv    *textView
	words string
}

// viewerWatch looks, on the dashboard's tick, whether the file on top may
// still show; nil when nothing is to be looked at yet.
func (m *dash) viewerWatch() tea.Cmd {
	tv, ok := m.top().(*textView)
	if !ok || tv.keep == nil || time.Since(tv.kept) < filesFarEvery {
		return nil
	}
	tv.kept = time.Now()
	keep := tv.keep
	return func() tea.Msg {
		if words := keep(); words != "" {
			return viewerGoneMsg{tv: tv, words: words}
		}
		return nil
	}
}

// goes empties the viewer: what showed left, and why stays.
func (tv *textView) goes(words string) {
	tv.body, tv.note, tv.diff, tv.wrapped, tv.wrapOf, tv.wrapW, tv.keep, tv.scroll = "", words, nil, nil, "", 0, nil, 0
	tv.title, _, _ = strings.Cut(tv.title, " · ")
}

// scrollKey scrolls *scroll for key k over n lines; it says whether k
// was a scroll key.
func scrollKey(scroll *int, k tea.KeyPressMsg, n int) bool {
	d, ok := scrollKeys[k.String()]
	if ok {
		*scroll = clampScroll(*scroll+d, n)
	}
	return ok
}

func (m *dash) openLibFile(pv *projectView, f thread.LibFile) tea.Cmd {
	tv := &textView{title: oneLine(f.Name) + " · " + sizeWords(f.Size)}
	if f.DocTitle != "" {
		tv.title = oneLine(f.DocTitle) + " · " + tv.title
	}
	id := libFileID(f)
	if f.Peer != "" {
		// Another project's: data, shown as its cells, as text or not at all.
		tv.title += " · from " + f.Peer
	}
	tv.open = func(m *dash) tea.Cmd { return m.openLibCopy(pv.slug, id, f.Name) }
	if r, ok := pv.libStackOf(f); ok {
		// v switches here too: comparing is what a stack is for.
		tv.switchWord = r.otherWord()
		tv.switchTo = func(m *dash) tea.Cmd {
			pv.switchLib(r)
			m.pop()
			return m.openLibFile(pv, r.otherFile())
		}
	}
	if previewable() && thumb.Supported(f.Name) {
		key := thumbKey(f)
		img, known := pv.thumbs[key]
		if !known {
			img = fetchThumb(m.src, pv.slug, id, f.Name)
			pv.keepThumb(key, img)
		}
		if img != nil {
			tv.image = img
			m.push(tv)
			return nil
		}
	}
	data, truncated, err := m.src.LibraryRead(pv.slug, id, f.Name, libMax)
	switch {
	case err != nil:
		tv.note = "can't read it: " + oneLine(err.Error())
	default:
		tv.body, tv.note = libText(f.Name, data, truncated)
	}
	m.push(tv)
	return nil
}

// openLibPublished opens page f's published copy in this computer's
// browser; a file with none says so.
func (m *dash) openLibPublished(f thread.LibFile) tea.Cmd {
	if f.Published == "" {
		m.msg = oneLine(f.Name) + " has no published page"
		return nil
	}
	url := f.Published
	return m.act(func() actionMsg {
		if err := openURL(url); err != nil {
			return actionMsg{err: errors.New("the browser didn't start: " + err.Error())}
		}
		return actionMsg{msg: "opened the published page in this computer's browser"}
	})
}

// openLinkFile shows the file that came with message msg of project
// slug's link to other, as the Library tab shows one.
func (m *dash) openLinkFile(slug, other string, msg *link.Message) tea.Cmd {
	tv := &textView{title: oneLine(msg.File) + " · " + link.MB(msg.FileSize) + " · from " + other}
	tv.open = func(m *dash) tea.Cmd { return m.openLibCopy(slug, msg.ID, msg.File) }
	if previewable() && thumb.Supported(msg.File) {
		if img := fetchThumb(m.src, slug, msg.ID, msg.File); img != nil {
			tv.image = img
			m.push(tv)
			return nil
		}
	}
	data, truncated, err := m.src.LibraryRead(slug, msg.ID, msg.File, libMax)
	if err != nil {
		tv.note = "can't read it: " + oneLine(err.Error())
	} else {
		tv.body, tv.note = libText(msg.File, data, truncated)
	}
	m.push(tv)
	return nil
}

// libText is how a file shows: text as it is, JSON indented, anything
// else not at all (its note says so). Control characters never reach the
// terminal.
func libText(name string, data []byte, truncated bool) (string, string) {
	if bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 || !utf8.Valid(trimPartial(data)) {
		return "", "not text: only its name and size show"
	}
	text := string(data)
	if strings.EqualFold(filepath.Ext(name), ".json") && !truncated {
		var buf bytes.Buffer
		if json.Indent(&buf, data, "", "  ") == nil {
			text = buf.String()
		}
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\t", "    ")
	note := ""
	if truncated {
		note = "shows the first " + sizeWords(libMax)
	}
	return strings.Map(func(r rune) rune {
		if r != '\n' && (r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, text), note
}

// trimPartial drops a multi-byte character a cut ended in the middle of.
func trimPartial(b []byte) []byte {
	for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
		if r, n := utf8.DecodeLastRune(b); r == utf8.RuneError && n <= 1 {
			b = b[:len(b)-1]
		} else {
			break
		}
	}
	return b
}

// hardLines are text's lines as a read-only popup shows them: tabs as
// four spaces, each line cut to w cells (10 at least), trailing newlines
// dropped.
func hardLines(text string, w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		out = append(out, strings.Split(ansi.Hardwrap(strings.ReplaceAll(l, "\t", "    "), max(w, 10), true), "\n")...)
	}
	return out
}

// wrapBody is the body's lines hard-wrapped for w cells (hardLines),
// kept for as long as w and the body stay the same. The slice is
// shared: callers copy before changing it.
func (tv *textView) wrapBody(w int) []string {
	w = max(w, 10)
	if tv.wrapped != nil && tv.wrapW == w && tv.wrapOf == tv.body {
		return tv.wrapped
	}
	out := []string{}
	switch {
	case tv.diff != nil:
		out, tv.changeAt = diffRows(tv.diff, w)
	case tv.body != "":
		out = hardLines(tv.body, w)
	}
	tv.wrapped, tv.wrapW, tv.wrapOf = out, w, tv.body
	return out
}

func (tv *textView) lines(w int) []string {
	out := tv.wrapBody(w)
	out = out[:len(out):len(out)] // an append below must not write into the cache
	if tv.note != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, th().Faint.Render(tv.note))
	}
	return out
}

func (tv *textView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if scrollKey(&tv.scroll, k, len(tv.lines(m.inner(viewWidth)))) {
		tv.bottom = false
		return nil
	}
	s := k.String()
	if s == "o" && tv.open != nil {
		return tv.open(m)
	}
	if s == "v" && tv.switchTo != nil {
		return tv.switchTo(m)
	}
	if s == "esc" || s == "q" && tv.quit {
		m.pop()
	}
	return nil
}

func (tv *textView) wheel(m *dash, d int) { tv.key(m, arrow(d)) }

// keyHints are the viewer's action row.
func (tv *textView) keyHints() string {
	keys := "↑ ↓ scroll · esc back"
	if tv.image != nil {
		keys = "esc back"
	}
	if tv.open != nil {
		keys = "o open · " + keys
	}
	if tv.switchTo != nil {
		keys = "v " + tv.switchWord + " · " + keys
	}
	return keys
}

func (tv *textView) render(m *dash) string {
	if tv.image != nil {
		b := box{title: tv.title, sel: -1, keys: tv.keyHints()}
		b.body = tv.image.lines(m.inner(viewWidth), m.boxRows(b))
		return m.popup(b)
	}
	lines := tv.lines(m.inner(viewWidth))
	if tv.seek {
		tv.seek = false
		tv.scroll = clampScroll(tv.changeAt-diffLeadIn, len(lines))
	}
	b := box{title: tv.title, body: lines, sel: -1, scroll: tv.scroll, keys: tv.keyHints()}
	if tv.bottom {
		// From the end: as many lines as the box shows.
		tv.scroll = max(len(lines)-m.boxRows(b), 0)
		b.scroll = tv.scroll
	}
	return m.popup(b)
}
