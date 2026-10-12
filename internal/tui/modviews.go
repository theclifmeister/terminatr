package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Views user mods draw (mods design §6; docs/SPEC.md §4, Mod views): a
// mod's project.tab is a tab of the project popup after the core tabs,
// and its dashboard.card a card below the dashboard's sections, which
// enter opens as a popup to move, filter and press the mod's keys in.
// The mod publishes a widget tree; terminatr draws it (widget.View, in
// this console's look) in its own frame, and keeps
// the cursor, filter and scroll. Only a declared key and a click on a
// row or a key hint reach the mod, as ui.key and ui.click, and where the
// cursor went, as ui.cursor (one at a time per view). The console
// polls the trees (mod.views) and says there which views it shows, so
// the mods get view.opened and view.closed. A mod whose host failed
// shows that, with its log, never its stale tree.

// modViews is the console's side of the mods' views.
type modViews struct {
	list []proto.ModView // as the last poll had them
	// hosted are the views on screen, by panel key: the tree shown and
	// the core's state in it.
	hosted map[string]*widget.View
	trees  map[string]*widget.Widget // the tree each hosted view was given
	// folded are the dashboard cards folded to their header.
	folded map[string]bool
	// seen is the last open request acted on; sent the shown views the
	// server was last told, busy while a poll is on its way.
	seen uint64
	sent string
	busy bool
	// cursors are the ui.cursor sends by panel key: one on its way at
	// a time, and the latest move waiting behind it.
	cursors map[string]*cursorSend
}

// cursorSend is a view's ui.cursor sends: busy while one is on its way,
// next the move to send when it is back.
type cursorSend struct {
	busy bool
	next *proto.ModViewInputParams
}

// modViewsMsg is a poll's answer.
type modViewsMsg struct {
	res   proto.ModViewsResult
	shown string
	err   error
}

// modInputMsg is what came of sending the mod a key or a click.
type modInputMsg struct{ err error }

// modCursorMsg is a ui.cursor send back, for the panel key's next.
type modCursorMsg struct{ key string }

// cardKey is a dashboard card's row key; panelKey a hosted view's key.
func cardKey(v proto.ModView) string { return "mc:" + v.Mod + "/" + v.View }
func panelKey(v proto.ModView, project string) string {
	return v.Mod + "/" + v.View + "/" + v.Slot + "/" + project
}

// runsFor says whether view v's mod runs for project slug.
func runsFor(v proto.ModView, slug string) bool {
	return v.Projects == nil || slices.Contains(v.Projects, slug)
}

// coreTabMods are the built-in mods whose project tab the popup draws
// as a core tab, rather than after the core tabs: the Memory tab mod's
// (mods design §11, proof 2) and the Tasks tab mod's (§12 Phase 2).
// Neither tab has a core drawing of its own: while its mod doesn't run
// it says so (modOffLines). Only a bundled mod has its id: an installed
// folder of that name is shadowed.
var coreTabMods = []struct {
	tab       int
	mod, view string
}{
	{tabMemory, mod.BundledMemory, mod.MemoryView},
	{tabTasks, mod.BundledTasks, mod.TasksView},
}

// coreTabOf is the core tab mod view v draws in, ok false for a view
// that is a tab of its own.
func coreTabOf(v proto.ModView) (int, bool) {
	if v.Slot != mod.SlotProjectTab {
		return 0, false
	}
	for _, c := range coreTabMods {
		if v.Mod == c.mod && v.View == c.view {
			return c.tab, true
		}
	}
	return 0, false
}

// coreTab is the built-in mod's tab that draws core tab tab for
// project slug, while the mod runs for it.
func (mv *modViews) coreTab(slug string, tab int) (proto.ModView, bool) {
	for _, v := range mv.list {
		if t, ok := coreTabOf(v); ok && t == tab && runsFor(v, slug) {
			return v, true
		}
	}
	return proto.ModView{}, false
}

