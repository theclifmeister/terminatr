package widget

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A curve draws a filled area with its total and half of it on the left,
// the labels under it, and rows along the same columns; each value takes
// the same columns in all of them.
func TestCurve(t *testing.T) {
	w := Widget{Kind: KindCurve, Values: []float64{1, 2, 4, 8}, Labels: []string{"5 Oct", "", "7 Oct", ""}, Under: []CurveRow{
		{Label: "done", Word: "per day", Values: []float64{1, 1, 2, 4}},
		{Label: "rel", Dots: []bool{true, false, true, false}},
	}}
	got := renderCurve(t, &w, 50)
	all := strings.Join(got, "\n")
	if len(got) != 9 {
		t.Fatalf("%d lines:\n%s", len(got), all)
	}
	if !strings.HasPrefix(got[0], "   8 ") || !strings.HasPrefix(got[3], "   4 ") {
		t.Errorf("the total and half of it label the top and the middle:\n%s", all)
	}
	if !strings.Contains(got[5], "████") {
		t.Errorf("the area is filled to the bottom:\n%s", all)
	}
	if !strings.HasPrefix(got[6], "     5 Oct") || !strings.Contains(got[6], "7 Oct") {
		t.Errorf("the axis:\n%s", all)
	}
	if !strings.HasPrefix(got[7], "done ") || !strings.HasSuffix(got[7], "per day") {
		t.Errorf("the done row:\n%s", all)
	}
	if !strings.HasPrefix(got[8], " rel • ") {
		t.Errorf("the dots row:\n%s", all)
	}
	for i, l := range got {
		if ansi.StringWidth(l) > 50 {
			t.Errorf("line %d passes the width: %q", i, l)
		}
	}
}

// With more values than columns, every value is still drawn: a column is the
// last total it stands for, the most of a row's values and a dot of any.
func TestCurveManyValues(t *testing.T) {
	var vals []float64
	var dots []bool
	for i := range 100 {
		vals = append(vals, float64(i+1))
		dots = append(dots, i == 3)
	}
	w := Widget{Kind: KindCurve, Values: vals, Under: []CurveRow{{Label: "rel", Dots: dots}}}
	got := renderCurve(t, &w, 30)
	all := strings.Join(got, "\n")
	if !strings.Contains(got[len(got)-1], "•") {
		t.Errorf("a dot in a column of several shows:\n%s", all)
	}
	if !strings.HasSuffix(got[0], "█") {
		t.Errorf("the newest value tops the last column:\n%s", all)
	}
	for i, l := range got {
		if ansi.StringWidth(l) > 30 {
			t.Errorf("line %d passes the width: %q", i, l)
		}
	}
}

// One value, as a project's first day has: drawn, at no silly width.
func TestCurveOneValue(t *testing.T) {
	w := Widget{Kind: KindCurve, Values: []float64{3}, Labels: []string{"6 Nov"}}
	got := renderCurve(t, &w, 80)
	all := strings.Join(got, "\n")
	if !strings.Contains(got[0], "████████") || strings.Contains(got[0], "█████████") {
		t.Errorf("one value:\n%s", all)
	}
	if !strings.Contains(got[len(got)-1], "6 Nov") {
		t.Errorf("its label:\n%s", all)
	}
}

func TestCurveChecked(t *testing.T) {
	bad := []Widget{
		{Kind: KindCurve, Values: []float64{1}, Under: []CurveRow{{Values: []float64{1, 2}}}},
		{Kind: KindCurve, Values: []float64{1}, Under: []CurveRow{{Dots: []bool{true, false}}}},
		{Kind: KindCurve, Values: []float64{1}, Under: make([]CurveRow, MaxUnder+1)},
	}
	for i := range bad {
		if err := bad[i].Check(); err == nil {
			t.Errorf("%d: passes", i)
		}
	}
}

func renderCurve(t *testing.T, w *Widget, width int) []string {
	t.Helper()
	if err := w.Check(); err != nil {
		t.Fatal(err)
	}
	v := NewView()
	v.Set(w)
	var out []string
	for _, s := range v.Render(width, 40, plainLook{}) {
		out = append(out, strings.TrimRight(ansi.Strip(s), " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}
