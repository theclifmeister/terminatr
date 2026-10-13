package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// detailsTestViews are two details sections for beta's thread t-0005:
// checks, which opens a view, and notes.
func detailsTestViews() []proto.ModView {
	return []proto.ModView{
		{Mod: "ci", Name: "CI", View: "checks", Title: "Checks", Slot: mod.SlotDetailsSection, Opens: "full",
			Trees: []modview.Tree{{Project: "beta", Thread: "t-0005", Tree: noteTree("#408 checks failed"),
				Badge: []widget.Span{{Text: "1 ☒", Role: "bad"}}}}},
		{Mod: "notes", Name: "Notes", View: "n", Title: "Notes", Slot: mod.SlotDetailsSection,
			Trees: []modview.Tree{{Project: "beta", Thread: "t-0005", Tree: noteTree("reviewed twice")}}},
	}
}

// detailsDash is a dashboard with the details panel on the right,
// showing beta's thread t-0005 with views' sections, the panel holding
// the keyboard.
func detailsDash(t *testing.T, views []proto.ModView) (*fakeSource, *dash) {
	t.Helper()
	src := &fakeSource{data: testData(), modViews: proto.ModViewsResult{Views: views}}
	m := newDash(DashOptions{Source: src, Width: 160 + sideDefault, Height: 40, State: DashState{Current: "beta"}})
	m.layout.Details = true
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	m.sel, m.userSel = "th:beta:t-0005", true
	m.focus = areaDetails
	return src, m
}

// TestDetailsSectionKeys: while the details panel has the keyboard, ]
// and [ select its mod sections' headers round the ends, ← and → fold
// and unfold the selected one in the view's folds (panel:mod.view),
// enter sends its mod the ui.click that opens its view, esc drops the
// selection, then gives the keyboard back. The footer names the keys.
// With no section, ] is the list's: the next project.
func TestDetailsSectionKeys(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	src, m := detailsDash(t, detailsTestViews())
	if out := whole(m); !strings.Contains(out, "] [ sections") || !strings.Contains(out, "▾ CHECKS") {
		t.Fatalf("the footer doesn't name the section keys:\n%s", out)
	}
	keyPress(m, "]")
	if m.detSel != "panel:ci.checks" || !strings.Contains(whole(m), "← fold · enter open") {
		t.Fatalf("] selected %q:\n%s", m.detSel, whole(m))
	}
	keyPress(m, "]")
	if m.detSel != "panel:notes.n" {
		t.Fatalf("] again: %q", m.detSel)
	}
	keyPress(m, "]")
	if m.detSel != "panel:ci.checks" {
		t.Fatalf("] round the end: %q", m.detSel)
	}
	keyPress(m, "[")
	if m.detSel != "panel:notes.n" {
		t.Fatalf("[ round the top: %q", m.detSel)
	}
	keyPress(m, "[")
	runAll(m, keyPress(m, "left"))
	if !m.folds.IsSectionFolded("panel:ci.checks") || !strings.Contains(whole(m), "▸ CHECKS") || strings.Contains(whole(m), "#408") {
		t.Fatalf("← didn't fold checks: %v\n%s", m.folds.SectionsFolded, whole(m))
	}
	if !strings.Contains(whole(m), "1 ☒") {
		t.Fatalf("a folded header keeps its badge:\n%s", whole(m))
	}
	runAll(m, keyPress(m, "right"))
	if m.folds.IsSectionFolded("panel:ci.checks") {
		t.Fatal("→ didn't unfold it")
	}
	runAll(m, keyPress(m, "enter"))
	want := proto.ModViewInputParams{Mod: "ci", View: "checks", Project: "beta", Kind: widget.EventClick}
	if len(src.inputs) != 1 || src.inputs[0] != want || m.result.Attach != "" {
		t.Fatalf("enter sent %+v, attach %q", src.inputs, m.result.Attach)
	}
	keyPress(m, "]") // notes: no view to open, so enter folds it
	runAll(m, keyPress(m, "enter"))
	if !m.folds.IsSectionFolded("panel:notes.n") || len(src.inputs) != 1 {
		t.Fatalf("enter on notes: %v %+v", m.folds.SectionsFolded, src.inputs)
	}
	keyPress(m, "esc")
	if m.detSel != "" || m.focus != areaDetails {
		t.Fatalf("esc: selection %q, focus %v", m.detSel, m.focus)
	}
	keyPress(m, "esc")
	if m.focus != areaMain {
		t.Fatalf("esc again: focus %v", m.focus)
	}

	// Another row drops the selection.
	m.focus = areaDetails
	keyPress(m, "]")
	m.sel = "th:beta:t-0006"
	if m.detSelNow() != "" {
		t.Fatalf("another row keeps the selection %q", m.detSelNow())
	}

	// No sections: ] and enter are the list's.
	src, m = detailsDash(t, nil)
	m.current = "alpha"
	run(m, press(m, "]"))
	if got := strings.Join(src.opened, ","); got != "beta" || m.detSel != "" {
		t.Fatalf("] without sections: opened %q, selection %q", got, m.detSel)
	}
}

