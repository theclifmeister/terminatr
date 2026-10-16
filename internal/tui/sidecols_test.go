package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// TestSidebarColumnOrder: the grid follows the view's column order, and
// the tooltip finds each column where it is drawn.
func TestSidebarColumnOrder(t *testing.T) {
	m, _ := sideDash(t)
	m.sideCols = []string{"needs", "state", "count"} // paused and inactive come back at the end
	end := m.sideW() - 1 - sideGap
	for _, s := range []struct {
		x    int
		want string
	}{
		{end - 12, "needs you: it waits on you"}, // t-0005
		{end - 10, "working"},
		{end - 6, "progress"},
		{end - 1, ""}, // inactive: blank on a thread
	} {
		if got := sideTip(m.tree(), m.sideW(), m.h, m.sideFoot(), s.x, 5); got != s.want {
			t.Errorf("at %d: %q, want %q", s.x, got, s.want)
		}
	}
}

// TestSidebarModColumn: a mod column takes its slot in the grid, blank
// until its mod publishes cells, and the columns around it stay as they
// are; a narrow sidebar gives it up first (24: as without it); a mod column no mod declares
// takes no room.
func TestSidebarModColumn(t *testing.T) {
	freshConsole(t)
	order := []string{"count", "ci", "review-age.age", "state", "remote", "paused", "inactive", "needs"}
	// goldenTree's rows have the PR/CI mod's column (ci), as by default.
	prci := map[string]modColumn{mod.PRCIColumn: {width: 2, title: "CI"}}
	mods := map[string]modColumn{"review-age.age": {width: 4, title: "age"}, mod.PRCIColumn: prci[mod.PRCIColumn]}
	for _, set := range IconChoices[1:] {
		setIcons(set)
		withMod, undeclared := goldenTree(), goldenTree()
		for i := range withMod {
			withMod[i].cols = sideCols(order, mods)
			undeclared[i].cols = sideCols(order, prci)
		}
		for i, r := range goldenTree() {
			if r.kind == treeDivider {
				continue
			}
			cw := 44 // room for every label beside the slot in every set
			plain := ansi.Strip(treeCells(r, cw, false, false))
			got := ansi.Strip(treeCells(withMod[i], cw, false, false))
			if ansi.StringWidth(got) != cw {
				t.Errorf("%s row %d: %d cells wide, want %d", set, i, ansi.StringWidth(got), cw)
			}
			// state..needs: 5 columns of 2 cells; before them the slot.
			if a, b := ansi.Cut(got, cw-10, cw), ansi.Cut(plain, cw-10, cw); a != b {
				t.Errorf("%s row %d: %q, want %q", set, i, a, b)
			}
			if slot := ansi.Cut(got, cw-14, cw-10); slot != "    " {
				t.Errorf("%s row %d: the slot is %q", set, i, slot)
			}
			if a, b := ansi.Cut(got, cw-21, cw-14), ansi.Cut(plain, cw-17, cw-10); a != b {
				t.Errorf("%s row %d: count/%% and PR/CI %q, want %q", set, i, a, b)
			}
			if u := ansi.Strip(treeCells(undeclared[i], cw, false, false)); u != plain {
				t.Errorf("%s row %d: an undeclared mod column took room:\n%q\n%q", set, i, u, plain)
			}
			narrow := ansi.Strip(treeCells(withMod[i], 24, false, false))
			if want := ansi.Strip(treeCells(r, 24, false, false)); narrow != want {
				t.Errorf("%s row %d at 24: %q, want %q", set, i, narrow, want)
			}
		}
	}
	if c := sideCols(order, map[string]modColumn{"review-age.age": {width: 50}, mod.PRCIColumn: prci[mod.PRCIColumn]}); c[2].w != maxModCol {
		t.Errorf("a mod column %d wide", c[2].w)
	}
	c := sideCols(order, mods)[2]
	if got := gridTip(treeRow{kind: treeThread}, c); got != "age" {
		t.Errorf("tip %q", got)
	}
}
