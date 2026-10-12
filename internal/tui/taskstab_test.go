package tui

import (
	tea "charm.land/bubbletea/v2"

	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// The built-in Tasks tab mod, as the console's tests see it: the tree
// examples/mods/tasks-tab publishes for the fake board, built here in
// Go (the mod's own fixtures test the mod; these test the console
// drawing and pressing it). A test turns it on with src.tasksTab and
// republishes it (tasksTabNow) as the mod does on tasks.changed.

// tasksTabView is the Tasks tab mod's view, a tree for each project.
func (f *fakeSource) tasksTabView() proto.ModView {
	v := proto.ModView{Mod: mod.BundledTasks, Name: "Tasks tab", View: mod.TasksView, Title: "tasks", Slot: mod.SlotProjectTab}
	for _, p := range f.data.Projects {
		b, _ := f.Board(p.Slug)
		// Opened to show a done task, the mod lists the done tasks.
		doneAll := f.tasksDoneAll
		for _, s := range f.shown {
			if s.Mod == v.Mod && s.Project == p.Slug && s.Task != "" {
				doneAll = doneAll || slices.ContainsFunc(b.Tasks, func(t *tasks.Task) bool { return t.Ref() == s.Task && t.Status == tasks.Done })
			}
		}
		v.Trees = append(v.Trees, modview.Tree{Project: p.Slug, Tree: tasksTabTree(p.Slug, b, p.Items, doneAll)})
	}
	return v
}

// tasksTabNow gives m the mod views now, the Tasks tab's drawn from the
// board and the asks as they are.
func tasksTabNow(m *dash, f *fakeSource) {
	m.setModViews(modViewsMsg{res: f.modViewsNow()})
}

// modViewsNow are the mod views ModViews answers: the test's own, and
// the Tasks tab's while it is on.
func (f *fakeSource) modViewsNow() proto.ModViewsResult {
	res := f.modViews
	if f.tasksTab {
		res.Views = append(slices.Clone(res.Views), f.tasksTabView())
	}
	return res
}

// tasksTabTree is the mod's tree for board b of project slug.
func tasksTabTree(slug string, b *tasks.Board, items []project.Item, doneAll bool) *widget.Widget {
	var rows []widget.Row
	var group tasks.Group
	done := 0
	for _, g := range tasks.Groups {
		var in []*tasks.Task
		for _, t := range b.Tasks {
			if tasks.GroupOf(t.Status) == g {
				in = append(in, t)
			}
		}
		if g == tasks.DoneG {
			done = len(in)
			if !doneAll {
				continue
			}
			slices.SortStableFunc(in, func(a, b *tasks.Task) int {
				return cmp.Or(-strings.Compare(a.Updated, b.Updated), b.ID-a.ID)
			})
		}
		for _, t := range in {
			if g != group {
				group = g
				title := strings.ToUpper(string(g))
				rows = append(rows, widget.Row{ID: "g:" + string(g), Head: true, Spans: []widget.Span{{Text: title, Role: "head"}}})
			}
			rows = append(rows, tasksTabRow(slug, t, project.TaskAsked(items, t.Ref()) != ""))
		}
	}
	if !doneAll && done > 0 {
		rows = append(rows, widget.Row{ID: "g:done", Head: true, Spans: []widget.Span{{Text: "DONE", Role: "head"}}})
	}
	children := []widget.Widget{{Kind: widget.KindTable, ID: "tasks", Empty: "no tasks", Columns: make([]widget.Column, 4), Rows: rows}}
	if !doneAll && done > 0 {
		children = append(children, widget.Widget{Kind: widget.KindText, Text: fmt.Sprintf("… %d done (b shows them)", done), Role: "faint"})
	}
	keys := widget.Widget{Kind: widget.KindKeys, ID: "keys",
		Actions: []widget.Action{{ID: "task.new", Target: map[string]string{"project": slug}}}}
	if doneAll || done > 0 {
		keys.Keys = []widget.Key{{Key: "b", Label: map[bool]string{true: "hide done", false: "done"}[doneAll]}}
	}
	return &widget.Widget{Kind: widget.KindStack, Children: append(children, keys)}
}

// tasksTabRow is task t's row, binding the actions its status allows.
func tasksTabRow(slug string, t *tasks.Task, asked bool) widget.Row {
	target := map[string]string{"project": slug, "task": t.Ref()}
	acts := []widget.Action{{ID: "task.show", Target: target}}
	ask := func(id string) {
		if !asked {
			acts = append(acts, widget.Action{ID: id, Target: target})
		}
	}
	switch t.Status {
	case tasks.Review:
		ask("task.accept")
		ask("task.send-back")
	case tasks.Done:
		ask("task.send-back")
	case tasks.Blocked:
		acts = append(acts, widget.Action{ID: "task.coordinator", Target: target})
		ask("task.delegate")
	case tasks.Open, tasks.Ready:
		ask("task.delegate")
	}
	icon, role := map[tasks.Status][2]string{tasks.Started: {"working", "good"}, tasks.Blocked: {"blocked", "bad"},
		tasks.Review: {"review", "warn"}, tasks.Done: {"done", "good"}, tasks.Open: {"idle", ""}, tasks.Ready: {"idle", ""}}[t.Status][0], ""
	role = map[tasks.Status]string{tasks.Started: "good", tasks.Blocked: "bad", tasks.Review: "warn", tasks.Done: "good"}[t.Status]
	var rest []widget.Span
	if asked {
		rest = append(rest, widget.Span{Text: "asked the coordinator", Role: "warn"})
	}
	var more []string
	if len(t.Steps) > 0 {
		more = append(more, fmt.Sprintf("%d/%d", t.StepsDone(), len(t.Steps)))
	}
	if t.Thread != "" {
		more = append(more, t.Thread)
	}
	if len(more) > 0 {
		rest = append(rest, widget.Span{Text: strings.Join(more, " "), Role: "faint"})
	}
	r := widget.Row{ID: t.Ref(), Actions: acts, Cells: [][]widget.Span{
		{{Text: t.Ref()}}, {{Text: t.Title}}, {{Icon: icon, Role: role}, {Text: " " + string(t.Status), Role: role}}, rest}}
	if t.Status != tasks.Done {
		for _, s := range t.Steps {
			icon, role := "todo.open", "faint"
			if s.Done {
				icon, role = "todo.done", "good"
			}
			r.More = append(r.More, []widget.Span{{Text: "      "}, {Icon: icon, Role: role}, {Text: " " + s.Text}})
		}
	}
	return r
}

// pollModViews polls the mod views as the console's tick does, saying
// which it shows.
func pollModViews(m *dash) {
	m.mv.busy = false // a poll the test dropped
	if cmd := m.pollModViews(); cmd != nil {
		m.Update(cmd())
	}
}

// tasksCursor is the row under the cursor of the Tasks tab of the open
// project popup.
func tasksCursor(m *dash, f *fakeSource) string {
	pv := m.projectPopupView()
	if pv == nil {
		return ""
	}
	if host := m.mv.host(f.tasksTabView(), pv.slug); host != nil {
		return host.Cursor("tasks")
	}
	return ""
}

// pressKey presses a key and runs what it starts, a bound action's
// check and run included.
func pressKey(m *dash, name string) { runAll(m, keyPress(m, name)) }

// feed runs cmd, each of a batch's, and gives m what it came to, as the
// program would (without following what that returns): a key that opens
// a popup loads its board and polls the mod views at once.
func feed(m *dash, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			feed(m, c)
		}
	default:
		m.Update(msg)
	}
}
