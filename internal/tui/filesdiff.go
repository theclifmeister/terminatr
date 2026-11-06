package tui

import (
	"fmt"
	"strings"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A file's changes in the Files view's viewer: the whole file, as without
// them, with what was added and what was removed since the base among its
// lines, opening at the first change. files.diff says where; the lines
// added are the text the viewer read. Colour isn't the only sign: each
// changed line has its mark at its start.

// diffLeadIn is how many lines show above the first change it opens at.
const diffLeadIn = 3

// Kinds of a diffLine.
const (
	lineSame = iota
	lineAdded
	lineRemoved
)

// diffLine is a line of a file with its changes.
type diffLine struct {
	kind int
	text string
}

// fileDiff is what the server says changed in a file. A server that can't
// say answers no change: the text shows as it is.
func fileDiff(paths tmclient.Paths, session, path string, ignored bool) proto.FilesDiffResult {
	var res proto.FilesDiffResult
	if filesCall(paths, proto.MethodFilesDiff, proto.FilesDiffParams{Session: session, Path: path, Ignored: ignored}, &res) != nil {
		return proto.FilesDiffResult{}
	}
	return res
}

// mergeDiff lays d's hunks over the file's text: its lines in order, the
// removed ones standing where they went. A hunk past the text's end (it
// changed since it was read) marks what there is.
func mergeDiff(body string, d proto.FilesDiffResult) []diffLine {
	var lines []string
	if body != "" {
		lines = strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	}
	out := make([]diffLine, 0, len(lines))
	if d.Status == proto.FileAdded {
		for _, l := range lines {
			out = append(out, diffLine{lineAdded, l})
		}
		return out
	}
	at := 0
	same := func(to int) {
		for ; at < min(to, len(lines)); at++ {
			out = append(out, diffLine{lineSame, lines[at]})
		}
	}
	for _, h := range d.Hunks {
		same(h.New - 1)
		for _, l := range h.Removed {
			out = append(out, diffLine{lineRemoved, l})
		}
		for end := min(h.New-1+h.Added, len(lines)); at < end; at++ {
			out = append(out, diffLine{lineAdded, lines[at]})
		}
	}
	same(len(lines))
	return out
}

// diffRows are a file's lines with their changes, w cells wide, and which
// row the first change is on.
func diffRows(lines []diffLine, w int) ([]string, int) {
	out := make([]string, 0, len(lines))
	first := -1
	for _, l := range lines {
		mark := "  "
		look := th().Plain
		switch l.kind {
		case lineAdded:
			mark, look = "+ ", th().Good
		case lineRemoved:
			mark, look = "− ", th().Bad
		}
		if l.kind != lineSame && first < 0 {
			first = len(out)
		}
		for i, part := range hardLines(l.text, w-2) {
			row := "  " + part
			if i == 0 {
				row = mark + part
			}
			if l.kind != lineSame {
				row = look.Render(row)
			}
			out = append(out, row)
		}
	}
	return out, max(first, 0)
}

// changeWords says a file's change in the viewer's title.
func changeWords(d proto.FilesDiffResult) string {
	var parts []string
	switch d.Status {
	case proto.FileAdded:
		parts = append(parts, "new")
	case proto.FileDeleted:
		parts = append(parts, "deleted")
	case proto.FileModified:
		if d.From != "" {
			parts = append(parts, "renamed from "+oneLine(d.From))
		} else if d.Added == 0 && d.Deleted == 0 {
			parts = append(parts, "changed")
		}
	}
	switch {
	case d.Status == proto.FileAdded && d.Added > 0:
		parts = append(parts, fmt.Sprintf("+%d", d.Added))
	case d.Status == proto.FileDeleted && d.Deleted > 0:
		parts = append(parts, fmt.Sprintf("−%d", d.Deleted))
	case d.Added > 0 || d.Deleted > 0:
		parts = append(parts, fmt.Sprintf("+%d −%d", d.Added, d.Deleted))
	}
	words := strings.Join(parts, " ")
	if d.Base != "" {
		words += " since " + oneLine(d.Base)
	}
	return words
}

// mark lays d over the text read. No change leaves the text as it was.
func (tv *textView) mark(d proto.FilesDiffResult) {
	if d.Status == "" {
		return
	}
	tv.title += " · " + changeWords(d)
	tv.markLines(d)
}

// markLines lays d's lines over the text read, and says what doesn't show.
func (tv *textView) markLines(d proto.FilesDiffResult) {
	switch {
	case d.TooBig:
		tv.addNote("too big to mark its changes")
		return
	case d.Truncated:
		tv.addNote("more changes than are marked here")
	}
	if d.Status == proto.FileAdded || len(d.Hunks) > 0 {
		tv.diff, tv.seek = mergeDiff(tv.body, d), true
		if len(tv.diff) == 0 {
			tv.diff = nil // a new file with nothing in it
		}
	}
}

// addNote adds to the line under the text.
func (tv *textView) addNote(s string) {
	if tv.note != "" {
		s = tv.note + " · " + s
	}
	tv.note = s
}

// deletedViewer fills tv with what deleted file e held, as far as the
// server shows it.
func deletedViewer(tv *textView, paths tmclient.Paths, session string, e proto.FileEntry, ignored bool) {
	var d proto.FilesDiffResult
	if err := filesCall(paths, proto.MethodFilesDiff, proto.FilesDiffParams{Session: session, Path: e.Path, Ignored: ignored}, &d); err != nil {
		msg, _ := filesErrText(err)
		tv.note = "deleted, and can't read what it held: " + oneLine(msg)
		return
	}
	tv.title += " · " + sizeWords(d.Size) + " · " + changeWords(d)
	switch {
	case d.Binary:
		tv.note = "not text: only its name and size show"
	case d.TooBig:
		tv.note = "too big to show what it held"
	default:
		tv.note = "deleted from the worktree: this is what it held"
		tv.markLines(d)
		if c := fileCaution(e); c != "" {
			tv.title += " · " + c
		}
	}
}
