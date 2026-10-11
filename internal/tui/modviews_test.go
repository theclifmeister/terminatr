package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/tasks"

	"github.com/theclifmeister/terminatr/internal/modview"
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

// fullDash is panelDash with a mod's full view too.
func fullDash(t *testing.T) (*fakeSource, *dash) {
	t.Helper()
	src, m := panelDash(t)
	timeline := &widget.Widget{Kind: widget.KindStack, Children: []widget.Widget{
		{Kind: widget.KindList, ID: "tl", Rows: []widget.Row{
			{ID: "e1", Spans: []widget.Span{{Text: "09:12 t-0004 started"}}},
			{ID: "e2", Spans: []widget.Span{{Text: "09:40 t-0004 reported"}}}}},
		{Kind: widget.KindKeys, ID: "k", Keys: []widget.Key{{Key: "o", Label: "older first"}}},
	}}
	views := append(panelViews(), proto.ModView{Mod: "panel", Name: "Panel", View: "timeline", Title: "Timeline", Slot: "full",
		Trees: []modview.Tree{{Tree: timeline}}})
	src.modViews = proto.ModViewsResult{Views: views}
	m.setModViews(modViewsMsg{res: src.modViews})
	return src, m
}

// TestModFullView: a full view opens from the ≡ menu, never by itself;
// it takes the dashboard's body with the mod's mark, keeps the header,
// the footer, the sidebar and NEEDS YOU's count, counts as shown, sends
// declared keys and clicks to the mod, and esc returns.
func TestModFullView(t *testing.T) {
	src, m := fullDash(t)
	if _, ok := m.top().(*fullView); ok {
		t.Fatal("a full view opened by itself")
	}
	if strings.Contains(screen(m), "09:12") {
		t.Fatal("the full view's tree shows on the dashboard")
	}
	i := slices.IndexFunc(m.dashItems(), func(it menuItem) bool { return it.label == "Timeline · mod: panel" })
	if i < 0 {
		t.Fatal("the ≡ menu lacks the full view")
	}
	run(m, m.dashItems()[i].run(m))
	if _, ok := m.top().(*fullView); !ok {
		t.Fatalf("the menu didn't open the full view: %T", m.top())
	}
	out := whole(m)
	for _, want := range []string{"tm dashboard · Timeline", "server ok", "NEEDS YOU 2", "mod: panel · Timeline", "09:12 t-0004 started", "older first", "esc back", "alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("full view lacks %q:\n%s", want, out)
		}
	}
	if s := m.shownViews(); !slices.ContainsFunc(s, func(v modview.Shown) bool { return v.View == "timeline" && v.Slot == "full" && v.Project == "alpha" }) {
		t.Errorf("shown %+v", s)
	}
	press(m, "down")
	run(m, press(m, "o"))
	if len(src.inputs) != 1 || src.inputs[0].View != "timeline" || src.inputs[0].Row != "e2" || src.inputs[0].Key != "o" {
		t.Fatalf("inputs %+v", src.inputs)
	}
	clickOn(t, m, "09:12 t-0004 started")
	if len(src.inputs) != 2 || src.inputs[1].Kind != "click" || src.inputs[1].Row != "e1" {
		t.Fatalf("a click sent %+v", src.inputs)
	}
	m.Update(keyMsg("esc"))
	if m.top() != nil {
		t.Fatalf("esc left %T open", m.top())
	}
	for _, s := range m.shownViews() {
		if s.View == "timeline" {
			t.Error("a closed full view counts as shown")
		}
	}
}

// TestModFullViewOpen: a mod's open request after a human input opens
// its full view, over the popup the input came from; esc returns to it.
func TestModFullViewOpen(t *testing.T) {
	src, m := fullDash(t)
	m.openTab("alpha", tabCount, 0)
	m.setModViews(modViewsMsg{res: proto.ModViewsResult{Views: src.modViews.Views, Open: []modview.Open{{Seq: 1, Mod: "panel", View: "timeline", Project: "alpha"}}}})
	if fv, ok := m.top().(*fullView); !ok || fv.project != "alpha" {
		t.Fatalf("not opened: %T", m.top())
	}
	m.Update(keyMsg("esc"))
	if m.projectPopupView() == nil || m.top() != m.projectPopupView() {
		t.Fatalf("esc didn't return to the popup: %T", m.top())
	}
}

