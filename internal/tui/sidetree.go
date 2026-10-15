package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// The sidebar's tree (docs/SPEC.md §4): its rows built from the
// projects and the view's folds (buildTree), and each row's cells in the
// grid of columns (treeCells, gridCell).

// The kinds of tree rows.
const (
	treeProject = iota
	treeCoordinator
	treeThread
	treeDivider // "inactive (n)": the inactive projects hang under it
	treeSection // a mod's section in the band below the tree (sidesections.go)
	// A server group's header, with peers (sidepeers.go): this server's
	// above its tree, each paired server's above its shared projects.
	treeServer
	// A paired server's shared project, its coordinator and its threads
	// (sidepeers.go): drawn as the kinds above, never acted on as them.
	treePeerProject
	treePeerCoord
	treePeerThread
	// The PROJECTS header as the cursor's row (headRow): never in the
	// tree, drawn as its header.
	treeHead
)

// dividerKey is the inactive divider's row key.
const dividerKey = "d:inactive"

// headKey is the PROJECTS header's key as View.SideSel names it: the
// cursor can rest on the header, whose target is every project's NEEDS
// YOU (allProjects).
const headKey = "h:projects"

// headRow is the PROJECTS header as a row, for what the cursor does on
// it (target, preview, details, the key bar); here: the all-projects
// NEEDS YOU shows.
func headRow(here bool) treeRow { return treeRow{kind: treeHead, pct: -1, here: here} }

// sideRowOf is the row sel names among rows, the header's (headRow)
// for headKey, with here for it.
func sideRowOf(rows []treeRow, sel string, here bool) (treeRow, bool) {
	if sel == headKey {
		return headRow(here), true
	}
	for _, r := range rows {
		if r.key() == sel {
			return r, true
		}
	}
	return treeRow{}, false
}

// treeRow is one row of the sidebar's tree.
type treeRow struct {
	kind    int
	slug    string // its project
	session string // the coordinator's or the thread's live session, "" for none
	thread  string // a thread's id
	name    string // a thread's name: its task's id, else its own (threadName)
	title   string // a thread's title
	// state is the coordinator's (on a project or coordinator row) or the
	// thread's state word; "" when no coordinator runs.
	state   string
	pct     int  // a thread's percent, -1 for none
	last    bool // a coordinator or thread: the last row under its parent
	current bool // a project: the current one
	// hint: something here needs you. A project: one of its threads is
	// blocked or waiting, a task needs you or its coordinator has
	// questions; a coordinator: it has questions waiting; a thread: it is
	// blocked or waits on you.
	hint    bool
	ci      string // a thread: its PR's checks, pass, fail or pending; "" for none
	pr      int    // a thread: its PR's number, 0 for none
	asks    int    // a coordinator row: its questions waiting
	remote  bool   // a coordinator row: its remote control is on
	ctx     int    // a coordinator row: its context window's use in percent, -1 for unknown
	ctxHint bool   // a coordinator row: ctx reached [ui] context_hint
	paused  bool   // a project: paused (no nudges, follow-up or new threads)
	// prFails: a project whose PR host fails, the PR polls in a row that
	// failed (ProjectData.PRHostFails); its name gets a red ▲.
	prFails int
	pinned  bool // a project: pinned to the top of the active ones
	// inactive: a project the user deactivated, shown collapsed: its row
	// alone, no coordinator or thread rows (§5.1), unless the user
	// expanded it; then its coordinator and threads are dormant rows.
	inactive bool
	expanded bool // an inactive project: the user expanded it
	// fold: a project or the divider hides the rows under it.
	fold    bool
	dormant bool // a coordinator or thread row of an inactive project
	// threads: a project's open threads; the divider's inactive projects.
	threads int
	// slugs: the divider's inactive projects, shown or not.
	slugs []string
	here  bool // the row you are on
	// cursor is the keyboard's row, set while the sidebar has this
	// console's keyboard focus (markCursor).
	cursor bool
	// cols are the grid's columns (sideCols); nil for the default ones.
	cols []sideCol
	// task is a thread's task ("T12"); cells are the mods' cells this
	// console knows (modCells), for the grid's mod columns.
	task  string
	cells *modCells
	// sec is a section row's section (treeSection).
	sec *sideSection
	// undrawn: a section's header the band has no room to draw
	// (markUndrawn); the cursor's path skips it.
	undrawn bool
	// peer is the paired server's alias of a server header or a remote
	// row, "" for this server's; link is a server header's (sidepeers.go).
	peer string
	link *peerLink
	// stale: a remote row whose link is down or whose last read failed:
	// its last values, dimmed, no live state.
	stale bool
	// gap: a blank line is drawn above the row (a paired server's
	// header); the row itself starts the group.
	gap bool
}

