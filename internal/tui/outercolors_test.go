package tui

import (
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/theclifmeister/terminatr/internal/emu"
)

func TestPaletteAnswer(t *testing.T) {
	for _, tc := range []struct {
		in   string
		i    int
		want color.Color
	}{
		{"\x1b]4;1;rgb:cccc/6666/6666\x07", 1, color.RGBA{0xcc, 0x66, 0x66, 0xff}},
		{"\x1b]4;15;rgb:ff/fe/fd\x1b\\", 15, color.RGBA{0xff, 0xfe, 0xfd, 0xff}},
		{"\x1b]4;16;rgb:ff/fe/fd\x07", -1, nil}, // not one of the 16
		{"\x1b]4;x;rgb:ff/fe/fd\x07", -1, nil},
		{"\x1b]4;2;nonsense\x07", -1, nil},
		{"\x1b]52;c;eA==\x07", -1, nil},
	} {
		i, col, ok := paletteAnswer(tc.in)
		if tc.i < 0 {
			if ok {
				t.Errorf("%q: parsed as %d %v", tc.in, i, col)
			}
			continue
		}
		r, g, b, _ := col.RGBA()
		wr, wg, wb, _ := tc.want.RGBA()
		if !ok || i != tc.i || r>>8 != wr>>8 || g>>8 != wg>>8 || b>>8 != wb>>8 {
			t.Errorf("%q: %d %v %v, want %d %v", tc.in, i, col, ok, tc.i, tc.want)
		}
	}
}

// TestOuterColors: the client asks its terminal's colours with a DA1
// fence, records the answers, and takes them as complete at DA1, sending
// only what changed.
func TestOuterColors(t *testing.T) {
	if !strings.HasSuffix(askColors, "\x1b[c") || !strings.Contains(askColors, "\x1b]4;15;?\x07") {
		t.Fatalf("askColors: %q", askColors)
	}
	c := &client{}
	c.handle(uv.ForegroundColorEvent{Color: color.RGBA{0xee, 0xee, 0xee, 0xff}})
	c.handle(uv.BackgroundColorEvent{Color: color.RGBA{0x10, 0x20, 0x30, 0xff}})
	c.handle(uv.UnknownOscEvent("\x1b]4;3;rgb:aaaa/bbbb/cccc\x07"))
	if c.sentColors.Known != 0 {
		t.Fatal("sent before the DA1 fence")
	}
	c.handle(uv.PrimaryDeviceAttributesEvent{62, 22})
	want := emu.Colors{Fg: emu.RGB{R: 0xee, G: 0xee, B: 0xee}, Bg: emu.RGB{R: 0x10, G: 0x20, B: 0x30},
		Known: emu.KnownFg | emu.KnownBg | 1<<3}
	want.Palette[3] = emu.RGB{R: 0xaa, G: 0xbb, B: 0xcc}
	if c.sentColors != want {
		t.Fatalf("sent %+v, want %+v", c.sentColors, want)
	}
	// A scheme change asks again.
	c.osc = nil
	c.handle(uv.LightColorSchemeEvent{})
	if !strings.Contains(string(c.osc), askColors) {
		t.Fatalf("a scheme change doesn't ask the colours again: %q", c.osc)
	}
	c.handle(uv.BackgroundColorEvent{Color: color.RGBA{0xfa, 0xfa, 0xfa, 0xff}})
	c.handle(uv.PrimaryDeviceAttributesEvent{62})
	if c.sentColors.Bg != (emu.RGB{R: 0xfa, G: 0xfa, B: 0xfa}) {
		t.Fatalf("the new background wasn't sent: %+v", c.sentColors)
	}
}

// TestThemedColors: the panes' programs are answered with the theme's
// pane palette where it sets a colour, the terminal's elsewhere, and
// again when the palette changes; without one, as the terminal answered.
func TestThemedColors(t *testing.T) {
	c := &client{}
	c.handle(uv.ForegroundColorEvent{Color: color.RGBA{0xee, 0xee, 0xee, 0xff}})
	c.handle(uv.UnknownOscEvent("\x1b]4;3;rgb:aaaa/bbbb/cccc\x07"))
	c.handle(uv.UnknownOscEvent("\x1b]4;4;rgb:1111/2222/3333\x07"))
	c.handle(uv.PrimaryDeviceAttributesEvent{62})
	outer := c.sentColors

	pal := &emu.Palette{Bg: &emu.RGB{R: 0x2e, G: 0x34, B: 0x40}}
	pal.ANSI[3] = &emu.RGB{R: 0xeb, G: 0xcb, B: 0x8b}
	c.pal = pal
	c.sendColors()
	got := c.sentColors
	if got.Fg != outer.Fg || got.Bg != *pal.Bg || got.Palette[3] != *pal.ANSI[3] || got.Palette[4] != outer.Palette[4] {
		t.Fatalf("themed %+v", got)
	}
	if want := emu.KnownFg | emu.KnownBg | 1<<3 | 1<<4; got.Known != want {
		t.Fatalf("known %b, want %b", got.Known, want)
	}
	// The terminal answering again keeps the theme's over its own.
	c.handle(uv.UnknownOscEvent("\x1b]4;3;rgb:0000/0000/0000\x07"))
	c.handle(uv.PrimaryDeviceAttributesEvent{62})
	if c.sentColors.Palette[3] != *pal.ANSI[3] {
		t.Fatalf("the terminal's colour 3 went over the theme's: %+v", c.sentColors.Palette[3])
	}
	// No palette (another theme, safe mode, no truecolor): the terminal's.
	c.pal = nil
	c.sendColors()
	if c.sentColors.Known&emu.KnownBg != 0 || c.sentColors.Palette[3] != (emu.RGB{}) {
		t.Fatalf("unthemed %+v", c.sentColors)
	}

	// A theme alone answers, before the terminal does.
	if th := themedColors(emu.Colors{}, pal); th.Known != emu.KnownBg|1<<3 || th.Bg != *pal.Bg {
		t.Fatalf("theme alone %+v", th)
	}
}
