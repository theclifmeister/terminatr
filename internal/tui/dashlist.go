package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The dashboard's list: its rows drawn in sections, scrolled, in the
// frame of its header and footer.

func (m *dash) renderList() string {
	if m.over != nil {
		return m.frame("", m.base(), -1, "")
	}
	return m.frame(m.shownCrumb(), m.listBody(), -1, m.footKeys())
}

// listBody is the list (and the details panel beside or under it) as
// the body's rows, the list scrolled so the selected row shows.
func (m *dash) listBody() []string {
	if m.remoteHere() {
		return m.remoteBody()
	}
	g := m.geom()
	ls, dr, place := g.list(), g.details(), g.panel()
	f := m.listLines(ls.w, place == "")
	top := scrollTop(f.sel, ls.h, len(f.lines))
	list := make([]string, ls.h)
	copy(list, f.lines[min(top, len(f.lines)):])
	if place == "" {
		return list
	}
	// The list scrolls on its own; the details panel shows the selected
	// row from the top.
	var panel []string
	m.bodyLay = nil
	if r, ok := m.selected(); ok {
		lay := m.detailsLay(r, dr.w, dr.h)
		panel, m.bodyLay = lay.lines, &lay
	}
	if m.detailKey == m.sel {
		// Scrolled with the wheel.
		m.detailTop = min(m.detailTop, max(len(panel)-dr.h, 0))
		panel = panel[m.detailTop:]
	}
	panel = append(panel, make([]string, max(dr.h-len(panel), 0))...)[:dr.h]
	// The details panel's divider is in the accent colour while the
	// panel has the keyboard.
	st := th().Faint
	if m.focus == areaDetails {
		st = th().Accent
	}
	if place == placeBottom {
		body := make([]string, 0, g.body().h)
		for _, l := range list {
			body = append(body, fit(l, ls.w)+reset)
		}
		body = append(body, st.Render(strings.Repeat("─", dr.w)))
		for _, d := range panel {
			body = append(body, fit(d, dr.w)+reset)
		}
		return body
	}
	div := st.Render("│")
	body := make([]string, ls.h)
	for i := range body {
		body[i] = fit(list[i], ls.w) + reset + div + fit(panel[i], dr.w) + reset
	}
	return body
}

// listFrame is the laid-out list: its lines, the row key on each (for
// the mouse), where a project header's state label sits on its line
// (stateAt), and the selected line.
type listFrame struct {
	lines, keys []string
	labels      map[int][2]int // line -> label's cells x0, x1
	sel         int
}

// listLines lays out the list w cells wide. With inline set, a
// selected thread's details show under its row, unless the rows are
// compact (enter and the popup show them).
func (m *dash) listLines(w int, inline bool) listFrame {
	f := listFrame{sel: -1}
	inline = inline && !m.layout.compact()
	add := func(l, key string) {
		f.lines = append(f.lines, l)
		f.keys = append(f.keys, key)
	}
	for _, r := range m.rows {
		switch {
		case r.card:
			if v, ok := m.mv.card(r.key); ok {
				selected := r.key == m.sel
				for i, l := range m.cardLines(v, w, selected) {
					if i == 0 && selected {
						f.sel = len(f.lines)
					}
					add(l, r.key)
				}
			}
		case r.head == "NEEDS YOU" || r.head == "OTHER SESSIONS":
			add(m.ruleIn(countLabel(r.head, r.count), sectionStyle(r.head), w), "")
		case r.head != "":
			// A project's own section is headed by its name, its state
			// label at the right, which a click toggles (stateAt).
			head, x0, x1 := projectHeadAt(r, w)
			if x1 > x0 {
				if f.labels == nil {
					f.labels = map[int][2]int{}
				}
				f.labels[len(f.lines)] = [2]int{x0, x1}
			}
			add(head, stateKey+r.head)
		case r.key != "" && r.key == m.sel:
			f.sel = len(f.lines)
			st := th().Sel
			if m.area() != areaMain {
				st = st.Faint(true) // the keyboard is elsewhere
			}
			add(st.Render(fit(r.text(w), w)), r.key)
			if inline && r.thread != nil && strings.HasPrefix(r.key, "th:") {
				for _, l := range threadDetail(r.thread, "        ", true) {
					add(fit(l, w)+reset, r.key)
				}
			}
		default:
			add(r.styled(w), r.key)
		}
	}
	return f
}

