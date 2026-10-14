package tui

// The dashboard's geometry (the layout v2 design §4, L2): where the
// sidebar, the header, the body (the list, and the details panel beside
// or under it) and the footer are in the window, in one place, one method each,
// so the layout's parts change one region at a time. Rects are in
// window cells; the dashboard's own code works in its area's columns
// (x less main().x) and the window's rows.

// rect is a region of the window: x, y its top left cell, w by h.
type rect struct{ x, y, w, h int }

// hasX says whether column x is in r.
func (r rect) hasX(x int) bool { return x >= r.x && x < r.x+r.w }

// hasY says whether row y is in r.
func (r rect) hasY(y int) bool { return y >= r.y && y < r.y+r.h }

// has says whether cell (x, y) is in r.
func (r rect) has(x, y int) bool { return r.hasX(x) && r.hasY(y) }

// dashGeo is the dashboard's geometry in a window w by h, the sidebar
// sideW wide, under the effective layout l.
type dashGeo struct {
	w, h  int
	sideW int
	l     Layout
	// noPanel: no details panel, whatever the layout (the remote
	// dashboard, peerdash.go, has no rows to detail).
	noPanel bool
}

// geom is the dashboard's geometry now.
func (m *dash) geom() dashGeo {
	return dashGeo{w: m.winW, h: m.h, sideW: m.sideW(), l: m.layout, noPanel: m.remoteHere()}
}

// side is the sidebar: the window's left columns, or its right ones;
// none (w 0) while hidden.
func (g dashGeo) side() rect {
	if g.l.Sidebar.Right() {
		return rect{max(g.w-g.sideW, 0), 0, g.sideW, g.h}
	}
	return rect{0, 0, g.sideW, g.h}
}

// main is the dashboard's own area: the window less the sidebar, at
// least a column.
func (g dashGeo) main() rect {
	if g.l.Sidebar.Right() {
		return rect{0, 0, max(g.w-g.sideW, 1), g.h}
	}
	return rect{g.sideW, 0, max(g.w-g.sideW, 1), g.h}
}

// sideCol is window column x as the sidebar's own drawing has it, -1
// off the sidebar (sideLocal).
func (g dashGeo) sideCol(x int) int {
	s := g.side()
	return sideLocal(x, s.x, s.w, g.l.Sidebar.Right())
}

// headRows is the header's height: a row, none when hidden.
func (g dashGeo) headRows() int {
	if g.l.header == styleHidden {
		return 0
	}
	return 1
}

// footRows is the footer's height: a rule, the keys and the message;
// compact, the keys or the message; hidden, none.
func (g dashGeo) footRows() int {
	switch g.l.footer {
	case styleHidden:
		return 0
	case styleCompact:
		return 1
	}
	return 3
}

// header is the dashboard's top row: its title and the server's state;
// no rows (h 0) when hidden, when its alarms and the needs-you badge
// draw over the body's first row, at the same y (chrome.go).
func (g dashGeo) header() rect {
	a := g.main()
	return rect{a.x, 0, a.w, g.headRows()}
}

// footer is the dashboard's bottom rows: a rule, the keys, the message;
// one row compact, none (h 0) hidden.
func (g dashGeo) footer() rect {
	a := g.main()
	n := g.footRows()
	return rect{a.x, g.h - n, a.w, n}
}

// keysRow is the footer's row of key hints, whose buttons take clicks;
// -1 for none. A compact footer's row has them while it shows no
// message (dash.shownKeys says).
func (g dashGeo) keysRow() int {
	f := g.footer()
	switch f.h {
	case 0:
		return -1
	case 1:
		return f.y
	}
	return f.y + 1
}

// body is the room between the header and the footer, at least a row.
func (g dashGeo) body() rect {
	a := g.main()
	top := g.headRows()
	return rect{a.x, top, a.w, max(g.h-top-g.footRows(), 1)}
}

// panel is where the details panel shows: right of the list when the
// dashboard is splitMin columns or wider, under it when the body has
// bottomMin rows or more, else "" (also with the panel off): the
// selected thread's details then show inline under its row.
func (g dashGeo) panel() string {
	switch p := g.l.place(); {
	case g.noPanel:
	case p == placeRight && g.main().w >= splitMin:
		return placeRight
	case p == placeBottom && g.body().h >= bottomMin:
		return placeBottom
	}
	return ""
}

// listW is the list's width beside the right panel (in the dashboard's
// columns): a panel given in cells keeps them while the list keeps its
// least share.
func (g dashGeo) listW() int {
	w := g.main().w
	if c := g.l.detCells; c > 0 {
		return min(max(w-c-1, int(float64(w)*minSplit+0.5)), int(float64(w)*maxSplit+0.5))
	}
	return int(float64(w)*g.l.Split + 0.5)
}

// listH is the list's height above the bottom panel: the body less the
// panel's share (or its rows and the divider), the list and the panel,
// its divider in, keeping bottomMin/2 rows each.
func (g dashGeo) listH() int {
	h := g.body().h
	lh := h - int(float64(h)*g.l.rows()+0.5)
	if c := g.l.rowCells; c > 0 {
		lh = h - c - 1
	}
	lo := max(bottomMin/2, int(float64(h)*(1-maxRows)+0.5))
	hi := min(h-bottomMin/2, int(float64(h)*(1-minRows)+0.5))
	return min(max(lh, lo), hi)
}

// list is the list: the body, or its part beside or above the details
// panel.
func (g dashGeo) list() rect {
	b := g.body()
	switch g.panel() {
	case placeRight:
		b.w = g.listW()
	case placeBottom:
		b.h = g.listH()
	}
	return b
}

// divider is the details panel's divider: a column right of the list,
// or a row under it; empty (w or h 0) with no panel.
func (g dashGeo) divider() rect {
	b, l := g.body(), g.list()
	switch g.panel() {
	case placeRight:
		return rect{l.x + l.w, b.y, 1, b.h}
	case placeBottom:
		return rect{b.x, l.y + l.h, b.w, 1}
	}
	return rect{b.x + b.w, b.y, 0, b.h}
}

// details is the details panel past the divider; empty (w 0) with no
// panel.
func (g dashGeo) details() rect {
	b, d := g.body(), g.divider()
	switch g.panel() {
	case placeRight:
		return rect{d.x + 1, b.y, b.x + b.w - d.x - 1, b.h}
	case placeBottom:
		return rect{b.x, d.y + 1, b.w, b.y + b.h - d.y - 1}
	}
	return rect{b.x + b.w, b.y, 0, b.h}
}

// bodyRows is the room between the header and the footer.
func (m *dash) bodyRows() int { return m.geom().body().h }
