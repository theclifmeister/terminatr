package tui

import (
	"cmp"
	"encoding/json"
	"maps"
	"math"
	"os"
	"slices"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The layout: whether the dashboard's details panel shows beside the
// list, how wide the list is, the projects sidebar (sidebar.go) and the
// info panel beside a thread's pane (infopanel.go). It
// is this console's view preference, so it lives in ui.json next to
// config.toml, which holds the human's settings (docs/SPEC.md §5.1).

// Layout is ui.json. As the console's layout (dash.layout) it is the
// effective one: ui.json resolved against the layout mod in use
// (effectiveLayout), which also fills in what only a layout mod sets.
type Layout struct {
	// Details shows the details panel beside the list when the dashboard,
	// the window less the sidebar, is at least splitMin columns wide.
	Details bool
	// DetailsPlace is where the panel shows: right, bottom or off; ""
	// is right (ui.json writes only bottom: Details says on or off).
	DetailsPlace string
	// Split is the list's share of the window's width, between minSplit
	// and maxSplit.
	Split float64
	// DetailsRows is the bottom panel's share of the body's rows, 0 for
	// the default.
	DetailsRows float64
	// Sidebar is the projects sidebar's width, on the dashboard and while
	// attached alike.
	Sidebar SidebarLayout
	// SideMode is the sidebar's mode: full, slim or hidden; "" is what
	// Sidebar.Slim says (ui.json writes only hidden).
	SideMode string
	// Info is the info panel's beside a thread's pane, while attached.
	Info view.Info
	// Tree is what the sidebar's tree folds: new views start with it.
	Tree view.Tree
	// Layouts are the console's own changes under each layout mod, by
	// its id: of a part the layout sets (tweak). Only in ui.json.
	Layouts map[string]LayoutTweak

	// What effectiveLayout resolved from the layout mod, not in ui.json.
	set       layoutSet // the parts the layout (or a tweak under it) sets
	detCells  int       // the right panel's width in cells, 0 for Split's
	rowCells  int       // the bottom panel's height in rows, 0 for DetailsRows'
	sideShare float64   // the sidebar's width as a share of the window, 0 for Sidebar.Width
	side      string    // sidebar.side: left, right
	header    string    // header.style: shown, hidden
	footer    string    // footer.style: shown, compact, hidden
	statusBar string    // status_bar.style: shown, compact
	density   string    // rows.density: normal, compact
	sections  []string  // dashboard.sections, needs and project always in
}

// layoutSet says which of the console's parts the layout in use sets:
// a change to one is kept under its id (tweak).
type layoutSet struct {
	place, split, rows, width, mode bool
}

// LayoutTweak is the console's own change under one layout mod
// (ui.json layouts.<id>), of the parts that layout sets. Zero values
// are parts not changed.
type LayoutTweak struct {
	DetailsPlace string        `json:"details_place,omitempty"`
	Split        float64       `json:"split,omitempty"`
	DetailsRows  float64       `json:"details_rows,omitempty"`
	Sidebar      *SidebarTweak `json:"sidebar,omitempty"`
	// Seq orders the entries, the latest changed highest: past
	// maxTweaks the lowest goes.
	Seq int `json:"seq"`
}

// SidebarTweak is a LayoutTweak's sidebar.
type SidebarTweak struct {
	Width int    `json:"width,omitempty"`
	Mode  string `json:"mode,omitempty"`
}

// maxTweaks is how many layouts ui.json keeps changes under.
const maxTweaks = 16

// layoutFile is Layout as ui.json writes it.
type layoutFile struct {
	Details      bool                   `json:"details"`
	DetailsPlace string                 `json:"details_place,omitempty"`
	Split        float64                `json:"split"`
	DetailsRows  float64                `json:"details_rows,omitempty"`
	Sidebar      sidebarFile            `json:"sidebar"`
	Info         view.Info              `json:"info"`
	Tree         view.Tree              `json:"tree"`
	Layouts      map[string]LayoutTweak `json:"layouts,omitempty"`
}

// sidebarFile is ui.json's sidebar: the view's, and the mode (mode wins
// over slim).
type sidebarFile struct {
	SidebarLayout
	Mode string `json:"mode,omitempty"`
}

func (l Layout) MarshalJSON() ([]byte, error) {
	return json.Marshal(layoutFile{Details: l.Details, DetailsPlace: l.DetailsPlace, Split: l.Split, DetailsRows: l.DetailsRows,
		Sidebar: sidebarFile{l.Sidebar, l.SideMode}, Info: l.Info, Tree: l.Tree, Layouts: l.Layouts})
}

func (l *Layout) UnmarshalJSON(b []byte) error {
	f := layoutFile{Details: l.Details, DetailsPlace: l.DetailsPlace, Split: l.Split, DetailsRows: l.DetailsRows,
		Sidebar: sidebarFile{l.Sidebar, l.SideMode}, Info: l.Info, Tree: l.Tree, Layouts: l.Layouts}
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*l = Layout{Details: f.Details, DetailsPlace: f.DetailsPlace, Split: f.Split, DetailsRows: f.DetailsRows,
		Sidebar: f.Sidebar.SidebarLayout, SideMode: f.Sidebar.Mode, Info: f.Info, Tree: f.Tree, Layouts: f.Layouts}
	return nil
}

const (
	splitMin     = 120  // a narrower dashboard (less the sidebar) shows details under the row
	defaultSplit = 0.65 // the list's share
	minSplit     = 0.3  // of the width, either way
	maxSplit     = 0.8  //
	splitStep    = 0.05 // < and > move the divider this much
	defaultRows  = 0.4  // the bottom panel's share of the body
	minRows      = 0.2  // either way
	maxRows      = 0.7  //
	bottomMin    = 16   // a shorter body shows details under the row: the list and the panel 8 rows each
)

// The details panel's places and the sidebar's modes (layout.toml's
// details.place and sidebar.mode).
const (
	placeRight  = "right"
	placeBottom = "bottom"
	placeOff    = "off"
	modeFull    = "full"
	modeSlim    = "slim"
	modeHidden  = "hidden"
)

// DefaultLayout is the layout without a ui.json.
var DefaultLayout = Layout{Details: true, Split: defaultSplit, Sidebar: SidebarLayout{Width: sideDefault}, Info: view.Info{Width: view.InfoDefault}}

// LoadLayout reads path; a missing or unreadable file gives the default.
func LoadLayout(path string) Layout {
	l := DefaultLayout
	if path == "" {
		return l
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &l) != nil {
		return DefaultLayout
	}
	l.Split = clampSplit(l.Split)
	l.DetailsRows = clampRows(l.DetailsRows)
	if !slices.Contains([]string{placeRight, placeBottom, placeOff}, l.DetailsPlace) {
		l.DetailsPlace = ""
	}
	if !slices.Contains([]string{modeFull, modeSlim, modeHidden}, l.SideMode) {
		l.SideMode = ""
	}
	l.Sidebar = l.Sidebar.Clamp()
	l.Info = l.Info.Clamp()
	if l.Tree.Valid() != nil {
		l.Tree = view.Tree{}
	}
	for id, t := range l.Layouts {
		t.Split = clampSplit0(t.Split)
		t.DetailsRows = clampRows(t.DetailsRows)
		if !slices.Contains([]string{placeRight, placeBottom, placeOff}, t.DetailsPlace) {
			t.DetailsPlace = ""
		}
		if s := t.Sidebar; s != nil {
			if s.Width != 0 {
				s.Width = max(s.Width, view.SideMin)
			}
			if !slices.Contains([]string{modeFull, modeSlim, modeHidden}, s.Mode) {
				s.Mode = ""
			}
		}
		l.Layouts[id] = t
	}
	return l
}

// SaveLayout writes l to path, atomically.
func SaveLayout(path string, l Layout) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(path, append(b, '\n'), 0o600)
}

