package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// goldenWatch is the coordinator's project of the T348 design's
// mockups: a question waits, two threads, a task on deck.
func goldenWatch() *proto.ProjectWatch {
	return &proto.ProjectWatch{
		Project: "terminatr",
		NeedsYou: []proto.WatchNeed{
			{Why: proto.WhyQuestion, Task: "T351", Title: "Check the release notes", Status: "started", Thread: "t-0004", Question: "Which tag do we ship?"},
		},
		Threads: []proto.WatchThread{
			{ID: "t-0323", Title: "Right-pane sections", Session: "s-5", State: "working",
				Task: &proto.WatchTask{ID: "T348", Title: "Right-pane sections", StepsDone: 3, StepsTotal: 5, Current: "Right-pane section design"}},
			{ID: "t-0004", Title: "Check the release notes", Session: "s-6", State: "blocked", Reason: "question",
				Task: &proto.WatchTask{ID: "T351", Title: "Check the release notes", StepsDone: 1, StepsTotal: 2}},
		},
		Ready: []proto.WatchTodo{{Task: "T349", Title: "Layout v2 docs", Status: "ready"}},
	}
}

// tickerView is a coordinator section like the ticker mod's: three
// rows, built in.
func tickerView() proto.ModView {
	rows := &widget.Widget{Kind: widget.KindText, Text: "PRs      40s ago · next 1m20s\nsynced   1m ago\nPR host  ok"}
	return proto.ModView{Mod: "ticker", Name: "Ticker", View: "ticker", Title: "ticker", Slot: mod.SlotCoordinatorSection,
		Trees: []modview.Tree{{Tree: rows}}}
}

// digestView is a user mod's coordinator section with a badge.
func digestView() proto.ModView {
	return proto.ModView{Mod: "digest", Name: "Digest", View: "today", Title: "digest", Slot: mod.SlotCoordinatorSection,
		Trees: []modview.Tree{{Tree: noteTree("3 merged today"), Badge: []widget.Span{{Text: "3 ☑", Role: "good"}}}}}
}

// TestPanelSectionsGolden: the right pane's mod sections on both
// screens, as testdata/panel-sections.golden has them (go test -run
// TestPanelSectionsGolden -update rewrites it): the coordinator's info
// panel with sections below core's blocks, above them, between them,
// folded, failed, selected, in a short panel and in safe mode; a
// thread's info panel with a details section; the dashboard's details
// panel on a thread's row.
func TestPanelSectionsGolden(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	var b strings.Builder
	write := func(name string, lines []string, rows int) {
		fmt.Fprintf(&b, "%s (%d rows)\n", name, rows)
		for i, l := range lines {
			if i == rows {
				b.WriteString("  ┄ scrolls ┄\n")
			}
			b.WriteString("│" + strings.TrimRight(ansi.Strip(l), " ") + "\n")
		}
		b.WriteString("\n")
	}
	mods := []mod.Mod{{ID: "ticker", Builtin: true}, {ID: "digest"}}
	coord := func(name string, rows int, list []proto.ModView, o panelOpts) {
		pl, hits := coordPanel(goldenWatch(), 40, time.Now())
		o.rows, o.mods = rows, mods
		write("coordinator: "+name, layPanel(pl, hits, list, panelTarget{slot: mod.SlotCoordinatorSection, project: "terminatr"}, o).lines, rows)
	}
	both := []proto.ModView{tickerView(), digestView()}
	coord("below core's blocks (no layout)", 26, both, panelOpts{})
	coord("above NEEDS YOU", 30, []proto.ModView{tickerView()}, panelOpts{order: []string{"ticker.ticker", "needs"}})
	coord("between NEEDS YOU and THREADS", 30, []proto.ModView{tickerView()}, panelOpts{order: []string{"needs", "ticker.ticker"}})
	coord("above NEEDS YOU, 14 rows: the body gives way", 14, []proto.ModView{tickerView()}, panelOpts{order: []string{"ticker.ticker", "needs"}})
	coord("above NEEDS YOU, 10 rows: at the bottom", 10, []proto.ModView{tickerView()}, panelOpts{order: []string{"ticker.ticker", "needs"}})
	folds := view.Tree{}
	folds.FoldSections([]string{"panel:ticker.ticker", "panel:digest.today"}, true)
	coord("folded, with a badge", 26, both, panelOpts{folds: folds})
	failed := tickerView()
	failed.State, failed.Err, failed.Log, failed.Trees = proto.ModViewFailed, "exit 1", "~/.terminatr/log/mods/ticker.log", nil
	coord("failed", 26, []proto.ModView{failed, digestView()}, panelOpts{})
	coord("failed, folded", 26, []proto.ModView{failed}, panelOpts{folds: folds})
	coord("the digest's header selected", 26, both, panelOpts{sel: "panel:digest.today", focused: true})
	coord("safe mode: the built-in ticker alone", 26, both, panelOpts{noMods: true})

	// A thread's info panel: the details sections for the thread.
	c := infoClient(t, 200)
	c.views = []proto.ModView{{Mod: "ci", Name: "CI", View: "checks", Title: "checks", Slot: mod.SlotDetailsSection,
		Trees: []modview.Tree{{Project: "demo", Thread: "t-0002", Tree: noteTree("#408 ☒ global thread cap"), Badge: []widget.Span{{Text: "1 ☒", Role: "bad"}}}}}}
	c.showInfo(sampleInfo(time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)))
	c.mu.Lock()
	lines, rows := c.info.lines, c.infoRows()
	c.mu.Unlock()
	write("a thread's info panel, at the bottom", lines, rows)
	c.info.detOrder = []string{"ci.checks", "core"}
	c.mu.Lock()
	c.infoLayout()
	lines = c.info.lines
	c.mu.Unlock()
	write("a thread's info panel, above core's block", lines, rows)

	// The dashboard's details panel on a thread's row.
	views := detailsTestViews()
	_, m := detailsDash(t, views)
	m.focus = areaMain
	lay, _ := m.detailsNow()
	write("dashboard details panel", lay.lines, m.geom().details().h)
	_, m = detailsDash(t, views)
	m.layout.detSections = []string{"ci.checks", "core"}
	m.focus = areaMain
	lay, _ = m.detailsNow()
	write("dashboard details panel, checks above core's block", lay.lines, m.geom().details().h)
	src, m := detailsDash(t, views)
	src.data.NoMods = true
	src.data.Mods = []mod.Mod{{ID: "ci", Builtin: true}, {ID: "notes"}}
	m.setData(src.data)
	m.sel = "th:beta:t-0005"
	lay, _ = m.detailsNow()
	write("dashboard details panel, safe mode: the built-in mod's alone", lay.lines, m.geom().details().h)

	got := b.String()
	path := filepath.Join("testdata", "panel-sections.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestPanelSectionsGolden -update writes it)", err)
	}
	if got != string(want) {
		t.Errorf("the panels differ from %s:\n%s", path, got)
	}
}
