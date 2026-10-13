package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/view"
)

// TestDefaultKeymap: the bundled default-keymap binds every action that
// can be rebound, to the keys of its table, and nothing else: applying
// it changes no key (the move out of Go is lossless), and the console
// checks accept it.
func TestDefaultKeymap(t *testing.T) {
	km := DefaultKeymap()
	for _, b := range slices.Concat(listBindables(), sideBindables(), panelBindables()) {
		keys, ok := km[b.id]
		switch {
		case b.fixed && ok:
			t.Errorf("%s is fixed, but the keymap binds it", b.id)
		case !b.fixed && !slices.Equal(keys, b.keys):
			t.Errorf("%s: keymap %q, table %q", b.id, keys, b.keys)
		}
		delete(km, b.id)
	}
	if len(km) > 0 {
		t.Errorf("the keymap binds unknown ids %v", km)
	}
	if err := mod.DataChecks[mod.KindKeymap](mod.BundledData(mod.BundledKeymap, mod.KindKeymap)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Cleanup(func() { ApplyKeymap(nil, ConfigPrefix()) })
	if err := loadKeymap(loadMods(nil)); err != nil {
		t.Fatal(err)
	}
	for _, b := range listBindables() {
		if binds.list.rebound(b.id) {
			t.Errorf("%s rebound by the default keymap", b.id)
		}
	}
}

// TestDefaultLayout: the bundled default-layout is the built-in column
// order, which the view keeps as nil.
func TestDefaultLayout(t *testing.T) {
	l, err := mod.ParseLayout(mod.BundledData(mod.BundledLayout, mod.KindLayout))
	if err != nil || !slices.Equal(l.Columns, view.DefaultColumns) {
		t.Fatalf("default layout %q %v, want %q", l.Columns, err, view.DefaultColumns)
	}
	for _, v := range []string{"", ThemeDefault} {
		if l, err := modLayout(&mod.Registry{}, v); layoutColumns(l) != nil || err != nil ||
			!reflect.DeepEqual(effectiveLayout(l, DefaultLayout, v), effectiveLayout(mod.Layout{}, DefaultLayout, v)) {
			t.Errorf("%q: %+v %v", v, l, err)
		}
	}
}

// TestBundledLook: the built-in theme and icon sets are the bundled
// mods' whole files, and pass the console's checks; the pickers offer
// them by their old names (default, unicode…), not again by mod id.
func TestBundledLook(t *testing.T) {
	if err := mod.DataChecks[mod.KindTheme](mod.BundledData(mod.BundledTheme, mod.KindTheme)); err != nil {
		t.Fatal(err)
	}
	for _, name := range IconChoices[1:] {
		if err := mod.DataChecks[mod.KindIcons](mod.BundledData(mod.BundledIcons(name), mod.KindIcons)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if builtinIcons(name).name != name {
			t.Errorf("%s set named %q", name, builtinIcons(name).name)
		}
	}
	t.Setenv("TERMINATR_HOME", t.TempDir())
	for kind, want := range map[string][]string{mod.KindTheme: {ThemeDefault}, mod.KindIcons: IconChoices, mod.KindLayout: {ThemeDefault}} {
		builtin := []string{ThemeDefault}
		if kind == mod.KindIcons {
			builtin = IconChoices
		}
		if got := modChoices(builtin, kind); !slices.Equal(got, want) {
			t.Errorf("%s choices %q", kind, got)
		}
	}
	// A bundled mod's id names it too.
	if err := useTheme(loadMods((*config.Config)(nil)), mod.BundledTheme); err != nil {
		t.Errorf("theme %s: %v", mod.BundledTheme, err)
	}
	useTheme(nil, ThemeDefault)
	if set, err := modIcons(loadMods(nil), mod.BundledIcons(IconsASCII)); err != nil || set.barOn != asciiIcons.barOn {
		t.Errorf("icons %s: %v", mod.BundledIcons(IconsASCII), err)
	}
	if !strings.Contains(string(mod.BundledData(mod.BundledIcons(IconsNerd), mod.KindIcons)), "nf-md-robot") {
		t.Error("the nerd set lost its glyph names")
	}
}
