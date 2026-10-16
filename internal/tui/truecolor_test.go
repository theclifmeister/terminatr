package tui

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
)

// TestTruecolorAnswer: the answers that say 24-bit colour, and DA1 as the
// fence, from terminals that answer differently.
func TestTruecolorAnswer(t *testing.T) {
	const da1 = "\x1b[?62;22c"
	for _, c := range []struct {
		name, in   string
		tc, fenced bool
	}{
		{"xtgettcap RGB", "\x1bP1+r524742\x1b\\" + da1, true, true},
		{"xtgettcap RGB with value", "\x1bP1+r524742=38\x1b\\" + da1, true, true},
		{"xtgettcap Tc", "\x1bP0+r524742\x1b\\\x1bP1+r5463\x1b\\" + da1, true, true},
		{"decrqss semicolons", "\x1bP1$r0;38;2;1;2;3m\x1b\\" + da1, true, true},
		{"decrqss colons", "\x1bP1$r38:2::1:2:3m\x1b\\" + da1, true, true},
		{"decrqss colour space", "\x1bP1$r38:2:0:1:2:3m\x1b\\" + da1, true, true},
		{"decrqss downsampled", "\x1bP1$r0;38;5;16m\x1b\\" + da1, false, true},
		{"invalid (tmux)", "\x1bP0$r\x1b\\" + da1, false, true},
		{"da1 only", da1, false, true},
		{"before da1", "\x1bP1+r524742\x1b\\", true, false},
		{"partial dcs", "\x1bP1+r5247", false, false},
		{"nothing", "", false, false},
	} {
		tc, fenced := truecolorAnswer([]byte(c.in))
		if tc != c.tc || fenced != c.fenced {
			t.Errorf("%s: got tc %v fenced %v, want %v %v", c.name, tc, fenced, c.tc, c.fenced)
		}
	}
}

// fakeTerm is a terminal for probeTruecolor: what it answers the
// queries, nothing for one that never answers.
type fakeTerm struct {
	pr     *io.PipeReader
	pw     *io.PipeWriter
	answer string
	asked  bytes.Buffer
}

func newFakeTerm(answer string) *fakeTerm {
	pr, pw := io.Pipe()
	return &fakeTerm{pr: pr, pw: pw, answer: answer}
}

func (f *fakeTerm) Write(b []byte) (int, error) {
	f.asked.Write(b)
	if f.answer != "" {
		go f.pw.Write([]byte(f.answer))
	}
	return len(b), nil
}

func (f *fakeTerm) cancel() bool {
	f.pr.CloseWithError(errors.New("canceled"))
	return true
}

// TestProbeTruecolor: a terminal that answers RGB is truecolor, at once;
// one that answers only DA1 isn't; one that never answers isn't either,
// and the probe gives up at its bound, its read canceled.
func TestProbeTruecolor(t *testing.T) {
	ft := newFakeTerm("\x1bP1+r524742\x1b\\\x1bP0$r\x1b\\\x1b[?1;2c")
	start := time.Now()
	if !probeTruecolor(ft.pr, ft, ft.cancel, 20*time.Second) {
		t.Error("a terminal answering RGB: not truecolor")
	}
	if time.Since(start) > 10*time.Second {
		t.Error("the probe waited out its bound despite DA1")
	}
	if ft.asked.String() != askTruecolor {
		t.Errorf("asked %q", ft.asked.String())
	}

	ft = newFakeTerm("\x1b[?1;2c")
	if probeTruecolor(ft.pr, ft, ft.cancel, 5*time.Second) {
		t.Error("a terminal answering only DA1: truecolor")
	}

	ft = newFakeTerm("")
	start = time.Now()
	if probeTruecolor(ft.pr, ft, ft.cancel, 100*time.Millisecond) {
		t.Error("a terminal that never answers: truecolor")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("the probe took %v for a terminal that never answers", d)
	}
}

// TestProbedTruecolorPanePalette: without COLORTERM, a terminal that
// answered truecolor gets the theme's pane palette (T344); without the
// answer the panes keep their symbolic colours.
func TestProbedTruecolorPanePalette(t *testing.T) {
	t.Setenv("COLORTERM", "")
	tt, err := parseTheme([]byte("[dark.pane]\nfg = \"#ffffff\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer probedTruecolor.Store(probedTruecolor.Load())
	defer setTheme(builtinTheme, themeDark.Load())

	probedTruecolor.Store(false)
	setTheme(tt, true)
	if th().Pane != nil {
		t.Error("no truecolor: a pane palette")
	}
	probedTruecolor.Store(true)
	setTheme(tt, true)
	if p := th().Pane; p == nil || p.Fg == nil || *p.Fg != (emu.RGB{R: 255, G: 255, B: 255}) {
		t.Errorf("truecolor answered: pane palette %+v", th().Pane)
	}
}
