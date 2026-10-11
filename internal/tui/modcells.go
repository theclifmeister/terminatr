package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Mods' cells (mods design §4 and §6; the server's, session.list's Mods
// and each session's Cells): spans a code mod published into its columns
// on the sidebar's and the dashboard's rows, and into the status bar.
// They are drawn here, in this console's theme and icon set and clipped
// to the width tm gives them; a cell never carries terminal bytes.

// statusModMax is the most cells a mod's status-bar segment takes.
const statusModMax = 24

// modCells are the cells a poll brought, looked up by column and target.
type modCells struct {
	cols   []proto.ModColumn
	cells  map[cellKey][]widget.Span
	status []proto.ModStatus
}

type cellKey struct {
	col    string // "<mod>.<column>"
	target proto.CellTarget
}

// newModCells indexes m and the sessions' own cells; nil when there are
// none.
func newModCells(m *proto.ModCells, sessions []proto.SessionInfo) *modCells {
	mc := &modCells{cells: map[cellKey][]widget.Span{}}
	add := func(c proto.ModCell) {
		mc.cells[cellKey{c.Mod + "." + c.Column, c.Target}] = c.Spans
	}
	if m != nil {
		mc.cols, mc.status = m.Columns, m.Status
		for _, c := range m.Cells {
			add(c)
		}
	}
	for _, s := range sessions {
		for _, c := range s.Cells {
			add(c)
		}
	}
	if len(mc.cols) == 0 && len(mc.status) == 0 {
		return nil
	}
	return mc
}

// cell is column col's spans for the first of targets that has some.
func (mc *modCells) cell(col string, targets ...proto.CellTarget) []widget.Span {
	if mc == nil {
		return nil
	}
	for _, t := range targets {
		if ss, ok := mc.cells[cellKey{col, t}]; ok {
			return ss
		}
	}
	return nil
}

// in is the columns shown in slot, in mod order.
func (mc *modCells) in(slot string) []proto.ModColumn {
	if mc == nil {
		return nil
	}
	var out []proto.ModColumn
	for _, c := range mc.cols {
		if slices.Contains(c.Where, slot) {
			out = append(out, c)
		}
	}
	return out
}

// shows is whether column col shows in slot.
func (mc *modCells) shows(col, slot string) bool {
	if mc == nil {
		return false
	}
	i := slices.IndexFunc(mc.cols, func(c proto.ModColumn) bool { return c.ID == col })
	return i >= 0 && slices.Contains(mc.cols[i].Where, slot)
}

// sideMods are the sidebar's mod columns (sideCols): those shown in one
// of the sidebar's slots.
func (mc *modCells) sideMods() map[string]modColumn {
	if mc == nil {
		return nil
	}
	out := map[string]modColumn{}
	for _, c := range mc.cols {
		for _, w := range c.Where {
			if strings.HasPrefix(w, "sidebar.") {
				out[c.ID] = modColumn{width: c.Width, title: c.Title}
				break
			}
		}
	}
	return out
}

// sideOrder is the sidebar's column order with the mod columns: those it
// names stay where it puts them; the others go before the state column,
// in mod order, so a mod column shows without a layout naming it.
func (mc *modCells) sideOrder(order []string) []string {
	mods := mc.sideMods()
	if len(mods) == 0 {
		return order
	}
	if len(order) == 0 {
		order = view.DefaultColumns
	}
	var add []string
	for _, c := range mc.cols {
		if _, ok := mods[c.ID]; ok && !slices.Contains(order, c.ID) {
			add = append(add, c.ID)
		}
	}
	if len(add) == 0 {
		return order
	}
	i := slices.Index(order, view.ColState)
	if i < 0 {
		i = len(order)
	}
	return slices.Concat(order[:i:i], add, order[i:])
}

// roleStyle is a span role's style in this console's theme.
func roleStyle(role string) lipgloss.Style {
	t := th()
	switch role {
	case "accent":
		return t.Accent
	case "good":
		return t.Good
	case "warn":
		return t.Warn
	case "bad":
		return t.Bad
	case "info":
		return t.Info
	case "faint":
		return t.Faint
	case "head":
		return t.Head
	case "sel":
		return t.Sel
	}
	return t.Plain
}

// spansCell is spans as a cell w cells wide: plain and styled, cut to w
// (a cut ends in a blank, as cutRuns cuts) and padded with blanks.
func spansCell(ss []widget.Span, w int) (plain, styled string) {
	var rs []cellRun
	for _, s := range ss {
		text := s.Text
		if s.Icon != "" {
			text = widgetLook{}.Icon(s.Icon) + text
		}
		if text != "" {
			rs = append(rs, cellRun{text, roleStyle(s.Role)})
		}
	}
	rs = cutRuns(rs, w)
	pad := strings.Repeat(" ", max(w-runsWidth(rs), 0))
	return runsPlain(rs) + pad, runsRender(rs) + pad
}

// modRun is a mod cell as a run of the sidebar's grid: w cells, the last
// a blank that parts it from the next column (none when w is 1).
func modRun(ss []widget.Span, w int) cellRun {
	inner := w
	if w > 1 {
		inner = w - 1
	}
	plain, styled := spansCell(ss, inner)
	pad := strings.Repeat(" ", w-inner)
	return cellRun{plain + pad, spanStyle{plain + pad, styled + pad}}
}

// spanStyle draws a mod cell's run in its spans' own styles; cut (a
// narrow row), what is left is drawn plain.
type spanStyle struct{ plain, styled string }

func (s spanStyle) Render(strs ...string) string {
	in := strings.Join(strs, " ")
	if in == s.plain {
		return s.styled
	}
	return th().Plain.Render(in)
}

// statusMods is the mods' status segments for the status bar, each cut
// to statusModMax cells and parted by two blanks, and its width.
func statusMods(mc *modCells) (string, int) {
	if mc == nil {
		return "", 0
	}
	var parts []string
	w := 0
	for _, st := range mc.status {
		n := 0
		for _, s := range st.Spans {
			n += ansi.StringWidth(s.Text)
			if s.Icon != "" {
				n += ansi.StringWidth(widgetLook{}.Icon(s.Icon))
			}
		}
		plain, styled := spansCell(st.Spans, min(n, statusModMax))
		if strings.TrimSpace(plain) == "" {
			continue
		}
		if w > 0 {
			w += 2
		}
		w += ansi.StringWidth(plain)
		parts = append(parts, styled)
	}
	return strings.Join(parts, "  "), w
}
