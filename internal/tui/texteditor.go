package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// textEditor edits a text of many lines over the popup (a guide's
// body): typing and pasting go in at the cursor, the arrows, home and
// end move it, enter breaks the line, ctrl+s saves, and esc leaves the
// text as it was, asking first when it changed. Control characters
// never go in; tabs and newlines do.
type textEditor struct {
	title, label string
	text         []rune
	pos          int
	orig         string
	// max is the most bytes it takes.
	max  int
	save func(string) tea.Cmd
}

// editText opens the editor on text, saving with save.
func (m *dash) editText(title, label, text string, max int, save func(string) tea.Cmd) {
	m.push(&textEditor{title: title, label: label, text: []rune(text), pos: len([]rune(text)), orig: text, max: max, save: save})
}

func (ed *textEditor) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		if string(ed.text) == ed.orig {
			m.pop()
			m.msg = "unchanged"
			return nil
		}
		m.confirmNo(ed.title, "Leave without saving? What you changed is lost.", "still editing", func() tea.Cmd {
			m.close(ed)
			m.msg = "unchanged"
			return nil
		})
	case "ctrl+s":
		m.pop()
		return ed.save(string(ed.text))
	case "enter":
		ed.insert("\n")
	case "tab":
		ed.insert("\t")
	case "backspace":
		if ed.pos > 0 {
			ed.text = append(ed.text[:ed.pos-1], ed.text[ed.pos:]...)
			ed.pos--
		}
	case "delete":
		if ed.pos < len(ed.text) {
			ed.text = append(ed.text[:ed.pos], ed.text[ed.pos+1:]...)
		}
	case "left":
		ed.pos = max(ed.pos-1, 0)
	case "right":
		ed.pos = min(ed.pos+1, len(ed.text))
	case "home", "ctrl+a":
		ed.pos = ed.lineStart(ed.pos)
	case "end", "ctrl+e":
		ed.pos = ed.lineEnd(ed.pos)
	case "up":
		ed.vertical(-1)
	case "down":
		ed.vertical(1)
	case "pgup":
		for range 10 {
			ed.vertical(-1)
		}
	case "pgdown":
		for range 10 {
			ed.vertical(1)
		}
	default:
		ed.insert(k.Text)
	}
	return nil
}

// paste puts pasted text in at the cursor.
func (ed *textEditor) paste(_ *dash, text string) { ed.insert(text) }

// insert puts s in at the cursor, its line ends as newlines and without
// control characters, as far as max allows.
func (ed *textEditor) insert(s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	r := []rune(textsafe.Text(s))
	room := ed.max - len(string(ed.text))
	for ed.max > 0 && len(r) > 0 && len(string(r)) > room {
		r = r[:len(r)-1]
	}
	if len(r) == 0 {
		return
	}
	ed.text = append(ed.text[:ed.pos], append(r, ed.text[ed.pos:]...)...)
	ed.pos += len(r)
}

// lineStart and lineEnd are where the line holding p starts and ends.
func (ed *textEditor) lineStart(p int) int {
	for p > 0 && ed.text[p-1] != '\n' {
		p--
	}
	return p
}

func (ed *textEditor) lineEnd(p int) int {
	for p < len(ed.text) && ed.text[p] != '\n' {
		p++
	}
	return p
}

// vertical moves the cursor d lines up or down, keeping its column where
// the line is long enough.
func (ed *textEditor) vertical(d int) {
	start := ed.lineStart(ed.pos)
	col := ed.pos - start
	switch {
	case d < 0 && start == 0:
		ed.pos = 0
	case d < 0:
		prev := ed.lineStart(start - 1)
		ed.pos = min(prev+col, start-1)
	default:
		end := ed.lineEnd(ed.pos)
		if end == len(ed.text) {
			ed.pos = end
			return
		}
		ed.pos = min(end+1+col, ed.lineEnd(end+1))
	}
}

func (ed *textEditor) render(m *dash) string {
	w := m.inner(viewWidth)
	body := wrapLines(ed.label, w)
	body = append(body, "")
	sel := len(body)
	// Each line of the text, broken at w; the cursor a block.
	line, n := []rune{}, 0
	flush := func() {
		body = append(body, string(line))
		line, n = line[:0:0], 0
	}
	for i := 0; i <= len(ed.text); i++ {
		if i == ed.pos {
			sel = len(body)
			line = append(line, '█')
			n++
		}
		if i == len(ed.text) {
			break
		}
		r := ed.text[i]
		if r == '\n' {
			flush()
			continue
		}
		if n >= w {
			flush()
		}
		if r == '\t' {
			line = append(line, []rune("    ")...)
			n += 4
			continue
		}
		line = append(line, r)
		n++
	}
	flush()
	if size := len(string(ed.text)); ed.max > 0 && size*10 >= ed.max*8 {
		body = append(body, th().Faint.Render(fmt.Sprintf("%d of at most %d bytes", size, ed.max)))
	}
	return m.popup(box{title: ed.title, body: body, sel: sel, keys: "ctrl+s save · enter new line · ↑ ↓ ← → move · esc cancel"})
}
