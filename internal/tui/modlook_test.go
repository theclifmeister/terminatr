package tui

import (
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// nord is the design's example A (mods design §10): a theme and an icon
// set.
var nord = map[string]string{
	"mod.toml": "id = \"nord\"\nname = \"Nord\"\nversion = \"0.4.0\"\napi = \"1.6\"\n[provides]\ntheme = \"theme.toml\"\nicons = \"icons.toml\"\n",
	"theme.toml": `[dark]
accent = { fg = ["cyan", "#88c0d0"] }
bad    = { fg = ["red", "#bf616a"], bold = true }
pane   = { bg = "#2e3440" }
[light]
accent = { fg = ["cyan", "#5e81ac"] }
`,
	"icons.toml": "base = \"unicode\"\nworking = \"◉\"\nreview = \"◇\"\n",
}

// writeConfig writes config.toml.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	path, _ := config.Path()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestThemeMod: [ui] theme names an enabled mod: the console takes its
// roles for its scheme, the others stay built in; light follows the
// terminal; a theme that doesn't resolve, or a mod that is off, leaves
// the built-in look and says why.
func TestThemeMod(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv("COLORTERM", "truecolor")
	freshConsole(t)
	defaultKeymap(t)
	installMod(t, "nord", nord)
	writeConfig(t, "[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = true\n")
	if err := loadLook(); err != nil {
		t.Fatal(err)
	}
	if !themed() || themeSetting() != "nord" || !th().Bad.GetBold() || th().Warn.GetBold() {
		t.Fatalf("nord dark: %v %v", themeSetting(), th().Bad.GetBold())
	}
	if !strings.Contains(th().Accent.Render("x"), "136;192;208") {
		t.Fatalf("accent isn't nord's: %q", th().Accent.Render("x"))
	}
	if p := th().Pane; p == nil || p.Bg == nil || *p.Bg != (emu.RGB{R: 0x2e, G: 0x34, B: 0x40}) || p.Fg != nil {
		t.Fatalf("nord dark pane palette: %+v", p)
	}
	setScheme(false)
	if th().Bad.GetBold() || !strings.Contains(th().Accent.Render("x"), "94;129;172") {
		t.Fatalf("nord light: %q", th().Accent.Render("x"))
	}
	if th().Pane != nil {
		t.Fatalf("nord light has no pane table, yet %+v", th().Pane)
	}

	installMod(t, "nord", map[string]string{"theme.toml": "[dark]\nbad = { fg = [\"#zzz\"] }\n"})
	err := loadLook()
	if err == nil || !strings.Contains(err.Error(), "nord theme: bad") || themed() {
		t.Fatalf("bad colour: %v, themed %v", err, themed())
	}
	writeConfig(t, "[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = false\n")
	if err := loadLook(); err == nil || !strings.Contains(err.Error(), "nord is disabled") || themed() {
		t.Fatalf("off: %v", err)
	}
}

// TestIconsMod: [ui] icons names a mod's set: its glyphs, the base's for
// the rest; a set that breaks the width rules leaves auto's.
func TestIconsMod(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	freshConsole(t)
	defaultKeymap(t)
	installMod(t, "nord", nord)
	writeConfig(t, "[ui]\nicons = \"nord\"\n[mods.nord]\nenabled = true\n")
	if err := loadLook(); err != nil {
		t.Fatal(err)
	}
	if ic().name != "nord" || ic().working != "◉" || ic().done != unicodeIcons.done || iconsSetting() != "nord" {
		t.Fatalf("icons %+v", *ic())
	}
	installMod(t, "nord", map[string]string{"icons.toml": "working = \"ab\"\n"})
	if err := loadLook(); err == nil || !strings.Contains(err.Error(), "nord icons: working") || ic().name != IconsUnicode {
		t.Fatalf("two cells: %v %s", err, ic().name)
	}
}

// TestKeymapMods: every enabled keymap applies, the first enabled
// winning a key; a refused binding keeps its default and names its mod.
func TestKeymapMods(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	freshConsole(t)
	defaultKeymap(t)
	keys := func(id, body string) map[string]string {
		return map[string]string{"mod.toml": "id = \"" + id + "\"\nname = \"K\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nkeymap = \"keys.toml\"\n", "keys.toml": body}
	}
	installMod(t, "first", keys("first", "project.next = [\"L\"]\n"))
	installMod(t, "second", keys("second", "project.prev = [\"L\"]\nlist.up = [\"esc\"]\n"))
	writeConfig(t, "[mods.second]\nenabled = true\n[mods.first]\nenabled = true\n")
	err := loadLook()
	if binds.list.canon("L") != "[" || binds.list.rebound("project.next") {
		t.Fatalf("L: %q", binds.list.canon("L"))
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	for _, want := range []string{"first keymap: key conflict: L is second's project.prev", "second keymap: list.up: esc is locked"} {
		if !strings.Contains(msg, want) {
			t.Errorf("errors lack %q:\n%s", want, msg)
		}
	}
	if n := lookNews(err); !strings.HasPrefix(n, "mods: ") || lookNews(err) != "" {
		t.Fatalf("said again: %q", n)
	}
}

// TestLayoutMod: [ui] layout names a mod's layout: the console asks for
// its column order, the safety columns kept; a broken one is the
// built-in order.
func TestLayoutMod(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	freshConsole(t)
	defaultKeymap(t)
	installMod(t, "wide", map[string]string{
		"mod.toml":    "id = \"wide\"\nname = \"Wide\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nlayout = \"layout.toml\"\n",
		"layout.toml": "sidebar = [\"needs\", \"review-age.age\", \"count\"]\n",
	})
	writeConfig(t, "[ui]\nlayout = \"wide\"\n[mods.wide]\nenabled = true\n")
	l, err := configLayout()
	if cols := layoutColumns(l); err != nil || !slices.Equal(cols, []string{"needs", "review-age.age", "count", "state", "paused", "inactive"}) || layoutValue() != "wide" {
		t.Fatalf("%v %v", l.Columns, err)
	}
	installMod(t, "wide", map[string]string{"layout.toml": "sidebar = [\"bogus\"]\n"})
	if l, err := configLayout(); layoutColumns(l) != nil || err == nil || !strings.Contains(err.Error(), "wide layout") {
		t.Fatalf("broken: %v %v", l, err)
	}
}

// TestLookPickers: General settings' Theme, Icons and Layout rows offer
// the built-in choices, then the enabled mods' (a mod that is off is not
// offered); a pick applies to this console and is saved, the built-in
// one as unset.
func TestLookPickers(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	freshConsole(t)
	defaultKeymap(t)
	installMod(t, "nord", nord)
	installMod(t, "off", nord)
	installMod(t, "off", map[string]string{"mod.toml": strings.Replace(nord["mod.toml"], "\"nord\"", "\"off\"", 1)})
	installMod(t, "wide", map[string]string{
		"mod.toml":    "id = \"wide\"\nname = \"Wide\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nlayout = \"layout.toml\"\n",
		"layout.toml": "sidebar = [\"needs\"]\n",
	})
	writeConfig(t, "[mods.nord]\nenabled = true\n[mods.wide]\nenabled = true\n")
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 80, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",")
	file := func() *config.Config {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	modsRow(t, m, "Theme")
	act(t, m, src, "enter")
	if themeSetting() != "nord" || !themed() || file().Theme != "nord" {
		t.Fatalf("theme nord: %s %q", themeSetting(), file().Theme)
	}
	act(t, m, src, "enter")
	if themeSetting() != ThemeDefault || themed() || file().Theme != "" {
		t.Fatalf("theme back: %s %q", themeSetting(), file().Theme)
	}

	modsRow(t, m, "Icons")
	setIcons(IconsAuto)
	for _, want := range []string{IconsNerd, IconsUnicode, IconsASCII, "nord", IconsAuto} {
		act(t, m, src, "enter")
		if iconsSetting() != want || file().Icons != want {
			t.Fatalf("icons: %s, file %q, want %s", iconsSetting(), file().Icons, want)
		}
	}

	modsRow(t, m, "Layout")
	act(t, m, src, "enter")
	if layoutValue() != "wide" || file().Layout != "wide" {
		t.Fatalf("layout: %s %q", layoutValue(), file().Layout)
	}
	act(t, m, src, "enter")
	if layoutValue() != ThemeDefault || file().Layout != "" {
		t.Fatalf("layout back: %s %q", layoutValue(), file().Layout)
	}
	if out := screen(m); strings.Contains(out, "[ui]") || strings.Contains(out, "theme =") {
		t.Fatalf("the popup names the file's keys:\n%s", out)
	}
}

// TestThemeAcrossViews (T319): a mod's theme keeps its scheme as the
// console goes from the dashboard (a project's folder) to a session
// (its coordinator) and back. Both take light or dark from the
// terminal's background; the session's colour scheme report (2031),
// which may differ (the system's appearance under a dark terminal),
// goes to the panes' programs and leaves the theme be.
func TestThemeAcrossViews(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv("COLORTERM", "truecolor")
	freshConsole(t)
	defaultKeymap(t)
	installMod(t, "nord", nord)
	writeConfig(t, "[ui]\ntheme = \"nord\"\n[mods.nord]\nenabled = true\n")
	const light, dark = "94;129;172", "136;192;208" // nord's accent
	accent := func(view, want string) {
		t.Helper()
		if got := th().Accent.Render("x"); !strings.Contains(got, want) {
			t.Fatalf("%s: accent %q, want %s", view, got, want)
		}
	}

	// A light terminal background, with the system dark.
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for i := range 3 {
		// The dashboard: it loads the look and asks the background.
		if err := loadLook(); err != nil {
			t.Fatal(err)
		}
		(&dash{}).update(tea.BackgroundColorMsg{Color: white})
		accent(fmt.Sprintf("dashboard %d", i), light)

		// The session: it loads the look again, then the terminal
		// answers its scheme query (dark) and its background one.
		if err := loadLook(); err != nil {
			t.Fatal(err)
		}
		c := &client{}
		c.handle(uv.DarkColorSchemeEvent{})
		accent(fmt.Sprintf("session %d, scheme report", i), light)
		if !strings.Contains(string(c.osc), askColors) {
			t.Fatalf("a scheme change doesn't ask the background again: %q", c.osc)
		}
		c.handle(uv.BackgroundColorEvent{Color: white})
		accent(fmt.Sprintf("session %d, background", i), light)
	}

	// The background turning dark recolours the session's own parts.
	c := &client{}
	c.handle(uv.BackgroundColorEvent{Color: color.RGBA{0x2e, 0x34, 0x40, 0xff}})
	accent("session, dark background", dark)
	if !c.full {
		t.Fatal("a new scheme doesn't redraw the sidebar and status bar")
	}
}

// TestSafeModeLook: with tm --no-mods the console takes no user mod's
// theme, icons, layout or keymap, offers none, and says so once.
func TestSafeModeLook(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv(mod.EnvNoMods, "1")
	freshConsole(t)
	defaultKeymap(t)
	installMod(t, "nord", nord)
	installMod(t, "keys", map[string]string{
		"mod.toml":  "id = \"keys\"\nname = \"K\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nkeymap = \"keys.toml\"\n",
		"keys.toml": "project.next = [\"L\"]\n",
	})
	installMod(t, "wide", map[string]string{
		"mod.toml":    "id = \"wide\"\nname = \"Wide\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nlayout = \"layout.toml\"\n",
		"layout.toml": "sidebar = [\"needs\", \"count\"]\n",
	})
	writeConfig(t, "[ui]\ntheme = \"nord\"\nicons = \"nord\"\nlayout = \"wide\"\n[mods.nord]\nenabled = true\n[mods.keys]\nenabled = true\n[mods.wide]\nenabled = true\n")
	err := loadLook()
	if err != nil {
		t.Fatal(err)
	}
	if themed() || themeSetting() != ThemeDefault || ic().name == "nord" || binds.list.canon("L") == "[" || th().Pane != nil {
		t.Fatalf("user data loaded: %v %s %s", themed(), themeSetting(), ic().name)
	}
	if l, lerr := configLayout(); layoutColumns(l) != nil || lerr != nil || layoutValue() != ThemeDefault {
		t.Fatalf("layout %v %v %s", l, lerr, layoutValue())
	}
	for _, kind := range []string{mod.KindTheme, mod.KindIcons, mod.KindLayout} {
		if c := modChoices(nil, kind); len(c) != 0 {
			t.Errorf("%s offered %v", kind, c)
		}
	}
	if n := lookNews(err); n != safeLookNote || lookNews(err) != "" {
		t.Fatalf("note %q", n)
	}
	if rows := globalSettings(""); !safeMode() || safeModeRow().value(nil) == "" || len(rows) == 0 {
		t.Fatal("no safe mode row")
	}
	t.Setenv(mod.EnvNoMods, "")
	if err := loadLook(); err != nil || themeSetting() != "nord" {
		t.Fatalf("off: %v %s", err, themeSetting())
	}
}