// clampSplit keeps a split in range, to two decimals.
func clampSplit(f float64) float64 {
	if f == 0 {
		return defaultSplit
	}
	return clampSplit0(f)
}

// clampSplit0 is clampSplit, keeping 0 for none.
func clampSplit0(f float64) float64 {
	if f == 0 {
		return 0
	}
	return math.Round(min(max(f, minSplit), maxSplit)*100) / 100
}

// clampRows keeps the bottom panel's share in range, 0 for the default.
func clampRows(f float64) float64 {
	if f == 0 {
		return 0
	}
	return math.Round(min(max(f, minRows), maxRows)*100) / 100
}

// place is where the details panel shows: right, bottom, or off when
// it is turned off. Turned on where the layout says off, it is right.
func (l Layout) place() string {
	switch {
	case !l.Details:
		return placeOff
	case l.DetailsPlace == placeBottom:
		return placeBottom
	}
	return placeRight
}

// rows is the bottom panel's share of the body's rows, its divider in.
func (l Layout) rows() float64 { return cmp.Or(l.DetailsRows, defaultRows) }

// mode is the sidebar's mode: hidden as SideMode says, else full or
// slim as Sidebar.Slim does.
func (l Layout) mode() string {
	switch {
	case l.SideMode == modeHidden && l.Sidebar.Slim:
		return modeHidden
	case l.Sidebar.Slim:
		return modeSlim
	}
	return modeFull
}