// frame draws the header, the body scrolled so line sel shows, and the
// footer, by the layout's styles (chrome.go): keys are the footer's key
// hints.
func (m *dash) frame(title string, body []string, sel int, keys string) string {
	defer func() { m.bodyLay = nil }()
	g := m.geom()
	right, alarm, badge := m.headRight()
	// The app, not a project: the project's own section is headed by its
	// slug, which may well be "terminatr".
	left := th().Title.Render(" tm dashboard")
	if m.over != nil {
		left = th().Title.Render(" tm") + th().Faint.Render(" "+oneLine(m.over.Title))
	}
	if title != "" {
		left += th().Faint.Render(" · ") + th().Head.Render(title)
	}
	switch {
	case m.prefixed:
		keys = prefixHint()
	case m.area() == areaSide && m.top() == nil:
		// The sidebar's own keys are in its key bar.
		keys = "esc back to the list · tab next area"
	case m.area() == areaDetails && m.top() == nil:
		keys = "details: ↑ ↓ scroll · esc back · tab next"
		// The panel listBody just laid out for this frame, if it did.
		lay, ok := panelLay{}, false
		if m.bodyLay != nil {
			lay, ok = *m.bodyLay, true
		} else {
			lay, ok = m.detailsNow()
		}
		if ok {
			if sk := panelHintKeys(lay, m.detSelNow()); sk != "" {
				keys = "details: ↑ ↓ scroll · " + sk + " · esc back · tab next"
			}
		}
	}
	foot := m.footLines(g, keys)

	room := g.body().h
	top := scrollTop(sel, room, len(body))
	end := min(top+room, len(body))
	rows := append([]string(nil), body[top:end]...)
	for i := end - top; i < room; i++ {
		rows = append(rows, "")
	}
	var out []string
	shown := right // the header's right part, as drawn
	if g.headRows() > 0 {
		out = append(out, plainUnder(fit(left, max(m.w-ansi.StringWidth(right)-1, 1))+reset+" "+right))
	} else if shown = alarm; alarm != "" {
		rows[0] = overRight(rows[0], alarm, m.w)
	}
	m.badgeAt = [2]int{}
	if badge != "" {
		x := max(m.w-ansi.StringWidth(shown)-1, 1) + 1
		m.badgeAt = [2]int{x, x + ansi.StringWidth(badge)}
	}
	if g.footRows() == 0 {
		msg := m.footMsg()
		if m.prefixed {
			msg = " " + keysLine(keys)
		}
		if msg != "" {
			rows[len(rows)-1] = overLeft(rows[len(rows)-1], msg, m.w)
		}
	}
	if m.over == nil { // over a session, the rows are its screen
		for i, r := range rows {
			rows[i] = plainUnder(r)
		}
	}
	out = append(out, rows...)
	for _, f := range foot {
		out = append(out, plainUnder(f))
	}
	return strings.Join(out, "\n")
}

// scrollTop is the first of n lines to show in room rows so that line
// sel shows.
func scrollTop(sel, room, n int) int {
	top := 0
	if sel >= room {
		top = sel - room + 1
	}
	if top > 0 && top+room > n {
		top = max(n-room, 0)
	}
	return top
}

// stateKey marks the project header's line among listLines' keys: no
// row to select, but its state label takes a click.
const stateKey = "state:"

// stateLabel is a project header's state label: "● active — space to
// deactivate", or "⊘ inactive — space to activate"; short leaves the
// hint out.
func stateLabel(active, short bool) (string, lipgloss.Style) {
	label, st := ic().working+" active — space to deactivate", th().Good
	if !active {
		label, st = ic().inactive+" inactive — space to activate", th().Faint
	}
	if short {
		label, _, _ = strings.Cut(label, " — ")
	}
	return label, st
}