// modOffLines are a core tab whose built-in mod (coreTabMods) doesn't
// run for pv's project: which mod draws it, that it is off and how to
// turn it on; enabled and not drawing yet, that it is on its way.
func (pv *projectView) modOffLines(m *dash, w int) []string {
	id := ""
	for _, c := range coreTabMods {
		if c.tab == pv.tab {
			id = c.mod
		}
	}
	var md mod.Mod
	if i := slices.IndexFunc(m.data.Mods, func(md mod.Mod) bool { return md.ID == id }); i >= 0 {
		md = m.data.Mods[i]
	}
	switch {
	case md.State == mod.Enabled && md.Runs(pv.slug):
		return faintLines(fmt.Sprintf("Waiting for the %s mod to draw this tab…", id), w)
	case md.State == mod.Enabled:
		return append(faintLines(fmt.Sprintf("The %s mod draws this tab, and it is off for %s.", id, pv.slug), w),
			faintLines(fmt.Sprintf("Turn it on for %s in the Mods tab (, then 3), or with tm mod enable %s --project %s.", pv.slug, id, pv.slug), w)...)
	}
	return append(faintLines(fmt.Sprintf("The %s mod draws this tab, and it is off.", id), w),
		faintLines(fmt.Sprintf("Turn it on in the Mods tab (, then 3), or with tm mod enable %s.", id), w)...)
}

// tabsFor are the mods' project tabs for project slug, in order, after
// the core tabs (the built-in ones that draw in a core tab are not).
func (mv *modViews) tabsFor(slug string) []proto.ModView {
	var out []proto.ModView
	for _, v := range mv.list {
		if _, core := coreTabOf(v); v.Slot == mod.SlotProjectTab && runsFor(v, slug) && !core {
			out = append(out, v)
		}
	}
	return out
}

// cards are the mods' dashboard cards, in order.
func (mv *modViews) cards() []proto.ModView {
	var out []proto.ModView
	for _, v := range mv.list {
		if v.Slot == mod.SlotDashboardCard {
			out = append(out, v)
		}
	}
	return out
}

// card is the card of row key key.
func (mv *modViews) card(key string) (proto.ModView, bool) {
	for _, v := range mv.cards() {
		if cardKey(v) == key {
			return v, true
		}
	}
	return proto.ModView{}, false
}

// tree is the tree v shows for project: the project's own, else the one
// for all projects; nil while the mod drew none.
func tree(v proto.ModView, project string) *modview.Tree {
	var all *modview.Tree
	for i := range v.Trees {
		t := &v.Trees[i]
		switch t.Project {
		case project:
			if project != "" {
				return t
			}
			all = t
		case "":
			all = t
		}
	}
	return all
}

// host is the widget.View for v shown for project, given its latest
// tree; nil while there is none (or the mod failed).
func (mv *modViews) host(v proto.ModView, project string) *widget.View {
	t := tree(v, project)
	if v.State == proto.ModViewFailed || t == nil || t.Tree == nil {
		return nil
	}
	if mv.hosted == nil {
		mv.hosted, mv.trees = map[string]*widget.View{}, map[string]*widget.Widget{}
	}
	k := panelKey(v, project)
	h := mv.hosted[k]
	if h == nil {
		h = widget.NewView()
		h.SetBindKeys(BindKeys) // this console's keys for bound actions
		mv.hosted[k] = h
	}
	if mv.trees[k] != t.Tree {
		mv.trees[k] = t.Tree
		h.Set(t.Tree)
	}
	return h
}

// panelLines draws v for project in w cells by h lines: the tree, or
// why there is none.
func (m *dash) panelLines(v proto.ModView, project string, w, h int) []string {
	if v.State == proto.ModViewFailed {
		return failedLines(v, w)
	}
	host := m.mv.host(v, project)
	if host == nil && m.safeOff(v.Mod) {
		// Its host doesn't run: it would wait for ever (T301 B8).
		return faintLines(fmt.Sprintf("%s is off in safe mode (tm --no-mods). tm server restart runs it again.", v.Name), w)
	}
	if host == nil {
		return faintLines(fmt.Sprintf("Waiting for %s to draw %s…", v.Name, v.Title), w)
	}
	return host.Render(w, h, widgetLook{})
}

// safeOff says whether mod id is a user mod the server doesn't run, in
// safe mode (a built-in one still runs).
func (m *dash) safeOff(id string) bool {
	if !m.data.NoMods {
		return false
	}
	i := slices.IndexFunc(m.data.Mods, func(md mod.Mod) bool { return md.ID == id })
	return i < 0 || !m.data.Mods[i].Builtin
}

