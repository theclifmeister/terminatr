package tui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The projects sidebar (docs/SPEC.md §4): a column on the left of every
// screen, the dashboard and the attach view alike, holding the project
// tree. Each project row has its coordinator's state glyph, a hint when
// one of its threads is blocked or waiting or one of its tasks needs
// you (review or blocked), and its count of open threads. Every active
// project is always expanded (an inactive one is collapsed, its row
// alone, docs/SPEC.md §5.1): its coordinator hangs under it and its open
// threads under the coordinator, on tree connectors (├─ └─), with their
// state glyphs and progress in columns of their own. The current project is in
// the accent colour, and the row you are on is highlighted. A click on a project shows its dashboard, on the
// coordinator attaches to it, on a thread attaches its session. Its border column
// can be dragged, and { } b change it from the keys.
//
// Its state (width, slim strip, keyboard row) is part of the
// server-owned view; the rest here only draws and hit-tests.

// SidebarLayout is the sidebar's layout: part of the server-owned view
// (docs/SPEC.md §3.3), and kept in ui.json as the layout new views start
// with.
type SidebarLayout = view.Sidebar

const (
	sideDefault = view.SideDefault
	sideSlim    = view.SideSlim
	sideRoom    = view.SideRoom
)

// The kinds of tree rows.
const (
	treeProject = iota
	treeCoordinator
	treeThread
)

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
	// inactive: a project the user deactivated, shown collapsed: its row
	// alone, no coordinator or thread rows (§5.1), unless the user
	// expanded it; then its coordinator and threads are dormant rows.
	inactive bool
	expanded bool // an inactive project: the user expanded it
	dormant  bool // a coordinator or thread row of an inactive project
	threads  int  // a project: its open threads
	here     bool // the row you are on
	// cursor is the keyboard's row, set while the sidebar has this
	// console's keyboard focus (markCursor).
	cursor bool
}

// folded says a project row hides its coordinator and threads.
func (r treeRow) folded() bool { return r.kind == treeProject && r.inactive && !r.expanded }

