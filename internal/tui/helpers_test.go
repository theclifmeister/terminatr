package tui

// Wrappers that give the tests a simpler signature than the real entry
// points (coordPanel, infoLay, sideLines, detailsLay…): nothing in the
// program calls them.

import (
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
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

// resetConsole forgets the filter and the places (tests).
func resetConsole() {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	thisConsole.filter, thisConsole.now, thisConsole.last = sideFilterState{}, place{}, place{}
	thisConsole.known, thisConsole.lastKnown, thisConsole.hover = false, false, ""
}

// sideGrid is the width of the default grid on the right of every row
// of the full sidebar without mod columns: the count/% column, then five
// glyph columns two cells wide (treeCells). The PR/CI column is the
// pr-ci-column mod's: two cells more while it is on.
const sideGrid = pctCol + 5*2

// details is the panel for r, w cells wide, styled.
func (m *dash) details(r row, w int) []string { return m.detailsLay(r, w, 0).lines }
