package tui

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/theclifmeister/terminatr/internal/plat/term"
)

// Truecolor (T344): a theme's #rrggbb colours and its pane palette need a
// terminal that takes 24-bit colour. COLORTERM says so when set; SSH
// drops it by default, so a console without it asks the terminal once,
// in-band, as it starts: XTGETTCAP for RGB and Tc, and a DECRQSS of the
// SGR after setting a 24-bit colour, which a terminal that takes it
// reports back as set. DA1 fences the answers, as in askColors; a
// terminal that answers none of them by probeWait keeps the ANSI look.

// askTruecolor asks the terminal whether it takes 24-bit colour, then
// its attributes (DA1), and leaves the SGR reset.
const askTruecolor = "\x1bP+q524742\x1b\\\x1bP+q5463\x1b\\" + // XTGETTCAP RGB, Tc
	"\x1b[38;2;1;2;3m\x1bP$qm\x1b\\\x1b[m" + // DECRQSS SGR under 24-bit 1,2,3
	"\x1b[c"

// probeWait bounds the probe: a terminal that never answers DA1 costs
// the console this much once.
var probeWait = 750 * time.Millisecond

// probedTruecolor is the probe's answer: the terminal said it takes
// 24-bit colour.
var (
	probedTruecolor atomic.Bool
	probeOnce       sync.Once
)

// envTruecolor reports whether COLORTERM says the terminal takes 24-bit
// colour.
func envTruecolor() bool {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	return ct == "truecolor" || ct == "24bit"
}

// trueColor reports whether this console's terminal takes 24-bit colour.
func trueColor() bool { return envTruecolor() || probedTruecolor.Load() }

// probeTerminal asks in's terminal whether it takes 24-bit colour, once
// per console and only without COLORTERM; raw says in is in raw mode
// already. A truecolor answer makes the theme's truecolor choices this
// console's.
func probeTerminal(in, out *os.File, raw bool) {
	if envTruecolor() || in == nil || out == nil || !term.IsTerminal(in) {
		return
	}
	probeOnce.Do(func() {
		if !raw {
			restore, err := term.MakeRaw(in)
			if err != nil {
				return
			}
			defer restore()
		}
		cr, err := uv.NewCancelReader(in)
		if err != nil {
			return // a read left blocked would take the console's input
		}
		defer cr.Close()
		if probeTruecolor(cr, out, cr.Cancel, probeWait) {
			probedTruecolor.Store(true)
			setTheme(*themeTab.Load(), themeDark.Load())
		}
	})
}

// probeTruecolor writes askTruecolor to w and reads the answers from r
// until DA1 or wait, then cancels the read (cancel) and waits for it to
// end, so no read is left to take the console's input.
func probeTruecolor(r io.Reader, w io.Writer, cancel func() bool, wait time.Duration) bool {
	if _, err := io.WriteString(w, askTruecolor); err != nil {
		return false
	}
	var (
		mu  sync.Mutex
		buf []byte
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		b := make([]byte, 256)
		for {
			n, err := r.Read(b)
			mu.Lock()
			buf = append(buf, b[:n]...)
			_, fenced := truecolorAnswer(buf)
			mu.Unlock()
			if fenced || err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(wait):
		cancel()
		<-done
	}
	mu.Lock()
	defer mu.Unlock()
	tc, _ := truecolorAnswer(buf)
	return tc
}

// truecolorAnswer reads the terminal's answers to askTruecolor: tc when
// one says it takes 24-bit colour, fenced once DA1 came.
func truecolorAnswer(b []byte) (tc, fenced bool) {
	for len(b) > 0 {
		i := bytes.IndexByte(b, 0x1b)
		if i < 0 || i+1 >= len(b) {
			break
		}
		b = b[i:]
		switch b[1] {
		case 'P': // DCS … ST
			end := bytes.Index(b, []byte("\x1b\\"))
			if end < 0 {
				return tc, false
			}
			tc = tc || dcsTruecolor(string(b[2:end]))
			b = b[end+2:]
		case '[': // CSI: DA1 is CSI ? … c
			j := 2
			for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
				j++
			}
			if j == len(b) {
				return tc, false
			}
			if b[j] == 'c' && j > 2 && b[2] == '?' {
				return tc, true
			}
			b = b[j+1:]
		default:
			b = b[1:]
		}
	}
	return tc, false
}

// dcsTruecolor reports whether a DCS answer's body says 24-bit colour: a
// valid XTGETTCAP answer for RGB or Tc (1+r name[=value]), or a DECRQSS
// SGR answer (1$r … m) that kept the 24-bit 1,2,3 askTruecolor set.
func dcsTruecolor(s string) bool {
	if caps, ok := strings.CutPrefix(s, "1+r"); ok {
		for _, c := range strings.Split(caps, ";") {
			name, _, _ := strings.Cut(c, "=")
			n, err := hex.DecodeString(name)
			if err == nil && (string(n) == "RGB" || string(n) == "Tc") {
				return true
			}
		}
		return false
	}
	sgr, ok := strings.CutPrefix(s, "1$r")
	if !ok || !strings.HasSuffix(sgr, "m") {
		return false
	}
	// 38;2;1;2;3, 38:2:1:2:3 or 38:2::1:2:3 (an empty or a colour
	// space id before the channels).
	f := strings.FieldsFunc(strings.TrimSuffix(sgr, "m"), func(r rune) bool { return r == ';' || r == ':' })
	for i := 0; i+4 < len(f); i++ {
		if f[i] != "38" || f[i+1] != "2" {
			continue
		}
		ch := f[i+2:]
		if len(ch) >= 3 && ch[0] == "1" && ch[1] == "2" && ch[2] == "3" {
			return true
		}
		if len(ch) >= 4 && ch[1] == "1" && ch[2] == "2" && ch[3] == "3" {
			return true
		}
	}
	return false
}
