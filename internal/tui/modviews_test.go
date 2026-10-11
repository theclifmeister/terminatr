package tui

import (
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

func (f *fakeSource) ModViews(shown []modview.Shown) (proto.ModViewsResult, error) {
	f.shown = shown
	return f.modViews, nil
}

func (f *fakeSource) ModViewInput(p proto.ModViewInputParams) error {
	f.inputs = append(f.inputs, p)
	return nil
}

// panelViews are a mod's tab (for alpha, with a count) and card.
func panelViews() []proto.ModView {
	two := 2
	queue := &widget.Widget{Kind: widget.KindStack, Children: []widget.Widget{
		{Kind: widget.KindList, ID: "q", Rows: []widget.Row{
			{ID: "T1", Spans: []widget.Span{{Text: "T1 fix the login"}}},
			{ID: "T2", Spans: []widget.Span{{Text: "T2 write docs"}}}}},
		{Kind: widget.KindKeys, ID: "k", Keys: []widget.Key{{Key: "o", Label: "older first"}}},
	}}
	cost := &widget.Widget{Kind: widget.KindKV, Pairs: []widget.Pair{{Key: "today", Value: []widget.Span{{Text: "$4.20"}}}}}
	return []proto.ModView{
		{Mod: "panel", Name: "Panel", View: "queue", Title: "Review queue", Slot: "project.tab", Projects: []string{"alpha"},
			Trees: []modview.Tree{{Project: "alpha", Count: &two, Tree: queue}}},
		{Mod: "panel", Name: "Panel", View: "cost", Title: "Cost", Slot: "dashboard.card",
			Trees: []modview.Tree{{Tree: cost, Height: 3}}},
	}
}

func panelDash(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	src := &fakeSource{data: testData(), modViews: proto.ModViewsResult{Views: panelViews()}}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40, State: DashState{Current: "alpha"}})
	m.layout.Details = false
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	return src, m
}

// TestModTab: a mod's project tab comes after the core tabs with its
// title and count, is marked as the mod's, moves its cursor in the core
// and sends a declared key to the mod with the row under the cursor.
func TestModTab(t *testing.T) {
	src, m := panelDash(t)
	m.openTab("alpha", tabOverview, 0)
	out := screen(m)
	if !strings.Contains(out, "8 Review queue 2") {
		t.Fatalf("no mod tab in the tab bar:\n%s", out)
	}
	press(m, "8")
	out = screen(m)
	for _, want := range []string{"mod: panel · Review queue", "T1 fix the login", "T2 write docs", "older first", "/ filter"} {
		if !strings.Contains(out, want) {
			t.Errorf("mod tab lacks %q:\n%s", want, out)
		}
	}
	if s := m.shownViews(); len(s) != 2 || s[0].View != "queue" || s[0].Project != "alpha" || s[0].Slot != "project.tab" {
		t.Errorf("shown %+v", s)
	}
	press(m, "down")
	cmd := press(m, "o")
	if cmd == nil {
		t.Fatal("a declared key sent nothing")
	}
	run(m, cmd)
	if len(src.inputs) != 1 || src.inputs[0].Row != "T2" || src.inputs[0].Key != "o" || src.inputs[0].Project != "alpha" || src.inputs[0].Widget != "q" {
		t.Fatalf("inputs %+v", src.inputs)
	}
	// An undeclared key reaches no mod; esc closes the popup.
	press(m, "x")
	if len(src.inputs) != 1 {
		t.Fatalf("an undeclared key was sent: %+v", src.inputs)
	}
	// The mod's tab isn't beta's.
	m.pop()
	m.openTab("beta", tabOverview, 0)
	if strings.Contains(screen(m), "Review queue") {
		t.Error("beta has alpha's mod tab")
	}
}

