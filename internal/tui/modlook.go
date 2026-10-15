package tui

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	tmtheme "github.com/theclifmeister/terminatr/internal/theme"
	"github.com/theclifmeister/terminatr/internal/view"
)

// Data mods in the console (mods design §2 tiers 1-2, docs/SPEC.md
// §5.3): the theme and the icon set, one at a time, picked in General
// settings ([ui] theme, [ui] icons) from the built-in ones and those of
// the enabled mods; every enabled keymap mod (keybind.go's ApplyKeymap);
// and the sidebar's layout ([ui] layout), which the console asks the
// server to put on the view so every console agrees. Each console reads
// them when it starts; a pick made in its settings applies to it at
// once, and the others pick it up when they next open. A mod that
// breaks leaves the built-in look, and says why.

// ThemeDefault is the built-in theme's and layout's name in the pickers.
const ThemeDefault = "default"

// safeMode is tm --no-mods (mod.EnvNoMods): the console uses the
// built-in theme, icon set, layout and keymap, and no user mod's data,
// so a mod can never make the keys or the sidebar unreachable.
func safeMode() bool { return mod.NoMods(os.Getenv) }

// safeLookNote is the footer's one line about safe mode's built-in look.
const safeLookNote = "safe mode (tm --no-mods): built-in theme, icons, layout and keymap; mods' data is not loaded"

// loadMods is the registry of the user's mods with config.toml's tables;
// none when ~/.terminatr/mods can't be read. In safe mode only the
// bundled mods stay.
func loadMods(cfg *config.Config) *mod.Registry {
	r, err := mod.Load(cfg)
	if err != nil {
		return &mod.Registry{}
	}
	if safeMode() {
		r.Mods = slices.DeleteFunc(slices.Clone(r.Mods), func(m mod.Mod) bool { return !m.Builtin })
	}
	return r
}

// modChoices are the built-in choices (the bundled mods', by their
// names: default, unicode…), then the enabled user mods that provide
// kind, in the order they were enabled.
func modChoices(builtin []string, kind string) []string {
	cfg, _ := config.Load()
	out := append([]string(nil), builtin...)
	for _, m := range loadMods(cfg).Providers(kind) {
		if !m.Builtin {
			out = append(out, m.ID)
		}
	}
	return out
}

// Themes

// The console's theme table and scheme, from which setTheme resolves
// th().
var (
	themeTab   atomic.Pointer[themeTable]
	themeDark  atomic.Bool
	themeValue atomic.Value
)

func init() {
	themeTab.Store(&builtinTheme)
	themeDark.Store(true)
	themeValue.Store(ThemeDefault)
}

// modTheme reads mod id's theme.
func modTheme(r *mod.Registry, id string) (themeTable, error) {
	tt, err := tmtheme.Load(r, id)
	if err != nil {
		return builtinTheme, err
	}
	// Resolve both schemes now, so a bad colour is said once, here.
	for _, dark := range []bool{false, true} {
		if _, err := themeFor(tt, dark, true); err != nil {
			return builtinTheme, fmt.Errorf("%s theme: %w", id, err)
		}
	}
	return tt, nil
}

// useTheme makes the theme value names ("default" or a mod id) this
// console's, for its scheme. A theme that doesn't load leaves the
// built-in one, and the error says why.
func useTheme(r *mod.Registry, value string) error {
	tab, err := &builtinTheme, error(nil)
	if value != "" && value != ThemeDefault {
		if tt, e := modTheme(r, value); e != nil {
			err = e
		} else {
			tab = &tt
		}
	}
	themeTab.Store(tab)
	tt := *tab
	themeValue.Store(value)
	if err2 := setTheme(tt, themeDark.Load()); err == nil {
		err = err2
	}
	return err
}

// setScheme follows the console's colour scheme, light or dark: the
// terminal's background (OSC 11), in the dashboard and in a session
// alike. It reports whether the scheme changed.
func setScheme(dark bool) bool {
	if themeDark.Swap(dark) == dark {
		return false
	}
	setTheme(*themeTab.Load(), dark)
	return true
}

