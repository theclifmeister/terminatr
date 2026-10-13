package tui

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The sidebar's extra keys (docs/SPEC.md §4): the filter (/), the rows
// that need you (w W), the nth project (1-9), the ends and pages (home
// end pgup pgdown), and the last place (prefix+;). The filter and the
// last place are this console's own, not the view's: another console
// filters its sidebar for itself. Both outlive the screen, so the
// filter kept on the dashboard still narrows the sidebar in a session.

// sideFilterState is the sidebar's filter: its text, and whether keys
// still type into it.
type sideFilterState struct {
	text   string
	typing bool
}

// on says the filter shows: typing, or a text kept.
func (f sideFilterState) on() bool { return f.typing || f.text != "" }

// place is where this console's keyboard is: a session's (session) or
// the dashboard (""), in which area, on which sidebar row while the
// sidebar has it.
type place struct {
	session, project string
	area             area
	sel              string
}

// thisConsole is the sidebar's state that is this console's own.
var thisConsole struct {
	sync.Mutex
	filter    sideFilterState
	now, last place
	known     bool   // now was noted
	lastKnown bool   // last was
	hover     string // the tooltip (sideHoverTip)
}

// resetConsole forgets the filter and the places (tests).
func resetConsole() {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	thisConsole.filter, thisConsole.now, thisConsole.last = sideFilterState{}, place{}, place{}
	thisConsole.known, thisConsole.lastKnown, thisConsole.hover = false, false, ""
}

func sideFilterNow() sideFilterState {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	return thisConsole.filter
}

func setSideFilter(f sideFilterState) {
	thisConsole.Lock()
	thisConsole.filter = f
	thisConsole.Unlock()
}

// notePlace notes where the keyboard is now; where it was before is the
// last place.
func notePlace(p place) {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	if p.area != areaSide {
		p.sel = ""
	}
	if thisConsole.known && p == thisConsole.now {
		return
	}
	if thisConsole.known {
		thisConsole.last, thisConsole.lastKnown = thisConsole.now, true
	}
	thisConsole.now, thisConsole.known = p, true
}

// lastPlace is the place before the one the keyboard is in.
func lastPlace() (place, bool) {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	return thisConsole.last, thisConsole.lastKnown
}

// sideHeadRows are the rows above the tree: the header, and the filter's
// line while it shows.
func sideHeadRows() int {
	if sideFilterNow().on() {
		return 2
	}
	return 1
}

// sideFilterLine is the filter's line, cw cells wide, in the accent
// colour: "/text", with a bar for the caret while typing; ok is false
// while there is no filter.
func sideFilterLine(cw int) (string, bool) {
	f := sideFilterNow()
	if !f.on() {
		return "", false
	}
	s := " /" + f.text
	if f.typing {
		s += "▏"
	}
	return th().Accent.Render(fit(s, cw)), true
}

// sideTree is what the tree folds: while a filter narrows the tree
// nothing, every project unfolded and the inactive ones expanded, so
// what matches anywhere shows; else what the user folded (t).
func sideTree(ps []ProjectData, t view.Tree) view.Tree {
	if sideFilterNow().text == "" {
		return t
	}
	return unfoldedTree(ps)
}

// unfoldedTree folds nothing: the divider open, every inactive project
// expanded.
func unfoldedTree(ps []ProjectData) view.Tree {
	t := view.Tree{InactiveOpen: true}
	for _, p := range ps {
		t.Expanded = append(t.Expanded, p.Slug)
	}
	return t
}

// sideRows are the tree's rows as this console shows them: the projects
// where something needs you first (needsFirst, the setting), then
// narrowed by the filter.
func sideRows(rows []treeRow, needsFirst bool) []treeRow {
	if needsFirst {
		rows = needsFirstOrder(rows)
	}
	return filterTree(rows, sideFilterNow().text)
}

// filterHit says r matches q (lower case): a project by its name, a
// thread by its name, title or id, the coordinator by its word.
func filterHit(r treeRow, q string) bool {
	switch r.kind {
	case treeProject:
		return strings.Contains(strings.ToLower(r.slug), q)
	case treeCoordinator:
		return strings.Contains("coordinator", q)
	case treeThread:
		return strings.Contains(strings.ToLower(r.name+" "+r.title+" "+r.thread), q)
	}
	return false
}

// filterTree narrows rows to those matching q, and the project rows over
// them: the project of a match stays, though it doesn't match itself.
// Any other row (a divider) goes while filtering.
func filterTree(rows []treeRow, q string) []treeRow {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return rows
	}
	keep := map[string]bool{}
	for _, r := range rows {
		if filterHit(r, q) {
			keep[r.slug] = true
		}
	}
	var out []treeRow
	for _, r := range rows {
		if r.kind == treeProject && keep[r.slug] || r.kind != treeProject && filterHit(r, q) {
			out = append(out, r)
		}
	}
	// The last thread kept under its coordinator ends the branch.
	for i := range out {
		if out[i].kind == treeThread {
			out[i].last = i+1 == len(out) || out[i+1].kind != treeThread
		}
	}
	return out
}

