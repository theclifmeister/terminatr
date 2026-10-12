package tui

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestDashboardSidebarRight: on the right the sidebar is the window's
// last columns, its border its first; clicks, the drag and the tooltip
// follow it. Hidden, it takes no columns and tab skips it.
func TestDashboardSidebarRight(t *testing.T) {
	src := &fakeSource{data: testData()}
	ui := filepath.Join(t.TempDir(), "ui.json")
	m := newDash(DashOptions{Source: src, Width: 120, Height: 30, UIFile: ui, State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.layout.Sidebar.Side = "right"
	m.setWidth(120)
	x0 := 120 - sideDefault
	if g := m.geom(); g.side() != (rect{x0, 0, sideDefault, 30}) || g.main() != (rect{0, 0, x0, 30}) || m.w != x0 {
		t.Fatalf("geometry: side %+v main %+v", g.side(), g.main())
	}
	lines := strings.Split(whole(m), "\n")
	if !strings.HasPrefix(lines[0], " tm dashboard") {
		t.Fatalf("the dashboard starts the row: %q", lines[0])
	}
	for i, want := range []string{"│ PROJECTS", "│ ■ alpha", "│ └─ coordinator"} {
		if got := ansi.Cut(lines[i], x0, 120); !strings.HasPrefix(got, want) {
			t.Errorf("row %d's sidebar is %q, want %q…", i, got, want)
		}
	}

	// A project row is a click at its own columns, right of the border.
	m.Update(tea.MouseClickMsg{X: x0 + 5, Y: 1, Button: tea.MouseLeft})
	if m.current != "alpha" {
		t.Fatalf("click on alpha: current %q", m.current)
	}
	// The border drags: the sidebar is what's right of it.
	m.Update(tea.MouseClickMsg{X: x0, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 90, Y: 5, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 90, Y: 5, Button: tea.MouseLeft})
	if m.sideW() != 30 || m.w != 90 || LoadLayout(ui).Sidebar.Width != 30 || !m.layout.Sidebar.Right() {
		t.Fatalf("drag: sidebar %d, dashboard %d, ui.json %+v", m.sideW(), m.w, LoadLayout(ui).Sidebar)
	}

	// Hidden (b after the slim strip): no columns, the list has the
	// window, tab skips it.
	press(m, "b", "b")
	if m.sideW() != 0 || m.w != 120 || !strings.HasPrefix(strings.Split(whole(m), "\n")[0], " tm dashboard") {
		t.Fatalf("hidden: sidebar %d, dashboard %d", m.sideW(), m.w)
	}
	for range 3 {
		press(m, "tab")
		if m.area() == areaSide {
			t.Fatal("tab went to a hidden sidebar")
		}
	}
	m.focus = areaSide
	if m.area() != areaMain {
		t.Fatal("a hidden sidebar has the keyboard")
	}
}

// TestAttachSidebarRight: attached, a sidebar on the right is the
// window's last columns, the info panel left of it and the pane from
// the first column; hidden, the pane and the panel have the window and
// the keyboard leaves it.
func TestAttachSidebarRight(t *testing.T) {
	c := infoClient(t, 200)
	c.v.Sidebar.Side = "right"
	c.relayout()
	sx, ix := 200-sideDefault, 200-sideDefault-c.infoW
	if c.sideX() != sx || c.paneX() != 0 || c.infoX() != ix || c.paneCols != ix {
		t.Fatalf("side at %d, pane at %d (%d wide), info at %d", c.sideX(), c.paneX(), c.paneCols, c.infoX())
	}
	if c.geo.SideX != sx || c.geo.Area.X != 0 {
		t.Fatalf("geometry %+v", c.geo)
	}
	b, _ := c.appendSidebar(nil, false)
	if !strings.Contains(string(b), "\x1b[1;"+strconv.Itoa(sx+1)+"H") || !strings.HasPrefix(ansi.Strip(strings.SplitN(string(b), "H", 2)[1]), "│ PROJECTS") {
		t.Fatalf("the sidebar isn't drawn at the right edge, border first: %q", ansi.Strip(string(b)))
	}
	if !c.onSide(sx) || c.onSide(sx-1) || c.sideCol(sx) != sideDefault-1 || c.sideCol(sx+1) != 0 {
		t.Fatalf("hits: on %v %v, cols %d %d", c.onSide(sx), c.onSide(sx-1), c.sideCol(sx), c.sideCol(sx+1))
	}

	c.kb = areaSide
	c.v.Sidebar.Hidden = true
	c.relayout()
	if c.sideW != 0 || c.paneCols != 200-c.infoW || c.infoX() != 200-c.infoW || c.kb != areaMain || c.onSide(199) {
		t.Fatalf("hidden: side %d, pane %d, info at %d, kb %v", c.sideW, c.paneCols, c.infoX(), c.kb)
	}
	if b, _ := c.appendSidebar(nil, false); len(b) != 0 {
		t.Fatalf("a hidden sidebar drew %q", b)
	}
	for _, a := range c.areas() {
		if a == areaSide {
			t.Fatal("prefix+tab goes to a hidden sidebar")
		}
	}
}

// TestNeedsBadge: with the sidebar hidden, "⚑ N" counts what NEEDS YOU
// counts: in the dashboard's header, where a click selects NEEDS YOU's
// first row, and at the right of the attach status bar, where a click
// opens the dashboard there.
func TestNeedsBadge(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 160, Height: 30, UIFile: filepath.Join(t.TempDir(), "ui.json"), State: DashState{Current: "beta"}})
	m.setData(src.data)
	n := needsYouCount(src.data.Projects, src.data.Sessions)
	if n == 0 || n != m.needsCount() {
		t.Fatalf("needs-you count %d, NEEDS YOU says %d", n, m.needsCount())
	}
	badge := "⚑ " + strconv.Itoa(n)
	if head := strings.Split(whole(m), "\n")[0]; strings.Contains(head, badge) {
		t.Fatalf("a badge beside the sidebar: %q", head)
	}
	press(m, "b", "b")
	head := strings.Split(whole(m), "\n")[0]
	i := strings.Index(head, badge)
	if i < 0 || !strings.Contains(head[i:], "server ok") {
		t.Fatalf("hidden: no badge before the server's state: %q", head)
	}
	m.sel = ""
	m.Update(tea.MouseClickMsg{X: ansi.StringWidth(head[:i]), Y: 0, Button: tea.MouseLeft})
	if r, ok := m.selected(); !ok || !strings.HasPrefix(r.key, "n") {
		t.Fatalf("badge click selected %q", m.sel)
	}

	// Clicked in a session: the dashboard opens on NEEDS YOU.
	m = newDash(DashOptions{Source: src, Width: 160, Height: 30, State: DashState{Current: "beta", Then: thenNeeds}})
	m.setData(src.data)
	if r, ok := m.selected(); !ok || !strings.HasPrefix(r.key, "n") || m.then != "" {
		t.Fatalf("then needs-you selected %q", m.sel)
	}

	c := infoClient(t, 160)
	c.statusBar = true
	c.focus = &pane{info: proto.SessionInfo{ID: "s-1", Role: proto.RoleCoordinator, Project: "alpha"}}
	c.side.projects, c.side.sessions = src.data.Projects, src.data.Sessions
	c.status()
	if strings.Contains(ansi.Strip(c.statusText), badge) {
		t.Fatalf("a badge beside the sidebar: %q", ansi.Strip(c.statusText))
	}
	c.v.Sidebar.Hidden = true
	f := c.focus
	c.relayout()
	c.focus = f // no pane in the test: relayout forgets it
	c.status()
	bar := ansi.Strip(c.statusText)
	if !strings.HasSuffix(bar, badge+" ") || ansi.StringWidth(bar) != 160 || hintAt(c.statusHits, 160-2) != thenNeeds {
		t.Fatalf("hidden: status %q, hits %v", bar, c.statusHits)
	}
}

// TestSlimPaused: the slim strip's state column says paused for a
// paused project whose coordinator is idle; blocked still wins, and
// inactive.
func TestSlimPaused(t *testing.T) {
	d := testData()
	d.Projects[0].Safety = &config.Safety{Paused: true, Active: true} // alpha: its coordinator is blocked
	d.Projects[1].Safety = &config.Safety{Paused: true, Active: true} // beta: none runs
	rows := slimRows(buildTree(d.Projects, d.Sessions, treeIn{current: "beta"}))
	got := map[string]string{}
	for _, r := range rows {
		got[r.slug] = ansi.Strip(slimCells(r, sideSlim-1, th().Plain))
	}
	if !strings.HasPrefix(got["beta"], " "+ic().paused+" beta") {
		t.Errorf("paused beta in the strip: %q (%q)", got["beta"], got)
	}
	if strings.Contains(got["alpha"], ic().paused) {
		t.Errorf("paused alpha's blocked coordinator gave way: %q", got["alpha"])
	}
}
