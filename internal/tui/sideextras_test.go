package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// sideDash is a dashboard on testData with the sidebar holding the
// keyboard, the filter and the last place forgotten.
func sideDash(t *testing.T) (*dash, *fakeSource) {
	t.Helper()
	resetConsole()
	t.Cleanup(resetConsole)
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120, Height: 30, State: DashState{Current: "beta"}})
	m.layout.Details = false
	m.setData(src.data)
	keyPress(m, "tab")
	return m, src
}

// sideCursorKey is the key of the sidebar's keyboard row, "" for none.
func sideCursorKey(m *dash) string {
	for _, r := range m.tree() {
		if r.cursor {
			return r.key()
		}
	}
	return ""
}

func treeKeys(m *dash) []string {
	var out []string
	for _, r := range m.tree() {
		out = append(out, r.key())
	}
	return out
}

// pressCode sends a key without text: home, end, pgup, backspace.
func pressCode(m *dash, code rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code})
	return cmd
}

// TestSidebarFilter: / starts the filter, whose line shows under the
// header; letters type into it (j and q too), narrowing the tree to what
// matches and the projects over it; the cursor falls back on the first
// row shown; enter keeps the filter, the keys move again, esc clears it
// and a second esc leaves the sidebar.
func TestSidebarFilter(t *testing.T) {
	m, _ := sideDash(t)
	keyPress(m, "/")
	if l := strings.Split(whole(m), "\n")[1]; !strings.Contains(l, " /▏") {
		t.Fatalf("the filter's line: %q", l)
	}
	for _, k := range []string{"d", "o", "c", "x"} {
		keyPress(m, k)
	}
	if got := treeKeys(m); len(got) != 0 || !strings.Contains(whole(m), "no match") {
		t.Fatalf("docx: %v\n%s", got, whole(m))
	}
	pressCode(m, tea.KeyBackspace)
	if got, want := treeKeys(m), []string{"p:beta", "t:beta/t-0005"}; !slices.Equal(got, want) {
		t.Fatalf("doc: %v, want %v", got, want)
	}
	if c := sideCursorKey(m); c != "p:beta" {
		t.Fatalf("cursor %q", c)
	}
	keyPress(m, "enter")
	if f := sideFilterNow(); f.typing || f.text != "doc" || !strings.Contains(whole(m), " /doc ") {
		t.Fatalf("enter keeps the filter: %+v\n%s", f, whole(m))
	}
	keyPress(m, "j")
	if c := sideCursorKey(m); c != "t:beta/t-0005" {
		t.Fatalf("j in the kept filter: %q", c)
	}
	keyPress(m, "esc")
	if m.focus != areaSide || sideFilterNow().on() || len(treeKeys(m)) != 6 {
		t.Fatalf("esc clears the filter: focus %d %+v %v", m.focus, sideFilterNow(), treeKeys(m))
	}
	if c := sideCursorKey(m); c != "t:beta/t-0005" {
		t.Fatalf("the cursor stays on its row: %q", c)
	}
	keyPress(m, "esc")
	if m.focus != areaMain {
		t.Fatal("a second esc keeps the sidebar")
	}

	// Letters that are keys elsewhere type, and match a thread's id.
	keyPress(m, "tab")
	keyPress(m, "/")
	for _, k := range []string{"q", "j"} {
		keyPress(m, k)
	}
	if m.quitting || sideFilterNow().text != "qj" {
		t.Fatalf("q j while typing: %+v", sideFilterNow())
	}
	keyPress(m, "esc")
	keyPress(m, "/")
	for _, k := range strings.Split("0006", "") {
		keyPress(m, k)
	}
	if got, want := treeKeys(m), []string{"p:beta", "t:beta/t-0006"}; !slices.Equal(got, want) {
		t.Fatalf("0006: %v, want %v", got, want)
	}
}

