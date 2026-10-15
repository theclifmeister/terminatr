package tui

import (
	"io"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
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
	m.notePlace() // as the first frame does
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
			r, ok, _ := sideHitAt(m.tree(), m.sideW(), m.h, m.sideFoot(), 5, y)
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

// TestDashLastPlace: ; (as prefix+;) goes back to where the keyboard
// was before, and again back to where it came from; to a session, it
// attaches it.
func TestDashLastPlace(t *testing.T) {
	m, _ := sideDash(t)
	keyPress(m, ";")
	if m.msg != "back to the last place" || m.focus != areaMain {
		t.Fatalf("; after tab: %q, focus %d", m.msg, m.focus)
	}
	keyPress(m, ";")
	if m.focus != areaSide || sideCursorKey(m) != "p:beta" {
		t.Fatalf("; again: focus %d cursor %q", m.focus, sideCursorKey(m))
	}
	keyPress(m, "1")
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	keyPress(m, ";")
	if m.focus != areaSide || sideCursorKey(m) != "p:beta" {
		t.Fatalf("prefix+; after 1: focus %d cursor %q", m.focus, sideCursorKey(m))
	}

	// The last place a session: ; attaches it, the sidebar keeping the
	// keyboard.
	notePlace(place{session: "s-5", project: "beta", area: areaSide, sel: "t:beta/t-0005"})
	m.notePlace()
	drain(t, m, keyPress(m, ";"))
	if m.result.Attach != "s-5" || m.focus != areaSide {
		t.Fatalf("; to s-5: attach %q focus %d", m.result.Attach, m.focus)
	}
}

// TestSessionLastPlace: in a session, prefix+; takes the keyboard back
// to the sidebar row it was on, or to the pane; the filter types there
// as on the dashboard.
func TestSessionLastPlace(t *testing.T) {
	resetConsole()
	t.Cleanup(resetConsole)
	c, err := newClient(tmclient.Paths{}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.enc.Close()
	d := testData()
	c.prefix, c.statusBar = chord{'\\'}, true
	c.setWindow(160, 40)
	p := &pane{info: proto.SessionInfo{ID: "s-5", Role: proto.RoleThread, Project: "beta", Thread: "t-0005"}}
	c.v = view.View{Mode: view.ModeLayout, Focus: "s-5"}
	c.panes["s-5"], c.focus = p, p
	c.side, c.sideW = &sidebar{projects: d.Projects, sessions: d.Sessions}, sideDefault
	c.vc = &ViewConn{} // down: the cursor moves here alone
	pk := uv.Key{Code: '\\', Mod: uv.ModCtrl}
	last := func() {
		c.key(pk)
		c.key(uv.Key{Code: ';', Text: ";"})
	}
	c.key(pk)
	c.key(uv.Key{Code: uv.KeyTab})
	c.key(uv.Key{Code: 'j', Text: "j"})
	if c.kb != areaSide || c.v.SideSel != "t:beta/t-0006" {
		t.Fatalf("prefix+tab, j: focus %d sel %q", c.kb, c.v.SideSel)
	}
	last()
	if c.kb != areaSide || c.v.SideSel != "t:beta/t-0005" {
		t.Fatalf("prefix+;: focus %d sel %q", c.kb, c.v.SideSel)
	}
	c.key(uv.Key{Code: uv.KeyEscape})
	last()
	if c.kb != areaSide || c.v.SideSel != "t:beta/t-0005" {
		t.Fatalf("esc, prefix+;: focus %d sel %q", c.kb, c.v.SideSel)
	}
	last()
	if c.kb != areaMain {
		t.Fatalf("prefix+; again: focus %d", c.kb)
	}

	// The filter in a session.
	c.key(pk)
	c.key(uv.Key{Code: uv.KeyTab})
	for _, k := range "/old" {
		c.key(uv.Key{Code: k, Text: string(k)})
	}
	c.key(uv.Key{Code: uv.KeyEnter})
	var keys []string
	for _, r := range c.sideTree() {
		keys = append(keys, r.key())
	}
	if want := []string{"p:beta", "t:beta/t-0006"}; !slices.Equal(keys, want) || c.v.SideSel != "p:beta" {
		t.Fatalf("/old: %v sel %q", keys, c.v.SideSel)
	}
}

// TestSidebarTooltip: the mouse over a row's glyph says what it means,
// in the sidebar's foot (the details line's hook, sideHoverTip); off
// the glyphs, and off the sidebar, it goes. The dashboard then asks for
// every motion.
func TestSidebarTooltip(t *testing.T) {
	m, src := sideDash(t)
	src.data.Server = ServerInfo{Version: "1.2.3"}
	m.setData(src.data)
	if m.View().MouseMode != tea.MouseModeAllMotion {
		t.Fatal("no motion for the tooltip")
	}
	end := m.sideW() - 1 - sideGap // the grid's right edge
	for _, s := range []struct {
		x, y int
		want string
	}{
		{end - 2, 5, "needs you: it waits on you"}, // t-0005
		{end - 10, 5, "working"},
		{end - 15, 5, "progress"},
		{end - 6, 5, ""}, // paused: blank on a thread
		{3, 5, ""},       // the label
		{end - 10, 2, "coordinator: blocked: waits on a permission prompt, a question or a task"},
		{end - 2, 1, "needs you: something here waits on you"},
		{m.sideW() + 3, 5, ""},
	} {
		m.Update(tea.MouseMotionMsg{X: s.x, Y: s.y})
		if got := sideHoverTip(); got != s.want {
			t.Errorf("at %d,%d: %q, want %q", s.x, s.y, got, s.want)
		}
		if s.want != "" && !strings.Contains(whole(m), s.want[:min(len(s.want), 20)]) {
			t.Errorf("at %d,%d: the tip doesn't show:\n%s", s.x, s.y, whole(m))
		}
	}
	m.layout.Sidebar.Slim = true
	m.setWidth(m.winW)
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("the slim strip has no tooltip")
	}
}

// TestSidebarTooltipIconSets: the tooltip finds its glyph in every icon
// set.
func TestSidebarTooltipIconSets(t *testing.T) {
	defer setIcons(IconsUnicode)
	for _, set := range []string{IconsNerd, IconsUnicode, IconsASCII} {
		setIcons(set)
		m, _ := sideDash(t)
		end := m.sideW() - 1 - sideGap
		if got := sideTip(m.tree(), m.sideW(), m.h, m.sideFoot(), end-10, 5); got != "working" {
			t.Errorf("%s: %q", set, got)
		}
		if got := sideTip(m.tree(), m.sideW(), m.h, m.sideFoot(), end-1, 5); got != "needs you: it waits on you" {
			t.Errorf("%s, the glyph's second cell: %q", set, got)
		}
	}
}

// TestAttachFoldSections: the attach screen folds sections here at once,
// in the view and in ui.json; on a server of protocol 14 it keeps them as
// its own and says nothing.
func TestAttachFoldSections(t *testing.T) {
	resetConsole()
	t.Cleanup(resetConsole)
	c, err := newClient(tmclient.Paths{}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.enc.Close()
	ui := filepath.Join(t.TempDir(), "ui.json")
	c.side = &sidebar{uiFile: ui}
	c.v = view.View{Name: "v", Seq: 1}
	c.vc = &ViewConn{v: c.v, up: true, watch: map[chan struct{}]bool{},
		serve: func(method string, _ proto.ViewParams) (view.View, error) {
			return view.View{}, proto.Errorf(proto.ErrUnknownMethod, "unknown method %q", method)
		}}
	c.foldSections([]string{"ticker.ticker"}, true)
	if v := c.vc.View(); !c.v.IsSectionFolded("ticker.ticker") || !v.IsSectionFolded("ticker.ticker") || c.flash != "" {
		t.Fatalf("folds %+v, view %+v, flash %q", c.v.Tree, v.Tree, c.flash)
	}
	if l := LoadLayout(ui).Tree; !l.IsSectionFolded("ticker.ticker") {
		t.Fatalf("ui.json: %+v", l)
	}
	c.foldSections([]string{"ticker.ticker"}, false)
	if v := c.vc.View(); c.v.IsSectionFolded("ticker.ticker") || v.IsSectionFolded("ticker.ticker") || LoadLayout(ui).Tree.SectionsFolded != nil {
		t.Fatalf("unfold: folds %+v, view %+v", c.v.Tree, v.Tree)
	}
}
