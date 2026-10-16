package tui

// Wrappers that give the tests a simpler signature than the real entry
// points (coordPanel, infoLay, sideLines, detailsLay…): nothing in the
// program calls them.

import (
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// coordLines draws a coordinator's panel w cells wide, with what a click
// on each line does.
func coordLines(pw *proto.ProjectWatch, w int, now time.Time) ([]string, []infoHit) {
	pl, hits := coordPanel(pw, w, now)
	return pl.lines, hits
}

// infoLines draws the panel's content w cells wide (its border not
// included), with what a click on each line does. now dates the ages.
func infoLines(d *infoData, w int, now time.Time) ([]string, []infoHit) {
	lay := infoLay(d, w, now, panelOpts{})
	return lay.lines, lay.hits
}

// sidebarLines draws the sidebar on the left with nothing needing you:
// sideLines.
func sidebarLines(all []treeRow, w, h int, foot []string) []string {
	return sideLines(all, w, h, sideHeadIn{}, foot, false)
}

// loadIcons makes config.toml's icons setting this console's set.
func loadIcons() string {
	cfg, _ := config.Load()
	v := configIcons(cfg)
	setIconsFrom(loadMods(cfg), v)
	return v
}

// resetConsole forgets the filter, the places and the tooltip (tests).
func resetConsole() {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	thisConsole.filter, thisConsole.now, thisConsole.last = sideFilterState{}, place{}, place{}
	thisConsole.known, thisConsole.lastKnown, thisConsole.hover = false, false, ""
}

// freshConsole puts the console state the package keeps for the whole
// process back as a console starts, now and when t ends: the sidebar's
// filter, places and tooltip, the theme and its scheme, the icons, the
// layout and the notes said once. The keymap is defaultKeymap's.
func freshConsole(t testing.TB) {
	t.Helper()
	fresh := func() {
		resetConsole()
		setScheme(true)
		useTheme(&mod.Registry{}, ThemeDefault)
		setIcons(IconsUnicode)
		layoutVal.Store(ThemeDefault)
		layoutSpec.Store(nil)
		lookSaid.Store("")
		oldConsoleSaid.Store(false)
	}
	fresh()
	t.Cleanup(fresh)
}

// defaultKeymap puts the default keymap back when t ends. The keymap is
// read without a lock (ApplyKeymap), so only a test that changes it sets
// it back: a session an earlier test started may still be drawing.
func defaultKeymap(t testing.TB) {
	t.Cleanup(func() { ApplyKeymap(nil, DefaultPrefixKey) })
}

// sideGrid is the width of the default grid on the right of every row
// of the full sidebar without mod columns: the count/% column, then five
// glyph columns two cells wide (treeCells). The PR/CI column is the
// pr-ci-column mod's: two cells more while it is on.
const sideGrid = pctCol + 5*2

// details is the panel for r, w cells wide, styled.
func (m *dash) details(r row, w int) []string { return m.detailsLay(r, w, 0).lines }
