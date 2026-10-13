package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// The band of sidebar sections below the tree (the T348 design §1-§6;
// docs/SPEC.md §4, Mod views): a mod's sidebar.section is a section
// under the projects tree, for the current project, after a blank row.
// Its header is a row of the tree's cursor path ("▾ TICKER ──
// terminatr"): ← and → fold and unfold it, enter opens the view the mod
// names (opens, by a ui.click) or else folds it. Its body is the mod's
// tree, read-only: no cursor, no keys. It shows from the mod's first
// tree: until then, and while the tree is null, nothing shows, not even
// its header (a view the mod leaves out, as the ticker's place does,
// never flashes one). The view keeps the folds
// (view.Tree.SectionsFolded, view.sections). The band takes at most half
// the sidebar's body, its headers a quarter, and gives way, bodies first
// and then headers, the bottom section first, so that every row with ⚑,
// the cursor's and the one you are on show as they would without it
// (fitBand). The slim strip and the hidden sidebar have no sections, and
// a sidebar body under bandMinBody rows has none either.
//
// A mod's sidebar.project.section is a section under each active,
// unfolded project, after its last thread, at thread depth, for that
// project (projectRows): a row of the tree, scrolled with it. All of
// them take at most a quarter of the body, and give way first, the
// bottom project's first. A failed one says so once, in the band below
// the tree.

// bandMinBody is the fewest rows of the sidebar's body (its rows less
// the header and the footer) that show sections.
const bandMinBody = 12

// sideSection is a band section, as its header row carries it.
type sideSection struct {
	// id is its fold's id, mod.view, or mod.view@slug under a project
	// (view.Tree.SectionsFolded).
	id          string
	mod, view   string
	title, name string
	desc, opens string
	log         string // a failed mod's log
	builtin     bool
	project     string // the project it shows for: enter's target
	above       bool   // in the band above the tree (sidebar.sections)
	proj        bool   // under its project's threads (sidebar.project.section)
	of          string // the project of the tree it shows, "" for all
	badge       []widget.Span
	lines       []string // the body unfolded, as drawn
	folded      bool     // the user folded it
	failed      bool
}

// want is how many body lines s asks for: none folded.
func (s *sideSection) want() int {
	if s.folded {
		return 0
	}
	return len(s.lines)
}

// bandIn is what the band is built from: the mods' views (mod.views),
// the current project, the view's folds, the sidebar's width, the mods
// (which are built in) and safe mode, and the console's line cache.
type bandIn struct {
	list    []proto.ModView
	project string
	folds   view.Tree
	w       int
	mods    []mod.Mod
	noMods  bool
	cache   *sectionCache
	// order is the layout's sidebar.sections: the band's order, those
	// before "tree" above the tree; projOrder its
	// sidebar.project_sections, the order under each project.
	order, projOrder []string
}

// bandWidth is a band section's body width in a sidebar w columns wide:
// less the border, the gap, the margin and a 2-cell indent.
func bandWidth(w int) int { return max(w-1-sideGap-3, 1) }

// projIndent is the cells before a per-project section's header's fold
// mark, at thread depth: its body starts 2 cells further, under the
// title.
const projIndent = 4

// projWidth is a per-project section's body width in a sidebar w
// columns wide: a band section's less its thread-depth indent.
func projWidth(w int) int { return max(bandWidth(w)-projIndent, 1) }

// bandViews are the band's sections the console shows for in.project,
// above the tree and below it, in the layout's order (orderSections):
// in safe mode a user mod's don't show (its host doesn't run).
func bandViews(in bandIn) (above, below []proto.ModView) {
	if in.w <= sideSlim {
		return nil, nil
	}
	var out []proto.ModView
	for _, v := range sections(in.list, mod.SlotSidebarSection, in.project) {
		if in.noMods && !builtinMod(in.mods, v.Mod) {
			continue
		}
		out = append(out, v)
	}
	above, below = orderSections(out, in.order)
	// A failed per-project section says so once, here, not under every
	// project.
	for _, v := range projViews(in) {
		if v.State == proto.ModViewFailed {
			below = append(below, v)
		}
	}
	return above, below
}

// projViews are the per-project sections the console shows, in the
// layout's order (sidebar.project_sections), then in mod order: in safe
// mode a user mod's don't show.
func projViews(in bandIn) []proto.ModView {
	if in.w <= sideSlim {
		return nil
	}
	var out []proto.ModView
	for _, v := range in.list {
		if v.Slot == mod.SlotProjectSection && !(in.noMods && !builtinMod(in.mods, v.Mod)) {
			out = append(out, v)
		}
	}
	above, below := orderSections(out, in.projOrder)
	return slices.Concat(above, below)
}