// folded says a project row hides its coordinator and threads, or the
// divider the inactive projects.
func (r treeRow) folded() bool { return r.fold }

// key names r as View.SideSel does: "p:<project>", "c:<project>",
// "t:<project>/<thread>" or the divider's "d:inactive".
func (r treeRow) key() string {
	switch r.kind {
	case treeCoordinator:
		return "c:" + r.slug
	case treeThread:
		return "t:" + r.slug + "/" + r.thread
	case treeDivider:
		return dividerKey
	case treeSection:
		return "s:" + r.sec.id
	case treeServer, treePeerProject, treePeerCoord, treePeerThread:
		return peerKey(r)
	case treeHead:
		return headKey
	}
	return "p:" + r.slug
}

// sideCursor is the index in rows of the keyboard's row: the row sel
// names, else the row you are on, else the first; -1 without rows.
func sideCursor(rows []treeRow, sel string) int {
	here := -1
	for i, r := range rows {
		if sel != "" && r.key() == sel {
			return i
		}
		if r.here && here < 0 {
			here = i
		}
	}
	if here < 0 && len(rows) > 0 {
		here = 0
	}
	return here
}

// hereKey is the key of the row you are on, "" for none.
func hereKey(rows []treeRow) string {
	for _, r := range rows {
		if r.here {
			return r.key()
		}
	}
	return ""
}

// markCursor marks the keyboard's row among the rows a sidebar w columns
// wide shows: the sidebar has the keyboard focus.
func markCursor(all []treeRow, w int, sel string) {
	rows, i := cursorAt(all, w, sel)
	if i < 0 {
		return
	}
	key := rows[i].key()
	for j := range all {
		if all[j].key() == key {
			all[j].cursor = true
			return
		}
	}
}

// markUndrawn marks the band's headers that a sidebar w columns wide and
// h rows tall, with a footer of foot rows, doesn't draw with the cursor
// on them, while it has the focus (side): those fitBand gives no room
// even as the cursor's, every one under bandMinBody rows. Without the
// focus, those it doesn't draw.
func markUndrawn(all []treeRow, w, h, foot int, sel string, side bool) {
	laid := func(sel string) sideLay {
		rows := slices.Clone(all)
		if side {
			markCursor(rows, w, sel)
		}
		return sideLayout(rows, w, h, foot)
	}
	lay := laid(sel)
	if len(lay.rows) != len(all) {
		return // the slim strip: no sections
	}
	// Under bandMinBody rows none shows, none counted as more.
	short := lay.fit.more == 0 && !slices.Contains(lay.fit.show, true)
	for j, i := range lay.band {
		switch {
		case !side || short || all[i].key() == sel:
			all[i].undrawn = !lay.fit.show[j]
		default:
			// The layout turns on the cursor: the band may draw a header
			// as the cursor's that it doesn't with the cursor elsewhere,
			// and the other way round.
			all[i].undrawn = !laid(all[i].key()).fit.show[j]
		}
	}
}

// cursorAt is the rows the cursor goes over in a sidebar w columns wide
// and the index among them of the keyboard's row (sideCursor): the rows
// it shows less the headers it doesn't draw (markUndrawn). A sel on one
// of those is on the row above it, as the band lays it out.
func cursorAt(all []treeRow, w int, sel string) ([]treeRow, int) {
	rows := shownRows(all, w)
	if !slices.ContainsFunc(rows, func(r treeRow) bool { return r.undrawn }) {
		return rows, sideCursor(rows, sel)
	}
	if k := slices.IndexFunc(rows, func(r treeRow) bool { return r.key() == sel }); k >= 0 && rows[k].undrawn {
		for ; k >= 0 && rows[k].undrawn; k-- {
		}
		sel = ""
		if k >= 0 {
			sel = rows[k].key()
		}
	}
	rows = slices.DeleteFunc(slices.Clone(rows), func(r treeRow) bool { return r.undrawn })
	return rows, sideCursor(rows, sel)
}

// treeIn is where a tree is drawn from: which project is current and
// which session has the focus ("" on the dashboard).
type treeIn struct {
	current string
	focus   string
	// ctxHint is [ui] context_hint: the percent from which a coordinator
	// row says to consider /clear; 0 for never.
	ctxHint int
	// tree is what the user folded (the view's).
	tree view.Tree
	// cols is the grid's column order (the view's Columns); mods are the
	// mod columns this console knows, by id (sideCols).
	cols []string
	mods map[string]modColumn
	// cells are the mods' cells; with them the grid gets their sidebar
	// columns (modCells.sideOrder) unless mods is set.
	cells *modCells
	// peers are the paired servers as the server's mirror has them
	// (peer.list): with any, the tree is in server groups (sidepeers.go);
	// host names this server's, thisHost when "".
	peers []proto.PeerMirror
	host  string
}

