package view

import (
	"encoding/json"
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
