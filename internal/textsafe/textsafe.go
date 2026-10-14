// Package textsafe is the one sanitizer for text that tm shows or passes
// on but didn't write: a mod's names, titles, notices and inbox items,
// and the like. Such text reaches a terminal, the TUI and agents, so
// whatever could be read as other than plain text goes: control
// characters (escape sequences start with one), Unicode format
// characters (bidi overrides and isolates, zero-width characters, tag
// characters, the soft hyphen), the line and paragraph separators, and
// the replacement character that stands for bytes that weren't UTF-8.
package textsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// unsafe reports whether r goes: Cc, Cf, Zl, Zp or U+FFFD.
func unsafe(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
		r == '\u2028' || r == '\u2029' || r == utf8.RuneError
}

// Clean is s without its unsafe characters: those that are white space
// (a tab, a carriage return, a line or paragraph separator) become a
// space and the rest (an escape, a bidi override, a zero-width joiner)
// are dropped. With lines, a newline stays, for text that breaks into
// lines; without, it is a space too.
func Clean(s string, lines bool) string {
	if strings.IndexFunc(s, func(r rune) bool { return unsafe(r) && !(lines && r == '\n') }) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case lines && r == '\n':
			return r
		case !unsafe(r):
			return r
		case unicode.IsSpace(r): // the separators among them
			return ' '
		}
		return -1
	}, s)
}

// Line is s as one line of at most n runes: Clean, then each run of
// white space one space, trimmed, and a cut ending in an ellipsis.
func Line(s string, n int) string {
	s = strings.Join(strings.Fields(Clean(s, false)), " ")
	if r := []rune(s); n > 0 && len(r) > n {
		s = strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}

// Safe reports whether s is one line with nothing Clean would change.
func Safe(s string) bool { return strings.IndexFunc(s, unsafe) < 0 }