// Resolving the layout (the layout v2 design §2.5). Each part of the
// console's layout comes from, highest first: the console's own change
// under the layout in use (ui.json layouts.<id>), the layout mod, the
// console's own value (ui.json's top level), the built-in one.

// effectiveLayout is ui (ui.json) under the layout mod spec, whose id
// (the [ui] layout setting) names its tweaks.
func effectiveLayout(spec mod.Layout, ui Layout, id string) Layout {
	l := ui
	l.Layouts = nil
	t := ui.Layouts[id]
	ts := cmp.Or(t.Sidebar, &SidebarTweak{})

	// Level 3: the console's own; the top level says off with details.
	place := ui.place()
	if spec.Details != "" {
		place, l.set.place = spec.Details, true
	}
	if t.DetailsPlace != "" {
		place, l.set.place = t.DetailsPlace, true
	}
	l.Details = place != placeOff
	l.DetailsPlace = place
	if place == placeOff {
		// | turns it on where the layout puts it, or the console did,
		// else at the right.
		l.DetailsPlace = cmp.Or(bottom(spec.Details), bottom(ui.DetailsPlace), placeRight)
	}

	// The panel's size: the list's share (right), the panel's (bottom).
	if s := spec.DetSize; !s.IsZero() {
		if spec.Details == placeBottom {
			l.set.rows = true
			l.DetailsRows, l.rowCells = clampRows(s.Share), s.Cells
		} else {
			l.set.split = true
			if s.Share != 0 {
				l.Split = clampSplit(1 - s.Share)
			}
			l.detCells = s.Cells
		}
	}
	if t.Split != 0 {
		l.Split, l.detCells, l.set.split = t.Split, 0, true
	}
	if t.DetailsRows != 0 {
		l.DetailsRows, l.rowCells, l.set.rows = t.DetailsRows, 0, true
	}

	if w := spec.Width; !w.IsZero() {
		l.set.width = true
		if w.Cells != 0 {
			l.Sidebar.Width = w.Cells
		}
		l.sideShare = w.Share
	}
	if ts.Width != 0 {
		l.Sidebar.Width, l.sideShare, l.set.width = ts.Width, 0, true
	}
	l.Sidebar = l.Sidebar.Clamp()

	mode := ui.SideMode
	if mode == "" {
		mode = ui.mode()
	}
	if spec.Mode != "" {
		mode, l.set.mode = spec.Mode, true
	}
	if ts.Mode != "" {
		mode, l.set.mode = ts.Mode, true
	}
	// Until the view has a hidden sidebar, hidden shows the slim strip:
	// it never disappears.
	l.Sidebar.Slim, l.SideMode = mode != modeFull, mode

	// What only a layout mod sets.
	l.side = cmp.Or(spec.Side, "left")
	l.header = cmp.Or(spec.Header, "shown")
	l.footer = cmp.Or(spec.Footer, "shown")
	l.statusBar = cmp.Or(spec.StatusBar, "shown")
	l.density = cmp.Or(spec.Density, "normal")
	l.sections = layoutSections(spec.Sections)
	return l
}

