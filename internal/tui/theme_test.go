package tui

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// The built-in table must draw exactly what the old package-level styles did.
func TestBuiltinThemeMatchesOldStyles(t *testing.T) {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	want := map[string]lipgloss.Style{
		"Head":   lipgloss.NewStyle().Bold(true),
		"Sel":    lipgloss.NewStyle().Reverse(true),
		"Faint":  lipgloss.NewStyle().Faint(true),
		"Accent": accent,
		"Title":  accent.Bold(true),
		"Bad":    lipgloss.NewStyle().Foreground(lipgloss.Red),
		"Warn":   lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		"Good":   lipgloss.NewStyle().Foreground(lipgloss.Green),
		"Info":   lipgloss.NewStyle().Foreground(lipgloss.Blue),
		"Plain":  lipgloss.NewStyle(),
	}
	for _, dark := range []bool{true, false} {
		th, err := themeFor(builtinTheme, dark, true)
		if err != nil {
			t.Fatal(err)
		}
		v := reflect.ValueOf(*th)
		for name, w := range want {
			got := v.FieldByName(name).Interface().(lipgloss.Style)
			if got.Render("x") != w.Render("x") || !reflect.DeepEqual(got.GetForeground(), w.GetForeground()) {
				t.Errorf("dark=%v %s differs", dark, name)
			}
		}
	}
}

func TestParseThemeAndFallback(t *testing.T) {
	tt, err := parseTheme([]byte(`
[dark]
accent = { fg = ["cyan", "#88c0d0"] }
[light]
accent = { fg = ["blue", "#5e81ac"], bold = true }
`))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := themeFor(tt, true, true)
	if !reflect.DeepEqual(d.Accent.GetForeground(), lipgloss.Color("#88c0d0")) {
		t.Errorf("dark truecolor accent = %v", d.Accent.GetForeground())
	}
	d, _ = themeFor(tt, true, false)
	if !reflect.DeepEqual(d.Accent.GetForeground(), lipgloss.Cyan) {
		t.Errorf("dark ansi accent = %v", d.Accent.GetForeground())
	}
	l, _ := themeFor(tt, false, false)
	if !l.Accent.GetBold() {
		t.Error("light accent not bold")
	}
	if !l.Sel.GetReverse() {
		t.Error("unset role did not fall back to built-in")
	}
}

func TestBadThemeKeepsBuiltin(t *testing.T) {
	tt, err := parseTheme([]byte("[dark]\nbad = { fg = [\"#12\"] }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := setTheme(tt, true); err == nil {
		t.Error("bad colour accepted")
	}
	if !reflect.DeepEqual(th().Bad.GetForeground(), lipgloss.Red) {
		t.Error("built-in look not kept")
	}
}

// The sidebar's focused row comes from the theme: the default keeps its
// cyan bar, and nord's has a foreground that differs from its background.
func TestSelFocusFromTheme(t *testing.T) {
	data, err := os.ReadFile("../../examples/mods/nord/theme.toml")
	if err != nil {
		t.Fatal(err)
	}
	tt, err := parseTheme(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{true, false} {
		th, err := themeFor(tt, dark, true)
		if err != nil {
			t.Fatal(err)
		}
		fg, bg := th.SelFocus.GetForeground(), th.SelFocus.GetBackground()
		if fg == nil || bg == nil || fg == bg || th.SelFocus.GetReverse() {
			t.Errorf("nord dark=%v focused row: fg %v bg %v", dark, fg, bg)
		}
	}
	// A theme that sets sel but not sel_focus falls back to its sel.
	tt2, _ := parseTheme([]byte("[dark]\nsel = { fg = [\"black\"], bg = [\"white\"] }\n"))
	th2, _ := themeFor(tt2, true, false)
	if th2.SelFocus.GetBackground() != th2.Sel.GetBackground() {
		t.Error("sel_focus should fall back to sel")
	}
	if !builtinRoles.SelFocus.Reverse {
		t.Error("default sel_focus is the cyan bar")
	}
}

// A theme's pane palette: nord's sets the default colours and all 16 in
// both schemes, on a truecolor terminal only; the built-in theme has none.
func TestPanePalette(t *testing.T) {
	data, err := os.ReadFile("../../examples/mods/nord/theme.toml")
	if err != nil {
		t.Fatal(err)
	}
	tt, err := parseTheme(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{true, false} {
		th, err := themeFor(tt, dark, true)
		if err != nil {
			t.Fatal(err)
		}
		p := th.Pane
		if p == nil || p.Fg == nil || p.Bg == nil || *p.Fg == *p.Bg {
			t.Fatalf("nord dark=%v pane: %+v", dark, p)
		}
		for i, c := range p.ANSI {
			if c == nil {
				t.Errorf("nord dark=%v pane colour %d unset", dark, i)
			}
		}
		if th, _ := themeFor(tt, dark, false); th.Pane != nil {
			t.Errorf("nord dark=%v pane palette without truecolor", dark)
		}
	}
	if d, _ := themeFor(tt, true, true); *d.Pane.Bg != (emu.RGB{R: 0x24, G: 0x29, B: 0x33}) || *d.Pane.ANSI[1] != (emu.RGB{R: 0xbf, G: 0x61, B: 0x6a}) {
		t.Errorf("nord dark pane: bg %v red %v", *d.Pane.Bg, *d.Pane.ANSI[1])
	}
	for _, dark := range []bool{true, false} {
		if th, _ := themeFor(builtinTheme, dark, true); th.Pane != nil {
			t.Errorf("built-in theme dark=%v has a pane palette", dark)
		}
	}

	// Only fg: the rest stays the terminal's.
	tt, err = parseTheme([]byte("[dark.pane]\nfg = \"#ffffff\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := themeFor(tt, true, true); d.Pane == nil || d.Pane.Bg != nil || d.Pane.ANSI[0] != nil || *d.Pane.Fg != (emu.RGB{R: 255, G: 255, B: 255}) {
		t.Errorf("fg only: %+v", d.Pane)
	}

	for _, tc := range []struct{ toml, want string }{
		{"[dark.pane]\nbg = \"red\"", `pane bg: bad colour "red" (want #rrggbb)`},
		{"[light.pane]\nfg = \"#12345\"", `pane fg: bad colour "#12345"`},
		{"[dark.pane]\npalette = [\"#000000\"]", "pane palette: takes the 16 ANSI colours, got 1"},
		{"[dark.pane]\npalette = [" + strings.Repeat(`"#000000", `, 15) + `"#00000g"]`, `pane palette 15: bad colour "#00000g"`},
		{"[dark.pane]\ncursor = \"#000000\"", "unknown key dark.pane.cursor"},
	} {
		tt, err := parseTheme([]byte(tc.toml))
		for _, dark := range []bool{false, true} {
			if err == nil {
				_, err = themeFor(tt, dark, false)
			}
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.toml, err, tc.want)
		}
		if err := mod.DataChecks[mod.KindTheme]([]byte(tc.toml)); err == nil {
			t.Errorf("%s: the data check passes it", tc.toml)
		}
	}
}
