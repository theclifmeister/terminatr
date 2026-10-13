package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// paintTheme has a pane palette in both schemes, each its own colours,
// and a selection with a background of its own.
const paintTheme = `
[dark]
sel = { fg = ["black", "#000001"], bg = ["cyan", "#100001"] }
sel_focus = { fg = ["black", "#000002"], bg = ["cyan", "#100002"] }

[dark.pane]
fg = "#c0c1c2"
bg = "#050a05"

[light]
sel = { fg = ["black", "#000001"], bg = ["cyan", "#100001"] }
sel_focus = { fg = ["black", "#000002"], bg = ["cyan", "#100002"] }

[light.pane]
fg = "#202122"
bg = "#f0f1f2"
`

// usePaintTheme makes the theme TOML s this console's, dark or light,
// on a truecolor terminal, for the test.
func usePaintTheme(t *testing.T, s string, dark bool) themeTable {
	t.Helper()
	tt, err := parseTheme([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COLORTERM", "truecolor")
	if err := setTheme(tt, dark); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setTheme(builtinTheme, true) })
	return tt
}

// cells is the dashboard's view as the terminal gets it: a w×h grid of
// cells.
func cells(m *dash) uv.ScreenBuffer {
	buf := uv.NewScreenBuffer(m.w, m.h)
	uv.NewStyledString(m.View().Content).Draw(buf, buf.Bounds())
	return buf
}

// hex is a colour as #rrggbb, "" for none (the terminal's default).
func rgbHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// With a pane palette every cell of the dashboard (sidebar, list,
// details panel, header, footer and the gaps between them) has the
// palette's background, or a role's own (the selection); and every
// character a colour, the palette's foreground where no role sets one.
func TestPaintDashboard(t *testing.T) {
	usePaintTheme(t, paintTheme, true)
	m := chromeDash(t, mod.Layout{})
	if g := m.geom(); g.details().w == 0 || g.headRows() == 0 || g.footRows() == 0 {
		t.Fatalf("the test wants a details panel, header and footer: %+v", g)
	}
	checkPainted(t, m, "#050a05", "#c0c1c2")

	// A popup over the window.
	press(m, "?")
	if m.top() == nil {
		t.Fatal("no help popup")
	}
	checkPainted(t, m, "#050a05", "#c0c1c2")
}

// checkPainted fails unless every cell of m's window is painted with
// bg and fg, or a selection's own colours.
func checkPainted(t *testing.T, m *dash, bg, fg string) {
	t.Helper()
	buf := cells(m)
	own := map[string]bool{bg: true, "#100001": true, "#100002": true}
	var bad []string
	plain := 0 // characters in the palette's fg
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			c := buf.CellAt(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			b := rgbHex(c.Style.Bg)
			if !own[b] {
				bad = append(bad, "bg "+b+" at "+strconv.Itoa(x)+","+strconv.Itoa(y))
			}
			switch f := rgbHex(c.Style.Fg); {
			case f == "" && c.Content != " " && c.Content != "":
				bad = append(bad, "no fg for "+c.Content+" at "+strconv.Itoa(x)+","+strconv.Itoa(y))
			case f == fg && c.Content != " ":
				plain++
			}
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%d cells unpainted, first %v\n%s", len(bad), bad[:min(len(bad), 5)], whole(m))
	}
	// The corners and the footer: the palette's own.
	for _, p := range [][2]int{{0, 0}, {m.w - 1, 0}, {0, m.h - 1}, {m.w - 1, m.h - 1}, {m.sideW(), m.h / 2}} {
		c := buf.CellAt(p[0], p[1])
		if rgbHex(c.Style.Bg) != bg {
			t.Errorf("cell %v: bg %q, want %s", p, rgbHex(c.Style.Bg), bg)
		}
	}
	if plain == 0 {
		t.Errorf("no text in the palette's fg %s", fg)
	}
}

