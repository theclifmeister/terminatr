package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/view"
)

// TestInactiveProjectCollapsed: an inactive project hangs under the
// "inactive (n)" divider, folded by default; unfolded, it is its row
// alone, marked; ] skips it; space on it activates it, and on an active
// project with running sessions asks first (docs/SPEC.md §4, §5.1).
func TestInactiveProjectCollapsed(t *testing.T) {
	d := testData()
	off := config.Defaults
	off.Active = false
	d.Projects[1].Safety = &off // beta: a thread runs, a coordinator doesn't
	keys := func(in treeIn) []string {
		var out []string
		for _, r := range buildTree(d.Projects, d.Sessions, in) {
			out = append(out, r.key())
			if r.slug == "beta" && !r.inactive {
				t.Errorf("%s isn't marked inactive", r.key())
			}
		}
		return out
	}
	if got, want := keys(treeIn{}), []string{"p:alpha", "c:alpha", dividerKey}; !slices.Equal(got, want) {
		t.Errorf("tree %v, want beta under the folded divider: %v", got, want)
	}
	open := treeIn{tree: view.Tree{InactiveOpen: true}}
	if got, want := keys(open), []string{"p:alpha", "c:alpha", dividerKey, "p:beta"}; !slices.Equal(got, want) {
		t.Errorf("tree %v, want beta collapsed: %v", got, want)
	}
	// Its row says so in every icon set's glyph.
	for _, r := range buildTree(d.Projects, d.Sessions, open) {
		if r.slug == "beta" {
			// In the grid's inactive column, after the paused one, before
			// the needs-you one.
			if line := treeCells(r, 30, false, false); gridCol(line, gridInactive) != ic().inactive {
				t.Errorf("row %q, want %q in its inactive column", ansi.Strip(line), ic().inactive)
			}
		}
	}

	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 30, State: DashState{Current: "alpha"}})
	m.folds.InactiveOpen = true
	m.setData(src.data)
	drain(t, m, prefixed(m, "]"))
	if !slices.Equal(src.opened, []string{"alpha"}) {
		t.Errorf("] opened %v; it skips the inactive beta, back to alpha", src.opened)
	}

	// space on beta activates it at once; on alpha, whose coordinator
	// runs, it asks, and n keeps it.
	keyPress(m, "tab")
	m.sideSel = "p:beta"
	drain(t, m, keyPress(m, "S"))
	m.sideSel = "p:alpha"
	drain(t, m, keyPress(m, "S"))
	if len(src.lifecycle) != 1 || src.lifecycle[0] != "beta activate" {
		t.Fatalf("lifecycle %v, want beta activated and alpha asked", src.lifecycle)
	}
	if !strings.Contains(whole(m), "Deactivate alpha? This stops its coordinator and t-0002") {
		t.Fatalf("no question:\n%s", whole(m))
	}
	keyPress(m, "N")
	m.sideSel = "p:alpha"
	drain(t, m, keyPress(m, "S"))
	drain(t, m, keyPress(m, "Y"))
	if want := []string{"beta activate", "alpha deactivate"}; !slices.Equal(src.lifecycle, want) {
		t.Errorf("lifecycle %v, want %v", src.lifecycle, want)
	}
}

// TestExpandInactiveProject: → expands an inactive project without
// activating it, its coordinator and threads dormant; a dormant thread
// doesn't attach; ← folds it; enter on its coordinator activates the
// project and opens the coordinator (docs/SPEC.md §4, §5.1).
func TestExpandInactiveProject(t *testing.T) {
	d := testData()
	off := config.Defaults
	off.Active = false
	d.Projects[1].Safety = &off
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 30, State: DashState{Current: "alpha"}})
	m.setData(src.data)
	keyPress(m, "tab")
	// → on the divider unfolds it, then on beta expands beta.
	m.sideSel = dividerKey
	drain(t, m, keyPress(m, "right"))
	m.sideSel = "p:beta"
	drain(t, m, keyPress(m, "right"))
	var keys []string
	for _, r := range m.tree() {
		keys = append(keys, r.key())
		if r.slug == "beta" && r.kind != treeProject && !r.dormant {
			t.Errorf("%s isn't dormant", r.key())
		}
	}
	if want := []string{"p:alpha", "c:alpha", dividerKey, "p:beta", "c:beta", "t:beta/t-0005", "t:beta/t-0006"}; !slices.Equal(keys, want) {
		t.Fatalf("expanded tree %v, want %v", keys, want)
	}
	if m.sideSel != "p:beta" || len(src.lifecycle) != 0 {
		t.Fatalf("cursor %q, lifecycle %v: expanding keeps the cursor and activates nothing", m.sideSel, src.lifecycle)
	}
	if !strings.Contains(whole(m), "coordinator") || !strings.Contains(whole(m), "T4 ") {
		t.Fatalf("expanded beta:\n%s", whole(m))
	}
	// A dormant thread says why it doesn't attach.
	m.sideSel = "t:beta/t-0005"
	drain(t, m, keyPress(m, "enter"))
	if !strings.Contains(m.msg, "t-0005 is dormant") || m.result.Attach != "" {
		t.Fatalf("enter on a dormant thread: msg %q attach %q", m.msg, m.result.Attach)
	}
	// ← on the project folds it.
	m.sideSel = "p:beta"
	drain(t, m, keyPress(m, "left"))
	if slices.ContainsFunc(m.tree(), func(r treeRow) bool { return r.key() == "c:beta" }) {
		t.Fatal("← didn't fold beta")
	}
	// ← on it folded goes up to the divider.
	drain(t, m, keyPress(m, "left"))
	if m.sideSel != dividerKey {
		t.Fatalf("← on folded beta: cursor %q, want the divider", m.sideSel)
	}
	// Expanded again, enter on its coordinator activates it and opens it.
	m.sideSel = "p:beta"
	drain(t, m, keyPress(m, "right"))
	m.sideSel = "c:beta"
	drain(t, m, keyPress(m, "enter"))
	if !slices.Equal(src.lifecycle, []string{"beta activate"}) || !slices.Contains(src.opened, "beta") {
		t.Fatalf("lifecycle %v, opened %v: want beta activated and its coordinator opened", src.lifecycle, src.opened)
	}
}