// needsFirstOrder moves the projects where something needs you (⚑) to
// the top of their group, each with its rows under it, below the pinned
// ones, which keep the top: the active projects and the inactive ones
// are each a group, and a row of another kind (a divider) stays where it
// is. Otherwise the order holds.
func needsFirstOrder(rows []treeRow) []treeRow {
	type block struct {
		rows         []treeRow
		pinned, hint bool
	}
	var out []treeRow
	var run []block // a group's projects, in order
	flush := func() {
		slices.SortStableFunc(run, func(a, b block) int {
			switch {
			case a.pinned != b.pinned:
				if a.pinned {
					return -1
				}
				return 1
			case a.hint == b.hint:
				return 0
			case a.hint:
				return -1
			}
			return 1
		})
		for _, b := range run {
			out = append(out, b.rows...)
		}
		run = nil
	}
	for _, r := range rows {
		switch r.kind {
		case treeProject:
			if len(run) > 0 && run[0].rows[0].inactive != r.inactive {
				flush()
			}
			run = append(run, block{rows: []treeRow{r}, pinned: r.pinned && !r.inactive, hint: r.hint})
		case treeCoordinator, treeThread:
			if len(run) > 0 {
				b := &run[len(run)-1]
				b.rows = append(b.rows, r)
				continue
			}
			out = append(out, r)
		default:
			flush()
			out = append(out, r)
		}
	}
	flush()
	return out
}

// Sidebar key operations of the extras, numbered apart from the others.
const (
	sideFirst = iota + 100
	sideLast
	sidePageUp
	sidePageDown
	sideNth
	sideNeedNext
	sideNeedPrev
	sideFilter
)

// sideKeysIn is what a sidebar key works on: the tree as shown (tree,
// filtered and folded), the whole tree with every project expanded and
// no filter (full, for w W), the sidebar's size and the keyboard's row.
type sideKeysIn struct {
	tree, full func() []treeRow
	w, h       int
	sel        string
}

// sideKeyFor decides what key does in the sidebar: the filter takes the
// keys while typing (and esc clears a kept one), the extras theirs, the
// rest go to sideKeyStep. ok is false for a key that isn't the
// sidebar's.
func sideKeyFor(in sideKeysIn, key string) (sideStep, bool) {
	if st, ok := sideFilterKey(in, key); ok {
		return st, true
	}
	key = binds.side.canon(key)
	op, ok := sideOp(key)
	if !ok {
		return sideStep{}, false
	}
	if op >= sideFirst {
		return sideExtraStep(in, op, key), true
	}
	return sideKeyStep(in.tree(), in.w, in.sel, op), true
}

// sideFilterKey runs a key on the filter: while typing, a letter types,
// backspace takes one back, enter keeps the filter and esc clears it;
// esc clears a kept one too. The cursor stays on its row while it shows,
// else goes to the first. ok is false for the other keys.
func sideFilterKey(in sideKeysIn, key string) (sideStep, bool) {
	f := sideFilterNow()
	switch {
	case key == "esc" && f.on():
		f = sideFilterState{}
	case !f.typing:
		return sideStep{}, false
	case key == "enter":
		f.typing = false
	case key == "backspace":
		if _, n := utf8.DecodeLastRuneInString(f.text); n > 0 {
			f.text = f.text[:len(f.text)-n]
		}
	case key == "space":
		f.text += " "
	case utf8.RuneCountInString(key) == 1:
		f.text += key
	default:
		return sideStep{}, false
	}
	setSideFilter(f)
	return sideFollow(in), true
}

// sideFollow moves the cursor to the row it falls back on when its own
// is no longer shown.
func sideFollow(in sideKeysIn) sideStep {
	rows := shownRows(in.tree(), in.w)
	if i := sideCursor(rows, in.sel); i >= 0 && rows[i].key() != in.sel {
		return sideStep{sel: rows[i].key()}
	}
	return sideStep{}
}