// panelKeyPress gives key k to v's view for project: the core's keys
// move, scroll and filter in it; a key the mod declared is sent to it.
// It reports whether the view took the key.
func (m *dash) panelKeyPress(v proto.ModView, project, k string) (bool, tea.Cmd) {
	host := m.mv.host(v, project)
	if host == nil {
		return false, nil
	}
	used, ev := host.Key(k)
	if alt, ok := map[string]string{"j": "down", "k": "up"}[k]; !used && ok && !host.Editing() {
		// j and k move, as in the core's lists, unless the mod took them.
		used, ev = host.Key(alt)
	}
	if a := host.Unbound(k); !used && a != nil {
		return true, m.whyNot(*a)
	}
	return used, tea.Batch(m.sendInput(v, project, ev), m.sendCursor(v, project, host.Moved()))
}

// panelClick clicks cell x of line y of v's view.
func (m *dash) panelClick(v proto.ModView, project string, x, y int) tea.Cmd {
	if host := m.mv.host(v, project); host != nil {
		ev := host.Click(x, y)
		return tea.Batch(m.sendInput(v, project, ev), m.sendCursor(v, project, host.Moved()))
	}
	return nil
}

// panelWheel scrolls v's view by three lines a notch.
func (m *dash) panelWheel(v proto.ModView, project string, d int) {
	if host := m.mv.host(v, project); host != nil {
		host.Wheel(3 * d)
	}
}

// sendInput sends ev, if any, to v's mod. A bound action pressed never
// reaches the mod: the core runs it (pressBinding, mods design §11).
func (m *dash) sendInput(v proto.ModView, project string, ev *widget.Event) tea.Cmd {
	if ev == nil {
		return nil
	}
	if ev.Kind == widget.EventAction {
		return m.pressBinding(v.Mod, ev)
	}
	src := m.src
	p := proto.ModViewInputParams{Mod: v.Mod, View: v.View, Project: project, Kind: ev.Kind, Widget: ev.Widget, Row: ev.Row, Key: ev.Key}
	return func() tea.Msg { return modInputMsg{err: src.ModViewInput(p)} }
}

// sendCursor sends ev, a move of v's cursor (widget.View.Moved), if any,
// to v's mod as ui.cursor. One send per view is on its way at a time,
// so the mod hears the moves in order; moves made meanwhile come down to
// the latest. An error is dropped: the mod misses a move, the human
// nothing.
func (m *dash) sendCursor(v proto.ModView, project string, ev *widget.Event) tea.Cmd {
	if ev == nil {
		return nil
	}
	if m.mv.cursors == nil {
		m.mv.cursors = map[string]*cursorSend{}
	}
	k := panelKey(v, project)
	c := m.mv.cursors[k]
	if c == nil {
		c = &cursorSend{}
		m.mv.cursors[k] = c
	}
	p := proto.ModViewInputParams{Mod: v.Mod, View: v.View, Project: project, Kind: ev.Kind, Widget: ev.Widget, Row: ev.Row}
	if c.busy {
		c.next = &p
		return nil
	}
	c.busy = true
	return m.cursorCmd(k, p)
}

func (m *dash) cursorCmd(k string, p proto.ModViewInputParams) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		_ = src.ModViewInput(p)
		return modCursorMsg{key: k}
	}
}

// cursorSent sends the move that waited behind the send just back.
func (m *dash) cursorSent(msg modCursorMsg) tea.Cmd {
	c := m.mv.cursors[msg.key]
	if c == nil {
		return nil
	}
	if c.next == nil {
		delete(m.mv.cursors, msg.key)
		return nil
	}
	p := *c.next
	c.next = nil
	return m.cursorCmd(msg.key, p)
}

// tellCursors tells each mod view on show where its cursor is, when that
// changed since it was last told (a new tree moved it, or the view just
// showed), and forgets what was told to those no longer on show, whose
// mods heard view.closed.
func (m *dash) tellCursors() tea.Cmd {
	shown := map[string]bool{}
	var cmds []tea.Cmd
	for _, s := range m.shownViews() {
		i := slices.IndexFunc(m.mv.list, func(v proto.ModView) bool { return v.Mod == s.Mod && v.View == s.View })
		if i < 0 || mod.IsSection(s.Slot) {
			continue // a section has no cursor
		}
		v := m.mv.list[i]
		shown[panelKey(v, s.Project)] = true
		if host := m.mv.host(v, s.Project); host != nil {
			cmds = append(cmds, m.sendCursor(v, s.Project, host.Moved()))
		}
	}
	for k, h := range m.mv.hosted {
		if !shown[k] {
			h.Forget()
		}
	}
	return tea.Batch(cmds...)
}

