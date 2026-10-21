package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

func rowKeys(rows []treeRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.key())
	}
	return out
}

// TestTreeOrder: the active projects come first, the pinned ones at the
// top in their own order, then the divider "inactive (n)", folded by
// default, the inactive projects under it, pinned or not. A folded
// active project is its row alone; the header counts every project.
func TestTreeOrder(t *testing.T) {
	mk := func(slug string, active, pinned bool) ProjectData {
		s := config.Defaults
		s.Active, s.Pinned = active, pinned
		return ProjectData{Slug: slug, Safety: &s}
	}
	ps := []ProjectData{mk("a", true, false), mk("b", false, true), mk("c", true, true), mk("d", false, false), mk("e", true, false)}
	rows := buildTree(ps, nil, treeIn{})
	if want := []string{"p:c", "c:c", "p:a", "c:a", "p:e", "c:e", dividerKey}; !slices.Equal(rowKeys(rows), want) {
		t.Fatalf("tree %v, want %v", rowKeys(rows), want)
	}
	div := rows[len(rows)-1]
	if !div.folded() || div.threads != 2 || !slices.Equal(div.slugs, []string{"b", "d"}) {
		t.Fatalf("divider %+v", div)
	}
	if l := ansi.Strip(treeCells(div, 35, false, false)); !strings.HasPrefix(l, " ── ▸ inactive (2) ──") || ansi.StringWidth(l) != 35 {
		t.Fatalf("divider row %q", l)
	}
	if l := ansi.Strip(sidebarLines(rows, sideDefault, 10, nil)[0]); !strings.Contains(l, " 5 ") {
		t.Fatalf("header %q counts the projects under the folded divider too", l)
	}
	rows = buildTree(ps, nil, treeIn{tree: view.Tree{Folded: []string{"a"}, InactiveOpen: true}})
	if want := []string{"p:c", "c:c", "p:a", "p:e", "c:e", dividerKey, "p:b", "p:d"}; !slices.Equal(rowKeys(rows), want) {
		t.Fatalf("tree %v, want %v", rowKeys(rows), want)
	}
	if l := ansi.Strip(treeCells(rows[5], 35, false, false)); !strings.Contains(l, "▾ inactive (2)") {
		t.Fatalf("unfolded divider %q", l)
	}
	// The slim strip shows the divider too, shrunk.
	if got := rowKeys(slimRows(rows)); !slices.Equal(got, []string{"p:c", "p:a", "p:e", dividerKey, "p:b", "p:d"}) {
		t.Fatalf("slim %v", got)
	}
	if l := ansi.Strip(treeCells(rows[5], sideSlim-1, true, false)); l != " ─▾2───── " {
		t.Fatalf("slim divider %q", l)
	}
}

// foldDash is a dashboard over testData with beta inactive and its fold
// state kept in ui.json, the sidebar focused.
func foldDash(t *testing.T) (*dash, *fakeSource, string) {
	d := testData()
	on, off := config.Defaults, config.Defaults
	off.Active = false
	d.Projects[0].Safety, d.Projects[1].Safety = &on, &off
	ui := filepath.Join(t.TempDir(), "ui.json")
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 30, UIFile: ui, State: DashState{Current: "alpha"}})
	m.setData(src.data)
	keyPress(m, "tab")
	return m, src, ui
}

