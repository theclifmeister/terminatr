package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/caller"
	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The projects sidebar (docs/SPEC.md §4): a column on the left of every
// screen, the dashboard and the attach view alike, holding the project
// tree. Each project row has its coordinator's state glyph, a hint when
// one of its threads is blocked or waiting or one of its tasks needs
// you (review or blocked), and its count of open threads. The active
// projects come first, the pinned ones at the top, then a divider,
// "inactive (n)", the inactive projects under it (docs/SPEC.md §5.1),
// folded by default. An active project unfolded, as it starts, has its
// coordinator under it and its open threads under the coordinator, on
// tree connectors (├─ └─), with their state glyphs and progress in
// columns of their own; an inactive one is folded, its row alone. The
// view keeps what is folded (view.Tree). The current project is in
// the accent colour, and the row you are on is highlighted. A click on a project shows its dashboard, on the
// coordinator attaches to it, on a thread attaches its session. Its border column
// can be dragged, and { } b change it from the keys.
//
// Its state (width, slim strip, keyboard row) is part of the
// server-owned view; the rest here only draws and hit-tests: the tree
// and its cells are sidetree.go's, the keys sidekeys.go's, the footer
// sidefoot.go's, and the attach view's handling attachside.go's.

// SidebarLayout is the sidebar's layout: part of the server-owned view
// (docs/SPEC.md §3.3), and kept in ui.json as the layout new views start
// with.
type SidebarLayout = view.Sidebar

const (
	sideDefault = view.SideDefault
	sideSlim    = view.SideSlim
	sideRoom    = view.SideRoom
)

// sideHeadIn is how the sidebar's PROJECTS header is drawn: needs, the
// things that need you its ⚑ N counts; cursor, the keyboard's row is the
// header (headKey); here, every project's NEEDS YOU shows (allProjects),
// as the row you are on.
type sideHeadIn struct {
	needs        int
	cursor, here bool
}

// sideLines draws the sidebar w columns wide (its border included) and
// h rows tall: a header with the count of projects, then the tree, then
// the footer's lines (sideFooter) at the bottom; the band of mod
// sections below the tree, and above the header those a layout puts
// above it. While a section shows above the tree or a row with ⚑ is
// scrolled off, the header also says ⚑ N in the warning colour: needs
// things that need you, as NEEDS YOU counts them. While
// it has the keyboard focus (a row is the cursor) its border is in the
// accent colour and the cursor's row is highlighted instead of the one
// you are on. Its border is its last column, its first on the right
// (right): the rest is the same either way.
func sideLines(all []treeRow, w, h int, head sideHeadIn, foot []string, right bool) []string {
	if w <= 0 {
		return make([]string, max(h, 0))
	}
	cw := max(w-1, 0) // less the border
	slim := w <= sideSlim
	// Every full row leaves a column before the border (sideGap); in the
	// slim strip the needs-you column's blank cell does.
	gap := sideGap
	if slim {
		gap = 0
	}
	if len(foot) > 0 && h < len(foot)+4 {
		foot = nil // no room: the tree comes first
	}
	lay := sideLayout(all, w, h, len(foot))
	h -= len(foot)
	focused := head.cursor || slices.ContainsFunc(all, func(r treeRow) bool { return r.cursor })
	border := th().Faint.Render("│")
	if focused {
		border = th().Accent.Render("│")
	}
	edge := func(s string) string {
		if right {
			return border + s
		}
		return s + border
	}
	out := make([]string, 0, h)
	for _, s := range lay.above {
		out = append(out, edge(lay.line(s, cw, slim, focused)+reset))
	}
	n := projectCount(all)
	needs := head.needs
	if !lay.flag {
		needs = 0
	}
	hl := sideHead(n, needs, cw-gap, slim)
	// The header highlighted as a row is (treeSel): the keyboard's while
	// the sidebar has the focus, else while it is the row you are on.
	hr := headRow(head.here)
	hr.cursor = head.cursor
	if sel, look := treeSel(hr, focused); sel {
		hl = look.Render(ansi.Strip(hl))
	}
	out = append(out, edge(hl+strings.Repeat(" ", gap)+reset))
	if l, ok := sideFilterLine(cw - gap); ok {
		out = append(out, edge(l+strings.Repeat(" ", gap)+reset))
	}
	if n == 0 {
		note := " no projects"
		if sideFilterNow().text != "" {
			note = " no match"
		}
		if slim {
			note = " —"
		}
		out = append(out, edge(th().Faint.Render(fit(note, cw))+reset))
	}
	for _, s := range lay.slots {
		if len(out) >= h {
			break
		}
		out = append(out, edge(lay.line(s, cw, slim, focused)+reset))
	}
	for len(out) < h {
		out = append(out, edge(strings.Repeat(" ", cw)))
	}
	out = out[:h]
	for _, f := range foot {
		out = append(out, edge(fit(f, cw-gap)+reset+strings.Repeat(" ", gap)))
	}
	return out
}