// Without a pane palette, or without truecolor, nothing is painted: the
// view is what the dashboard drew, in the terminal's own colours.
func TestPaintNone(t *testing.T) {
	m := chromeDash(t, mod.Layout{})
	if got := m.View().Content; got != m.render() {
		t.Fatal("the built-in theme painted the window")
	}
	if c := cells(m).CellAt(m.w-1, m.h/2); c.Style.Bg != nil {
		t.Fatalf("built-in theme: bg %v", c.Style.Bg)
	}

	t.Setenv("COLORTERM", "")
	tt, err := parseTheme([]byte(paintTheme))
	if err != nil {
		t.Fatal(err)
	}
	if trueColor() {
		t.Skip("this terminal said truecolor")
	}
	setTheme(tt, true)
	t.Cleanup(func() { setTheme(builtinTheme, true) })
	if got := m.View().Content; got != m.render() {
		t.Fatal("a palette theme painted without truecolor")
	}
}

// A scheme switch paints the window in the other scheme's palette; a
// scheme without one leaves it unpainted.
func TestPaintScheme(t *testing.T) {
	tt := usePaintTheme(t, paintTheme, true)
	m := chromeDash(t, mod.Layout{})
	checkPainted(t, m, "#050a05", "#c0c1c2")
	setTheme(tt, false)
	checkPainted(t, m, "#f0f1f2", "#202122")

	half := usePaintTheme(t, strings.SplitN(paintTheme, "[light]", 2)[0], true)
	checkPainted(t, m, "#050a05", "#c0c1c2")
	setTheme(half, false)
	if got := m.View().Content; got != m.render() {
		t.Fatal("light scheme without a palette painted")
	}
}

