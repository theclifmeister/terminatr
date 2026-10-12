//go:build cgo

package emu

import (
	"bytes"
	"testing"
)

// A theme's pane palette replaces the default colours and the 16 ANSI
// colours; 256-colour indexes and the program's truecolor stay as drawn.
func TestRendererPalette(t *testing.T) {
	pane := newTerm(t, 20, 3)
	r, err := NewRenderer(20, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pane.Write([]byte("plain \x1b[31mred\x1b[0m \x1b[94mbb\x1b[0m \x1b[38;5;200mx\x1b[0m \x1b[38;2;1;2;3my\x1b[0m \x1b[7minv"))

	// Without a palette everything stays symbolic.
	f, _ := r.Frame(pane, false)
	for _, want := range []string{"\x1b[0;31mred", "\x1b[0;94mbb", "\x1b[0;38;5;200mx", "\x1b[0;38;2;1;2;3my", "\x1b[0mplain", "\x1b[0m\x1b[K"} {
		if !bytes.Contains(f, []byte(want)) {
			t.Errorf("symbolic frame lacks %q: %q", want, f)
		}
	}

	pal := &Palette{Fg: &RGB{216, 222, 233}, Bg: &RGB{46, 52, 64}}
	pal.ANSI[1] = &RGB{191, 97, 106}
	pal.ANSI[12] = &RGB{129, 161, 193}
	r.SetPalette(pal)
	f, _ = r.Frame(pane, false)
	if f == nil || !bytes.Contains(f, []byte("\x1b[2J")) {
		t.Fatalf("a new palette doesn't repaint: %q", f)
	}
	dflt := "\x1b[0;38;2;216;222;233;48;2;46;52;64m"
	for _, want := range []string{
		dflt + "\x1b[H\x1b[2J", // the clear paints the theme's background
		dflt + "plain",
		"\x1b[0;38;2;191;97;106;48;2;46;52;64mred",    // ANSI red from the palette
		"\x1b[0;38;2;129;161;193;48;2;46;52;64mbb",    // bright blue too
		"\x1b[0;38;5;200;48;2;46;52;64mx",             // 256 colours stay
		"\x1b[0;38;2;1;2;3;48;2;46;52;64my",           // the program's truecolor stays
		"\x1b[0;7;38;2;216;222;233;48;2;46;52;64minv", // inverse swaps the theme's
		dflt + "\x1b[K",                               // the row's end is erased in the background
	} {
		if !bytes.Contains(f, []byte(want)) {
			t.Errorf("palette frame lacks %q: %q", want, f)
		}
	}

	// An ANSI colour the palette leaves out stays symbolic; the same
	// palette again changes nothing.
	pal2 := &Palette{}
	r.SetPalette(pal2)
	f, _ = r.Frame(pane, false)
	if !bytes.Contains(f, []byte("\x1b[0;31mred")) || !bytes.Contains(f, []byte("\x1b[0mplain")) {
		t.Errorf("empty palette: %q", f)
	}
	r.SetPalette(pal2)
	if f, _ := r.Frame(pane, false); f != nil {
		t.Errorf("same palette redrew %q", f)
	}

	// Back to none: symbolic again.
	r.SetPalette(nil)
	if f, _ := r.Frame(pane, false); !bytes.Contains(f, []byte("\x1b[0;31mred")) || bytes.Contains(f, []byte(";2;46;52;64")) {
		t.Errorf("palette removed: %q", f)
	}
}

// In a shared window the pane's rectangle is erased in the theme's
// background, never past its edge.
func TestRendererPaletteSharedRect(t *testing.T) {
	pane := newTerm(t, 10, 2)
	r, err := NewRenderer(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.SetRect(5, 1, 10, 2)
	r.SetPalette(&Palette{Bg: &RGB{1, 2, 3}})
	pane.Write([]byte("hi"))
	f, _ := r.Frame(pane, false)
	for _, want := range []string{"\x1b[2;6H\x1b[0;48;2;1;2;3m\x1b[10X", "\x1b[0;48;2;1;2;3m\x1b[8X"} {
		if !bytes.Contains(f, []byte(want)) {
			t.Errorf("shared frame lacks %q: %q", want, f)
		}
	}
	if bytes.Contains(f, []byte("\x1b[K")) || bytes.Contains(f, []byte("\x1b[2J")) {
		t.Errorf("shared frame erases past the pane: %q", f)
	}
}
