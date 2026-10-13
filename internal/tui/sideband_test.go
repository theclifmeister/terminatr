package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// bandViews are a built-in ticker band section for beta and a user
// mod's CI-status section with a badge that opens its card.
func bandTestViews() []proto.ModView {
	ticker := &widget.Widget{Kind: widget.KindKV, Pairs: []widget.Pair{
		{Key: "PRs", Value: []widget.Span{{Text: "40s ago · next 1m20s", Role: "faint"}}},
		{Key: "synced", Value: []widget.Span{{Text: "1m ago", Role: "faint"}}},
		{Key: "PR host", Value: []widget.Span{{Text: "ok", Role: "faint"}}},
	}}
	ci := &widget.Widget{Kind: widget.KindText, Text: "#412 passes"}
	return []proto.ModView{
		{Mod: "ticker", Name: "Ticker", View: "ticker", Title: "Ticker", Slot: mod.SlotSidebarSection,
			Description: "this project's PR polls and sync",
			Trees:       []modview.Tree{{Project: "beta", Tree: ticker, Height: 3}}},
		{Mod: "ci", Name: "CI", View: "status", Title: "CI status", Slot: mod.SlotSidebarSection, Opens: "prs",
			Trees: []modview.Tree{{Tree: ci, Height: 1, Badge: []widget.Span{{Text: "1 ☒", Role: "bad"}}}}},
		{Mod: "ci", Name: "CI", View: "prs", Title: "PRs", Slot: mod.SlotDashboardCard},
	}
}

func bandDash(t *testing.T, w, h int, views []proto.ModView) (*fakeSource, *dash) {
	t.Helper()
	src := &fakeSource{data: testData(), modViews: proto.ModViewsResult{Views: views}}
	src.data.Mods = []mod.Mod{{ID: "ticker", Builtin: true}, {ID: "ci"}}
	m := newDash(DashOptions{Source: src, Width: w, Height: h, State: DashState{Current: "beta"}})
	m.layout.Details = false
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	return src, m
}

// sidebarOf is the dashboard's sidebar as text, its trailing blanks cut.
func sidebarOf(m *dash) []string {
	var out []string
	for _, l := range strings.Split(whole(m), "\n") {
		r := []rune(l)
		out = append(out, strings.TrimRight(string(r[:min(m.sideW(), len(r))]), " "))
	}
	return out
}

