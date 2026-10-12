package tui

// The dashboard's geometry (the layout v2 design §4, L2): where the
// sidebar, the header, the body (the list, and the details panel beside
// it) and the footer are in the window, in one place, one method each,
// so the layout's parts change one region at a time. Rects are in
// window cells; the dashboard's own code works in its area's columns
// (x less main().x) and the window's rows.

// rect is a region of the window: x, y its top left cell, w by h.
type rect struct{ x, y, w, h int }

// hasX says whether column x is in r.
func (r rect) hasX(x int) bool { return x >= r.x && x < r.x+r.w }

// hasY says whether row y is in r.
func (r rect) hasY(y int) bool { return y >= r.y && y < r.y+r.h }

// dashGeo is the dashboard's geometry in a window w by h, the sidebar
// sideW wide, under the effective layout l.
type dashGeo struct {
	w, h  int
	sideW int
	l     Layout
}

// geom is the dashboard's geometry now.
func (m *dash) geom() dashGeo {
	return dashGeo{w: m.winW, h: m.h, sideW: m.sideW(), l: m.layout}
}

// side is the sidebar: the window's left columns.
func (g dashGeo) side() rect { return rect{0, 0, g.sideW, g.h} }

// main is the dashboard's own area: the window less the sidebar, at
// least a column.
func (g dashGeo) main() rect { return rect{g.sideW, 0, max(g.w-g.sideW, 1), g.h} }

// footRows is the footer's height: a rule, the keys, the message.
const footRows = 3

// header is the dashboard's top row: its title and the server's state.
func (g dashGeo) header() rect {
	a := g.main()
	return rect{a.x, 0, a.w, 1}
}

// footer is the dashboard's bottom rows: a rule, the keys, the message.
func (g dashGeo) footer() rect {
	a := g.main()
	return rect{a.x, g.h - footRows, a.w, footRows}
}

// keysRow is the footer's row of key hints, whose buttons take clicks.
func (g dashGeo) keysRow() int { return g.footer().y + 1 }

// body is the room between the header and the footer, at least a row.
func (g dashGeo) body() rect {
	a := g.main()
	return rect{a.x, 1, a.w, max(g.h-1-footRows, 1)}
}

// split says whether the details panel shows beside the list, and the
// list's width (in the dashboard's columns) when it does: a panel
// given in cells keeps them while the list keeps its least share.
func (g dashGeo) split() (bool, int) {
	w := g.main().w
	if !g.l.Details || w < splitMin {
		return false, w
	}
	if c := g.l.detCells; c > 0 {
		return true, min(max(w-c-1, int(float64(w)*minSplit+0.5)), int(float64(w)*maxSplit+0.5))
	}
	return true, int(float64(w)*g.l.Split + 0.5)
}

// list is the list: the body, or its left part beside the details
// panel.
func (g dashGeo) list() rect {
	b := g.body()
	if split, lw := g.split(); split {
		b.w = lw
	}
	return b
}

// divider is the details panel's divider column, beside the list; -1
// with no panel.
func (g dashGeo) divider() int {
	if split, lw := g.split(); split {
		return g.body().x + lw
	}
	return -1
}

// details is the details panel right of the divider; empty (w 0) with
// no panel.
func (g dashGeo) details() rect {
	b := g.body()
	split, lw := g.split()
	if !split {
		return rect{b.x + b.w, b.y, 0, b.h}
	}
	return rect{b.x + lw + 1, b.y, b.w - lw - 1, b.h}
}

// bodyRows is the room between the header and the footer.
func (m *dash) bodyRows() int { return m.geom().body().h }