// TestModFullViewFailed: a failed mod's full view says so, with no stale
// tree, and keeps NEEDS YOU.
func TestModFullViewFailed(t *testing.T) {
	src, m := fullDash(t)
	m.openFull(src.modViews.Views[2], "alpha")
	views := slices.Clone(src.modViews.Views)
	views[2].State, views[2].Err, views[2].Trees = proto.ModViewFailed, "crashed", nil
	m.setModViews(modViewsMsg{res: proto.ModViewsResult{Views: views}})
	out := whole(m)
	if !strings.Contains(out, "mod failed: crashed") || strings.Contains(out, "09:12") || !strings.Contains(out, "NEEDS YOU") {
		t.Errorf("failed full view:\n%s", out)
	}
}

// TestModTabBinding: a row of a mod's tab that binds task.delegate shows
// the core's hint; D there (once the row has been still for 300 ms)
// never reaches the mod: the core re-checks the live board and asks.
func TestModTabBinding(t *testing.T) {
	src, m := panelDash(t)
	src.board = &tasks.Board{Tasks: []*tasks.Task{{ID: 2, Title: "Write docs", Status: tasks.Ready}}}
	v := &src.modViews.Views[0]
	q := &v.Trees[0].Tree.Children[0]
	q.Rows[1].Actions = []widget.Action{{ID: "task.delegate", Target: map[string]string{"project": "alpha", "task": "T2"}}}
	m.setModViews(modViewsMsg{res: src.modViews})
	m.openTab("alpha", tabOverview, 0)
	press(m, "8")
	press(m, "down")
	if out := screen(m); !strings.Contains(out, "D delegate") {
		t.Fatalf("no core hint for the bound row:\n%s", out)
	}
	time.Sleep(modaction.Stable + 20*time.Millisecond)
	cmd := press(m, "D")
	if cmd == nil {
		t.Fatal("D on the bound row did nothing")
	}
	runAll(m, cmd)
	if len(src.inputs) != 0 {
		t.Fatalf("the press reached the mod: %+v", src.inputs)
	}
	cv, ok := m.top().(*confirmView)
	if !ok || cv.question != "Delegate T2 to the coordinator? It starts a thread for T2 Write docs." {
		t.Fatalf("D opened %T %+v", m.top(), m.top())
	}
}

// TestModTabHintClick: a click on the core's hint for a bound row runs
// the action, as its key does, and never reaches the mod.
func TestModTabHintClick(t *testing.T) {
	src, m := panelDash(t)
	src.board = &tasks.Board{Tasks: []*tasks.Task{{ID: 2, Title: "Write docs", Status: tasks.Ready}}}
	v := &src.modViews.Views[0]
	q := &v.Trees[0].Tree.Children[0]
	q.Rows[1].Actions = []widget.Action{{ID: "task.delegate", Target: map[string]string{"project": "alpha", "task": "T2"}}}
	m.setModViews(modViewsMsg{res: src.modViews})
	m.openTab("alpha", tabOverview, 0)
	press(m, "8")
	press(m, "down")
	time.Sleep(modaction.Stable + 20*time.Millisecond)
	m.render()
	x, y := at(t, m, "D delegate")
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	runAll(m, cmd)
	if len(src.inputs) != 0 {
		t.Fatalf("the click reached the mod: %+v", src.inputs)
	}
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("the hint click opened %T", m.top())
	}
}

// TestModFullViewBinding: a bound row of a full view shows the core's
// hint in the footer, and its key runs the core's action, never the mod.
func TestModFullViewBinding(t *testing.T) {
	src, m := fullDash(t)
	src.board = &tasks.Board{Tasks: []*tasks.Task{{ID: 2, Title: "Write docs", Status: tasks.Ready}}}
	v := &src.modViews.Views[2]
	tl := &v.Trees[0].Tree.Children[0]
	tl.Rows[1].Actions = []widget.Action{{ID: "task.delegate", Target: map[string]string{"project": "alpha", "task": "T2"}}}
	m.setModViews(modViewsMsg{res: src.modViews})
	m.openFull(*v, "alpha")
	press(m, "down")
	if out := whole(m); !strings.Contains(out, "D delegate") {
		t.Fatalf("no core hint for the bound row:\n%s", out)
	}
	time.Sleep(modaction.Stable + 20*time.Millisecond)
	runAll(m, press(m, "D"))
	if len(src.inputs) != 0 {
		t.Fatalf("the press reached the mod: %+v", src.inputs)
	}
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("D opened %T", m.top())
	}
}
