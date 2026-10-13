package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// bandClient is an attached console on beta with a full sidebar, the
// mods' views views and session.list naming secs as sidebar sections.
func bandClient(views []proto.ModView, secs []proto.ModSection) *client {
	d := testData()
	c := &client{cols: 140, rows: 30, sideW: sideDefault, dashboard: true, views: views, v: view.View{Current: "beta"},
		side: &sidebar{projects: d.Projects, sessions: d.Sessions}}
	c.bandShown(secs)
	return c
}

// bandTestSections are session.list's sections for bandTestViews.
func bandTestSections() []proto.ModSection {
	return []proto.ModSection{{Mod: "ticker", View: "ticker", Title: "Ticker", Slot: mod.SlotSidebarSection, Builtin: true},
		{Mod: "ci", View: "status", Title: "CI status", Slot: mod.SlotSidebarSection}}
}

// sectionRows are the band's rows in rows, by key.
func sectionRows(rows []treeRow) []string {
	var out []string
	for _, r := range rows {
		if r.kind == treeSection {
			s := r.key()
			if r.sec.waiting {
				s += " waiting"
			}
			out = append(out, s)
		}
	}
	return out
}

// TestAttachBand: the attached sidebar draws the band from mod.views'
// list as session.list names its sections: none without any, those it
// names alone (safe mode leaves a user mod's out), one mod.views hasn't
// listed yet waits; and it tells mod.views of them as the dashboard
// does, for the focused pane's project.
func TestAttachBand(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	c := bandClient(bandTestViews(), bandTestSections())
	if got := sectionRows(c.sideTree()); !slices.Equal(got, []string{"s:ticker.ticker", "s:ci.status"}) {
		t.Fatalf("band %v", got)
	}
	lines := strings.Join(sideLines(c.sideTree(), c.sideW, c.sideRows(), 0, nil, false), "\n")
	if !strings.Contains(lines, "TICKER") || !strings.Contains(lines, "#412 passes") {
		t.Fatalf("the sidebar:\n%s", lines)
	}
	var shown []string
	for _, s := range c.bandShown(bandTestSections()) {
		shown = append(shown, fmt.Sprintf("%s.%s %s %s", s.Mod, s.View, s.Slot, s.Project))
	}
	if want := []string{"ticker.ticker sidebar.section beta", "ci.status sidebar.section beta"}; !slices.Equal(shown, want) {
		t.Fatalf("shown %v", shown)
	}

	if c := bandClient(bandTestViews(), nil); len(sectionRows(c.sideTree())) != 0 || len(c.bandShown(nil)) != 0 {
		t.Fatal("no sections named, yet a band")
	}
	safe := bandTestSections()[:1] // the server leaves the user mod's out
	if got := sectionRows(bandClient(bandTestViews(), safe).sideTree()); !slices.Equal(got, []string{"s:ticker.ticker"}) {
		t.Fatalf("safe mode %v", got)
	}
	if got := sectionRows(bandClient(nil, bandTestSections()).sideTree()); !slices.Equal(got, []string{"s:ticker.ticker waiting", "s:ci.status waiting"}) {
		t.Fatalf("before mod.views %v", got)
	}

	short := bandClient(bandTestViews(), bandTestSections())
	short.rows = 15
	short.bandShown(bandTestSections())
	if len(sectionRows(short.sideTree())) != 0 || len(short.bandShown(bandTestSections())) != 0 {
		t.Fatal("a short sidebar has sections")
	}
	hidden := bandClient(bandTestViews(), bandTestSections())
	hidden.sideW = 0
	if len(hidden.bandShown(bandTestSections())) != 0 {
		t.Fatal("a hidden sidebar shows sections")
	}

	// The cursor walks onto the headers after the tree.
	c.kb, c.v.SideSel = areaSide, "s:ci.status"
	rows := c.sideTree()
	if i := slices.IndexFunc(rows, func(r treeRow) bool { return r.cursor }); i < 0 || rows[i].key() != "s:ci.status" {
		t.Fatal("no cursor on the CI header")
	}
}