// buildTree lays out the tree: the active projects, pinned ones first,
// then the divider and under it the inactive ones; under each project
// its coordinator, and under that its open threads (ordered as the
// dashboard orders them). The row of the focused session is the one you
// are on, else the current project's.
func buildTree(ps []ProjectData, sessions []proto.SessionInfo, in treeIn) []treeRow {
	byID := map[string]proto.SessionInfo{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	var active, inactive []ProjectData
	for _, p := range ps {
		if p.inactive() {
			inactive = append(inactive, p)
		} else {
			active = append(active, p)
		}
	}
	// Pinned changes the order only.
	sort.SliceStable(active, func(i, j int) bool { return active[i].pinned() && !active[j].pinned() })
	var out []treeRow
	for _, p := range active {
		out = append(out, projectRows(p, sessions, byID, in)...)
	}
	if len(inactive) > 0 {
		div := treeRow{kind: treeDivider, pct: -1, threads: len(inactive), fold: !in.tree.InactiveOpen}
		for _, p := range inactive {
			div.slugs = append(div.slugs, p.Slug)
		}
		out = append(out, div)
		if !div.fold {
			for _, p := range inactive {
				out = append(out, projectRows(p, sessions, byID, in)...)
			}
		}
	}
	here := -1
	for i, r := range out {
		if in.focus != "" && r.kind != treeProject && r.session == in.focus {
			here = i
			break
		}
		if here < 0 && r.kind == treeProject && r.current {
			here = i
		}
	}
	if here >= 0 {
		out[here].here = true
	}
	out = withPeers(out, in)
	order, mods := in.cols, in.mods
	if mods == nil && in.cells != nil {
		order, mods = in.cells.sideOrder(order), in.cells.sideMods()
	}
	cols := sideCols(order, mods)
	for i := range out {
		out[i].cols, out[i].cells = cols, in.cells
	}
	return out
}

// projectRows are a project's rows in the tree: its own, and unless it
// is folded its coordinator's and its threads'.
func projectRows(p ProjectData, sessions []proto.SessionInfo, byID map[string]proto.SessionInfo, in treeIn) []treeRow {
	pr := treeRow{kind: treeProject, slug: p.Slug, pct: -1, threads: len(p.Threads), current: p.Slug == in.current,
		hint: p.Counts["needs_you"] > 0 || p.Questions > 0, paused: p.Safety != nil && p.Safety.Paused,
		pinned: p.pinned(), inactive: p.inactive(), prFails: p.PRHostFails}
	if pr.inactive && !in.tree.IsExpanded(p.Slug) {
		pr.fold = true
		return []treeRow{pr}
	}
	pr.expanded = pr.inactive
	remote, ctx := false, -1
	for _, s := range sessions {
		if s.Role == proto.RoleCoordinator && s.Project == p.Slug {
			pr.session, pr.state, remote, ctx = s.ID, stateWord(s), s.RemoteControl, s.ContextPercent()
			break
		}
	}
	threads := append([]ThreadRow(nil), p.Threads...)
	sort.SliceStable(threads, func(i, j int) bool {
		return threadGroup(threads[i], byID) < threadGroup(threads[j], byID)
	})
	var kids []treeRow
	for i := range threads {
		t := &threads[i]
		tr := treeRow{kind: treeThread, slug: p.Slug, thread: t.ID, name: threadName(t.Task, t.ID), title: oneLine(t.Title), pct: -1}
		tr.state, _, tr.session = threadState(t, byID)
		tr.pct = t.Status.Progress().Percent
		tr.ci, tr.pr, tr.task = t.Checks, t.PR, t.Task
		tr.hint = tr.state == "blocked" || tr.state == "review" || t.Status != nil && t.Status.NeedsYou != ""
		pr.hint = pr.hint || tr.hint
		kids = append(kids, tr)
	}
	// The threads hang under the coordinator, the project's only child.
	coord := treeRow{kind: treeCoordinator, slug: p.Slug, session: pr.session, state: pr.state, remote: remote, pct: -1, last: true,
		hint: p.Questions > 0, asks: p.Questions, ctx: ctx, ctxHint: ctx >= 0 && in.ctxHint > 0 && ctx >= in.ctxHint}
	if len(kids) > 0 {
		kids[len(kids)-1].last = true
	}
	if pr.inactive {
		// Expanded but inactive: nothing of it runs (§5.1).
		coord = treeRow{kind: treeCoordinator, slug: p.Slug, pct: -1, last: true, ctx: -1, dormant: true}
		for i := range kids {
			kids[i].dormant, kids[i].session, kids[i].state, kids[i].hint = true, "", "", false
		}
	}
	if !pr.inactive && in.tree.IsFolded(p.Slug) {
		// Folded: its row alone, with its coordinator's state and
		// whatever under it needs you.
		pr.fold = true
		return []treeRow{pr}
	}
	return append([]treeRow{pr, coord}, kids...)
}

// slimRows are the rows the slim strip shows: the projects and the
// divider, and with peers the server headers and remote projects.
func slimRows(rows []treeRow) []treeRow {
	var out []treeRow
	for _, r := range rows {
		if r.kind == treeProject || r.kind == treeDivider || r.kind == treeServer || r.kind == treePeerProject {
			r.here = r.current
			out = append(out, r)
		}
	}
	return out
}

// shownRows are the rows a sidebar w columns wide shows.
func shownRows(rows []treeRow, w int) []treeRow {
	if w <= sideSlim {
		return slimRows(rows)
	}
	return rows
}

// treeLine is one tree row cw cells wide, its last sideGap blank: the
// glyphs keep off the border, and the title gives up the room (the slim
// strip has no gap: its needs-you column ends in a blank). On the highlighted row the blank is highlighted
// too: a Nerd Font icon in the last column draws wider than its cell,
// and its overflow, in the highlight's foreground, would vanish on the
// plain background.
func treeLine(r treeRow, cw int, slim, focused bool) string {
	if slim {
		return treeCells(r, cw, true, focused)
	}
	gap := strings.Repeat(" ", min(sideGap, cw))
	if hl, sel := treeSel(r, focused); hl {
		gap = sel.Render(gap)
	}
	return treeCells(r, max(cw-sideGap, 0), slim, focused) + gap
}

// treeSel says whether r is the highlighted row (the keyboard's while
// the sidebar has the focus, else the one you are on) and its style.
func treeSel(r treeRow, focused bool) (bool, lipgloss.Style) {
	if focused {
		return r.cursor, th().SelFocus.Bold(true)
	}
	return r.here, th().Sel.Bold(true)
}

// treeCells is one tree row cw cells wide, in this console's icon set
// (icons.go). Its right-hand side is a fixed grid, the same on every row
// so its columns line up down the tree: the count or percent (pctCol
// cells), then six glyph columns two cells wide each, blank where they
// don't apply. A Unicode glyph sits in a column's left cell, a Nerd Font
// icon fills both. In a sidebar 36 columns wide, in the unicode set:
//
//	" ■ terminatr          3         ⚑"  a project: its open threads (faint
//	                                      at 0; 0 while inactive), ∥ while
//	                                      paused, ⊘ while inactive, and ⚑
//	                                      when something under it needs you
//	" └─ coordinator     38%   ● ⌁     "  its coordinator: its context use
//	                                      (yellow from [ui] context_hint, red
//	                                      from 80%), its state, ⌁ while its
//	                                      remote control is on, ⚑ while it
//	                                      has questions waiting
//	"    ├─ T229 Acti…  100% ☑ ◆       ⚑"  a thread: its task id and title
//	                                      (an ad hoc or adopted thread's own
//	                                      id, t-0002), its progress, its PR's
//	                                      checks (☑ pass, ☒ fail, ☐ running),
//	                                      its state, ⚑ while it waits on you
//
// The columns are count/%, PR/CI, state, remote, paused, inactive and
// needs you (sideGrid cells), in the order the view's Columns gives
// (view.Columns), where a mod's columns take their slots too (sideCols).
// A project row's state column is its
// coordinator's state, only while the project is folded: unfolded, the
// coordinator's row shows it. The rows of an inactive project the user
// unfolded are dormant: faint, "·" for their state and ⊘. The nerd set
// opens the current project's folder and marks the coordinator and
// threads with an icon of their own after the connector. A long name or
// title is cut with "…", but a thread's name (its task id) never is: its
// title gives way. Project rows are bold, the current one in the accent
// colour; the connectors are faint. A sidebar too narrow for the grid
// and a label drops the mod columns, then the count/% column, then the
// PR/CI one (rowCols).
//
// The slim strip (sideSlim) shows projects alone, "● term…⚑": its
// coordinator's state (⊘ while inactive), its name cut to 5 cells, and
// the needs-you column; the current one's name in the accent colour, as
// in the full sidebar.
// The row you are on is in reverse video, so colour is never the only
// signal. While the sidebar has the keyboard focus, the keyboard's row
// is instead in the theme's sel_focus look (cyan reverse by default).
func treeCells(r treeRow, cw int, slim, focused bool) string {
	var sel lipgloss.Style
	r.here, sel = treeSel(r, focused)
	if r.kind == treeDivider {
		return dividerCells(r, cw, slim, sel)
	}
	if r.kind == treeServer {
		return serverCells(r, cw, slim, sel)
	}
	r = r.drawn()
	if slim {
		return slimCells(r, cw, sel)
	}
	lead, label := treeLead(r)
	cols := rowCols(r, cw, lead, label)
	grid := treeGrid(r, cols)
	room := max(cw-runsWidth(grid), 0) // the lead's and the label's
	lead = cutRuns(lead, room)
	label = cutRuns(label, room-runsWidth(lead))
	lw := max(room-runsWidth(lead)-runsWidth(label)-1, 0) // a blank parts it from the grid
	switch r.kind {
	case treeProject:
		look := th().Head
		switch {
		case r.current:
			look = th().Title
		case r.inactive:
			look = th().Faint
		}
		label = append(label, projectName(r, lw, look)...)
	case treeCoordinator:
		look := th().Plain
		if r.state == "" || r.dormant {
			look = th().Faint
		}
		label = append(label, cellRun{fitLabel("coordinator", lw), look})
	default:
		look := th().Plain
		if r.dormant {
			look = th().Faint
		}
		label = append(label, cellRun{threadLabel(r.name, r.title, lw), look})
	}
	label = append(label, cellRun{strings.Repeat(" ", room-runsWidth(lead)-runsWidth(label)), th().Plain})
	rest := append(label, grid...)
	if r.here {
		return fit(runsRender(lead)+sel.Render(fit(runsPlain(rest), cw-runsWidth(lead))), cw)
	}
	if r.stale {
		// Its last values, dimmed (sidepeers.go).
		return fit(runsRender(lead)+th().Faint.Render(runsPlain(rest)), cw)
	}
	return fit(runsRender(lead)+runsRender(rest), cw)
}

// threadLabel is a thread row's label w cells wide: "T12 Title" (a
// thread without a task: "t-0001 Title"), the title cut to fit (or left
// out when not even a letter of it fits) and the name never cut. A name
// wider than w leaves the label blank.
func threadLabel(id, title string, w int) string {
	iw := ansi.StringWidth(id)
	switch {
	case iw > w:
		return strings.Repeat(" ", w)
	case title == "" || w-iw-1 < 2:
		return fit(id, w)
	}
	return id + " " + clipWord(title, w-iw-1)
}

// clipWord fits s in w cells; a cut title ends in "…" right after its
// last letter, never after a space.
func clipWord(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return fit(s, w)
	}
	cut := strings.TrimRight(ansi.Truncate(s, max(w-1, 0), ""), " ")
	return fit(cut+"…", w)
}