// TestDividerKeys: → unfolds the divider and ← folds it, enter and a
// click toggle it, space does nothing on it; what is folded is kept in
// ui.json, which a new dashboard starts with.
func TestDividerKeys(t *testing.T) {
	m, src, ui := foldDash(t)
	m.sideSel = dividerKey
	open := func() bool { return slices.Contains(rowKeys(m.tree()), "p:beta") }
	for i, s := range []struct {
		key  string
		open bool
	}{{"right", true}, {"right", true}, {"left", false}, {"left", false}, {"enter", true}, {"enter", false}} {
		drain(t, m, keyPress(m, s.key))
		if open() != s.open || m.sideSel != dividerKey {
			t.Fatalf("step %d (%s): open %v, cursor %q", i, s.key, open(), m.sideSel)
		}
	}
	drain(t, m, keyPress(m, "S"))
	if open() || len(src.lifecycle) != 0 || m.msg != "" {
		t.Fatalf("space on the divider: lifecycle %v msg %q", src.lifecycle, m.msg)
	}
	// A click on it toggles it.
	// The divider is pinned to the bottom of the tree: find its line.
	lay := sideLayout(m.tree(), m.sideW(), m.h, m.sideFoot())
	y := len(lay.above) + lay.head + slices.IndexFunc(lay.slots, func(s sideSlot) bool {
		return s.row >= 0 && lay.rows[s.row].key() == dividerKey
	})
	drain(t, m, m.sideClick(tea.Mouse{X: 5, Y: y, Button: tea.MouseLeft}))
	if !open() || len(src.opened) != 0 {
		t.Fatalf("click: open %v, opened %v", open(), src.opened)
	}
	if !LoadLayout(ui).Tree.InactiveOpen {
		t.Fatalf("ui.json: %+v", LoadLayout(ui).Tree)
	}
	n := newDash(DashOptions{Source: src, Width: 120, Height: 30, UIFile: ui})
	n.setData(src.data)
	if !slices.Contains(rowKeys(n.tree()), "p:beta") {
		t.Fatalf("a new dashboard didn't start with ui.json's tree: %v", rowKeys(n.tree()))
	}
}

// TestFoldAllAndPin: z folds every project and the divider, its cursor
// moving up to what still shows; Z unfolds them all, the inactive
// projects expanded (never activated); p pins a project, at once and in
// its setting, and again unpins it.
func TestFoldAllAndPin(t *testing.T) {
	m, src, ui := foldDash(t)
	m.sideSel = "p:alpha" // on alpha's running coordinator Z would type through
	if !slices.Contains(rowKeys(m.tree()), m.sideSel) {
		t.Fatalf("no row %s: %v", m.sideSel, rowKeys(m.tree()))
	}
	drain(t, m, keyPress(m, "Z"))
	if got := rowKeys(m.tree()); !slices.Equal(got, []string{"p:alpha", dividerKey}) || m.sideSel != "p:alpha" {
		t.Fatalf("z: tree %v, cursor %q", got, m.sideSel)
	}
	if l := LoadLayout(ui).Tree; !slices.Equal(l.Folded, []string{"alpha"}) || l.InactiveOpen {
		t.Fatalf("ui.json after z: %+v", l)
	}
	// Folded, alpha's row shows its coordinator's state.
	if r := m.tree()[0]; gridCol(treeCells(r, 35, false, false), gridState) == "" {
		t.Fatalf("folded alpha has no state: %q", ansi.Strip(treeCells(r, 35, false, false)))
	}
	drain(t, m, keyPress(m, "Z"))
	want := []string{"p:alpha", "c:alpha", dividerKey, "p:beta", "c:beta", "t:beta/t-0005", "t:beta/t-0006"}
	if got := rowKeys(m.tree()); !slices.Equal(got, want) || len(src.lifecycle) != 0 {
		t.Fatalf("Z: tree %v, want %v; lifecycle %v", got, want, src.lifecycle)
	}
	// z on a dormant row moves the cursor to the divider.
	m.sideSel = "c:beta"
	drain(t, m, keyPress(m, "Z"))
	if m.sideSel != dividerKey {
		t.Fatalf("z on beta's coordinator: cursor %q", m.sideSel)
	}

	m.sideSel = "p:alpha"
	drain(t, m, keyPress(m, "P"))
	if !slices.Equal(src.lifecycle, []string{"alpha pin"}) || !m.projectData("alpha").pinned() {
		t.Fatalf("p: lifecycle %v", src.lifecycle)
	}
	drain(t, m, keyPress(m, "P"))
	if !slices.Equal(src.lifecycle, []string{"alpha pin", "alpha unpin"}) || m.projectData("alpha").pinned() {
		t.Fatalf("p again: lifecycle %v", src.lifecycle)
	}
	m.sideSel = "c:beta" // alpha has no divider now: its pin reloaded its settings
	drain(t, m, keyPress(m, "P"))
	if len(src.lifecycle) != 2 || !strings.Contains(m.msg, "P on a project") {
		t.Fatalf("p on a coordinator: lifecycle %v msg %q", src.lifecycle, m.msg)
	}
}

