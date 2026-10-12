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

// TestPRCIColumnParity: the bundled mod's column draws each thread row
// exactly as the built-in ci column does, in every icon set, styles
// included: proof 1 of the mod API (mods design §11) loses nothing.
func TestPRCIColumnParity(t *testing.T) {
	defer icons.Store(ic())
	ps := prciProjects()
	for _, set := range []*iconSet{&unicodeIcons, &nerdIcons, &asciiIcons} {
		icons.Store(set)
		goRows := buildTree(ps, nil, treeIn{})
		modRows := buildTree(ps, nil, treeIn{cells: prciCells(ps)})
		if len(goRows) != len(modRows) {
			t.Fatalf("%d rows, %d with the mod", len(goRows), len(modRows))
		}
		ids := func(cs []sideCol) (out []string) {
			for _, c := range cs {
				out = append(out, c.id)
			}
			return out
		}
		if got := ids(modRows[0].columns()); slices.Contains(got, view.ColCI) || !slices.Contains(got, mod.PRCIColumn) {
			t.Fatalf("columns with the mod %v", got)
		}
		for i := range goRows {
			for _, w := range []int{20, 34, 60} {
				g, m := treeCells(goRows[i], w, false, false), treeCells(modRows[i], w, false, false)
				if ansi.Strip(g) != ansi.Strip(m) {
					t.Errorf("%s at %d cells:\n go  %q\n mod %q", goRows[i].key(), w, g, m)
				}
				// Where the row is wide enough for the grid, the glyph is drawn
				// in its role's style; the blank after it may be styled
				// apart (it draws the same).
				r := modRows[i]
				if r.kind != treeThread || r.ci == "" {
					continue
				}
				ss := prciLook[r.ci]
				open, _, _ := strings.Cut(roleStyle(ss[0].Role).Render("\x00"), "\x00")
				glyph := strings.TrimSpace(widgetLook{}.Icon(ss[0].Icon))
				if strings.Contains(ansi.Strip(g), glyph) && !strings.Contains(m, open+glyph) {
					t.Errorf("%s: no %q in %q", r.key(), open+glyph, m)
				}
			}
		}
	}
}

// TestPRCIOrder: a layout's ci is the mod's column while the mod is on,
// the built-in one while it is off, once, wherever the layout puts it.
func TestPRCIOrder(t *testing.T) {
	on := map[string]modColumn{mod.PRCIColumn: {width: 2, title: "CI"}}
	for _, c := range []struct {
		order []string
		mods  map[string]modColumn
		want  []string
	}{
		{[]string{"count", "ci", "state"}, on, []string{"count", mod.PRCIColumn, "state"}},
		{[]string{"count", "ci", "state"}, nil, []string{"count", "ci", "state"}},
		{[]string{"state", mod.PRCIColumn, "count"}, nil, []string{"state", "ci", "count"}},
		{[]string{mod.PRCIColumn, "ci", "state"}, on, []string{mod.PRCIColumn, "state"}},
		{[]string{"state", "needs"}, on, []string{"state", "needs"}},
	} {
		if got := prciOrder(c.order, c.mods); !slices.Equal(got, c.want) {
			t.Errorf("prciOrder(%v, on %v) = %v, want %v", c.order, c.mods != nil, got, c.want)
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

// BenchmarkSidebarCI is the sidebar's frame cost with the PR/CI column
// drawn by the built-in Go cell and by the bundled mod's cells: 10
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
		{"go", func() treeIn { return treeIn{} }},
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