// prMark is the mark after the name of a project whose PR host fails:
// a blank, then ▲ in the console's icon set (a Nerd Font icon draws over
// the blank after it, so it gets one).
func prMark() string {
	i := ic()
	if i.wide {
		return " " + i.blocked + " "
	}
	return " " + i.blocked
}

// projectName is project row r's name in at most lw cells, in look,
// and while its PR host fails, the red mark (prMark) right after it: the
// name is cut before the mark, and a row too narrow for both keeps the
// name alone.
func projectName(r treeRow, lw int, look lipgloss.Style) []cellRun {
	mark := prMark()
	mw := ansi.StringWidth(mark)
	if r.prFails == 0 || lw < mw+2 {
		return []cellRun{{fitLabel(r.slug, lw), look}}
	}
	name := r.slug
	if ansi.StringWidth(name) > lw-mw {
		name = fitLabel(name, lw-mw)
	}
	return []cellRun{{name, look}, {mark, th().Bad}}
}

// prHostWords are the tooltip's and the details line's words for a
// project whose PR host fails, n polls in a row.
func prHostWords(n int) string {
	return fmt.Sprintf("its PR host CLI failed on %s in a row · see the inbox", counted(n, "PR poll"))
}

// treeLead is the start of a full sidebar's row r, before its name:
// the lead, which stays out of the highlight (the margin, and on a row
// under a project the connector and its icon), and the label's icon.
func treeLead(r treeRow) (lead, label []cellRun) {
	i := ic()
	wide := ""
	if i.wide {
		wide = " " // a Nerd Font icon draws over the blank after it
	}
	if r.kind == treeProject {
		folder, fst := i.folder, th().Faint
		if r.current {
			folder, fst = i.folderOpen, th().Accent
		}
		return []cellRun{{" ", th().Plain}}, []cellRun{{folder + wide + " ", fst}}
	}
	conn, node := i.mid, i.thread
	if r.last {
		conn = i.end
	}
	if r.kind == treeCoordinator {
		node = i.coord
	}
	if node != "" {
		node += wide
	}
	// A thread sits one level down, under the coordinator's label. The
	// coordinator is the project's last row, so no │ runs down beside
	// its threads.
	indent := ""
	if r.kind == treeThread {
		indent = "   "
	}
	return []cellRun{{" " + indent, th().Plain}, {conn + node, th().Faint}, {" ", th().Plain}}, nil
}