// sideHead is the sidebar's header, cw cells wide: " PROJECTS", its
// count of projects on the right, just before the needs-you column, and
// after it, with needs things that need you, ⚑ N (headFlag), for which
// the count gives way; in the slim strip the word alone, cut with "…".
// A click on it (off ⚑ N), or enter with the cursor on it (headKey),
// shows every project's NEEDS YOU (allProjects).
func sideHead(n, needs, cw int, slim bool) string {
	if slim {
		return th().Title.Render(fit(" PROJECTS", cw))
	}
	count := ""
	if n > 0 {
		count = fmt.Sprint(n)
	}
	if lo, _ := headFlag(needs, cw); lo > 0 {
		left := th().Title.Render(" PROJECTS")
		if gap := lo - ansi.StringWidth(" PROJECTS") - len(count) - 2; count != "" && gap >= 1 {
			left += strings.Repeat(" ", gap) + th().Faint.Render(count) + "  "
		} else {
			left += strings.Repeat(" ", lo-ansi.StringWidth(" PROJECTS"))
		}
		return left + needsBadge(needs) + "  "
	}
	gap := cw - ansi.StringWidth(" PROJECTS") - len(count) - 2
	if gap < 1 {
		return th().Title.Render(fit(countLabel(" PROJECTS", n), cw))
	}
	return th().Title.Render(" PROJECTS") + strings.Repeat(" ", gap) + th().Faint.Render(count) + "  "
}

// coordLook is a coordinator's glyph and style: "·" when none runs.
func coordLook(state string) (string, lipgloss.Style) {
	if state == "" {
		return ic().none, th().Faint
	}
	return stateLook(state)
}

// sideGap is the blank column between the sidebar's rows and its border,
// so the state glyphs don't touch it.
const sideGap = 1

// sideHitAt is what column x, row y of a sidebar w×h with a footer of
// foot rows (sideFootRows) holds: the border, or a tree row; a band
// section's header row for its header and its body (body).
func sideHitAt(all []treeRow, w, h, foot, x, y int) (r treeRow, ok, border bool) {
	r, ok, border, _ = sideHit(all, w, h, foot, x, y)
	return r, ok, border
}

// sideHit is sideHitAt, saying whether the row is a section's body line.
func sideHit(all []treeRow, w, h, foot, x, y int) (r treeRow, ok, border, body bool) {
	if x < 0 || w <= 0 {
		return r, false, false, false
	}
	if x == w-1 {
		return r, false, true, false
	}
	lay := sideLayout(all, w, h, foot)
	if y >= 0 && y < len(lay.above) {
		s := lay.above[y]
		return lay.rows[s.row], true, false, s.line >= 0
	}
	i := y - len(lay.above) - lay.head
	if i < 0 || i >= len(lay.slots) || lay.slots[i].row < 0 {
		return r, false, false, false
	}
	s := lay.slots[i]
	return lay.rows[s.row], true, false, s.line >= 0
}

