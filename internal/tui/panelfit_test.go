package tui

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// randomPanel is a random panel for fitPanel: its rows, its core lines'
// count, its must-see lines and its sections' asks, in the order drawn
// (those placed among core's blocks by their line, then the bottom ones).
func randomPanel(r *rand.Rand) (h int, must []int, asks []panelAsk) {
	h = r.IntN(60)
	n := r.IntN(80)
	for i := range n {
		if r.IntN(4) == 0 {
			must = append(must, i)
		}
	}
	// Core's blocks start at a few lines; the first is "above".
	var starts []int
	for range 1 + r.IntN(4) {
		starts = append(starts, r.IntN(n+1))
	}
	slices.Sort(starts)
	k := r.IntN(7)
	for range k {
		a := panelAsk{want: r.IntN(16) - 1, at: -1, sel: false}
		if r.IntN(3) > 0 {
			b := r.IntN(len(starts))
			a.at, a.above = starts[b], b == 0
		}
		asks = append(asks, a)
	}
	// Placed ones first, by line; the bottom ones after.
	slices.SortStableFunc(asks, func(x, y panelAsk) int {
		kx, ky := x.at, y.at
		if kx < 0 {
			kx = 1 << 20
		}
		if ky < 0 {
			ky = 1 << 20
		}
		return kx - ky
	})
	if k > 0 && r.IntN(2) == 0 {
		asks[r.IntN(k)].sel = true
	}
	return h, must, asks
}

// TestFitPanelProperties: whatever the panel, the sections it draws
// keep every must-see line on the first screen where it was without
// them (the guarantee, §11.6), stay within the shares (§11.5), never get
// more body than they ask (at most 12 lines), and the count of headers
// left out is +N. A panel under panelMin rows draws them all at the
// bottom, whole.
func TestFitPanelProperties(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 348))
	for it := range 20000 {
		h, must, asks := randomPanel(r)
		f := fitPanel(h, must, asks)
		fail := func(format string, args ...any) {
			t.Helper()
			t.Fatalf("case %d (h %d, must %v, asks %+v) → %+v: "+format, append([]any{it, h, must, asks, f}, args...)...)
		}
		more := 0
		for j, a := range asks {
			want := min(max(a.want, 0), modview.MaxSidebarHeight)
			switch {
			case !f.show[j]:
				more++
				if f.body[j] != 0 || f.shrunk[j] {
					fail("section %d left out with a body or shrunk", j)
				}
			case f.shrunk[j] && f.body[j] != 0:
				fail("section %d shrunk with a body", j)
			case !f.shrunk[j] && f.body[j] != want:
				fail("section %d has %d lines, asks %d", j, f.body[j], want)
			}
			if f.body[j] > want {
				fail("section %d has more than it asks", j)
			}
			if f.bottom[j] != (a.at < 0 || h < panelMin) {
				fail("section %d bottom %v", j, f.bottom[j])
			}
		}
		if more != f.more {
			fail("+%d for %d left out", f.more, more)
		}
		if h < panelMin {
			if f.more != 0 || slices.Contains(f.shrunk, true) {
				fail("a short panel gave way")
			}
			continue
		}
		// The guarantee.
		for _, k := range must {
			if k >= h {
				continue
			}
			shift := 0
			for j, a := range asks {
				if !f.bottom[j] && a.at <= k {
					shift += f.rows(j)
				}
			}
			if k+shift >= h {
				fail("must-see line %d pushed to %d", k, k+shift)
			}
		}
		// The shares: half for all, a quarter for those placed among
		// core's blocks, a quarter for headers (the selected one kept).
		if u := f.used(nil); u > h/2 {
			fail("sections take %d rows of %d", u, h)
		}
		if u := f.used(func(j int) bool { return !f.bottom[j] }); u > h/4 {
			fail("sections among core's take %d rows of %d", u, h)
		}
		selShown := slices.ContainsFunc(asks, func(a panelAsk) bool { return a.sel }) && f.show[slices.IndexFunc(asks, func(a panelAsk) bool { return a.sel })]
		if n := f.headers(); n > h/4 && !(n == 1 && selShown) {
			fail("%d headers in %d rows", n, h)
		}
	}
}

