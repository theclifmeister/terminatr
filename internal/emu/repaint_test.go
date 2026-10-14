//go:build cgo

package emu

import (
	"fmt"
	"strings"
	"testing"
)

// repainted feeds a's Repaint into a fresh terminal of a's size.
func repainted(t *testing.T, a *Terminal, history int) *Terminal {
	t.Helper()
	vt, err := a.Repaint(history)
	if err != nil {
		t.Fatal(err)
	}
	cols, rows := a.Size()
	b := newTerm(t, cols, rows)
	if _, err := b.Write(vt); err != nil {
		t.Fatal(err)
	}
	return b
}

// sameState fails unless a and b show the same screen, styles, modes,
// cursor and keyboard state: their VT, the formatter's whole account of
// them, is equal over the last rows+history rows.
func sameState(t *testing.T, name string, a, b *Terminal, history int) {
	t.Helper()
	_, rows := a.Size()
	va, _ := a.VT()
	vb, _ := b.VT()
	ta, tb := trimHistory(va, int(rows)+history), trimHistory(vb, int(rows)+history)
	if string(ta) != string(tb) {
		t.Errorf("%s: VT differs after repaint\nwant %q\ngot  %q", name, ta, tb)
	}
	sa, _ := a.Screen()
	sb, _ := b.Screen()
	if sa != sb {
		t.Errorf("%s: screen differs\nwant %q\ngot  %q", name, sa, sb)
	}
	if a.Modes() != b.Modes() {
		t.Errorf("%s: modes %+v, want %+v", name, b.Modes(), a.Modes())
	}
}

// TestRepaintFidelity: a repaint (the VT formatter's output, the remote
// panes' resync, docs/SPEC.md §16.12) rebuilds a terminal's visible state
// in a fresh emulator: text, styles, wide characters, modes, cursor,
// scrolling region, keyboard flags and charsets.
func TestRepaintFidelity(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"styles", "plain \x1b[1mbold\x1b[0m \x1b[3;4mit-ul\x1b[0m \x1b[2mdim\x1b[0m \x1b[7mrev\x1b[0m\r\n" +
			"\x1b[31mred\x1b[0m \x1b[38;5;208m256\x1b[0m \x1b[38;2;10;20;30;48;2;200;210;220mrgb\x1b[0m\r\n" +
			"\x1b[4:3mcurly\x1b[0m \x1b[58;2;255;0;0;4munder-colour\x1b[0m \x1b[9mstrike\x1b[0m"},
		{"wide", "漢字かな ok\r\n❯ 🙂 emoji\r\n" + strings.Repeat("字", 41)},
		{"cursor", "line one\r\nline two\x1b[5;12H\x1b[?25l\x1b[4 q"},
		{"modes", "\x1b[?2004h\x1b[?1002h\x1b[?1006h\x1b[?1004h\x1b[?1h\x1b[?2031hready"},
		{"kitty keyboard", "\x1b[>1u\x1b[>3uprompt> "},
		{"scroll region", "\x1b[2;10rregion\x1b[3;1Hinside"},
		{"origin and wrap", "\x1b[?7l" + strings.Repeat("x", 100) + "\x1b[?6h\x1b[2;4r\x1b[1;1Hat origin"},
		{"charset", "\x1b(0lqqk\x1b(B text"},
		{"tabstops", "\x1b[3g\x1bH\x1b[10G\x1bH\ra\tb\tc"},
		{"title-free claude-ish", claudeScreen()},
	}
	for _, c := range cases {
		a := newTerm(t, 80, 24)
		a.Write([]byte(c.in))
		sameState(t, c.name, a, repainted(t, a, 0), 0)
	}
}

// TestRepaintHistory: a repaint carries the last history rows of the
// scrollback, and a fresh terminal keeps them as its scrollback. The
// screen's trailing blank row stays blank.
func TestRepaintHistory(t *testing.T) {
	a := newTerm(t, 40, 5)
	for i := range 50 {
		fmt.Fprintf(a, "row %d \x1b[32mgreen\x1b[0m\r\n", i)
	}
	b := repainted(t, a, 10)
	sa, _ := a.Screen()
	sb, _ := b.Screen()
	if sa != sb {
		t.Errorf("screen differs\nwant %q\ngot  %q", sa, sb)
	}
	text, _ := b.PlainText()
	if !strings.HasPrefix(text, "row 36 green\n") || !strings.HasSuffix(text, "row 49 green") {
		t.Errorf("scrollback after repaint with 10 rows of history:\n%s", text)
	}
}

// TestRepaintAltScreen: on the alternate screen, a repaint rebuilds the
// alternate screen and its mode; the primary screen under it is lost, as
// the design says (docs/SPEC.md §16.12).
func TestRepaintAltScreen(t *testing.T) {
	a := newTerm(t, 80, 24)
	a.Write([]byte("primary text\x1b[?1049h\x1b[Hfull-screen app \x1b[1mbold\x1b[0m"))
	b := repainted(t, a, 0)
	sameState(t, "alt", a, b, 0)
	if !b.Modes().AltScreen {
		t.Fatal("not on the alternate screen after repaint")
	}
	b.Write([]byte("\x1b[?1049l"))
	if s, _ := b.Screen(); strings.Contains(s, "primary text") {
		t.Errorf("primary screen came back: %q", s)
	}
}

// TestRepaintThenOutput: output after a repaint lands where it would on
// the original (the proxy feeds a repaint, then the stream's output).
func TestRepaintThenOutput(t *testing.T) {
	a := newTerm(t, 60, 10)
	a.Write([]byte(claudeScreen()))
	b := repainted(t, a, 0)
	more := "\x1b[8;3H\x1b[33mtyping…\x1b[0m\r\n\x1b[2K> y\x1b[?2026h\x1b[1;1Hheader\x1b[?2026l"
	a.Write([]byte(more))
	b.Write([]byte(more))
	sameState(t, "then output", a, b, 0)
}

// claudeScreen is a screen in the shape an agent's TUI draws: a box,
// styles, a prompt and a hint line.
func claudeScreen() string {
	var s strings.Builder
	s.WriteString("\x1b[38;2;215;119;87m╭───────────────────────────╮\x1b[0m\r\n")
	s.WriteString("\x1b[38;2;215;119;87m│\x1b[0m \x1b[1m✻ Welcome\x1b[0m                 \x1b[38;2;215;119;87m│\x1b[0m\r\n")
	s.WriteString("\x1b[38;2;215;119;87m╰───────────────────────────╯\x1b[0m\r\n\r\n")
	s.WriteString("\x1b[1m●\x1b[0m Edit src/export.go?\r\n")
	s.WriteString("  \x1b[36m❯ 1. Yes\x1b[0m\r\n    2. Yes, and don't ask again\r\n    3. No\r\n")
	s.WriteString("\x1b[2m  ? for shortcuts\x1b[0m\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\")
	s.WriteString("\x1b[6;5H")
	return s.String()
}
