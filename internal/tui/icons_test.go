package tui

import (
	"github.com/theclifmeister/terminatr/internal/config"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestIconWidths: every glyph of every set is one cell wide, a connector
// two, so the sidebar's columns line up whatever the set; none is an
// emoji. The unicode set has no Private Use Area glyph (those need a
// Nerd Font), the ascii set nothing beyond ASCII.
func TestIconWidths(t *testing.T) {
	for _, set := range []*iconSet{&unicodeIcons, &nerdIcons, &asciiIcons} {
		for _, e := range validateIcons(set, set.name == IconsNerd, set.name == IconsASCII) {
			t.Errorf("%s.%s", set.name, e)
		}
	}
}

// TestIconGlyphsComplete: glyphs() lists every string field of iconSet,
// so a field added later can't escape loading and validation.
func TestIconGlyphsComplete(t *testing.T) {
	v := reflect.TypeOf(iconSet{})
	g := unicodeIcons.glyphs()
	for i := 0; i < v.NumField(); i++ {
		n := v.Field(i).Name
		if v.Field(i).Type.Kind() != reflect.String || n == "name" {
			continue
		}
		if _, ok := g[n]; !ok {
			t.Errorf("glyphs() lacks %s", n)
		}
	}
	if len(g) != v.NumField()-2 { // name, wide
		t.Errorf("glyphs() has %d entries, iconSet %d fields", len(g), v.NumField())
	}
}

// TestParseIcons: a set loads from TOML over its base, missing fields
// fall back, and bad widths, emoji, Private Use Area glyphs in a unicode
// set, unknown keys and wrong types are errors.
func TestParseIcons(t *testing.T) {
	s, err := parseIcons([]byte("name = \"nord\"\nworking = \"◉\"\nreview = \"◇\"\nbarOn = \"━\"\nbarOff = \"─\"\nmid = \"├╴\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.name != "nord" || s.working != "◉" || s.barOff != "─" || s.mid != "├╴" || s.blocked != unicodeIcons.blocked || s.wide {
		t.Errorf("loaded %+v", s)
	}
	n, err := parseIcons([]byte("base = \"nerd\"\ndone = \"v\"\nthread = \"\"\n"))
	if err != nil || n.wide != true || n.done != "v" || n.thread != "" || n.folder != nerdIcons.folder {
		t.Errorf("nerd base: %+v %v", n, err)
	}
	if e, err := parseIcons(nil); err != nil || *e != func() iconSet { u := unicodeIcons; u.name = IconsUnicode; return u }() {
		t.Errorf("empty file: %v", err)
	}
	for body, want := range map[string]string{
		"working = \"ab\"":               "2 cells",
		"working = \"日\"":                "2 cells",
		"mid = \"-\"":                    "1 cells, want 2",
		"review = \"🔥\"":                 "emoji",
		"done = \"\uf00c\"":              "Private Use Area",
		"base = \"ascii\"\ndone = \"✓\"": "isn't ASCII",
		"base = \"nope\"":                "base",
		"colour = \"x\"":                 "unknown key",
		"working = 3":                    "want a string",
		"wide = \"yes\"":                 "wide",
		"working = ":                     "",
	} {
		if _, err := parseIcons([]byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want error with %q", body, err, want)
		}
	}
}

// TestIconsTree: in every set, each row of the tree is as wide as the
// sidebar and the grid's columns line up: every coordinator and thread
// row has its state glyph in the state column.
func TestIconsTree(t *testing.T) {
	defer setIcons(IconsUnicode)
	d := testData()
	for _, set := range IconChoices[1:] {
		setIcons(set)
		lines := sidebarLines(buildTree(d.Projects, d.Sessions, treeIn{current: "beta"}), sideDefault, 8, nil)
		for i, l := range lines {
			l = ansi.Strip(l)
			if w := ansi.StringWidth(l); w != sideDefault {
				t.Errorf("%s row %d %q is %d cells", set, i, l, w)
			}
			r := []rune(l)
			if i > 0 && r[len(r)-2] != ' ' {
				t.Errorf("%s row %d %q: no blank column before the border", set, i, l)
			}
			row := string(r[:len(r)-2])
			if i > 0 && (strings.Contains(l, ic().end) || strings.Contains(l, ic().mid)) && gridCol(row, gridState) == "" {
				t.Errorf("%s row %d %q: no state glyph", set, i, l)
			}
		}
	}
}

// TestIconsAuto: auto picks Nerd Font icons in Ghostty, Unicode elsewhere.
func TestIconsAuto(t *testing.T) {
	for _, c := range []struct{ value, term, want string }{
		{IconsAuto, "ghostty", IconsNerd}, {IconsAuto, "Apple_Terminal", IconsUnicode}, {IconsAuto, "", IconsUnicode},
		{IconsASCII, "ghostty", IconsASCII}, {IconsNerd, "", IconsNerd}, {IconsUnicode, "ghostty", IconsUnicode},
		{"bogus", "", IconsUnicode},
	} {
		if got := iconSetFor(c.value, c.term).name; got != c.want {
			t.Errorf("%s in %q: %s, want %s", c.value, c.term, got, c.want)
		}
	}
}

// TestIconsSetting: the settings popup's Icons row steps through auto,
// nerd, unicode and ascii, saves each in the settings file and changes
// this console's set at once; the next console reads it from the file.
func TestIconsSetting(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	defer setIcons(IconsUnicode)
	if loadIcons() != IconsAuto || ic().name != IconsUnicode {
		t.Fatalf("no settings file: %s %s", iconsSetting(), ic().name)
	}
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 60, State: DashState{Current: "beta"}})
	m.setData(src.data)
	press(m, ",")
	if !strings.Contains(screen(m), "auto (unicode)") {
		t.Fatalf("settings:\n%s", screen(m))
	}
	sv := m.top().(*settingsView)
	for i, r := range sv.tabs[0].rows {
		if r.label == "Icons" {
			sv.tabs[0].sel = i
		}
	}
	for _, want := range []string{IconsNerd, IconsUnicode, IconsASCII, IconsAuto} {
		drain(t, m, press(m, "enter"))
		if iconsSetting() != want || fileIcons() != want {
			t.Fatalf("enter: %s, file %s, want %s (%v)", iconsSetting(), fileIcons(), want, src.settings)
		}
	}
	if strings.Contains(screen(m), "icons =") || strings.Contains(screen(m), "[ui]") {
		t.Fatal("the popup names the file's keys")
	}
	for range 3 { // to ascii
		drain(t, m, press(m, "enter"))
	}
	setIcons(IconsUnicode)
	if loadIcons() != IconsASCII || ic().name != IconsASCII {
		t.Fatalf("reloaded: %s %s", iconsSetting(), ic().name)
	}
}

// fileIcons is config.toml's icons setting.
func fileIcons() string {
	cfg, _ := config.Load()
	return configIcons(cfg)
}
