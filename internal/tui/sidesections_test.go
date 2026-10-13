package tui

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestFitBandProperties: for any sidebar body, tree and band, the band
// keeps its caps (half the body, headers a quarter, none under
// bandMinBody rows), gives way bottom first, bodies before headers, and
// never scrolls a row that must stay (⚑, the cursor's, the one you are
// on) off the tree's rows.
func TestFitBandProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 353))
	for range 20000 {
		b, n := rng.IntN(60), rng.IntN(80)
		tree := make([]treeRow, n)
		for i := range tree {
			tree[i].hint = rng.IntN(6) == 0
		}
		if n > 0 && rng.IntN(2) == 0 {
			tree[rng.IntN(n)].here = true
		}
		cursorOnBand := false
		if n > 0 && rng.IntN(2) == 0 {
			tree[rng.IntN(n)].cursor = true
		} else {
			cursorOnBand = rng.IntN(2) == 0
		}
		asks := make([]bandAsk, rng.IntN(7))
		for j := range asks {
			asks[j].want = rng.IntN(15)
		}
		if cursorOnBand && len(asks) > 0 {
			asks[rng.IntN(len(asks))].cursor = true
		}
		cur, keep := bandKeep(tree, b)
		f := fitBand(b, n, cur, keep, asks)
		at := fmt.Sprintf("b=%d n=%d cur=%d keep=%v asks=%v: %+v", b, n, cur, keep, asks, f)

		if f.treeH+f.used() != b {
			t.Fatalf("%s: the tree's %d rows and the band's %d aren't the body", at, f.treeH, f.used())
		}
		if b < bandMinBody && f.headers() > 0 {
			t.Fatalf("%s: sections under %d rows", at, bandMinBody)
		}
		if f.used() > b/2 || f.headers() > b/4 {
			t.Fatalf("%s: over the caps", at)
		}
		more := 0
		for j, a := range asks {
			if f.body[j] > min(a.want, modview.MaxSidebarHeight) || !f.show[j] && f.body[j] > 0 {
				t.Fatalf("%s: section %d's body", at, j)
			}
			if f.shrunk[j] && (!f.show[j] || f.body[j] > 0 || a.want == 0) {
				t.Fatalf("%s: section %d shrunk wrongly", at, j)
			}
			if !f.show[j] && b >= bandMinBody {
				more++
			}
			// Bottom first: a body shrunk or a header dropped means
			// every one below gave its body up too.
			if (f.shrunk[j] || !f.show[j]) && b >= bandMinBody {
				for k := j + 1; k < len(asks); k++ {
					if f.body[k] > 0 {
						t.Fatalf("%s: section %d gave way before %d below it", at, j, k)
					}
				}
			}
		}
		if f.more != more {
			t.Fatalf("%s: more %d, %d not shown", at, f.more, more)
		}
		// The guarantee.
		if f.treeH < n {
			top := scrollTop(cur, max(f.treeH, 1), n)
			for _, k := range keep {
				if k < top || k >= top+f.treeH {
					t.Fatalf("%s: row %d scrolled off [%d, %d)", at, k, top, top+f.treeH)
				}
			}
		}
		// What has room keeps it: a short tree gives the band its caps.
		if b >= bandMinBody && n == 0 && len(asks) > 0 && len(asks) <= b/4 && !f.show[0] {
			t.Fatalf("%s: the first section was dropped with room", at)
		}
	}
}

