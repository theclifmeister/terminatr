package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// plainLook draws roles as nothing and the cursor's row in reverse, so
// tests read the text and still see the cursor.
type plainLook struct{ ascii bool }

func (plainLook) Style(role, s string) string {
	if role == "sel" {
		return "\x1b[7m" + s + "\x1b[m"
	}
	return s
}

func (l plainLook) Icon(name string) string {
	if l.ascii {
		return map[string]string{"bar.on": "#", "bar.off": "-", "todo.done": "x", "todo.now": "~", "todo.open": "o"}[name]
	}
	return map[string]string{"bar.on": "▰", "bar.off": "▱", "todo.done": "✓", "todo.now": "◐", "todo.open": "○", "review": "◆"}[name]
}

func (l plainLook) ASCII() bool { return l.ascii }

func mustDecode(t *testing.T, js string) *Widget {
	t.Helper()
	w, err := Decode([]byte(js))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return w
}

// listOf is a list "l" of rows with these ids, each row's text its id.
func listOf(ids ...string) *Widget {
	w := &Widget{Kind: KindList, ID: "l"}
	for _, id := range ids {
		w.Rows = append(w.Rows, Row{ID: id, Spans: []Span{{Text: id}}})
	}
	return w
}

func TestCheckRefuses(t *testing.T) {
	for _, tc := range []struct{ js, want string }{
		{`{"kind":"canvas"}`, `kind "canvas"`},
		{`{"kind":"list","rows":[]}`, "needs an id"},
		{`{"kind":"stack","children":[{"kind":"list","id":"a"},{"kind":"table","id":"a","columns":[{"title":"x"}]}]}`, `"a" is used twice`},
		{`{"kind":"list","id":"a","rows":[{"id":"x"},{"id":"x"}]}`, `rows[1].id: "x" is used twice`},
		{`{"kind":"keys","keys":[{"key":"q","label":"quit"}]}`, `"q" is terminatr's`},
		{`{"kind":"keys","keys":[{"key":"/","label":"find"}]}`, `"/" is terminatr's`},
		{`{"kind":"keys","keys":[{"key":"ctrl+x","label":"x"}]}`, "is not a key"},
		{`{"kind":"keys","keys":[{"key":"o","label":"a"},{"key":"o","label":"b"}]}`, "declared twice"},
		{`{"kind":"list","id":"a","rows":[{"spans":[{"text":"x","role":"pink"}]}]}`, `role "pink"`},
		{`{"kind":"list","id":"a","rows":[{"spans":[{"icon":"rocket"}]}]}`, `"rocket" is not an icon`},
		{`{"kind":"list","id":"a","rows":[{"actions":[{"id":"Delete Everything"}]}]}`, "not an action id"},
		{`{"kind":"table","id":"a","columns":[]}`, "1 to 12 columns"},
		{`{"kind":"table","id":"a","columns":[{"title":"x"}],"rows":[{"cells":[[],[]]}]}`, "2 cells for 1 columns"},
		{`{"kind":"steps","steps":[{"text":"a","state":"maybe"}]}`, "not done, now or open"},
		{`{"kind":"bars","values":[1,-2]}`, "values[1]"},
		{`{"kind":"bars","values":[1,2],"labels":["a"]}`, "1 labels for 2 values"},
		{`{"kind":"progress","value":-1}`, "negative"},
		{`{"kind":"stack","children":[{"kind":"stack","children":[{"kind":"stack","children":[{"kind":"stack","children":[{"kind":"stack"}]}]}]}]}`, "nest at most"},
	} {
		_, err := Decode([]byte(tc.js))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.js, err, tc.want)
		}
	}
}

func TestCheckLimits(t *testing.T) {
	w := listOf()
	for i := range MaxRows + 1 {
		w.Rows = append(w.Rows, Row{ID: fmt.Sprint(i)})
	}
	if err := w.Check(); err == nil || !strings.Contains(err.Error(), "rows") {
		t.Errorf("too many rows: %v", err)
	}
	if _, err := Decode(make([]byte, MaxBytes+1)); err == nil {
		t.Error("an oversized tree decoded")
	}
	// A text widget holds a long file; any other string is a line.
	long := strings.Repeat("abcdefg\n", MaxText/4)
	if err := (&Widget{Kind: KindText, Text: long}).Check(); err != nil {
		t.Errorf("a %d-byte text: %v", len(long), err)
	}
	if err := (&Widget{Kind: KindText, Title: long}).Check(); err == nil {
		t.Errorf("a %d-byte title passed", len(long))
	}
	if err := (&Widget{Kind: KindText, Text: strings.Repeat("x", MaxBody+1)}).Check(); err == nil {
		t.Error("a text over MaxBody passed")
	}
	if err := (&Widget{Kind: KindText, Text: strings.Repeat("\n", MaxTextLine+1)}).Check(); err == nil {
		t.Error("a text over MaxTextLine passed")
	}
}

