package modview

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/widget"
)

func clock(s *Store) *time.Time {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	s.SetNow(func() time.Time { return now })
	return &now
}

func text(s string) *widget.Widget { return &widget.Widget{Kind: widget.KindText, Text: s} }

func TestSetTrees(t *testing.T) {
	s := New()
	n := 3
	if err := s.Set("m", "q", "", Tree{Tree: text("all")}, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("m", "q", "", Tree{Project: "alpha", Tree: text("a"), Count: &n, Height: 99}, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("m", "q", "", Tree{Project: "alpha", Tree: text("a2"), Height: 99}, 10); err != nil {
		t.Fatal(err)
	}
	got := s.Trees("m", "q")
	if len(got) != 2 || got[1].Tree.Text != "a2" || got[1].Height != MaxHeight {
		t.Fatalf("trees %+v", got)
	}
	s.Set("m", "q", "", Tree{Project: "alpha"}, 0)
	if got := s.Trees("m", "q"); len(got) != 1 || got[0].Project != "" {
		t.Fatalf("after clear %+v", got)
	}
	if err := s.Set("m", "other", "", Tree{Tree: text("big")}, MaxBytes); err == nil {
		t.Fatal("over the mod's bytes: no error")
	}
	// Replacing a tree doesn't count its old size.
	if err := s.Set("m", "q", "", Tree{Tree: text("big")}, MaxBytes-10); err != nil {
		t.Fatal(err)
	}
	s.Clear("m")
	if len(s.Trees("m", "q")) != 0 {
		t.Fatal("Clear kept trees")
	}
}

func TestSectionTrees(t *testing.T) {
	s := New()
	for _, tr := range []Tree{
		{Project: "alpha", Tree: text("project")},
		{Project: "alpha", Task: "T1", Tree: text("task")},
		{Project: "alpha", Thread: "t-0001", Tree: text("thread")},
		{Project: "alpha", Task: "T1", Tree: text("task2")},
	} {
		if err := s.Set("m", "s", "", tr, 10); err != nil {
			t.Fatal(err)
		}
	}
	got := s.Trees("m", "s")
	if len(got) != 3 || got[2].Tree.Text != "task2" || !got[0].For("alpha", "", "") {
		t.Fatalf("trees %+v", got)
	}
	s.Set("m", "s", "", Tree{Project: "alpha", Task: "T1"}, 0)
	if got := s.Trees("m", "s"); len(got) != 2 || got[1].Thread != "t-0001" {
		t.Fatalf("after clearing the task's %+v", got)
	}
	// A section shown for another row is another view on show.
	opened, _ := s.Show(1, []Shown{{Mod: "m", View: "s", Slot: "details.section", Project: "alpha", Task: "T1"}})
	if len(opened) != 1 {
		t.Fatalf("opened %+v", opened)
	}
	opened, closed := s.Show(1, []Shown{{Mod: "m", View: "s", Slot: "details.section", Project: "alpha", Task: "T2"}})
	if len(opened) != 1 || len(closed) != 1 || closed[0].Task != "T1" || opened[0].Task != "T2" {
		t.Fatalf("opened %+v closed %+v", opened, closed)
	}
}

func TestOpenNeedsHumanInput(t *testing.T) {
	s := New()
	now := clock(s)
	if s.Open("m", "q") {
		t.Fatal("opened with no input")
	}
	s.Input("m", 7, "alpha")
	if !s.Open("m", "q") {
		t.Fatal("refused right after an input")
	}
	if s.Open("m", "q") {
		t.Fatal("one input opened twice")
	}
	if o := s.Opens(7); len(o) != 1 || o[0].Project != "alpha" || o[0].View != "q" {
		t.Fatalf("opens %+v", o)
	}
	if len(s.Opens(8)) != 0 {
		t.Fatal("another console got the open")
	}
	s.Input("m", 7, "")
	*now = now.Add(InputWindow + time.Second)
	if s.Open("m", "q") {
		t.Fatal("opened after the input's window")
	}
	*now = now.Add(OpenLife)
	if len(s.Opens(7)) != 0 {
		t.Fatal("an old open request stayed")
	}
}

func TestShowOpenedClosed(t *testing.T) {
	s := New()
	now := clock(s)
	tab := Shown{Mod: "m", View: "q", Slot: "project.tab", Project: "alpha", Width: 80, Height: 20}
	card := Shown{Mod: "m", View: "c", Slot: "dashboard.card", Width: 60, Height: 8}
	op, cl := s.Show(1, []Shown{tab, card})
	if len(op) != 2 || len(cl) != 0 {
		t.Fatalf("first show: %+v %+v", op, cl)
	}
	// A second console showing the same views opens nothing; a resize
	// is not a new view.
	big := tab
	big.Width = 120
	if op, cl := s.Show(2, []Shown{big}); len(op)+len(cl) != 0 {
		t.Fatalf("second console: %+v %+v", op, cl)
	}
	if op, cl := s.Show(1, []Shown{card}); len(op)+len(cl) != 0 {
		t.Fatalf("console 2 still shows the tab: %+v %+v", op, cl)
	}
	if got := s.ShownBy("m"); len(got) != 2 {
		t.Fatalf("shown by m: %+v", got)
	}
	*now = now.Add(ShownLife + time.Second)
	s.Show(1, []Shown{card}) // console 1 polls; console 2 went silent
	if got := s.ShownBy("m"); len(got) != 1 || got[0].View != "c" {
		t.Fatalf("after console 2 expired: %+v", got)
	}
	_, cl = s.Show(1, nil)
	if len(cl) != 1 || cl[0].View != "c" {
		t.Fatalf("closing: %+v", cl)
	}
	if !strings.Contains(ErrFull("x").Error(), "x") {
		t.Fatal("ErrFull")
	}
}

// TestHeightsAndBadge: each slot caps the height it asks for, and a
// badge is a few spans in a few cells.
func TestHeightsAndBadge(t *testing.T) {
	s := New()
	for slot, want := range map[string]int{"sidebar.section": MaxSidebarHeight, "sidebar.project.section": MaxProjectSectionHeight, "dashboard.card": MaxHeight} {
		if err := s.Set("m", slot, slot, Tree{Tree: text("x"), Height: 99}, 10); err != nil {
			t.Fatal(err)
		}
		if got := s.Trees("m", slot)[0].Height; got != want {
			t.Errorf("%s: height %d, want %d", slot, got, want)
		}
	}
	if d, m := Heights("sidebar.project.section"); d != DefaultProjectSectionHeight || m != MaxProjectSectionHeight {
		t.Errorf("heights %d %d", d, m)
	}
	ok := []widget.Span{{Text: "PR host ", Role: "bad"}, {Icon: "blocked", Role: "bad"}}
	if err := CheckBadge(ok); err != nil {
		t.Error(err)
	}
	for _, bad := range [][]widget.Span{
		{{Text: "twelve cells!"}},
		{{Text: "eleven cell"}, {Icon: "blocked"}},
		{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}, {Text: "e"}},
		{{Icon: "bogus"}},
	} {
		if err := CheckBadge(bad); err == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}