// TestSideLayoutGuarantee: with the band drawn below a real tree, every
// row with ⚑, the cursor's and the one you are on that shows without
// the band shows with it, and the tree rows keep their order.
func TestSideLayoutGuarantee(t *testing.T) {
	rng := rand.New(rand.NewPCG(2, 353))
	for range 3000 {
		n := rng.IntN(40)
		var tree []treeRow
		for i := range n {
			tree = append(tree, treeRow{kind: treeThread, slug: "p", thread: fmt.Sprintf("t-%04d", i), pct: -1, hint: rng.IntN(5) == 0})
		}
		if n > 0 {
			tree[rng.IntN(n)].here = true
			if rng.IntN(2) == 0 {
				tree[rng.IntN(n)].cursor = true
			}
		}
		all := slices.Clone(tree)
		for j := range rng.IntN(6) {
			s := &sideSection{id: fmt.Sprintf("m%d.v", j), title: "S", folded: rng.IntN(3) == 0}
			for range rng.IntN(13) {
				s.lines = append(s.lines, "x")
			}
			all = append(all, treeRow{kind: treeSection, pct: -1, sec: s})
		}
		h, foot := rng.IntN(50), []int{0, 1, 4, 5}[rng.IntN(4)]
		w := sideDefault
		without, with := sideLayout(tree, w, h, foot), sideLayout(all, w, h, foot)
		on := func(l sideLay) []string {
			var out []string
			for _, s := range l.slots {
				if s.row >= 0 && l.rows[s.row].kind != treeSection {
					out = append(out, l.rows[s.row].key())
				}
			}
			return out
		}
		got := on(with)
		for _, s := range without.slots {
			r := without.rows[s.row]
			if (r.hint || r.cursor || r.here) && !slices.Contains(got, r.key()) {
				t.Fatalf("h=%d foot=%d: %s shows without sections and not with them (%v)", h, foot, r.key(), got)
			}
		}
		if len(with.slots) > max(h-foot, 0) && h >= foot+4 {
			t.Fatalf("h=%d foot=%d: %d rows laid out", h, foot, len(with.slots))
		}
	}
}

// TestOrderSections: the layout's sidebar.sections orders the band, the
// ids before "tree" above it; what it doesn't name goes below after what
// it names, in mod order; an id no mod has is skipped; without "tree"
// all go below.
func TestOrderSections(t *testing.T) {
	vs := []proto.ModView{{Mod: "ticker", View: "ticker"}, {Mod: "ci", View: "ci"}, {Mod: "age", View: "age"}, {Mod: "ci", View: "prs"}}
	ids := func(vs []proto.ModView) string {
		var out []string
		for _, v := range vs {
			out = append(out, v.Mod+"."+v.View)
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		order        []string
		above, below string
	}{
		{nil, "", "ticker.ticker ci.ci age.age ci.prs"},
		{[]string{"tree"}, "", "ticker.ticker ci.ci age.age ci.prs"},
		{[]string{"ticker.ticker", "tree"}, "ticker.ticker", "ci.ci age.age ci.prs"},
		{[]string{"ci.prs", "gone.x", "tree", "age.age"}, "ci.prs", "age.age ticker.ticker ci.ci"},
		{[]string{"age.age", "ci.ci"}, "", "age.age ci.ci ticker.ticker ci.prs"},
		{[]string{"tree", "ci.ci"}, "", "ci.ci ticker.ticker age.age ci.prs"},
	} {
		above, below := orderSections(vs, tc.order)
		if ids(above) != tc.above || ids(below) != tc.below {
			t.Errorf("%v: above %q below %q", tc.order, ids(above), ids(below))
		}
	}
}

// TestEffectiveLayoutSections: the layout mod's sidebar.sections and
// project_sections reach the console's layout, and bandIn's order.
func TestEffectiveLayoutSections(t *testing.T) {
	spec, err := mod.ParseLayout([]byte("[sidebar]\nsections = [\"ticker.ticker\", \"tree\"]\nproject_sections = [\"ci.prs\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	l := effectiveLayout(spec, DefaultLayout, "mine")
	if !slices.Equal(l.sideSections, []string{"ticker.ticker", "tree"}) || !slices.Equal(l.projSections, []string{"ci.prs"}) {
		t.Fatalf("%+v", l)
	}
	if l = effectiveLayout(mod.Layout{}, DefaultLayout, ""); l.sideSections != nil || l.projSections != nil {
		t.Fatalf("no layout: %+v", l)
	}
}