// TestCheckNarrows: a wide glyph (an emoji, a CJK character, an emoji
// sequence) becomes a ?, wherever it is, and the tree still draws.
func TestCheckNarrows(t *testing.T) {
	w := mustDecode(t, `{"kind":"stack","title":"ok 🚀","children":[
		{"kind":"text","text":"漢字\nthumbs 👍🏽 family 👨‍👩‍👧 sun ☀️ é → ✓"},
		{"kind":"list","id":"l","rows":[{"spans":[{"text":"🚀 go"}]}]}]}`)
	if w.Title != "ok ?" {
		t.Errorf("title %q", w.Title)
	}
	if got, want := w.Children[0].Text, "??\nthumbs ? family ??? sun ? é → ✓"; got != want {
		t.Errorf("text %q, want %q", got, want)
	}
	if got := w.Children[1].Rows[0].Spans[0].Text; got != "? go" {
		t.Errorf("span %q", got)
	}
	ss := []Span{{Text: "日本 x"}}
	if err := CheckSpans(ss); err != nil || ss[0].Text != "?? x" {
		t.Errorf("spans %q: %v", ss[0].Text, err)
	}
}

// TestCheckCleans: escape sequences, bidi overrides and zero-width
// characters never reach the terminal; a span's newline is a space; a
// row without an id gets its index.
func TestCheckCleans(t *testing.T) {
	w := mustDecode(t, `{"kind":"stack","title":"a\u001b]52;c;SGVsbG8=\u0007b","children":[
		{"kind":"list","id":"l","rows":[{"spans":[{"text":"x\u001b[31my\u202ez\u200bw\nv\tu"}]},{"id":"r"}]},
		{"kind":"text","text":"one\ntwo"}]}`)
	if w.Title != "a]52;c;SGVsbG8=b" {
		t.Errorf("title %q", w.Title)
	}
	l := w.Children[0]
	if got := l.Rows[0].Spans[0].Text; got != "x[31myzw v u" {
		t.Errorf("span %q", got)
	}
	if l.Rows[0].ID != "#0" || l.Rows[1].ID != "r" {
		t.Errorf("row ids %q %q", l.Rows[0].ID, l.Rows[1].ID)
	}
	if w.Children[1].Text != "one\ntwo" {
		t.Errorf("text lost its newline: %q", w.Children[1].Text)
	}
}

func TestCursorAndFilter(t *testing.T) {
	v := NewView()
	v.Set(listOf("alpha", "beta", "gamma", "delta"))
	if v.Focus() != "l" || v.Cursor("l") != "alpha" {
		t.Fatalf("focus %q cursor %q", v.Focus(), v.Cursor("l"))
	}
	v.Key("down")
	v.Key("down")
	if v.Cursor("l") != "gamma" {
		t.Fatalf("cursor %q after two downs", v.Cursor("l"))
	}
	// A republish that reorders keeps the cursor on its row.
	v.Set(listOf("gamma", "alpha", "beta"))
	if v.Cursor("l") != "gamma" {
		t.Errorf("cursor %q after a reorder", v.Cursor("l"))
	}
	// One that drops the row keeps the cursor at its place.
	v.Key("end")
	v.Set(listOf("gamma", "alpha"))
	if v.Cursor("l") != "alpha" {
		t.Errorf("cursor %q after its row went", v.Cursor("l"))
	}

	v.Set(listOf("alpha", "beta", "gamma", "delta"))
	if ok, _ := v.Key("/"); !ok || !v.Editing() {
		t.Fatal("/ didn't start the filter")
	}
	for _, k := range []string{"E", "l", "x", "backspace"} {
		v.Key(k)
	}
	if v.Filter("l") != "El" || v.Cursor("l") != "delta" {
		t.Errorf("filter %q cursor %q", v.Filter("l"), v.Cursor("l"))
	}
	// Keys typed into the filter are never the mod's or the frame's.
	if ok, ev := v.Key("q"); !ok || ev != nil {
		t.Errorf("q while filtering: %v %v", ok, ev)
	}
	v.Key("backspace")
	v.Key("enter")
	if v.Editing() || v.Filter("l") != "El" {
		t.Errorf("enter: editing %v filter %q", v.Editing(), v.Filter("l"))
	}
	// The filter survives a republish; esc drops it, a second esc is the
	// frame's.
	v.Set(listOf("alpha", "beta", "gamma", "delta", "eel"))
	if got := v.Render(20, 10, plainLook{}); len(got) != 3 || !strings.Contains(got[0], "El") {
		t.Errorf("filtered: %q", got)
	}
	if ok, _ := v.Key("esc"); !ok || v.Filter("l") != "" {
		t.Errorf("esc kept the filter %q", v.Filter("l"))
	}
	if ok, _ := v.Key("esc"); ok {
		t.Error("esc with no filter wasn't left to the frame")
	}
}

