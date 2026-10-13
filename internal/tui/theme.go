package tui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/emu"
	tmtheme "github.com/theclifmeister/terminatr/internal/theme"
)

// The dashboard's look. Colours are the terminal's 16 ANSI colours, so
// they follow the user's own theme, light or dark; Bubble Tea drops them
// under NO_COLOR or a terminal without colour. Every state also has its
// own glyph, so colour is never the only signal.

// Theme is the dashboard's semantic styles, one per role. It is resolved
// per console from a themeTable, for the console's light or dark scheme;
// th() is the current one.
type Theme struct {
	Head, Sel, SelFocus, Faint, Accent, Title, Bad, Warn, Good, Info, Plain lipgloss.Style
	// Pane is the panes' palette, nil to keep their colours symbolic (no
	// pane table, or a terminal without truecolor).
	Pane *emu.Palette
}

// The theme's data (internal/theme): its role specs, a scheme's role
// set with the pane palette, and the table of both schemes.
type (
	roleSpec   = tmtheme.Role
	roleSet    = tmtheme.RoleSet
	themeTable = tmtheme.Table
)

// parseTheme reads a theme's TOML; a key it doesn't know is an error.
var parseTheme = tmtheme.Parse

// builtinTheme is the built-in look over the terminal's 16 ANSI colours:
// the bundled default-theme mod's theme.toml (mods design §11), every
// role set in both schemes. builtinRoles are its dark roles, to which a
// mod's role left out falls back.
var (
	builtinTheme = mustTheme(tmtheme.Bundled())
	builtinRoles = builtinTheme.Dark
)

// mustTheme is the bundled theme; tm built without a whole one is a bug.
func mustTheme(tt themeTable, err error) themeTable {
	if err != nil {
		panic("bundled theme: " + err.Error())
	}
	for _, rs := range []roleSet{tt.Light, tt.Dark} {
		if missing := rs.Missing(); len(missing) > 0 {
			panic("bundled theme: no " + strings.Join(missing, ", "))
		}
	}
	return tt
}

// parseColor reads one colour: an ANSI name, a 0-255 index or #rrggbb.
func parseColor(s string) (color.Color, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	n, ok := tmtheme.ANSINames[s]
	if !ok {
		if i, err := strconv.Atoi(s); err == nil && i >= 0 && i <= 255 {
			n, ok = i, true
		}
	}
	if ok {
		if n < 16 {
			return ansi.BasicColor(n), nil
		}
		return lipgloss.ANSIColor(n), nil
	}
	if len(s) == 7 && s[0] == '#' {
		if _, err := strconv.ParseUint(s[1:], 16, 24); err == nil {
			return lipgloss.Color(s), nil
		}
	}
	return nil, fmt.Errorf("bad colour %q", s)
}

// pickColor is the spec's colour: the truecolor entry on a truecolor
// terminal, else the first (the ANSI fallback). nil for no colour.
func pickColor(c []string, truecolor bool) (color.Color, error) {
	switch {
	case len(c) == 0:
		return nil, nil
	case len(c) > 2:
		return nil, fmt.Errorf("a colour takes at most two entries, got %d", len(c))
	case len(c) == 2 && truecolor:
		return parseColor(c[1])
	}
	return parseColor(c[0])
}

// specStyle is a role spec as a style.
func specStyle(r *roleSpec, truecolor bool) (lipgloss.Style, error) {
	st := lipgloss.NewStyle()
	fg, err := pickColor(r.Fg, truecolor)
	if err != nil {
		return st, err
	}
	bg, err := pickColor(r.Bg, truecolor)
	if err != nil {
		return st, err
	}
	if fg != nil {
		st = st.Foreground(fg)
	}
	if bg != nil {
		st = st.Background(bg)
	}
	return st.Bold(r.Bold).Faint(r.Faint).Reverse(r.Reverse), nil
}

// resolveRoles is the Theme for one scheme: each role from the set, else
// the built-in one. A bad colour is an error naming the role.
func resolveRoles(rs roleSet, truecolor bool) (*Theme, error) {
	var t Theme
	roles := []struct {
		name string
		spec *roleSpec
		dflt *roleSpec
		out  *lipgloss.Style
	}{
		{"head", rs.Head, builtinRoles.Head, &t.Head},
		{"sel", rs.Sel, builtinRoles.Sel, &t.Sel},
		{"sel_focus", rs.SelFocusSpec(), builtinRoles.SelFocus, &t.SelFocus},
		{"faint", rs.Faint, builtinRoles.Faint, &t.Faint},
		{"accent", rs.Accent, builtinRoles.Accent, &t.Accent},
		{"title", rs.Title, builtinRoles.Title, &t.Title},
		{"bad", rs.Bad, builtinRoles.Bad, &t.Bad},
		{"warn", rs.Warn, builtinRoles.Warn, &t.Warn},
		{"good", rs.Good, builtinRoles.Good, &t.Good},
		{"info", rs.Info, builtinRoles.Info, &t.Info},
		{"plain", rs.Plain, builtinRoles.Plain, &t.Plain},
	}
	for _, r := range roles {
		spec := r.spec
		if spec == nil {
			spec = r.dflt
		}
		st, err := specStyle(spec, truecolor)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.name, err)
		}
		*r.out = st
	}
	if rs.Pane != nil {
		pal, err := rs.Pane.Resolve()
		if err != nil {
			return nil, err
		}
		if truecolor {
			t.Pane = emuPalette(pal)
		}
	}
	return &t, nil
}

// emuPalette is a theme's pane palette as the renderer's.
func emuPalette(p *tmtheme.Palette) *emu.Palette {
	out := &emu.Palette{Fg: (*emu.RGB)(p.Fg), Bg: (*emu.RGB)(p.Bg)}
	for i, c := range p.ANSI {
		out.ANSI[i] = (*emu.RGB)(c)
	}
	return out
}