// TestDetailsSectionMouse: a click on a section's fold mark folds it, on
// its header elsewhere or its body selects the header and opens the
// view its mod names, or folds it when there is none.
func TestDetailsSectionMouse(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	src, m := detailsDash(t, detailsTestViews())
	m.focus = areaMain
	g := m.geom()
	dr := g.details()
	lineOf := func(s string) int {
		lines := strings.Split(whole(m), "\n")
		for y := dr.y; y < dr.y+dr.h; y++ {
			if strings.Contains(string([]rune(lines[y])[dr.x:]), s) {
				return y
			}
		}
		t.Fatalf("no %q in the details panel:\n%s", s, whole(m))
		return -1
	}
	click := func(x, y int) {
		_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		runAll(m, cmd)
	}
	click(dr.x+1, lineOf("CHECKS"))
	if !m.folds.IsSectionFolded("panel:ci.checks") || m.detSel != "panel:ci.checks" || m.focus != areaDetails || len(src.inputs) != 0 {
		t.Fatalf("a click on the fold mark: %v %q %v %+v", m.folds.SectionsFolded, m.detSel, m.focus, src.inputs)
	}
	click(dr.x+8, lineOf("CHECKS"))
	if len(src.inputs) != 1 || src.inputs[0].Mod != "ci" {
		t.Fatalf("a click on the header: %+v", src.inputs)
	}
	click(dr.x+6, lineOf("reviewed twice"))
	if m.detSel != "panel:notes.n" || !m.folds.IsSectionFolded("panel:notes.n") {
		t.Fatalf("a click on notes' body: %q %v", m.detSel, m.folds.SectionsFolded)
	}
}

// TestInfoSectionKeys: beside a thread's pane the info panel shows the
// details sections for that thread, as the dashboard's details panel
// does on its row, and says so to mod.views; ] [ ← → and esc work there
// as in the details panel, and with no header selected enter still
// shows the task.
func TestInfoSectionKeys(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	c := infoClient(t, 200)
	views := detailsTestViews()
	for i := range views {
		views[i].Trees[0].Project, views[i].Trees[0].Thread = "demo", "t-0002"
	}
	c.views = views
	c.showInfo(sampleInfo(time.Now()))
	text := strings.Join(c.info.lines, "\n")
	if !strings.Contains(text, "CHECKS") || !strings.Contains(text, "reviewed twice") {
		t.Fatalf("a thread's panel lacks its details sections:\n%s", text)
	}
	c.mu.Lock()
	d := c.info.data
	c.mu.Unlock()
	shown := panelShown(c.views, infoTarget(d), panelOpts{}, c.infoW-1)
	if len(shown) != 2 || shown[0].Slot != mod.SlotDetailsSection || shown[0].Thread != "t-0002" || shown[0].Project != "demo" {
		t.Fatalf("shown %+v", shown)
	}
	key := func(k uv.Key) {
		c.mu.Lock()
		c.kb = areaInfo
		c.infoKeyboard(k)
	}
	key(uv.Key{Code: ']'})
	if c.info.sel != "panel:ci.checks" || !strings.Contains(c.infoHintNow(), "] [ sections · ← fold · enter open") {
		t.Fatalf("] selected %q; hint %q", c.info.sel, c.infoHintNow())
	}
	key(uv.Key{Code: uv.KeyLeft})
	if !c.v.Tree.IsSectionFolded("panel:ci.checks") || strings.Contains(strings.Join(c.info.lines, "\n"), "#408") {
		t.Fatalf("← didn't fold checks: %v", c.v.Tree.SectionsFolded)
	}
	key(uv.Key{Code: uv.KeyRight})
	if c.v.Tree.IsSectionFolded("panel:ci.checks") {
		t.Fatal("→ didn't unfold it")
	}
	key(uv.Key{Code: uv.KeyEscape})
	if c.info.sel != "" || c.kb != areaInfo {
		t.Fatalf("esc: %q %v", c.info.sel, c.kb)
	}
	if h := c.infoHintNow(); !strings.Contains(h, "] [ sections · enter task") {
		t.Fatalf("hint with none selected %q", h)
	}

	// The mouse: a click on the fold mark folds notes, and selects it.
	c.mu.Lock()
	for i, l := range c.info.lines {
		if strings.Contains(l, "NOTES") {
			c.info.top = i
		}
	}
	c.mu.Unlock()
	c.mouse(uv.MouseClickEvent{X: c.infoX() + 2, Y: 0, Button: uv.MouseLeft})
	if !c.v.Tree.IsSectionFolded("panel:notes.n") || c.info.sel != "panel:notes.n" || c.kb != areaInfo {
		t.Fatalf("a click on notes' fold mark: %v %q", c.v.Tree.SectionsFolded, c.info.sel)
	}
}