// orderSections puts sections vs, in mod order, in a layout's order
// (sidebar.sections, or sidebar.project_sections, which has no "tree"):
// those it names before "tree" above the tree, those after it below,
// and those it doesn't name below after them, in mod order. An id no
// section has is skipped.
func orderSections(vs []proto.ModView, order []string) (above, below []proto.ModView) {
	ids, after := mod.SplitSections(order)
	used := make([]bool, len(vs))
	pick := func(ids []string) []proto.ModView {
		var out []proto.ModView
		for _, id := range ids {
			for i, v := range vs {
				if !used[i] && v.Mod+"."+v.View == id {
					used[i] = true
					out = append(out, v)
				}
			}
		}
		return out
	}
	above, below = pick(ids), pick(after)
	for i, v := range vs {
		if !used[i] {
			below = append(below, v)
		}
	}
	return above, below
}

// builtinMod says whether mod id is a built-in mod.
func builtinMod(mods []mod.Mod, id string) bool {
	i := slices.IndexFunc(mods, func(m mod.Mod) bool { return m.ID == id })
	return i >= 0 && mods[i].Builtin
}

// withSections is the tree rows with the sections' header rows: each
// project's after its last row (projRows), then the band's (withBand).
func withSections(tree []treeRow, in bandIn) []treeRow {
	return withBand(projRows(tree, in), bandRows(in))
}

// withBand is the tree rows with the band's header rows: those above the
// tree before its first row, the others after its last, so the cursor
// meets them in the order they are drawn.
func withBand(tree, band []treeRow) []treeRow {
	i := slices.IndexFunc(band, func(r treeRow) bool { return !r.sec.above })
	if i < 0 {
		i = len(band)
	}
	return slices.Concat(band[:i], tree, band[i:])
}

// bandRows are the band's header rows, with their sections: a failed
// mod's says so, and one that drew nothing yet, or whose tree for the
// project (and for all) is null, adds nothing.
func bandRows(in bandIn) []treeRow {
	var out []treeRow
	w := bandWidth(in.w)
	above, below := bandViews(in)
	for i, v := range slices.Concat(above, below) {
		s := &sideSection{id: v.Mod + "." + v.View, mod: v.Mod, view: v.View, title: v.Title, name: v.Name,
			desc: v.Description, opens: v.Opens, log: v.Log, builtin: builtinMod(in.mods, v.Mod), project: in.project,
			above: i < len(above)}
		s.folded = in.folds.IsSectionFolded(s.id)
		switch t := sectionTree(v, in.project, "", ""); {
		case v.State == proto.ModViewFailed:
			s.failed = true
			s.lines = []string{th().Bad.Render(fit(ic().blocked+" mod failed: "+cmp.Or(v.Err, "its host stopped"), w)) + reset,
				th().Faint.Render(fit("r in the Mods tab restarts it", w)) + reset}
		case t == nil:
			// Nothing drawn yet, or null: nothing to say here. A section
			// shows from its mod's first tree, so one the mod leaves out
			// never flashes a header (the ticker's place = "right").
			continue
		default:
			s.of, s.badge = t.Project, t.Badge
			if !s.folded {
				def, most := modview.Heights(mod.SlotSidebarSection)
				h := min(cmp.Or(t.Height, def), most)
				s.lines = in.cache.lines(v.Mod+"/"+v.View+"/"+in.project, t.Tree, w, h)
			}
		}
		out = append(out, treeRow{kind: treeSection, slug: in.project, pct: -1, sec: s})
	}
	return out
}