// TestPinnedAboveNeedsFirst: with needs-you-first on, the pinned
// projects keep the top, those that need you first among them, then the
// rest the same way; an inactive project's pin changes nothing.
func TestPinnedAboveNeedsFirst(t *testing.T) {
	p := func(slug string, pinned, hint, inactive bool) treeRow {
		return treeRow{kind: treeProject, slug: slug, pinned: pinned, hint: hint, inactive: inactive}
	}
	rows := []treeRow{
		p("b", true, false, false), p("e", true, true, false), p("a", false, true, false), p("c", false, false, false),
		{kind: treeDivider}, p("x", true, false, true), p("y", false, true, true),
	}
	want := []string{"p:e", "p:b", "p:a", "p:c", dividerKey, "p:y", "p:x"}
	if got := rowKeys(needsFirstOrder(rows)); !slices.Equal(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
}

// TestNeedYouUnfoldsActive: w to a row under a folded active project
// unfolds it, and under a folded divider opens it.
func TestNeedYouUnfoldsActive(t *testing.T) {
	m, _ := sideDash(t)
	m.sideSel = "p:beta"
	drain(t, m, keyPress(m, "left"))
	if slices.Contains(rowKeys(m.tree()), "t:beta/t-0005") {
		t.Fatal("← didn't fold beta")
	}
	drain(t, m, keyPress(m, "W"))
	if got := sideCursorKey(m); got != "t:beta/t-0005" || m.folds.IsFolded("beta") {
		t.Fatalf("W: cursor %q, folds %+v", got, m.folds)
	}
}

// TestSideKeysWhileActing: the sidebar's keys go on while an action
// runs. Activating a project shows it expanded at once, before the
// server answers; the cursor's moves then happen at once, and space
// waits for the activation, then acts on the row it was pressed on
// (T283: the keys were dropped, so space never asked about alpha).
func TestSideKeysWhileActing(t *testing.T) {
	m, src, _ := foldDash(t)
	m.sideSel = dividerKey
	drain(t, m, keyPress(m, "right")) // unfolds it
	drain(t, m, keyPress(m, "down"))
	if m.sideSel != "p:beta" {
		t.Fatalf("cursor %q under the divider", m.sideSel)
	}
	activate := keyPress(m, "S")
	if !m.busy || activate == nil {
		t.Fatal("space on beta started no activation")
	}
	if !slices.Contains(rowKeys(m.tree()), "c:beta") {
		t.Fatalf("beta not expanded at once: %v", rowKeys(m.tree()))
	}
	for m.sideSel != "p:alpha" {
		before := m.sideSel
		drain(t, m, keyPress(m, "up"))
		if m.sideSel == before {
			t.Fatalf("k while activating left the cursor on %q", before)
		}
	}
	drain(t, m, keyPress(m, "S"))
	if m.top() != nil || len(m.queued) != 1 {
		t.Fatalf("space while activating: popup %T, queued %d", m.top(), len(m.queued))
	}
	drain(t, m, activate)
	if !slices.Equal(src.lifecycle, []string{"beta activate"}) {
		t.Fatalf("lifecycle %v", src.lifecycle)
	}
	if _, ok := m.top().(*confirmView); !ok || !strings.Contains(whole(m), "Deactivate alpha?") {
		t.Fatalf("after the activation: popup %T, no question about alpha:\n%s", m.top(), whole(m))
	}
	drain(t, m, keyPress(m, "Y"))
	if !slices.Equal(src.lifecycle, []string{"beta activate", "alpha deactivate"}) {
		t.Fatalf("lifecycle after y %v", src.lifecycle)
	}
}

// TestFoldSections: a console folds sections in the view (view.sections)
// and ui.json, which a new dashboard starts with; z keeps them. On a
// server of protocol 14, which doesn't know view.sections, the console
// keeps them as its own, through the view's new versions, until a server
// that knows the method takes them.
func TestFoldSections(t *testing.T) {
	m, _, ui := foldDash(t)
	server := view.View{Name: "v", Seq: 1}
	var calls []string
	protocol := 14
	vc := &ViewConn{v: server, up: true, watch: map[chan struct{}]bool{},
		serve: func(method string, p proto.ViewParams) (view.View, error) {
			if method == proto.MethodViewSections || method == proto.MethodViewTree {
				calls = append(calls, method)
			}
			switch {
			case method == proto.MethodViewSections && protocol == 14:
				return view.View{}, proto.Errorf(proto.ErrUnknownMethod, "unknown method %q", method)
			case method == proto.MethodViewSections:
				server.FoldSections(p.Sections, p.On)
			case method == proto.MethodViewTree:
				folded := server.SectionsFolded
				server.Tree = *p.Tree
				server.SectionsFolded = folded
				if protocol == 14 {
					server.SectionsFolded = nil
				}
			}
			server.Seq++
			return server, nil
		}}
	m.view = vc
	// done runs cmd and the commands it batches, and gives the dashboard
	// what the view calls answer.
	var done func(cmd tea.Cmd)
	done = func(cmd tea.Cmd) {
		t.Helper()
		if cmd == nil {
			t.Fatal("no view call")
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				if c != nil {
					done(c)
				}
			}
		case viewDoneMsg:
			m.update(msg)
		}
	}
	done(m.foldSections([]string{"ticker.ticker", "ci.prs@alpha"}, true))
	if v := vc.View(); !m.folds.IsSectionFolded("ticker.ticker") || !v.IsSectionFolded("ci.prs@alpha") || m.msg != "" {
		t.Fatalf("a protocol-14 server: folds %+v, view %+v, msg %q", m.folds, vc.View().Tree, m.msg)
	}
	if m.foldSections([]string{"ticker.ticker"}, true) != nil {
		t.Fatal("folding a folded section called the server")
	}
	// z sends view.tree, which the old server keeps without the folds:
	// the console's stay, here and in ui.json.
	done(keyPress(m, "Z"))
	if v := vc.View(); !m.folds.IsSectionFolded("ticker.ticker") || !v.IsSectionFolded("ticker.ticker") {
		t.Fatalf("z lost the folds: %+v", m.folds)
	}
	if l := LoadLayout(ui).Tree; !l.IsSectionFolded("ticker.ticker") || !l.IsSectionFolded("ci.prs@alpha") {
		t.Fatalf("ui.json: %+v", l)
	}
	n := newDash(DashOptions{Source: &fakeSource{data: testData()}, Width: 120, Height: 30, UIFile: ui})
	if !n.folds.IsSectionFolded("ticker.ticker") {
		t.Fatalf("a new dashboard didn't start with ui.json's folds: %+v", n.folds)
	}
	// A server of protocol 15 keeps them for the view.
	protocol = 15
	done(m.foldSections([]string{"ticker.ticker"}, false))
	if v := vc.View(); v.IsSectionFolded("ticker.ticker") || v.IsSectionFolded("ci.prs@alpha") || m.folds.IsSectionFolded("ticker.ticker") {
		t.Fatalf("a protocol-15 server: view %+v, folds %+v", v.Tree, m.folds)
	}
	if want := []string{proto.MethodViewSections, proto.MethodViewTree, proto.MethodViewSections}; !slices.Equal(calls, want) {
		t.Fatalf("calls %v, want %v", calls, want)
	}
}

// TestUISectionsRoundTrip: ui.json keeps the sections' folds with the
// tree, and drops a broken list.
func TestUISectionsRoundTrip(t *testing.T) {
	ui := filepath.Join(t.TempDir(), "ui.json")
	tree := view.Tree{Folded: []string{"a"}, SectionsFolded: []string{"m.v", "m.w@a"}}
	if err := SaveTree(ui, tree); err != nil {
		t.Fatal(err)
	}
	if got := LoadLayout(ui).Tree; !slices.Equal(got.SectionsFolded, tree.SectionsFolded) || !got.IsFolded("a") {
		t.Fatalf("round trip: %+v", got)
	}
	if b, _ := os.ReadFile(ui); !strings.Contains(string(b), `"sections_folded"`) {
		t.Fatalf("ui.json: %s", b)
	}
	if err := SaveTree(ui, view.Tree{SectionsFolded: []string{"not a section"}}); err != nil {
		t.Fatal(err)
	}
	if got := LoadLayout(ui).Tree; len(got.SectionsFolded) != 0 {
		t.Fatalf("a broken list was kept: %+v", got)
	}
}
