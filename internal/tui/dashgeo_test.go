package tui

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// TestDashGeo: with the default layout the regions are where the
// dashboard always drew them: the sidebar at the left, a header row,
// the body, the details panel right of the divider at the list's share,
// and three footer rows.
func TestDashGeo(t *testing.T) {
	g := dashGeo{w: 200, h: 40, sideW: 36, l: effectiveLayout(mod.Layout{}, DefaultLayout, "")}
	lw := 107 // 164 × the default split, 0.65
	for name, c := range map[string]struct{ got, want rect }{
		"side":    {g.side(), rect{0, 0, 36, 40}},
		"main":    {g.main(), rect{36, 0, 164, 40}},
		"header":  {g.header(), rect{36, 0, 164, 1}},
		"body":    {g.body(), rect{36, 1, 164, 36}},
		"list":    {g.list(), rect{36, 1, lw, 36}},
		"details": {g.details(), rect{36 + lw + 1, 1, 164 - lw - 1, 36}},
		"footer":  {g.footer(), rect{36, 37, 164, 3}},
	} {
		if c.got != c.want {
			t.Errorf("%s: %+v, want %+v", name, c.got, c.want)
		}
	}
	if g.keysRow() != 38 || g.divider() != 36+lw {
		t.Errorf("keys row %d, divider %d", g.keysRow(), g.divider())
	}

	// Narrow: no panel, the list is the body; a tiny window keeps a row.
	g.w = 36 + splitMin - 1
	if split, _ := g.split(); split || g.list() != g.body() || g.details().w != 0 || g.divider() != -1 {
		t.Errorf("narrow: %+v %+v", g.list(), g.details())
	}
	g.h = 3
	if g.body().h != 1 {
		t.Errorf("tiny: body %+v", g.body())
	}
}