// projRows are the tree's rows with the per-project sections' header
// rows: after the last row of each active, unfolded project (its last
// thread, else its coordinator), none for a folded or an inactive one,
// nor under the divider. A project the filter took out has no rows, so
// no sections either. A failed mod's section is the band's (bandViews);
// one that drew nothing for the project yet, or whose tree for it (and
// for all) is null, adds nothing there.
func projRows(tree []treeRow, in bandIn) []treeRow {
	vs := slices.DeleteFunc(projViews(in), func(v proto.ModView) bool { return v.State == proto.ModViewFailed })
	if len(vs) == 0 {
		return tree
	}
	w := projWidth(in.w)
	def, most := modview.Heights(mod.SlotProjectSection)
	out := make([]treeRow, 0, len(tree)+len(vs))
	for i, r := range tree {
		out = append(out, r)
		if i+1 < len(tree) && tree[i+1].kind != treeProject && tree[i+1].kind != treeDivider {
			continue // not its project's last row
		}
		p := i
		for p >= 0 && (tree[p].kind == treeCoordinator || tree[p].kind == treeThread) {
			p--
		}
		if p < 0 || tree[p].kind != treeProject || tree[p].inactive || tree[p].fold {
			continue
		}
		slug := tree[p].slug
		for _, v := range vs {
			if !runsFor(v, slug) {
				continue
			}
			s := &sideSection{id: v.Mod + "." + v.View + "@" + slug, mod: v.Mod, view: v.View, title: v.Title, name: v.Name,
				desc: v.Description, opens: v.Opens, log: v.Log, builtin: builtinMod(in.mods, v.Mod), project: slug, proj: true}
			s.folded = in.folds.IsSectionFolded(s.id)
			switch t := sectionTree(v, slug, "", ""); {
			case t == nil:
				continue // nothing drawn for it yet, or null
			default:
				s.of, s.badge = t.Project, t.Badge
				if !s.folded {
					s.lines = in.cache.lines(v.Mod+"/"+v.View+"/"+slug, t.Tree, w, min(cmp.Or(t.Height, def), most))
				}
			}
			out = append(out, treeRow{kind: treeSection, slug: slug, pct: -1, sec: s})
		}
	}
	return out
}

// projShown are the per-project sections as mod.views is told of them:
// one per project in on (the projects whose rows are on screen) that
// would show them, folded or not, those with a null tree too.
func projShown(rows []treeRow, in bandIn, on func(slug string) bool) []modview.Shown {
	var out []modview.Shown
	def, most := modview.Heights(mod.SlotProjectSection)
	for _, r := range rows {
		if r.kind != treeProject || r.inactive || r.fold || !on(r.slug) {
			continue
		}
		for _, v := range projViews(in) {
			if v.State == proto.ModViewFailed || !runsFor(v, r.slug) {
				continue
			}
			h := def
			if t := sectionTree(v, r.slug, "", ""); t != nil && t.Height > 0 {
				h = min(t.Height, most)
			}
			out = append(out, modview.Shown{Mod: v.Mod, View: v.View, Slot: v.Slot, Project: r.slug, Width: projWidth(in.w), Height: h})
		}
	}
	return out
}

// sectionTree is the tree section v shows for project's task or thread
// (both "": the project's own): the one published for it, else, for the
// project's own, the one for all projects; nil while the mod published
// none, or only null ones (modview.Store keeps a section's null): then
// no section shows, not even its header. The band and the right pane's panels look trees
// up alike.
func sectionTree(v proto.ModView, project, task, thr string) *modview.Tree {
	own := task == "" && thr == ""
	var all *modview.Tree
	for i := range v.Trees {
		tr := &v.Trees[i]
		switch {
		case tr.Tree == nil:
			// null: says nothing
		case tr.For(project, task, thr) && (project != "" || !own):
			return tr
		case own && tr.For("", "", ""):
			all = tr
		}
	}
	return all
}

// bandShown are the band's sections as mod.views is told of them, so
// their mods hear view.opened: every one the console would draw, folded
// or not (a folded header still shows its badge), and those with a null
// tree, which may publish another.
func bandShown(in bandIn) []modview.Shown {
	var out []modview.Shown
	above, below := bandViews(in)
	for _, v := range slices.Concat(above, below) {
		if v.Slot != mod.SlotSidebarSection {
			continue // a failed per-project section's line
		}
		def, most := modview.Heights(mod.SlotSidebarSection)
		h := def
		if t := sectionTree(v, in.project, "", ""); t != nil && t.Height > 0 {
			h = min(t.Height, most)
		}
		out = append(out, modview.Shown{Mod: v.Mod, View: v.View, Slot: v.Slot, Project: in.project, Width: bandWidth(in.w), Height: h})
	}
	return out
}

// sectionCache is a console's band lines: per section, its widget.View
// and the lines last drawn, kept while the tree, the size and (for a
// tree with time spans) the second are the same. The attach sidebar
// draws up to 120 times a second.
type sectionCache struct {
	hosts map[string]*sectionHost
}

type sectionHost struct {
	view  *widget.View
	tree  *widget.Widget
	timed bool
	w, h  int
	at    int64
	lines []string
}

