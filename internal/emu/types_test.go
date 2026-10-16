package emu

import "testing"

// TestRGBDark: a colour is dark below half HSL lightness, as the
// console decides for a terminal's background.
func TestRGBDark(t *testing.T) {
	for _, c := range []struct {
		rgb  RGB
		want bool
	}{
		{RGB{}, true},
		{RGB{0x10, 0x12, 0x16}, true}, // GitHub Dark
		{RGB{0x2e, 0x34, 0x40}, true}, // nord
		{RGB{0xff, 0x00, 0x00}, false},
		{RGB{0x7f, 0x7f, 0x7f}, true},
		{RGB{0x80, 0x80, 0x80}, false},
		{RGB{0xec, 0xef, 0xf4}, false},
		{RGB{0xff, 0xff, 0xff}, false},
	} {
		if got := c.rgb.Dark(); got != c.want {
			t.Errorf("%+v: dark %v, want %v", c.rgb, got, c.want)
		}
	}
}