// themed reports whether the console's theme is a mod's, whose light and
// dark tables may differ: only then is the terminal asked its
// background.
func themed() bool { return themeTab.Load() != &builtinTheme }

// ThemeTerminal is how Settings names the built-in theme: it draws with
// the terminal's own colours, and Claude panes follow them too.
const ThemeTerminal = "follow terminal"

// themeLabel is a theme pick as Settings shows it.
func themeLabel(v string) string {
	if v == ThemeDefault {
		return ThemeTerminal
	}
	return v
}

// themeSetting is the theme setting's value this console uses.
func themeSetting() string {
	if v := themeValue.Load().(string); v != "" {
		return v
	}
	return ThemeDefault
}

// Icon sets from mods

// modIcons reads mod id's icon set.
func modIcons(r *mod.Registry, id string) (*iconSet, error) {
	m, err := r.Provider(mod.KindIcons, id)
	if err != nil {
		return nil, err
	}
	data, err := m.Data(mod.KindIcons)
	if err != nil {
		return nil, err
	}
	set, err := parseIcons(data)
	if err != nil {
		return nil, fmt.Errorf("%s icons: %w", id, err)
	}
	set.name = id
	return set, nil
}

// Layout

// modLayout is the layout value names ("default", the bundled
// default-layout mod's, or a mod id), the whole of it: the sidebar's
// columns and every other part it sets. One that doesn't load is the
// built-in layout, and the error says why.
func modLayout(r *mod.Registry, value string) (mod.Layout, error) {
	var data []byte
	if value == "" || value == ThemeDefault {
		value, data = mod.BundledLayout, mod.BundledData(mod.BundledLayout, mod.KindLayout)
	} else {
		m, err := r.Provider(mod.KindLayout, value)
		if err != nil {
			return mod.Layout{}, err
		}
		if data, err = m.Data(mod.KindLayout); err != nil {
			return mod.Layout{}, err
		}
	}
	l, err := mod.ParseLayout(data)
	if err != nil {
		return mod.Layout{}, fmt.Errorf("%s layout: %w", value, err)
	}
	return l, nil
}

// layoutColumns is the sidebar's column order a layout gives: nil for
// the built-in order, view.DefaultColumns, which the view keeps as nil.
func layoutColumns(l mod.Layout) []string {
	cols := view.Columns(l.Columns)
	if slices.Equal(cols, view.DefaultColumns) {
		return nil
	}
	return cols
}

// layoutVal is the layout setting's value this console uses, and
// layoutSpec its layout (nil until configLayout read it).
var (
	layoutVal  atomic.Value
	layoutSpec atomic.Pointer[mod.Layout]
)

func init() { layoutVal.Store(ThemeDefault) }

// layoutValue is the layout setting's value this console uses.
func layoutValue() string { return layoutVal.Load().(string) }

// useLayoutValue makes value, whose layout is spec, this console's.
func useLayoutValue(value string, spec mod.Layout) {
	layoutVal.Store(value)
	layoutSpec.Store(&spec)
}

// currentLayout is this console's layout and its setting's value, read
// from config.toml the first time.
func currentLayout() (mod.Layout, string) {
	if l := layoutSpec.Load(); l != nil {
		return *l, layoutValue()
	}
	l, _ := configLayout()
	return l, layoutValue()
}

// layoutSetting is config.toml's [ui] layout, "default" when unset.
func layoutSetting(cfg *config.Config) string {
	if cfg == nil || cfg.Layout == "" {
		return ThemeDefault
	}
	return cfg.Layout
}

// configLayout is the layout config.toml's [ui] layout gives, which it
// makes this console's: the built-in one in safe mode, and for one that
// doesn't load (the error says why).
func configLayout() (mod.Layout, error) {
	cfg, _ := config.Load()
	if safeMode() {
		cfg = nil // the built-in layout
	}
	l, err := modLayout(loadMods(cfg), layoutSetting(cfg))
	useLayoutValue(layoutSetting(cfg), l)
	if err != nil {
		return l, fmt.Errorf("layout: %w", err)
	}
	return l, nil
}

