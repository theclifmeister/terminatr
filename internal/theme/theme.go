// Package theme is a theme mod's theme.toml as data (docs/SPEC.md §5.3):
// a role set per colour scheme and the pane palette. The console styles
// its roles (internal/tui); the server reads the same data to hand an
// agent the theme's colours (Agent, §8.6).
package theme

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// Role is one role as data: foreground and background colours (each an
// ANSI name, a 0-255 index or #rrggbb; a second entry is the truecolor
// choice, the first its ANSI fallback) and attributes.
type Role struct {
	Fg      []string `toml:"fg"`
	Bg      []string `toml:"bg"`
	Bold    bool     `toml:"bold"`
	Faint   bool     `toml:"faint"`
	Reverse bool     `toml:"reverse"`
}

// RoleSet is every role's spec for one scheme. A role left out falls back
// to the built-in one.
type RoleSet struct {
	Head *Role `toml:"head"`
	Sel  *Role `toml:"sel"`
	// SelFocus is optional: the sidebar's keyboard row while it has the focus.
	SelFocus *Role `toml:"sel_focus"`
	Faint    *Role `toml:"faint"`
	Accent   *Role `toml:"accent"`
	Title    *Role `toml:"title"`
	Bad      *Role `toml:"bad"`
	Warn     *Role `toml:"warn"`
	Good     *Role `toml:"good"`
	Info     *Role `toml:"info"`
	Plain    *Role `toml:"plain"`
	// Pane is the panes' palette (mod API 1.6); none in the built-in theme.
	Pane *Pane `toml:"pane"`
}

// Pane is a scheme's pane palette, [light.pane] or [dark.pane]: the
// colours the panes' programs' default foreground and background and 16
// ANSI colours are drawn with, each #rrggbb. Whatever it leaves out
// stays the outer terminal's own.
type Pane struct {
	Fg      string   `toml:"fg"`
	Bg      string   `toml:"bg"`
	Palette []string `toml:"palette"`
}

// RGB is a 24-bit colour (RGB's layout: this package stays free of
// the emulator, so agents can use it).
type RGB struct{ R, G, B uint8 }

// Palette is a pane palette's colours (emu.Palette's layout), nil where
// it sets none.
type Palette struct {
	Fg, Bg *RGB
	ANSI   [16]*RGB
}

// Resolve is the spec's colours; a bad colour is an error naming its key.
func (p *Pane) Resolve() (*Palette, error) {
	var pal Palette
	var err error
	if pal.Fg, err = ParseRGB(p.Fg); err != nil {
		return nil, fmt.Errorf("pane fg: %w", err)
	}
	if pal.Bg, err = ParseRGB(p.Bg); err != nil {
		return nil, fmt.Errorf("pane bg: %w", err)
	}
	if n := len(p.Palette); n != 0 && n != 16 {
		return nil, fmt.Errorf("pane palette: takes the 16 ANSI colours, got %d", n)
	}
	for i, c := range p.Palette {
		if pal.ANSI[i], err = ParseRGB(c); err != nil {
			return nil, fmt.Errorf("pane palette %d: %w", i, err)
		}
	}
	return &pal, nil
}

// ParseRGB reads #rrggbb; nil for "".
func ParseRGB(s string) (*RGB, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) == 7 && s[0] == '#' {
		if v, err := strconv.ParseUint(s[1:], 16, 24); err == nil {
			return &RGB{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, nil
		}
	}
	return nil, fmt.Errorf("bad colour %q (want #rrggbb)", s)
}

// Table is a theme as data: a role set per colour scheme.
type Table struct {
	Light RoleSet `toml:"light"`
	Dark  RoleSet `toml:"dark"`
}

// Scheme is the role set for the dark or the light scheme.
func (t Table) Scheme(dark bool) RoleSet {
	if dark {
		return t.Dark
	}
	return t.Light
}

// Parse reads a theme's TOML ([light] and [dark] role tables); a key it
// doesn't know is an error.
func Parse(data []byte) (Table, error) {
	var tt Table
	md, err := toml.Decode(string(data), &tt)
	if err != nil {
		return tt, err
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return tt, fmt.Errorf("unknown key %s", strings.Join(keys, ", "))
	}
	return tt, nil
}

// Bundled is the built-in theme: the bundled default-theme mod's
// theme.toml (mods design §11). It doesn't parse only in a broken build.
var Bundled = sync.OnceValues(func() (Table, error) {
	return Parse(mod.BundledData(mod.BundledTheme, mod.KindTheme))
})

// Load reads mod id's theme from the registry, unstyled: the console
// checks its colours as it styles them.
func Load(r *mod.Registry, id string) (Table, error) {
	m, err := r.Provider(mod.KindTheme, id)
	if err != nil {
		return Table{}, err
	}
	data, err := m.Data(mod.KindTheme)
	if err != nil {
		return Table{}, err
	}
	tt, err := Parse(data)
	if err != nil {
		return Table{}, fmt.Errorf("%s theme: %w", id, err)
	}
	return tt, nil
}

