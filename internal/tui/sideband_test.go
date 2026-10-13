package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// bandViews are a built-in ticker band section for beta and a user
// mod's CI-status section with a badge that opens its card.
func bandTestViews() []proto.ModView {
	ticker := &widget.Widget{Kind: widget.KindKV, Pairs: []widget.Pair{
		{Key: "PRs", Value: []widget.Span{{Text: "40s ago · next 1m20s", Role: "faint"}}},
		{Key: "synced", Value: []widget.Span{{Text: "1m ago", Role: "faint"}}},
		{Key: "PR host", Value: []widget.Span{{Text: "ok", Role: "faint"}}},
	}}
	ci := &widget.Widget{Kind: widget.KindText, Text: "#412 passes"}
	return []proto.ModView{
		{Mod: "ticker", Name: "Ticker", View: "ticker", Title: "Ticker", Slot: mod.SlotSidebarSection,
			Description: "this project's PR polls and sync",
			Trees:       []modview.Tree{{Project: "beta", Tree: ticker, Height: 3}}},
		{Mod: "ci", Name: "CI", View: "status", Title: "CI status", Slot: mod.SlotSidebarSection, Opens: "prs",
			Trees: []modview.Tree{{Tree: ci, Height: 1, Badge: []widget.Span{{Text: "1 ☒", Role: "bad"}}}}},
		{Mod: "ci", Name: "CI", View: "prs", Title: "PRs", Slot: mod.SlotDashboardCard},
	}
}

func bandDash(t *testing.T, w, h int, views []proto.ModView) (*fakeSource, *dash) {
	t.Helper()
	src := &fakeSource{data: testData(), modViews: proto.ModViewsResult{Views: views}}
	src.data.Mods = []mod.Mod{{ID: "ticker", Builtin: true}, {ID: "ci"}}
	m := newDash(DashOptions{Source: src, Width: w, Height: h, State: DashState{Current: "beta"}})
	m.layout.Details = false
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	return src, m
}

// sidebarOf is the dashboard's sidebar as text, its trailing blanks cut.
func sidebarOf(m *dash) []string {
	var out []string
	for _, l := range strings.Split(whole(m), "\n") {
		r := []rune(l)
		out = append(out, strings.TrimRight(string(r[:min(m.sideW(), len(r))]), " "))
	}
	return out
}

// TestSideBandGolden: the band below the tree in each state, as
// testdata/sidebar-band.golden has it (go test -run TestSideBandGolden
// -update rewrites it).
func TestSideBandGolden(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	var b strings.Builder
	show := func(name string, m *dash) {
		fmt.Fprintf(&b, "%s (%d×%d, sidebar %d)\n", name, m.winW, m.h, m.sideW())
		for _, l := range sidebarOf(m) {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	_, m := bandDash(t, 120+sideDefault, 30, bandTestViews())
	show("full", m)

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	m.layout.Sidebar.Width = 20
	show("narrow", m)

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	m.layout.Sidebar.Slim = true
	show("slim", m)
	if s := m.shownViews(); slices.ContainsFunc(s, func(s modview.Shown) bool { return s.Slot == mod.SlotSidebarSection }) {
		t.Errorf("the slim strip shows sections: %+v", s)
	}

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	m.layout.Sidebar.Hidden = true
	if m.sideW() != 0 || slices.ContainsFunc(m.tree(), func(r treeRow) bool { return r.kind == treeSection }) {
		t.Errorf("a hidden sidebar has sections")
	}

	_, m = bandDash(t, 120+sideDefault, 17, bandTestViews())
	show("short window", m)

	_, m = bandDash(t, 120+sideDefault, 15, bandTestViews())
	show("too short for sections", m)

	failed := bandTestViews()
	failed = append([]proto.ModView{{Mod: "standup", Name: "Standup", View: "standup", Title: "Standup", Slot: mod.SlotSidebarSection,
		State: proto.ModViewFailed, Err: "exit 1", Log: "/x/standup.log"}}, failed...)
	_, m = bandDash(t, 120+sideDefault, 30, failed)
	show("failed mod", m)

	src, m := bandDash(t, 120+sideDefault, 30, bandTestViews())
	src.data.NoMods = true
	m.setData(src.data)
	show("safe mode", m)

	_, m = bandDash(t, 120+sideDefault, 30, bandTestViews())
	keyPress(m, "tab")
	m.sideSel = "s:ticker.ticker"
	show("cursor on a header", m)

	waiting := bandTestViews()
	waiting[0].Trees = nil
	_, m = bandDash(t, 120+sideDefault, 30, waiting)
	show("waiting", m)

	got := b.String()
	path := filepath.Join("testdata", "sidebar-band.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestSideBandGolden -update writes it)", err)
	}
	if got != string(want) {
		t.Errorf("the band differs from %s:\n%s", path, got)
	}
}

var _ = tea.KeyPressMsg{}
var _ = ansi.Strip
var _ view.Tree
