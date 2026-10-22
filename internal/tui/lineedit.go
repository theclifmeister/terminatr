package tui

import "unicode"

// lineEdit is an edit field's text and cursor, shared by the edit
// dialogs (inputView) and the text editor (textEditor): the cursor moves
// by a character (← →), a word (alt+← alt+→, or alt+b alt+f) and to its
// line's start or end (home end, ctrl+a ctrl+e); backspace and delete
// take a character, ctrl+w (or alt+backspace) the word before, ctrl+k
// the rest of the line and ctrl+u its start. A line is the whole text
// in a dialog, which has no newlines.
type lineEdit struct {
	text []rune
	pos  int
}

// newLine is a field holding s, its cursor at the end.
func newLine(s string) lineEdit {
	r := []rune(s)
	return lineEdit{text: r, pos: len(r)}
}

func (le *lineEdit) value() string { return string(le.text) }

// edit runs an editing key, reporting whether k is one.
func (le *lineEdit) edit(k string) bool {
	le.pos = min(max(le.pos, 0), len(le.text))
	switch k {
	case "left":
		le.pos = max(le.pos-1, 0)
	case "right":
		le.pos = min(le.pos+1, len(le.text))
	case "alt+left", "alt+b", "ctrl+left":
		le.pos = le.wordStart(le.pos)
	case "alt+right", "alt+f", "ctrl+right":
		le.pos = le.wordEnd(le.pos)
	case "home", "ctrl+a":
		le.pos = le.lineStart(le.pos)
	case "end", "ctrl+e":
		le.pos = le.lineEnd(le.pos)
	case "backspace":
		le.cut(max(le.pos-1, 0), le.pos)
	case "delete":
		le.cut(le.pos, min(le.pos+1, len(le.text)))
	case "ctrl+w", "alt+backspace":
		le.cut(le.wordStart(le.pos), le.pos)
	case "ctrl+k":
		le.cut(le.pos, le.lineEnd(le.pos))
	case "ctrl+u":
		le.cut(le.lineStart(le.pos), le.pos)
	default:
		return false
	}
	return true
}

// insert puts r in at the cursor, leaving the cursor after it.
func (le *lineEdit) insert(r []rune) {
	le.pos = min(max(le.pos, 0), len(le.text))
	le.text = append(le.text[:le.pos:le.pos], append(r, le.text[le.pos:]...)...)
	le.pos += len(r)
}

// cut removes the text from i to j, the cursor going to i.
func (le *lineEdit) cut(i, j int) {
	le.text = append(le.text[:i:i], le.text[j:]...)
	le.pos = i
}

// lineStart and lineEnd are where the line holding p starts and ends.
func (le *lineEdit) lineStart(p int) int {
	for p > 0 && le.text[p-1] != '\n' {
		p--
	}
	return p
}

func (le *lineEdit) lineEnd(p int) int {
	for p < len(le.text) && le.text[p] != '\n' {
		p++
	}
	return p
}

// wordStart is the start of the word before p, wordEnd the end of the
// one after it: a word is letters and digits, so a path or a host name
// goes a part at a time.
func (le *lineEdit) wordStart(p int) int {
	for p > 0 && !isWordRune(le.text[p-1]) {
		p--
	}
	for p > 0 && isWordRune(le.text[p-1]) {
		p--
	}
	return p
}

func (le *lineEdit) wordEnd(p int) int {
	for p < len(le.text) && !isWordRune(le.text[p]) {
		p++
	}
	for p < len(le.text) && isWordRune(le.text[p]) {
		p++
	}
	return p
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// lines lays the text out in lines of at most w cells, a newline
// breaking one, a tab four spaces and the cursor a block before the
// character it is at; cur is the line holding the cursor.
func (le *lineEdit) lines(w int) (lines []string, cur int) {
	w = max(w, 1)
	line, n := []rune{}, 0
	flush := func() {
		lines = append(lines, string(line))
		line, n = line[:0:0], 0
	}
	add := func(r ...rune) {
		if n+len(r) > w && n > 0 {
			flush()
		}
		line = append(line, r...)
		n += len(r)
	}
	for i := 0; i <= len(le.text); i++ {
		if i == le.pos {
			add('█')
			cur = len(lines)
		}
		if i == len(le.text) {
			break
		}
		switch r := le.text[i]; r {
		case '\n':
			flush()
		case '\t':
			add([]rune("    ")...)
		default:
			add(r)
		}
	}
	flush()
	return lines, cur
}