// TestSidebarFilterClick: while the filter's line shows, a click hits
// the row drawn where it lands.
func TestSidebarFilterClick(t *testing.T) {
	m, _ := sideDash(t)
	keyPress(m, "/")
	keyPress(m, "6")
	keyPress(m, "enter")
	lines := strings.Split(whole(m), "\n")
	for y, l := range lines {
		if strings.Contains(l, "t-0006") {
			r, ok, _ := sideHitAt(m.tree(), m.sideW(), m.h, 5, y)
			if !ok || r.key() != "t:beta/t-0006" {
				t.Fatalf("a click on line %d hits %q", y, r.key())
			}
			return
		}
	}
	t.Fatalf("no t-0006 row:\n%s", whole(m))
}

// TestSidebarJumps: home end pgup pgdown go to the ends and a page on;
// 1-9 to the nth project; w W to the next and previous row that needs
// you, round the ends.
func TestSidebarJumps(t *testing.T) {
	m, _ := sideDash(t)
	steps := []struct {
		key  any
		want string
	}{
		{tea.KeyEnd, "t:beta/t-0006"}, {tea.KeyHome, "p:alpha"}, {tea.KeyPgDown, "t:beta/t-0006"}, {tea.KeyPgUp, "p:alpha"},
		{"2", "p:beta"}, {"1", "p:alpha"}, {"9", "p:alpha"},
		{"w", "t:beta/t-0005"}, {"w", "t:beta/t-0005"}, {"W", "t:beta/t-0005"},
	}
	for i, s := range steps {
		switch k := s.key.(type) {
		case rune:
			pressCode(m, k)
		case string:
			keyPress(m, k)
		}
		if got := sideCursorKey(m); got != s.want {
			t.Fatalf("step %d (%v): cursor %q, want %q", i, s.key, got, s.want)
		}
	}
	keyPress(m, "1")
	keyPress(m, "9")
	if m.msg != "no project 9" {
		t.Fatalf("9: %q", m.msg)
	}
	keyPress(m, "w")
	if m.msg != "next row that needs you" {
		t.Fatalf("w: %q", m.msg)
	}
	keyPress(m, "W")
	if m.msg != "only this row needs you" {
		t.Fatalf("W on the only one: %q", m.msg)
	}

	// w clears a filter that hides the row.
	keyPress(m, "1")
	keyPress(m, "/")
	keyPress(m, "a")
	keyPress(m, "l")
	keyPress(m, "enter")
	keyPress(m, "w")
	if sideFilterNow().on() || sideCursorKey(m) != "t:beta/t-0005" {
		t.Fatalf("w past the filter: %+v %q", sideFilterNow(), sideCursorKey(m))
	}
}

// TestNeedsFirstOrder: the projects that need you move to the top of
// their group with their rows; the inactive ones stay below the active.
func TestNeedsFirstOrder(t *testing.T) {
	p := func(slug string, hint, inactive bool) treeRow {
		return treeRow{kind: treeProject, slug: slug, hint: hint, inactive: inactive}
	}
	rows := []treeRow{
		p("a", false, false), {kind: treeCoordinator, slug: "a"},
		p("b", true, false), {kind: treeCoordinator, slug: "b"}, {kind: treeThread, slug: "b", thread: "t-1"},
		p("c", false, true), p("d", true, true),
	}
	var got []string
	for _, r := range needsFirstOrder(rows) {
		got = append(got, r.key())
	}
	want := []string{"p:b", "c:b", "t:b/t-1", "p:a", "c:a", "p:d", "p:c"}
	if !slices.Equal(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
}

// TestNeedsFirstSetting: the setting reorders the dashboard's sidebar.
func TestNeedsFirstSetting(t *testing.T) {
	m, src := sideDash(t)
	src.data.Projects[0].Counts = map[string]int{} // alpha needs nothing now
	m.setData(src.data)
	if got := treeKeys(m)[0]; got != "p:alpha" {
		t.Fatalf("off: first %q", got)
	}
	src.data.NeedsFirst = true
	m.setData(src.data)
	if got := treeKeys(m)[0]; got != "p:beta" {
		t.Fatalf("on: first %q", got)
	}
}
