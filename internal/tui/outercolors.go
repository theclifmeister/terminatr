package tui

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The outer terminal's colours for the panes' programs (docs/SPEC.md
// §3.3): the client asks its terminal for the default foreground and
// background (OSC 10, 11) and the 16 ANSI palette entries (OSC 4) at
// start and whenever the colour scheme changes, and sends what it
// answered to the panes' sessions, whose emulators answer the programs'
// own OSC 10/11/4 queries with them.

// askColors asks the outer terminal its colours, then its attributes
// (DA1): every terminal answers DA1, after the colour queries before it,
// so the colours known by then are all there will be.
var askColors = func() string {
	var b strings.Builder
	b.WriteString("\x1b]10;?\x07\x1b]11;?\x07")
	for i := range 16 {
		b.WriteString("\x1b]4;" + strconv.Itoa(i) + ";?\x07")
	}
	b.WriteString("\x1b[c")
	return b.String()
}()

// gotColor records one of the outer terminal's colours: known is
// emu.KnownFg, emu.KnownBg or the palette entry's bit.
func (c *client) gotColor(known uint32, col color.Color) {
	if col == nil || !c.lock() {
		return
	}
	r, g, b, _ := col.RGBA()
	rgb := emu.RGB{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
	switch known {
	case emu.KnownFg:
		c.colors.Fg = rgb
	case emu.KnownBg:
		c.colors.Bg = rgb
	default:
		for i := range c.colors.Palette {
			if known == 1<<i {
				c.colors.Palette[i] = rgb
			}
		}
	}
	c.colors.Known |= known
	c.mu.Unlock()
}

// sendColors sends the panes the outer terminal's colours, once they
// answered (DA1 came) and only when they changed.
func (c *client) sendColors() {
	if !c.lock() {
		return
	}
	if c.colors == c.sentColors {
		c.mu.Unlock()
		return
	}
	c.sentColors = c.colors
	payload := c.colors.Encode()
	ps := c.leaves()
	c.mu.Unlock()
	for _, p := range ps {
		c.send(p, proto.FrameColors, payload)
	}
}

// paletteAnswer parses an OSC 4 answer, ESC ] 4 ; index ; colour ST,
// for one of the 16 ANSI entries.
func paletteAnswer(osc string) (int, color.Color, bool) {
	s, ok := strings.CutPrefix(osc, "\x1b]4;")
	if !ok {
		return 0, nil, false
	}
	s = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(s, "\x07"), "\x1b\\"), "\x9c")
	idx, spec, ok := strings.Cut(s, ";")
	i, err := strconv.Atoi(idx)
	if !ok || err != nil || i < 0 || i >= 16 {
		return 0, nil, false
	}
	col := ansi.XParseColor(spec)
	return i, col, col != nil
}