// panelHints are the keys a mod's panel takes, for its action row.
func panelHints(host *widget.View) string {
	if host == nil {
		return ""
	}
	if host.Editing() {
		return "enter keep the filter · esc drop it"
	}
	if host.Focus() == "" {
		return "↑ ↓ scroll"
	}
	if b := boundHints(host); b != "" {
		return b + " · ↑ ↓ move · / filter"
	}
	return "↑ ↓ move · / filter"
}

// shownViews are the mods' views this console shows now: a mod tab of
// an open project popup, the unfolded cards while the dashboard's list
// shows, an open card and an open full view.
func (m *dash) shownViews() []modview.Shown {
	var out []modview.Shown
	add := func(v proto.ModView, project, task string, w, h int) {
		s := modview.Shown{Mod: v.Mod, View: v.View, Slot: v.Slot, Project: project, Task: task, Width: w, Height: h}
		if !slices.ContainsFunc(out, func(o modview.Shown) bool { return o.Mod == s.Mod && o.View == s.View && o.Project == s.Project }) {
			out = append(out, s)
		}
	}
	w, h := m.inner(viewWidth), max(m.h*8/10-6, 1)
	for _, o := range m.stack {
		switch o := o.(type) {
		case *projectView:
			if v, ok := o.modTab(m); ok {
				add(v, o.slug, o.shownTask(v), w, h)
			}
		case *cardView:
			if v, ok := m.mv.card(o.card); ok {
				add(v, o.project, "", w, h)
			}
		case *fullView:
			if v, ok := o.modView(m); ok {
				fw, fh := o.size(m)
				add(v, o.project, "", fw, fh)
			}
		}
	}
	if m.over == nil {
		project := listProject(m.data, m.current)
		g := m.geom()
		lw := g.list().w
		for _, v := range m.mv.cards() {
			if m.layout.shows("cards") && m.cardShows(cardKey(v)) {
				add(v, project, "", lw, m.cardHeight(v, project))
			}
		}
		if r, ok := m.selected(); ok && g.details().w > 0 {
			if project, task, thr, ok := sectionTarget(r); ok {
				out = append(out, sectionsShown(m.mv.list, mod.SlotDetailsSection, project, task, thr, g.details().w)...)
			}
		}
	}
	return out
}

// shownKey is the shown views as one string, to tell a change.
func shownKey(list []modview.Shown) string {
	var b strings.Builder
	for _, s := range list {
		fmt.Fprintf(&b, "%s/%s/%s/%s/%s/%s %dx%d;", s.Mod, s.View, s.Slot, s.Project, s.Task, s.Thread, s.Width, s.Height)
	}
	return b.String()
}

// pollModViews asks for the mods' views, saying which this console
// shows; nil while a poll is on its way.
func (m *dash) pollModViews() tea.Cmd {
	if m.mv.busy || !m.data.ServerOK {
		return nil
	}
	m.mv.busy = true
	src, shown := m.src, m.shownViews()
	key := shownKey(shown)
	return func() tea.Msg {
		res, err := src.ModViews(shown)
		return modViewsMsg{res: res, shown: key, err: err}
	}
}

// modViewsChanged polls at once when what this console shows changed
// since it last said, so the mods hear of it now.
func (m *dash) modViewsChanged() tea.Cmd {
	if m.mv.busy || !m.data.ServerOK || shownKey(m.shownViews()) == m.mv.sent {
		return nil
	}
	return m.pollModViews()
}

// setModViews takes a poll's answer, and acts on the mods' requests to
// open a view after a human input here.
func (m *dash) setModViews(msg modViewsMsg) tea.Cmd {
	m.mv.busy = false
	if msg.err != nil {
		// An older server, or none: no mod views.
		m.mv.list = nil
		return nil
	}
	m.mv.sent = msg.shown
	m.mv.list = msg.res.Views
	// Forget hosted views no longer listed.
	for k := range m.mv.hosted {
		if !slices.ContainsFunc(m.mv.list, func(v proto.ModView) bool { return strings.HasPrefix(k, v.Mod+"/"+v.View+"/") }) {
			delete(m.mv.hosted, k)
			delete(m.mv.trees, k)
		}
	}
	m.rebuild()
	for _, o := range msg.res.Open {
		if o.Seq <= m.mv.seen {
			continue
		}
		m.mv.seen = o.Seq
		m.openModView(o)
		m.awaitView = time.Time{}
	}
	return m.tellCursors()
}