// TestModViewFailed: a failed mod's tab and card say so, with its log,
// and show no stale tree.
func TestModViewFailed(t *testing.T) {
	_, m := panelDash(t)
	views := panelViews()
	for i := range views {
		views[i].State, views[i].Err, views[i].Log = proto.ModViewFailed, "crashed 3 times", "/h/logs/mods/panel.log"
		views[i].Trees = nil
	}
	m.setModViews(modViewsMsg{res: proto.ModViewsResult{Views: views}})
	out := screen(m)
	if !strings.Contains(out, "mod failed: crashed 3 times") || strings.Contains(out, "$4.20") {
		t.Errorf("card:\n%s", out)
	}
	m.openTab("alpha", tabCount, 0)
	out = screen(m)
	for _, want := range []string{"mod failed: crashed 3 times", "panel.log", "mod: panel"} {
		if !strings.Contains(out, want) {
			t.Errorf("failed tab lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "T1 fix") || strings.Contains(out, "Review queue 2") {
		t.Errorf("stale tree or count:\n%s", out)
	}
}

// TestModCard: a card below the sections, headed by its title, the
// mod's name and mark; z folds it, enter opens it as a popup.
func TestModCard(t *testing.T) {
	_, m := panelDash(t)
	out := screen(m)
	sess, card := strings.Index(out, "SESSIONS"), strings.Index(out, "Cost · Panel")
	if card < 0 || card < sess || !strings.Contains(out, "mod: panel") || !strings.Contains(out, "$4.20") {
		t.Fatalf("no card below the sections:\n%s", out)
	}
	m.sel = "mc:panel/cost"
	press(m, "z")
	if out := screen(m); strings.Contains(out, "$4.20") || !strings.Contains(out, "▸ Cost") {
		t.Errorf("z didn't fold the card:\n%s", out)
	}
	for _, s := range m.shownViews() {
		if s.View == "cost" {
			t.Error("a folded card counts as shown")
		}
	}
	press(m, "z", "enter")
	if _, ok := m.top().(*cardView); !ok {
		t.Fatalf("enter didn't open the card: %T", m.top())
	}
	if out := screen(m); !strings.Contains(out, "mod: panel · Cost") || !strings.Contains(out, "$4.20") {
		t.Errorf("card popup:\n%s", out)
	}
}

// TestModViewOpen: a mod's open request after a human input opens its
// tab in the project's popup, once.
func TestModViewOpen(t *testing.T) {
	_, m := panelDash(t)
	open := modViewsMsg{res: proto.ModViewsResult{Views: panelViews(), Open: []modview.Open{{Seq: 1, Mod: "panel", View: "queue", Project: "alpha"}}}}
	m.setModViews(open)
	pv := m.projectPopupView()
	if pv == nil || pv.slug != "alpha" || pv.tab != tabCount {
		t.Fatalf("not opened on the mod tab: %+v", pv)
	}
	m.pop()
	m.setModViews(open)
	if m.projectPopupView() != nil {
		t.Error("the same request opened twice")
	}
}

// TestMemoryTabMod: the built-in Memory tab mod's tab is drawn in the
// core Memory tab's place (6 memory), marked as the mod's, not again
// after the core tabs; enter on a file reaches the mod. With the mod off
// the core Memory tab draws; a failed mod shows that, not the core tab.
func TestMemoryTabMod(t *testing.T) {
	files := &widget.Widget{Kind: widget.KindStack, Children: []widget.Widget{
		{Kind: widget.KindList, ID: "files", Rows: []widget.Row{
			{ID: "CONTEXT.md", Spans: []widget.Span{{Text: "Context"}, {Text: "  CONTEXT.md · 1 KB", Role: "faint"}}},
			{ID: "memory/decisions.md", Spans: []widget.Span{{Text: "Decisions"}}}}},
		{Kind: widget.KindKeys, Keys: []widget.Key{{Key: "enter", Label: "read"}}},
	}}
	memView := proto.ModView{Mod: mod.BundledMemory, Name: "Memory tab", View: mod.MemoryView, Title: "memory", Slot: mod.SlotProjectTab,
		Trees: []modview.Tree{{Project: "alpha", Tree: files}}}
	src := &fakeSource{data: testData(), modViews: proto.ModViewsResult{Views: append(panelViews(), memView)}}
	src.memory = project.Memory{Context: "# Context\n\nThe core tab's text.\n"}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40, State: DashState{Current: "alpha"}})
	m.layout.Details = false
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	m.openTab("alpha", tabOverview, 0)
	press(m, "6")
	out := screen(m)
	for _, want := range []string{"6 memory", "8 Review queue", "mod: memory-tab · memory", "Context", "CONTEXT.md · 1 KB", "Decisions", "read"} {
		if !strings.Contains(out, want) {
			t.Errorf("memory tab lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"9 memory", "The core tab's text."} {
		if strings.Contains(out, not) {
			t.Errorf("memory tab shows %q:\n%s", not, out)
		}
	}
	if s := m.shownViews(); len(s) == 0 || s[0].Mod != mod.BundledMemory || s[0].Project != "alpha" {
		t.Errorf("shown %+v", s)
	}
	press(m, "down")
	run(m, press(m, "enter"))
	if len(src.inputs) != 1 || src.inputs[0].Mod != mod.BundledMemory || src.inputs[0].Row != "memory/decisions.md" || src.inputs[0].Key != "enter" {
		t.Fatalf("inputs %+v", src.inputs)
	}
	if pv := m.projectPopupView(); pv == nil || pv.tab != tabMemory {
		t.Fatal("enter left the Memory tab")
	}

	// Failed: the core's failure state, never the core tab's text.
	src.modViews.Views[2].State = proto.ModViewFailed
	src.modViews.Views[2].Err = "host exited"
	m.setModViews(modViewsMsg{res: src.modViews})
	if out := screen(m); !strings.Contains(out, "mod failed: host exited") || strings.Contains(out, "The core tab's text.") {
		t.Errorf("failed memory mod:\n%s", out)
	}

	// Off: the core Memory tab.
	src.modViews.Views = panelViews()
	m.setModViews(modViewsMsg{res: src.modViews})
	m.projectPopupView().memory = &src.memory
	if out := screen(m); !strings.Contains(out, "The core tab's text.") || strings.Contains(out, "mod: memory-tab") {
		t.Errorf("memory mod off:\n%s", out)
	}
}