// TestFitPanelOrder: the give-way order. Sections above core's blocks
// give their bodies up first, the lowest first, then those between, and
// the bottom ones only for the shares; then the headers go, the
// selected one last.
func TestFitPanelOrder(t *testing.T) {
	// The 14-row panel of the design (§11.5): the ticker above NEEDS YOU
	// (lines 4 to 8) gives its body up, its header stays.
	f := fitPanel(14, []int{0, 1, 2, 4, 5, 6, 7, 8}, []panelAsk{{want: 3, at: 3, above: true}})
	if !f.show[0] || !f.shrunk[0] || f.body[0] != 0 {
		t.Fatalf("14 rows: %+v", f)
	}
	// Room enough: nothing gives way.
	f = fitPanel(40, []int{4, 5, 6}, []panelAsk{{want: 3, at: 3, above: true}, {want: 2, at: 9}, {want: 4, at: -1}})
	if slices.Contains(f.shrunk, true) || f.more != 0 {
		t.Fatalf("40 rows: %+v", f)
	}
	// Two above, one between: the lower one above gives way first, then
	// the upper one, and the one between keeps its body while it can.
	asks := []panelAsk{{want: 2, at: 3, above: true}, {want: 2, at: 3, above: true}, {want: 2, at: 12}}
	f = fitPanel(48, []int{37}, asks)
	if f.shrunk[0] || !f.shrunk[1] || f.shrunk[2] {
		t.Fatalf("the lower one above first: %+v", f)
	}
	f = fitPanel(48, []int{38}, asks)
	if !f.shrunk[0] || !f.shrunk[1] || f.shrunk[2] {
		t.Fatalf("then the upper one: %+v", f)
	}
	// A must-see line at the last row: everything placed gives way, the
	// selected header last; the bottom one stays.
	asks = []panelAsk{{want: 1, at: 2, above: true, sel: true}, {want: 1, at: 2, above: true}, {want: 1, at: -1}}
	f = fitPanel(16, []int{15}, asks)
	if f.show[0] || f.show[1] || !f.show[2] || f.more != 2 {
		t.Fatalf("must-see at the end: %+v", f)
	}
	f = fitPanel(16, []int{12}, asks)
	if !f.show[0] || f.show[1] {
		t.Fatalf("the selected header goes last: %+v", f)
	}
	// Short panels: all at the bottom, whole.
	f = fitPanel(11, []int{0, 1, 2, 3}, []panelAsk{{want: 9, at: 1, above: true}, {want: 30, at: -1}})
	if !f.bottom[0] || f.body[0] != 9 || f.body[1] != modview.MaxSidebarHeight {
		t.Fatalf("short: %+v", f)
	}
}

// panelTestViews are coordinator sections of three mods, n body lines
// each, for project demo.
func panelTestViews(n int) []proto.ModView {
	var out []proto.ModView
	for _, id := range []string{"a", "b", "c"} {
		lines := make([]string, n)
		for i := range lines {
			lines[i] = id + " line " + strconv.Itoa(i+1)
		}
		out = append(out, proto.ModView{Mod: id, View: "v", Title: id, Slot: mod.SlotCoordinatorSection,
			Trees: []modview.Tree{{Tree: &widget.Widget{Kind: widget.KindText, Text: strings.Join(lines, "\n")}, Height: n}}})
	}
	return out
}

// TestLayPanelGuarantee: the coordinator's panel drawn with sections
// anywhere among core's blocks and without them: every must-see line on
// the first screen without them is on it with them, in the same order
// (§11.6), whatever the panel's height and the sections' places.
func TestLayPanelGuarantee(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 367))
	places := []string{"needs", "inbox", "threads", "deck", "a.v", "b.v", "c.v"}
	for it := range 400 {
		pl, hits := coordPanel(sampleWatch(), 40, time.Now())
		rows := 6 + r.IntN(40)
		order := slices.Clone(places)
		r.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		// Core's markers keep their order.
		var marks []string
		for _, s := range order {
			if slices.Contains(mod.CoordinatorBlocks, s) {
				marks = append(marks, s)
			}
		}
		slices.SortFunc(marks, func(a, b string) int {
			return slices.Index(mod.CoordinatorBlocks, a) - slices.Index(mod.CoordinatorBlocks, b)
		})
		k := 0
		for i, s := range order {
			if slices.Contains(mod.CoordinatorBlocks, s) {
				order[i] = marks[k]
				k++
			}
		}
		lay := layPanel(pl, hits, panelTestViews(1+r.IntN(8)), panelTarget{slot: mod.SlotCoordinatorSection, project: "demo"},
			panelOpts{order: order, rows: rows})
		if len(lay.hits) != len(lay.lines) {
			t.Fatalf("%d hits for %d lines", len(lay.hits), len(lay.lines))
		}
		at := 0
		for i, on := range pl.must {
			if !on || i >= rows {
				continue
			}
			j := slices.Index(lay.lines[at:], pl.lines[i])
			if j < 0 || at+j >= rows {
				t.Fatalf("case %d (%d rows, order %v): must-see line %d %q at %d:\n%s", it, rows, order, i, ansi.Strip(pl.lines[i]), at+j,
					ansi.Strip(strings.Join(lay.lines, "\n")))
			}
			at += j + 1
		}
	}
}