// TestDashboardActiveToggle: the project's section header names its
// state in a label at its right, the footer says what space does, and
// space on the dashboard, or a click on the label, toggles the project
// shown, asking first while sessions run.
func TestDashboardActiveToggle(t *testing.T) {
	d := testData()
	off := config.Defaults
	off.Active = false
	d.Projects[1].Safety = &off // beta
	src := &fakeSource{data: d}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.layout.Details = false
	m.setData(src.data)
	m.sel = "p:beta"
	out := whole(m)
	if !strings.Contains(out, " beta ─") || !strings.Contains(out, "─ ⊘ inactive — S to activate") || !strings.Contains(out, "S activate") {
		t.Fatalf("inactive project: header or footer missing:\n%s", out)
	}
	label := -1
	for y := 1; y <= m.bodyRows(); y++ {
		if m.stateAt(m.w-3, y) == "beta" {
			label = y
		}
	}
	if label < 0 || m.stateAt(2, label) != "" {
		t.Fatalf("the state label is at row %d", label)
	}
	drain(t, m, m.click(m.w-3, label, false))
	if !slices.Equal(src.lifecycle, []string{"beta activate"}) {
		t.Fatalf("lifecycle %v, want beta activated", src.lifecycle)
	}
	if out := whole(m); !strings.Contains(out, "● active — S to deactivate") || !strings.Contains(out, "S deactivate") {
		t.Fatalf("after activating:\n%s", out)
	}
	m.current, m.sel = "alpha", "p:alpha"
	m.rebuild()
	on := config.Defaults
	m.projectData("alpha").Safety = &on
	drain(t, m, prefixed(m, "S")) // a coordinator's row: S would type through
	if len(src.lifecycle) != 1 || !strings.Contains(whole(m), "Deactivate alpha?") {
		t.Fatalf("alpha runs sessions: want a question, lifecycle %v:\n%s", src.lifecycle, whole(m))
	}
	drain(t, m, keyPress(m, "Y"))
	if !slices.Equal(src.lifecycle, []string{"beta activate", "alpha deactivate"}) {
		t.Errorf("lifecycle %v", src.lifecycle)
	}
}

// TestRefoldNotUndoneByOlderView: a version of the view from before
// this console's refold, arriving while the refold is on its way, doesn't
// fold back what was just unfolded; once the view has the change, the
// view's folds are taken again, another console's too.
func TestRefoldNotUndoneByOlderView(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)
	vc := &ViewConn{v: view.View{Name: "v", Seq: 1}, up: true, watch: map[chan struct{}]bool{}}
	m.view = vc
	m.refold(func(t *view.Tree) { t.InactiveOpen = true })
	vc.set(view.View{Name: "v", Seq: 2}) // e.g. a sidebar move's, sent before
	m.fromView()
	if !m.folds.InactiveOpen {
		t.Fatal("an older view folded the divider back while the refold was on its way")
	}
	vc.set(view.View{Name: "v", Seq: 3, Tree: view.Tree{InactiveOpen: true}})
	m.update(viewDoneMsg{tree: true})
	if !m.folds.InactiveOpen || m.treePending != 0 {
		t.Fatalf("after the refold landed: folds %+v, %d pending", m.folds, m.treePending)
	}
	vc.set(view.View{Name: "v", Seq: 4}) // another console folds it
	m.fromView()
	if m.folds.InactiveOpen {
		t.Fatal("the view's own folds aren't taken once nothing is on its way")
	}
}