// TestAttachBandAbove: under a layout with sections = ["ticker.ticker",
// "tree"] the attached sidebar draws the ticker above PROJECTS, which
// then says ⚑ N, and CI status below the tree; the ticker's header
// comes before the first project in the cursor's path.
func TestAttachBandAbove(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	c := bandClient(bandTestViews(), bandTestSections())
	c.side.order = []string{"ticker.ticker", "tree"}
	rows := c.sideTree()
	if rows[0].key() != "s:ticker.ticker" || !rows[0].sec.above || rows[len(rows)-1].key() != "s:ci.status" || rows[len(rows)-1].sec.above {
		t.Fatalf("rows %v", sectionRows(rows))
	}
	needs := needsYouCount(c.side.projects, c.side.sessions)
	lines := sideLines(rows, c.sideW, c.sideRows(), needs, nil, false)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	head := slices.IndexFunc(plain, func(l string) bool { return strings.Contains(l, "PROJECTS") })
	ci := slices.IndexFunc(plain, func(l string) bool { return strings.Contains(l, "CI STATUS") })
	if !strings.Contains(plain[0], "TICKER") || head < 1 || !strings.Contains(plain[head+1], "alpha") || ci < head ||
		needs == 0 || !strings.Contains(plain[head], fmt.Sprintf("%s %d", ic().hint, needs)) {
		t.Fatalf("the sidebar:\n%s", strings.Join(plain, "\n"))
	}

	// ↑ from the first project goes to the ticker's header.
	c.kb, c.v.SideSel = areaSide, "p:alpha"
	in := sideKeysIn{tree: c.sideTree, full: c.sideFullTree, w: c.sideW, h: c.sideRows(), sel: c.v.SideSel}
	if st, _ := sideKeyFor(in, "up"); st.sel != "s:ticker.ticker" {
		t.Fatalf("↑ from alpha: %q", st.sel)
	}
}

// TestPollViewsNow: the attached console asks mod.views when what it
// shows changed, about once a second while sections show, every tick
// while it waits for a view to open, and not at all with none shown.
func TestPollViewsNow(t *testing.T) {
	for _, tc := range []struct {
		key, sent    string
		odd, waiting bool
		want         bool
	}{
		{"", "", false, false, false},
		{"", "", true, false, false},
		{"", "\x00", false, false, true}, // the first tick says it
		{"", "a", true, false, true},     // none shown any more
		{"a", "", true, false, true},
		{"a", "a", false, false, true},
		{"a", "a", true, false, false},
		{"", "", true, true, true},
	} {
		if got := pollViewsNow(tc.key, tc.sent, tc.odd, tc.waiting); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

// TestHandViews: the dashboard ending for a session keeps the sidebar's
// sections shown and closes the rest; the next console starts from the
// list the last one had.
func TestHandViews(t *testing.T) {
	shown := []modview.Shown{{Mod: "ci", View: "prs", Slot: mod.SlotDashboardCard}, {Mod: "ticker", View: "ticker", Slot: mod.SlotSidebarSection},
		{Mod: "ci", View: "per", Slot: mod.SlotProjectSection, Project: "beta"}}
	if got := sidebarShown(shown); len(got) != 2 || got[0].View != "ticker" || got[1].View != "per" {
		t.Fatalf("kept %+v", got)
	}
	defer handed.Store(nil)
	handViews(bandTestViews())
	if got := handedViews(); len(got) != 3 || got[0].Mod != "ticker" {
		t.Fatalf("handed %+v", got)
	}
}

// TestAttachProj: the attached sidebar draws the per-project sections
// session.list names under each project, as the dashboard does, and
// tells mod.views of one per project on screen, next to the band's.
func TestAttachProj(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	secs := append(bandTestSections(), proto.ModSection{Mod: "prci", View: "ci", Title: "ci status", Slot: mod.SlotProjectSection})
	c := bandClient(projTestViews(), secs)
	if got := sectionRows(c.sideTree()); !slices.Equal(got, []string{"s:prci.ci@alpha", "s:prci.ci@beta", "s:ticker.ticker", "s:ci.status"}) {
		t.Fatalf("sections %v", got)
	}
	var shown []string
	for _, s := range c.bandShown(secs) {
		shown = append(shown, fmt.Sprintf("%s.%s %s", s.Mod, s.View, s.Project))
	}
	if want := []string{"ticker.ticker beta", "ci.status beta", "prci.ci alpha", "prci.ci beta"}; !slices.Equal(shown, want) {
		t.Fatalf("shown %v, want %v", shown, want)
	}
	lines := ansi.Strip(strings.Join(sideLines(c.sideTree(), c.sideW, c.sideRows(), 0, nil, false), "\n"))
	if !strings.Contains(lines, "    ▾ ci status") || !strings.Contains(lines, "      ☑ #412 beta docs") {
		t.Fatalf("the sidebar:\n%s", lines)
	}
}