// lines are tree t drawn w cells by h lines, for section key.
func (c *sectionCache) lines(key string, t *widget.Widget, w, h int) []string {
	if c == nil {
		v := widget.NewView()
		v.Set(t)
		return v.Render(w, h, sectionLook{})
	}
	if c.hosts == nil {
		c.hosts = map[string]*sectionHost{}
	}
	sh := c.hosts[key]
	if sh == nil {
		sh = &sectionHost{view: widget.NewView()}
		c.hosts[key] = sh
	}
	now := time.Now().Unix()
	if sh.tree == t && sh.w == w && sh.h == h && sh.lines != nil && (!sh.timed || sh.at == now) {
		return sh.lines
	}
	if sh.tree != t {
		sh.view.Set(t)
		sh.tree, sh.timed = t, t.Timed()
	}
	sh.w, sh.h, sh.at = w, h, now
	sh.lines = sh.view.Render(w, h, sectionLook{})
	if sh.lines == nil {
		sh.lines = []string{}
	}
	return sh.lines
}

// forget drops the sections not in keep.
func (c *sectionCache) forget(keep func(key string) bool) {
	if c == nil {
		return
	}
	for k := range c.hosts {
		if !keep(k) {
			delete(c.hosts, k)
		}
	}
}

// The budget.

// Where a section is: in the band above the tree, under a project's
// threads, or in the band below the tree.
const (
	placeAbove = iota
	placeProj
	placeBelow
)

// bandAsk is what a section asks of the budget: its body lines (none
// folded), whether its header is the cursor's row, and where it is; a
// per-project one (proj) comes after the tree's first at rows.
type bandAsk struct {
	want          int
	cursor, above bool
	proj          bool
	at            int
}

// place is where a is.
func (a bandAsk) place() int {
	switch {
	case a.above:
		return placeAbove
	case a.proj:
		return placeProj
	}
	return placeBelow
}

// bandFit is how the sections fit a sidebar body: the rows the tree
// keeps (treeH, its per-project sections shown in them), and per section
// whether its header shows, its body lines, and whether it gave its body
// up for room (shrunk); more is how many headers didn't fit, which the
// last one shown says (+N). asks are the sections' asks; top is the
// first of the tree's slots (its rows and its per-project sections'
// lines) that shows.
type bandFit struct {
	treeH  int
	show   []bool
	body   []int
	shrunk []bool
	asks   []bandAsk
	more   int
	top    int
}

// used is the rows the sections take, above the tree, under its
// projects and below it.
func (f bandFit) used() int { return f.part(placeAbove) + f.part(placeProj) + f.part(placeBelow) }

// part is the rows the sections of place take, their headers shown and
// their bodies; the band below the tree a blank row above it too.
func (f bandFit) part(place int) int {
	n := 0
	for j, on := range f.show {
		if on && f.asks[j].place() == place {
			n += 1 + f.body[j]
		}
	}
	if n > 0 && place == placeBelow {
		n++
	}
	return n
}

func (f bandFit) headers() int {
	n := 0
	for _, on := range f.show {
		if on {
			n++
		}
	}
	return n
}

// treeSlot is tree row k's slot: its place among the tree's rows and
// the lines of the per-project sections shown before it.
func (f bandFit) treeSlot(k int) int {
	s := k
	for j, a := range f.asks {
		if a.proj && f.show[j] && a.at <= k {
			s += 1 + f.body[j]
		}
	}
	return s
}

// headSlot is per-project section j's header's slot.
func (f bandFit) headSlot(j int) int {
	s := f.asks[j].at
	for i := range j {
		if f.asks[i].proj && f.show[i] {
			s += 1 + f.body[i]
		}
	}
	return s
}

// scroll is the tree's rows on screen and its first slot that shows, in
// a body of b rows with n tree rows, cur the tree row it scrolls to (-1
// for none) unless the cursor is on a per-project header shown.
func (f bandFit) scroll(b, n, cur int) (h, top int) {
	h = b - f.part(placeAbove) - f.part(placeBelow)
	at := -1
	if cur >= 0 {
		at = f.treeSlot(cur)
	}
	for j, a := range f.asks {
		if a.proj && a.cursor && f.show[j] {
			at = f.headSlot(j)
		}
	}
	return h, scrollTop(at, max(h, 1), n+f.part(placeProj))
}

