package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

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
// tree, read-only: no cursor, no keys. The view keeps the folds
// (view.Tree.SectionsFolded, view.sections). The band takes at most half
// the sidebar's body, its headers a quarter, and gives way, bodies first
// and then headers, the bottom section first, so that every row with ⚑,
// the cursor's and the one you are on show as they would without it
// (fitBand). The slim strip and the hidden sidebar have no sections, and
// a sidebar body under bandMinBody rows has none either.

// bandMinBody is the fewest rows of the sidebar's body (its rows less
// the header and the footer) that show sections.
const bandMinBody = 12

// sideSection is a band section, as its header row carries it.
type sideSection struct {
	// id is its fold's id, mod.view (view.Tree.SectionsFolded).
	id              string
	mod, view       string
	title, name     string
	desc, opens     string
	log             string // a failed mod's log
	builtin         bool
	project         string // the project it shows for: enter's target
	of              string // the project of the tree it shows, "" for all
	badge           []widget.Span
	lines           []string // the body unfolded, as drawn
	folded, waiting bool     // the user folded it; the mod drew nothing yet
	failed          bool
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
}

// bandWidth is a band section's body width in a sidebar w columns wide:
// less the border, the gap, the margin and a 2-cell indent.
func bandWidth(w int) int { return max(w-1-sideGap-3, 1) }

