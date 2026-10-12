package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// parseSpec is a layout.toml for the tests.
func parseSpec(t *testing.T, src string) mod.Layout {
	t.Helper()
	l, err := mod.ParseLayout([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestEffectiveLayout: each part comes from the tweak under the layout,
// else the layout, else ui.json's top level, else the built-in value
// (the layout v2 design §2.5).
func TestEffectiveLayout(t *testing.T) {
	ui := DefaultLayout
	ui.Sidebar.Width, ui.Split = 44, 0.5

	// No layout: ui.json as it is.
	l := effectiveLayout(mod.Layout{}, ui, "")
	if l.Sidebar.Width != 44 || l.Split != 0.5 || !l.Details || l.place() != placeRight || l.mode() != modeFull || l.set != (layoutSet{}) {
		t.Fatalf("no layout: %+v", l)
	}
	if !slices.Equal(l.sections, mod.LayoutSections) || l.side != "left" || l.header != "shown" || l.footer != "shown" || l.statusBar != "shown" || l.density != "normal" {
		t.Fatalf("built-in parts: %+v", l)
	}

	// The layout's values win over ui.json's.
	cockpit := parseSpec(t, "[sidebar]\nside = \"right\"\nwidth = 50\nmode = \"slim\"\n[details]\nplace = \"bottom\"\nsize = \"40%\"\n[footer]\nstyle = \"compact\"\n[dashboard]\nsections = [\"project\"]\n")
	l = effectiveLayout(cockpit, ui, "cockpit")
	if l.Sidebar.Width != 50 || !l.Sidebar.Slim || l.mode() != modeSlim || l.place() != placeBottom || l.DetailsRows != 0.4 || l.Split != 0.5 {
		t.Fatalf("cockpit: %+v", l)
	}
	if l.side != "right" || l.footer != "compact" || !slices.Equal(l.sections, []string{"needs", "project"}) {
		t.Fatalf("cockpit's own parts: %+v", l)
	}
	if want := (layoutSet{place: true, rows: true, width: true, mode: true}); l.set != want {
		t.Fatalf("set %+v, want %+v", l.set, want)
	}

	// The tweak under the layout wins over it, and only under it.
	ui.Layouts = map[string]LayoutTweak{"cockpit": {Sidebar: &SidebarTweak{Width: 30, Mode: modeFull}, DetailsRows: 0.6}}
	if l = effectiveLayout(cockpit, ui, "cockpit"); l.Sidebar.Width != 30 || l.Sidebar.Slim || l.DetailsRows != 0.6 {
		t.Fatalf("tweaked: %+v", l)
	}
	if l = effectiveLayout(mod.Layout{}, ui, ThemeDefault); l.Sidebar.Width != 44 || l.Sidebar.Slim {
		t.Fatalf("default after cockpit: %+v", l)
	}

	// Sizes: a share of the panel is 1 - the list's; cells stay cells.
	if l = effectiveLayout(parseSpec(t, "[details]\nsize = \"35%\"\n"), ui, "x"); l.Split != 0.65 || l.detCells != 0 {
		t.Fatalf("35%%: %+v", l)
	}
	if l = effectiveLayout(parseSpec(t, "[details]\nsize = 50\n"), ui, "x"); l.detCells != 50 || !l.set.split {
		t.Fatalf("50 cells: %+v", l)
	}
	if l = effectiveLayout(parseSpec(t, "[sidebar]\nwidth = \"25%\"\n"), ui, "x").sized(200); l.Sidebar.Width != 50 {
		t.Fatalf("25%% of 200: %+v", l)
	}

	// Off: no panel, and | turns it on at the right (bottom for a
	// bottom layout turned off by the user).
	if l = effectiveLayout(parseSpec(t, "[details]\nplace = \"off\"\n"), ui, "zen"); l.Details || l.place() != placeOff {
		t.Fatalf("off: %+v", l)
	}
	l.Details = true
	if l.place() != placeRight {
		t.Fatalf("| on an off layout: %s", l.place())
	}
	ui.Layouts["cockpit"] = LayoutTweak{DetailsPlace: placeOff}
	l = effectiveLayout(cockpit, ui, "cockpit")
	l.Details = true
	if l.place() != placeBottom {
		t.Fatalf("| on cockpit: %s", l.place())
	}

	// ui.json's top level: details off, the slim strip, hidden.
	top := DefaultLayout
	top.Details, top.Sidebar.Slim = false, true
	if l = effectiveLayout(mod.Layout{}, top, ""); l.place() != placeOff || l.mode() != modeSlim {
		t.Fatalf("top level: %+v", l)
	}
	top.SideMode = modeHidden
	if l = effectiveLayout(mod.Layout{}, top, ""); l.mode() != modeHidden || !l.Sidebar.Slim {
		t.Fatalf("hidden: %+v", l)
	}
}

// TestLayoutSections: needs and project always show, needs first when
// left out, project after it.
func TestLayoutSections(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{nil, []string{"needs", "project", "other", "cards"}},
		{[]string{"project"}, []string{"needs", "project"}},
		{[]string{"cards", "other"}, []string{"needs", "project", "cards", "other"}},
		{[]string{"other", "needs"}, []string{"other", "needs", "project"}},
		{[]string{"project", "needs", "cards"}, []string{"project", "needs", "cards"}},
	} {
		if got := layoutSections(c.in); !slices.Equal(got, c.want) {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}

// TestTweak: a change to a part the layout sets is kept under its id;
// one it doesn't set goes to ui.json's top level, as before layouts.
func TestTweak(t *testing.T) {
	ui := DefaultLayout
	ui.Sidebar.Width = 44
	cockpit := parseSpec(t, "[sidebar]\nwidth = 50\n[details]\nplace = \"right\"\n")

	was := effectiveLayout(cockpit, ui, "cockpit")
	now := was
	now.Sidebar.Width, now.Split = 30, 0.5
	ui = tweak(ui, was, now, "cockpit", false)
	if ui.Sidebar.Width != 44 || ui.Split != 0.5 || ui.Layouts["cockpit"].Sidebar == nil || ui.Layouts["cockpit"].Sidebar.Width != 30 || ui.Layouts["cockpit"].Split != 0 {
		t.Fatalf("width under cockpit, split at the top: %+v %+v", ui, ui.Layouts["cockpit"])
	}
	if l := effectiveLayout(cockpit, ui, "cockpit"); l.Sidebar.Width != 30 || l.Split != 0.5 {
		t.Fatalf("cockpit again: %+v", l)
	}
	if l := effectiveLayout(mod.Layout{}, ui, ThemeDefault); l.Sidebar.Width != 44 {
		t.Fatalf("back to default: %+v", l)
	}

	// | under a layout that sets the place: kept under it.
	was = effectiveLayout(cockpit, ui, "cockpit")
	now = was
	now.Details = false
	ui = tweak(ui, was, now, "cockpit", false)
	if !ui.Details || ui.Layouts["cockpit"].DetailsPlace != placeOff || ui.Layouts["cockpit"].Sidebar.Width != 30 {
		t.Fatalf("| under cockpit: %+v %+v", ui, ui.Layouts["cockpit"])
	}

	// b under the default layout, which sets no mode: the top level.
	was = effectiveLayout(mod.Layout{}, ui, ThemeDefault)
	now = was
	now.Sidebar.Slim = true
	ui = tweak(ui, was, now, ThemeDefault, false)
	if !ui.Sidebar.Slim || ui.SideMode != "" || len(ui.Layouts) != 1 {
		t.Fatalf("b: %+v", ui)
	}

	// Safe mode writes nothing under a layout.
	safe := DefaultLayout
	was = effectiveLayout(cockpit, safe, "cockpit")
	now = was
	now.Sidebar.Width = 20
	if safe = tweak(safe, was, now, "cockpit", true); safe.Layouts != nil || safe.Sidebar.Width != 20 {
		t.Fatalf("safe mode: %+v", safe)
	}

	// At most maxTweaks layouts: the one changed longest ago goes.
	ui = DefaultLayout
	for i := range maxTweaks + 2 {
		id := string(rune('a' + i))
		was = effectiveLayout(cockpit, ui, id)
		now = was
		now.Sidebar.Width = 20 + i
		ui = tweak(ui, was, now, id, false)
	}
	if _, ok := ui.Layouts["a"]; ok || len(ui.Layouts) != maxTweaks {
		t.Fatalf("%d layouts, a kept: %v", len(ui.Layouts), ok)
	}
	if _, ok := ui.Layouts[string(rune('a'+maxTweaks+1))]; !ok {
		t.Fatal("the latest went")
	}
}

// TestLayoutFileKeys: ui.json's new keys are written only when needed, the
// old slim is still read, and mode wins over it.
func TestLayoutFileKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ui.json")
	if err := SaveLayout(path, DefaultLayout); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, k := range []string{"layouts", "details_place", "details_rows", "mode"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("default ui.json has %s:\n%s", k, b)
		}
	}
	os.WriteFile(path, []byte(`{"details": true, "split": 0.6, "sidebar": {"width": 40, "slim": true, "mode": "hidden"},
		"details_place": "bottom", "details_rows": 0.9,
		"layouts": {"cockpit": {"sidebar": {"width": 30, "mode": "bogus"}, "split": 0.1, "seq": 3}}}`), 0o600)
	l := LoadLayout(path)
	if l.Sidebar.Width != 40 || !l.Sidebar.Slim || l.SideMode != modeHidden || l.DetailsPlace != placeBottom || l.DetailsRows != maxRows {
		t.Fatalf("read %+v", l)
	}
	if c := l.Layouts["cockpit"]; c.Sidebar.Width != 30 || c.Sidebar.Mode != "" || c.Split != minSplit || c.Seq != 3 {
		t.Fatalf("cockpit %+v %+v", c, c.Sidebar)
	}
	if err := SaveLayout(path, l); err != nil {
		t.Fatal(err)
	}
	if again := LoadLayout(path); again.SideMode != modeHidden || again.Layouts["cockpit"].Sidebar.Width != 30 {
		t.Fatalf("round trip %+v", again)
	}
}

// TestDashLayout: the dashboard under a layout mod. A layout picked
// applies whole; one read as the console starts with a view leaves the
// view's sidebar what the layout doesn't set; a change is saved under
// the layout where it sets the part.
func TestDashLayout(t *testing.T) {
	src := &fakeSource{data: testData()}
	ui := filepath.Join(t.TempDir(), "ui.json")
	os.WriteFile(ui, []byte(`{"details": true, "split": 0.6, "sidebar": {"width": 44}}`), 0o600)
	m := newDash(DashOptions{Source: src, Width: 200, Height: 40, UIFile: ui, State: DashState{Current: "beta"}})
	m.safe = false
	if m.sideW() != 44 {
		t.Fatalf("sidebar %d", m.sideW())
	}
	g0 := m.geom().body()

	cockpit := parseSpec(t, "[sidebar]\nwidth = \"25%\"\n[details]\nsize = 60\n")
	m.useLayout("cockpit", cockpit, true)
	if m.sideW() != 50 || m.w != 150 {
		t.Fatalf("cockpit: sidebar %d, dashboard %d", m.sideW(), m.w)
	}
	if lw := m.geom().list().w; m.panel() != placeRight || lw != 150-60-1 {
		t.Fatalf("cockpit's panel: list %d", lw)
	}
	if g := m.geom(); g.details().w != 60 || g.divider().x != 50+89 || g.list().w != 89 || g.body() != (rect{50, 1, 150, 36}) {
		t.Fatalf("geometry %+v %+v", g.details(), g.body())
	}

	// < steps from the panel shown, and is kept for cockpit.
	m.resize("<")
	l := LoadLayout(ui)
	if c := l.Layouts["cockpit"]; c.Split == 0 || l.Split != 0.6 || l.Sidebar.Width != 44 {
		t.Fatalf("< under cockpit: %+v %+v", l, c)
	}
	// { is kept for cockpit too; } on the default layout is the top
	// level's.
	m.sideKey("{")
	if l = LoadLayout(ui); l.Layouts["cockpit"].Sidebar == nil || l.Layouts["cockpit"].Sidebar.Width != 48 || l.Sidebar.Width != 44 {
		t.Fatalf("{ under cockpit: %+v", l)
	}
	m.useLayout(ThemeDefault, mod.Layout{}, true)
	if m.sideW() != 44 || m.geom().body() != g0 {
		t.Fatalf("back to default: sidebar %d", m.sideW())
	}
	m.sideKey("}")
	if l = LoadLayout(ui); l.Sidebar.Width != 46 || l.Layouts["cockpit"].Sidebar.Width != 48 {
		t.Fatalf("} under default: %+v", l)
	}
	m.useLayout("cockpit", cockpit, true)
	if m.sideW() != 48 {
		t.Fatalf("cockpit again: %d", m.sideW())
	}

	// Started with a view: what the layout doesn't set stays the view's.
	m.layout.Sidebar.Width, m.layout.Sidebar.Slim = 70, true
	m.useLayout("narrow", parseSpec(t, "[sidebar]\nwidth = 20\n"), false)
	if m.layout.Sidebar.Width != 20 || !m.layout.Sidebar.Slim {
		t.Fatalf("view's sidebar: %+v", m.layout.Sidebar)
	}
}

// TestSafeModeLayout: in safe mode the console's layout is the built-in
// one, whatever [ui] layout says, and nothing is saved under a layout.
func TestSafeModeLayout(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv(mod.EnvNoMods, "1")
	resetLook(t)
	installMod(t, "wide", map[string]string{
		"mod.toml":    "id = \"wide\"\nname = \"Wide\"\nversion = \"1.0.0\"\napi = \"1.5\"\n[provides]\nlayout = \"layout.toml\"\n",
		"layout.toml": "[sidebar]\nwidth = 80\nmode = \"slim\"\n",
	})
	writeConfig(t, "[ui]\nlayout = \"wide\"\n[mods.wide]\nenabled = true\n")
	spec, err := configLayout()
	if err != nil || layoutValue() != ThemeDefault || !spec.Width.IsZero() || spec.Mode != "" {
		t.Fatalf("safe mode's layout %q: %+v %v", layoutValue(), spec, err)
	}
	ui := filepath.Join(t.TempDir(), "ui.json")
	m := newDash(DashOptions{Source: &fakeSource{data: testData()}, Width: 200, Height: 40, UIFile: ui})
	m.layout.Sidebar.Width = 80 // a layout left it on the view
	m.useLayout(layoutValue(), spec, m.safe)
	if !m.safe || m.sideW() != sideDefault {
		t.Fatalf("safe %v, sidebar %d", m.safe, m.sideW())
	}
	if lookNote(nil) != safeLookNote || !strings.Contains(safeLookNote, "layout") {
		t.Fatalf("note %q", lookNote(nil))
	}
	t.Setenv(mod.EnvNoMods, "")
	if spec, _ := configLayout(); layoutValue() != "wide" || spec.Width.Cells != 80 {
		t.Fatalf("off: %s %+v", layoutValue(), spec)
	}
}

// TestPickLayout: General settings → Layout applies the whole layout
// picked, not only its columns, and default brings back the console's
// own.
func TestPickLayout(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	resetLook(t)
	installMod(t, "cockpit", map[string]string{
		"mod.toml":    "id = \"cockpit\"\nname = \"Cockpit\"\nversion = \"1.0.0\"\napi = \"1.5\"\n[provides]\nlayout = \"layout.toml\"\n",
		"layout.toml": "[sidebar]\nwidth = 50\nmode = \"slim\"\n[details]\nplace = \"off\"\n",
	})
	writeConfig(t, "[mods.cockpit]\nenabled = true\n")
	ui := filepath.Join(t.TempDir(), "ui.json")
	m := newDash(DashOptions{Source: &fakeSource{data: testData()}, Width: 200, Height: 40, UIFile: ui})
	var pick setting
	for _, s := range globalSettings("") {
		if s.label == "Layout" {
			pick = s
		}
	}
	pick.change(m)
	if layoutValue() != "cockpit" || m.layoutID != "cockpit" || m.sideW() != sideSlim || m.layout.Details {
		t.Fatalf("cockpit: %s, sidebar %d, details %v", layoutValue(), m.sideW(), m.layout.Details)
	}
	m.sideKey("b") // the full sidebar, at cockpit's width, kept for it
	if m.sideW() != 50 || LoadLayout(ui).Layouts["cockpit"].Sidebar.Mode != modeFull {
		t.Fatalf("b under cockpit: %d %+v", m.sideW(), LoadLayout(ui))
	}
	pick.change(m)
	if layoutValue() != ThemeDefault || m.sideW() != sideDefault || !m.layout.Details {
		t.Fatalf("default: %s, sidebar %d, details %v", layoutValue(), m.sideW(), m.layout.Details)
	}
}

// TestDetailsBottom: under a layout that puts the details panel at the
// bottom, it shows under the list with a rule above it; the wheel and
// the keys scroll it, < > and the divider's drag size it (kept for the
// layout); a short window shows the details under the row; the
// settings row says and steps the place.
func TestDetailsBottom(t *testing.T) {
	src := &fakeSource{data: testData()}
	ui := filepath.Join(t.TempDir(), "ui.json")
	m := newDash(DashOptions{Source: src, Width: 200, Height: 40, UIFile: ui, State: DashState{Current: "beta"}})
	m.safe = false
	m.setData(src.data)
	m.useLayout("cockpit", parseSpec(t, "[details]\nplace = \"bottom\"\n"), true)
	m.sel = "th:beta:t-0005"

	g := m.geom()
	if m.panel() != placeBottom || g.list().h != 22 {
		t.Fatalf("panel %q, list %+v", m.panel(), g.list())
	}
	lines := strings.Split(screen(m), "\n")
	if rule := strings.Repeat("─", g.body().w); lines[g.divider().y] != rule {
		t.Fatalf("divider row %q", lines[g.divider().y])
	}
	panel := strings.Join(lines[g.details().y:g.details().y+g.details().h], "\n")
	if !strings.Contains(panel, "thread    t-0005") || strings.Contains(screen(m), "        todos") {
		t.Fatalf("the panel:\n%s", screen(m))
	}

	// The wheel over the panel scrolls it; over the list it moves.
	first := screen(m)
	mouseAt(m, tea.MouseWheelDown, g.details().x+5, g.details().y+2)
	if m.sel != "th:beta:t-0005" || screen(m) == first || m.detailTop != 3 {
		t.Fatalf("the wheel over the panel: sel %s top %d", m.sel, m.detailTop)
	}
	// The keys too, once it has the keyboard.
	mouseAt(m, tea.MouseLeft, g.details().x+5, g.details().y+2)
	if m.focus != areaDetails {
		t.Fatalf("a click on the panel: focus %d", m.focus)
	}
	keyPress(m, "up")
	if m.detailTop != 2 || m.sel != "th:beta:t-0005" {
		t.Fatalf("up in the panel: top %d sel %s", m.detailTop, m.sel)
	}
	keyPress(m, "esc")
	mouseAt(m, tea.MouseWheelDown, g.list().x+5, g.list().y+2)
	if m.sel == "th:beta:t-0005" {
		t.Fatal("the wheel over the list didn't move")
	}

	// < shrinks the list, > grows it back; the layout doesn't set the
	// size, so ui.json's top level keeps it.
	press(m, "<")
	if h := m.geom().list().h; h != 20 {
		t.Fatalf("< list %d", h)
	}
	if l := LoadLayout(ui); l.DetailsRows != 0.45 || l.Layouts["cockpit"].DetailsRows != 0 {
		t.Fatalf("< saved %+v", l)
	}
	press(m, ">")
	if h := m.geom().list().h; h != 22 {
		t.Fatalf("> list %d", h)
	}

	// The divider's drag: the panel takes the rows under the mouse.
	d := m.geom().divider()
	m.Update(tea.MouseClickMsg{X: d.x + 10, Y: d.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: d.x + 10, Y: 20, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: d.x + 10, Y: 20, Button: tea.MouseLeft})
	if g := m.geom(); g.list().h != 19 || g.divider().y != 20 || LoadLayout(ui).DetailsRows != 0.47 {
		t.Fatalf("drag: %+v, ui.json %+v", g.list(), LoadLayout(ui))
	}

	// A short window: the details under the row, and < says why.
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 19})
	m.sel = "th:beta:t-0005"
	if m.panel() != "" || !strings.Contains(screen(m), "        todos") {
		t.Fatalf("short window:\n%s", screen(m))
	}
	press(m, "<")
	if !strings.Contains(m.msg, "16 rows") {
		t.Fatalf("msg %q", m.msg)
	}
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	// The settings row says the place, and enter steps it: off, right,
	// bottom.
	var place setting
	for _, s := range globalSettings("") {
		if s.label == "Details panel" {
			place = s
		}
	}
	for _, want := range []string{"bottom", "off", "right", "bottom"} {
		if v := place.value(m); v != want {
			t.Fatalf("details panel %q, want %q", v, want)
		}
		place.change(m)
	}
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 19})
	if v := place.value(m); v != "off" {
		t.Fatalf("off in a short window: %q", v)
	}
	press(m, "|")
	if v := place.value(m); !strings.HasPrefix(v, "bottom (no room") || !strings.Contains(m.msg, "16 rows") {
		t.Fatalf("| in a short window: %q, msg %q", v, m.msg)
	}
}