// bottom is place when it is bottom, else "": ui.json writes no other.
func bottom(place string) string {
	if place == placeBottom {
		return place
	}
	return ""
}

// layoutSections is a layout's dashboard.sections as the dashboard
// shows them: all of them for none; needs and project always, needs
// first and project after it when left out.
func layoutSections(s []string) []string {
	if s == nil {
		return slices.Clone(mod.LayoutSections)
	}
	out := slices.Clone(s)
	if !slices.Contains(out, "needs") {
		out = append([]string{"needs"}, out...)
	}
	if !slices.Contains(out, "project") {
		i := slices.Index(out, "needs") + 1
		out = slices.Insert(out, i, "project")
	}
	return out
}

// tweak is ui with the console's change from was to now (both
// effective under spec): each part that changed goes under id when the
// layout sets it, else at the top level. In safe mode (safe) nothing
// goes under an id. The info panel and the tree are always the top
// level's.
func tweak(ui Layout, was, now Layout, id string, safe bool) Layout {
	ui.Info, ui.Tree = now.Info, now.Tree
	t, under := ui.Layouts[id], false
	ts := cmp.Or(t.Sidebar, &SidebarTweak{})
	ts = &SidebarTweak{Width: ts.Width, Mode: ts.Mode}
	at := func(set bool) bool {
		if set && !safe && id != "" {
			under = true
			return true
		}
		return false
	}
	if p := now.place(); p != was.place() {
		if at(was.set.place) {
			t.DetailsPlace = p
		} else if ui.Details = p != placeOff; ui.Details {
			ui.DetailsPlace = bottom(p) // off keeps where | turns it on
		}
	}
	if now.Split != was.Split || now.detCells != was.detCells {
		if at(was.set.split) {
			t.Split = now.Split
		} else {
			ui.Split = now.Split
		}
	}
	if now.DetailsRows != was.DetailsRows || now.rowCells != was.rowCells {
		if at(was.set.rows) {
			t.DetailsRows = now.DetailsRows
		} else {
			ui.DetailsRows = now.DetailsRows
		}
	}
	if now.Sidebar.Width != was.Sidebar.Width {
		if at(was.set.width) {
			ts.Width = now.Sidebar.Width
		} else {
			ui.Sidebar.Width = now.Sidebar.Width
		}
	}
	if m := now.mode(); m != was.mode() {
		if at(was.set.mode) {
			ts.Mode = m
		} else {
			ui.Sidebar.Slim = m != modeFull
			ui.SideMode = map[string]string{modeHidden: modeHidden}[m]
		}
	}
	if !under {
		return ui
	}
	if *ts != (SidebarTweak{}) {
		t.Sidebar = ts
	}
	ui.Layouts = maps.Clone(ui.Layouts)
	if ui.Layouts == nil {
		ui.Layouts = map[string]LayoutTweak{}
	}
	for _, o := range ui.Layouts {
		t.Seq = max(t.Seq, o.Seq+1)
	}
	ui.Layouts[id] = t
	for len(ui.Layouts) > maxTweaks {
		oldest := id
		for k, o := range ui.Layouts {
			if o.Seq < ui.Layouts[oldest].Seq {
				oldest = k
			}
		}
		delete(ui.Layouts, oldest)
	}
	return ui
}

// panel is where the dashboard shows the details panel: right,
// bottom, or "" for none (dashGeo.panel).
func (m *dash) panel() string { return m.geom().panel() }

// setLayout changes the layout and saves it.
func (m *dash) setLayout(l Layout) {
	l.Split = clampSplit(l.Split)
	l.DetailsRows = clampRows(l.DetailsRows)
	l.Sidebar = l.Sidebar.Clamp()
	if !sameLayout(l, m.layout) {
		side := l.Sidebar != m.layout.Sidebar
		m.layout = l
		if side {
			m.setWidth(m.winW)
		}
		m.saveLayout()
	}
}

