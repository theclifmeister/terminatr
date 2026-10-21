package tui

import (
	"slices"

	"github.com/theclifmeister/terminatr/internal/view"
)

// The sidebar's keyboard (docs/SPEC.md §4): tab on the dashboard, or
// prefix+tab in a session, gives it the focus; these keys then move its
// cursor (the view's SideSel), and the view previews what the cursor
// rests on (previewDelay) while the keyboard stays here; enter or →
// takes it into the pane. One table drives the keys and the help (sideExtras adds its own).

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
	sideFoldAll
	sideUnfoldAll
	sideFoldToggle // fold all while any is open, else unfold all
	sidePin        // on a project, pin it to the top or unpin it
	// sideUnfoldOnly is side.unfold from an unshifted letter a keymap
	// gave it (vim-keys' l): it unfolds, and never goes in, so typed
	// text can't open a pane or start a coordinator.
	sideUnfoldOnly
)

// sideAction is a sidebar key: its keys, what it does, its help line and
// the mouse's way to the same thing (TestEverySidebarClickHasKey).
type sideAction struct {
	// id is its stable name and keys its default keys, as an action's
	// (actions.go); fixed ones can't be rebound (keybind.go).
	id          string
	keys        []string
	op          int
	label, help string
	fixed       bool
}

var sideActions = []sideAction{
	{id: "side.up", keys: []string{"up"}, op: sideUp, label: "↑ ↓", help: "move through the tree and the PROJECTS header; the pane shows what the cursor rests on (a project its dashboard, a coordinator or a thread its session, live; the header every project's NEEDS YOU), the keyboard stays in the sidebar, and nothing starts"},
	{id: "side.down", keys: []string{"down"}, op: sideDown},
	{id: "side.enter", keys: []string{"enter"}, op: sideEnter, label: "enter", help: "into the pane: a project's dashboard or a session gets the keyboard; a coordinator not running starts, and an inactive project's starts activating the project; on the PROJECTS header, every project's NEEDS YOU, labelled; on the inactive divider it folds or unfolds; on a mod's section it opens the view the mod names, else folds or unfolds it"},
	{id: "side.unfold", keys: []string{"right"}, op: sideIn, label: "→ ←", help: "→ unfolds a folded project, the inactive divider or a mod's section, else goes into the pane as enter; ← folds an unfolded one, or from a row under a project goes up to it. Folding never activates or deactivates"},
	{id: "side.fold", keys: []string{"left"}, op: sideOut},
	{id: "side.back", keys: []string{"esc"}, op: sideBack, label: "esc", help: "back to the list (in a session: to the pane)", fixed: true},
	{id: "side.activate", keys: []string{"S"}, op: sideActive, label: "S", help: "on a project: activate it, or deactivate it (its coordinator and threads stop, to resume when you activate it; asks first while they run)"},
	{id: "side.fold-toggle", keys: []string{"Z"}, op: sideFoldToggle, label: "Z", help: "fold every project, the inactive divider and the mods' sections while any is open, else unfold them all"},
	// The toggle's two halves, kept bindable with no key of their own.
	{id: "side.fold-all", op: sideFoldAll},
	{id: "side.unfold-all", op: sideUnfoldAll},
	{id: "side.pin", keys: []string{"P"}, op: sidePin, label: "P", help: "on a project: pin it to the top of the active projects, or unpin it (its order only)"},
	{id: "side.first", keys: []string{"home"}, op: sideFirst, label: "home end", help: "the first / last row"},
	{id: "side.last", keys: []string{"end"}, op: sideLast},
	{id: "side.page-up", keys: []string{"pgup"}, op: sidePageUp, label: "pgup pgdown", help: "a page up / down"},
	{id: "side.page-down", keys: []string{"pgdown"}, op: sidePageDown},
	{id: "side.nth", keys: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, op: sideNth, label: "1-9", help: "the first to ninth project, counted down the tree", fixed: true},
	{id: "side.needs-next", keys: []string{"W"}, op: sideNeedNext, label: "W", help: "the next row that needs you, round the ends; it unfolds what hides the row, and clears a filter that does"},
	// The previous one, kept bindable with no key: W goes round the ends.
	{id: "side.needs-prev", op: sideNeedPrev},
	{id: "side.filter", keys: []string{"/"}, op: sideFilter, label: "/", help: "filter: type to narrow the tree to what matches (a project's name, a thread's task, title or id) and the projects over it; enter keeps the filter, esc clears it"},
	{id: "side.help", keys: []string{"?"}, op: sideHelp, label: "?", help: "help, as prefix+?"},
}