// themeFor is the Theme a table gives a console of the given scheme.
func themeFor(tt themeTable, dark, truecolor bool) (*Theme, error) {
	if dark {
		return resolveRoles(tt.Dark, truecolor)
	}
	return resolveRoles(tt.Light, truecolor)
}

// theme is this console's Theme.
var theme atomic.Pointer[Theme]

func init() { setTheme(builtinTheme, true) }

// th is this console's theme.
func th() *Theme { return theme.Load() }

// setTheme makes the table's roles for the scheme this console's theme.
// A table that doesn't resolve leaves the built-in look.
func setTheme(tt themeTable, dark bool) error {
	t, err := themeFor(tt, dark, trueColor())
	if err != nil {
		t, _ = themeFor(builtinTheme, dark, false)
	}
	theme.Store(t)
	return err
}

// stateLook is how a state word is drawn: its glyph, in this console's
// icon set, and colour.
func stateLook(state string) (string, lipgloss.Style) {
	word, _, _ := strings.Cut(state, " ")
	i := ic()
	switch word {
	case "working", "started":
		return i.working, th().Good
	case "blocked":
		return i.blocked, th().Bad.Bold(true)
	case "idle", "open", "ready":
		return i.idle, th().Plain
	case "starting":
		return i.starting, th().Info
	case "running":
		return i.running, th().Plain
	case "review":
		return i.review, th().Warn
	case "done":
		return i.done, th().Good
	case "—", "":
		return "", th().Faint
	}
	return i.other, th().Faint // stopped, exited, resolved and the like
}

// stateText is a state with its glyph, as a column shows it.
func stateText(state string) string {
	if g, _ := stateLook(state); g != "" {
		return g + " " + state
	}
	return state
}

// markStyle colours a row's marker: ! blocked, ? needs you.
func markStyle(mark string) lipgloss.Style {
	switch strings.TrimSpace(mark) {
	case "!":
		return th().Bad.Bold(true)
	case "?":
		return th().Warn.Bold(true)
	}
	return th().Plain
}

// bar is a five-cell progress bar: "▰▰▱▱▱" ("##---" in ascii).
func bar(pct int) string {
	n := min(max((pct+10)/20, 0), 5)
	return strings.Repeat(ic().barOn, n) + strings.Repeat(ic().barOff, 5-n)
}

// pctOf is done out of total as a percent, -1 without a total.
func pctOf(done, total int) int {
	if total <= 0 {
		return -1
	}
	return done * 100 / total
}

// keysLine styles a footer's "key label · key label" list: keys in the
// accent colour, labels faint.
func keysLine(keys string) string {
	parts := strings.Split(keys, " · ")
	for i, p := range parts {
		k, label, ok := strings.Cut(p, " ")
		if !ok {
			parts[i] = th().Accent.Render(p)
			continue
		}
		parts[i] = th().Accent.Render(k) + " " + th().Faint.Render(label)
	}
	return strings.Join(parts, th().Faint.Render(" · "))
}

// todoGlyph is a todo or step's box: ✓ done, ◐ in progress, ○ to do.
func todoGlyph(status string) string {
	switch status {
	case "completed", "done":
		return th().Good.Render(ic().todoDone)
	case "in_progress":
		return th().Warn.Render(ic().todoNow)
	}
	return th().Faint.Render(ic().todoOpen)
}

// countLabel is a section header with its count: "NEEDS YOU 2".
func countLabel(title string, n int) string {
	if n <= 0 {
		return title
	}
	return fmt.Sprintf("%s %d", title, n)
}

// plainUnder lays the plain role under s, one line of the console's chrome:
// text drawn in no other role (no colour of its own) is drawn plain, so
// a theme's plain colour reaches it too, not the terminal's default one.
// The built-in plain draws nothing, so s stays as it is there.
func plainUnder(s string) string {
	pre := plainSGR()
	if pre == "" || s == "" {
		return s
	}
	var b strings.Builder
	b.WriteString(pre)
	fg := true
	state := byte(0)
	for len(s) > 0 {
		seq, width, n, ns := ansi.DecodeSequence(s, state, nil)
		state, s = ns, s[n:]
		b.WriteString(seq)
		if width == 0 && isSGR(seq) {
			if fg = sgrFg(seq, fg); !fg {
				b.WriteString(pre)
				fg = true
			}
		}
	}
	b.WriteString(reset)
	return b.String()
}

// plainSGR is the SGR sequence that turns the plain role on, "" when it
// draws nothing.
func plainSGR() string {
	pre, _, _ := strings.Cut(th().Plain.Render("x"), "x")
	return pre
}

// isSGR says whether seq is an SGR (colour and attribute) sequence.
func isSGR(seq string) bool {
	return strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m")
}

// sgrFg is whether a foreground colour is set after the SGR sequence
// seq, fg whether one was before it.
func sgrFg(seq string, fg bool) bool {
	ps := strings.Split(seq[2:len(seq)-1], ";")
	for i := 0; i < len(ps); i++ {
		switch p := ps[i]; {
		case p == "" || p == "0" || p == "39":
			fg = false
		case p == "38" || p == "48" || p == "58":
			if p == "38" {
				fg = true
			}
			switch {
			case i+1 < len(ps) && ps[i+1] == "5":
				i += 2
			case i+1 < len(ps) && ps[i+1] == "2":
				i += 4
			}
		case len(p) == 2 && (p[0] == '3' || p[0] == '9') && p[1] >= '0' && p[1] <= '7':
			fg = true
		}
	}
	return fg
}
