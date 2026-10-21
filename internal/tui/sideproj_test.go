package tui

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// projTestView is a user mod's per-project CI-status section, with a
// tree for each of slugs: two lines and a badge, opening its card.
func projTestView(slugs ...string) proto.ModView {
	v := proto.ModView{Mod: "prci", Name: "PR CI", View: "ci", Title: "ci status", Slot: mod.SlotProjectSection, Opens: "prs",
		Description: "this project's PRs and their checks"}
	for _, slug := range slugs {
		v.Trees = append(v.Trees, modview.Tree{Project: slug, Height: 2, Badge: []widget.Span{{Text: "1 ☒", Role: "bad"}},
			Tree: &widget.Widget{Kind: widget.KindText, Text: "☑ #412 " + slug + " docs\n☒ #415 " + slug + " release"}})
	}
	return v
}

// projTestViews are bandTestViews and the per-project section for alpha
// and beta.
func projTestViews() []proto.ModView {
	return append(bandTestViews(), projTestView("alpha", "beta"))
}

// projDash is bandDash with views, its mods knowing prci.
func projDash(t *testing.T, h int, views []proto.ModView) (*fakeSource, *dash) {
	t.Helper()
	src, m := bandDash(t, 120+sideDefault, h, views)
	src.data.Mods = append(src.data.Mods, mod.Mod{ID: "prci"})
	m.setData(src.data)
	return src, m
}

// manyProjects are n active projects p01…, each with a coordinator and
// one thread, the odd ones's thread waiting on you.
func manyProjects(n int) []ProjectData {
	var out []ProjectData
	for i := range n {
		s := config.Defaults
		s.Active = true
		p := ProjectData{Slug: fmt.Sprintf("p%02d", i+1), Safety: &s, Counts: map[string]int{}}
		st := &thread.Status{}
		if i%2 == 1 {
			st.NeedsYou = "a question"
		}
		p.Threads = []ThreadRow{{Record: &thread.Record{ID: fmt.Sprintf("t-%04d", i+1), Title: "Work", State: thread.Running}, Status: st}}
		out = append(out, p)
	}
	return out
}

// slugsOf are the slugs of ps.
func slugsOf(ps []ProjectData) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Slug)
	}
	return out
}

