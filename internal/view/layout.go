package view

// The geometry: where a view's pane goes in a window. The server lays
// the view out at its Latest client's window to size the session; every
// client lays it out at the same size to draw it, cropping or padding
// the frame to its own window. Both use Lay, so they agree.

// Rect is a rectangle of window cells, 0-based.
type Rect struct{ X, Y, W, H int }

// Geometry is a view laid out in a window.
type Geometry struct {
	// SideW is the projects sidebar's width, 0 for none, and SideX its
	// first column: 0, or the window's last SideW on the right; InfoW the
	// info panel's right of the pane, 0 for none; Status is the status
	// bar's height with the rule above it (0, 1 compact without the
	// rule, or 2). Area is the
	// pane's: the window less those. On the right the order is the pane,
	// the info panel, the sidebar.
	SideW, SideX, InfoW, Status int
	Area                        Rect
	// Pane is the session shown in Area, "" for none.
	Pane string
}

// Chrome is the sidebar's width and the height of the status bar with
// the rule between it and the pane, of v in a window cols wide.
// Every view has the sidebar, 0 wide when hidden; a bare one has the
// status bar only when asked for; a compact one has no rule.
func (v *View) Chrome(cols int) (sideW, status int) {
	status = 2
	switch {
	case v.Bare && !v.StatusBar:
		status = 0
	case v.StatusCompact:
		status = 1
	}
	return v.Sidebar.Cols(cols), status
}

// Lay lays v out in a window of cols×rows.
func (v *View) Lay(cols, rows int) Geometry {
	sideW, status := v.Chrome(cols)
	infoW := 0
	if v.Panel && v.Mode == ModeLayout {
		infoW = v.Info.Cols(cols, sideW)
	}
	g := Geometry{SideW: sideW, InfoW: infoW, Status: status, Pane: v.Shown(),
		Area: Rect{sideW, 0, max(cols-sideW-infoW, 1), max(rows-status, 1)}}
	if v.Sidebar.Right() {
		g.SideX, g.Area.X = max(cols-sideW, 0), 0
	}
	return g
}