// openModView shows the view a mod asked to open: its tab in the
// project popup of the input's project, its card, unfolded and
// selected, or its full view over what shows.
func (m *dash) openModView(o modview.Open) {
	for _, v := range m.mv.list {
		if v.Mod != o.Mod || v.View != o.View {
			continue
		}
		switch v.Slot {
		case mod.SlotProjectTab:
			slug := cmp.Or(o.Project, m.projectHere())
			if slug == "" || !runsFor(v, slug) {
				return
			}
			pv := m.projectPopupView()
			if pv == nil || pv.slug != slug {
				m.openTab(slug, tabOverview, 0)
				pv = m.projectPopupView()
			}
			if t, ok := coreTabOf(v); ok {
				pv.tab = t
			}
			for i, t := range m.mv.tabsFor(slug) {
				if t.Mod == v.Mod && t.View == v.View {
					pv.tab = tabCount + i
				}
			}
		case mod.SlotDashboardCard:
			if !m.layout.shows("cards") {
				return // hidden: no row to select
			}
			delete(m.mv.folded, cardKey(v))
			if m.top() == nil {
				m.sel, m.userSel = cardKey(v), true
			}
		case mod.SlotFull:
			m.openFull(v, cmp.Or(o.Project, listProject(m.data, m.current)))
		}
		return
	}
}

// The project popup's mod tabs come after its core tabs: tab tabCount
// is the first mod tab.

// tabs is how many tabs pv has: the core's and the mods'.
func (pv *projectView) tabs(m *dash) int { return tabCount + len(m.mv.tabsFor(pv.slug)) }

// modTab is the mod view of pv's open tab, if it is a mod's: the
// Memory and Tasks tabs are their built-in mods' while those run.
func (pv *projectView) modTab(m *dash) (proto.ModView, bool) {
	if v, ok := m.mv.coreTab(pv.slug, pv.tab); ok {
		return v, true
	}
	if pv.tab < tabCount {
		return proto.ModView{}, false
	}
	tabs := m.mv.tabsFor(pv.slug)
	if i := pv.tab - tabCount; i < len(tabs) {
		return tabs[i], true
	}
	pv.tab = tabOverview // its mod went
	return proto.ModView{}, false
}

// modTabLabel is mod tab v's label: its number, title and count.
func modTabLabel(v proto.ModView, slug string, i int) string {
	label := v.Title
	if i < 9 {
		label = fmt.Sprintf("%d %s", i+1, label)
	}
	if t := tree(v, slug); t != nil && t.Count != nil && v.State != proto.ModViewFailed {
		label += fmt.Sprintf(" %d", *t.Count)
	}
	return label
}

// modTabKey handles a key on a mod tab; ok false leaves it to the
// popup (tabs, esc).
func (pv *projectView) modTabKey(m *dash, v proto.ModView, k string) (tea.Cmd, bool) {
	pv.picking = false
	used, cmd := m.panelKeyPress(v, pv.slug, k)
	return cmd, used
}

// modTabBox is a mod tab's box: the tab bar, a blank line, and its
// view drawn in the rows left.
func (pv *projectView) modTabBox(m *dash, p ProjectData, v proto.ModView) box {
	w := m.inner(viewWidth)
	head := []string{pv.tabBar(m, p), ""}
	pv.pickTask(v, m.mv.host(v, pv.slug))
	keys := joinKeys(panelHints(m.mv.host(v, pv.slug)), "← → tabs · esc close")
	b := box{title: pv.slug, head: head, sel: -1, keys: keys}
	body := m.panelLines(v, pv.slug, w, max(m.boxRows(b)-len(head), 1))
	hits := []int{tabHit, noHit}
	for i := range body {
		hits = append(hits, i)
	}
	b.body, b.hits = body, hits
	return b
}

// The dashboard's cards.

