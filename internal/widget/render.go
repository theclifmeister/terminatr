package widget

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Look is how the console draws: its theme's roles and its icon set.
// The widget set never picks a colour or a glyph itself, so a widget
// follows the theme, the light or dark scheme, NO_COLOR and the icons
// setting like the rest of terminatr.
type Look interface {
	// Style draws s in a role: one of Roles, "sel" for the cursor's row,
	// or "" for plain text.
	Style(role, s string) string
	// Icon is an icon's glyph (one of Icons, or bar.on and bar.off, a
	// progress bar's cells), padded to the cells it draws over.
	Icon(name string) string
	// ASCII tells whether the icon set is plain ASCII, for the bar
	// charts' and sparklines' cells.
	ASCII() bool
}

// line is one laid-out line.
type line struct {
	s string // styled, at most the width
	// For a row: its list and id, and its text unstyled and padded to the
	// width, which the cursor draws in the sel role.
	list, row string
	plain     func() string
	keys      []keyHit // a keys line's hints, for clicks
}

// keyHit is a key hint's cells, [x0, x1).
type keyHit struct {
	x0, x1 int
	key    string
}

// Render draws the view in w cells by h lines: h lines or fewer, each at
// most w cells wide. It scrolls to the cursor if it moved, and keeps the
// scroll within the tree.
func (v *View) Render(w, h int, look Look) []string {
	w, h = max(w, 1), max(h, 0)
	now := v.now()
	if !v.layValid || v.layW != w || v.timed && now.Unix() != v.layAt {
		v.lay = layout(v, w, look, now)
		v.layW, v.layValid, v.layAt = w, true, now.Unix()
	}
	cur := v.cursorLine()
	if v.follow {
		if y := cur; y >= 0 {
			if y < v.top {
				v.top = y
			} else if y >= v.top+h {
				v.top = y - h + 1
			}
			// Keep the list's title and filter in view at its first row.
			if l := v.lists[v.focus]; l != nil && l.idx == 0 {
				for v.top > 0 && y-v.top < h-1 && v.lay[v.top-1].list == "" {
					v.top--
				}
			}
		}
		v.follow = false
	}
	v.top = min(v.top, max(len(v.lay)-h, 0))
	v.shown, v.height = v.top, h
	end := min(v.top+h, len(v.lay))
	out := make([]string, 0, end-v.top)
	for i := v.top; i < end; i++ {
		if i == cur {
			out = append(out, look.Style("sel", v.lay[i].plain()))
			continue
		}
		out = append(out, v.lay[i].s)
	}
	return out
}

// cursorLine is the focused list's cursor's laid-out line, -1 for none.
func (v *View) cursorLine() int {
	l := v.lists[v.focus]
	if l == nil || l.cursor == "" {
		return -1
	}
	for i := range v.lay {
		if v.lay[i].list == v.focus && v.lay[i].row == l.cursor {
			return i
		}
	}
	return -1
}

// layout lays the whole tree out at width w.
// now is the clock time spans are drawn at.
func layout(v *View, w int, look Look, now time.Time) []line {
	if v.tree == nil {
		return nil
	}
	lo := &layer{v: v, w: w, look: look, now: now}
	lo.widget(v.tree)
	return lo.out
}

type layer struct {
	v    *View
	w    int
	look Look
	now  time.Time
	out  []line
}

func (lo *layer) add(s string) { lo.out = append(lo.out, line{s: clip(s, lo.w)}) }

