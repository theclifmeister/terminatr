package emu

import "encoding/base64"

// The emulator's portable vocabulary: options, schemes, modes, input
// events and OSC 52. It has no libghostty in it, so it builds without cgo
// (emu_nocgo.go), where the rest of this package is a stub.

// DefaultScrollback is the number of scrollback lines a pane keeps.
const DefaultScrollback = 10000

// Options configure a new Terminal.
type Options struct {
	Cols, Rows uint16
	// Scrollback is the scrollback limit in lines; 0 means DefaultScrollback.
	Scrollback uint
	// WritePty receives the emulator's answers to terminal queries. Only
	// the server's emulator sets it. It is called synchronously from Write
	// and must not block.
	WritePty func([]byte)
	// Xtversion is the name reported for XTVERSION queries.
	Xtversion string
	// ColorScheme answers colour-scheme queries (CSI ? 996 n) when
	// WritePty is set: the scheme, and false while it is unknown (the
	// query then goes unanswered).
	ColorScheme func() (Scheme, bool)
	// Colors are the default colours the emulator answers OSC 10, 11
	// and 4 queries with (when WritePty is set), from the terminal a
	// client is attached from; SetColors changes them.
	Colors Colors
}

// RGB is a 24-bit colour.
type RGB struct{ R, G, B uint8 }

// Dark reports whether c is a dark colour: its HSL lightness is below
// one half, as the console decides for a terminal's background.
func (c RGB) Dark() bool {
	return int(max(c.R, c.G, c.B))+int(min(c.R, c.G, c.B)) < 255
}

// Colors are a terminal's default colours as its answers to OSC 10
// (foreground), 11 (background) and 4 (the 16 ANSI palette entries) gave
// them, with which of them are known. The zero value knows none.
type Colors struct {
	Fg, Bg  RGB
	Palette [16]RGB
	Known   uint32 // KnownFg, KnownBg, PaneDark or PaneLight, and bit i for Palette[i]
}

const (
	KnownFg uint32 = 1 << 16
	KnownBg uint32 = 1 << 17
	// PaneDark and PaneLight say the colours are under a theme's pane
	// palette (docs/SPEC.md §5.3), the dark scheme's or the light one's:
	// an agent started then may switch to an ANSI theme of its own, which
	// the palette colours (§8.6).
	PaneDark  uint32 = 1 << 18
	PaneLight uint32 = 1 << 19
)

// colorsLen is the length of an encoded Colors: Known, then Fg, Bg and
// the palette, three bytes each.
const colorsLen = 4 + 18*3

// Encode returns c as a FrameColors payload.
func (c Colors) Encode() []byte {
	b := make([]byte, 0, colorsLen)
	b = append(b, byte(c.Known>>24), byte(c.Known>>16), byte(c.Known>>8), byte(c.Known))
	for _, rgb := range append([]RGB{c.Fg, c.Bg}, c.Palette[:]...) {
		b = append(b, rgb.R, rgb.G, rgb.B)
	}
	return b
}

// DecodeColors reverses Encode; false for a payload of another length.
func DecodeColors(b []byte) (Colors, bool) {
	var c Colors
	if len(b) != colorsLen {
		return c, false
	}
	c.Known = uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	c.Known &= KnownFg | KnownBg | PaneDark | PaneLight | 0xffff
	rgb := func(i int) RGB { o := 4 + 3*i; return RGB{b[o], b[o+1], b[o+2]} }
	c.Fg, c.Bg = rgb(0), rgb(1)
	for i := range c.Palette {
		c.Palette[i] = rgb(2 + i)
	}
	return c, true
}

// Scheme is a light or dark colour scheme (mode 2031 reports).
type Scheme uint8

const (
	SchemeDark  Scheme = 1
	SchemeLight Scheme = 2
)

// Modes are the program's terminal modes that matter to an attach client.
type Modes struct {
	NormalMouse       bool // 1000
	ButtonMouse       bool // 1002
	AnyMouse          bool // 1003
	Focus             bool // 1004
	BracketedPaste    bool // 2004
	ColorSchemeReport bool // 2031
	AltScreen         bool // the alternate screen is active
}

// MouseTracking reports whether the program asked for any mouse reports.
func (m Modes) MouseTracking() bool { return m.NormalMouse || m.ButtonMouse || m.AnyMouse }

// Mods are key modifiers.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
)

// SpecialKey names a key that has no text of its own.
type SpecialKey uint8

const (
	KeyNone SpecialKey = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeySpace
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyDelete
	KeyKpEnter
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Key is one key press, as decoded from the outer terminal.
type Key struct {
	// Special is set for named keys; otherwise Rune is the key's
	// unshifted character ('a' for Shift+A).
	Special SpecialKey
	Rune    rune
	Mods    Mods
	// Text is what the key types, if anything ("A" for Shift+A).
	Text string
}

// MouseButton is a mouse button or wheel direction.
type MouseButton uint8

const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
)

// MouseAction is what happened to a button.
type MouseAction uint8

const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMotion
)

// Mouse is one mouse event in pane cells (0-based).
type Mouse struct {
	Action MouseAction
	Button MouseButton
	Mods   Mods
	X, Y   int
}

// The clipboards of OSC 52, by their letter there.
const (
	ClipboardStandard  = 'c'
	ClipboardPrimary   = 'p'
	ClipboardSelection = 's'
)

// MaxClipboard caps the text one OSC 52 write carries, so a runaway
// selection or program can't flood the outer terminal. Some terminals
// take less (tmux and xterm by default around 100 KB).
const MaxClipboard = 1 << 20

// OSC52 is the sequence that puts data on the outer terminal's clipboard
// which (Clipboard*), or clears it when data is empty; nil when data is
// over MaxClipboard.
func OSC52(which byte, data []byte) []byte {
	if len(data) > MaxClipboard {
		return nil
	}
	b := append([]byte("\x1b]52;"), which, ';')
	b = base64.StdEncoding.AppendEncode(b, data)
	return append(b, '\a')
}

// Palette is the colours a renderer draws a pane's symbolic colours with
// (a theme's pane palette, docs/SPEC.md §5.3): the default foreground and
// background (SGR 39/49) and the 16 ANSI colours (palette indexes 0-15).
// A nil entry stays symbolic, for the outer terminal's own; indexes
// 16-255 and the program's truecolor always do.
type Palette struct {
	Fg, Bg *RGB
	ANSI   [16]*RGB
}
