package textsafe

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	for _, c := range []struct{ in, one, lines string }{
		{"plain text", "plain text", "plain text"},
		{"a\nb", "a b", "a\nb"},
		{"a\tb\vc\fd", "a b c d", "a b c d"},
		{"a\r\nb", "a  b", "a \nb"},
		{"a\u0085b", "a b", "a b"},
		// escapes and other controls are dropped
		{"\x1b[31mred\x1b[0m", "[31mred[0m", "[31mred[0m"},
		{"a\x00b\x7fc\u009bd", "abcd", "abcd"},
		// bidi overrides, isolates and marks
		{"abc\u202edcba", "abcdcba", "abcdcba"},
		{"\u2066x\u2069\u200e\u200f\u061c", "x", "x"},
		// zero-width characters, the soft hyphen and the BOM
		{"a\u200bb\u200cc\u200dd\u2060e\u00adf\ufeffg", "abcdefg", "abcdefg"},
		// tag characters
		{"x\U000e0041\U000e007fy", "xy", "xy"},
		// line and paragraph separators are white space
		{"a\u2028b\u2029c", "a b c", "a b c"},
		// not UTF-8, and the replacement character itself
		{"a\xffb\ufffdc", "abc", "abc"},
		// wide text is text
		{"日本 ✓ é", "日本 ✓ é", "日本 ✓ é"},
	} {
		if got := Clean(c.in, false); got != c.one {
			t.Errorf("Clean(%q, false) = %q, want %q", c.in, got, c.one)
		}
		if got := Clean(c.in, true); got != c.lines {
			t.Errorf("Clean(%q, true) = %q, want %q", c.in, got, c.lines)
		}
		if Safe(c.in) != (c.in == c.one) {
			t.Errorf("Safe(%q) = %v", c.in, Safe(c.in))
		}
	}
}

func TestLine(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"  one\n\ttwo   three ", 80, "one two three"},
		{"abc\u202edef", 80, "abcdef"},
		{"a\u200b\u200bb", 80, "ab"},
		{"a\u2028\u2029b", 80, "a b"},
		{"abcdefghij", 5, "abcd…"},
		{"abc   defgh", 5, "abc…"},
		{strings.Repeat("é", 10), 10, strings.Repeat("é", 10)},
		{"x", 0, "x"},
	} {
		if got := Line(c.in, c.n); got != c.want {
			t.Errorf("Line(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestHidden(t *testing.T) {
	for _, c := range []struct {
		in string
		r  rune
		at int
	}{
		{"plain\ttext\r\nlines\n", 0, -1},
		{"日本 ✓ é", 0, -1},
		{"a\u200bb", '\u200b', 1},
		{"ab\U000e0049", '\U000e0049', 2},
		{"a\u202eb", '\u202e', 1},
		{"\x1b[31m", '\x1b', 0},
		{"a\xffb", 0xff, 1},
		{"a\ufeff", '\ufeff', 1},
	} {
		if r, at := Hidden(c.in); r != c.r || at != c.at {
			t.Errorf("Hidden(%q) = %q, %d, want %q, %d", c.in, r, at, c.r, c.at)
		}
	}
}

func TestEscape(t *testing.T) {
	for in, want := range map[string]string{
		"plain café":                    "plain café",
		"a\nb":                          `a\nb`,
		"\x1b]52;c;eA==\x07":            `\x1b]52;c;eA==\a`,
		"x\rhidden":                     `x\rhidden`,
		"a\u202eb\u2028c":               `a\u202eb\u2028c`,
		`a\nb`:                          `a\\nb`,
		"bad\xffbyte":                   `bad\xffbyte`,
		"2026-10-15 broker: http.fetch": "2026-10-15 broker: http.fetch",
	} {
		got := Escape(in)
		if got != want {
			t.Errorf("Escape(%q) = %q, want %q", in, got, want)
		}
		if !Safe(got) {
			t.Errorf("Escape(%q) = %q isn't safe", in, got)
		}
	}
}

func TestCut(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 3, "abc"},
		{"abcd", 3, "abc…"},
		{"aé", 2, "a…"},
		{"日本", 4, "日…"},
	} {
		if got := Cut(c.in, c.n); got != c.want {
			t.Errorf("Cut(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestClip(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 3, "abc"},
		{"abcd", 3, "ab…"},
		{"日本語です", 3, "日本…"},
		{"ééé", 3, "ééé"},
	} {
		if got := Clip(c.in, c.n); got != c.want {
			t.Errorf("Clip(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