// loadLook makes config.toml's theme, icons and keymap this console's,
// as it starts. The errors say which mod's data was left out.
func loadLook() error {
	cfg, _ := config.Load()
	r := loadMods(cfg)
	var errs []error
	theme := ThemeDefault
	if cfg != nil && cfg.Theme != "" && !safeMode() {
		theme = cfg.Theme
	}
	if err := useTheme(r, theme); err != nil {
		errs = append(errs, fmt.Errorf("theme: %w", err))
	}
	icons := configIcons(cfg)
	if safeMode() && !slices.Contains(IconChoices, icons) {
		icons = IconsAuto // a mod's set
	}
	if err := setIconsFrom(r, icons); err != nil {
		errs = append(errs, fmt.Errorf("icons: %w", err))
	}
	if err := loadKeymap(r); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// lookNote is loadLook's error as one line for a footer.
func lookNote(err error) string {
	if err == nil {
		if safeMode() {
			return safeLookNote
		}
		return ""
	}
	return "mods: " + oneLine(err.Error())
}

// Keymaps

// loadKeymap applies the keymaps to this console: the bundled
// default-keymap's (DefaultKeymap), with the enabled keymap mods'
// (mod.Registry.Keymap) over it. An action a mod binds takes the mod's
// keys; a key two mods bind goes to the one enabled first, and what
// ApplyKeymap refuses (an escape hatch, a fixed action, a clash with a
// built-in key) keeps its default. The errors name each mod.
func loadKeymap(r *mod.Registry) error {
	km, from, errs := r.Keymap()
	for id, keys := range DefaultKeymap() {
		if _, ok := km[id]; !ok && !slices.Equal(keys, defaultKeys(id)) {
			km[id], from[id] = keys, mod.BundledKeymap
		}
	}
	err := ApplyKeymap(km, ConfigPrefix())
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range j.Unwrap() {
			id, _, _ := strings.Cut(e.Error(), ":")
			errs = append(errs, fmt.Errorf("%s keymap: %w", cmp.Or(from[id], "a"), e))
		}
	} else if err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// lookSaid is the last loadLook note a console showed, so a dashboard
// coming back from a session doesn't say it again.
var lookSaid atomic.Value

// lookNews is loadLook's note when it is news: "" when it was said
// already, or there is none.
func lookNews(err error) string {
	n := lookNote(err)
	if prev, _ := lookSaid.Swap(n).(string); prev == n {
		return ""
	}
	return n
}

// DefaultKeymap is the bundled default-keymap mod's keymap.toml (mods
// design §11): the keys of every action that can be rebound, which the
// action tables' own keys match (TestDefaultKeymap). Keymap mods apply
// over it.
func DefaultKeymap() map[string][]string {
	km, err := mod.ParseKeymap(mod.BundledData(mod.BundledKeymap, mod.KindKeymap))
	if err != nil {
		panic("bundled keymap: " + err.Error())
	}
	return km
}

// defaultKeys are action id's keys in its table; nil for no such id.
func defaultKeys(id string) []string {
	for _, b := range slices.Concat(listBindables(), sideBindables(), panelBindables()) {
		if b.id == id {
			return b.keys
		}
	}
	return nil
}

// Load checks

// The console's checks of mods' data files (mod.DataChecks), for tm
// doctor and the Mods tab: what useTheme, setIcons and loadKeymap would
// refuse.
func init() {
	mod.DataChecks[mod.KindTheme] = func(data []byte) error {
		tt, err := parseTheme(data)
		for _, dark := range []bool{false, true} {
			if err == nil {
				_, err = themeFor(tt, dark, true)
			}
		}
		return err
	}
	mod.DataChecks[mod.KindIcons] = func(data []byte) error {
		_, err := parseIcons(data)
		return err
	}
	mod.DataChecks[mod.KindKeymap] = func(data []byte) error {
		km, err := mod.ParseKeymap(data)
		if err != nil {
			return err
		}
		// As the console would apply it over tm's keys: an action id
		// it lacks, a locked key, and a key two of the mod's own
		// actions (or one of tm's) want (T301 B3).
		_, _, _, err = planKeymap(km, ConfigPrefix())
		return err
	}
}
