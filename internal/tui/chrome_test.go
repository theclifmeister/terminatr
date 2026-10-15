package tui

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/theclifmeister/terminatr/internal/mod"
)

// chromeDash is a dashboard 160×30 on beta under the layout spec.
func chromeDash(t *testing.T, spec mod.Layout) *dash {
	t.Helper()
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 160, Height: 30, UIFile: filepath.Join(t.TempDir(), "ui.json"), State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.useLayout("test", spec, true)
	m.rebuild()
	return m
}

// TestChromeHidden: with the header and the footer hidden the body has
// every row; the server's alarm draws over its first row at the right
// and a message over its last at the left, while they last; the
// needs-you badge (sidebar hidden) still takes its click.
func TestChromeHidden(t *testing.T) {
	m := chromeDash(t, mod.Layout{Header: "hidden", Footer: "hidden"})
	lines := strings.Split(screen(m), "\n")
	if len(lines) != 30 || m.bodyRows() != 30 {
		t.Fatalf("%d rows, body %d", len(lines), m.bodyRows())
	}
	out := strings.Join(lines, "\n")
	if strings.Contains(out, "tm dashboard") || strings.Contains(out, "server ok") || strings.Contains(out, menuButton+" menu") {
		t.Fatalf("a hidden header or footer drew:\n%s", out)
	}
	first := lines[0]

	m.data.ServerOK, m.data.Err = false, "boom"
	m.msg = "hello there"
	lines = strings.Split(screen(m), "\n")
	if len(lines) != 30 || !strings.HasSuffix(strings.TrimRight(lines[0], " "), "▲ server down: boom") {
		t.Fatalf("no alarm over the first row: %q", lines[0])
	}
	// The row under it keeps its start: nothing moved.
	if a, b := []rune(lines[0]), []rune(first); string(a[:20]) != string(b[:20]) {
		t.Fatalf("the first row moved: %q, was %q", lines[0], first)
	}
	if !strings.HasPrefix(lines[29], " hello there ") {
		t.Fatalf("no message over the last row: %q", lines[29])
	}
	m.data.ServerOK, m.msg = true, ""

	// The sidebar hidden too: the badge at the first row's right.
	m = chromeDash(t, mod.Layout{Header: "hidden", Footer: "hidden", Mode: "hidden"})
	badge := "⚑ " + strconv.Itoa(m.needsCount())
	row := strings.Split(whole(m), "\n")[0]
	i := strings.Index(row, badge)
	if i < 0 || !strings.HasSuffix(strings.TrimRight(row, " "), badge) {
		t.Fatalf("no badge over the first row: %q", row)
	}
	m.sel = ""
	m.Update(tea.MouseClickMsg{X: ansi.StringWidth(row[:i]), Y: 0, Button: tea.MouseLeft})
	if r, ok := m.selected(); !ok || !strings.HasPrefix(r.key, "n") {
		t.Fatalf("badge click selected %q", m.sel)
	}
}

// TestChromeCompactFooter: a compact footer is one row: the keys, whose
// buttons take clicks, or the message while there is one.
func TestChromeCompactFooter(t *testing.T) {
	m := chromeDash(t, mod.Layout{Footer: "compact"})
	lines := strings.Split(screen(m), "\n")
	if len(lines) != 30 || m.bodyRows() != 28 || !strings.HasPrefix(lines[29], " "+menuButton+" menu") {
		t.Fatalf("body %d, last row %q", m.bodyRows(), lines[29])
	}
	if strings.Contains(lines[28], "────") && !strings.Contains(lines[28], "SESSIONS") {
		t.Fatalf("a rule above a compact footer: %q", lines[28])
	}
	x := m.geom().main().x + 1
	m.msg = "hello there"
	if lines = strings.Split(screen(m), "\n"); !strings.HasPrefix(lines[29], " hello there") {
		t.Fatalf("no message: %q", lines[29])
	}
	m.Update(tea.MouseClickMsg{X: x, Y: 29, Button: tea.MouseLeft})
	if m.top() != nil {
		t.Fatal("a click on the message pressed a key")
	}
	m.msg = ""
	screen(m)
	m.Update(tea.MouseClickMsg{X: x, Y: 29, Button: tea.MouseLeft})
	if m.top() == nil {
		t.Fatal("the menu button didn't open the menu")
	}
}

