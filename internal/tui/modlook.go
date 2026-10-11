package tui

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
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

// loadMods is the registry of the user's mods with config.toml's tables;
// none when ~/.terminatr/mods can't be read.
func loadMods(cfg *config.Config) *mod.Registry {
	r, err := mod.Load(cfg)
	if err != nil {
		return &mod.Registry{}
	}
	return r
}

// modChoices are the built-in choices, then the enabled mods that
// provide kind, in the order they were enabled.
func modChoices(builtin []string, kind string) []string {
	cfg, _ := config.Load()
	out := append([]string(nil), builtin...)
	for _, m := range loadMods(cfg).Providers(kind) {
		out = append(out, m.ID)
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
	m, err := r.Provider(mod.KindTheme, id)
	if err != nil {
		return builtinTheme, err
	}
	data, err := m.Data(mod.KindTheme)
	if err != nil {
		return builtinTheme, err
	}
	tt, err := parseTheme(data)
	if err != nil {
		return builtinTheme, fmt.Errorf("%s theme: %w", id, err)
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

// setScheme follows the console's colour scheme, light or dark (mode
// 2031, or the terminal's background).
func setScheme(dark bool) {
	if themeDark.Swap(dark) != dark {
		setTheme(*themeTab.Load(), dark)
	}
}

// themed reports whether the console's theme is a mod's, whose light and
// dark tables may differ: only then is the terminal asked its
// background.
func themed() bool { return themeTab.Load() != &builtinTheme }

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

// modLayout is the sidebar's column order layout names ("default" or a
// mod id): nil for the built-in order.
func modLayout(r *mod.Registry, value string) ([]string, error) {
	if value == "" || value == ThemeDefault {
		return nil, nil
	}
	m, err := r.Provider(mod.KindLayout, value)
	if err != nil {
		return nil, err
	}
	data, err := m.Data(mod.KindLayout)
	if err != nil {
		return nil, err
	}
	l, err := mod.ParseLayout(data)
	if err != nil {
		return nil, fmt.Errorf("%s layout: %w", value, err)
	}
	return view.Columns(l.Sidebar), nil
}

// layoutVal is the layout setting's value this console uses.
var layoutVal atomic.Value

func init() { layoutVal.Store(ThemeDefault) }

// layoutValue is the layout setting's value this console uses.
func layoutValue() string { return layoutVal.Load().(string) }

// layoutSetting is config.toml's [ui] layout, "default" when unset.
func layoutSetting(cfg *config.Config) string {
	if cfg == nil || cfg.Layout == "" {
		return ThemeDefault
	}
	return cfg.Layout
}

// configLayout is the column order config.toml's [ui] layout gives:
// nil for the built-in one, and for one that doesn't load (the error
// says why).
func configLayout() ([]string, error) {
	cfg, _ := config.Load()
	layoutVal.Store(layoutSetting(cfg))
	cols, err := modLayout(loadMods(cfg), layoutSetting(cfg))
	if err != nil {
		return nil, fmt.Errorf("layout: %w", err)
	}
	return cols, nil
}

// loadLook makes config.toml's theme, icons and keymap this console's,
// as it starts. The errors say which mod's data was left out.
func loadLook() error {
	cfg, _ := config.Load()
	r := loadMods(cfg)
	var errs []error
	theme := ThemeDefault
	if cfg != nil && cfg.Theme != "" {
		theme = cfg.Theme
	}
	if err := useTheme(r, theme); err != nil {
		errs = append(errs, fmt.Errorf("theme: %w", err))
	}
	if err := setIconsFrom(r, configIcons(cfg)); err != nil {
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
		return ""
	}
	return "mods: " + oneLine(err.Error())
}

// Keymaps

// loadKeymap applies the enabled keymap mods (mod.Registry.Keymap) to
// this console: a key two mods bind goes to the one enabled first, and
// what ApplyKeymap refuses (an escape hatch, a fixed action, a clash
// with a built-in key) keeps its default. The errors name each mod.
func loadKeymap(r *mod.Registry) error {
	km, from, errs := r.Keymap()
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
