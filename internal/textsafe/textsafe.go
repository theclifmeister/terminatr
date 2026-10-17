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
	"fmt"
	"strconv"
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

// Text is Clean with lines that keeps tabs too, for a file's or a
// message's text, whose indent they may be.
func Text(s string) string {
	if !strings.Contains(s, "\t") {
		return Clean(s, true)
	}
	parts := strings.Split(s, "\t")
	for i, p := range parts {
		parts[i] = Clean(p, true)
	}
	return strings.Join(parts, "\t")
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

// Escape is s as one line with nothing hidden: each character Clean
// would drop or blank written out as Go would quote it (\n, \x1b,
// \u202e), and a backslash doubled so an escape can't be forged; for
// a log line from text tm didn't write.
func Escape(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return r == '\\' || unsafe(r) }) < 0 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == utf8.RuneError:
			if _, n := utf8.DecodeRuneInString(s[i:]); n == 1 {
				fmt.Fprintf(&b, `\x%02x`, s[i])
			} else {
				b.WriteString(`\ufffd`)
			}
		case unsafe(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Clip is s cut to at most n runes, the last one an ellipsis when cut.
func Clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Cut is s cut to at most n bytes on a character's edge, with an
// ellipsis when cut.
func Cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// Safe reports whether s is one line with nothing Clean would change.
func Safe(s string) bool { return strings.IndexFunc(s, unsafe) < 0 }

// Hidden is the first character of s that Clean would drop or blank
// other than the white space a text file holds (a newline, a tab, a
// carriage return), and its byte offset; -1 when there is none. Text an
// agent reads is refused with one: it shows as nothing to the human and
// reaches the agent whole.
func Hidden(s string) (rune, int) {
	for i, r := range s {
		if unsafe(r) && r != '\n' && r != '\t' && r != '\r' {
			if r == utf8.RuneError {
				_, n := utf8.DecodeRuneInString(s[i:])
				if n == 1 {
					return rune(s[i]), i
				}
			}
			return r, i
		}
	}
	return 0, -1
}
