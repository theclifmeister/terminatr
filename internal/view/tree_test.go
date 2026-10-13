package view

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestTreeFolds: folding, expanding and renaming change the lists without
// sharing them with a copy; a tree keeps its JSON names inside a view.
func TestTreeFolds(t *testing.T) {
	var a Tree
	if !a.Fold("p", true) || a.Fold("p", true) || !a.IsFolded("p") {
		t.Fatalf("fold: %+v", a)
	}
	b := a
	if !b.Expand("q", true) || !b.Fold("r", true) || a.IsFolded("r") {
		t.Fatalf("a copy shares its lists: %+v %+v", a, b)
	}
	if !b.Rename("p", "s") || b.IsFolded("p") || !b.IsFolded("s") || b.Rename("zz", "y") {
		t.Fatalf("rename: %+v", b)
	}
	if !b.Rename("q", "t") || !b.IsExpanded("t") {
		t.Fatalf("rename an expanded project: %+v", b)
	}
	if a.Fold("", true) || a.Fold(strings.Repeat("x", MaxKey+1), true) {
		t.Fatal("folded a bad slug")
	}
	if err := (&Tree{Folded: []string{""}}).Valid(); err == nil {
		t.Fatal("an empty slug is valid")
	}
	v := View{Name: "main", Tree: Tree{Expanded: []string{"e"}, Folded: []string{"f"}, InactiveOpen: true}}
	data, _ := json.Marshal(v)
	for _, k := range []string{`"expanded_inactive":["e"]`, `"folded":["f"]`, `"inactive_open":true`} {
		if !strings.Contains(string(data), k) {
			t.Errorf("view JSON %s lacks %s", data, k)
		}
	}
	var back View
	if json.Unmarshal(data, &back) != nil || !slices.Equal(back.Expanded, []string{"e"}) || !back.IsFolded("f") || !back.InactiveOpen {
		t.Fatalf("round trip: %+v", back)
	}
}

// TestSectionFolds: a section's id is checked, folding is bounded with
// the oldest going first, never changes a copy, and follows a renamed
// project.
func TestSectionFolds(t *testing.T) {
	for id, ok := range map[string]bool{"ticker.ticker": true, "ci-status.prs@terminatr": true, "a.b@x-1": true,
		"panel:ticker.ticker": true, "panel:a.b@x": false, "panel:a": false, "panel:": false, "Panel:a.b": false, "x:a.b": false,
		"": false, "ticker": false, "Ticker.t": false, "a.b@": false, "a.b@-x": false, "a.b.c": false, "a.b@x@y": false} {
		if ValidSection(id) != ok {
			t.Errorf("ValidSection(%q) = %v", id, !ok)
		}
	}
	var a Tree
	if !a.FoldSections([]string{"m.v", "bad", "m.p@x", "panel:m.p"}, true) || !a.IsSectionFolded("m.v") || a.IsSectionFolded("bad") ||
		!a.IsSectionFolded("panel:m.p") {
		t.Fatalf("fold: %+v", a)
	}
	if a.FoldSections([]string{"m.v"}, true) {
		t.Fatal("folding a folded section changed the tree")
	}
	b := a
	if !b.FoldSections([]string{"m.v"}, false) || !a.IsSectionFolded("m.v") || b.IsSectionFolded("m.v") {
		t.Fatalf("a copy shares its list: %+v %+v", a, b)
	}
	if !b.Rename("x", "y") || !b.IsSectionFolded("m.p@y") || b.IsSectionFolded("m.p@x") || !a.IsSectionFolded("m.p@x") ||
		!b.IsSectionFolded("panel:m.p") {
		t.Fatalf("rename: %+v %+v", a, b)
	}
	var c Tree
	for i := range MaxSectionsFolded + 2 {
		c.FoldSections([]string{fmt.Sprintf("m.v@p%d", i)}, true)
	}
	if len(c.SectionsFolded) != MaxSectionsFolded || c.IsSectionFolded("m.v@p1") || !c.IsSectionFolded("m.v@p2") {
		t.Fatalf("bound: %d folded, first %q", len(c.SectionsFolded), c.SectionsFolded[0])
	}
	if c.Valid() != nil || (&Tree{SectionsFolded: []string{"nope"}}).Valid() == nil {
		t.Fatal("Valid on sections")
	}
	if (&Tree{SectionsFolded: make([]string, MaxSectionsFolded+1)}).Valid() == nil {
		t.Fatal("too many folded sections are valid")
	}
	data, _ := json.Marshal(View{Name: "main", Tree: Tree{SectionsFolded: []string{"m.v"}}})
	if !strings.Contains(string(data), `"sections_folded":["m.v"]`) {
		t.Errorf("view JSON %s lacks sections_folded", data)
	}
}