func TestKeysAndFocus(t *testing.T) {
	tree := mustDecode(t, `{"kind":"stack","children":[
		{"kind":"list","id":"files","rows":[{"id":"a.md","spans":[{"text":"a.md"}]},{"id":"b.md","spans":[{"text":"b.md"}]}]},
		{"kind":"table","id":"runs","columns":[{"title":"run"}],"rows":[{"id":"r1","cells":[[{"text":"one"}]]}]},
		{"kind":"keys","id":"k","keys":[{"key":"o","label":"open"},{"key":"enter","label":"show"}]}]}`)
	v := NewView()
	v.Set(tree)
	v.Key("down")
	ok, ev := v.Key("o")
	if !ok || ev == nil || *ev != (Event{Kind: EventKey, Widget: "files", Row: "b.md", Key: "o"}) {
		t.Errorf("o: %v %+v", ok, ev)
	}
	v.Key("tab")
	if v.Focus() != "runs" {
		t.Errorf("tab: focus %q", v.Focus())
	}
	if _, ev := v.Key("enter"); ev == nil || ev.Widget != "runs" || ev.Row != "r1" {
		t.Errorf("enter: %+v", ev)
	}
	v.Key("shift+tab")
	if v.Focus() != "files" || v.Cursor("files") != "b.md" {
		t.Errorf("shift+tab: focus %q cursor %q", v.Focus(), v.Cursor("files"))
	}
	for _, k := range []string{"x", "left", "right", "ctrl+x"} {
		if ok, ev := v.Key(k); ok || ev != nil {
			t.Errorf("%s was taken: %v %v", k, ok, ev)
		}
	}
}

// TestRenderFits: every line of every widget fits the width it is drawn
// in, down to one cell, in both icon sets.
func TestRenderFits(t *testing.T) {
	tree := mustDecode(t, sample)
	for _, ascii := range []bool{false, true} {
		for w := 1; w <= 100; w++ {
			v := NewView()
			v.Set(tree)
			for _, s := range v.Render(w, 1000, plainLook{ascii}) {
				if got := ansi.StringWidth(s); got > w {
					t.Fatalf("width %d (ascii %v): %q is %d wide", w, ascii, s, got)
				}
			}
		}
	}
}

const sample = `{"kind":"stack","children":[
	{"kind":"text","title":"About","text":"A long line of text that wraps at the width the frame gives it."},
	{"kind":"kv","pairs":[{"key":"Cost","value":[{"text":"$1.20","role":"warn"}]},{"key":"Threads","value":[{"text":"3"}]}]},
	{"kind":"progress","label":"Budget","value":42,"max":100},
	{"kind":"steps","steps":[{"text":"Plan","state":"done"},{"text":"Build","state":"now"},{"text":"Ship"}]},
	{"kind":"bars","title":"Week","values":[1,4,2,8,0,3,5],"labels":["mon","tue","wed","thu","fri","sat","sun"]},
	{"kind":"bars","values":[1,4,2,8,0,3,5],"spark":true},
	{"kind":"table","id":"t","title":"Threads","columns":[{"title":"thread"},{"title":"state"},{"title":"cost","right":true}],"rows":[
		{"id":"t-1","cells":[[{"text":"t-0001 Fix the sidebar"}],[{"icon":"review","role":"warn"},{"text":" review"}],[{"text":"0.40"}]]},
		{"id":"t-2","cells":[[{"text":"t-0002"}],[{"text":"running"}],[{"text":"12.00"}]]}]},
	{"kind":"keys","keys":[{"key":"o","label":"open"},{"key":"s","label":"sort"}]}]}`