// sideOp is the sidebar operation of key; ok is false for none.
func sideOp(key string) (op int, ok bool) {
	for _, a := range sideActions {
		if runsAs(a.id, a.keys, key) {
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
	// refold changes what the tree folds (the view's, and ui.json's).
	refold func(*view.Tree)
	// pin is the project to pin (pinOn) or unpin.
	pin   string
	pinOn bool
	// sections are the band's sections to fold (foldOn) or unfold, by
	// id (view.sections); open the one whose view enter opens.
	sections []string
	foldOn   bool
	open     *sideSection
	// everySection: unfold every section the view folds too (Z), those
	// of the projects it folded among them, which have no rows.
	everySection bool
}

// foldStep is what → (sideIn), ← (sideOut) and enter (sideEnter) do to
// the tree's folds with the cursor on rows[i]: → unfolds a folded
// project or the divider, ← folds an unfolded one (on a folded inactive
// project, it goes up to the divider), enter folds or unfolds the
// divider. ok is false where the key does something else. The slim
// strip shows no rows under a project: there only the divider folds.
func foldStep(rows []treeRow, i, op int, slim bool) (st sideStep, ok bool) {
	r := rows[i]
	if r.kind == treeServer || r.isPeer() {
		return peerFoldStep(rows, i, op, slim)
	}
	switch {
	case r.kind == treeDivider && (op == sideIn && r.fold || op == sideOut && !r.fold || op == sideEnter):
		return sideStep{refold: foldDivider(r)}, true
	case r.kind != treeProject || slim:
	case op == sideIn && r.fold, op == sideOut && !r.fold:
		return sideStep{refold: foldProject(r, !r.fold)}, true
	case op == sideOut && r.inactive:
		return sideStep{sel: dividerKey}, true
	}
	return sideStep{}, false
}

// foldProject folds (fold) or unfolds project row r: an active project
// is folded in the view's tree, an inactive one expanded. Neither
// activates or deactivates it (§5.1).
func foldProject(r treeRow, fold bool) func(*view.Tree) {
	slug := r.slug
	if r.inactive {
		return func(t *view.Tree) { t.Expand(slug, !fold) }
	}
	return func(t *view.Tree) { t.Fold(slug, fold) }
}

// anyOpen says whether anything in the tree all that folds is open: a
// project, an expanded inactive one, the divider, a paired server or a
// mod's section. Z folds all then, else unfolds all.
func anyOpen(all []treeRow) bool {
	for _, r := range all {
		switch r.kind {
		case treeProject:
			if r.inactive && r.expanded || !r.inactive && !r.fold {
				return true
			}
		case treeDivider, treeServer, treePeerProject:
			if !r.fold {
				return true
			}
		case treeSection:
			if !r.sec.folded {
				return true
			}
		}
	}
	return false
}

// foldAll is Z's fold (fold) and unfold: the fold folds every active
// project and the divider, and collapses the inactive projects; the
// unfold opens them all,
// the inactive projects expanded under the open divider.
func foldAll(all []treeRow, fold bool) func(*view.Tree) {
	var active, inactive []string
	for _, r := range all {
		switch {
		case r.kind == treeProject && !r.inactive:
			active = append(active, r.slug)
		case r.kind == treeDivider:
			inactive = r.slugs
		}
	}
	active = append(active, peerFolds(all)...)
	active, inactive = active[:min(len(active), view.MaxExpanded)], inactive[:min(len(inactive), view.MaxExpanded)]
	// The sections' folds are view.sections' (foldSections).
	if fold {
		return func(t *view.Tree) { *t = view.Tree{Folded: active, SectionsFolded: t.SectionsFolded} }
	}
	return func(t *view.Tree) {
		*t = view.Tree{Expanded: inactive, InactiveOpen: true, SectionsFolded: t.SectionsFolded}
	}
}

// sideKeyStep decides what sidebar key op does with the cursor at sel in
// the tree all, drawn w columns wide.
func sideKeyStep(all []treeRow, w int, sel string, op int) sideStep {
	onlyUnfold := op == sideUnfoldOnly
	if onlyUnfold {
		op = sideIn
	}
	switch op {
	case sideFoldToggle:
		op = sideUnfoldAll
		if anyOpen(all) {
			op = sideFoldAll
		}
	case sideBack:
		return sideStep{back: true}
	case sideHelp:
		return sideStep{help: true}
	}
	if sel == headKey {
		if onlyUnfold {
			return sideStep{}
		}
		return headStep(all, w, op)
	}
	rows, i := cursorAt(all, w, sel)
	if i < 0 {
		return sideStep{msg: "no projects"}
	}
	r := rows[i]
	if r.kind == treeSection && r.sec.proj && r.sec.folded && op == sideOut {
		// ← on a project's folded section: up to its project, as on a
		// thread.
		if j := slices.IndexFunc(rows, func(x treeRow) bool { return x.kind == treeProject && x.slug == r.slug }); j >= 0 {
			return sideStep{sel: rows[j].key()}
		}
	}
	if r.kind == treeSection {
		if st, ok := sectionStep(r, op); ok {
			return st
		}
	}
	if st, ok := foldStep(rows, i, op, w <= sideSlim); ok {
		return st
	}
	if onlyUnfold {
		return sideStep{}
	}
	moveTo := func(j int) sideStep {
		if j < 0 || j >= len(rows) || j == i {
			return sideStep{}
		}
		return sideStep{sel: rows[j].key()}
	}
	switch op {
	case sideUp:
		// Round the ends, the PROJECTS header among the rows: the
		// sidebar only, the list stops at them.
		return cursorStep(rows, r.key(), -1)
	case sideDown:
		return cursorStep(rows, r.key(), 1)
	case sideIn:
		if r.kind == treeDivider {
			return sideStep{}
		}
		return enterStep(r)
	case sideOut:
		for j := i - 1; j >= 0 && (r.kind == treeCoordinator || r.kind == treeThread); j-- {
			if rows[j].kind == treeProject {
				return moveTo(j)
			}
		}
		for j := i - 1; j >= 0 && (r.kind == treePeerCoord || r.kind == treePeerThread); j-- {
			if rows[j].kind == treePeerProject {
				return moveTo(j)
			}
		}
	case sideActive:
		switch r.kind {
		case treeProject:
			return sideStep{toggle: r.slug, activate: r.inactive}
		case treeDivider: // S keeps one meaning
			return sideStep{}
		}
		return sideStep{msg: barKey("side.activate", "S") + " on a project activates or deactivates it"}
	case sidePin:
		if r.kind != treeProject {
			return sideStep{msg: barKey("side.pin", "P") + " on a project pins it to the top"}
		}
		return sideStep{pin: r.slug, pinOn: !r.pinned}
	case sideFoldAll, sideUnfoldAll:
		// The sections too: view.tree, then view.sections.
		st := sideStep{refold: foldAll(all, op == sideFoldAll), sections: sectionIDs(all), foldOn: op == sideFoldAll,
			everySection: op == sideUnfoldAll}
		// The cursor moves onto what still shows: its project's row, or
		// the divider.
		switch {
		case op == sideUnfoldAll:
		case r.kind == treeSection && r.sec.proj:
			st.sel = "p:" + r.slug
		case r.kind == treeSection:
		case r.inactive || r.dormant:
			st.sel = dividerKey
		case r.kind != treeProject && r.kind != treeDivider:
			st.sel = "p:" + r.slug
		}
		return st
	case sideEnter:
		return enterStep(r)
	}
	return sideStep{}
}

// headStep is what sidebar key op does with the cursor on the PROJECTS
// header: ↑ ↓ go on through the rows; enter and → show every project's
// NEEDS YOU, the keyboard following into the list; Z folds or unfolds
// everything, the cursor staying there; ← does nothing, and S and P say
// what they are for.
func headStep(all []treeRow, w, op int) sideStep {
	rows, _ := cursorAt(all, w, "")
	switch op {
	case sideIn, sideEnter:
		return enterStep(headRow(false))
	case sideUp:
		return cursorStep(rows, headKey, -1)
	case sideDown:
		return cursorStep(rows, headKey, 1)
	case sideActive:
		return sideStep{msg: barKey("side.activate", "S") + " on a project activates or deactivates it"}
	case sidePin:
		return sideStep{msg: barKey("side.pin", "P") + " on a project pins it to the top"}
	case sideFoldAll, sideUnfoldAll:
		return sideStep{refold: foldAll(all, op == sideFoldAll), sections: sectionIDs(all), foldOn: op == sideFoldAll,
			everySection: op == sideUnfoldAll}
	}
	return sideStep{}
}

// cursorStep moves the cursor from the row keyed sel by d (↑ -1, ↓ 1),
// round the ends, through the rows and the PROJECTS header at its place:
// under the band above the tree, over the tree.
func cursorStep(rows []treeRow, sel string, d int) sideStep {
	above := 0
	for above < len(rows) && rows[above].kind == treeSection && rows[above].sec.above {
		above++
	}
	keys := make([]string, 0, len(rows)+1)
	for _, r := range rows[:above] {
		keys = append(keys, r.key())
	}
	keys = append(keys, headKey)
	for _, r := range rows[above:] {
		keys = append(keys, r.key())
	}
	i := slices.Index(keys, sel)
	if i < 0 {
		return sideStep{}
	}
	if j := (i + d + len(keys)) % len(keys); j != i {
		return sideStep{sel: keys[j]}
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