// sideExtraStep decides what an extra's key does.
func sideExtraStep(in sideKeysIn, op int, key string) sideStep {
	if op == sideFilter {
		setSideFilter(sideFilterState{typing: true})
		return sideFollow(in)
	}
	rows := shownRows(in.tree(), in.w)
	i := sideCursor(rows, in.sel)
	if i < 0 {
		return sideStep{msg: "no projects"}
	}
	moveTo := func(j int) sideStep {
		j = max(min(j, len(rows)-1), 0)
		if j == i {
			return sideStep{}
		}
		return sideStep{sel: rows[j].key()}
	}
	page := max(in.h-sideHeadRows()-4, 1)
	switch op {
	case sideFirst:
		return moveTo(0)
	case sideLast:
		return moveTo(len(rows) - 1)
	case sidePageUp:
		return moveTo(i - page)
	case sidePageDown:
		return moveTo(i + page)
	case sideNth:
		n := int(key[0] - '0')
		for _, r := range rows {
			if r.kind == treeProject {
				if n--; n == 0 {
					return moveTo(slices.IndexFunc(rows, func(x treeRow) bool { return x.key() == r.key() }))
				}
			}
		}
		return sideStep{msg: "no project " + key}
	case sideNeedNext, sideNeedPrev:
		return sideNeedStep(in, rows[i], op == sideNeedPrev)
	}
	return sideStep{}
}

// sideNeedStep moves the cursor from the row at to the next row that
// needs you (⚑), or the previous (back), round the end of the tree: a
// coordinator with questions or a thread waiting on you. In the slim
// strip, a project with something that needs you. What hides the row
// goes: the filter is cleared, its project unfolded (an inactive one's
// divider opened and the project expanded).
func sideNeedStep(in sideKeysIn, at treeRow, back bool) sideStep {
	slim := in.w <= sideSlim
	needs := func(r treeRow) bool {
		if slim {
			return r.kind == treeProject && r.hint
		}
		return r.kind != treeProject && r.hint
	}
	full := in.full()
	pos := slices.IndexFunc(full, func(r treeRow) bool { return r.key() == at.key() })
	var to *treeRow
	for n := 1; n <= len(full) && to == nil; n++ {
		j := pos + n
		if back {
			j = pos - n
		}
		j = (j%len(full) + len(full)) % len(full)
		if needs(full[j]) {
			to = &full[j]
		}
	}
	if to == nil {
		return sideStep{msg: "nothing needs you"}
	}
	st := sideStep{sel: to.key(), msg: "next row that needs you"}
	if back {
		st.msg = "previous row that needs you"
	}
	if to.key() == at.key() {
		st.sel, st.msg = "", "only this row needs you"
	}
	shown := func() bool {
		return slices.ContainsFunc(shownRows(in.tree(), in.w), func(r treeRow) bool { return r.key() == to.key() })
	}
	if !shown() && sideFilterNow().on() {
		setSideFilter(sideFilterState{})
	}
	if !shown() {
		slug, inactive := to.slug, to.inactive || to.dormant
		st.refold = func(t *view.Tree) {
			if inactive {
				t.InactiveOpen = true
				t.Expand(slug, true)
			} else {
				t.Fold(slug, false)
			}
		}
	}
	return st
}

// notePlace notes where this console's keyboard is in the session.
// c.mu held.
func (c *client) notePlace() {
	if c.focus == nil {
		return
	}
	notePlace(place{session: c.focus.info.ID, project: c.focus.info.Project, area: c.kb, sel: c.v.SideSel})
}