// TestSideProjGolden: per-project sections under each active, unfolded
// project's threads, at thread depth, as testdata/sidebar-proj.golden
// has them (go test -run TestSideProjGolden -update rewrites it).
func TestSideProjGolden(t *testing.T) {
	freshConsole(t)
	var b strings.Builder
	show := func(name string, m *dash) {
		fmt.Fprintf(&b, "%s (%d×%d, sidebar %d)\n", name, m.winW, m.h, m.sideW())
		for _, l := range sidebarOf(m) {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	_, m := projDash(t, 34, projTestViews())
	show("under each project", m)

	_, m = projDash(t, 34, projTestViews())
	m.folds.Fold("beta", true)
	show("beta folded: no section under it", m)

	_, m = projDash(t, 34, projTestViews())
	m.folds.FoldSections([]string{"prci.ci@alpha"}, true)
	show("alpha's section folded to its badge", m)

	src, m := projDash(t, 34, projTestViews())
	in := config.Defaults
	in.Active = false
	src.data.Projects = append(src.data.Projects, ProjectData{Slug: "gamma", Safety: &in})
	m.folds.InactiveOpen = true
	m.folds.Expand("gamma", true)
	m.setData(src.data)
	show("an inactive project, expanded: none", m)

	_, m = projDash(t, 34, projTestViews())
	keyPress(m, "tab")
	m.sideSel = "s:prci.ci@beta"
	show("cursor on beta's section", m)

	views := projTestViews()
	views[len(views)-1].Trees = views[len(views)-1].Trees[1:] // beta's only
	views[len(views)-1].Trees = append(views[len(views)-1].Trees, modview.Tree{Project: "alpha"})
	_, m = projDash(t, 34, views)
	show("a null tree for alpha: nothing under it", m)

	views = projTestViews()
	views[len(views)-1].Trees = nil
	_, m = projDash(t, 34, views)
	show("before the mod draws: no section, not even its header", m)

	failed := projTestViews()
	failed[len(failed)-1].State, failed[len(failed)-1].Err = proto.ModViewFailed, "exit 1"
	_, m = projDash(t, 34, failed)
	show("failed: once, in the band", m)

	_, m = projDash(t, 34, projTestViews())
	setSideFilter(sideFilterState{text: "beta"})
	show("filter beta", m)
	setSideFilter(sideFilterState{})

	_, m = projDash(t, 20, projTestViews())
	show("short window", m)

	src, m = projDash(t, 40, nil)
	src.data.Projects = manyProjects(10)
	src.modViews.Views = append(bandTestViews(), projTestView(slugsOf(src.data.Projects)...))
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	show("ten projects: the quarter share, the bottom projects' first", m)

	got := b.String()
	path := filepath.Join("testdata", "sidebar-proj.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestSideProjGolden -update writes it)", err)
	}
	if got != string(want) {
		t.Errorf("the per-project sections differ from %s:\n%s", path, got)
	}
}

// TestSideProjRows: a project's sections follow its last row, never
// between its row and its coordinator or threads; none for a folded or
// inactive project, nor in the slim strip; in the layout's order.
func TestSideProjRows(t *testing.T) {
	freshConsole(t)
	other := projTestView("alpha", "beta")
	other.Mod, other.View = "age", "age"
	views := append(projTestViews(), other)
	_, m := projDash(t, 34, views)
	if got := rowKeys(m.tree()); !slices.Equal(got, []string{"p:alpha", "c:alpha", "s:prci.ci@alpha", "s:age.age@alpha",
		"p:beta", "c:beta", "t:beta/t-0005", "t:beta/t-0006", "s:prci.ci@beta", "s:age.age@beta", "s:ticker.ticker", "s:ci.status"}) {
		t.Fatalf("rows %v", got)
	}
	m.layout.projSections = []string{"age.age"}
	if got := rowKeys(m.tree()); !slices.Equal(got[2:4], []string{"s:age.age@alpha", "s:prci.ci@alpha"}) {
		t.Fatalf("project_sections = [age.age]: %v", got)
	}
	m.folds.Fold("alpha", true)
	if got := rowKeys(m.tree()); slices.ContainsFunc(got, func(k string) bool { return strings.HasSuffix(k, "@alpha") }) {
		t.Fatalf("folded alpha has sections: %v", got)
	}
	m.layout.Sidebar.Slim = true
	if slices.ContainsFunc(m.tree(), func(r treeRow) bool { return r.kind == treeSection }) {
		t.Fatalf("the slim strip has sections")
	}
	src, m := projDash(t, 34, views)
	src.data.NoMods = true
	m.setData(src.data)
	if slices.ContainsFunc(m.tree(), func(r treeRow) bool { return r.kind == treeSection && r.sec.proj }) {
		t.Fatalf("safe mode shows a user mod's per-project sections")
	}
}

// TestSideProjKeys: the cursor goes through a project's sections after
// its last thread; ← folds one, and on a folded one moves up to its
// project; enter sends the ui.click for the section's own project; z on
// it folds everything and keeps the cursor on its project; 1-9 skip it.
func TestSideProjKeys(t *testing.T) {
	freshConsole(t)
	src, m := projDash(t, 34, projTestViews())
	keyPress(m, "tab")
	m.sideSel = "c:alpha"
	keyPress(m, "down")
	if m.sideSel != "s:prci.ci@alpha" {
		t.Fatalf("↓ from alpha's coordinator: %q", m.sideSel)
	}
	keyPress(m, "down")
	if m.sideSel != "p:beta" {
		t.Fatalf("↓ skips the body: %q", m.sideSel)
	}
	keyPress(m, "up")
	drain(t, m, keyPress(m, "left"))
	if !m.folds.IsSectionFolded("prci.ci@alpha") || m.folds.IsSectionFolded("prci.ci@beta") || m.sideSel != "s:prci.ci@alpha" {
		t.Fatalf("← folded %v, cursor %q", m.folds.SectionsFolded, m.sideSel)
	}
	drain(t, m, keyPress(m, "left"))
	if m.sideSel != "p:alpha" {
		t.Fatalf("← on a folded section: %q", m.sideSel)
	}
	m.sideSel = "s:prci.ci@alpha"
	drain(t, m, keyPress(m, "right"))
	if m.folds.IsSectionFolded("prci.ci@alpha") {
		t.Fatalf("→ didn't unfold it")
	}
	drain(t, m, keyPress(m, "enter"))
	want := proto.ModViewInputParams{Mod: "prci", View: "ci", Project: "alpha", Kind: widget.EventClick}
	if len(src.inputs) != 1 || src.inputs[0] != want {
		t.Fatalf("enter sent %+v, want %+v", src.inputs, want)
	}
	if out := whole(m); !strings.Contains(out, "ci status · prci mod") || !strings.Contains(out, "this project's PRs") {
		t.Errorf("the details footer on a section:\n%s", out)
	}
	keyPress(m, "2")
	if m.sideSel != "p:beta" {
		t.Fatalf("2: %q", m.sideSel)
	}
	m.sideSel = "s:prci.ci@beta"
	drain(t, m, keyPress(m, "Z"))
	if !m.folds.IsFolded("beta") || !m.folds.IsSectionFolded("prci.ci@beta") || m.sideSel != "p:beta" {
		t.Fatalf("z: %+v, cursor %q", m.folds, m.sideSel)
	}
	m.folds.FoldSections([]string{"panel:prci.ci"}, true)
	drain(t, m, keyPress(m, "Z"))
	if !slices.Equal(m.folds.SectionsFolded, []string{"panel:prci.ci"}) {
		t.Fatalf("Z left %v, want the right pane's fold alone", m.folds.SectionsFolded)
	}
}

// TestSideProjMouse: a click on a project's section or its body gives
// the sidebar the keyboard on its header and sends the click for its
// project; the hover names its mod.
func TestSideProjMouse(t *testing.T) {
	freshConsole(t)
	src, m := projDash(t, 34, projTestViews())
	y := slices.IndexFunc(sidebarOf(m), func(l string) bool { return strings.Contains(l, "#415 beta") })
	if y < 0 {
		t.Fatalf("no beta body:\n%s", whole(m))
	}
	if tip := sideTip(m.tree(), m.sideW(), m.h, m.sideFoot(), 8, y); tip != "ci status · prci mod" {
		t.Errorf("hover: %q", tip)
	}
	drain(t, m, m.sideClick(tea.Mouse{X: 8, Y: y, Button: tea.MouseLeft}))
	if m.focus != areaSide || m.sideSel != "s:prci.ci@beta" || len(src.inputs) != 1 || src.inputs[0].Project != "beta" {
		t.Fatalf("click: focus %v sel %q inputs %+v", m.focus, m.sideSel, src.inputs)
	}
}

// TestSideProjShown: the dashboard tells mod.views of one per-project
// section per project whose rows are on screen, folded or not: none for
// a folded project or one scrolled off, none in the slim strip.
func TestSideProjShown(t *testing.T) {
	freshConsole(t)
	shown := func(m *dash) []string {
		var out []string
		for _, s := range m.shownViews() {
			if s.Slot == mod.SlotProjectSection {
				out = append(out, fmt.Sprintf("%s.%s %s %dx%d", s.Mod, s.View, s.Project, s.Width, s.Height))
			}
		}
		return out
	}
	_, m := projDash(t, 34, projTestViews())
	m.folds.FoldSections([]string{"prci.ci@alpha"}, true)
	if got, want := shown(m), []string{"prci.ci alpha 27x2", "prci.ci beta 27x2"}; !slices.Equal(got, want) {
		t.Errorf("shown %v, want %v", got, want)
	}
	m.folds.Fold("alpha", true)
	if got := shown(m); !slices.Equal(got, []string{"prci.ci beta 27x2"}) {
		t.Errorf("alpha folded: %v", got)
	}
	m.layout.Sidebar.Slim = true
	if got := shown(m); got != nil {
		t.Errorf("slim: %v", got)
	}

	src, m := projDash(t, 30, nil)
	src.data.Projects = manyProjects(20)
	src.modViews.Views = []proto.ModView{projTestView()}
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	got := shown(m)
	if len(got) == 0 || len(got) >= 20 {
		t.Fatalf("20 projects, some scrolled off: %v", got)
	}
	on := sideOnScreen(m.tree(), m.sideW(), m.h, m.sideFoot())
	for _, p := range src.data.Projects {
		if slices.Contains(got, "prci.ci "+p.Slug+" 27x2") != on(p.Slug) {
			t.Errorf("%s: on screen %v, shown %v", p.Slug, on(p.Slug), got)
		}
	}
}

// TestFitBandProjProperties: fitBand with per-project sections keeps
// their quarter share and gives way in its order (under the projects
// bottom first, then above the tree, then below), and never scrolls a
// row that must stay off the tree's rows, those sections among them.
func TestFitBandProjProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 355))
	for range 20000 {
		b, n := rng.IntN(60), rng.IntN(80)
		tree := make([]treeRow, n)
		for i := range tree {
			tree[i].hint = rng.IntN(6) == 0
		}
		if n > 0 && rng.IntN(2) == 0 {
			tree[rng.IntN(n)].here = true
		}
		if n > 0 && rng.IntN(2) == 0 {
			tree[rng.IntN(n)].cursor = true
		}
		// Above, under the projects (at rising), below: as drawn.
		var asks []bandAsk
		for range rng.IntN(3) {
			asks = append(asks, bandAsk{want: rng.IntN(13), above: true})
		}
		at := 0
		for range rng.IntN(12) {
			at = min(at+rng.IntN(6), n)
			asks = append(asks, bandAsk{want: rng.IntN(5), proj: true, at: at})
		}
		for range rng.IntN(3) {
			asks = append(asks, bandAsk{want: rng.IntN(13)})
		}
		if len(asks) > 0 && !slices.ContainsFunc(tree, func(r treeRow) bool { return r.cursor }) && rng.IntN(2) == 0 {
			asks[rng.IntN(len(asks))].cursor = true
		}
		var order []int
		for _, place := range []int{placeProj, placeAbove, placeBelow} {
			for j := len(asks) - 1; j >= 0; j-- {
				if asks[j].place() == place {
					order = append(order, j)
				}
			}
		}
		cur, keep := bandKeep(tree, b)
		f := fitBand(b, n, cur, keep, asks)
		at2 := fmt.Sprintf("b=%d n=%d cur=%d keep=%v asks=%v: %+v", b, n, cur, keep, asks, f)
		if f.treeH+f.part(placeAbove)+f.part(placeBelow) != b {
			t.Fatalf("%s: the rows aren't the body", at2)
		}
		if b < bandMinBody && f.headers() > 0 {
			t.Fatalf("%s: sections under %d rows", at2, bandMinBody)
		}
		if f.used() > b/2 || f.headers() > b/4 || f.part(placeAbove) > b/4 || f.part(placeProj) > b/4 {
			t.Fatalf("%s: over the caps", at2)
		}
		// In order within each place: the shares cap each on its own.
		for j := range asks {
			if (f.shrunk[j] || !f.show[j]) && b >= bandMinBody {
				for _, k := range order[:slices.Index(order, j)] {
					if f.body[k] > 0 && asks[k].place() == asks[j].place() {
						t.Fatalf("%s: section %d gave way before %d", at2, j, k)
					}
				}
			}
		}
		// The guarantee, with the per-project sections in the tree's
		// rows.
		if N := n + f.part(placeProj); f.treeH < N {
			for _, k := range keep {
				if s := f.treeSlot(k); s < f.top || s >= f.top+f.treeH {
					t.Fatalf("%s: row %d (slot %d) scrolled off [%d, %d)", at2, k, s, f.top, f.top+f.treeH)
				}
			}
		}
	}
}

