package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// aboveDash is bandDash under a layout that puts the ticker above the
// tree: sections = ["ticker.ticker", "tree"].
func aboveDash(t *testing.T, h int) (*fakeSource, *dash) {
	t.Helper()
	src, m := bandDash(t, 120+sideDefault, h, bandTestViews())
	m.useLayout("above", mod.Layout{SideSections: []string{"ticker.ticker", "tree"}}, false)
	m.layout.Details = false
	return src, m
}

// manyNeeds adds n threads that need you to beta.
func manyNeeds(src *fakeSource, n int) {
	for i := range n {
		src.data.Projects[1].Threads = append(src.data.Projects[1].Threads, ThreadRow{
			Record: &thread.Record{ID: fmt.Sprintf("t-01%02d", i), Title: "Waits", State: thread.Running},
			Status: &thread.Status{NeedsYou: "a question"}})
	}
}

// TestSideAboveGolden: the band above the tree, PROJECTS directly above
// the first project and saying ⚑ N, as testdata/sidebar-above.golden
// has it (go test -run TestSideAboveGolden -update rewrites it).
func TestSideAboveGolden(t *testing.T) {
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
	_, m := aboveDash(t, 30)
	show("ticker above, CI status below", m)

	_, m = aboveDash(t, 30)
	m.useLayout("above", mod.Layout{SideSections: []string{"ci.status", "ticker.ticker", "tree"}}, false)
	m.layout.Details = false
	show("both above", m)

	_, m = aboveDash(t, 30)
	keyPress(m, "tab")
	m.sideSel = "s:ticker.ticker"
	show("cursor on the header above", m)

	_, m = aboveDash(t, 30)
	m.layout.Sidebar.Width = 20
	show("narrow", m)

	_, m = aboveDash(t, 17)
	show("short window", m)

	_, m = aboveDash(t, 15)
	show("too short for sections", m)

	src, m := bandDash(t, 120+sideDefault, 30, nil)
	manyNeeds(src, 30)
	m.setData(src.data)
	show("no sections, rows with ⚑ scrolled off", m)

	got := b.String()
	path := filepath.Join("testdata", "sidebar-above.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestSideAboveGolden -update writes it)", err)
	}
	if got != string(want) {
		t.Errorf("the band above differs from %s:\n%s", path, got)
	}
}

// TestSideAboveKeys: a header above the tree comes before the first
// project in the cursor's path, those below after the last row, round
// the ends; 1-9 and w skip the headers.
func TestSideAboveKeys(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	_, m := aboveDash(t, 30)
	keyPress(m, "tab")
	m.sideSel = "p:alpha"
	for _, step := range []struct{ key, want string }{
		{"up", headKey}, // PROJECTS, between the band above and the tree
		{"up", "s:ticker.ticker"},
		{"up", "s:ci.status"}, // round the top, to the band below
		{"down", "s:ticker.ticker"},
		{"down", headKey},
		{"down", "p:alpha"},
	} {
		keyPress(m, step.key)
		if m.sideSel != step.want {
			t.Fatalf("%s: %q, want %q", step.key, m.sideSel, step.want)
		}
	}
	m.sideSel = "s:ticker.ticker"
	keyPress(m, "1")
	if m.sideSel != "p:alpha" {
		t.Errorf("1 from the header above: %q", m.sideSel)
	}
	m.sideSel = "s:ticker.ticker"
	keyPress(m, "w")
	if m.sideSel != firstNeeds(m) {
		t.Errorf("w from the header above: %q, want %q", m.sideSel, firstNeeds(m))
	}
	// Folding it works as below the tree.
	m.sideSel = "s:ticker.ticker"
	runAll(m, keyPress(m, "left"))
	if !m.folds.IsSectionFolded("ticker.ticker") || !strings.Contains(sidebarOf(m)[0], "▸ TICKER") {
		t.Fatalf("← on the header above: %v\n%s", m.folds.SectionsFolded, strings.Join(sidebarOf(m), "\n"))
	}
	if !strings.Contains(sidebarOf(m)[1], "PROJECTS") {
		t.Errorf("PROJECTS isn't under the folded header:\n%s", strings.Join(sidebarOf(m), "\n"))
	}
}

