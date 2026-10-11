package tui

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
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