// key names r as View.SideSel does: "p:<project>", "c:<project>" or
// "t:<project>/<thread>".
func (r treeRow) key() string {
	switch r.kind {
	case treeCoordinator:
		return "c:" + r.slug
	case treeThread:
		return "t:" + r.slug + "/" + r.thread
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
	rows := shownRows(all, w)
	i := sideCursor(rows, sel)
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

// treeIn is where a tree is drawn from: which project is current and
// which session has the focus ("" on the dashboard).
type treeIn struct {
	current string
	focus   string
	// ctxHint is [ui] context_hint: the percent from which a coordinator
	// row says to consider /clear; 0 for never.
	ctxHint int
	// expanded are the inactive projects the user expanded (the view's).
	expanded []string
}

// buildTree lays out the tree: every project, under each its
// coordinator, and under that its open threads (ordered as the dashboard
// orders them). The row of the focused session is the one you are on, else the
// current project's.
func buildTree(ps []ProjectData, sessions []proto.SessionInfo, in treeIn) []treeRow {
	byID := map[string]proto.SessionInfo{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	var out []treeRow
	for _, p := range ps {
		pr := treeRow{kind: treeProject, slug: p.Slug, pct: -1, threads: len(p.Threads), current: p.Slug == in.current,
			hint: p.Counts["needs_you"] > 0 || p.Questions > 0, paused: p.Safety != nil && p.Safety.Paused, inactive: p.inactive()}
		if pr.inactive && !slices.Contains(in.expanded, p.Slug) {
			out = append(out, pr)
			continue
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
			tr.ci, tr.pr = t.Checks, t.PR
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
		kids = append([]treeRow{coord}, kids...)
		out = append(out, pr)
		out = append(out, kids...)
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
	return out
}

// slimRows are the rows the slim strip shows: the projects alone.
func slimRows(rows []treeRow) []treeRow {
	var out []treeRow
	for _, r := range rows {
		if r.kind == treeProject {
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

// sideTop is the first row shown in h rows (one is the header), so that
// the keyboard's row, else the row you are on, shows.
func sideTop(rows []treeRow, h int) int {
	cur := -1
	for i, r := range rows {
		if r.cursor {
			cur = i
			break
		}
		if r.here {
			cur = i
		}
	}
	return scrollTop(cur, max(h-1, 1), len(rows))
}

// sidebarLines draws the sidebar w columns wide (its border included) and
// h rows tall: a header with the count of projects, then the tree, then
// the footer's lines (sideFooter) at the bottom. While
// it has the keyboard focus (a row is the cursor) its border is in the
// accent colour and the cursor's row is highlighted instead of the one
// you are on.
func sidebarLines(all []treeRow, w, h int, foot []string) []string {
	cw := max(w-1, 0) // less the border
	slim := w <= sideSlim
	// Every full row leaves a column before the border (sideGap); in the
	// slim strip the needs-you column's blank cell does.
	gap := sideGap
	if slim {
		gap = 0
	}
	rows := shownRows(all, w)
	if len(foot) > 0 && h < len(foot)+4 {
		foot = nil // no room: the tree comes first
	}
	h -= len(foot)
	focused := slices.ContainsFunc(all, func(r treeRow) bool { return r.cursor })
	border := styleFaint.Render("│")
	if focused {
		border = styleAccent.Render("│")
	}
	out := make([]string, 0, h)
	n := 0
	for _, r := range all {
		if r.kind == treeProject {
			n++
		}
	}
	out = append(out, sideHead(n, cw-gap, slim)+strings.Repeat(" ", gap)+reset+border)
	if n == 0 {
		note := " no projects"
		if slim {
			note = " —"
		}
		out = append(out, styleFaint.Render(fit(note, cw))+reset+border)
	}
	for _, r := range rows[sideTop(rows, h):] {
		if len(out) >= h {
			break
		}
		out = append(out, treeLine(r, cw, slim, focused)+reset+border)
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", cw)+border)
	}
	out = out[:h]
	for _, f := range foot {
		out = append(out, fit(f, cw-gap)+reset+strings.Repeat(" ", gap)+border)
	}
	return out
}

// sideHead is the sidebar's header, cw cells wide: " PROJECTS", its
// count of projects on the right, just before the needs-you column; in
// the slim strip the word alone, cut with "…".
func sideHead(n, cw int, slim bool) string {
	if slim {
		return styleTitle.Render(fit(" PROJECTS", cw))
	}
	count := ""
	if n > 0 {
		count = fmt.Sprint(n)
	}
	gap := cw - ansi.StringWidth(" PROJECTS") - len(count) - 2
	if gap < 1 {
		return styleTitle.Render(fit(countLabel(" PROJECTS", n), cw))
	}
	return styleTitle.Render(" PROJECTS") + strings.Repeat(" ", gap) + styleFaint.Render(count) + "  "
}

// coordLook is a coordinator's glyph and style: "·" when none runs.
func coordLook(state string) (string, lipgloss.Style) {
	if state == "" {
		return ic().none, styleFaint
	}
	return stateLook(state)
}

// sideGap is the blank column between the sidebar's rows and its border,
// so the state glyphs don't touch it.
const sideGap = 1

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
	sel := styleSel.Bold(true)
	if focused {
		return r.cursor, sel.Foreground(lipgloss.Cyan)
	}
	return r.here, sel
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
// needs you (sideGrid cells). A project row's state column is its
// coordinator's state, only while the project is folded: unfolded, the
// coordinator's row shows it. The rows of an inactive project the user
// unfolded are dormant: faint, "·" for their state and ⊘. The nerd set
// opens the current project's folder and marks the coordinator and
// threads with an icon of their own after the connector. A long name or
// title is cut with "…", but a thread's name (its task id) never is: its
// title gives way. Project rows are bold, the current one in the accent
// colour; the connectors are faint. A sidebar too narrow for the grid
// and a label drops the count/% column, then the PR/CI one.
//
// The slim strip (sideSlim) shows projects alone, "● term…⚑": its
// coordinator's state (⊘ while inactive), its name cut to 5 cells, and
// the needs-you column; the current one's name in the accent colour, as
// in the full sidebar.
// The row you are on is in reverse video, so colour is never the only
// signal. While the sidebar has the keyboard focus, the keyboard's row
// is instead, in the accent colour.
func treeCells(r treeRow, cw int, slim, focused bool) string {
	var sel lipgloss.Style
	r.here, sel = treeSel(r, focused)
	if slim {
		return slimCells(r, cw, sel)
	}
	i := ic()
	wide := ""
	if i.wide {
		wide = " " // a Nerd Font icon draws over the blank after it
	}
	grid := treeGrid(r)
	// The lead stays out of the highlight: the margin, and on a row under
	// a project the connector and its icon.
	var lead, label []cellRun
	if r.kind == treeProject {
		folder, fst := i.folder, styleFaint
		if r.current {
			folder, fst = i.folderOpen, styleAccent
		}
		lead = []cellRun{{" ", stylePlain}}
		label = []cellRun{{folder + wide + " ", fst}}
	} else {
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
		// A thread sits one level down, under the coordinator's label.
		// The coordinator is the project's last row, so no │ runs down
		// beside its threads.
		indent := ""
		if r.kind == treeThread {
			indent = "   "
		}
		lead = []cellRun{{" " + indent, stylePlain}, {conn + node, styleFaint}, {" ", stylePlain}}
	}
	// Narrow: the count/% column gives way, then the PR/CI one, before
	// the label is cut below a thread's whole name (5 cells for the
	// others); narrower still, the label goes, then the lead is cut.
	least := 5
	if r.kind == treeThread {
		least = ansi.StringWidth(r.name)
	}
	need := runsWidth(lead) + runsWidth(label) + least + 1
	for len(grid) > 5 && cw-runsWidth(grid) < need {
		grid = grid[1:]
	}
	room := max(cw-runsWidth(grid), 0) // the lead's and the label's
	lead = cutRuns(lead, room)
	label = cutRuns(label, room-runsWidth(lead))
	lw := max(room-runsWidth(lead)-runsWidth(label)-1, 0) // a blank parts it from the grid
	switch r.kind {
	case treeProject:
		look := styleHead
		switch {
		case r.current:
			look = styleTitle
		case r.inactive:
			look = styleFaint
		}
		label = append(label, cellRun{fitLabel(r.slug, lw), look})
	case treeCoordinator:
		look := stylePlain
		if r.state == "" || r.dormant {
			look = styleFaint
		}
		label = append(label, cellRun{fitLabel("coordinator", lw), look})
	default:
		look := stylePlain
		if r.dormant {
			look = styleFaint
		}
		label = append(label, cellRun{threadLabel(r.name, r.title, lw), look})
	}
	label = append(label, cellRun{strings.Repeat(" ", room-runsWidth(lead)-runsWidth(label)), stylePlain})
	rest := append(label, grid...)
	if r.here {
		return fit(runsRender(lead)+sel.Render(fit(runsPlain(rest), cw-runsWidth(lead))), cw)
	}
	return fit(runsRender(lead)+runsRender(rest), cw)
}

// fitLabel is fit, blank where not even a letter and "…" fit.
func fitLabel(s string, w int) string {
	if w < 2 && ansi.StringWidth(s) > w {
		return strings.Repeat(" ", max(w, 0))
	}
	return fit(s, w)
}

// treeGrid is the grid on the right of a full sidebar's row: count/%,
// PR/CI, state, remote, paused, inactive, needs you (treeCells).
func treeGrid(r treeRow) []cellRun {
	i := ic()
	col := func(on bool, g string, st lipgloss.Style) cellRun {
		if !on || g == "" {
			return cellRun{"  ", stylePlain}
		}
		return cellRun{g + " ", st}
	}
	num, nst := "", styleFaint
	switch r.kind {
	case treeProject:
		n := r.threads
		if r.inactive {
			n = 0 // nothing of it runs
		}
		num = fmt.Sprint(n)
		if n > 0 {
			nst = stylePlain
		}
	case treeCoordinator:
		if r.ctx >= 0 && !r.dormant {
			num = fmt.Sprintf("%d%%", r.ctx)
			switch {
			case r.ctx >= 80:
				nst = styleBad
			case r.ctxHint:
				nst = styleWarn
			}
		}
	default:
		if r.pct >= 0 {
			num = fmt.Sprintf("%d%%", r.pct)
		}
	}
	out := []cellRun{{fmt.Sprintf("%*s ", pctCol-1, num), nst}}
	thread, proj := r.kind == treeThread, r.kind == treeProject
	switch {
	case thread && r.ci == "pass":
		out = append(out, col(true, i.ciPass, styleGood))
	case thread && r.ci == "fail":
		out = append(out, col(true, i.ciFail, styleBad))
	case thread && r.ci == "pending":
		out = append(out, col(true, i.ciRun, styleFaint))
	default:
		out = append(out, col(false, "", stylePlain))
	}
	var g string
	var st lipgloss.Style
	switch {
	case proj && (!r.folded() || r.inactive):
	case r.dormant:
		g, st = i.none, styleFaint
	case thread:
		if g, st = stateLook(r.state); g == "" {
			g, st = i.none, styleFaint
		}
	default:
		g, st = coordLook(r.state)
	}
	return append(out,
		col(g != "", g, st),
		col(r.kind == treeCoordinator && r.remote && !r.dormant, i.remote, styleAccent),
		col(proj && r.paused, i.paused, styleWarn),
		col(proj && r.inactive || r.dormant, i.inactive, styleFaint),
		col(r.hint, i.hint, styleWarn))
}

// slimCells is a project's row in the slim strip, cw cells wide: its
// state column, its name, its needs-you column.
func slimCells(r treeRow, cw int, sel lipgloss.Style) string {
	i := ic()
	g, st := coordLook(r.state)
	if r.inactive {
		g, st = i.inactive, styleFaint
	}
	hint := cellRun{"  ", stylePlain}
	if r.hint {
		hint = cellRun{i.hint + " ", styleWarn}
	}
	look := styleHead
	switch {
	case r.current:
		look = styleTitle
	case r.inactive:
		look = styleFaint
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
	st lipgloss.Style
}

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

// sideHitAt is what column x, row y of a sidebar w×h holds: the border,
// or a tree row.
func sideHitAt(all []treeRow, w, h, x, y int) (r treeRow, ok, border bool) {
	if x == w-1 {
		return r, false, true
	}
	rows := shownRows(all, w)
	i := sideTop(rows, h) + y - 1
	if y < 1 || x < 0 || x >= w-1 || i >= len(rows) {
		return r, false, false
	}
	return rows[i], true, false
}

// Target is where a click on the sidebar goes: a project's dashboard, its
// coordinator (started when none runs), or a thread's session.
type Target struct {
	Project string
	// Coordinator opens the project's coordinator; Session shows that
	// session; neither shows the project's dashboard.
	Coordinator bool
	Session     string
	// Activate activates the project first: its coordinator's row in
	// the tree of an inactive project the user expanded (§4, §5.1).
	Activate bool
}

// target is where a click on r goes; ok is false for a thread without a
// running session, with why.
func (r treeRow) target() (t Target, ok bool, why string) {
	t.Project = r.slug
	switch r.kind {
	case treeCoordinator:
		t.Coordinator, t.Activate = true, r.dormant
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
// anything. ok is false on the row you are on, which already shows.
func (r treeRow) preview() (t Target, ok bool) {
	if r.here {
		return t, false
	}
	t.Project = r.slug
	if r.kind != treeProject && r.session != "" {
		t.Session = r.session
	}
	return t, true
}

// OpenTarget opens t in the view vc: shows the project's dashboard, or
// attaches the coordinator (started first, by the project's
// coordinator_agent, when none runs)
// or the session. A bare view's console hands over to view main with it
// (docs/SPEC.md §3.3); a full one opens it in place.
func OpenTarget(p server.Paths, vc *ViewConn, t Target) error {
	switch {
	case t.Session != "":
		_, err := vc.Do(proto.MethodViewAttach, proto.ViewParams{Session: t.Session, Project: t.Project})
		return err
	case t.Coordinator:
		c, err := server.Connect(p, true)
		if err != nil {
			return err
		}
		if t.Activate {
			if _, err := ActivateProject(c.Call, caller.FromEnv(), t.Project, true); err != nil {
				c.Close()
				return err
			}
		}
		v := vc.View()
		cols, rows := int(v.Cols), int(v.Rows)
		if cols == 0 || rows == 0 {
			cols, rows = 80, 24
		}
		area := v.Lay(cols, rows).Area
		id, err := OpenCoordinator(c.Call, t.Project, "", area.W, area.H)
		c.Close()
		if err != nil {
			return err
		}
		_, err = vc.Do(proto.MethodViewAttach, proto.ViewParams{Session: id, Project: t.Project})
		return err
	}
	_, err := vc.Do(proto.MethodViewProject, proto.ViewParams{Project: t.Project})
	return err
}

// loadSideProjects reads the projects and their open threads, with their
// STATUS.md and their PRs' checks (the ticker's), from the project folders:
// what the attach client polls for
// its sidebar.
func loadSideProjects(paths server.Paths) []ProjectData {
	list, _ := project.List()
	out := make([]ProjectData, 0, len(list))
	for _, sum := range list {
		if sum.Safety != nil && sum.Safety.Archived {
			continue
		}
		pd := ProjectData{Slug: sum.Slug, Safety: sum.Safety}
		if p, err := project.Open(sum.Slug); err == nil {
			recs, _ := thread.List(p)
			prs := ticker.PRs(ticker.StatePath(paths.Sessions), sum.Slug)
			for _, r := range recs {
				if r.State == thread.Resolved {
					continue
				}
				tr := ThreadRow{Record: r, Checks: prs[r.ID].Checks, PR: prs[r.ID].Number}
				tr.Status, _ = thread.ReadStatus(p, r.ID)
				pd.Threads = append(pd.Threads, tr)
			}
		}
		out = append(out, pd)
	}
	return out
}

// sidebar is the attach client's sidebar: what it shows. Its layout and
// tree state are the view's. Guarded by client.mu.
type sidebar struct {
	uiFile   string
	projects []ProjectData
	sessions []proto.SessionInfo
	ctxHint  int        // [ui] context_hint
	server   ServerInfo // the running server
	drag     bool       // the mouse is moving its border
	drawn    []string   // the lines on screen; nil repaints them all
}

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
	rows := buildTree(c.side.projects, c.side.sessions, treeIn{current: c.sideCurrent(), focus: focus, ctxHint: c.side.ctxHint, expanded: c.v.Expanded})
	if c.kb == areaSide {
		markCursor(rows, c.sideW, c.v.SideSel)
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
	op, ok := sideOp(name)
	if name == "tab" {
		c.flash = ""
		c.mu.Unlock()
		c.nextArea()
		return
	}
	if !ok || c.side == nil {
		c.flash = ""
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	st := sideKeyStep(c.sideTree(), c.sideW, c.v.SideSel, op)
	c.flash = st.msg
	if st.help {
		// A plain key that is a prefix command does what it does.
		do := prefixStep(c.prefix, true, k, c.dashboard)
		c.status()
		c.mu.Unlock()
		c.run(do)
		return
	}
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
	if st.expand != "" {
		c.act(proto.MethodViewExpand, proto.ViewParams{Project: st.expand, On: st.expandOn})
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
		for _, r := range shownRows(c.sideTree(), c.sideW) {
			if r.key() == sel {
				t, ok = r.preview()
				break
			}
		}
	}
	c.status()
	c.mu.Unlock()
	c.poke()
	if ok {
		c.sideGo(t)
	}
}

// expandLabel is an inactive project row's menu item that expands or
// collapses it, and its key.
func expandLabel(r treeRow) (label, key string) {
	if r.expanded {
		return "collapse", "←"
	}
	return "expand", "→"
}

// coordLabel is a coordinator row's menu item that opens it: starting a
// dormant one activates its project.
func coordLabel(r treeRow) string {
	if r.dormant {
		return "start the coordinator"
	}
	return "open the coordinator"
}

// activeLabel is the menu item of a project row that space does.
func activeLabel(r treeRow) string {
	if r.inactive {
		return "activate"
	}
	return "deactivate"
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
	lines := sidebarLines(rows, c.sideW, c.sideRows(), sideFooter(rows, c.sideW, c.side.server, true))
	for y, l := range lines {
		if y < len(c.side.drawn) && c.side.drawn[y] == l {
			continue
		}
		b = append(b, fmt.Sprintf("\x1b[%d;1H\x1b[0m", y+1)...)
		b = append(b, l...)
		wrote = true
	}
	c.side.drawn = lines
	return b, wrote
}

// sideRows is the sidebar's height in a session: the window less the
// status bar and the rule above it, which run under it too. c.mu held.
func (c *client) sideRows() int {
	if c.statusBar {
		return max(c.rows-2, 1)
	}
	return c.rows
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
		l := c.v.Sidebar.DragTo(m.X, c.viewCols())
		same := l == c.v.Sidebar
		c.mu.Unlock()
		if !same {
			c.setSidebar(l, false)
		}
		c.poke()
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
	r, ok, border := sideHitAt(c.sideTree(), c.sideW, c.sideRows(), m.X, m.Y)
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
	open := false
	switch {
	case border:
		sd.drag = true
	case !ok:
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

// The sidebar's keyboard (docs/SPEC.md §4): tab on the dashboard, or
// prefix+tab in a session, gives it the focus; these keys then move its
// cursor (the view's SideSel), and the view previews what the cursor
// rests on (previewDelay) while the keyboard stays here; enter or →
// takes it into the pane. One table drives the keys and the help.

// Sidebar key operations.
const (
	sideUp = iota
	sideDown
	sideIn     // a folded row unfolds; any other goes in, as sideEnter
	sideOut    // an unfolded row folds; a row under a project goes up to it
	sideEnter  // into the pane: the keyboard follows
	sideBack   // the focus goes back to the list or the pane
	sideActive // on a project, activate or deactivate it
	sideHelp   // the help, as prefix+?
)

// sideAction is a sidebar key: its keys, what it does, its help line and
// the mouse's way to the same thing (TestEverySidebarClickHasKey).
type sideAction struct {
	keys        []string
	op          int
	label, help string
}

var sideActions = []sideAction{
	{[]string{"up", "k"}, sideUp, "↑ ↓ k j", "move through the tree; the pane shows what the cursor rests on (a project its dashboard, a coordinator or a thread its session, live), the keyboard stays in the sidebar, and nothing starts"},
	{[]string{"down", "j"}, sideDown, "", ""},
	{[]string{"enter"}, sideEnter, "enter", "into the pane: a project's dashboard or a session gets the keyboard; a coordinator not running starts, and an inactive project's starts activating the project"},
	{[]string{"right", "l"}, sideIn, "→ ←", "→ unfolds a folded project, else goes into the pane as enter; ← folds an unfolded one, or from a row under a project goes up to it. Unfolding an inactive project never activates it"},
	{[]string{"left", "h"}, sideOut, "", ""},
	{[]string{"esc"}, sideBack, "esc", "back to the list (in a session: to the pane)"},
	{[]string{"space"}, sideActive, "space", "on a project: activate it, or deactivate it (its coordinator and threads stop, to resume when you activate it; asks first while they run)"},
	{[]string{"?"}, sideHelp, "?", "help, as prefix+?"},
}

// sideOp is the sidebar operation of key; ok is false for none.
func sideOp(key string) (op int, ok bool) {
	for _, a := range sideActions {
		if slices.Contains(a.keys, key) {
			return a.op, true
		}
	}
	return 0, false
}

// sideStep is what a sidebar key does to the tree: move the cursor to
// sel, go to a row's target, or say msg.
type sideStep struct {
	sel    string
	target *Target
	here   bool // target is the row you are on
	// into: the keyboard goes into the pane (enter, →), to the target
	// once it shows.
	into bool
	msg  string
	back bool
	help bool
	// toggle is the project to activate (activate) or deactivate.
	toggle   string
	activate bool
	// expand is the inactive project to expand (expandOn) or collapse.
	expand   string
	expandOn bool
}

// sideKeyStep decides what sidebar key op does with the cursor at sel in
// the tree all, drawn w columns wide.
func sideKeyStep(all []treeRow, w int, sel string, op int) sideStep {
	switch op {
	case sideBack:
		return sideStep{back: true}
	case sideHelp:
		return sideStep{help: true}
	}
	rows := shownRows(all, w)
	i := sideCursor(rows, sel)
	if i < 0 {
		return sideStep{msg: "no projects"}
	}
	r := rows[i]
	moveTo := func(j int) sideStep {
		if j < 0 || j >= len(rows) || j == i {
			return sideStep{}
		}
		return sideStep{sel: rows[j].key()}
	}
	switch op {
	case sideUp:
		return moveTo(i - 1)
	case sideDown:
		return moveTo(i + 1)
	case sideIn:
		if r.folded() && w > sideSlim {
			// Expanding never activates (§5.1).
			return sideStep{expand: r.slug, expandOn: true, sel: "c:" + r.slug}
		}
		return enterStep(r)
	case sideOut:
		if r.kind == treeProject && r.expanded {
			return sideStep{expand: r.slug}
		}
		for j := i - 1; j >= 0 && r.kind != treeProject; j-- {
			if rows[j].kind == treeProject {
				return moveTo(j)
			}
		}
	case sideActive:
		if r.kind != treeProject {
			return sideStep{msg: "space on a project activates or deactivates it"}
		}
		return sideStep{toggle: r.slug, activate: r.inactive}
	case sideEnter:
		return enterStep(r)
	}
	return sideStep{}
}

// enterStep is enter on r: into the pane, opening r's target first
// unless it already shows.
func enterStep(r treeRow) sideStep {
	t, can, why := r.target()
	if !can {
		return sideStep{msg: why}
	}
	return sideStep{target: &t, here: r.here, into: true}
}

// sideFooter is the full sidebar's footer, below its tree (docs/SPEC.md
// §4): a faint rule, two lines about the highlighted row (the cursor's
// while the sidebar has the keyboard, else the one you are on; details),
// its key bar (sideKeyBar), and the hint that an update is out when
// there is one. session: drawn beside a session, where prefix+tab gives
// the sidebar the keyboard (tab on the dashboard). None in the slim
// strip. Each line is w-1-sideGap cells wide, styled.
func sideFooter(all []treeRow, w int, s ServerInfo, session bool) []string {
	cw := w - 1 - sideGap
	if w <= sideSlim || cw < 4 {
		return nil
	}
	var r *treeRow
	focused := false
	for i := range all {
		if all[i].cursor {
			r, focused = &all[i], true
			break
		}
		if all[i].here && r == nil {
			r = &all[i]
		}
	}
	d1, d2 := "", ""
	if r != nil {
		d1, d2 = r.details()
	}
	out := []string{
		styleFaint.Render(" " + strings.Repeat("─", cw-1)),
		styleHead.Render(fit(" "+d1, cw)),
		styleFaint.Render(fit(" "+d2, cw)),
		sideKeyBar(r, focused, session, cw),
	}
	if h := versionHint(s); h != "" {
		if ansi.StringWidth(h) > cw-1 {
			h = versionHintShort(s)
		}
		out = append(out, styleFaint.Render(fit(" "+h, cw)))
	}
	return out
}

// details are the footer's two lines about r: its full name and state,
// then what goes with it (the coordinator's context, a thread's
// progress, its PR's number and checks).
func (r treeRow) details() (string, string) {
	running := func(state string) string { return cmp.Or(state, "not running") }
	switch r.kind {
	case treeProject:
		bits := []string{r.slug, "active"}
		if r.inactive {
			bits[1] = "inactive"
		}
		if r.paused {
			bits = append(bits, "paused")
		}
		if r.inactive {
			return strings.Join(bits, " · "), "previewing it starts nothing"
		}
		return strings.Join(bits, " · "), fmt.Sprintf("%s · coordinator %s", counted(r.threads, "thread"), running(r.state))
	case treeCoordinator:
		if r.dormant {
			return r.slug + " coordinator · dormant", "enter starts it and activates " + r.slug
		}
		var bits []string
		if r.ctx >= 0 {
			bits = append(bits, fmt.Sprintf("context %d%%", r.ctx))
		}
		if r.remote {
			bits = append(bits, "remote control on")
		}
		if r.asks > 0 {
			bits = append(bits, counted(r.asks, "question")+" waiting")
		}
		if r.state == "" {
			bits = append(bits, "enter starts it")
		}
		return r.slug + " coordinator · " + running(r.state), strings.Join(bits, " · ")
	}
	l1 := r.thread + " · ad hoc"
	switch {
	case r.name != r.thread:
		l1 = strings.TrimSpace(r.name+" "+r.title) + " (" + r.thread + ")"
	case r.title != "":
		l1 = r.thread + " " + r.title
	}
	bits := []string{running(r.state)}
	if r.dormant {
		bits[0] = "dormant"
	}
	if r.pct >= 0 {
		bits = append(bits, fmt.Sprintf("%d%%", r.pct))
	}
	if r.pr > 0 {
		bits = append(bits, fmt.Sprintf("PR #%d", r.pr))
		if w := map[string]string{"pass": "passes", "fail": "fails", "pending": "running"}[r.ci]; w != "" {
			bits = append(bits, "CI "+w)
		}
	}
	return l1, strings.Join(bits, " · ")
}

// counted is "1 thread", "2 threads".
func counted(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// sideKeyBar is the footer's key bar, cw cells wide: the keys that do
// something on r while the sidebar has the keyboard (focused), else the
// way to give it the keyboard. "? help" gives way first when they don't
// fit.
func sideKeyBar(r *treeRow, focused, session bool, cw int) string {
	var keys []string
	switch {
	case !focused && session:
		keys = []string{"prefix+tab sidebar", "prefix+? help"}
	case !focused:
		keys = []string{"tab sidebar", "? help"}
	case r == nil:
	case r.kind == treeProject && r.inactive:
		in := "→ unfold"
		if !r.folded() {
			in = "← fold"
		}
		keys = []string{"⏎ open", in, "␣ activate"}
	case r.kind == treeProject:
		keys = []string{"⏎ open", "␣ deactivate"}
	case r.kind == treeCoordinator && r.dormant:
		keys = []string{"⏎ start", "← project"}
	case r.kind == treeCoordinator && r.state == "":
		keys = []string{"⏎ start", "← project"}
	case r.kind == treeCoordinator, r.session != "":
		keys = []string{"⏎ attach", "← project"}
	default:
		keys = []string{"← project"}
	}
	if focused {
		keys = append(keys, "? help")
	}
	for len(keys) > 1 && ansi.StringWidth(" "+strings.Join(keys, " · ")) > cw {
		keys = keys[:len(keys)-1]
	}
	return fit(" "+keysLine(strings.Join(keys, " · ")), cw)
}

// pctCol is the width of the grid's count/% column: "100% ".
const pctCol = 5

// sideGrid is the width of the grid on the right of every row of the
// full sidebar: the count/% column, then six glyph columns two cells
// wide (treeCells).
const sideGrid = pctCol + 6*2

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
