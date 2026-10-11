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

func treeKeys(rows []treeRow) []string {
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
	if want := []string{"p:c", "c:c", "p:a", "c:a", "p:e", "c:e", dividerKey}; !slices.Equal(treeKeys(rows), want) {
		t.Fatalf("tree %v, want %v", treeKeys(rows), want)
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
	if want := []string{"p:c", "c:c", "p:a", "p:e", "c:e", dividerKey, "p:b", "p:d"}; !slices.Equal(treeKeys(rows), want) {
		t.Fatalf("tree %v, want %v", treeKeys(rows), want)
	}
	if l := ansi.Strip(treeCells(rows[5], 35, false, false)); !strings.Contains(l, "▾ inactive (2)") {
		t.Fatalf("unfolded divider %q", l)
	}
	// The slim strip shows the divider too, shrunk.
	if got := treeKeys(slimRows(rows)); !slices.Equal(got, []string{"p:c", "p:a", "p:e", dividerKey, "p:b", "p:d"}) {
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
	open := func() bool { return slices.Contains(treeKeys(m.tree()), "p:beta") }
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
	y := slices.Index(treeKeys(m.tree()), dividerKey) + 1
	run(m, m.sideClick(tea.Mouse{X: 5, Y: y, Button: tea.MouseLeft}))
	if !open() || len(src.opened) != 0 {
		t.Fatalf("click: open %v, opened %v", open(), src.opened)
	}
	if !LoadLayout(ui).Tree.InactiveOpen {
		t.Fatalf("ui.json: %+v", LoadLayout(ui).Tree)
	}
	n := newDash(DashOptions{Source: src, Width: 120, Height: 30, UIFile: ui})
	n.setData(src.data)
	if !slices.Contains(treeKeys(n.tree()), "p:beta") {
		t.Fatalf("a new dashboard didn't start with ui.json's tree: %v", treeKeys(n.tree()))
	}
}

// TestFoldAllAndPin: z folds every project and the divider, its cursor
// moving up to what still shows; Z unfolds them all, the inactive
// projects expanded (never activated); p pins a project, at once and in
// its setting, and again unpins it.
func TestFoldAllAndPin(t *testing.T) {
	m, src, ui := foldDash(t)
	m.sideSel = "c:alpha"
	if !slices.Contains(treeKeys(m.tree()), m.sideSel) {
		t.Fatalf("no row %s: %v", m.sideSel, treeKeys(m.tree()))
	}
	run(m, keyPress(m, "z"))
	if got := treeKeys(m.tree()); !slices.Equal(got, []string{"p:alpha", dividerKey}) || m.sideSel != "p:alpha" {
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
	if got := treeKeys(m.tree()); !slices.Equal(got, want) || len(src.lifecycle) != 0 {
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
