package tui

import (
	"fmt"
	"image/color"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"
)

// The dashboard's look. Colours are the terminal's 16 ANSI colours, so
// they follow the user's own theme, light or dark; Bubble Tea drops them
// under NO_COLOR or a terminal without colour. Every state also has its
// own glyph, so colour is never the only signal.

// Theme is the dashboard's semantic styles, one per role. It is resolved
// per console from a themeTable, for the console's light or dark scheme;
// th() is the current one.
type Theme struct {
	Head, Sel, Faint, Accent, Title, Bad, Warn, Good, Info, Plain lipgloss.Style
}

// roleSpec is one role as data: foreground and background colours (each
// an ANSI name, a 0-255 index or #rrggbb; a second entry is the
// truecolor choice, the first its ANSI fallback) and attributes.
type roleSpec struct {
	Fg      []string `toml:"fg"`
	Bg      []string `toml:"bg"`
	Bold    bool     `toml:"bold"`
	Faint   bool     `toml:"faint"`
	Reverse bool     `toml:"reverse"`
}

// roleSet is every role's spec for one scheme. A role left out falls back
// to the built-in one.
type roleSet struct {
	Head   *roleSpec `toml:"head"`
	Sel    *roleSpec `toml:"sel"`
	Faint  *roleSpec `toml:"faint"`
	Accent *roleSpec `toml:"accent"`
	Title  *roleSpec `toml:"title"`
	Bad    *roleSpec `toml:"bad"`
	Warn   *roleSpec `toml:"warn"`
	Good   *roleSpec `toml:"good"`
	Info   *roleSpec `toml:"info"`
	Plain  *roleSpec `toml:"plain"`
}

// themeTable is a theme as data: a role set per colour scheme.
type themeTable struct {
	Light roleSet `toml:"light"`
	Dark  roleSet `toml:"dark"`
}

// builtinRoles is the built-in look over the terminal's 16 ANSI colours.
var builtinRoles = roleSet{
	Head:   &roleSpec{Bold: true},
	Sel:    &roleSpec{Reverse: true},
	Faint:  &roleSpec{Faint: true},
	Accent: &roleSpec{Fg: []string{"cyan"}},
	Title:  &roleSpec{Fg: []string{"cyan"}, Bold: true},
	Bad:    &roleSpec{Fg: []string{"red"}},
	Warn:   &roleSpec{Fg: []string{"yellow"}},
	Good:   &roleSpec{Fg: []string{"green"}},
	Info:   &roleSpec{Fg: []string{"blue"}},
	Plain:  &roleSpec{},
}

var builtinTheme = themeTable{Light: builtinRoles, Dark: builtinRoles}

var ansiNames = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"brightblack": 8, "brightred": 9, "brightgreen": 10, "brightyellow": 11,
	"brightblue": 12, "brightmagenta": 13, "brightcyan": 14, "brightwhite": 15,
}

// parseColor reads one colour: an ANSI name, a 0-255 index or #rrggbb.
func parseColor(s string) (color.Color, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	n, ok := ansiNames[s]
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

func (r *roleSpec) style(truecolor bool) (lipgloss.Style, error) {
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

// resolve is the Theme for one scheme: each role from the set, else the
// built-in one. A bad colour is an error naming the role.
func (rs roleSet) resolve(truecolor bool) (*Theme, error) {
	var t Theme
	roles := []struct {
		name string
		spec *roleSpec
		dflt *roleSpec
		out  *lipgloss.Style
	}{
		{"head", rs.Head, builtinRoles.Head, &t.Head},
		{"sel", rs.Sel, builtinRoles.Sel, &t.Sel},
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
		st, err := spec.style(truecolor)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.name, err)
		}
		*r.out = st
	}
	return &t, nil
}

// parseTheme reads a theme's TOML ([light] and [dark] role tables).
func parseTheme(data []byte) (themeTable, error) {
	var tt themeTable
	if _, err := toml.Decode(string(data), &tt); err != nil {
		return tt, err
	}
	return tt, nil
}

// themeFor is the Theme a table gives a console of the given scheme.
func themeFor(tt themeTable, dark, truecolor bool) (*Theme, error) {
	if dark {
		return tt.Dark.resolve(truecolor)
	}
	return tt.Light.resolve(truecolor)
}

// theme is this console's Theme.
var theme atomic.Pointer[Theme]

func init() { setTheme(builtinTheme, true) }

// th is this console's theme.
func th() *Theme { return theme.Load() }

// setTheme makes the table's roles for the scheme this console's theme.
// A table that doesn't resolve leaves the built-in look.
func setTheme(tt themeTable, dark bool) error {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	t, err := themeFor(tt, dark, ct == "truecolor" || ct == "24bit")
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
