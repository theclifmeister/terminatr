package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// prciLook is the bundled pr-ci-column mod's cell for a PR's checks, as
// examples/mods/pr-ci-column/main.ts publishes it.
var prciLook = map[string][]widget.Span{
	"pass":    {{Icon: "ci.pass", Role: "good"}},
	"fail":    {{Icon: "ci.fail", Role: "bad"}},
	"pending": {{Icon: "ci.run", Role: "faint"}},
}

// prciProjects is a project whose threads' PRs pass, fail, run, and have
// none.
func prciProjects() []ProjectData {
	p := ProjectData{Slug: "demo"}
	for i, checks := range []string{"pass", "fail", "pending", ""} {
		p.Threads = append(p.Threads, ThreadRow{Record: &thread.Record{ID: fmt.Sprintf("t-%04d", i+1), State: thread.Running},
			Checks: checks, PR: i + 1})
	}
	return []ProjectData{p}
}

// prciCells are the mod's column and its cells for prciProjects.
func prciCells(ps []ProjectData) *modCells {
	m := &proto.ModCells{Columns: []proto.ModColumn{{ID: mod.PRCIColumn, Title: "CI", Width: 2, Where: []string{proto.SlotSidebarThread}}}}
	for _, t := range ps[0].Threads {
		if ss := prciLook[t.Checks]; ss != nil {
			m.Cells = append(m.Cells, proto.ModCell{Mod: mod.BundledPRCI, Column: "ci",
				Target: proto.CellTarget{Project: ps[0].Slug, Thread: t.Record.ID}, Spans: ss})
		}
	}
	return newModCells(m, nil)
}

// TestPRCIColumn: the bundled mod's column is the PR/CI column, each
// thread's checks drawn in its role's style, in every icon set (mods
// design §11, proof 1). With the mod off there is no PR/CI column: it
// takes no room and no row draws a check.
func TestPRCIColumn(t *testing.T) {
	defer icons.Store(ic())
	ps := prciProjects()
	ids := func(cs []sideCol) (out []string) {
		for _, c := range cs {
			out = append(out, c.id)
		}
		return out
	}
	for _, set := range []*iconSet{&unicodeIcons, &nerdIcons, &asciiIcons} {
		icons.Store(set)
		offRows := buildTree(ps, nil, treeIn{})
		modRows := buildTree(ps, nil, treeIn{cells: prciCells(ps)})
		if got := ids(modRows[0].columns()); slices.Contains(got, view.ColCI) || !slices.Contains(got, mod.PRCIColumn) {
			t.Fatalf("columns with the mod %v", got)
		}
		if got := ids(offRows[0].columns()); slices.Contains(got, view.ColCI) || slices.Contains(got, mod.PRCIColumn) {
			t.Fatalf("columns without the mod %v", got)
		}
		if on, off := colsWidth(modRows[0].columns()), colsWidth(offRows[0].columns()); on != off+2 {
			t.Errorf("the grid is %d cells with the mod, %d without", on, off)
		}
		for i, r := range modRows {
			if r.kind != treeThread || r.ci == "" {
				continue
			}
			ss := prciLook[r.ci]
			open, _, _ := strings.Cut(roleStyle(ss[0].Role).Render("\x00"), "\x00")
			glyph := strings.TrimSpace(widgetLook{}.Icon(ss[0].Icon))
			if m := treeCells(r, 60, false, false); !strings.Contains(m, open+glyph) {
				t.Errorf("%s: no %q in %q", r.key(), open+glyph, m)
			}
			if g := ansi.Strip(treeCells(offRows[i], 60, false, false)); strings.Contains(g, glyph) {
				t.Errorf("%s without the mod draws %q: %q", r.key(), glyph, g)
			}
		}
	}
}

// TestPRCIOrder: a layout's ci is the mod's column, once, wherever the
// layout puts it.
func TestPRCIOrder(t *testing.T) {
	for _, c := range []struct {
		order, want []string
	}{
		{[]string{"count", "ci", "state"}, []string{"count", mod.PRCIColumn, "state"}},
		{[]string{"state", mod.PRCIColumn, "count"}, []string{"state", mod.PRCIColumn, "count"}},
		{[]string{mod.PRCIColumn, "ci", "state"}, []string{mod.PRCIColumn, "state"}},
		{[]string{"state", "needs"}, []string{"state", "needs"}},
	} {
		if got := prciOrder(c.order); !slices.Equal(got, c.want) {
			t.Errorf("prciOrder(%v) = %v, want %v", c.order, got, c.want)
		}
	}
	// No layout naming the column adds it a second time.
	mc := prciCells(prciProjects())
	if got := mc.sideOrder(nil); !slices.Equal(got, []string{"count", mod.PRCIColumn, "state", "remote", "paused", "inactive", "needs"}) {
		t.Errorf("default order with the mod %v", got)
	}
	if got := mc.sideOrder([]string{"state", "needs"}); slices.Contains(got, view.ColCI) || !slices.Contains(got, mod.PRCIColumn) {
		t.Errorf("a layout without ci %v", got)
	}
}

// BenchmarkSidebarCI is the sidebar's frame cost without the PR/CI
// column and with the bundled mod's cells: 10
// projects of 8 threads, built (buildTree, as each poll does) and drawn
// (treeCells per row, as each frame does).
func BenchmarkSidebarCI(b *testing.B) {
	var ps []ProjectData
	for p := range 10 {
		d := ProjectData{Slug: fmt.Sprintf("p%d", p)}
		for i := range 8 {
			d.Threads = append(d.Threads, ThreadRow{Record: &thread.Record{ID: fmt.Sprintf("t-%04d", i+1), State: thread.Running},
				Checks: []string{"pass", "fail", "pending", ""}[i%4], PR: i + 1})
		}
		ps = append(ps, d)
	}
	m := &proto.ModCells{Columns: []proto.ModColumn{{ID: mod.PRCIColumn, Title: "CI", Width: 2, Where: []string{proto.SlotSidebarThread}}}}
	for _, d := range ps {
		for _, t := range d.Threads {
			if ss := prciLook[t.Checks]; ss != nil {
				m.Cells = append(m.Cells, proto.ModCell{Mod: mod.BundledPRCI, Column: "ci",
					Target: proto.CellTarget{Project: d.Slug, Thread: t.Record.ID}, Spans: ss})
			}
		}
	}
	for _, c := range []struct {
		name string
		in   func() treeIn
	}{
		{"off", func() treeIn { return treeIn{} }},
		{"mod", func() treeIn { return treeIn{cells: newModCells(m, nil)} }},
	} {
		b.Run(c.name+"/build", func(b *testing.B) {
			for b.Loop() {
				buildTree(ps, nil, c.in())
			}
		})
		rows := buildTree(ps, nil, c.in())
		b.Run(c.name+"/draw", func(b *testing.B) {
			for b.Loop() {
				for _, r := range rows {
					treeCells(r, 40, false, false)
				}
			}
		})
	}
}