// goLastPlace takes the keyboard back to the last place (prefix+;): an
// area and sidebar row of this session, another session (attached), or
// the dashboard (left for).
func (c *client) goLastPlace() {
	if !c.lock() {
		return
	}
	to, ok := lastPlace()
	cur := ""
	if c.focus != nil {
		cur = c.focus.info.ID
	}
	switch {
	case !ok:
		c.flash = "no last place yet"
	case to.session == "" && c.bare:
		c.flash = "no dashboard here"
		ok = false
	}
	if !ok {
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	area := to.area
	if !slices.Contains(c.areas(), area) {
		area = areaMain
	}
	c.kb = area
	if area == areaSide {
		c.v.SideSel = to.sel
	}
	c.flash = "back to the last place"
	c.status()
	c.mu.Unlock()
	c.poke()
	if area == areaSide && c.vc != nil {
		c.act(proto.MethodViewSideSel, proto.ViewParams{Key: to.sel})
	}
	switch {
	case to.session == "":
		c.detachThen("")
	case to.session != cur:
		c.sideGo(Target{Project: to.project, Session: to.session})
	}
}

// notePlace notes where this console's keyboard is on the dashboard;
// not while it is a popup over a session, which is the session's place.
func (m *dash) notePlace() {
	if m.over != nil || m.quitting {
		return
	}
	notePlace(place{project: m.current, area: m.focus, sel: m.sideSel})
}

// goLastPlace takes the keyboard back to the last place (; and
// prefix+;): an area and sidebar row of the dashboard, or a session,
// attached.
func (m *dash) goLastPlace(string) tea.Cmd {
	to, ok := lastPlace()
	if !ok {
		m.msg = "no last place yet"
		return nil
	}
	m.stack = nil
	m.focus = to.area
	var cmds []tea.Cmd
	if to.area == areaSide {
		m.sideSel = to.sel
		cmds = append(cmds, m.sendSideSel())
	}
	m.msg = "back to the last place"
	switch {
	case to.session != "":
		cmds = append(cmds, m.openTarget(Target{Project: to.project, Session: to.session}))
	case to.project != "" && to.project != m.current:
		cmds = append(cmds, m.showProject(to.project))
	}
	return tea.Batch(cmds...)
}

// The hover tooltip (docs/SPEC.md §4): while the mouse is over one of a
// row's glyphs, the sidebar says what it means, in the details line, so
// there is nothing to close. The outer terminal reports every motion
// (1003) while the full sidebar shows; the pane's program still gets only
// the motion it asked for.

// sideHoverTip is what the glyph under the mouse means, "" while the
// mouse is on none: the details line shows it instead of the row's.
func sideHoverTip() string {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	return thisConsole.hover
}

// setSideHover sets the tooltip; changed says it differs from before.
func setSideHover(tip string) (changed bool) {
	thisConsole.Lock()
	defer thisConsole.Unlock()
	changed = thisConsole.hover != tip
	thisConsole.hover = tip
	return changed
}

// sideTip is what the glyph at column x, row y of a sidebar w×h with a
// footer of foot rows means; over a mod's section, the mod that draws
// it; "" off the glyphs, and in the slim strip, which has no details
// line.
func sideTip(all []treeRow, w, h, foot, x, y int) string {
	if w <= sideSlim {
		return ""
	}
	r, ok, _ := sideHitAt(all, w, h, foot, x, y)
	if !ok {
		return ""
	}
	if r.kind == treeSection {
		return sectionTip(r.sec)
	}
	end := w - 1 - sideGap // the grid's right edge
	lead, label := treeLead(r)
	cols := rowCols(r, end, lead, label)
	grid := treeGrid(r, cols)
	line := ansi.Strip(treeCells(r, end, false, false))
	for j := len(grid) - 1; j >= 0; j-- {
		gw := ansi.StringWidth(grid[j].s)
		end -= gw
		if x < end || x >= end+gw {
			continue
		}
		// A column a narrow row gave up holds the label instead.
		if strings.TrimSpace(grid[j].s) == "" || ansi.Cut(line, end, end+gw) != grid[j].s {
			return ""
		}
		return gridTip(r, cols[j])
	}
	return ""
}

// gridTip is the meaning of r's cell in column c (treeGrid).
func gridTip(r treeRow, c sideCol) string {
	switch c.id {
	case view.ColCount:
		switch r.kind {
		case treeProject:
			return "open threads"
		case treeCoordinator:
			if r.ctxHint {
				return "context use: consider /clear, the context lives in files"
			}
			return "context use"
		}
		return "progress"
	case view.ColState:
		switch {
		case r.dormant:
			return "dormant: its project is inactive"
		case r.kind == treeProject:
			return "its coordinator: " + stateMeaning(r.state)
		case r.kind == treeCoordinator:
			return "coordinator: " + stateMeaning(r.state)
		}
		return stateMeaning(r.state)
	case view.ColRemote:
		return "remote control is on for this coordinator"
	case view.ColPaused:
		return "the project is paused"
	case view.ColInactive:
		return "inactive: its coordinator and threads don't run"
	case view.ColNeeds:
		switch r.kind {
		case treeProject:
			return "needs you: something here waits on you"
		case treeCoordinator:
			return "needs you: its questions wait"
		}
		return "needs you: it waits on you"
	}
	if c.mod != nil {
		return cmp.Or(c.mod.title, c.id)
	}
	return ""
}

// stateMeaning is a state word in the help's words (symbolLines).
func stateMeaning(state string) string {
	word, _, _ := strings.Cut(state, " ")
	switch word {
	case "working", "started":
		return "working"
	case "blocked":
		return "blocked: waits on a permission prompt, a question or a task"
	case "idle", "open", "ready":
		return "idle: waiting for a prompt"
	case "starting", "running", "done":
		return word
	case "review":
		return "in review: waits for you to accept it"
	case "", "—":
		return "not running"
	}
	return "stopped, exited or resolved"
}

// hover follows the mouse over the dashboard: on a sidebar glyph, its
// tooltip.
func (m *dash) hover(mo tea.Mouse) {
	tip := ""
	if g := m.geom(); g.side().hasX(mo.X) && m.top() == nil {
		tip = sideTip(m.tree(), m.sideW(), m.h, m.sideFoot(), g.sideCol(mo.X), mo.Y)
	}
	setSideHover(tip)
}