// rowCols are the grid's columns row r keeps in a row cw cells wide
// whose lead and label's icon are lead and label. Narrow, the mod
// columns give way (the PR/CI one among them), the rightmost first,
// then the count/% column, before the label is cut below a thread's whole name (5
// cells for the others); narrower still, the label goes, then the lead
// is cut. The other columns never give way.
func rowCols(r treeRow, cw int, lead, label []cellRun) []sideCol {
	cols := r.columns()
	least := 5
	if r.kind == treeThread {
		least = ansi.StringWidth(r.name)
	}
	need := runsWidth(lead) + runsWidth(label) + least + 1
	for cw-colsWidth(cols) < need {
		j := dropCol(cols)
		if j < 0 {
			break
		}
		cols = slices.Delete(slices.Clone(cols), j, j+1)
	}
	return cols
}

// dropCol is the column of cols a narrow row gives up first (rowCols),
// -1 when none may go.
func dropCol(cols []sideCol) int {
	for j := len(cols) - 1; j >= 0; j-- {
		if cols[j].mod != nil {
			return j
		}
	}
	if j := slices.IndexFunc(cols, func(c sideCol) bool { return c.id == view.ColCount }); j >= 0 {
		return j
	}
	return -1
}

// fitLabel is fit, blank where not even a letter and "…" fit.
func fitLabel(s string, w int) string {
	if w < 2 && ansi.StringWidth(s) > w {
		return strings.Repeat(" ", max(w, 0))
	}
	return fit(s, w)
}

