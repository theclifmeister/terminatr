package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/tasks"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

func (f *fakeSource) ModViews(shown []modview.Shown) (proto.ModViewsResult, error) {
	f.shown = shown
	return f.modViewsNow(), nil
}

func (f *fakeSource) ModViewInput(p proto.ModViewInputParams) error {
	f.inputs = append(f.inputs, p)
	return nil
}

func (f *fakeSource) ModCommand(p proto.ModCommandParams) error {
	f.commands = append(f.commands, p)
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
	for _, want := range []string{"T1 fix the login", "T2 write docs", "older first", "/ filter"} {
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

// drainCursors runs cmd and the batch it may be, feeding ui.cursor sends
// back as they return.
func drainCursors(m *dash, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drainCursors(m, c)
		}
	case modCursorMsg:
		_, next := m.Update(msg)
		drainCursors(m, next)
	}
}

// TestModViewCursor: a mod hears where its view's cursor is, as
// ui.cursor: when the view shows, then on each move, one send at a time
// with the moves made meanwhile down to the latest, never twice the
// same, and again once the view was hidden and shows again.
func TestModViewCursor(t *testing.T) {
	src, m := panelDash(t)
	m.openTab("alpha", tabCount, 0)
	cursors := func() (out []string) {
		for _, p := range src.inputs {
			if p.Kind != widget.EventCursor || p.Mod != "panel" || p.View != "queue" || p.Project != "alpha" {
				t.Fatalf("input %+v", p)
			}
			out = append(out, p.Widget+"/"+p.Row)
		}
		return out
	}
	drainCursors(m, m.setModViews(modViewsMsg{res: src.modViews}))
	if got := cursors(); !slices.Equal(got, []string{"q/T1"}) {
		t.Fatalf("shown: %q", got)
	}
	drainCursors(m, m.setModViews(modViewsMsg{res: src.modViews}))
	sent := press(m, "down")
	if press(m, "up", "down", "up") != nil {
		t.Error("a second send while one is on its way")
	}
	drainCursors(m, sent)
	if got := cursors(); !slices.Equal(got, []string{"q/T1", "q/T2", "q/T1"}) {
		t.Fatalf("moves: %q", got)
	}
	m.pop()
	drainCursors(m, m.setModViews(modViewsMsg{res: src.modViews}))
	m.openTab("alpha", tabCount, 0)
	drainCursors(m, m.setModViews(modViewsMsg{res: src.modViews}))
	if got := cursors(); len(got) != 4 || got[3] != "q/T1" {
		t.Fatalf("shown again: %q", got)
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
	for _, want := range []string{"mod failed: crashed 3 times", "panel.log"} {
		if !strings.Contains(out, want) {
			t.Errorf("failed tab lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "T1 fix") || strings.Contains(out, "Review queue 2") {
		t.Errorf("stale tree or count:\n%s", out)
	}
}

// TestModCard: a card below the sections, headed by its title, the
// mod's name; z folds it, enter opens it as a popup.
func TestModCard(t *testing.T) {
	_, m := panelDash(t)
	out := screen(m)
	sess, card := strings.Index(out, "SESSIONS"), strings.Index(out, "Cost · Panel")
	if card < 0 || card < sess || !strings.Contains(out, "Cost") || !strings.Contains(out, "$4.20") {
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
	if out := screen(m); strings.Contains(out, "mod: ") || !strings.Contains(out, "$4.20") {
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

// TestModCardOpenProject: a request to open a card for another project
// than the dashboard lists shows that project's dashboard, with the card
// unfolded and selected.
func TestModCardOpenProject(t *testing.T) {
	_, m := panelDash(t)
	m.mv.folded = map[string]bool{"mc:panel/cost": true}
	m.setModViews(modViewsMsg{res: proto.ModViewsResult{Views: panelViews(),
		Open: []modview.Open{{Seq: 1, Mod: "panel", View: "cost", Project: "beta"}}}})
	if got := listProject(m.data, m.current); got != "beta" {
		t.Errorf("dashboard lists %q, want beta", got)
	}
	if m.sel != "mc:panel/cost" || m.mv.folded["mc:panel/cost"] {
		t.Errorf("card not selected and unfolded: sel %q folded %v", m.sel, m.mv.folded)
	}
}

// TestMemoryTabMod: the built-in Memory tab mod draws the Memory tab (6
// memory), unmarked, and not again after the core tabs; enter on a file
// reaches the mod. A failed mod shows that; with the mod off the tab
// says so, and how to turn it on.
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
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40, State: DashState{Current: "alpha"}})
	m.layout.Details = false
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})
	m.openTab("alpha", tabOverview, 0)
	press(m, "6")
	out := screen(m)
	for _, want := range []string{"6 memory", "8 Review queue", "Context", "CONTEXT.md · 1 KB", "Decisions", "read"} {
		if !strings.Contains(out, want) {
			t.Errorf("memory tab lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"9 memory", "mod: memory-tab"} {
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

	// Failed: the core's failure state.
	src.modViews.Views[2].State = proto.ModViewFailed
	src.modViews.Views[2].Err = "host exited"
	m.setModViews(modViewsMsg{res: src.modViews})
	if out := screen(m); !strings.Contains(out, "mod failed: host exited") {
		t.Errorf("failed memory mod:\n%s", out)
	}

	// Off: the tab says so, and how to turn it on.
	src.modViews.Views = panelViews()
	m.setModViews(modViewsMsg{res: src.modViews})
	checkModOff(t, m, mod.BundledMemory)
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
	i := slices.IndexFunc(m.dashItems(), func(it menuItem) bool { return it.label == "Timeline · panel" })
	if i < 0 {
		t.Fatal("the ≡ menu lacks the full view")
	}
	run(m, m.dashItems()[i].run(m))
	if _, ok := m.top().(*fullView); !ok {
		t.Fatalf("the menu didn't open the full view: %T", m.top())
	}
	out := whole(m)
	for _, want := range []string{"tm dashboard · Timeline", "server ok", "NEEDS YOU 2", "09:12 t-0004 started", "older first", "esc back", "alpha"} {
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

// TestTasksTabMod: the built-in Tasks tab mod's tab is drawn as the
// Tasks tab (3 tasks), with its section heads and the selected task's
// steps; enter on a row opens the task over the popup through the bound
// task.show, at once (the view's first tree needs no wait); + runs the
// task.new the tab binds in the whole view; keys a row doesn't bind run
// nothing. With the mod off the tab says so.
func TestTasksTabMod(t *testing.T) {
	src, m := panelDash(t)
	src.board = &tasks.Board{Tasks: []*tasks.Task{
		{ID: 1, Title: "Write the README", Status: tasks.Started, Steps: []tasks.Step{{N: 1, Text: "Draft", Done: true}, {N: 2, Text: "Proofread"}}},
		{ID: 2, Title: "Ship it", Status: tasks.Ready},
	}}
	task := func(id string) map[string]string { return map[string]string{"project": "alpha", "task": id} }
	board := &widget.Widget{Kind: widget.KindStack, Children: []widget.Widget{
		{Kind: widget.KindTable, ID: "tasks", Columns: []widget.Column{{}, {}, {}}, Rows: []widget.Row{
			{ID: "g:in-motion", Head: true, Spans: []widget.Span{{Text: "IN MOTION", Role: "head"}}},
			{ID: "T1", Cells: [][]widget.Span{{{Text: "T1"}}, {{Text: "Write the README"}}, {{Text: "started"}}},
				Actions: []widget.Action{{ID: "task.show", Target: task("T1")}},
				More:    [][]widget.Span{{{Text: "   "}, {Icon: "todo.open"}, {Text: " Proofread"}}}},
			{ID: "g:on-deck", Head: true, Spans: []widget.Span{{Text: "ON DECK", Role: "head"}}},
			{ID: "T2", Cells: [][]widget.Span{{{Text: "T2"}}, {{Text: "Ship it"}}, {{Text: "ready"}}},
				Actions: []widget.Action{{ID: "task.show", Target: task("T2")}, {ID: "task.delegate", Target: task("T2")}}},
		}},
		{Kind: widget.KindKeys, ID: "keys", Actions: []widget.Action{{ID: "task.new", Target: map[string]string{"project": "alpha"}}}},
	}}
	tasksView := proto.ModView{Mod: mod.BundledTasks, Name: "Tasks tab", View: mod.TasksView, Title: "tasks", Slot: mod.SlotProjectTab,
		Trees: []modview.Tree{{Project: "alpha", Tree: board}}}
	src.modViews.Views = append(src.modViews.Views, tasksView)
	m.setModViews(modViewsMsg{res: src.modViews})
	m.Update(m.openTab("alpha", tabOverview, 0)())
	press(m, "3")
	out := screen(m)
	for _, want := range []string{"3 tasks", "8 Review queue", "IN MOTION ─", "ON DECK ─", "Write the README", "Proofread", "enter show"} {
		if !strings.Contains(out, want) {
			t.Errorf("tasks tab lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "9 tasks") {
		t.Errorf("the mod's tab is listed again after the core tabs:\n%s", out)
	}
	// j, as down, skips the head: T2 is under the cursor, T1's steps fold.
	press(m, "j")
	out = screen(m)
	if !strings.Contains(out, "D delegate") || strings.Contains(out, "Proofread") {
		t.Errorf("down didn't land on T2:\n%s", out)
	}
	// The core tab's c, A and x do nothing here: T2 binds none of them.
	for _, k := range []string{"A", "x", "c"} {
		runAll(m, press(m, k))
		if _, ok := m.top().(*projectView); !ok {
			t.Fatalf("%s opened %T", k, m.top())
		}
	}
	runAll(m, press(m, "enter"))
	if tv, ok := m.top().(*taskView); !ok || tv.id != 2 {
		t.Fatalf("enter opened %T %+v", m.top(), m.top())
	}
	if len(src.inputs) != 0 {
		t.Errorf("the press reached the mod: %+v", src.inputs)
	}
	// The mod hears action.done with the press token its check gave.
	if want := "tasks-tab task.show alpha/T2 true"; len(src.done) != 1 || src.done[0] != want {
		t.Errorf("done %q, want %q", src.done, want)
	}
	m.pop()
	runAll(m, press(m, "+"))
	if _, ok := m.top().(*projectView); ok {
		t.Error("+ asked for no new task")
	}
	if want := "tasks-tab task.new alpha/ true"; len(src.done) != 2 || src.done[1] != want {
		t.Errorf("done %q, want %q", src.done, want)
	}
	for len(m.stack) > 0 {
		if _, ok := m.top().(*projectView); ok {
			break
		}
		m.pop()
	}

	// Off: the tab says so, and how to turn it on.
	src.modViews.Views = panelViews()
	m.setModViews(modViewsMsg{res: src.modViews})
	checkModOff(t, m, mod.BundledTasks)
}

// checkModOff checks the open core tab that built-in mod id draws, with
// no view of the mod's: off, it says so and names the Mods tab's real
// keys; off for the project, how to turn it on for it; on, that it
// waits for the mod.
func checkModOff(t *testing.T, m *dash, id string) {
	t.Helper()
	slug := m.projectPopupView().slug
	saved := m.data.Mods
	defer func() { m.data.Mods = saved }()
	for _, c := range []struct {
		md   mod.Mod
		want []string
	}{
		{mod.Mod{ID: id, State: mod.Disabled}, []string{"The " + id + " mod draws this tab, and it is off.", "Mods tab (, then 3)", "tm mod enable " + id + "."}},
		{mod.Mod{ID: id, State: mod.Enabled, Projects: []string{"zeta"}}, []string{"it is off for " + slug, "Mods tab (, then 3)", "tm mod enable " + id + " --project " + slug}},
		{mod.Mod{ID: id, State: mod.Enabled}, []string{"Waiting for the " + id + " mod to draw this tab"}},
	} {
		m.data.Mods = []mod.Mod{c.md}
		out := strings.Join(strings.Fields(screen(m)), " ")
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s %s: lacks %q:\n%s", id, c.md.State, want, screen(m))
			}
		}
		if strings.Contains(out, "≡") || strings.Contains(out, "mod: ") {
			t.Errorf("%s %s: names ≡ or marks the mod:\n%s", id, c.md.State, screen(m))
		}
	}
}
