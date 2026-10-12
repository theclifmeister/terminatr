package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// noteTree is a section's tree: a list, which a section draws with no
// cursor row.
func noteTree(text string) *widget.Widget {
	return &widget.Widget{Kind: widget.KindList, ID: "n", Rows: []widget.Row{{ID: "1", Spans: []widget.Span{{Text: text}}}}}
}

// TestDetailsSections: the details panel ends with the mods' sections
// for the row it shows, marked as theirs, and says which it shows, by
// the row's task or thread; a row a mod drew nothing for gets none.
func TestDetailsSections(t *testing.T) {
	src := &fakeSource{data: testData(), modViews: proto.ModViewsResult{Views: []proto.ModView{
		{Mod: "notes", Name: "Notes", View: "n", Title: "Notes", Slot: "details.section", Trees: []modview.Tree{
			{Project: "beta", Thread: "t-0005", Tree: noteTree("reviewed twice")},
			{Project: "alpha", Tree: noteTree("alpha is on track")},
		}},
		{Mod: "digest", View: "d", Title: "Digest", Slot: "coordinator.section", Trees: []modview.Tree{{Tree: noteTree("not here")}}},
	}}}
	m := newDash(DashOptions{Source: src, Width: 160 + sideDefault, Height: 40, State: DashState{Current: "alpha"}})
	m.layout.Details = true
	m.setData(src.data)
	m.setModViews(modViewsMsg{res: src.modViews})

	panelOf := func(key string) string {
		t.Helper()
		m.current = strings.Split(key, ":")[1]
		m.rebuild()
		m.sel, m.userSel = key, true
		r, ok := m.selected()
		if !ok {
			t.Fatalf("no row %s", key)
		}
		return ansi.Strip(strings.Join(m.details(r, 60), "\n"))
	}
	out := panelOf("th:beta:t-0005")
	i, j := strings.Index(out, "Notes ─"), strings.Index(out, "reviewed twice")
	if i < 0 || j < i || strings.Contains(out, "not here") || strings.Contains(out, "alpha is on track") {
		t.Fatalf("thread's panel:\n%s", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "reviewed twice") {
		t.Errorf("the section isn't at the bottom:\n%s", out)
	}
	var sec []modview.Shown
	for _, s := range m.shownViews() {
		if s.Slot == "details.section" {
			sec = append(sec, s)
		}
	}
	if len(sec) != 1 || sec[0].Mod != "notes" || sec[0].Project != "beta" || sec[0].Thread != "t-0005" || sec[0].Task != "" {
		t.Fatalf("shown %+v", sec)
	}
	if out := panelOf("p:alpha"); !strings.Contains(out, "alpha is on track") {
		t.Fatalf("project's panel:\n%s", out)
	}
	if out := panelOf("p:beta"); strings.Contains(out, "Notes ─") {
		t.Fatalf("a section the mod drew nothing for:\n%s", out)
	}
	// A failed mod says so, not its stale tree.
	src.modViews.Views[0].State, src.modViews.Views[0].Err = proto.ModViewFailed, "boom"
	m.setModViews(modViewsMsg{res: src.modViews})
	if out := panelOf("th:beta:t-0005"); !strings.Contains(out, "mod failed: boom") || strings.Contains(out, "reviewed twice") {
		t.Fatalf("failed:\n%s", out)
	}
}

// TestCoordSections: the coordinator's panel ends with the mods'
// coordinator sections for its project.
func TestCoordSections(t *testing.T) {
	list := []proto.ModView{
		{Mod: "digest", View: "d", Title: "Digest", Slot: "coordinator.section", Projects: []string{"demo"},
			Trees: []modview.Tree{{Project: "demo", Tree: noteTree("3 merged today")}}},
		{Mod: "other", View: "o", Title: "Other", Slot: "coordinator.section", Projects: []string{"elsewhere"},
			Trees: []modview.Tree{{Tree: noteTree("not for demo")}}},
		{Mod: "notes", View: "n", Title: "Notes", Slot: "details.section", Trees: []modview.Tree{{Tree: noteTree("details only")}}},
	}
	pw := &proto.ProjectWatch{Project: "demo"}
	lines, hits := infoLines(&infoData{slug: "demo", watch: pw, sections: list}, 40, time.Now())
	out := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(out, "Digest ─") || strings.Contains(out, "mod: ") || !strings.HasSuffix(strings.TrimSpace(out), "3 merged today") ||
		strings.Contains(out, "not for demo") || strings.Contains(out, "details only") {
		t.Fatalf("panel:\n%s", out)
	}
	if len(hits) != len(lines) {
		t.Fatalf("%d hits for %d lines", len(hits), len(lines))
	}
	shown := sectionsShown(list, "coordinator.section", "demo", "", "", 40)
	if len(shown) != 1 || shown[0].Mod != "digest" || shown[0].Width != 38 {
		t.Fatalf("shown %+v", shown)
	}
}