// fitBand fits the sections asks, in the order they are drawn (above the
// tree, under its projects, below it), around a tree of n rows in a
// sidebar body of b rows: the budget (the T348 design §4) and the safety
// guarantee (§5) in one. cur is the tree row the tree scrolls to (the
// cursor's, else the one you are on; -1 for none), and keep the tree
// rows that must stay on screen: those with ⚑, the cursor's and the one
// you are on, among the rows the tree shows without sections (bandKeep).
//
// Each section gets its body lines, a folded one its header. The
// sections take at most half the body, their headers a quarter, the band
// above the tree a quarter and those under the projects a quarter; under
// bandMinBody rows nothing. Then, while a row of keep would scroll off
// the tree's rows left, sections give way: the bodies under the
// projects, the bottom project's first, then those above the tree, then
// those below, each band's bottom section first; then the headers in the
// same order, the cursor's last. The rows left are the tree's, and its
// per-project sections are among them.
func fitBand(b, n, cur int, keep []int, asks []bandAsk) bandFit {
	f := bandFit{show: make([]bool, len(asks)), body: make([]int, len(asks)), shrunk: make([]bool, len(asks)), asks: asks}
	if b >= bandMinBody {
		f.give(b, n, cur, keep)
	}
	for j := range asks {
		if !f.show[j] && b >= bandMinBody {
			f.more++
			f.shrunk[j] = false
		}
	}
	f.treeH, f.top = f.scroll(max(b, 0), n, cur)
	return f
}

// give is fitBand's budget and guarantee.
func (f *bandFit) give(b, n, cur int, keep []int) {
	asks := f.asks
	for j, a := range asks {
		f.show[j], f.body[j] = true, min(max(a.want, 0), modview.MaxSidebarHeight)
	}
	// The order sections give way in: under the projects, then above the
	// tree, then below, each from its bottom.
	var order []int
	for _, place := range []int{placeProj, placeAbove, placeBelow} {
		for j := len(asks) - 1; j >= 0; j-- {
			if asks[j].place() == place {
				order = append(order, j)
			}
		}
	}
	all := func(int) bool { return true }
	in := func(place int) func(int) bool { return func(j int) bool { return asks[j].place() == place } }
	shrink := func(in func(int) bool) bool {
		for _, j := range order {
			if in(j) && f.show[j] && f.body[j] > 0 {
				f.body[j], f.shrunk[j] = 0, true
				return true
			}
		}
		return false
	}
	drop := func(cursor bool, in func(int) bool) bool {
		for _, j := range order {
			switch {
			case !in(j) || !f.show[j]:
			case cursor || !asks[j].cursor:
				f.show[j], f.body[j] = false, 0
				return true
			case f.body[j] > 0:
				// The cursor's header stays; its body gives way first.
				f.body[j], f.shrunk[j] = 0, true
			}
		}
		return false
	}
	give := func(in func(int) bool) bool { return shrink(in) || drop(false, in) || drop(true, in) }
	// The shares keep the cursor's header: alone it is one row, in any.
	share := func(in func(int) bool) bool { return shrink(in) || drop(false, in) }
	for f.headers() > b/4 && drop(false, all) {
	}
	for f.part(placeAbove) > b/4 && share(in(placeAbove)) {
	}
	for f.part(placeProj) > b/4 && share(in(placeProj)) {
	}
	for f.used() > b/2 && share(all) {
	}
	safe := func() bool {
		h, top := f.scroll(b, n, cur)
		if h >= n+f.part(placeProj) {
			return true
		}
		for _, k := range keep {
			if s := f.treeSlot(k); s < top || s >= top+h {
				return false
			}
		}
		return true
	}
	for !safe() && give(all) {
	}
}

// bandKeep are the rows of tree a body of b rows shows without
// sections that must stay on screen with them (fitBand), and the row
// the tree scrolls to: the cursor's, else the one you are on.
func bandKeep(tree []treeRow, b int) (cur int, keep []int) {
	cur = -1
	for i, r := range tree {
		if r.cursor {
			cur = i
			break
		}
		if r.here {
			cur = i
		}
	}
	top := scrollTop(cur, max(b, 1), len(tree))
	for i := top; i < min(top+b, len(tree)); i++ {
		if r := tree[i]; r.hint || r.cursor || r.here {
			keep = append(keep, i)
		}
	}
	return cur, keep
}

// The layout: what each row of the sidebar below its header shows.

// sideSlot is a row of the sidebar's body: a shown row (row, an index
// in sideLay.rows) or a section's body line (line ≥ 0), or blank (row
// -1).
type sideSlot struct {
	row, line int
}

// sideLay is a sidebar's layout: its shown rows, the band above the
// tree (above, drawn above the header), the header's rows (head), the
// rest of its body's rows in order, and how the band fit.
type sideLay struct {
	rows  []treeRow
	above []sideSlot
	head  int
	slots []sideSlot
	fit   bandFit
	band  []int // the rows' indexes of the band's headers, in fit's order
	// flag: the header shows ⚑ N, what needs you, as a section above
	// the tree shows or a row with ⚑ is scrolled off.
	flag bool
}

