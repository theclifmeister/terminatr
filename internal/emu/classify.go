package emu

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// What a console sends a pane, read back from the bytes (docs/SPEC.md §4,
// §16.12): keys, pastes and mouse presses are a person's typing; the
// reports the program asked for (mouse release, motion and wheel, focus,
// kitty key releases and lone modifiers) are not, and the terminal's
// replies (DA, DSR, DECRPM, kitty flags, colour scheme, window reports,
// OSC, DCS, APC, PM and SOS strings) are never for the program: the
// server's emulator answers its queries. It has no libghostty in it.

// InputKind is what one piece of console input is.
type InputKind uint8

const (
	InputKey     InputKind = iota // a key press: text, a control key, Esc or Alt with a key, a CSI or SS3 key
	InputPaste                    // a bracketed paste, markers and text
	InputClick                    // a mouse button press
	InputMouse                    // a mouse release, motion or wheel
	InputFocus                    // focus in or out
	InputRelease                  // a kitty key release or repeat, or a modifier key alone
	InputReply                    // a terminal's reply or report string
)

// Typed says whether the kind is a person typing: a key, a paste or a
// click, which takes a thread over.
func (k InputKind) Typed() bool { return k == InputKey || k == InputPaste || k == InputClick }

// NextInput reads the piece of input at b's start: its kind and length
// (at least 1 for a non-empty b). A sequence cut short is a key, the
// rest of b: consoles send a key's sequence whole.
func NextInput(b []byte) (InputKind, int) {
	if len(b) == 0 {
		return InputKey, 0
	}
	if b[0] != 0x1b {
		if b[0] < 0x20 || b[0] == 0x7f {
			return InputKey, 1
		}
		_, n := utf8.DecodeRune(b)
		return InputKey, n
	}
	if len(b) == 1 {
		return InputKey, 1 // Esc
	}
	switch b[1] {
	case '[':
		return nextCSI(b)
	case 'O':
		return InputKey, min(3, len(b)) // SS3: F1-F4, arrows, keypad
	case ']', 'P', '_', '^', 'X':
		return InputReply, stringEnd(b)
	case 0x1b:
		return InputKey, 1 // Esc, then another sequence
	}
	_, n := utf8.DecodeRune(b[1:])
	return InputKey, 1 + n // Alt with a key
}

// nextCSI reads ESC [ params intermediates final at b's start.
func nextCSI(b []byte) (InputKind, int) {
	if bytes.HasPrefix(b, pastePrefix) {
		if end := bytes.Index(b, pasteSuffix); end >= 0 {
			return InputPaste, end + len(pasteSuffix)
		}
		return InputPaste, len(b)
	}
	if bytes.HasPrefix(b, pasteSuffix) {
		return InputPaste, len(pasteSuffix)
	}
	i := 2
	for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
		i++
	}
	params := string(b[2:i])
	inter := i
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
		i++
	}
	if i == len(b) {
		return InputKey, len(b)
	}
	final, n := b[i], i+1
	switch {
	case final == 'M' && params == "" && inter == i:
		// X10 mouse: ESC [ M Cb Cx Cy.
		if len(b) < 6 {
			return InputReply, len(b)
		}
		return mouseKind(int(b[3])-32, true), 6
	case params != "" && params[0] == '<' && (final == 'M' || final == 'm'):
		// SGR mouse: ESC [ < Cb ; Cx ; Cy M (press) or m (release).
		cb, err := strconv.Atoi(strings.SplitN(params[1:], ";", 2)[0])
		if err != nil {
			return InputReply, n
		}
		return mouseKind(cb, final == 'M'), n
	case params == "" && (final == 'I' || final == 'O'):
		return InputFocus, n
	case params != "" && strings.ContainsRune("<=>?", rune(params[0])), inter != i:
		// DA, DECRPM, kitty flags, colour scheme, XTWINOPS replies: no
		// key has a private marker or an intermediate.
		return InputReply, n
	case final == 'c' || final == 'n' || final == 't':
		return InputReply, n // DA, DSR, window reports
	case final == 'R' && strings.Contains(params, ";") && !strings.HasPrefix(params, "1;"):
		// A cursor position report; ESC [ 1 ; mods R is F3 with modifiers.
		return InputReply, n
	case final == 'u':
		return kittyKind(params), n
	}
	return InputKey, n
}

// mouseKind is a mouse report's kind by its button byte: a press unless
// it is motion (32), the wheel (64) or a release (SGR's m, X10's
// button 3).
func mouseKind(cb int, press bool) InputKind {
	if !press || cb&(32|64) != 0 || cb&3 == 3 {
		return InputMouse
	}
	return InputClick
}

// kittyKind is a kitty keyboard report's kind, ESC [ code[:alts] ;
// mods[:event] ; text u: a release (event 3) or repeat (2), or a
// modifier key alone (57441-57452, and the lock keys), is no typing.
func kittyKind(params string) InputKind {
	f := strings.Split(params, ";")
	code, err := strconv.Atoi(strings.Split(f[0], ":")[0])
	if err != nil {
		return InputKey
	}
	if len(f) > 1 {
		if _, ev, ok := strings.Cut(f[1], ":"); ok && ev != "1" {
			return InputRelease
		}
	}
	if code >= 57441 && code <= 57452 || code >= 57358 && code <= 57360 {
		return InputRelease
	}
	return InputKey
}

// stringEnd is the length of the OSC, DCS, APC, PM or SOS string at b's
// start, through its BEL (OSC) or ESC \; all of b when it isn't ended.
func stringEnd(b []byte) int {
	for i := 2; i < len(b); i++ {
		switch {
		case b[i] == 0x07 && b[1] == ']':
			return i + 1
		case b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\':
			return i + 2
		}
	}
	return len(b)
}

var (
	pastePrefix = []byte("\x1b[200~")
	pasteSuffix = []byte("\x1b[201~")
)

// Typed says whether b holds a person's typing: a key, a paste or a
// mouse press.
func Typed(b []byte) bool {
	for len(b) > 0 {
		kind, n := NextInput(b)
		if kind.Typed() {
			return true
		}
		b = b[n:]
	}
	return false
}

// ForProgram is b without what its program, in modes m, never asked
// for: terminal replies always, focus reports without mode 1004, mouse
// reports without mouse tracking. It returns b itself when it keeps all.
func ForProgram(b []byte, m Modes) []byte {
	var out []byte
	for i := 0; i < len(b); {
		kind, n := NextInput(b[i:])
		drop := kind == InputReply || kind == InputFocus && !m.Focus ||
			(kind == InputClick || kind == InputMouse) && !m.MouseTracking()
		switch {
		case drop && out == nil:
			out = append(make([]byte, 0, len(b)), b[:i]...)
		case !drop && out != nil:
			out = append(out, b[i:i+n]...)
		}
		i += n
	}
	if out == nil {
		return b
	}
	return out
}
