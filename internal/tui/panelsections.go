package tui

import (
	"slices"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The right pane's mod sections (the T348 design §11; docs/SPEC.md §4,
// Mod views): a coordinator.section in the coordinator's info panel, a
// details.section in the dashboard's details panel and in a thread's
// info panel. Each panel has a head nothing goes above, then core's
// blocks, named (mod.CoordinatorBlocks, mod.DetailsBlocks); the layout's
// coordinator.sections or details.sections places each section just
// above one of them, else at the bottom (mod.PanelSections). A section
// is the band's: a header with its fold mark, title and badge, and its
// body, the mod's tree, read-only. The view keeps the folds under
// panel:mod.view ids. Sections take at most half the panel, their
// headers a quarter, those above or between core's blocks a quarter,
// and give way so that every line core marks as must-see (the head,
// NEEDS YOU, a thread's question) stays on the first screen (fitPanel).
// A panel under panelMin rows draws them all at the bottom.

// panelMin is the fewest rows a panel places sections above or between
// core's blocks in; a shorter one draws them at the bottom.
const panelMin = 12

// panelSec is one of a panel's sections and where it goes: just above
// core's block before, at the bottom for "".
type panelSec struct {
	sec    *sideSection
	before string
}

// panelOpts are a console's own state of a panel's sections: the
// layout's order, the view's folds, the selected header's id, whether
// the panel has the keyboard, its rows on screen (0 for no fitting),
// the mods (which are built in) and safe mode, and the line cache.
type panelOpts struct {
	order   []string
	folds   view.Tree
	sel     string
	focused bool
	rows    int
	mods    []mod.Mod
	noMods  bool
	cache   *sectionCache
}

// panelTarget is what a panel's sections show for: their slot, and the
// project and its task or thread (both "" for the project's own).
type panelTarget struct {
	slot, project, task, thr string
}

// blocks are core's blocks in slot's panels.
func (t panelTarget) blocks() []string {
	if t.slot == mod.SlotCoordinatorSection {
		return mod.CoordinatorBlocks
	}
	return mod.DetailsBlocks
}

// sectionViews are the views of t's slot the panel shows, in mod order: in
// safe mode a user mod's don't show (its host doesn't run).
func sectionViews(list []proto.ModView, t panelTarget, o panelOpts) []proto.ModView {
	var out []proto.ModView
	for _, v := range sections(list, t.slot, t.project) {
		if o.noMods && !builtinMod(o.mods, v.Mod) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// panelHeight is the body lines section v asks for in t: its tree's
// height, else the slot's default, at most the slot's most.
func panelHeight(v proto.ModView, t panelTarget) int {
	def, most := modview.Heights(t.slot)
	if tr, _ := sectionTree(v, t.project, t.task, t.thr); tr != nil && tr.Height > 0 {
		return min(tr.Height, most)
	}
	return def
}

// panelSections are the panel's sections for t, in the order they are
// drawn, a body w cells wide: a failed mod's says so, never its stale
// tree; one that drew nothing for t adds nothing.
func panelSections(list []proto.ModView, t panelTarget, o panelOpts, w int) []panelSec {
	vs := sectionViews(list, t, o)
	ids := make([]string, len(vs))
	for i, v := range vs {
		ids[i] = v.Mod + "." + v.View
	}
	var out []panelSec
	used := map[string]bool{}
	defer func() {
		if o.cache != nil {
			// The cache keeps the lines of the sections drawn, for the row
			// shown now.
			o.cache.forget(func(k string) bool { return used[k] })
		}
	}()
	for _, p := range mod.PanelSections(o.order, t.blocks(), ids) {
		v := vs[slices.Index(ids, p.ID)]
		s := &sideSection{id: view.PanelSection + p.ID, mod: v.Mod, view: v.View, title: v.Title, name: v.Name,
			desc: v.Description, opens: v.Opens, log: v.Log, builtin: builtinMod(o.mods, v.Mod), project: t.project}
		s.folded = o.folds.IsSectionFolded(s.id)
		if v.State == proto.ModViewFailed {
			s.failed = true
			if !s.folded {
				s.lines = failedLines(v, w)
			}
		} else {
			tr, _ := sectionTree(v, t.project, t.task, t.thr)
			if tr == nil {
				continue
			}
			s.badge = tr.Badge
			if !s.folded {
				key := v.Mod + "/" + v.View + "/" + t.project + "/" + t.task + "/" + t.thr
				used[key] = true
				s.lines = o.cache.lines(key, tr.Tree, w, panelHeight(v, t))
			}
		}
		out = append(out, panelSec{sec: s, before: p.Before})
	}
	return out
}

// panelShown are the panel's sections for t as mod.views is told of
// them, a panel w cells wide: every one it would draw, folded or not (a
// folded header still shows its badge), each with the height it gets.
func panelShown(list []proto.ModView, t panelTarget, o panelOpts, w int) []modview.Shown {
	var out []modview.Shown
	for _, v := range sectionViews(list, t, o) {
		out = append(out, modview.Shown{Mod: v.Mod, View: v.View, Slot: v.Slot, Project: t.project, Task: t.task, Thread: t.thr,
			Width: panelBody(w), Height: panelHeight(v, t)})
	}
	return out
}

// panelBody is a section's body width in a panel w cells wide: less the
// margin, an indent of 2 and a blank at the right.
func panelBody(w int) int { return max(w-4, 1) }

// The budget.

// panelAsk is what a panel's section asks of the budget: its body lines
// (none folded), the core line it goes above (-1 at the bottom),
// whether that is above core's first block, and whether its header is
// selected.
type panelAsk struct {
	want  int
	at    int
	above bool
	sel   bool
}

// panelFit is how a panel's sections fit: per section whether its header
// shows, its body lines, whether it gave its body up for room (shrunk)
// and whether it is drawn at the bottom; more is how many headers didn't
// fit, which the last one shown says (+N).
type panelFit struct {
	show, shrunk, bottom []bool
	body                 []int
	more                 int
}

// rows are the rows section j takes: a blank, its header and its body.
func (f panelFit) rows(j int) int {
	if !f.show[j] {
		return 0
	}
	return 2 + f.body[j]
}

// used is the rows the sections in (all for nil) take.
func (f panelFit) used(in func(int) bool) int {
	n := 0
	for j := range f.show {
		if in == nil || in(j) {
			n += f.rows(j)
		}
	}
	return n
}

func (f panelFit) headers() int {
	n := 0
	for _, on := range f.show {
		if on {
			n++
		}
	}
	return n
}

// fitPanel fits a panel's sections asks into a panel of h rows whose
// core lines must (the must-see ones) stay where they are seen: the
// budget (the T348 design §11.5) and the safety guarantee (§11.6) in
// one.
//
// Each section gets its body lines (at most modview.MaxSidebarHeight),
// a folded one its header. Under panelMin rows they all go to the
// bottom, where they push nothing off and the panel scrolls to them.
// Otherwise sections take at most half the panel, their headers a
// quarter, and those above or between core's blocks a quarter. Then,
// while a must-see line on the first screen without sections would be
// pushed off it, sections give way: those above core's blocks, the
// lowest first, then those between them, each body to its header; then
// the headers in the same order, the selected one last. The bottom
// sections give way, last, only for the shares.
func fitPanel(h int, must []int, asks []panelAsk) panelFit {
	n := len(asks)
	f := panelFit{show: make([]bool, n), shrunk: make([]bool, n), bottom: make([]bool, n), body: make([]int, n)}
	for j, a := range asks {
		f.bottom[j] = a.at < 0 || h < panelMin
		f.show[j], f.body[j] = true, min(max(a.want, 0), modview.MaxSidebarHeight)
	}
	if h < panelMin || n == 0 {
		return f
	}
	// The order sections give way in: above core's blocks, then between
	// them, then at the bottom, each from its lowest.
	var order []int
	for _, group := range []func(int) bool{
		func(j int) bool { return !f.bottom[j] && asks[j].above },
		func(j int) bool { return !f.bottom[j] && !asks[j].above },
		func(j int) bool { return f.bottom[j] },
	} {
		for j := n - 1; j >= 0; j-- {
			if group(j) {
				order = append(order, j)
			}
		}
	}
	all := func(int) bool { return true }
	placed := func(j int) bool { return !f.bottom[j] }
	shrink := func(in func(int) bool) bool {
		for _, j := range order {
			if in(j) && f.show[j] && f.body[j] > 0 {
				f.body[j], f.shrunk[j] = 0, true
				return true
			}
		}
		return false
	}
	drop := func(sel bool, in func(int) bool) bool {
		for _, j := range order {
			if in(j) && f.show[j] && (sel || !asks[j].sel) {
				f.show[j], f.body[j] = false, 0
				return true
			}
		}
		return false
	}
	give := func(in func(int) bool) bool { return shrink(in) || drop(false, in) || drop(true, in) }
	for f.headers() > h/4 && drop(false, all) {
	}
	for f.used(placed) > h/4 && give(placed) {
	}
	for f.used(nil) > h/2 && give(all) {
	}
	safe := func() bool {
		for _, k := range must {
			if k >= h {
				continue
			}
			shift := 0
			for j, a := range asks {
				if !f.bottom[j] && a.at <= k {
					shift += f.rows(j)
				}
			}
			if k+shift >= h {
				return false
			}
		}
		return true
	}
	for !safe() && give(placed) {
	}
	for j := range asks {
		if !f.show[j] {
			f.more++
			f.shrunk[j] = false
		}
	}
	return f
}

// The layout: core's lines with the sections placed.

// panelLay is a panel with its sections placed: its lines, what a click
// on each does, its sections in the order drawn, and the line of each
// one's header (-1 for one that didn't fit).
type panelLay struct {
	lines []string
	hits  []infoHit
	secs  []*sideSection
	heads []int
}

// headAt is the index in l.secs of the section whose header is line i,
// -1 for none.
func (l panelLay) headAt(i int) int { return slices.Index(l.heads, i) }

// shownHeads are the indexes in l.secs of the headers drawn, in order.
func (l panelLay) shownHeads() []int {
	var out []int
	for j, h := range l.heads {
		if h >= 0 {
			out = append(out, j)
		}
	}
	return out
}

// layPanel places the sections of t from list among core's lines in pl
// (hits: what a click on each does, nil for nothing), as o says.
func layPanel(pl *panel, hits []infoHit, list []proto.ModView, t panelTarget, o panelOpts) panelLay {
	hits = slices.Clone(hits)
	for len(hits) < len(pl.lines) {
		hits = append(hits, infoHit{})
	}
	secs := panelSections(list, t, o, panelBody(pl.w))
	lay := panelLay{secs: make([]*sideSection, len(secs)), heads: make([]int, len(secs))}
	asks := make([]panelAsk, len(secs))
	first := ""
	if bs := t.blocks(); len(bs) > 0 {
		first = bs[0]
	}
	for j, p := range secs {
		lay.secs[j] = p.sec
		at, ok := pl.starts[p.before]
		if p.before == "" || !ok {
			at = -1
		}
		asks[j] = panelAsk{want: p.sec.want(), at: at, above: p.before == first, sel: p.sec.id == o.sel}
	}
	var must []int
	for i, on := range pl.must {
		if on {
			must = append(must, i)
		}
	}
	rows := o.rows
	if rows <= 0 {
		rows = len(pl.lines) + 1<<10 // no fitting: room for all
	}
	fit := fitPanel(rows, must, asks)
	last := -1
	for j := range secs {
		if fit.show[j] {
			last = j
		}
	}
	var sel = th().Sel.Bold(true)
	if o.focused {
		sel = th().SelFocus.Bold(true)
	}
	// section adds section j's lines.
	section := func(j int) {
		lay.heads[j] = -1
		if !fit.show[j] {
			return
		}
		s := lay.secs[j]
		hit := infoHit{kind: hitSection, sec: s.id}
		more := 0
		if j == last {
			more = fit.more
		}
		lay.lines = append(lay.lines, "")
		lay.hits = append(lay.hits, infoHit{})
		lay.heads[j] = len(lay.lines)
		head := sectionHeader(s, "", max(pl.w-2, 1), 0, fit.shrunk[j], more, "", s.id == o.sel, sel)
		lay.lines = append(lay.lines, fit1(" "+head, pl.w))
		lay.hits = append(lay.hits, hit)
		for _, l := range s.lines[:min(fit.body[j], len(s.lines))] {
			lay.lines = append(lay.lines, fit1("   "+l, pl.w))
			lay.hits = append(lay.hits, hit)
		}
	}
	next := 0
	for i, l := range pl.lines {
		for ; next < len(secs) && !fit.bottom[next] && asks[next].at <= i; next++ {
			section(next)
		}
		lay.lines = append(lay.lines, l)
		lay.hits = append(lay.hits, hits[i])
	}
	for j := range secs {
		if j >= next || fit.bottom[j] {
			section(j)
		}
	}
	return lay
}

// fit1 is a panel line: s cut to w cells, its styles reset.
func fit1(s string, w int) string { return fit(s, w) + reset }

// panelFoldIDs are the fold ids of a panel's sections in lay.
func panelFoldIDs(lay panelLay) []string {
	out := make([]string, len(lay.secs))
	for i, s := range lay.secs {
		out[i] = s.id
	}
	return out
}

// panelStep is what a panel key op does on the selected section s (nil
// for none): ] and [ move the selection among the headers drawn,
// → unfolds s, ← folds it, enter opens the view its mod names (opens),
// or folds or unfolds it when it names none. ok is false for a key it
// doesn't take: with no header selected, ← → and enter keep their
// meaning (the task, the list's).
func panelStep(lay panelLay, sel string, op int) (st sideStep, next string, ok bool) {
	i := slices.IndexFunc(lay.secs, func(s *sideSection) bool { return s.id == sel })
	var s *sideSection
	if i >= 0 && lay.heads[i] >= 0 {
		s = lay.secs[i]
	}
	switch op {
	case panelNext, panelPrev:
		heads := lay.shownHeads()
		if len(heads) == 0 {
			return sideStep{}, sel, false
		}
		at := slices.Index(heads, i)
		switch {
		case at < 0 && op == panelNext:
			at = 0
		case at < 0:
			at = len(heads) - 1
		case op == panelNext:
			at = (at + 1) % len(heads)
		default:
			at = (at + len(heads) - 1) % len(heads)
		}
		return sideStep{}, lay.secs[heads[at]].id, true
	case panelUnfold, panelFold, panelEnter:
		if s == nil {
			return sideStep{}, sel, false
		}
		r := treeRow{kind: treeSection, sec: s}
		sop := map[int]int{panelUnfold: sideIn, panelFold: sideOut, panelEnter: sideEnter}[op]
		st, _ := sectionStep(r, sop)
		return st, sel, true
	}
	return sideStep{}, sel, false
}

// panelHeadClick is what a click at column x (in the panel's content)
// on a line of section s does: on the fold mark it folds or unfolds it,
// elsewhere it opens the view its mod names, or folds or unfolds it.
func panelHeadClick(s *sideSection, x int, onHead bool) sideStep {
	fold := sideStep{sections: []string{s.id}, foldOn: !s.folded}
	if onHead && x <= 2 {
		return fold
	}
	if s.opens != "" {
		return sideStep{open: s}
	}
	return fold
}

// panelHintKeys are the status bar's or the footer's words for the
// panel's section keys: "] [ sections" while it has sections, and on a
// selected header "← fold · enter open".
func panelHintKeys(lay panelLay, sel string) string {
	if len(lay.shownHeads()) == 0 {
		return ""
	}
	out := barKey("panel.section-next", "]") + " " + barKey("panel.section-prev", "[") + " sections"
	i := slices.IndexFunc(lay.secs, func(s *sideSection) bool { return s.id == sel })
	if i < 0 || lay.heads[i] < 0 {
		return out
	}
	s := lay.secs[i]
	if s.folded {
		out += " · " + barKey("panel.unfold", "→") + " unfold"
	} else {
		out += " · " + barKey("panel.fold", "←") + " fold"
	}
	if s.opens != "" {
		out += " · " + barKey("panel.enter", "enter") + " open"
	}
	return out
}

// The panels' keys: while the info panel (attach) or the details panel
// (the dashboard) has the keyboard. A table, as the list's and the
// sidebar's, so the help and keymap mods see them.

// Panel key ops.
const (
	panelUp = iota
	panelDown
	panelPageUp
	panelPageDown
	panelFirst
	panelLast
	panelNext
	panelPrev
	panelUnfold
	panelFold
	panelEnter
	panelBack
)

// panelAction is a panel key: its id, its default keys, what it does and
// its help line, as a sideAction.
type panelAction struct {
	id          string
	keys        []string
	op          int
	label, help string
	fixed       bool
}

var panelActions = []panelAction{
	{id: "panel.up", keys: []string{"up", "k"}, op: panelUp, label: "↑ ↓ k j", help: "scroll the panel"},
	{id: "panel.down", keys: []string{"down", "j"}, op: panelDown},
	{id: "panel.page-up", keys: []string{"pgup"}, op: panelPageUp, label: "pgup pgdown", help: "a page up / down"},
	{id: "panel.page-down", keys: []string{"pgdown"}, op: panelPageDown},
	{id: "panel.first", keys: []string{"home"}, op: panelFirst, label: "home end", help: "the top / the end of the panel"},
	{id: "panel.last", keys: []string{"end"}, op: panelLast},
	{id: "panel.section-next", keys: []string{"]"}, op: panelNext, label: "] [", help: "select the next / previous mod section's header, round the ends, and scroll it into view (while the panel has sections; else, on the dashboard, the next / previous project as on the list)"},
	{id: "panel.section-prev", keys: []string{"["}, op: panelPrev},
	{id: "panel.unfold", keys: []string{"right"}, op: panelUnfold, label: "→ ←", help: "on a selected header: unfold / fold the section, in every console of the view"},
	{id: "panel.fold", keys: []string{"left"}, op: panelFold},
	{id: "panel.enter", keys: []string{"enter"}, op: panelEnter, label: "enter", help: "on a selected header: open the view its mod names, else fold or unfold it; with none selected, as before (the thread's task; on the dashboard, the list's enter)"},
	{id: "panel.back", keys: []string{"esc"}, op: panelBack, label: "esc", help: "drop the selected header, then give the keyboard back", fixed: true},
}

func panelBindables() []bindable {
	out := make([]bindable, len(panelActions))
	for i, a := range panelActions {
		out[i] = bindable{a.id, a.keys, a.fixed, a.label, a.help}
	}
	return out
}

// panelOp is the panel op typed key runs (through the keymap); ok is
// false for none.
func panelOp(key string) (int, bool) {
	key = binds.panel.canon(key)
	for _, a := range panelActions {
		if slices.Contains(a.keys, key) {
			return a.op, true
		}
	}
	return 0, false
}