// projectCount is every project of the tree, those under a folded
// divider too, as the header counts them.
func projectCount(all []treeRow) int {
	n := 0
	for _, r := range all {
		switch {
		case r.kind == treeProject && !r.inactive:
			n++
		case r.kind == treeDivider:
			n += r.threads
		}
	}
	return n
}

// sideLayout lays out a sidebar w columns wide and h rows tall whose
// footer takes foot rows (none when the sidebar is too short for it, as
// sideLines draws it): the band above the tree, the header, the tree
// with its per-project sections scrolled so the cursor's row shows, then
// the band below (fitBand).
func sideLayout(all []treeRow, w, h, foot int) sideLay {
	lay := sideLay{rows: shownRows(all, w), head: sideHeadRows()}
	if foot > 0 && h < foot+4 {
		foot = 0
	}
	if projectCount(all) == 0 {
		lay.head++ // "no projects"
	}
	body := max(h-foot-lay.head, 0)
	var tree []int
	var asks []bandAsk
	for i, r := range lay.rows {
		if r.kind == treeSection {
			lay.band = append(lay.band, i)
			asks = append(asks, bandAsk{want: r.sec.want(), cursor: r.cursor, above: r.sec.above, proj: r.sec.proj, at: len(tree)})
		} else {
			tree = append(tree, i)
		}
	}
	trees := make([]treeRow, len(tree))
	for i, j := range tree {
		trees[i] = lay.rows[j]
	}
	keepH := body
	for _, a := range asks {
		if a.proj && a.cursor && a.at > 0 {
			// Without sections the cursor would be on the project's
			// last row, just above its section. Its header needs a row
			// of its own, so the rows kept are those of a body one row
			// shorter: a ⚑ row that gives way for it is counted in the
			// header's ⚑ N, and the cursor stays on screen.
			trees[a.at-1].cursor = true
			keepH = max(body-1, 0)
		}
	}
	cur, keep := bandKeep(trees, keepH)
	lay.fit = fitBand(body, len(trees), cur, keep, asks)
	section := func(j int) []sideSlot {
		if !lay.fit.show[j] {
			return nil
		}
		i := lay.band[j]
		out := []sideSlot{{row: i, line: -1}}
		for k := range lay.fit.body[j] {
			out = append(out, sideSlot{row: i, line: k})
		}
		return out
	}
	// The tree's slots: its rows, and under its projects their sections.
	var slots, below []sideSlot
	var hints []int // the slots of the rows with ⚑
	for j, i := 0, 0; i < len(lay.rows); i++ {
		if lay.rows[i].kind != treeSection {
			if lay.rows[i].hint {
				hints = append(hints, len(slots))
			}
			slots = append(slots, sideSlot{row: i, line: -1})
			continue
		}
		switch asks[j].place() {
		case placeAbove:
			lay.above = append(lay.above, section(j)...)
		case placeProj:
			slots = append(slots, section(j)...)
		default:
			below = append(below, section(j)...)
		}
		j++
	}
	top := min(lay.fit.top, len(slots))
	lay.slots = slots[top:min(top+lay.fit.treeH, len(slots))]
	for _, s := range hints {
		if s < top || s >= top+lay.fit.treeH {
			lay.flag = true
		}
	}
	if len(below) > 0 {
		lay.slots = append(lay.slots, sideSlot{row: -1, line: -1})
		lay.slots = append(lay.slots, below...)
	}
	if len(lay.above) > 0 {
		lay.flag = true
	}
	return lay
}

// sideOnScreen says which projects have rows on screen in a sidebar w×h
// with a footer of foot rows: their per-project sections are shown.
func sideOnScreen(all []treeRow, w, h, foot int) func(slug string) bool {
	lay := sideLayout(all, w, h, foot)
	on := map[string]bool{}
	for _, s := range lay.slots {
		if s.row >= 0 && lay.rows[s.row].kind != treeSection {
			on[lay.rows[s.row].slug] = true
		}
	}
	return func(slug string) bool { return on[slug] }
}

// headFlag is the columns of a full sidebar's header cw cells wide
// (sideHead's) that ⚑ N, needs things that need you, takes: [lo, hi),
// the needs-you column's two cells included; both 0 where it doesn't
// fit.
func headFlag(needs, cw int) (lo, hi int) {
	if needs <= 0 {
		return 0, 0
	}
	fw := ansi.StringWidth(fmt.Sprintf("%s %d", ic().hint, needs))
	if cw-ansi.StringWidth(" PROJECTS")-fw-2 < 1 {
		return 0, 0
	}
	return cw - 2 - fw, cw
}

