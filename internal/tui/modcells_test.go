package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// testCells: review-age's age column on thread rows and NEEDS YOU's
// tasks, pr-ci's dot on projects, and a status segment.
func testCells() *modCells {
	return newModCells(&proto.ModCells{
		Columns: []proto.ModColumn{
			{ID: "pr-ci.dot", Title: "dot", Width: 2, Where: []string{proto.SlotSidebarProject}},
			{ID: "review-age.age", Title: "age", Width: 4, Where: []string{proto.SlotSidebarThread, proto.SlotDashboardTask, proto.SlotDashboardThread}},
		},
		Cells: []proto.ModCell{
			{Mod: "review-age", Column: "age", Target: proto.CellTarget{Project: "demo", Task: "T3"}, Spans: []widget.Span{{Text: "3d", Role: "warn"}}},
			{Mod: "review-age", Column: "age", Target: proto.CellTarget{Project: "demo", Thread: "t-0002"}, Spans: []widget.Span{{Text: "12345678"}}},
			{Mod: "pr-ci", Column: "dot", Target: proto.CellTarget{Project: "demo"}, Spans: []widget.Span{{Text: "*", Role: "bad"}}},
		},
		Status: []proto.ModStatus{{Mod: "review-age", Name: "Review age", Spans: []widget.Span{{Text: "2 waiting", Role: "warn"}}}},
	}, []proto.SessionInfo{{ID: "s-9", Cells: []proto.ModCell{
		{Mod: "review-age", Column: "age", Target: proto.CellTarget{Session: "s-9"}, Spans: []widget.Span{{Text: "9h"}}},
	}}})
}

// TestModCellsSidebar: a mod's sidebar column shows without a layout
// naming it, before the state column; each row shows its own cell
// (a thread its own, else its task's, else its session's), cut to the
// column with a blank after it, and blank in the rows of slots the
// column doesn't name.
func TestModCellsSidebar(t *testing.T) {
	defer setIcons(IconsUnicode)
	setIcons(IconsUnicode)
	mc := testCells()
	order := mc.sideOrder(nil)
	if want := []string{"count", "ci", "pr-ci.dot", "review-age.age", "state", "remote", "paused", "inactive", "needs"}; !slices.Equal(order, want) {
		t.Fatalf("order %v", order)
	}
	if got := mc.sideOrder([]string{"review-age.age", "needs"}); got[0] != "review-age.age" || !slices.Contains(got, "pr-ci.dot") {
		t.Errorf("an order naming one %v", got)
	}
	ps := []ProjectData{{Slug: "demo", Threads: []ThreadRow{
		{Record: &thread.Record{ID: "t-0001", Task: "T3", State: thread.Running}},
		{Record: &thread.Record{ID: "t-0002", Task: "T4", State: thread.Running}},
		{Record: &thread.Record{ID: "t-0005", State: thread.Running}},
	}}}
	sessions := []proto.SessionInfo{{ID: "s-9", Role: proto.RoleThread, Project: "demo", Thread: "t-0005", State: "working"}}
	rows := buildTree(ps, sessions, treeIn{cells: mc})
	cell := func(r treeRow, col string) string {
		cols := r.columns()
		end := 60
		line := ansi.Strip(treeCells(r, end, false, false))
		for j := len(cols) - 1; j >= 0; j-- {
			end -= cols[j].w
			if cols[j].id == col {
				return ansi.Cut(line, end, end+cols[j].w)
			}
		}
		t.Fatalf("no column %s", col)
		return ""
	}
	want := map[string][2]string{ // row key: age, dot
		"p:demo":        {"    ", "* "},
		"t:demo:t-0001": {"3d  ", "  "}, // its task's
		"t:demo:t-0002": {"12 ", ""},    // cut, a blank after it
		"t:demo:t-0005": {"9h  ", "  "}, // its session's
		"c:demo":        {"    ", "  "},
	}
	for _, r := range rows {
		w, ok := want[r.key()]
		if !ok {
			continue
		}
		if got := cell(r, "review-age.age"); !strings.HasPrefix(got, w[0]) {
			t.Errorf("%s age %q, want %q", r.key(), got, w[0])
		}
		if got := cell(r, "pr-ci.dot"); w[1] != "" && got != w[1] {
			t.Errorf("%s dot %q, want %q", r.key(), got, w[1])
		}
		if got := ansi.StringWidth(treeCells(r, 60, false, false)); got != 60 {
			t.Errorf("%s: %d cells wide", r.key(), got)
		}
	}
	// The tooltip still finds the mod's column.
	for _, r := range rows {
		if r.key() == "t:demo:t-0001" {
			for _, c := range r.columns() {
				if c.id == "review-age.age" && gridTip(r, c) != "age (mod: review-age)" {
					t.Errorf("tip %q", gridTip(r, c))
				}
			}
		}
	}
}

