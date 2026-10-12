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
	if g.keysRow() != 38 || g.divider() != (rect{36 + lw, 1, 1, 36}) || g.panel() != placeRight {
		t.Errorf("keys row %d, divider %d", g.keysRow(), g.divider())
	}

	// Narrow: no panel, the list is the body; a tiny window keeps a row.
	g.w = 36 + splitMin - 1
	if g.panel() != "" || g.list() != g.body() || g.details().w != 0 || g.divider().w != 0 {
		t.Errorf("narrow: %+v %+v", g.list(), g.details())
	}
	g.h = 3
	if g.body().h != 1 {
		t.Errorf("tiny: body %+v", g.body())
	}
}

// TestDashGeoBottom: a bottom panel is under the list, across the
// body, at its share of the rows (or its rows and the divider), the
// list and the panel 8 rows each at least; a shorter body has none.
func TestDashGeoBottom(t *testing.T) {
	spec := mod.Layout{Details: placeBottom}
	g := dashGeo{w: 200, h: 40, sideW: 36, l: effectiveLayout(spec, DefaultLayout, "")}
	// The body's 36 rows less the default 40%, 14.
	for name, c := range map[string]struct{ got, want rect }{
		"list":    {g.list(), rect{36, 1, 164, 22}},
		"divider": {g.divider(), rect{36, 23, 164, 1}},
		"details": {g.details(), rect{36, 24, 164, 13}},
	} {
		if c.got != c.want {
			t.Errorf("%s: %+v, want %+v", name, c.got, c.want)
		}
	}
	if g.panel() != placeBottom {
		t.Errorf("panel %q", g.panel())
	}

	// In rows: the panel's rows under the divider.
	g.l.rowCells = 10
	if g.details().h != 10 || g.list().h != 25 {
		t.Errorf("10 rows: %+v %+v", g.list(), g.details())
	}
	// More than leaves the list its least share: 30% of the rows.
	g.l.rowCells = 100
	if g.list().h != 11 {
		t.Errorf("100 rows: %+v", g.list())
	}

	// 16 rows: the list 8, the divider and 7; 15: none.
	g.l.rowCells = 0
	g.h = 16 + 1 + g.footRows()
	if g.list().h != 8 || g.details().h != 7 {
		t.Errorf("16 rows: %+v %+v", g.list(), g.details())
	}
	g.h--
	if g.panel() != "" || g.list() != g.body() || g.details().w != 0 || g.divider().w != 0 {
		t.Errorf("15 rows: %+v %+v", g.list(), g.details())
	}

	// Off: no panel, at any size.
	g = dashGeo{w: 200, h: 40, sideW: 36, l: effectiveLayout(mod.Layout{Details: placeOff}, DefaultLayout, "")}
	if g.panel() != "" || g.list() != g.body() {
		t.Errorf("off: %+v", g.list())
	}
}

// TestDashGeoChrome: the header's and the footer's styles give their
// rows to the body; the keys row follows the footer, none when hidden.
func TestDashGeoChrome(t *testing.T) {
	for _, c := range []struct {
		header, footer string
		body           rect
		footer1        rect
		keys           int
	}{
		{"", "", rect{36, 1, 164, 36}, rect{36, 37, 164, 3}, 38},
		{"hidden", "", rect{36, 0, 164, 37}, rect{36, 37, 164, 3}, 38},
		{"", "compact", rect{36, 1, 164, 38}, rect{36, 39, 164, 1}, 39},
		{"hidden", "hidden", rect{36, 0, 164, 40}, rect{36, 40, 164, 0}, -1},
	} {
		spec := mod.Layout{Header: c.header, Footer: c.footer}
		g := dashGeo{w: 200, h: 40, sideW: 36, l: effectiveLayout(spec, DefaultLayout, "")}
		if g.body() != c.body || g.footer() != c.footer1 || g.keysRow() != c.keys {
			t.Errorf("%q/%q: body %+v footer %+v keys %d", c.header, c.footer, g.body(), g.footer(), g.keysRow())
		}
		if g.header().h != 1-min(len(c.header), 1) || g.header().y != 0 {
			t.Errorf("%q: header %+v", c.header, g.header())
		}
	}
}
