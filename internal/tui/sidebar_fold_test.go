package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
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
		run(m, keyPress(m, s.key))
		if open() != s.open || m.sideSel != dividerKey {
			t.Fatalf("step %d (%s): open %v, cursor %q", i, s.key, open(), m.sideSel)
		}
	}
	run(m, keyPress(m, "space"))
	if open() || len(src.lifecycle) != 0 || m.msg != "" {
		t.Fatalf("space on the divider: lifecycle %v msg %q", src.lifecycle, m.msg)
	}
	// A click on it toggles it.
	y := slices.Index(rowKeys(m.tree()), dividerKey) + 1
	run(m, m.sideClick(tea.Mouse{X: 5, Y: y, Button: tea.MouseLeft}))
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
	m.sideSel = "c:alpha"
	if !slices.Contains(rowKeys(m.tree()), m.sideSel) {
		t.Fatalf("no row %s: %v", m.sideSel, rowKeys(m.tree()))
	}
	run(m, keyPress(m, "z"))
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
	run(m, keyPress(m, "Z"))
	want := []string{"p:alpha", "c:alpha", dividerKey, "p:beta", "c:beta", "t:beta/t-0005", "t:beta/t-0006"}
	if got := rowKeys(m.tree()); !slices.Equal(got, want) || len(src.lifecycle) != 0 {
		t.Fatalf("Z: tree %v, want %v; lifecycle %v", got, want, src.lifecycle)
	}
	// z on a dormant row moves the cursor to the divider.
	m.sideSel = "c:beta"
	run(m, keyPress(m, "z"))
	if m.sideSel != dividerKey {
		t.Fatalf("z on beta's coordinator: cursor %q", m.sideSel)
	}

	m.sideSel = "p:alpha"
	run(m, keyPress(m, "p"))
	if !slices.Equal(src.lifecycle, []string{"alpha pin"}) || !m.projectData("alpha").pinned() {
		t.Fatalf("p: lifecycle %v", src.lifecycle)
	}
	run(m, keyPress(m, "p"))
	if !slices.Equal(src.lifecycle, []string{"alpha pin", "alpha unpin"}) || m.projectData("alpha").pinned() {
		t.Fatalf("p again: lifecycle %v", src.lifecycle)
	}
	m.sideSel = dividerKey
	run(m, keyPress(m, "p"))
	if len(src.lifecycle) != 2 || !strings.Contains(m.msg, "p on a project") {
		t.Fatalf("p on the divider: lifecycle %v msg %q", src.lifecycle, m.msg)
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
	run(m, keyPress(m, "left"))
	if slices.Contains(rowKeys(m.tree()), "t:beta/t-0005") {
		t.Fatal("← didn't fold beta")
	}
	run(m, keyPress(m, "w"))
	if got := sideCursorKey(m); got != "t:beta/t-0005" || m.folds.IsFolded("beta") {
		t.Fatalf("w: cursor %q, folds %+v", got, m.folds)
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
	run(m, keyPress(m, "right")) // unfolds it
	run(m, keyPress(m, "j"))
	if m.sideSel != "p:beta" {
		t.Fatalf("cursor %q under the divider", m.sideSel)
	}
	activate := keyPress(m, "space")
	if !m.busy || activate == nil {
		t.Fatal("space on beta started no activation")
	}
	if !slices.Contains(rowKeys(m.tree()), "c:beta") {
		t.Fatalf("beta not expanded at once: %v", rowKeys(m.tree()))
	}
	for m.sideSel != "p:alpha" {
		before := m.sideSel
		run(m, keyPress(m, "k"))
		if m.sideSel == before {
			t.Fatalf("k while activating left the cursor on %q", before)
		}
	}
	run(m, keyPress(m, "space"))
	if m.top() != nil || len(m.queued) != 1 {
		t.Fatalf("space while activating: popup %T, queued %d", m.top(), len(m.queued))
	}
	run(m, activate)
	if !slices.Equal(src.lifecycle, []string{"beta activate"}) {
		t.Fatalf("lifecycle %v", src.lifecycle)
	}
	if _, ok := m.top().(*confirmView); !ok || !strings.Contains(whole(m), "Deactivate alpha?") {
		t.Fatalf("after the activation: popup %T, no question about alpha:\n%s", m.top(), whole(m))
	}
	run(m, keyPress(m, "y"))
	if !slices.Equal(src.lifecycle, []string{"beta activate", "alpha deactivate"}) {
		t.Fatalf("lifecycle after y %v", src.lifecycle)
	}
}