// lastShown is the index in band of the last header shown, -1 for none.
func (l sideLay) lastShown() int {
	for j := len(l.band) - 1; j >= 0; j-- {
		if l.fit.show[j] {
			return j
		}
	}
	return -1
}

// line is slot s drawn cw cells wide (the border not included).
func (l sideLay) line(s sideSlot, cw int, slim, focused bool) string {
	if s.row < 0 {
		return strings.Repeat(" ", cw)
	}
	r := l.rows[s.row]
	if r.kind != treeSection {
		return treeLine(r, cw, slim, focused)
	}
	if s.line >= 0 {
		in := 3 // under the title: " ▾ " in the band
		if r.sec.proj {
			in = projIndent + 2
		}
		in = min(in, cw)
		bw := max(cw-sideGap-in, 0)
		return strings.Repeat(" ", in) + fit(r.sec.lines[s.line], bw) + reset + strings.Repeat(" ", min(sideGap, cw-in))
	}
	j := slices.Index(l.band, s.row)
	more := 0
	if j == l.lastShown() {
		more = l.fit.more
	}
	return sectionHead(r, cw, l.fit.shrunk[j], more, focused)
}

// sectionHead is a section's header row cw cells wide, its gap
// included: " ▾ TICKER ───── terminatr", its fold mark, its title, a
// faint rule and on the right its badge, else unfolded the project its
// tree is for (sectionHeader); under a project, at thread depth and its
// title as the mod wrote it, "    ▾ ci status ──── 1 ☒", without the
// project. The cursor's is highlighted.
func sectionHead(r treeRow, cw int, shrunk bool, more int, focused bool) string {
	hl, sel := treeSel(r, focused)
	gap := strings.Repeat(" ", min(sideGap, cw))
	// The needs-you column's cells stay clear; the header starts with a
	// blank, at the tree's margin.
	pad, word := " ", r.sec.of
	if r.sec.proj {
		pad, word = strings.Repeat(" ", projIndent), ""
	}
	return sectionHeader(r.sec, pad, max(cw-sideGap, 0), 2, shrunk, more, word, hl, sel) + gap
}

// sectionHeader is a mod section's header w cells wide, the band's and
// the right pane's: "▾ TICKER ───── word", its fold mark, its title, a
// faint rule and on the right its badge, else unfolded word (the
// band's project; none in a panel). A folded failed section's badge is
// a red "▲ failed". A section that gave its body up for room (shrunk,
// drawn folded) ends in a faint "…"; the last header
// shown says how many didn't fit (+N). pad leads it; end cells at the
// right stay clear (the sidebar's needs-you column). hl draws it in sel.
func sectionHeader(s *sideSection, pad string, w, end int, shrunk bool, more int, word string, hl bool, sel lipgloss.Style) string {
	mark := ic().unfold
	if s.folded || shrunk {
		mark = ic().fold
	}
	title := strings.ToUpper(s.title)
	if s.proj {
		title = s.title
	}
	left := pad + mark + " " + title
	var right []cellRun
	switch {
	case s.failed && s.folded:
		right = append(right, cellRun{ic().blocked + " failed", th().Bad})
	case len(s.badge) > 0 && !s.failed:
		plain, styled := spansCell(s.badge, spansWidth(s.badge))
		right = append(right, cellRun{plain, spanStyle{plain, styled}})
	case !s.folded && !shrunk && word != "":
		right = append(right, cellRun{word, th().Faint})
	}
	if shrunk {
		right = append(right, cellRun{"…", th().Faint})
	}
	if more > 0 {
		right = append(right, cellRun{fmt.Sprintf("+%d", more), th().Faint})
	}
	for i := 1; i < len(right); i++ {
		right[i].s = " " + right[i].s
	}
	// The badge keeps its cells, the title is cut for it, down to its
	// mark and a few letters; narrower still the badge goes.
	least := min(ansi.StringWidth(pad), 1) + 5 // " ▾ CI…"
	rw := runsWidth(right)
	if rw > 0 && rw+1+end+least > w {
		right, rw = nil, 0
	}
	room := w - rw - end
	if rw == 0 {
		room = w
	}
	head := left
	if n := ansi.StringWidth(left); n > room-1 {
		head = fit(left, max(room-1, 0))
	}
	// A faint rule fills the room between, a blank at either end.
	fill := max(room-ansi.StringWidth(head), 0)
	mid, rule := strings.Repeat(" ", fill), ""
	if fill >= 3 {
		mid, rule = " ", strings.Repeat("─", fill-2)
	}
	if hl {
		plain := head + mid + rule
		if rule != "" {
			plain += " "
		}
		return sel.Render(fit(plain+runsPlain(right), w))
	}
	out := th().Head.Render(head) + mid
	if rule != "" {
		out += th().Faint.Render(rule) + " "
	}
	return fit(out+runsRender(right), w)
}