// bandViews are the band's sections the console shows for in.project,
// in mod order: in safe mode a user mod's don't (its host doesn't run).
func bandViews(in bandIn) []proto.ModView {
	if in.w <= sideSlim {
		return nil
	}
	var out []proto.ModView
	for _, v := range sections(in.list, mod.SlotSidebarSection, in.project) {
		if in.noMods && !builtinMod(in.mods, v.Mod) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// builtinMod says whether mod id is a built-in mod.
func builtinMod(mods []mod.Mod, id string) bool {
	i := slices.IndexFunc(mods, func(m mod.Mod) bool { return m.ID == id })
	return i >= 0 && mods[i].Builtin
}

// bandRows are the band's header rows, with their sections: a failed
// mod's says so, one that drew nothing yet waits, and one whose tree
// for the project (and for all) is null adds nothing.
func bandRows(in bandIn) []treeRow {
	var out []treeRow
	w := bandWidth(in.w)
	for _, v := range bandViews(in) {
		s := &sideSection{id: v.Mod + "." + v.View, mod: v.Mod, view: v.View, title: v.Title, name: v.Name,
			desc: v.Description, opens: v.Opens, log: v.Log, builtin: builtinMod(in.mods, v.Mod), project: in.project}
		s.folded = in.folds.IsSectionFolded(s.id)
		switch t, drew := bandTree(v, in.project); {
		case v.State == proto.ModViewFailed:
			s.failed = true
			s.lines = []string{th().Bad.Render(fit(ic().blocked+" mod failed: "+cmp.Or(v.Err, "its host stopped"), w)) + reset,
				th().Faint.Render(fit("r in the Mods tab restarts it", w)) + reset}
		case t == nil && drew:
			continue // null: nothing to say here
		case t == nil:
			s.waiting = true
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

// bandTree is the tree v's band section shows for project: the
// project's, else the one for all projects; nil with drew true when the
// mod published only null ones, nil with drew false while it published
// none.
func bandTree(v proto.ModView, project string) (t *modview.Tree, drew bool) {
	var all *modview.Tree
	for i := range v.Trees {
		tr := &v.Trees[i]
		switch {
		case tr.Task != "" || tr.Thread != "":
			continue
		case tr.Project == project && project != "":
			drew = true
			if tr.Tree != nil {
				return tr, true
			}
		case tr.Project == "":
			drew = true
			if tr.Tree != nil {
				all = tr
			}
		}
	}
	return all, drew
}

// bandShown are the band's sections as mod.views is told of them, so
// their mods hear view.opened: every one the console would draw, folded
// or not (a folded header still shows its badge), and those with a null
// tree, which may publish another.
func bandShown(in bandIn) []modview.Shown {
	var out []modview.Shown
	for _, v := range bandViews(in) {
		def, most := modview.Heights(mod.SlotSidebarSection)
		h := def
		if t, _ := bandTree(v, in.project); t != nil && t.Height > 0 {
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
	for k := range c.hosts {
		if !keep(k) {
			delete(c.hosts, k)
		}
	}
}

// The budget.

// bandAsk is what a band section asks of the budget: its body lines
// (none folded), and whether its header is the cursor's row.
type bandAsk struct {
	want   int
	cursor bool
}

// bandFit is how the band fits a sidebar body: the rows the tree keeps,
// and per section whether its header shows, its body lines, and whether
// it gave its body up for room (shrunk); more is how many headers
// didn't fit, which the last one shown says (+N).
type bandFit struct {
	treeH  int
	show   []bool
	body   []int
	shrunk []bool
	more   int
}

// used is the rows the band takes: a blank row above it, the headers
// shown and their bodies.
func (f bandFit) used() int {
	n := 0
	for j, on := range f.show {
		if on {
			n += 1 + f.body[j]
		}
	}
	if n > 0 {
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

// fitBand fits the band's sections asks under a tree of n rows in a
// sidebar body of b rows: the budget (the T348 design §4) and the
// safety guarantee (§5) in one. cur is the tree row the tree scrolls to
// (the cursor's, else the one you are on; -1 for none), and keep the
// tree rows that must stay on screen: those with ⚑, the cursor's and
// the one you are on, among the rows the tree shows without sections
// (bandKeep).
//
// Each section gets its body lines, a folded one its header. The band
// takes at most half the body, its headers a quarter; under bandMinBody
// rows nothing. Then, while a row of keep would scroll off the tree's
// rows left, the bodies give way, the bottom section's first, then the
// headers, the cursor's last. The rows left are the tree's.
func fitBand(b, n, cur int, keep []int, asks []bandAsk) bandFit {
	f := bandFit{treeH: max(b, 0), show: make([]bool, len(asks)), body: make([]int, len(asks)), shrunk: make([]bool, len(asks))}
	if b < bandMinBody || len(asks) == 0 {
		return f
	}
	for j, a := range asks {
		f.show[j], f.body[j] = true, min(max(a.want, 0), modview.MaxSidebarHeight)
	}
	shrink := func() bool {
		for j := len(asks) - 1; j >= 0; j-- {
			if f.show[j] && f.body[j] > 0 {
				f.body[j], f.shrunk[j] = 0, true
				return true
			}
		}
		return false
	}
	drop := func(cursor bool) bool {
		for j := len(asks) - 1; j >= 0; j-- {
			switch {
			case !f.show[j]:
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
	for f.headers() > b/4 && drop(false) {
	}
	for f.used() > b/2 && (shrink() || drop(false) || drop(true)) {
	}
	safe := func() bool {
		h := b - f.used()
		if h >= n {
			return true
		}
		top := scrollTop(cur, max(h, 1), n)
		for _, k := range keep {
			if k < top || k >= top+h {
				return false
			}
		}
		return true
	}
	for !safe() && (shrink() || drop(false) || drop(true)) {
	}
	f.treeH = b - f.used()
	for j := range asks {
		if !f.show[j] {
			f.more++
			f.shrunk[j] = false
		}
	}
	return f
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

// sideLay is a sidebar's layout: its shown rows, the rows above the
// tree (head), its body's rows in order, and how the band fit.
type sideLay struct {
	rows  []treeRow
	head  int
	slots []sideSlot
	fit   bandFit
	band  []int // the rows' indexes of the band's headers, in fit's order
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
// sideLines draws it): the tree scrolled so the cursor's row shows,
// then the band (fitBand).
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
	for i, r := range lay.rows {
		if r.kind == treeSection {
			lay.band = append(lay.band, i)
		} else {
			tree = append(tree, i)
		}
	}
	trees := make([]treeRow, len(tree))
	for i, j := range tree {
		trees[i] = lay.rows[j]
	}
	cur, keep := bandKeep(trees, body)
	asks := make([]bandAsk, len(lay.band))
	for j, i := range lay.band {
		r := lay.rows[i]
		asks[j] = bandAsk{want: r.sec.want(), cursor: r.cursor}
	}
	lay.fit = fitBand(body, len(trees), cur, keep, asks)
	top := scrollTop(cur, max(lay.fit.treeH, 1), len(trees))
	for i := top; i < min(top+lay.fit.treeH, len(trees)); i++ {
		lay.slots = append(lay.slots, sideSlot{row: tree[i], line: -1})
	}
	if lay.fit.headers() > 0 {
		lay.slots = append(lay.slots, sideSlot{row: -1, line: -1})
	}
	for j, i := range lay.band {
		if !lay.fit.show[j] {
			continue
		}
		lay.slots = append(lay.slots, sideSlot{row: i, line: -1})
		for k := range lay.fit.body[j] {
			lay.slots = append(lay.slots, sideSlot{row: i, line: k})
		}
	}
	return lay
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
		bw := max(cw-sideGap-3, 0)
		return "   " + fit(r.sec.lines[s.line], bw) + reset + strings.Repeat(" ", min(sideGap, cw))
	}
	j := slices.Index(l.band, s.row)
	more := 0
	if j == l.lastShown() {
		more = l.fit.more
	}
	return sectionHead(r, cw, l.fit.shrunk[j], more, focused)
}

// sectionHead is a band section's header row cw cells wide, its gap
// included: " ▾ TICKER ───── terminatr", its fold mark, its title, a
// faint rule and on the right its badge, else unfolded the project its
// tree is for. A section that drew nothing yet, or gave its body up for
// room (shrunk, drawn folded), ends in a faint "…"; the last header
// shown says how many didn't fit (+N). The cursor's is highlighted.
func sectionHead(r treeRow, cw int, shrunk bool, more int, focused bool) string {
	s := r.sec
	hl, sel := treeSel(r, focused)
	w := max(cw-sideGap, 0)
	mark := ic().unfold
	if s.folded || shrunk {
		mark = ic().fold
	}
	left := " " + mark + " " + strings.ToUpper(s.title)
	var right []cellRun
	switch {
	case len(s.badge) > 0 && !s.failed:
		plain, styled := spansCell(s.badge, spansWidth(s.badge))
		right = append(right, cellRun{plain, spanStyle{plain, styled}})
	case !s.folded && !shrunk && s.of != "":
		right = append(right, cellRun{s.of, th().Faint})
	}
	if s.waiting || shrunk {
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
	const end, least = 2, 6 // the needs-you column's cells; " ▾ CI…"
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
	gap := strings.Repeat(" ", min(sideGap, cw))
	if hl {
		plain := head + mid + rule
		if rule != "" {
			plain += " "
		}
		return sel.Render(fit(plain+runsPlain(right), w)) + gap
	}
	out := th().Head.Render(head) + mid
	if rule != "" {
		out += th().Faint.Render(rule) + " "
	}
	return fit(out+runsRender(right), w) + gap
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
	case s.waiting:
		return l1, "waiting for the mod to draw it"
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

// sectionIDs are the ids of the band's sections in rows.
func sectionIDs(rows []treeRow) []string {
	var out []string
	for _, r := range rows {
		if r.kind == treeSection {
			out = append(out, r.sec.id)
		}
	}
	return out
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