// cardHeight is how many lines card v takes on the dashboard under its
// header: what the mod asks for, within modview.MaxHeight and half the
// list.
func (m *dash) cardHeight(v proto.ModView, project string) int {
	h := modview.DefaultHeight
	if t := tree(v, project); t != nil && t.Height > 0 {
		h = t.Height
	}
	if v.State == proto.ModViewFailed {
		h = 3
	}
	return max(min(h, modview.MaxHeight, m.geom().list().h/2), 1)
}

// cardRows are the cards' rows below the dashboard's sections: a
// header row each, selectable; its lines are drawn under it (listLines).
func (m *dash) cardRows() []row {
	var out []row
	if !m.layout.shows("cards") {
		return nil
	}
	for _, v := range m.mv.cards() {
		out = append(out, row{key: cardKey(v), what: v.Title, pct: -1, card: true})
	}
	return out
}

// cardLines are card v's lines w cells wide: its header, with the mod's
// name, and unless folded its view. sel draws the header
// selected.
func (m *dash) cardLines(v proto.ModView, w int, sel bool) []string {
	project := listProject(m.data, m.current)
	fold := "▾"
	if m.mv.folded[cardKey(v)] {
		fold = "▸"
	}
	left := " " + fold + " " + v.Title
	if v.Name != "" && v.Name != v.Title {
		left += " · " + v.Name
	}
	var head string
	if sel {
		st := th().Sel
		if m.area() != areaMain {
			st = st.Faint(true)
		}
		head = st.Render(fit(left, w))
	} else {
		fill := max(w-len([]rune(left))-2, 1)
		head = th().Title.Render(left) + " " + th().Faint.Render(strings.Repeat("─", fill))
	}
	out := []string{fit(head, w) + reset}
	if !m.cardShows(cardKey(v)) {
		return out
	}
	for _, l := range m.panelLines(v, project, w-3, m.cardHeight(v, project)) {
		out = append(out, "   "+fit(l, w-3)+reset)
	}
	return out
}

// cardShows says whether the card key shows its lines under its header:
// unless it is folded, or the rows are compact and it isn't selected.
func (m *dash) cardShows(key string) bool {
	return !m.mv.folded[key] && (!m.layout.compact() || key == m.sel)
}

// openCard opens the selected card as a popup, to move and filter in
// it and press the mod's keys.
func (m *dash) openCard(key string) tea.Cmd {
	m.push(&cardView{card: key, project: listProject(m.data, m.current)})
	return nil
}

// foldCard folds the selected card to its header, or unfolds it.
func (m *dash) foldCard(string) tea.Cmd {
	r, ok := m.selected()
	if !ok || !r.card {
		return nil
	}
	if m.mv.folded == nil {
		m.mv.folded = map[string]bool{}
	}
	m.mv.folded[r.key] = !m.mv.folded[r.key]
	return nil
}

// cardView is a dashboard card opened as a popup.
type cardView struct {
	card, project string // the card's row key, the project listed
}

func (cv *cardView) view(m *dash) (proto.ModView, bool) { return m.mv.card(cv.card) }

func (cv *cardView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	v, ok := cv.view(m)
	if !ok {
		m.pop()
		return nil
	}
	used, cmd := m.panelKeyPress(v, cv.project, k.String())
	if !used && k.String() == "esc" {
		m.pop()
	}
	return cmd
}

func (cv *cardView) box(m *dash) box {
	v, ok := cv.view(m)
	if !ok {
		return box{title: "mod card", body: []string{"This card is gone."}, sel: -1, keys: "esc close"}
	}
	w := m.inner(viewWidth)
	b := box{title: v.Title, sel: -1,
		keys: joinKeys(panelHints(m.mv.host(v, cv.project)), "esc close")}
	body := m.panelLines(v, cv.project, w, max(m.boxRows(b), 1))
	var hits []int
	for i := range body {
		hits = append(hits, i)
	}
	b.body, b.hits = body, hits
	return b
}

func (cv *cardView) render(m *dash) string { return m.popup(cv.box(m)) }

func (cv *cardView) click(m *dash, item, col int, _ bool) tea.Cmd {
	if v, ok := cv.view(m); ok && item >= 0 {
		return m.panelClick(v, cv.project, col, item)
	}
	return nil
}