// sectionTip is what the hover says over a section: its mod.
func sectionTip(s *sideSection) string {
	return s.title + " · " + modWords(s)
}

// modWords name s's mod: "ticker mod (built in)".
func modWords(s *sideSection) string {
	w := s.mod + " mod"
	if s.builtin {
		w += " (built in)"
	}
	return w
}

// sectionDetails are the footer's two lines about a section's header:
// its title and mod, then what it shows (its description), or why it
// failed and its log.
func sectionDetails(s *sideSection) (string, string) {
	l1 := s.title + " · " + modWords(s)
	switch {
	case s.failed && s.log != "":
		return l1, "mod failed · its log: " + s.log
	case s.failed:
		return l1, "mod failed · r in the Mods tab restarts it"
	}
	return l1, s.desc
}

// sectionStep is what a sidebar key op does on section header r: →
// unfolds it, ← folds it, enter opens the view the mod names (opens),
// or folds or unfolds it when it names none; space and p nothing. ok
// is false for an op that does what it does on any row (a move).
func sectionStep(r treeRow, op int) (sideStep, bool) {
	s := r.sec
	fold := func(on bool) sideStep { return sideStep{sections: []string{s.id}, foldOn: on} }
	switch op {
	case sideIn:
		if s.folded {
			return fold(false), true
		}
		return sideStep{}, true
	case sideOut:
		if !s.folded {
			return fold(true), true
		}
		return sideStep{}, true
	case sideEnter:
		if s.opens != "" {
			return sideStep{open: s}, true
		}
		return fold(!s.folded), true
	case sideActive, sidePin:
		return sideStep{}, true
	}
	return sideStep{}, false
}

// sectionIDs are the ids of the sections in rows, at most a
// view.sections call's (the band's first, which a long tree's
// per-project sections would crowd out).
func sectionIDs(rows []treeRow) []string {
	var band, proj []string
	for _, r := range rows {
		switch {
		case r.kind != treeSection:
		case r.sec.proj:
			proj = append(proj, r.sec.id)
		default:
			band = append(band, r.sec.id)
		}
	}
	out := slices.Concat(band, proj)
	return out[:min(len(out), proto.MaxViewSections)]
}

// sectionIDs are the sections st folds or unfolds, the view's folds t
// being what they are: with everySection those t folds too, at most a
// view.sections call's. The right pane's (panel:…) aren't the
// sidebar's.
func (st sideStep) sectionIDs(t view.Tree) []string {
	if !st.everySection {
		return st.sections
	}
	out := slices.Clone(st.sections)
	for _, id := range t.SectionsFolded {
		if !strings.HasPrefix(id, "panel:") && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out[:min(len(out), proto.MaxViewSections)]
}

// sectionClick is the ui.click enter on section s's header sends its
// mod: on the section's view, for its project. The mod's handler may
// then open the view it names (opens), as after a click in its views.
func sectionClick(s *sideSection) proto.ModViewInputParams {
	return proto.ModViewInputParams{Mod: s.mod, View: s.view, Project: s.project, Kind: widget.EventClick}
}

// sectionKeys are the key bar's keys on section header s.
func sectionKeys(s *sideSection) []string {
	enter := barKey("side.enter", "⏎")
	var keys []string
	if s.folded {
		keys = append(keys, barKey("side.unfold", "→")+" unfold")
	} else {
		keys = append(keys, barKey("side.fold", "←")+" fold")
	}
	if s.opens != "" {
		keys = append(keys, enter+" open")
	}
	return keys
}

// sideFootRows are the rows sideFooter takes in a sidebar w wide and h
// tall, as sideLines draws it: none when it doesn't fit.
func sideFootRows(w, h int, s ServerInfo, compact bool) int {
	if w <= sideSlim || w-1-sideGap < 4 {
		return 0
	}
	n := 4
	if compact {
		n = 1
	}
	if versionHint(s) != "" {
		n++
	}
	if h < n+4 {
		return 0
	}
	return n
}

// bandFits says whether a full sidebar h rows tall with a footer of
// foot rows has the body for sections.
func bandFits(w, h, foot int) bool {
	return w > sideSlim && h-foot-sideHeadRows() >= bandMinBody
}