// sideFlagAt says whether column x, row y of a sidebar w×h with a footer
// of foot rows is the header's ⚑ N (sideLines with needs), a click on
// which selects the first row that needs you, as w.
func sideFlagAt(all []treeRow, w, h, foot, needs, x, y int) bool {
	if w <= sideSlim || needs <= 0 || x < 0 || x >= w-1 {
		return false
	}
	lay := sideLayout(all, w, h, foot)
	if !lay.flag || y != len(lay.above) {
		return false
	}
	lo, hi := headFlag(needs, w-1-sideGap)
	return lo > 0 && x >= lo && x < hi
}

// sideHeadAt says whether column x, row y of a sidebar w×h with a footer
// of foot rows is its PROJECTS header (off its ⚑ N, sideFlagAt), a click
// on which shows every project's NEEDS YOU.
func sideHeadAt(all []treeRow, w, h, foot, x, y int) bool {
	if w <= 0 || x < 0 || x >= w-1 {
		return false
	}
	return y == len(sideLayout(all, w, h, foot).above)
}

// sideLocal is window column x of a sidebar w wide from column at, as
// the sidebar's own drawing has it (sideHitAt, sideTip): its border
// last. On the right (right) the border is its first column. -1 off
// the sidebar.
func sideLocal(x, at, w int, right bool) int {
	x -= at
	switch {
	case x < 0 || x >= w:
		return -1
	case !right:
		return x
	case x == 0:
		return w - 1
	}
	return x - 1
}

// Target is where a click on the sidebar goes: a project's dashboard, its
// coordinator (started when none runs), or a thread's session.
type Target struct {
	// Project is the project's slug, or a paired server's project as
	// "@alias/slug" (peerFold), whose read-only dashboard shows
	// (peerdash.go).
	Project string
	// Coordinator opens the project's coordinator; Session shows that
	// session; neither shows the project's dashboard.
	Coordinator bool
	Session     string
	// Activate activates the project first: its coordinator's row in
	// the tree of an inactive project the user expanded (§4, §5.1).
	Activate bool
}

// OpenTarget opens t in the view vc: shows the project's dashboard, or
// attaches the coordinator (started first, by the project's
// coordinator_agent, when none runs)
// or the session. A bare view's console hands over to view main with it
// (docs/SPEC.md §3.3); a full one opens it in place.
func OpenTarget(p tmclient.Paths, vc *ViewConn, t Target) error {
	switch {
	case t.Session != "":
		_, err := vc.Do(proto.MethodViewAttach, proto.ViewParams{Session: t.Session, Project: t.Project})
		return err
	case t.Coordinator:
		c, err := tmclient.Connect(p, true)
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

	// needsFirst is [ui] needs_first: the projects that need you first.
	needsFirst bool
	// compact is the console's layout's rows.density = compact: the
	// footer keeps its key bar alone.
	compact bool
	// order is the console's layout's sidebar.sections: the band's
	// order, those before "tree" above the tree; projOrder its
	// sidebar.project_sections.
	order, projOrder []string
	// sections are the sidebar sections session.list names
	// (mods.sections), band the band's lines (sidesections.go).
	sections []proto.ModSection
	band     sectionCache
	// peers are the paired servers (Data.Peers), for the server groups.
	peers []proto.PeerMirror
}

// pinIn marks project slug of ps pinned (on) or not, until the next
// poll reads its setting.
func pinIn(ps []ProjectData, slug string, on bool) {
	for i := range ps {
		if ps[i].Slug == slug && ps[i].Safety != nil {
			s := *ps[i].Safety
			s.Pinned = on
			ps[i].Safety = &s
		}
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

// foldLabel is the menu item of an active project or the divider that
// folds or unfolds it, and its key.
func foldLabel(r treeRow) (label, key string) {
	if r.fold {
		return "unfold", "→"
	}
	return "fold", "←"
}

// pinLabel is a project row's menu item that p does.
func pinLabel(r treeRow) string {
	if r.pinned {
		return "unpin"
	}
	return "pin to the top"
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

// foldDivider is a click on the inactive divider: it folds or unfolds.
func foldDivider(r treeRow) func(*view.Tree) {
	open := r.fold
	return func(t *view.Tree) { t.InactiveOpen = open }
}