func (cv *cardView) wheel(m *dash, d int) {
	if v, ok := cv.view(m); ok {
		m.panelWheel(v, cv.project, d)
	}
}

// The full views: a mod's view in the dashboard's body, opened only by
// the human, from the ≡ menu or a key in another of the mod's views (a
// request the mod makes in its handler). The header, the footer with
// its keys and message, and the sidebar with its needs-you marks stay
// as they are, and the body's first line is NEEDS YOU's count, so a
// full view never hides what waits for the user. esc returns to what
// was under it.

// fullView is a mod's full view, open.
type fullView struct {
	mod, view, project string
}

// fullViews are the mods' full views, in order.
func (mv *modViews) fullViews() []proto.ModView {
	var out []proto.ModView
	for _, v := range mv.list {
		if v.Slot == mod.SlotFull {
			out = append(out, v)
		}
	}
	return out
}

// fullItems are the ≡ menu's items opening the mods' full views.
func (m *dash) fullItems() []menuItem {
	var out []menuItem
	for _, v := range m.mv.fullViews() {
		out = append(out, menuItem{label: v.Title + " · " + v.Mod, run: func(m *dash) tea.Cmd {
			m.openFull(v, listProject(m.data, m.current))
			return nil
		}})
	}
	return out
}

// openFull opens v's full view for project, over whatever shows.
func (m *dash) openFull(v proto.ModView, project string) {
	if fv, ok := m.top().(*fullView); ok && fv.mod == v.Mod && fv.view == v.View {
		return
	}
	m.push(&fullView{mod: v.Mod, view: v.View, project: project})
}

func (fv *fullView) modView(m *dash) (proto.ModView, bool) {
	for _, v := range m.mv.fullViews() {
		if v.Mod == fv.mod && v.View == fv.view {
			return v, true
		}
	}
	return proto.ModView{}, false
}

// fullGutter is the cells left of a full view's lines.
const fullGutter = 2

// size is the room for the mod's tree: the body less NEEDS YOU's line,
// and the gutters.
func (fv *fullView) size(m *dash) (int, int) {
	return max(m.w-2*fullGutter, 4), max(m.bodyRows()-1, 1)
}

// needsLine is NEEDS YOU's count, as the dashboard heads its section.
func (m *dash) needsLine(w int) string {
	for _, r := range m.rows {
		if r.head == "NEEDS YOU" {
			return m.ruleIn(countLabel(r.head, r.count), sectionStyle(r.head), w)
		}
	}
	return m.ruleIn("NEEDS YOU 0", th().Faint, w)
}

func (fv *fullView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	v, ok := fv.modView(m)
	if !ok {
		m.close(fv)
		return nil
	}
	used, cmd := m.panelKeyPress(v, fv.project, k.String())
	if !used && k.String() == "esc" {
		m.close(fv)
	}
	return cmd
}

func (fv *fullView) render(m *dash) string {
	v, ok := fv.modView(m)
	w, h := fv.size(m)
	gutter := strings.Repeat(" ", fullGutter)
	body := []string{m.needsLine(m.w)}
	title, keys := "mod view", "esc back"
	var lines []string
	if !ok {
		lines = []string{th().Faint.Render("This view is gone: its mod was disabled or removed.")}
	} else {
		title = v.Title
		keys = joinKeys(panelHints(m.mv.host(v, fv.project)), keys)
		lines = m.panelLines(v, fv.project, w, h)
	}
	hits := []int{noHit}
	for i, l := range lines {
		body = append(body, gutter+fit(l, w)+reset)
		hits = append(hits, i)
	}
	out := m.withSide(m.frame(title, body, -1, keys))
	// The mouse: the body is the view's, the row above it (the header) its edge.
	g := m.geom()
	m.geo = &boxGeo{x: g.main().x + fullGutter - 2, y: g.body().y - 1, w: g.main().w, h: g.body().h + 1,
		head: 0, top: 0, rows: g.body().h, hits: hits}
	return out
}

func (fv *fullView) click(m *dash, item, col int, _ bool) tea.Cmd {
	if v, ok := fv.modView(m); ok && item >= 0 {
		return m.panelClick(v, fv.project, col, item)
	}
	return nil
}

func (fv *fullView) wheel(m *dash, d int) {
	if v, ok := fv.modView(m); ok {
		m.panelWheel(v, fv.project, d)
	}
}