// projectHead is the project section's header w cells wide: its name,
// ∥ paused while paused, a faint rule, and at the right its state label
// (docs/SPEC.md §4), shortened, then left out, when narrow. With
// compact rows its task counts come before the label, which gives way
// first.
func projectHead(r row, w int) string {
	line, _, _ := projectHeadAt(r, w)
	return line
}

// projectHeadAt is projectHead with the cells x0 to x1 its state label
// fills; none (x1 == x0) when the header shows no label.
func projectHeadAt(r row, w int) (line string, x0, x1 int) {
	left := " " + th().Title.Render(countLabel(r.head, r.count))
	if r.paused {
		left += "  " + th().Warn.Render(ic().paused+" paused")
	}
	lw := ansi.StringWidth(left)
	if r.headNote == "" {
		return left + " " + th().Faint.Render(strings.Repeat("─", max(w-lw-1, 0))), 0, 0
	}
	tasks := ""
	if r.headTasks != "" {
		tasks = " " + th().Faint.Render(r.headTasks)
	}
	for _, t := range []string{tasks, ""} {
		for _, short := range []bool{false, true} {
			label, st := stateLabel(r.headNote == "active", short)
			if fill := w - lw - ansi.StringWidth(t) - ansi.StringWidth(label) - 3; fill >= 3 {
				x0 = lw + 1 + fill + ansi.StringWidth(t) + 1
				return left + " " + th().Faint.Render(strings.Repeat("─", fill)) + t + " " + st.Render(label) + " ", x0, x0 + ansi.StringWidth(label)
			}
		}
		if fill := w - lw - ansi.StringWidth(t) - 2; t != "" && fill >= 3 {
			return left + " " + th().Faint.Render(strings.Repeat("─", fill)) + t + " ", 0, 0
		}
	}
	return fit(left, w), 0, 0
}

// stateAt is the project whose state label is at body cell (x, y) of the
// list, "" for none.
func (m *dash) stateAt(x, y int) string {
	g := m.geom()
	b := g.list()
	if !b.has(x+b.x, y) {
		return ""
	}
	f := m.listLines(b.w, g.panel() == "")
	i := scrollTop(f.sel, b.h, len(f.keys)) + y - b.y
	if i >= len(f.keys) {
		return ""
	}
	slug, ok := strings.CutPrefix(f.keys[i], stateKey)
	if !ok {
		return ""
	}
	if l, ok := f.labels[i]; ok && x >= l[0] && x < l[1] {
		return slug
	}
	return ""
}

// projectActive says whether slug is active.
func (m *dash) projectActive(slug string) bool {
	p := m.projectData(slug)
	return p != nil && !p.inactive()
}

// rule is a section header drawn across the width: "NEEDS YOU ───…".
func (m *dash) rule(title string) string { return m.ruleIn(title, th().Title, m.w) }

// ruleIn is a rule w cells wide with the title in st, a cell in: the
// dashboard's gutter.
func (m *dash) ruleIn(title string, st lipgloss.Style, w int) string {
	if title == "" {
		return th().Faint.Render(strings.Repeat("─", w))
	}
	return " " + sectionRule(title, st, w-1)
}

// sectionRule is a section heading w cells wide: the title in st, then
// a faint rule to the edge (docs/STYLE.md, Section).
func sectionRule(title string, st lipgloss.Style, w int) string {
	t := title + " "
	return st.Render(title) + " " + th().Faint.Render(strings.Repeat("─", max(w-len([]rune(t)), 0)))
}

// sectionStyle is how a section heading is drawn: bold, in the warn
// colour for NEEDS YOU, else uncoloured (docs/STYLE.md C2).
func sectionStyle(title string) lipgloss.Style {
	if strings.HasPrefix(title, "NEEDS YOU") {
		return th().Warn.Bold(true)
	}
	return th().Head
}