func TestRenderSample(t *testing.T) {
	v := NewView()
	v.Set(mustDecode(t, sample))
	got := strings.Join(v.Render(40, 100, plainLook{}), "\n")
	want := `About
A long line of text that wraps at the
width the frame gives it.
Cost     $1.20
Threads  3
Budget  ▰▰▰▰▰▰▰▰▰▰▰▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱  42%
✓ Plan
◐ Build
○ Ship

Week
mon ████                               1
tue █████████████████                  4
wed █████████                          2
thu ██████████████████████████████████ 8
fri                                    0
sat █████████████                      3
sun █████████████████████              5
▂▅▃█▁▄▅

Threads
thread                  state      cost
` + "\x1b[7mt-0001 Fix the sidebar  ◆ review   0.40 \x1b[m" + `
t-0002                  running   12.00
o open · s sort`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestScrollFollowsCursor(t *testing.T) {
	ids := make([]string, 50)
	for i := range ids {
		ids[i] = fmt.Sprintf("r%02d", i)
	}
	tree := &Widget{Kind: KindStack, Children: []Widget{{Kind: KindText, Text: "head"}, *listOf(ids...)}}
	if err := tree.Check(); err != nil {
		t.Fatal(err)
	}
	v := NewView()
	v.Set(tree)
	if got := v.Render(10, 5, plainLook{}); got[0] != "head" {
		t.Errorf("top: %q", got)
	}
	v.Key("pgdown") // 4 rows down
	v.Key("pgdown")
	got := v.Render(10, 5, plainLook{})
	if v.Cursor("l") != "r08" || !strings.Contains(got[len(got)-1], "r08") {
		t.Errorf("cursor %q, lines %q", v.Cursor("l"), got)
	}
	v.Key("home")
	if got := v.Render(10, 5, plainLook{}); got[0] != "head" {
		t.Errorf("home didn't bring the head back: %q", got)
	}
	// The wheel scrolls without the cursor, and stops at the bottom.
	v.Wheel(1000)
	got = v.Render(10, 5, plainLook{})
	if v.Cursor("l") != "r00" || got[len(got)-1] != "r49" {
		t.Errorf("wheel: cursor %q, lines %q", v.Cursor("l"), got)
	}
}

func TestClick(t *testing.T) {
	tree := mustDecode(t, `{"kind":"stack","children":[
		{"kind":"list","id":"a","rows":[{"id":"x","spans":[{"text":"x"}]}]},
		{"kind":"list","id":"b","rows":[{"id":"y","spans":[{"text":"y"}]},{"id":"z","spans":[{"text":"z"}]}]},
		{"kind":"keys","keys":[{"key":"o","label":"open"},{"key":"s","label":"sort"}]}]}`)
	v := NewView()
	v.Set(tree)
	v.Render(30, 10, plainLook{})
	ev := v.Click(0, 2)
	if ev == nil || *ev != (Event{Kind: EventClick, Widget: "b", Row: "z"}) || v.Focus() != "b" {
		t.Errorf("row click: %+v, focus %q", ev, v.Focus())
	}
	// "o open · s sort": s is at cell 9.
	if ev := v.Click(9, 3); ev == nil || ev.Key != "s" || ev.Row != "z" {
		t.Errorf("hint click: %+v", ev)
	}
	if ev := v.Click(7, 3); ev != nil {
		t.Errorf("click between hints: %+v", ev)
	}
	if ev := v.Click(0, 9); ev != nil {
		t.Errorf("click below: %+v", ev)
	}
}

// TestMoved: the mod hears where the cursor is once, first when the
// tree shows, then each time a key, a click, the filter or a publish
// puts it on another row or list, and again after Forget.
func TestMoved(t *testing.T) {
	cur := func(w, r string) *Event { return &Event{Kind: EventCursor, Widget: w, Row: r} }
	same := func(got, want *Event) bool {
		return got == nil && want == nil || got != nil && want != nil && *got == *want
	}
	v := NewView()
	if ev := v.Moved(); ev != nil {
		t.Errorf("no tree: %+v", ev)
	}
	tree := mustDecode(t, `{"kind":"stack","children":[
		{"kind":"list","id":"a","rows":[{"id":"x","spans":[{"text":"x"}]},{"id":"y","spans":[{"text":"y"}]}]},
		{"kind":"list","id":"b","rows":[]}]}`)
	v.Set(tree)
	for i, step := range []struct {
		do   func()
		want *Event
	}{
		{func() {}, cur("a", "x")},
		{func() {}, nil},
		{func() { v.Key("up") }, nil},
		{func() { v.Key("down") }, cur("a", "y")},
		{func() { v.Key("/"); v.Key("x") }, cur("a", "x")},
		{func() { v.Key("esc"); v.Key("esc") }, nil},
		{func() { v.Set(listOf("y", "z")) }, cur("l", "y")},
		{func() { v.Set(listOf("y", "z")) }, nil},
		{func() { v.Set(listOf("z")) }, cur("l", "z")},
		{func() { v.Render(20, 5, plainLook{}); v.Click(0, 0) }, nil},
		{func() { v.Set(listOf()) }, cur("l", "")},
		{func() { v.Forget() }, cur("l", "")},
	} {
		step.do()
		if got := v.Moved(); !same(got, step.want) {
			t.Errorf("step %d: %+v, want %+v", i, got, step.want)
		}
	}
	v.Set(tree)
	v.Moved()
	if v.Key("tab"); !same(v.Moved(), cur("b", "")) {
		t.Error("tab to an empty list wasn't told")
	}
}

func TestEmptyAndNoMatch(t *testing.T) {
	v := NewView()
	v.Set(&Widget{Kind: KindList, ID: "l", Empty: "no memory files"})
	if got := v.Render(30, 5, plainLook{}); len(got) != 1 || got[0] != "no memory files" {
		t.Errorf("empty: %q", got)
	}
	v.Set(listOf("a"))
	v.Key("/")
	v.Key("z")
	if got := v.Render(30, 5, plainLook{}); len(got) != 2 || got[1] != "no match" || v.Cursor("l") != "" {
		t.Errorf("no match: %q cursor %q", got, v.Cursor("l"))
	}
	if ok, ev := v.Key("down"); !ok || ev != nil {
		t.Errorf("down on no rows: %v %v", ok, ev)
	}
}

// TestHeadsAndMore: a head row is a section rule the cursor skips and a
// filter hides; a row's more lines show under it only while the cursor
// is on it; a table without column titles draws no header; a head row
// can't bind or carry cells.
func TestHeadsAndMore(t *testing.T) {
	w := mustDecode(t, `{"kind":"table","id":"t","columns":[{"title":""},{"title":""}],"rows":[
		{"id":"h1","head":true,"spans":[{"text":"NEEDS YOU","role":"warn"}]},
		{"id":"T1","cells":[[{"text":"T1"}],[{"text":"fix"}]],"more":[[{"text":"  "},{"icon":"todo.open"},{"text":" step one"}]]},
		{"id":"h2","head":true,"spans":[{"text":"DONE"}]},
		{"id":"T2","cells":[[{"text":"T2"}],[{"text":"ship"}]]}]}`)
	v := NewView()
	v.Set(w)
	if v.Cursor("t") != "T1" {
		t.Fatalf("cursor on %q, not the first task", v.Cursor("t"))
	}
	got := v.Render(20, 10, plainLook{})
	want := []string{"NEEDS YOU ──────────", "\x1b[7mT1  fix             \x1b[m", "  ○ step one", "", "DONE ───────────────", "T2  ship"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("render:\n%q\nwant\n%q", got, want)
	}
	v.Key("down")
	if v.Cursor("t") != "T2" {
		t.Fatalf("down landed on %q", v.Cursor("t"))
	}
	if got := strings.Join(v.Render(20, 10, plainLook{}), "\n"); strings.Contains(got, "step one") {
		t.Errorf("more stays under a row the cursor left:\n%s", got)
	}
	v.Key("up")
	if v.Cursor("t") != "T1" {
		t.Fatalf("up landed on %q", v.Cursor("t"))
	}
	v.Key("/")
	v.Key("s")
	v.Key("h")
	if got := v.Render(20, 10, plainLook{}); len(got) != 2 || !strings.Contains(got[1], "T2  ship") {
		t.Errorf("filtered:\n%q", got)
	}
	for _, bad := range []string{
		`{"kind":"list","id":"l","rows":[{"head":true,"spans":[{"text":"x"}],"actions":[{"id":"task.show","target":{"project":"p","task":"T1"}}]}]}`,
		`{"kind":"table","id":"t","columns":[{"title":"a"}],"rows":[{"head":true,"cells":[[{"text":"x"}]]}]}`,
	} {
		if _, err := Decode([]byte(bad)); err == nil || !strings.Contains(err.Error(), "head row") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