// TestSideBandGolden: the band below the tree in each state, as
// testdata/sidebar-band.golden has it (go test -run TestSideBandGolden
// -update rewrites it).
func TestSideBandGolden(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	var b strings.Builder
	show := func(name string, m *dash) {
		fmt.Fprintf(&b, "%s (%d×%d, sidebar %d)\n", name, m.winW, m.h, m.sideW())
		for _, l := range sidebarOf(m) {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	_, m := bandDash(t, 120+sideDefault, 30, bandTestViews())
	show("full", m)

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	m.layout.Sidebar.Width = 20
	show("narrow", m)

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	m.layout.Sidebar.Slim = true
	show("slim", m)
	if s := m.shownViews(); slices.ContainsFunc(s, func(s modview.Shown) bool { return s.Slot == mod.SlotSidebarSection }) {
		t.Errorf("the slim strip shows sections: %+v", s)
	}

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	m.layout.Sidebar.Hidden = true
	if m.sideW() != 0 || slices.ContainsFunc(m.tree(), func(r treeRow) bool { return r.kind == treeSection }) {
		t.Errorf("a hidden sidebar has sections")
	}

	_, m = bandDash(t, 120+sideDefault, 17, bandTestViews())
	show("short window", m)

	_, m = bandDash(t, 120+sideDefault, 15, bandTestViews())
	show("too short for sections", m)

	failed := bandTestViews()
	failed = append([]proto.ModView{{Mod: "standup", Name: "Standup", View: "standup", Title: "Standup", Slot: mod.SlotSidebarSection,
		State: proto.ModViewFailed, Err: "exit 1", Log: "/x/standup.log"}}, failed...)
	_, m = bandDash(t, 120+sideDefault, 30, failed)
	show("failed mod", m)

	src, m := bandDash(t, 120+sideDefault, 30, bandTestViews())
	src.data.NoMods = true
	m.setData(src.data)
	show("safe mode", m)

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	keyPress(m, "tab")
	m.sideSel = "s:ticker.ticker"
	show("cursor on a header", m)

	waiting := bandTestViews()
	waiting[0].Trees = nil
	_, m = bandDash(t, 120+sideDefault, 30, waiting)
	show("before the mod draws: no section, not even its header", m)

	got := b.String()
	path := filepath.Join("testdata", "sidebar-band.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestSideBandGolden -update writes it)", err)
	}
	if got != string(want) {
		t.Errorf("the band differs from %s:\n%s", path, got)
	}
}

// TestSideBandKeys: the headers are rows of the cursor's path after the
// tree; ← and → fold and unfold one in the view's folds, enter sends
// the mod a ui.click when the section opens a view and folds it when it
// doesn't, space and p do nothing there, and z Z fold and unfold every
// section with the tree.
func TestSideBandKeys(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	src, m := bandDash(t, 120+sideDefault, 30, bandTestViews())
	keyPress(m, "tab")
	m.sideSel = "t:beta/t-0006"
	keyPress(m, "down")
	if m.sideSel != "s:ticker.ticker" {
		t.Fatalf("↓ from the last thread: %q", m.sideSel)
	}
	keyPress(m, "down")
	if m.sideSel != "s:ci.status" {
		t.Fatalf("↓ again: %q", m.sideSel)
	}
	keyPress(m, "down")
	if m.sideSel != "p:alpha" {
		t.Fatalf("↓ from the last header goes round: %q", m.sideSel)
	}
	keyPress(m, "up")
	if m.sideSel != "s:ci.status" {
		t.Fatalf("↑ round the top: %q", m.sideSel)
	}
	keyPress(m, "up")

	runAll(m, keyPress(m, "left"))
	if !m.folds.IsSectionFolded("ticker.ticker") || !strings.Contains(whole(m), "▸ TICKER") || strings.Contains(whole(m), "PR host") {
		t.Fatalf("← didn't fold the ticker: %v\n%s", m.folds.SectionsFolded, whole(m))
	}
	runAll(m, keyPress(m, "left")) // folded already: nothing
	if !m.folds.IsSectionFolded("ticker.ticker") || m.sideSel != "s:ticker.ticker" {
		t.Fatalf("← on a folded header: %v %q", m.folds.SectionsFolded, m.sideSel)
	}
	runAll(m, keyPress(m, "right"))
	if m.folds.IsSectionFolded("ticker.ticker") {
		t.Fatalf("→ didn't unfold it")
	}
	runAll(m, keyPress(m, "enter")) // no view to open: it folds
	if !m.folds.IsSectionFolded("ticker.ticker") || len(src.inputs) != 0 || m.area() != areaSide {
		t.Fatalf("enter on the ticker: %v %+v", m.folds.SectionsFolded, src.inputs)
	}
	for _, k := range []string{"space", "p"} {
		before := whole(m)
		runAll(m, keyPress(m, k))
		if whole(m) != before || len(src.lifecycle) > 0 {
			t.Errorf("%s on a header did something", k)
		}
	}

	keyPress(m, "down")
	runAll(m, keyPress(m, "enter"))
	want := proto.ModViewInputParams{Mod: "ci", View: "status", Project: "beta", Kind: widget.EventClick}
	if len(src.inputs) != 1 || src.inputs[0] != want || m.folds.IsSectionFolded("ci.status") {
		t.Fatalf("enter on CI status sent %+v", src.inputs)
	}

	runAll(m, keyPress(m, "Z"))
	if len(m.folds.SectionsFolded) != 0 {
		t.Fatalf("Z left %v folded", m.folds.SectionsFolded)
	}
	runAll(m, keyPress(m, "z"))
	if !m.folds.IsSectionFolded("ticker.ticker") || !m.folds.IsSectionFolded("ci.status") || !m.folds.IsFolded("beta") {
		t.Fatalf("z folded %+v", m.folds)
	}
	if m.sideSel != "s:ci.status" {
		t.Fatalf("z moved the cursor off its header: %q", m.sideSel)
	}
	runAll(m, keyPress(m, "Z"))
	if len(m.folds.SectionsFolded) != 0 || m.folds.IsFolded("beta") {
		t.Fatalf("Z folded %+v", m.folds)
	}
}

// TestSideBandMouse: a click on a header or its body gives the sidebar
// the keyboard with the cursor on the header and does what enter does
// there; the hover over a section names its mod, and the details line
// on a header says which mod drew it.
func TestSideBandMouse(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	lineOf := func(m *dash, s string) int {
		for y, l := range sidebarOf(m) {
			if strings.Contains(l, s) {
				return y
			}
		}
		t.Fatalf("no %q in the sidebar", s)
		return -1
	}
	src, m := bandDash(t, 120+sideDefault, 30, bandTestViews())
	runAll(m, m.sideClick(tea.Mouse{X: 5, Y: lineOf(m, "synced"), Button: tea.MouseLeft}))
	if m.focus != areaSide || m.sideSel != "s:ticker.ticker" || !m.folds.IsSectionFolded("ticker.ticker") {
		t.Fatalf("a click on the ticker's body: focus %v sel %q folds %v", m.focus, m.sideSel, m.folds.SectionsFolded)
	}
	runAll(m, m.sideClick(tea.Mouse{X: 5, Y: lineOf(m, "CI STATUS"), Button: tea.MouseLeft}))
	if m.sideSel != "s:ci.status" || len(src.inputs) != 1 || src.inputs[0].Mod != "ci" {
		t.Fatalf("a click on CI status's header: %q %+v", m.sideSel, src.inputs)
	}
	if tip := sideTip(m.tree(), m.sideW(), m.h, m.sideFoot(), 5, lineOf(m, "#412")); tip != "CI status · ci mod" {
		t.Errorf("the hover over its body: %q", tip)
	}
	if out := whole(m); !strings.Contains(out, "CI status · ci mod") || !strings.Contains(out, "← fold · ⏎ open") {
		t.Errorf("the details footer on a header:\n%s", out)
	}

	// The same as enter, from the keys.
	ksrc, k := bandDash(t, 120+sideDefault, 30, bandTestViews())
	keyPress(k, "tab")
	k.sideSel = "s:ci.status"
	runAll(k, keyPress(k, "enter"))
	if fmt.Sprint(ksrc.inputs) != fmt.Sprint(src.inputs) {
		t.Errorf("enter sent %+v, the click %+v", ksrc.inputs, src.inputs)
	}
}

// TestSideBandShown: the dashboard tells mod.views of the band's
// sections for the current project while the full sidebar has the room,
// a folded one and one that drew a null tree too, and of none in the
// slim strip or a window too short.
func TestSideBandShown(t *testing.T) {
	defer resetConsole()
	views := bandTestViews()
	views = append(views, proto.ModView{Mod: "quiet", View: "q", Title: "Quiet", Slot: mod.SlotSidebarSection,
		Trees: []modview.Tree{{Project: "beta"}}})
	_, m := bandDash(t, 120+sideDefault, 30, views)
	m.folds.FoldSections([]string{"ci.status"}, true)
	var got []string
	for _, s := range m.shownViews() {
		if s.Slot == mod.SlotSidebarSection {
			got = append(got, fmt.Sprintf("%s.%s %s %dx%d", s.Mod, s.View, s.Project, s.Width, s.Height))
		}
	}
	if want := []string{"ticker.ticker beta 31x3", "ci.status beta 31x1", "quiet.q beta 31x4"}; !slices.Equal(got, want) {
		t.Errorf("shown %v, want %v", got, want)
	}
	if strings.Contains(whole(m), "QUIET") {
		t.Errorf("a null tree drew a header")
	}
	_, m = bandDash(t, 120+sideDefault, 15, views)
	if s := m.shownViews(); slices.ContainsFunc(s, func(s modview.Shown) bool { return s.Slot == mod.SlotSidebarSection }) {
		t.Errorf("a short sidebar shows sections: %+v", s)
	}
}

// TestSideBandGuarantee: on the dashboard, with many threads that need
// you, the band gives way so every ⚑ row the tree shows without it still
// shows, the last header shown counting those that didn't fit.
func TestSideBandGuarantee(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	src := &fakeSource{data: testData(), modViews: proto.ModViewsResult{Views: bandTestViews()}}
	for i := range 18 {
		src.data.Projects[1].Threads = append(src.data.Projects[1].Threads, ThreadRow{
			Record: &thread.Record{ID: fmt.Sprintf("t-01%02d", i), Title: "Waits", State: thread.Running},
			Status: &thread.Status{NeedsYou: "a question"}})
	}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	out := whole(m)
	for i := range 18 {
		if id := fmt.Sprintf("t-01%02d", i); !strings.Contains(out, id) {
			t.Errorf("%s needs you and doesn't show:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "▸ TICKER") || !strings.Contains(out, "+1") || strings.Contains(out, "PR host") {
		t.Errorf("want the ticker's header with +1:\n%s", out)
	}
}

// TestSideBandUndrawnCursor: a header the band has no room to draw, even
// as the cursor's, is off the cursor's path: down goes past it, and a
// cursor left on it falls back to the row you are on (T369).
func TestSideBandUndrawnCursor(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	undrawn := 0
	for h := 14; h <= 40; h++ {
		src, m := projDash(t, h, nil)
		src.data.Projects = manyProjects(8)
		src.modViews.Views = append(bandTestViews(), projTestView(slugsOf(src.data.Projects)...))
		m.setData(src.data)
		m.setModViews(modViewsMsg{res: src.modViews})
		m.focus = areaSide
		drawn := func() []string {
			l := sideLayout(m.tree(), m.sideW(), h, m.sideFoot())
			var out []string
			for _, s := range slices.Concat(l.above, l.slots) {
				if s.row >= 0 && s.line < 0 {
					out = append(out, l.rows[s.row].key())
				}
			}
			return out
		}
		for _, r := range m.tree() {
			m.sideSel = r.key()
			rows := m.tree()
			i := slices.IndexFunc(rows, func(r treeRow) bool { return r.cursor })
			if i < 0 || !slices.Contains(drawn(), rows[i].key()) {
				t.Fatalf("h=%d sel %s: the cursor isn't drawn (%v)", h, m.sideSel, drawn())
			}
			if rows[i].key() != m.sideSel {
				undrawn++
			}
		}
		for range 2 * len(m.tree()) {
			keyPress(m, "down")
			if !slices.Contains(drawn(), m.sideSel) {
				t.Fatalf("h=%d: down goes onto %s, which isn't drawn (%v)", h, m.sideSel, drawn())
			}
		}
	}
	if undrawn == 0 {
		t.Errorf("no cursor on a header the band doesn't draw")
	}
}