// Roles are the role names, in theme.toml's order, each with its spec in
// rs.
func (rs RoleSet) Roles() []struct {
	Name string
	Spec *Role
} {
	return []struct {
		Name string
		Spec *Role
	}{{"head", rs.Head}, {"sel", rs.Sel}, {"sel_focus", rs.SelFocus}, {"faint", rs.Faint}, {"accent", rs.Accent}, {"title", rs.Title},
		{"bad", rs.Bad}, {"warn", rs.Warn}, {"good", rs.Good}, {"info", rs.Info}, {"plain", rs.Plain}}
}

// Missing are the roles rs leaves out.
func (rs RoleSet) Missing() []string {
	var out []string
	for _, r := range rs.Roles() {
		if r.Spec == nil {
			out = append(out, r.Name)
		}
	}
	return out
}

// SelFocusSpec is the focused-selection spec: the set's own, else its sel
// (so a theme that restyles sel never gets the built-in colours over
// it), else nil for the built-in one.
func (rs RoleSet) SelFocusSpec() *Role {
	if rs.SelFocus != nil {
		return rs.SelFocus
	}
	return rs.Sel
}

// ANSINames are the 16 ANSI colours' names in a theme, by index.
var ANSINames = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3, "blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"brightblack": 8, "brightred": 9, "brightgreen": 10, "brightyellow": 11,
	"brightblue": 12, "brightmagenta": 13, "brightcyan": 14, "brightwhite": 15,
}

// Agent is a theme's colours for one scheme, for an agent that takes a
// theme of its own (docs/SPEC.md §8.6), each "#rrggbb" or "" where the
// theme gives none: every role's foreground and background (by name),
// and the pane palette.
type Agent struct {
	Scheme         string // "dark" or "light"
	Fg, Bg         map[string]string
	PaneFg, PaneBg string
	ANSI           [16]string
}

// AgentColors are tt's colours for the scheme, nil when the scheme has
// no pane palette (or a bad one): without it the panes keep the
// terminal's colours, and an agent its own theme. A role's colour is its
// truecolor choice; an ANSI one is the pane palette's entry ("" where it
// sets none), a 256-colour index its xterm value. A role left out is the
// built-in one's, sel_focus its sel's.
func AgentColors(tt Table, dark bool) *Agent {
	rs := tt.Scheme(dark)
	if rs.Pane == nil {
		return nil
	}
	pal, err := rs.Pane.Resolve()
	if err != nil {
		return nil
	}
	a := &Agent{Scheme: "light", Fg: map[string]string{}, Bg: map[string]string{}, PaneFg: hexOf(pal.Fg), PaneBg: hexOf(pal.Bg)}
	if dark {
		a.Scheme = "dark"
	}
	for i, c := range pal.ANSI {
		a.ANSI[i] = hexOf(c)
	}
	builtin, _ := Bundled()
	def := builtin.Dark.Roles()
	rs.SelFocus = rs.SelFocusSpec()
	for i, r := range rs.Roles() {
		spec := r.Spec
		if spec == nil {
			spec = def[i].Spec
		}
		if spec == nil {
			a.Fg[r.Name], a.Bg[r.Name] = "", ""
			continue
		}
		a.Fg[r.Name], a.Bg[r.Name] = a.color(spec.Fg), a.color(spec.Bg)
	}
	return a
}

// color is one role colour as #rrggbb: the truecolor choice, else the
// fallback through the pane palette; "" for none.
func (a *Agent) color(c []string) string {
	if len(c) == 0 {
		return ""
	}
	s := strings.ToLower(strings.TrimSpace(c[len(c)-1]))
	if rgb, err := ParseRGB(s); err == nil && rgb != nil {
		return hexOf(rgb)
	}
	n, ok := ANSINames[s]
	if !ok {
		i, err := strconv.Atoi(s)
		if err != nil || i < 0 || i > 255 {
			return ""
		}
		n = i
	}
	if n < 16 {
		return a.ANSI[n]
	}
	return hexOf(xterm256(n))
}

// xterm256 is a 256-colour index's xterm value (16-255).
func xterm256(n int) *RGB {
	if n >= 232 {
		v := uint8(8 + 10*(n-232))
		return &RGB{R: v, G: v, B: v}
	}
	n -= 16
	level := func(i int) uint8 {
		if i == 0 {
			return 0
		}
		return uint8(55 + 40*i)
	}
	return &RGB{R: level(n / 36), G: level(n / 6 % 6), B: level(n % 6)}
}

func hexOf(c *RGB) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}
