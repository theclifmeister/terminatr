package emu

import "testing"

// TestNextInput (T686): each piece of console input, and its length.
func TestNextInput(t *testing.T) {
	for _, c := range []struct {
		in   string
		kind InputKind
		n    int
	}{
		{"a", InputKey, 1},
		{"é", InputKey, 2},
		{"\r", InputKey, 1},
		{"\x7f", InputKey, 1},
		{"\x1b", InputKey, 1},
		{"\x1bx", InputKey, 2},
		{"\x1b\x1b[A", InputKey, 1},
		{"\x1b[A", InputKey, 3},
		{"\x1bOP", InputKey, 3},
		{"\x1b[5~", InputKey, 4},
		{"\x1b[1;5R", InputKey, 6}, // F3 with ctrl
		{"\x1b[13u", InputKey, 5},
		{"\x1b[97;5u", InputKey, 7},
		{"\x1b[97;1:1u", InputKey, 9},
		{"\x1b[200~hi\x1b[201~x", InputPaste, 14},
		{"\x1b[200~cut", InputPaste, 9},
		{"\x1b[<0;10;5M", InputClick, 10},
		{"\x1b[<2;10;5M", InputClick, 10},
		{"\x1b[<16;10;5M", InputClick, 11}, // ctrl+click
		{"\x1b[<0;10;5m", InputMouse, 10},
		{"\x1b[<35;10;5M", InputMouse, 11},
		{"\x1b[<32;10;5M", InputMouse, 11}, // a drag
		{"\x1b[<64;10;5M", InputMouse, 11},
		{"\x1b[<65;10;5M", InputMouse, 11},
		{"\x1b[M #!", InputClick, 6},
		{"\x1b[M#!!", InputMouse, 6},
		{"\x1b[MC!!", InputMouse, 6},
		{"\x1b[I", InputFocus, 3},
		{"\x1b[O", InputFocus, 3},
		{"\x1b[97;1:3u", InputRelease, 9},
		{"\x1b[97;1:2u", InputRelease, 9},
		{"\x1b[57441;2u", InputRelease, 10}, // left shift alone
		{"\x1b[?62;22c", InputReply, 9},
		{"\x1b[>1;10;0c", InputReply, 10},
		{"\x1b[?1u", InputReply, 5},
		{"\x1b[?2004;1$y", InputReply, 11},
		{"\x1b[?997;1n", InputReply, 9},
		{"\x1b[0n", InputReply, 4},
		{"\x1b[12;40R", InputReply, 8},
		{"\x1b[4;800;1200t", InputReply, 13},
		{"\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\x", InputReply, 25},
		{"\x1b]10;rgb:d4d4/d4d4/d4d4\x07x", InputReply, 24},
		{"\x1b]4;1;rgb:cccc/0000/0000\x1b\\", InputReply, 26},
		{"\x1bP>|ghostty 1.2\x1b\\", InputReply, 17},
		{"\x1b_Gi=1;OK\x1b\\", InputReply, 11},
		{"\x1b]11;rgb:1e1e", InputReply, 13},
	} {
		kind, n := NextInput([]byte(c.in))
		if kind != c.kind || n != c.n {
			t.Errorf("NextInput(%q) = %d, %d; want %d, %d", c.in, kind, n, c.kind, c.n)
		}
	}
}

// TestTyped (T479, T686): a console sends focus and mouse reports and
// the terminal's replies with no one typing; only keys, pastes and
// mouse presses are typing.
func TestTyped(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"", false},
		{"a", true},
		{"\r", true},
		{"\x1b", true},
		{"\x1b[A", true},
		{"\x1b[200~hi\x1b[201~", true},
		{"\x1b[I", false},
		{"\x1b[O", false},
		{"\x1b[I\x1b[O", false},
		{"\x1b[<0;10;5M", true},
		{"\x1b[<0;10;5m", false},
		{"\x1b[<35;10;5M", false},
		{"\x1b[<64;10;5M", false},
		{"\x1b[M #!", true},
		{"\x1b[M#!!", false},
		{"\x1b[I\x1b[<0;1;1M", true},
		{"\x1b[Ix", true},
		{"\x1b[97;1:3u", false},
		{"\x1b[57441;2u", false},
		{"\x1b[?62;22c\x1b]11;rgb:0/0/0\x07\x1b[?997;1n\x1b[12;40R", false},
		{"\x1bP>|xterm\x1b\\\x1b[?1u\x1b[?2004;2$y", false},
		{"\x1b]11;rgb:0/0/0\x07q", true},
	} {
		if got := Typed([]byte(c.in)); got != c.want {
			t.Errorf("Typed(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestForProgram (T686): replies never reach the program; focus and
// mouse reports only when it asked for them; keys always.
func TestForProgram(t *testing.T) {
	none, focus, mouse := Modes{}, Modes{Focus: true}, Modes{AnyMouse: true}
	for _, c := range []struct {
		in   string
		m    Modes
		want string
	}{
		{"hi\r", none, "hi\r"},
		{"\x1b[A\x1b[5~", none, "\x1b[A\x1b[5~"},
		{"\x1b[I", none, ""},
		{"\x1b[I", focus, "\x1b[I"},
		{"\x1b[<0;1;1M\x1b[<35;2;1M", none, ""},
		{"\x1b[<0;1;1M\x1b[<35;2;1M", mouse, "\x1b[<0;1;1M\x1b[<35;2;1M"},
		{"a\x1b[?62;22cb", focus, "ab"},
		{"\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\\x1b[12;40R", focus, ""},
		{"\x1b[200~x\x1b[201~", none, "\x1b[200~x\x1b[201~"},
		{"\x1b[97;1:3u", none, "\x1b[97;1:3u"},
	} {
		if got := string(ForProgram([]byte(c.in), c.m)); got != c.want {
			t.Errorf("ForProgram(%q, %+v) = %q, want %q", c.in, c.m, got, c.want)
		}
	}
	b := []byte("keep")
	if got := ForProgram(b, none); &got[0] != &b[0] {
		t.Error("ForProgram copied input it keeps whole")
	}
}