// sameLayout says whether a and b lay the screen out alike: the tree's
// folds aside (refold saves them).
func sameLayout(a, b Layout) bool {
	return a.Details == b.Details && a.DetailsPlace == b.DetailsPlace && a.Split == b.Split && a.detCells == b.detCells &&
		a.DetailsRows == b.DetailsRows && a.rowCells == b.rowCells && a.Sidebar == b.Sidebar && a.SideMode == b.SideMode && a.Info == b.Info
}

// saveLayout writes ui.json: what changed since it was read, under the
// layout in use where it sets that part (tweak). A failed save only
// costs the preference, so it shows in the footer and nothing else.
func (m *dash) saveLayout() {
	was := effectiveLayout(m.spec, m.ui, m.layoutID).sized(m.winW)
	m.ui = tweak(m.ui, was, m.layout, m.layoutID, m.safe)
	// Resolved again, the parts changed are the tweaks'; the view's
	// sidebar is as shown.
	l := effectiveLayout(m.spec, m.ui, m.layoutID)
	l.Sidebar, l.SideMode, l.Info, l.Tree = m.layout.Sidebar, m.layout.SideMode, m.layout.Info, m.layout.Tree
	m.layout = l
	if m.uiFile == "" {
		return
	}
	if err := SaveLayout(m.uiFile, m.ui); err != nil {
		m.fail(err)
	}
}

// sized is l with a sidebar width given as a share of the window made
// cells, in a window w wide.
func (l Layout) sized(w int) Layout {
	if l.sideShare != 0 {
		l.Sidebar.Width = max(int(l.sideShare*float64(w)+0.5), view.SideMin)
	}
	return l
}

// useLayout makes the layout mod spec, the [ui] layout id, this
// console's. whole (a layout picked, safe mode) takes every part of the
// sidebar from it and ui.json; else the view's sidebar keeps what the
// layout doesn't set, as another console left it.
func (m *dash) useLayout(id string, spec mod.Layout, whole bool) {
	m.spec, m.layoutID = spec, id
	l := effectiveLayout(spec, m.ui, id).sized(m.winW)
	side, mode := l.Sidebar, l.SideMode
	if !whole {
		side, mode = m.layout.Sidebar, m.layout.SideMode
		if l.set.width {
			side.Width = l.Sidebar.Width
		}
		if l.set.mode {
			side.Slim, mode = l.Sidebar.Slim, l.SideMode
		}
	}
	l.Sidebar, l.SideMode, l.Info, l.Tree = side, mode, m.layout.Info, m.layout.Tree
	m.layout = l
	m.setWidth(m.winW)
}

// SaveSidebar writes the sidebar's layout to path and keeps the rest of
// the file: the attach client changes only the sidebar. A part the
// console's layout sets goes under its id (tweak).
func SaveSidebar(path string, s SidebarLayout) error {
	ui := LoadLayout(path)
	spec, id := currentLayout()
	was := effectiveLayout(spec, ui, id)
	now := was
	now.Sidebar = s.Clamp()
	return SaveLayout(path, tweak(ui, was, now, id, safeMode()))
}

// ConsoleLayout is ui.json at path under this console's layout ([ui]
// layout), in a window cols wide: the sidebar a new view starts with.
func ConsoleLayout(path string, cols int) Layout {
	spec, id := currentLayout()
	return effectiveLayout(spec, LoadLayout(path), id).sized(cols)
}

// SaveTree writes what the sidebar's tree folds to path and keeps the
// rest of the file, as SaveSidebar.
func SaveTree(path string, t view.Tree) error {
	l := LoadLayout(path)
	l.Tree = t
	return SaveLayout(path, l)
}

// SaveInfo writes the info panel's layout to path and keeps the rest of
// the file, as SaveSidebar.
func SaveInfo(path string, p view.Info) error {
	l := LoadLayout(path)
	l.Info = p.Clamp()
	return SaveLayout(path, l)
}
