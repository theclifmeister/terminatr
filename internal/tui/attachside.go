package tui

import (
	"errors"
	"fmt"
	"slices"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The sidebar in the attach view: the client's tree, keys, mouse and
// actions on it (sidebar.go and sidetree.go draw it).

// sideCurrent is the attach view's current project: the focused pane's,
// else the view's. c.mu held.
func (c *client) sideCurrent() string {
	if c.focus != nil && c.focus.info.Project != "" {
		return c.focus.info.Project
	}
	return c.v.Current
}

// sideTree is the attach view's tree. c.mu held.
func (c *client) sideTree() []treeRow {
	focus := c.v.Focus
	if c.focus != nil {
		focus = c.focus.info.ID
	}
	rows := buildTree(c.side.projects, c.sideSessions(), treeIn{current: c.sideCurrent(), focus: focus, ctxHint: c.side.ctxHint,
		tree: sideTree(c.side.projects, c.v.Tree), cols: c.v.Columns, cells: c.cells, peers: c.side.peers})
	rows = sideRows(rows, c.side.needsFirst)
	if c.bandShows() {
		rows = withSections(rows, c.band())
		markUndrawn(rows, c.sideW, c.sideRows(), c.sideFoot(), c.v.SideSel, c.kb == areaSide)
	}
	if c.kb == areaSide {
		markCursor(rows, c.sideW, c.v.SideSel)
	}
	return rows
}

// sideSessions are the sessions the sidebar's tree is built from: the
// last poll's, and the panes' this client shows that it doesn't list
// yet. Until the first poll it lists none, and a thread's row whose
// session shows would otherwise have none (enter refused on it). c.mu
// held.
func (c *client) sideSessions() []proto.SessionInfo {
	out := c.side.sessions
	for _, p := range c.panes {
		if p.info.ID != "" && !slices.ContainsFunc(out, func(s proto.SessionInfo) bool { return s.ID == p.info.ID }) {
			out = append(slices.Clip(out), p.info)
		}
	}
	return out
}

// bandShows says whether the attached sidebar shows its band of mod
// sections: some mod has one, and the full sidebar has the room. c.mu
// held.
func (c *client) bandShows() bool {
	return c.side != nil && len(c.side.sections) > 0 && c.sideW > 0 && bandFits(c.sideW, c.sideRows(), c.sideFoot())
}

// band is what the attached sidebar's band is built from: the views
// mod.views last listed that session.list names as sidebar sections
// (in safe mode only the built-in mods'), in their order; one it names
// that mod.views didn't list yet waits. c.mu held.
func (c *client) band() bandIn {
	secs := c.side.sections
	var list []proto.ModView
	mods := make([]mod.Mod, 0, len(secs))
	for _, s := range secs {
		mods = append(mods, mod.Mod{ID: s.Mod, Builtin: s.Builtin})
		if i := slices.IndexFunc(c.views, func(v proto.ModView) bool { return v.Mod == s.Mod && v.View == s.View }); i >= 0 {
			list = append(list, c.views[i])
		} else {
			list = append(list, proto.ModView{Mod: s.Mod, View: s.View, Title: s.Title, Slot: s.Slot})
		}
	}
	return bandIn{list: list, project: c.sideCurrent(), folds: c.v.Tree, w: c.sideW, mods: mods, cache: &c.side.band, order: c.side.order,
		projOrder: c.side.projOrder}
}

// sideFullTree is the attach view's whole tree, nothing folded and no
// filter (w W). c.mu held.
func (c *client) sideFullTree() []treeRow {
	rows := buildTree(c.side.projects, c.sideSessions(), treeIn{current: c.sideCurrent(), ctxHint: c.side.ctxHint, tree: unfoldedTree(c.side.projects), cols: c.v.Columns, cells: c.cells, peers: c.side.peers})
	if c.side.needsFirst {
		rows = needsFirstOrder(rows)
	}
	return rows
}

// areas are the session's areas that take the keyboard, in prefix+tab's
// order: the pane, the sidebar, the info panel while it shows. c.mu held.
func (c *client) areas() []area {
	out := []area{areaMain}
	if c.side != nil && c.sideW > 0 {
		out = append(out, areaSide)
	}
	if c.infoW > 0 {
		out = append(out, areaInfo)
	}
	return out
}

// nextArea moves the keyboard to the next area (prefix+tab, and tab in
// the sidebar or the info panel): pane, sidebar, info panel, pane. The
// sidebar's cursor starts on the row you are on.
func (c *client) nextArea() {
	if !c.lock() {
		return
	}
	here := ""
	areas := c.areas()
	switch next := cycle(areas, c.kb, false); {
	case len(areas) == 1:
		c.flash = "no sidebar here"
	case next == areaSide:
		here = hereKey(c.sideTree())
		c.kb, c.v.SideSel = areaSide, here
	default:
		c.kb = next
	}
	c.status()
	c.mu.Unlock()
	c.poke()
	if here != "" && c.vc != nil {
		c.act(proto.MethodViewSideSel, proto.ViewParams{Key: here})
	}
}

// sideKeyboard runs a key while the sidebar has the keyboard: a sidebar
// key (sideActions) moves its cursor, and the view previews the row it
// rests on (previewSoon), or goes into the pane; tab moves on to the
// next area, as prefix+tab; ? is the help, as prefix+?; any other key is
// dropped, so nothing reaches the pane. c.mu held; released here.
func (c *client) sideKeyboard(k uv.Key) {
	name := keyName(k)
	in := sideKeysIn{tree: c.sideTree, full: c.sideFullTree, w: c.sideW, h: c.sideRows(), sel: c.v.SideSel}
	if name == "tab" {
		c.flash = ""
		c.mu.Unlock()
		c.nextArea()
		return
	}
	var st sideStep
	ok := false
	if c.side != nil {
		st, ok = sideKeyFor(in, name)
	}
	if !ok {
		c.flash = ""
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	if st.help {
		// A plain key that is a prefix command does what it does.
		c.flash = st.msg
		do := prefixStep(c.prefix, true, k, c.dashboard)
		c.status()
		c.mu.Unlock()
		c.run(do)
		return
	}
	c.sideDo(st)
}

// sideDo does what a sidebar key decided (sideKeyFor). c.mu held;
// released here.
func (c *client) sideDo(st sideStep) {
	c.flash = st.msg
	if st.back || st.into {
		// Into the pane: the session's, or the dashboard's list.
		c.kb = areaMain
	}
	if st.sel != "" {
		c.v.SideSel = st.sel // at once here; the view follows
		c.previewSoon(st.sel)
	}
	c.status()
	c.mu.Unlock()
	c.poke()
	if st.sel != "" {
		c.act(proto.MethodViewSideSel, proto.ViewParams{Key: st.sel})
	}
	if st.target != nil && !st.here {
		c.sideGo(*st.target)
	}
	if st.toggle != "" {
		c.askActive(st.toggle, st.activate)
	}
	if st.refold != nil {
		c.refold(st.refold)
	}
	c.sectionDo(st)
	if st.pin != "" {
		c.setPinned(st.pin, st.pinOn)
	}
	c.poke()
}

// previewSoon shows the row sel in the view once the sidebar's cursor
// has rested on it for previewDelay, the keyboard still in the sidebar
// (preview). A bare view has nothing to preview in: it would hand this
// console over to view main. c.mu held.
func (c *client) previewSoon(sel string) {
	if c.bare {
		return
	}
	c.previewSeq++
	seq := c.previewSeq
	c.previewing = true
	time.AfterFunc(previewDelay, func() { c.previewNow(seq, sel) })
}

// previewNow shows the row sel, unless the cursor moved on or left the
// sidebar meanwhile.
func (c *client) previewNow(seq uint64, sel string) {
	if !c.lock() {
		return
	}
	if seq != c.previewSeq {
		c.mu.Unlock()
		return
	}
	c.previewing = false
	var t Target
	ok := false
	if c.kb == areaSide && c.side != nil && c.v.SideSel == sel {
		if r, found := sideRowOf(shownRows(c.sideTree(), c.sideW), sel, false); found {
			t, ok = r.preview()
		}
	}
	c.status()
	c.mu.Unlock()
	c.poke()
	if ok {
		c.sideGo(t)
	}
}

// refold changes what the tree folds: here at once, in the view, and
// in ui.json, as the tree new views start with.
func (c *client) refold(fn func(*view.Tree)) {
	if !c.lock() {
		return
	}
	t := c.v.Tree
	fn(&t)
	c.v.Tree = t
	path := ""
	if c.side != nil {
		path = c.side.uiFile
	}
	c.mu.Unlock()
	c.poke()
	c.act(proto.MethodViewTree, proto.ViewParams{Tree: &t})
	c.saveTree(path, t)
}

// foldSections folds (on) or unfolds the sidebar sections ids: here at
// once, in the view (view.sections, every console of it) and in ui.json,
// with the tree new views start with. On a server of protocol 14 the
// folds stay this console's (ViewConn.Sections).
func (c *client) foldSections(ids []string, on bool) {
	if !c.lock() {
		return
	}
	t := c.v.Tree
	if !t.FoldSections(ids, on) {
		c.mu.Unlock()
		return
	}
	c.v.Tree = t
	path := ""
	if c.side != nil {
		path = c.side.uiFile
	}
	c.mu.Unlock()
	c.poke()
	if v, err := c.vc.Sections(ids, on); err != nil {
		c.failed(err)
	} else {
		c.apply(v)
	}
	c.saveTree(path, t)
}

// saveTree keeps t in ui.json at path, if any, as the tree new views
// start with.
func (c *client) saveTree(path string, t view.Tree) {
	if path == "" {
		return
	}
	if err := SaveTree(path, t); err != nil && c.lock() {
		c.flash = "ui.json: " + err.Error()
		c.status()
		c.mu.Unlock()
		c.poke()
	}
}

// setPinned pins a project to the top of the sidebar, or unpins it
// (its setting), and says what came of it in the status bar.
func (c *client) setPinned(slug string, on bool) {
	go func() {
		msg, err := PinProject(caller.FromEnv(), slug, on)
		if err != nil {
			msg = errMessage(err)
		}
		if !c.lock() {
			return
		}
		if err == nil && c.side != nil {
			pinIn(c.side.projects, slug, on)
		}
		c.flash = msg
		c.status()
		c.mu.Unlock()
		c.poke()
	}()
}

// askActive activates or deactivates a project from the sidebar
// (space); deactivating asks first, in a dialog, while its coordinator
// or threads run.
func (c *client) askActive(slug string, on bool) {
	if !c.lock() {
		return
	}
	busy := runningIn(c.side.sessions, slug)
	if on || len(busy) == 0 {
		c.mu.Unlock()
		c.setActive(slug, on)
		return
	}
	c.confirm = func(yes bool) {
		c.confirm = nil
		c.closeMenu()
		if !yes {
			c.flash = slug + " stays active"
		}
		c.status()
		c.mu.Unlock()
		c.poke()
		if yes {
			c.setActive(slug, false)
		}
	}
	c.openDialog("Deactivate "+slug, DeactivateQuestion(slug, busy))
	c.status()
	c.mu.Unlock()
	c.poke()
}

// setActive activates or deactivates a project through the server, and
// says what came of it in the status bar.
func (c *client) setActive(slug string, on bool) {
	go func() {
		ctl, err := server.Connect(c.paths, false)
		msg := ""
		if err == nil {
			msg, err = ActivateProject(ctl.Call, caller.FromEnv(), slug, on)
			ctl.Close()
		}
		if err != nil {
			msg = errMessage(err)
		}
		if c.lock() {
			c.flash = msg
			c.status()
			c.mu.Unlock()
			c.poke()
		}
	}()
}

// appendSidebar draws the sidebar's changed lines. c.mu held.
func (c *client) appendSidebar(b []byte, wrote bool) ([]byte, bool) {
	rows := c.sideTree()
	head := sideHeadIn{needs: needsYouCount(c.side.projects, c.side.sessions), cursor: c.kb == areaSide && c.v.SideSel == headKey}
	lines := sideLines(rows, c.sideW, c.sideRows(), head, sideFooter(rows, c.sideW, head, c.side.server, true, c.side.compact), c.v.Sidebar.Right())
	if c.sideW == 0 {
		lines = nil // hidden
	}
	x := c.sideX()
	for y, l := range lines {
		if y < len(c.side.drawn) && c.side.drawn[y] == l {
			continue
		}
		b = append(b, fmt.Sprintf("\x1b[%d;%dH\x1b[0m", y+1, x+1)...)
		b = append(b, l...)
		wrote = true
	}
	c.side.drawn = lines
	return b, wrote
}

// sideFoot is the rows the sidebar's footer takes (sideFootRows). c.mu
// held.
func (c *client) sideFoot() int {
	if c.side == nil {
		return 0
	}
	return sideFootRows(c.sideW, c.sideRows(), c.side.server, c.side.compact)
}

// sectionDo runs what a key or a click on a band section does: folds or
// unfolds sections in the view (foldSections), or sends the mod the
// ui.click that opens its view, which then shows over the session as a
// command's does (cmdWait, takeOpen).
func (c *client) sectionDo(st sideStep) {
	ids := st.sections
	if st.everySection && c.lock() {
		ids = st.sectionIDs(c.v.Tree)
		c.mu.Unlock()
	}
	if len(ids) > 0 {
		c.foldSections(ids, st.foldOn)
	}
	if s := st.open; s != nil {
		p := sectionClick(s)
		go func() {
			var res proto.ModViewInputResult
			ctl, err := server.Connect(c.paths, false)
			if err == nil {
				err = ctl.Call(proto.MethodModViewInput, p, &res)
				ctl.Close()
			}
			if !c.lock() {
				return
			}
			if err != nil {
				c.flash = errMessage(err)
				c.status()
			} else {
				c.wait = &cmdWait{mod: p.Mod, after: res.After, until: time.Now().Add(modview.InputWindow + 2*statePoll)}
			}
			c.mu.Unlock()
			c.poke()
		}()
	}
}

// sideRows is the sidebar's height in a session: the window less the
// status bar and the rule above it, which run under it too. c.mu held.
func (c *client) sideRows() int {
	return max(c.rows-c.barRows(), 1)
}

// viewCols is the width the view is laid out at. c.mu held.
func (c *client) viewCols() int {
	if c.v.Cols > 0 {
		return int(c.v.Cols)
	}
	return c.cols
}

// setSidebar changes the view's sidebar: a layout change, so every
// console's panes follow (docs/SPEC.md §3.3). save also keeps it in
// ui.json, as the default of new views.
func (c *client) setSidebar(l SidebarLayout, save bool) {
	c.act(proto.MethodViewSidebar, proto.ViewParams{Sidebar: &l})
	if !save || !c.lock() {
		return
	}
	path := c.side.uiFile
	c.mu.Unlock()
	if path == "" {
		return
	}
	if err := SaveSidebar(path, l); err != nil && c.lock() {
		c.flash = "ui.json: " + err.Error()
		c.status()
		c.mu.Unlock()
		c.poke()
	}
}

// sideKey runs a sidebar key ({, } or b) after the prefix.
func (c *client) sideKey(key string) {
	if !c.lock() {
		return
	}
	if c.side == nil {
		c.mu.Unlock()
		return
	}
	l, msg := c.v.Sidebar.Key(key, c.viewCols())
	same := l == c.v.Sidebar
	if msg != "" {
		c.flash = msg
		c.status()
	}
	c.mu.Unlock()
	if !same {
		c.setSidebar(l, true)
	}
	c.poke()
}

// sideMouse handles the mouse over the sidebar, or dragging its border:
// a click on a row gives the sidebar the keyboard and goes where the row
// points (sideGo). c.mu held; released here.
func (c *client) sideMouse(m emu.Mouse) {
	sd := c.side
	switch {
	case sd.drag && m.Action == emu.MouseMotion:
		// The border's column in the view's window, which may be
		// another size than this one: on the right, from its right edge.
		x := m.X
		if c.v.Sidebar.Right() {
			x = c.viewCols() - (c.cols - m.X)
		}
		l := c.v.Sidebar.DragTo(x, c.viewCols())
		same := l == c.v.Sidebar
		c.mu.Unlock()
		if !same {
			c.setSidebar(l, false)
		}
		c.poke()
		return
	case !sd.drag && m.Action == emu.MouseMotion && m.Button == emu.MouseNone:
		changed := setSideHover(sideTip(c.sideTree(), c.sideW, c.sideRows(), c.sideFoot(), c.sideCol(m.X), m.Y))
		c.mu.Unlock()
		if changed {
			c.poke()
		}
		return
	case sd.drag && m.Action == emu.MouseRelease:
		sd.drag = false
		l := c.v.Sidebar
		c.mu.Unlock()
		c.setSidebar(l, true)
		return
	case m.Action != emu.MousePress || m.Button != emu.MouseLeft:
		c.mu.Unlock()
		return
	}
	if sideFlagAt(c.sideTree(), c.sideW, c.sideRows(), c.sideFoot(), needsYouCount(c.side.projects, c.side.sessions), c.sideCol(m.X), m.Y) {
		// The header's ⚑ N: the first row that needs you, as w.
		c.kb = areaSide
		in := sideKeysIn{tree: c.sideTree, full: c.sideFullTree, w: c.sideW, h: c.sideRows(), sel: c.v.SideSel}
		c.sideDo(sideNeedStep(in, treeRow{}, false))
		return
	}
	if sideHeadAt(c.sideTree(), c.sideW, c.sideRows(), c.sideFoot(), c.sideCol(m.X), m.Y) {
		// The PROJECTS header: every project's NEEDS YOU, on the
		// dashboard; the sidebar keeps the keyboard on it.
		c.kb, c.v.SideSel = areaSide, headKey
		c.flash = ""
		c.status()
		c.mu.Unlock()
		if c.vc != nil {
			c.act(proto.MethodViewSideSel, proto.ViewParams{Key: headKey})
		}
		c.sideGo(Target{Project: allProjects})
		c.poke()
		return
	}
	r, ok, border := sideHitAt(c.sideTree(), c.sideW, c.sideRows(), c.sideFoot(), c.sideCol(m.X), m.Y)
	t, can, why := r.target()
	sel := ""
	if ok && !border {
		// A click on a row gives the sidebar the keyboard, its cursor on
		// the row; it keeps it while the row's session shows.
		sel = r.key()
		c.kb, c.v.SideSel = areaSide, sel
		c.flash = ""
		c.status()
	}
	open, fold := false, false
	var sec sideStep
	var peer func(*view.Tree) // a paired server's header folds
	switch {
	case border:
		sd.drag = true
	case !ok:
	case r.kind == treeSection:
		// Its header or its body: as enter on the header.
		sec, _ = sectionStep(r, sideEnter)
	case r.kind == treeDivider:
		fold = true
	case r.kind == treeServer && !r.link.local:
		peer = foldPeer(r, !r.fold)
	case !can:
		c.flash = why
		c.status()
	case r.here && r.kind != treeProject:
		c.flash = "you are on it"
		c.status()
	default:
		open = true
	}
	c.mu.Unlock()
	if sel != "" && c.vc != nil {
		c.act(proto.MethodViewSideSel, proto.ViewParams{Key: sel})
	}
	if open {
		c.sideGo(t)
	}
	if fold {
		c.refold(foldDivider(r))
	}
	if peer != nil {
		c.refold(peer)
	}
	c.sectionDo(sec)
	c.poke()
}

// sideGo opens a clicked sidebar row. A full view opens it in place, on
// every console of the view; a bare one (tm attach, tm project open) has
// no dashboard, so this console leaves it and hands over to a full tm
// joined to view main, which opens it (Result.GoTo).
func (c *client) sideGo(t Target) {
	if c.bare {
		c.goTo = &t
		c.detachThen("")
		return
	}
	go func() {
		if err := OpenTarget(c.paths, c.vc, t); err != nil {
			var perr *proto.Error
			msg := err.Error()
			if errors.As(err, &perr) {
				msg = perr.Message
			}
			if c.lock() {
				c.flash = msg
				c.status()
				c.mu.Unlock()
				c.poke()
			}
			return
		}
		c.apply(c.vc.View())
	}()
}