func clip(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func (lo *layer) widget(w *Widget) {
	if w.Title != "" {
		if len(lo.out) > 0 {
			lo.add("")
		}
		lo.add(lo.look.Style("head", w.Title))
	}
	switch w.Kind {
	case KindStack:
		for i := range w.Children {
			lo.widget(&w.Children[i])
		}
	case KindText:
		if w.Text != "" {
			for _, p := range strings.Split(ansi.Wrap(w.Text, lo.w, ""), "\n") {
				lo.add(lo.look.Style(w.Role, p))
			}
		}
		for _, ss := range w.Lines {
			lo.add(lo.spans(ss))
		}
	case KindList, KindTable:
		lo.rows(w)
	case KindKV:
		kw := 0
		for _, p := range w.Pairs {
			kw = max(kw, ansi.StringWidth(p.Key))
		}
		kw = min(kw, 24, lo.w/3)
		for _, p := range w.Pairs {
			lo.add(lo.look.Style("faint", pad(clip(p.Key, kw), kw, false)) + "  " + lo.spans(p.Value))
		}
	case KindProgress:
		lo.progress(w)
	case KindSteps:
		for _, s := range w.Steps {
			var g string
			switch s.State {
			case StepDone:
				g = lo.look.Style("good", lo.look.Icon("todo.done"))
			case StepNow:
				g = lo.look.Style("warn", lo.look.Icon("todo.now"))
			default:
				g = lo.look.Style("faint", lo.look.Icon("todo.open"))
			}
			lo.add(g + " " + s.Text)
		}
	case KindBars:
		lo.bars(w)
	case KindKeys:
		lo.keys(w.Keys)
	}
}

// spans draws spans in their roles.
func (lo *layer) spans(ss []Span) string {
	var b strings.Builder
	for _, s := range ss {
		if s.Icon != "" {
			b.WriteString(lo.look.Style(s.Role, lo.look.Icon(s.Icon)))
		}
		if t := s.Words(lo.now); t != "" {
			b.WriteString(lo.look.Style(s.Role, t))
		}
	}
	return b.String()
}

// plainSpans is spans unstyled, icons included.
func (lo *layer) plainSpans(ss []Span) string {
	var b strings.Builder
	for _, s := range ss {
		if s.Icon != "" {
			b.WriteString(lo.look.Icon(s.Icon))
		}
		b.WriteString(s.Words(lo.now))
	}
	return b.String()
}

// pad pads s with blanks to w cells, on the left when right.
func pad(s string, w int, right bool) string {
	n := max(w-ansi.StringWidth(s), 0)
	if right {
		return strings.Repeat(" ", n) + s
	}
	return s + strings.Repeat(" ", n)
}

// rows lays out a list or table: its filter line, a table's header, then
// the rows that pass the filter.
func (lo *layer) rows(w *Widget) {
	l := lo.v.lists[w.ID]
	if l == nil {
		return
	}
	if l.filter != "" || lo.v.editing && lo.v.focus == w.ID {
		f := lo.look.Style("accent", "/") + " " + l.filter
		if lo.v.editing && lo.v.focus == w.ID {
			f += lo.look.Style("faint", "_")
		}
		lo.add(f)
	}
	var widths []int
	if w.Kind == KindTable {
		widths = lo.columnWidths(w, l.shown)
		var hs []string
		for i, c := range w.Columns {
			hs = append(hs, pad(clip(c.Title, widths[i]), widths[i], c.Right))
		}
		// A table whose columns have no titles has no header.
		if h := strings.TrimRight(strings.Join(hs, "  "), " "); h != "" {
			lo.add(lo.look.Style("head", h))
		}
	}
	drawn := l.drawn()
	if len(drawn) == 0 {
		switch {
		case l.filter != "":
			lo.add(lo.look.Style("faint", "no match"))
		case w.Empty != "":
			lo.add(lo.look.Style("faint", w.Empty))
		}
		return
	}
	for n, i := range drawn {
		r := &w.Rows[i]
		if r.Head {
			// A section rule: the head's spans, then ─ to the width.
			if n > 0 {
				lo.add("")
			}
			head := lo.spans(r.Spans)
			lo.add(head + " " + lo.look.Style("faint", strings.Repeat("─", max(lo.w-ansi.StringWidth(head)-1, 0))))
			continue
		}
		// The unstyled text is what the cursor draws in the sel role: made
		// when the cursor is on the row, which a move may do without a
		// new layout.
		plain := func() string {
			if w.Kind != KindTable {
				return pad(clip(lo.plainSpans(r.Spans), lo.w), lo.w, false)
			}
			ps := make([]string, 0, len(w.Columns))
			for j, c := range w.Columns {
				var cell []Span
				if j < len(r.Cells) {
					cell = r.Cells[j]
				}
				ps = append(ps, pad(clip(lo.plainSpans(cell), widths[j]), widths[j], c.Right))
			}
			return pad(clip(strings.Join(ps, "  "), lo.w), lo.w, false)
		}
		var s string
		if w.Kind == KindTable {
			cs := make([]string, 0, len(w.Columns))
			for j, c := range w.Columns {
				var cell []Span
				if j < len(r.Cells) {
					cell = r.Cells[j]
				}
				cs = append(cs, pad(clip(lo.spans(cell), widths[j]), widths[j], c.Right))
			}
			s = strings.Join(cs, "  ")
		} else {
			s = lo.spans(r.Spans)
		}
		lo.out = append(lo.out, line{s: clip(s, lo.w), list: w.ID, row: r.ID, plain: plain})
		// The cursor's row's lines under it; a click on one picks the row.
		if r.ID == l.cursor {
			for _, m := range r.More {
				lo.out = append(lo.out, line{s: clip(lo.spans(m), lo.w), list: w.ID, row: r.ID, plain: constText(pad(clip(lo.plainSpans(m), lo.w), lo.w, false))})
			}
		}
	}
}

// columnWidths sizes a table's columns: a set width, else the widest of
// its title and cells; then, past the width, the widest shrink first,
// down to 3 cells.
func (lo *layer) columnWidths(w *Widget, shown []int) []int {
	ws := make([]int, len(w.Columns))
	fixed := make([]bool, len(w.Columns))
	for i, c := range w.Columns {
		if c.Width > 0 {
			ws[i], fixed[i] = c.Width, true
			continue
		}
		ws[i] = ansi.StringWidth(c.Title)
		for _, r := range shown {
			if cells := w.Rows[r].Cells; i < len(cells) {
				ws[i] = max(ws[i], ansi.StringWidth(lo.plainSpans(cells[i])))
			}
		}
		ws[i] = min(max(ws[i], 1), 60)
	}
	total := 2 * (len(ws) - 1)
	for _, x := range ws {
		total += x
	}
	// At most 12 columns of at most 200 cells: a cell at a time is cheap.
	for total > lo.w {
		wi := -1
		for i, x := range ws {
			if !fixed[i] && x > 3 && (wi < 0 || x > ws[wi]) {
				wi = i
			}
		}
		if wi < 0 {
			break // nothing left to shrink: the lines clip
		}
		ws[wi]--
		total--
	}
	return ws
}

// progress is "label  ▰▰▰▱▱▱  42%".
func (lo *layer) progress(w *Widget) {
	m := w.Max
	if m <= 0 {
		m = 100
	}
	frac := min(max(w.Value/m, 0), 1)
	pct := strconv.Itoa(int(math.Floor(frac*100))) + "%"
	label := ""
	if w.Label != "" {
		label = clip(w.Label, max(lo.w/2, 1)) + "  "
	}
	bw := min(max(lo.w-ansi.StringWidth(label)-2-len(pct), 5), 30)
	on := int(math.Round(frac * float64(bw)))
	role := w.Role
	if role == "" {
		role = "accent"
	}
	lo.add(label + lo.look.Style(role, strings.Repeat(lo.look.Icon("bar.on"), on)) +
		lo.look.Style("faint", strings.Repeat(lo.look.Icon("bar.off"), bw-on)) + "  " + pct)
}

// The cells of a sparkline, low to high.
var (
	sparkUnicode = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	sparkASCII   = []string{"_", ".", "-", ":", "=", "+", "*", "#"}
)

// bars is a sparkline (the last values that fit), or one bar a value,
// labelled, scaled to the largest.
func (lo *layer) bars(w *Widget) {
	top := 0.0
	for _, x := range w.Values {
		top = max(top, x)
	}
	if w.Spark {
		cells := sparkUnicode
		if lo.look.ASCII() {
			cells = sparkASCII
		}
		vs := w.Values[max(len(w.Values)-lo.w, 0):]
		var b strings.Builder
		for _, x := range vs {
			i := 0
			if top > 0 {
				i = min(int(x/top*float64(len(cells)-1)+0.5), len(cells)-1)
			}
			b.WriteString(cells[i])
		}
		if b.Len() > 0 {
			lo.add(lo.look.Style("accent", b.String()))
		}
		return
	}
	lw, nw := 0, 0
	nums := make([]string, len(w.Values))
	for i, x := range w.Values {
		nums[i] = number(x)
		nw = max(nw, len(nums[i]))
		if i < len(w.Labels) {
			lw = max(lw, ansi.StringWidth(w.Labels[i]))
		}
	}
	lw = min(lw, lo.w/3)
	block := "█"
	if lo.look.ASCII() {
		block = "#"
	}
	room := max(lo.w-nw-1-(lw+1)*min(lw, 1), 1)
	for i, x := range w.Values {
		var s string
		if lw > 0 {
			label := ""
			if i < len(w.Labels) {
				label = w.Labels[i]
			}
			s = lo.look.Style("faint", pad(clip(label, lw), lw, false)) + " "
		}
		n := 0
		if top > 0 {
			n = int(math.Round(x / top * float64(room)))
		}
		if n == 0 && x > 0 {
			n = 1
		}
		s += lo.look.Style("accent", strings.Repeat(block, n)) + strings.Repeat(" ", room-n) + " " + pad(nums[i], nw, true)
		lo.add(s)
	}
}

// number is a value as a chart labels it: whole numbers without a point.
func number(x float64) string {
	if x == math.Trunc(x) && x < 1e15 {
		return strconv.FormatFloat(x, 'f', 0, 64)
	}
	return strconv.FormatFloat(x, 'f', 1, 64)
}

// keys is the hint line "key label · key label", keys in the accent role,
// labels faint, each hint clickable.
func (lo *layer) keys(ks []Key) {
	if len(ks) == 0 {
		return
	}
	var b strings.Builder
	var hits []keyHit
	x := 0
	sep := " · "
	ks = slices.DeleteFunc(slices.Clone(ks), func(k Key) bool { return lo.v.coreKey(k.Key) })
	if len(ks) == 0 {
		return
	}
	for i, k := range ks {
		if i > 0 {
			b.WriteString(lo.look.Style("faint", sep))
			x += len(sep) - 1 // the dot is one cell
		}
		name := k.Key
		hint := lo.look.Style("accent", name)
		cells := len(name)
		if k.Label != "" {
			hint += " " + lo.look.Style("faint", k.Label)
			cells += 1 + ansi.StringWidth(k.Label)
		}
		if x+cells > lo.w {
			break
		}
		b.WriteString(hint)
		hits = append(hits, keyHit{x, x + cells, k.Key})
		x += cells
	}
	lo.out = append(lo.out, line{s: clip(b.String(), lo.w), keys: hits})
}

func constText(s string) func() string { return func() string { return s } }