// firstNeeds is the first row of m's whole tree that w goes to.
func firstNeeds(m *dash) string {
	for _, r := range m.fullTree() {
		if r.kind != treeProject && r.hint {
			return r.key()
		}
	}
	return ""
}

// TestSideAboveFlag: PROJECTS says ⚑ N while a section is above the
// tree, not with the band below alone; a click on it gives the sidebar
// the keyboard on the first row that needs you, and a click on the
// header off it shows every project's NEEDS YOU, the keyboard on the
// header.
func TestSideAboveFlag(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	_, m := bandDash(t, 120+sideDefault, 30, bandTestViews())
	if l := sidebarOf(m)[0]; strings.Contains(l, ic().hint) {
		t.Fatalf("⚑ N with the band below alone: %q", l)
	}
	_, m = aboveDash(t, 30)
	y := -1
	for i, l := range sidebarOf(m) {
		if strings.Contains(l, "PROJECTS") {
			y = i
		}
	}
	head := strings.TrimRight(sidebarOf(m)[y], " │")
	if want := fmt.Sprintf("%s %d", ic().hint, m.needsCount()); m.needsCount() == 0 || !strings.HasSuffix(head, want) {
		t.Fatalf("the header %q, want it to end in %q", head, want)
	}
	x := strings.Index(head, ic().hint)
	x = len([]rune(head[:x]))
	runAll(m, m.sideClick(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	if m.focus != areaSide || m.sideSel != firstNeeds(m) {
		t.Fatalf("a click on ⚑ N: focus %v sel %q, want %q", m.focus, m.sideSel, firstNeeds(m))
	}
	runAll(m, m.sideClick(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
	if m.focus != areaSide || m.sideSel != headKey || !m.allShown() {
		t.Fatalf("a click on PROJECTS off ⚑ N: focus %v sel %q current %q", m.focus, m.sideSel, m.current)
	}
}

// TestSideAboveGuarantee: on the dashboard, with many threads that need
// you, the band above gives way before the band below, so every ⚑ row
// the tree shows without sections still shows.
func TestSideAboveGuarantee(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	src, m := aboveDash(t, 30)
	manyNeeds(src, 15)
	m.setData(src.data)
	out := whole(m)
	for i := range 15 {
		if id := fmt.Sprintf("t-01%02d", i); !strings.Contains(out, id) {
			t.Errorf("%s needs you and doesn't show:\n%s", id, out)
		}
	}
	side := sidebarOf(m)
	if !strings.Contains(side[0], "▸ TICKER") || !strings.Contains(side[1], "PROJECTS") || strings.Contains(out, "PR host") {
		t.Errorf("want the ticker's header alone above PROJECTS:\n%s", strings.Join(side, "\n"))
	}
	if !strings.Contains(out, "#412") {
		t.Errorf("the band below gave its body up before the band above:\n%s", strings.Join(side, "\n"))
	}
}

// TestSideFlagAt: the header's ⚑ N is hit on its cells while it shows,
// as both screens' clicks ask; not off them, not without anything that
// needs you, and not while it doesn't show.
func TestSideFlagAt(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	_, m := aboveDash(t, 30)
	all, w, h, foot := m.tree(), m.sideW(), m.h, m.sideFoot()
	y := len(sideLayout(all, w, h, foot).above)
	lo, hi := headFlag(2, w-1-sideGap)
	for x := range w {
		if got := sideFlagAt(all, w, h, foot, 2, x, y); got != (x >= lo && x < hi) {
			t.Errorf("x=%d: %v", x, got)
		}
	}
	if sideFlagAt(all, w, h, foot, 0, lo, y) || sideFlagAt(all, w, h, foot, 2, lo, y+1) || sideFlagAt(all, w, h, foot, 2, lo, 0) {
		t.Errorf("hit off the flag")
	}
	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	if sideFlagAt(m.tree(), w, h, m.sideFoot(), 2, lo, 0) {
		t.Errorf("hit with no flag shown")
	}
}