// TestSideProjGuarantee: on the dashboard with many projects, each with
// a per-project section and half with a thread that needs you, every ⚑
// row, the cursor's and the one you are on that shows without sections
// shows with them, for any window height and cursor.
func TestSideProjGuarantee(t *testing.T) {
	freshConsole(t)
	rng := rand.New(rand.NewPCG(4, 355))
	for range 200 {
		ps := manyProjects(1 + rng.IntN(16))
		h := 10 + rng.IntN(50)
		src, m := projDash(t, h, nil)
		src.data.Projects = ps
		src.modViews.Views = append(bandTestViews(), projTestView(slugsOf(ps)...))
		if rng.IntN(2) == 0 {
			m.layout.sideSections = []string{"ticker.ticker", "tree"}
		}
		m.setData(src.data)
		m.setModViews(modViewsMsg{res: src.modViews})
		if rng.IntN(2) == 0 {
			keyPress(m, "tab")
			rows := m.tree()
			m.sideSel = rows[rng.IntN(len(rows))].key()
		}
		with := m.tree()
		// Without sections, the cursor on a project's section is on the
		// project's last row.
		var without []treeRow
		for _, r := range with {
			switch {
			case r.kind != treeSection:
				without = append(without, r)
			case r.cursor && r.sec.proj:
				without[len(without)-1].cursor = true
			}
		}
		w, foot := m.sideW(), m.sideFoot()
		// The cursor on a project's section: its header takes a row, so
		// the rows kept are those of a sidebar one row shorter (a ⚑ row
		// that gives way is counted in the header's ⚑ N).
		hw := h
		if slices.ContainsFunc(with, func(r treeRow) bool { return r.cursor && r.kind == treeSection && r.sec.proj }) {
			hw--
		}
		on := func(rows []treeRow) []string {
			l := sideLayout(rows, w, h, foot)
			var out []string
			for _, s := range slices.Concat(l.above, l.slots) {
				if s.row >= 0 && s.line < 0 {
					out = append(out, l.rows[s.row].key())
				}
			}
			return out
		}
		got := on(with)
		lw := sideLayout(without, w, hw, foot)
		for _, s := range lw.slots {
			if r := lw.rows[s.row]; (r.hint || r.cursor || r.here) && !slices.Contains(got, r.key()) {
				t.Fatalf("h=%d %d projects sel %q: %s shows without sections and not with them (%v)", h, len(ps), m.sideSel, r.key(), got)
			}
		}
		// The cursor shows, on a header the band draws too: a sel on one
		// it doesn't draw falls back (cursorRows).
		if i := slices.IndexFunc(with, func(r treeRow) bool { return r.cursor }); i >= 0 && !slices.Contains(got, with[i].key()) {
			t.Fatalf("h=%d sel %q: the cursor's %s doesn't show (%v)", h, m.sideSel, with[i].key(), got)
		}
		lines := sidebarOf(m)
		if len(lines) != h {
			t.Fatalf("h=%d: %d lines", h, len(lines))
		}
	}
}