// treeGrid is the grid on the right of a full sidebar's row: the cells
// of r's columns cols (rowCols), each as wide as its column.
func treeGrid(r treeRow, cols []sideCol) []cellRun {
	out := make([]cellRun, 0, len(cols))
	for _, c := range cols {
		if c.mod != nil {
			out = append(out, modRun(r.modCell(c.id), c.w))
			continue
		}
		out = append(out, gridCell(r, c))
	}
	return out
}

// pctCol is the width of the grid's count/% column: "100% ".
const pctCol = 5

// sideGrid is the width of the default grid on the right of every row
// of the full sidebar without mod columns: the count/% column, then five
// glyph columns two cells wide (treeCells). The PR/CI column is the
// pr-ci-column mod's: two cells more while it is on.
const sideGrid = pctCol + 5*2

// modCell is r's cell in mod column col: a project's, a coordinator's
// session's, or a thread's (its own, else its task's, else its
// session's), when the column shows on rows of r's kind; nil for none.
func (r treeRow) modCell(col string) []widget.Span {
	if r.isPeer() {
		return nil // the mods are this server's
	}
	p := r.slug
	switch r.kind {
	case treeProject:
		if r.cells.shows(col, proto.SlotSidebarProject) {
			return r.cells.cell(col, proto.CellTarget{Project: p})
		}
	case treeCoordinator:
		if r.session != "" && r.cells.shows(col, proto.SlotSidebarCoordinator) {
			return r.cells.cell(col, proto.CellTarget{Session: r.session})
		}
	case treeThread:
		if !r.cells.shows(col, proto.SlotSidebarThread) {
			return nil
		}
		ts := []proto.CellTarget{{Project: p, Thread: r.thread}}
		if r.task != "" {
			ts = append(ts, proto.CellTarget{Project: p, Task: r.task})
		}
		if r.session != "" {
			ts = append(ts, proto.CellTarget{Session: r.session})
		}
		return r.cells.cell(col, ts...)
	}
	return nil
}