// TestCompactDensity: compact rows leave out every extra line: the
// project's task counts go onto its header, a selected thread's details
// don't show under it, an unselected card is its header, and the
// sidebar's footer is its key bar.
func TestCompactDensity(t *testing.T) {
	m := chromeDash(t, mod.Layout{})
	m.current = "alpha"
	m.rebuild()
	if out := screen(m); !strings.Contains(out, "\n  tasks: ") {
		t.Fatalf("normal: no task counts' line:\n%s", out)
	}
	m.current = "beta"
	m.rebuild()
	m.sel = "th:beta:t-0005"
	normal := m.listLines(100, true).keys

	m = chromeDash(t, mod.Layout{Density: "compact"})
	m.current = "alpha"
	m.rebuild()
	out := screen(m)
	if strings.Contains(out, "\n  tasks: ") {
		t.Fatalf("compact: a task counts' line:\n%s", out)
	}
	var head string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, " alpha") {
			head = l
		}
	}
	if !strings.Contains(head, "tasks: ") || !strings.Contains(head, "active") {
		t.Fatalf("compact: no counts and state on the header: %q", head)
	}
	// Narrow, the state label gives way first.
	for _, r := range m.rows {
		if r.headTasks != "" {
			w := len(" alpha") + ansi.StringWidth(r.headTasks) + 7 // the counts and a short rule
			if h := ansi.Strip(projectHead(r, w)); !strings.Contains(h, "tasks: ") || strings.Contains(h, "active") {
				t.Fatalf("narrow header: %q", h)
			}
		}
	}

	m.current = "beta"
	m.rebuild()
	m.sel = "th:beta:t-0005"
	count := func(keys []string) (n int) {
		for _, k := range keys {
			if k == m.sel {
				n++
			}
		}
		return n
	}
	if compact := m.listLines(100, true).keys; count(normal) < 2 || count(compact) != 1 {
		t.Fatalf("inline details: normal %d lines, compact %d", count(normal), count(compact))
	}

	if f := sideFooter(nil, 32, sideHeadIn{}, ServerInfo{}, false, true); len(f) != 1 {
		t.Fatalf("compact sidebar footer: %q", f)
	}

	_, m = panelDash(t)
	m.useLayout("test", mod.Layout{Density: "compact"}, true)
	m.rebuild()
	if out := screen(m); !strings.Contains(out, "Cost · Panel") || strings.Contains(out, "$4.20") {
		t.Fatalf("compact: an unselected card's lines:\n%s", out)
	}
	for _, s := range m.shownViews() {
		if s.View == "cost" {
			t.Error("a card shown as its header counts as shown")
		}
	}
	m.sel = "mc:panel/cost"
	if out := screen(m); !strings.Contains(out, "$4.20") {
		t.Fatalf("compact: the selected card's lines:\n%s", out)
	}
}

// TestStatusCompact: the layout's compact status bar is the view's: a
// view with it lays the pane out a row taller (no rule), and the attach
// client's sidebar and info panel follow.
func TestStatusCompact(t *testing.T) {
	if m := chromeDash(t, mod.Layout{StatusBar: "compact"}); m.wantCompact == nil || !*m.wantCompact {
		t.Fatal("the layout's status bar isn't asked of the view")
	}
	c := infoClient(t, 160)
	c.relayout()
	if c.geo.Status != 2 || c.paneRows != 28 || c.sideRows() != 28 || c.infoRows() != 28 {
		t.Fatalf("shown: status %d, pane %d, side %d, info %d", c.geo.Status, c.paneRows, c.sideRows(), c.infoRows())
	}
	c.v.StatusCompact = true
	c.relayout()
	if c.geo.Status != 1 || !c.statusBar || c.paneRows != 29 || c.sideRows() != 29 || c.infoRows() != 29 {
		t.Fatalf("compact: status %d, pane %d, side %d, info %d", c.geo.Status, c.paneRows, c.sideRows(), c.infoRows())
	}
}
