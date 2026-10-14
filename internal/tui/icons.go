package tui

import (
	"os"
	"slices"
	"sync/atomic"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// Icon sets (docs/SPEC.md §4): the glyphs of the sidebar's tree and of
// the states, todos and progress bars wherever tm draws them. The global
// setting picks one of auto, nerd, unicode or ascii; auto is decided by
// each console for itself, since the font belongs to the terminal you
// look at: Nerd Font icons in Ghostty, which bundles the Nerd Font
// symbols, plain Unicode elsewhere. Every glyph is one cell wide (two for
// a connector), so columns line up in each set (TestIconWidths); none is
// an emoji, whose width differs between terminals. A Nerd Font icon is
// one cell to the terminal's count but draws over the blank after it, so
// the sidebar gives every glyph a column two cells wide: a Unicode glyph
// sits in its left cell, a Nerd Font icon fills both.

// The values of the icons setting.
const (
	IconsAuto    = "auto"
	IconsNerd    = "nerd"
	IconsUnicode = "unicode"
	IconsASCII   = "ascii"
)

// IconChoices are the icons setting's values, in the order enter steps
// through them.
var IconChoices = []string{IconsAuto, IconsNerd, IconsUnicode, IconsASCII}

// iconSet is one set of glyphs.
type iconSet struct {
	name string
	// wide: the icons draw two cells wide (a Nerd Font's), so the tree's
	// folder, coordinator and thread marks take a blank after them.
	wide bool
	// The tree: a project's folder (open: the current one), the
	// connectors of the rows under it (two cells), and the marks before
	// the coordinator's and a thread's label ("" for none).
	folder, folderOpen string
	mid, end           string
	coord, thread      string
	// hint marks a project one of whose threads is blocked or waiting, or
	// one of whose tasks needs you;
	// current marks the current project in the slim strip; remote follows
	// a coordinator with remote control on; paused follows a paused
	// project's name, inactive an inactive one's.
	hint, current, remote, paused, inactive string
	// The states (stateLook): none is a coordinator that doesn't run,
	// other a stopped, exited or resolved one.
	working, blocked, idle, starting, running, review, done, other, none string
	// A thread's pull request checks: passing, failing, running.
	ciPass, ciFail, ciRun string
	// The inactive divider's mark: folded, unfolded.
	fold, unfold string
	// linked marks a server header whose link is up, and this server's
	// (sidepeers.go); connecting, offline and refused ones take starting,
	// idle and inactive.
	linked string
	// Todos and steps (todoGlyph), and the progress bar's cells (bar).
	todoDone, todoNow, todoOpen string
	barOn, barOff               string
}

// The built-in sets are the bundled icon mods' (mods design §11):
// unicode-icons, nerd-icons and ascii-icons, each a whole set
// (bundledIcons).
var (
	unicodeIcons = bundledIcons(IconsUnicode)
	nerdIcons    = bundledIcons(IconsNerd)
	asciiIcons   = bundledIcons(IconsASCII)
)

// icons is this console's set, iconsValue the setting's value it came
// from.
var (
	icons      atomic.Pointer[iconSet]
	iconsValue atomic.Value
)

func init() { icons.Store(&unicodeIcons); iconsValue.Store(IconsUnicode) }

// ic is this console's icon set.
func ic() *iconSet { return icons.Load() }

// iconSetFor is the set a setting's value picks in a terminal whose
// TERM_PROGRAM is term.
func iconSetFor(value, term string) *iconSet {
	switch value {
	case IconsNerd:
		return &nerdIcons
	case IconsASCII:
		return &asciiIcons
	case IconsUnicode:
		return &unicodeIcons
	}
	if term == "ghostty" {
		return &nerdIcons
	}
	return &unicodeIcons
}

// setIcons makes the setting's value this console's icon set: a
// built-in choice, or an enabled icons mod's id. A mod's set that
// doesn't load leaves auto's, and the error says why.
func setIcons(value string) error {
	if slices.Contains(IconChoices, value) {
		return setIconsFrom(nil, value)
	}
	cfg, _ := config.Load()
	return setIconsFrom(loadMods(cfg), value)
}

// setIconsFrom is setIcons with the mods in r.
func setIconsFrom(r *mod.Registry, value string) error {
	set, err := iconSetFor(value, os.Getenv("TERM_PROGRAM")), error(nil)
	if !slices.Contains(IconChoices, value) && r != nil {
		if s, e := modIcons(r, value); e != nil {
			err = e
		} else {
			set = s
		}
	}
	icons.Store(set)
	iconsValue.Store(value)
	return err
}

// iconsSetting is the icons setting's value this console uses.
func iconsSetting() string { return iconsValue.Load().(string) }

// configIcons is config.toml's [ui] icons (cfg, nil when it doesn't
// load): auto when the file or the key is missing.
func configIcons(cfg *config.Config) string {
	if cfg == nil || cfg.Icons == "" {
		return IconsAuto
	}
	return cfg.Icons
}

// loadIcons makes config.toml's icons setting this console's set.
func loadIcons() string {
	cfg, _ := config.Load()
	v := configIcons(cfg)
	setIconsFrom(loadMods(cfg), v)
	return v
}