// gridCell is r's cell in column c: a built-in column's (treeCells), a
// mod's blank until the mod publishes cells.
func gridCell(r treeRow, c sideCol) cellRun {
	i := ic()
	col := func(on bool, g string, st lipgloss.Style) cellRun {
		if !on || g == "" {
			return cellRun{"  ", th().Plain}
		}
		return cellRun{g + " ", st}
	}
	thread, proj := r.kind == treeThread, r.kind == treeProject
	switch c.id {
	case view.ColCount:
		num, nst := "", th().Faint
		switch r.kind {
		case treeProject:
			n := r.threads
			if r.inactive {
				n = 0 // nothing of it runs
			}
			num = fmt.Sprint(n)
			if n > 0 {
				nst = th().Plain
			}
		case treeCoordinator:
			if r.ctx >= 0 && !r.dormant {
				num = fmt.Sprintf("%d%%", r.ctx)
				switch {
				case r.ctx >= 80:
					nst = th().Bad
				case r.ctxHint:
					nst = th().Warn
				}
			}
		default:
			if r.pct >= 0 {
				num = fmt.Sprintf("%d%%", r.pct)
			}
		}
		return cellRun{fmt.Sprintf("%*s ", pctCol-1, num), nst}
	case view.ColState:
		var g string
		var st lipgloss.Style
		switch {
		case proj && (!r.folded() || r.inactive):
		case r.dormant, r.stale:
			g, st = i.none, th().Faint
		case thread:
			if g, st = stateLook(r.state); g == "" {
				g, st = i.none, th().Faint
			}
		default:
			g, st = coordLook(r.state)
		}
		return col(g != "", g, st)
	case view.ColRemote:
		return col(r.kind == treeCoordinator && r.remote && !r.dormant, i.remote, th().Accent)
	case view.ColPaused:
		return col(proj && r.paused, i.paused, th().Warn)
	case view.ColInactive:
		return col(proj && r.inactive || r.dormant, i.inactive, th().Faint)
	case view.ColNeeds:
		return col(r.hint, i.hint, th().Warn)
	}
	return cellRun{strings.Repeat(" ", c.w), th().Plain}
}

// sideCol is a column of the grid on the right of a full sidebar's rows
// (treeCells): a built-in one, or a mod's.
type sideCol struct {
	id  string
	w   int
	mod *modColumn // a mod's column, nil for a built-in one
}

// modColumn is a mod's sidebar column as its manifest declares it (the
// mods design §5): its width in cells and its title. A mod column draws
// blank until the mod publishes its cells, and blank again while its mod
// host is down; the rest of the row never moves for it.
type modColumn struct {
	width int
	title string
}

// maxModCol bounds a mod column's width (view.MaxModColumnWidth, which
// the manifest check holds mods to as well).
const maxModCol = view.MaxModColumnWidth

// defaultCols are the grid's columns in the default order.
var defaultCols = sideCols(nil, nil)

// sideCols are the grid's columns in order (view.Columns: the safety
// columns always among them): the built-in ones, and those of the mods
// in mods; a mod column no mod declares takes no room.
func sideCols(order []string, mods map[string]modColumn) []sideCol {
	var out []sideCol
	for _, id := range prciOrder(view.Columns(order)) {
		switch {
		case id == view.ColCount:
			out = append(out, sideCol{id: id, w: pctCol})
		case !view.IsModColumn(id):
			out = append(out, sideCol{id: id, w: 2})
		default:
			if m, ok := mods[id]; ok && m.width > 0 {
				m.width = min(m.width, maxModCol)
				out = append(out, sideCol{id: id, w: m.width, mod: &m})
			}
		}
	}
	return out
}

// columns are r's grid columns, the default ones unless buildTree set
// them.
func (r treeRow) columns() []sideCol {
	if r.cols == nil {
		return defaultCols
	}
	return r.cols
}

// colsWidth is the width of the grid of cols.
func colsWidth(cols []sideCol) int {
	w := 0
	for _, c := range cols {
		w += c.w
	}
	return w
}

// dividerCells is the inactive divider's row cw cells wide, faint:
// " ── ▸ inactive (3) ───", ▾ while unfolded; in the slim strip " ─▸3──"
// and a blank.
func dividerCells(r treeRow, cw int, slim bool, sel lipgloss.Style) string {
	i := ic()
	mark := i.fold
	if !r.fold {
		mark = i.unfold
	}
	line := fmt.Sprintf("── %s inactive (%d) ", mark, r.threads)
	w := max(cw-1, 0) // less the margin
	if slim {
		// The strip has no gap before the border: the rule leaves one.
		line, w = fmt.Sprintf("─%s%d", mark, r.threads), max(cw-2, 0)
	}
	line = fit(line+strings.Repeat("─", max(w-ansi.StringWidth(line), 0)), w)
	if r.here {
		return fit(" "+sel.Render(line), cw)
	}
	return fit(" "+th().Faint.Render(line), cw)
}