// TestModCellsDashboard: NEEDS YOU's task rows and the project's thread
// rows carry the mods' dashboard columns after the state, lined up.
func TestModCellsDashboard(t *testing.T) {
	d := Data{Cells: testCells(), Projects: []ProjectData{{Slug: "demo",
		NeedsYou: []*tasks.Task{{ID: 3, Title: "Review me", Status: tasks.Review}},
		Threads:  []ThreadRow{{Record: &thread.Record{ID: "t-0002", Task: "T4", State: thread.Running}}},
	}}}
	var task, thr row
	for _, r := range buildRows(d, "demo") {
		switch {
		case r.task != nil:
			task = r
		case r.thread != nil:
			thr = r
		}
	}
	if len(task.mods) != 1 || len(thr.mods) != 1 {
		t.Fatalf("mods %+v %+v", task.mods, thr.mods)
	}
	tl, hl := task.text(100), thr.text(100)
	if !strings.Contains(tl, "3d  ") || !strings.Contains(hl, "123   t-0002") {
		t.Errorf("rows:\n%q\n%q", tl, hl)
	}
	if ansi.StringWidth(ansi.Strip(task.styled(100))) != 100 {
		t.Error("styled row not 100 wide")
	}
}

// TestModCellsStatusBar: the mods' segments show after the rest of the
// status bar when there is room, and give way first.
func TestModCellsStatusBar(t *testing.T) {
	info := proto.SessionInfo{ID: "s-1", Project: "demo", Role: proto.RoleCoordinator, State: "idle"}
	line, _ := statusBar(info, nil, "", false, 160, "", testCells())
	if !strings.Contains(ansi.Strip(line), "coordinator  2 waiting") {
		t.Errorf("wide: %q", ansi.Strip(line))
	}
	narrow, _ := statusBar(info, nil, "", false, 70, "", testCells())
	plain, _ := statusBar(info, nil, "", false, 70, "", nil)
	if narrow != plain {
		t.Errorf("narrow:\n%q\n%q", ansi.Strip(narrow), ansi.Strip(plain))
	}
}

// TestModToasts: a toast shows once in the message line, its mod's name
// in front; the first poll's toasts are old news, an error draws as one,
// and a server restarted (counting from 1 again) still gets through.
func TestModToasts(t *testing.T) {
	m := &dash{}
	m.showToasts([]proto.ModToast{{Seq: 4, Mod: "standup", Name: "Standup", Text: "old"}})
	if m.msg != "" {
		t.Fatalf("first poll showed %q", m.msg)
	}
	m.showToasts([]proto.ModToast{{Seq: 4, Text: "old"}, {Seq: 5, Mod: "standup", Name: "Standup", Text: "demo: 3 started", Level: "info"}})
	if m.msg != "Standup: demo: 3 started" || m.errMsg != "" {
		t.Fatalf("msg %q err %q", m.msg, m.errMsg)
	}
	m.msg = "something else"
	m.showToasts([]proto.ModToast{{Seq: 5, Text: "demo: 3 started"}})
	if m.msg != "something else" {
		t.Fatalf("shown twice: %q", m.msg)
	}
	m.showToasts([]proto.ModToast{{Seq: 1, Mod: "lab", Text: "broke", Level: "error"}})
	if m.msg != "lab: broke" || m.errMsg != m.msg {
		t.Fatalf("after a restart msg %q err %q", m.msg, m.errMsg)
	}
}