// appendPainted, on a session's frame: each reset of the default
// colours sets the palette's (after the reset, before what follows it,
// so a colour after it wins); other sequences pass as they are.
func TestAppendPainted(t *testing.T) {
	usePaintTheme(t, paintTheme, true)
	f := "38;2;192;193;194;48;2;5;10;5"
	for _, c := range []struct{ in, want string }{
		{"plain", "plain"},
		{"\x1b[0m\x1b[H\x1b[2J", "\x1b[0;" + f + "m\x1b[H\x1b[2J"},
		{"\x1b[m", "\x1b[0;" + f + "m"},
		{"\x1b[1;39;44m", "\x1b[1;39;38;2;192;193;194;44m"},
		{"\x1b[49m", "\x1b[49;48;2;5;10;5m"},
		{"\x1b[0;38;2;1;2;3;48;5;0m", "\x1b[0;" + f + ";38;2;1;2;3;48;5;0m"},
		{"\x1b[38:2::1:2:3m", "\x1b[38:2::1:2:3m"},
		{"\x1b[>4;2m\x1b[0 q\x1b[?25l\x1b[3;1H", "\x1b[>4;2m\x1b[0 q\x1b[?25l\x1b[3;1H"},
		{"\x1b]2;a\x1b[0mb\a\x1b[0m", "\x1b]2;a\x1b[0mb\a\x1b[0;" + f + "m"},
		{"x\x1b[", "x\x1b["},
		{"x\x1b", "x\x1b"},
	} {
		if got := string(th().appendPainted(nil, []byte(c.in))); got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
	if got := th().fillReset(); got != "\x1b[0;"+f+"m" {
		t.Errorf("fillReset %q", got)
	}
}

// Attached, the frame drawn in full paints every cell outside the pane
// (the sidebar, the info panel, the status bar and its rule, the gaps)
// in the palette's background, and a scheme switch draws it all again
// in the other's.
func TestPaintAttach(t *testing.T) {
	tt := usePaintTheme(t, paintTheme, true)
	c := infoClient(t, 200)
	mirror, err := emu.New(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c.statusText, c.focus = "bar", &pane{mirror: mirror}
	frame := func() *bgScreen {
		t.Helper()
		c.refill(th(), nil)
		b, err := c.frameSplit(nil)
		if err != nil {
			t.Fatal(err)
		}
		s := newBgScreen(c.cols, c.rows)
		s.write(c.paint(th(), b))
		return s
	}
	s := frame()
	if bad := s.not("#050a05", "#100001", "#100002"); len(bad) > 0 {
		t.Fatalf("%d cells not painted, first %v", len(bad), bad[:min(len(bad), 5)])
	}
	for _, x := range []int{0, c.sideX() + c.sideW - 1, c.infoX(), c.cols - 1} {
		if bg := s.bg[c.rows-1][x]; bg != "#050a05" {
			t.Errorf("status bar at %d: bg %q", x, bg)
		}
	}

	// Nothing changed: no frame. The light scheme: all of it again.
	if b, _ := c.frameSplit(nil); len(c.paint(th(), b)) != 0 {
		t.Errorf("an unchanged window drew %q", b)
	}
	setTheme(tt, false)
	if bad := frame().not("#f0f1f2", "#100001", "#100002"); len(bad) > 0 {
		t.Fatalf("light: %d cells not painted, first %v", len(bad), bad[:min(len(bad), 5)])
	}

	// The built-in theme: the terminal's own colours, as drawn.
	setTheme(builtinTheme, true)
	c.refill(th(), nil)
	b, _ := c.frameSplit(nil)
	if got := c.paint(th(), b); string(got) != string(b) {
		t.Errorf("the built-in theme painted the frame")
	}
}

// bgScreen is the background colour of each cell of a w×h terminal
// that output is written to: just what a frame's painting changes (SGR
// backgrounds, cursor moves, printing and erases, which take the
// current background).
type bgScreen struct {
	w, h, x, y int
	pen        string
	bg         [][]string
}

func newBgScreen(w, h int) *bgScreen {
	s := &bgScreen{w: w, h: h, bg: make([][]string, h)}
	for y := range s.bg {
		s.bg[y] = make([]string, w)
		for x := range s.bg[y] {
			s.bg[y][x] = "unset"
		}
	}
	return s
}

// not lists the cells whose background is none of want.
func (s *bgScreen) not(want ...string) []string {
	var bad []string
	for y, row := range s.bg {
		for x, bg := range row {
			if !slices.Contains(want, bg) {
				bad = append(bad, fmt.Sprintf("%d,%d %s", x, y, bg))
			}
		}
	}
	return bad
}

func (s *bgScreen) fill(y, x0, x1 int) {
	for x := max(x0, 0); x < min(x1, s.w); x++ {
		if y >= 0 && y < s.h {
			s.bg[y][x] = s.pen
		}
	}
}

func (s *bgScreen) write(b []byte) {
	state := byte(0)
	in := string(b)
	for len(in) > 0 {
		seq, width, n, ns := ansi.DecodeSequence(in, state, nil)
		state, in = ns, in[n:]
		if width > 0 {
			s.fill(s.y, s.x, s.x+width)
			s.x += width
			continue
		}
		if !strings.HasPrefix(seq, "\x1b[") || len(seq) < 3 {
			continue
		}
		args, final := seq[2:len(seq)-1], seq[len(seq)-1]
		num := func(i, dflt int) int {
			ps := strings.Split(args, ";")
			if i < len(ps) {
				if n, err := strconv.Atoi(ps[i]); err == nil && n > 0 {
					return n
				}
			}
			return dflt
		}
		switch final {
		case 'H':
			s.y, s.x = num(0, 1)-1, num(1, 1)-1
		case 'J':
			if args == "2" {
				for y := range s.bg {
					s.fill(y, 0, s.w)
				}
			}
		case 'K':
			s.fill(s.y, s.x, s.w)
		case 'X':
			s.fill(s.y, s.x, s.x+num(0, 1))
		case 'm':
			if !isSGR(seq) || strings.ContainsAny(args, "<=>?") {
				continue
			}
			ps := strings.Split(args, ";")
			for i := 0; i < len(ps); i++ {
				switch p := ps[i]; p {
				case "", "0", "49":
					s.pen = ""
				case "48", "38", "58":
					if i+4 < len(ps) && ps[i+1] == "2" {
						if p == "48" {
							r, _ := strconv.Atoi(ps[i+2])
							g, _ := strconv.Atoi(ps[i+3])
							b, _ := strconv.Atoi(ps[i+4])
							s.pen = fmt.Sprintf("#%02x%02x%02x", r, g, b)
						}
						i += 4
					} else if i+2 < len(ps) && ps[i+1] == "5" {
						if p == "48" {
							s.pen = "index " + ps[i+2]
						}
						i += 2
					}
				default:
					if (len(p) == 2 && p[0] == '4' || len(p) == 3 && p[:2] == "10") && p[len(p)-1] >= '0' && p[len(p)-1] <= '7' {
						s.pen = "ansi " + p
					}
				}
			}
		}
	}
}