// slimCells is a project's row in the slim strip, cw cells wide: its
// state column, its name, its needs-you column. The state column says
// inactive, else paused (the strip has no paused column), unless the
// coordinator is blocked or waiting, which needs you first.
func slimCells(r treeRow, cw int, sel lipgloss.Style) string {
	i := ic()
	g, st := coordLook(r.state)
	word, _, _ := strings.Cut(r.state, " ")
	switch {
	case r.stale:
		g, st = i.none, th().Faint
	case r.inactive:
		g, st = i.inactive, th().Faint
	case r.paused && word != "blocked" && word != "waiting":
		g, st = i.paused, th().Warn
	}
	hint := cellRun{"  ", th().Plain}
	if r.hint {
		hint = cellRun{i.hint + " ", th().Warn}
	}
	look := th().Head
	switch {
	case r.prFails > 0:
		// Its PR host fails: the full sidebar's red ▲ has no room here.
		look = th().Bad
	case r.current:
		look = th().Title
	case r.inactive, r.stale:
		look = th().Faint
	}
	rest := []cellRun{{g + " ", st}, {fit(r.slug, max(cw-5, 0)), look}, hint}
	if r.here {
		return fit(" "+sel.Render(runsPlain(rest)), cw)
	}
	return fit(" "+runsRender(rest), cw)
}

// cellRun is a run of cells of a tree row in one style.
type cellRun struct {
	s  string
	st runStyle
}

// runStyle draws a run: a theme style, or a mod cell's spans
// (spanStyle).
type runStyle interface{ Render(strs ...string) string }

// cutRuns cuts rs to at most w cells; a cut run ends in a blank, so a
// Nerd Font icon never draws over what follows.
func cutRuns(rs []cellRun, w int) []cellRun {
	var out []cellRun
	for _, r := range rs {
		if n := ansi.StringWidth(r.s); n > w {
			r.s = ansi.Truncate(r.s, max(w-1, 0), "")
			if w > 0 {
				r.s += " "
			}
		}
		w -= ansi.StringWidth(r.s)
		if r.s != "" {
			out = append(out, r)
		}
	}
	return out
}

func runsWidth(rs []cellRun) int { return ansi.StringWidth(runsPlain(rs)) }

func runsPlain(rs []cellRun) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString(r.s)
	}
	return b.String()
}

func runsRender(rs []cellRun) string {
	var b strings.Builder
	for _, r := range rs {
		if strings.TrimSpace(r.s) == "" {
			b.WriteString(r.s)
			continue
		}
		b.WriteString(r.st.Render(r.s))
	}
	return b.String()
}

// target is where a click on r goes; ok is false for a thread without a
// running session, with why.
func (r treeRow) target() (t Target, ok bool, why string) {
	if r.kind == treeHead {
		// Every project's NEEDS YOU.
		return Target{Project: allProjects}, true, ""
	}
	if r.kind == treePeerProject {
		// Its read-only dashboard (peerdash.go).
		return Target{Project: peerFold(r.peer, r.slug)}, true, ""
	}
	if r.kind == treeServer || r.isPeer() {
		return peerTarget(r)
	}
	t.Project = r.slug
	switch r.kind {
	case treeCoordinator:
		t.Coordinator, t.Activate = true, r.dormant
	case treeDivider:
		return t, false, "enter on the divider folds or unfolds it"
	case treeSection:
		return t, false, "a section: ← → fold it, enter opens it"
	case treeThread:
		if r.dormant {
			return t, false, r.thread + " is dormant: start " + r.slug + "'s coordinator, or activate " + r.slug
		}
		if r.session == "" {
			return t, false, r.thread + " has no running session; its coordinator restarts it"
		}
		t.Session = r.session
	}
	return t, true, ""
}

// previewDelay is how long the sidebar's cursor rests on a row before
// the view shows what is under it, so moving through the tree doesn't
// open every row on its way.
const previewDelay = 200 * time.Millisecond

// preview is what the view shows while the sidebar's cursor rests on r:
// a project's dashboard, a coordinator's or a thread's live session. A
// row with no session running (an inactive project's, a coordinator not
// started) shows its project's dashboard: a preview never starts
// anything. ok is false on the row you are on, which already shows, and
// on the inactive divider, which shows nothing.
func (r treeRow) preview() (t Target, ok bool) {
	if r.kind == treePeerProject && !r.here {
		return Target{Project: peerFold(r.peer, r.slug)}, true
	}
	if r.kind == treeHead && !r.here {
		return Target{Project: allProjects}, true
	}
	if r.here || r.kind == treeDivider || r.kind == treeSection || r.kind == treeServer || r.isPeer() {
		return t, false
	}
	t.Project = r.slug
	if r.kind != treeProject && r.session != "" {
		t.Session = r.session
	}
	return t, true
}
