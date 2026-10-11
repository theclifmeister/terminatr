package view

import (
	"slices"
	"strings"
	"testing"
)

// TestColumns: an order keeps the ids it names, in its order and once
// each, drops the ones this tm doesn't know, gets back the safety columns
// it left out, and is the default when empty.
func TestColumns(t *testing.T) {
	for _, c := range []struct {
		in, want []string
	}{
		{nil, DefaultColumns},
		{[]string{"needs", "state", "count", "state", "ctx", "Bad.Mod"},
			[]string{"needs", "state", "count", "paused", "inactive"}},
		{[]string{"count", "review-age.age", "state", "remote", "paused", "inactive", "needs"},
			[]string{"count", "review-age.age", "state", "remote", "paused", "inactive", "needs"}},
		{[]string{"ci"}, []string{"ci", "state", "paused", "inactive", "needs"}},
	} {
		if got := Columns(c.in); !slices.Equal(got, c.want) {
			t.Errorf("Columns(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsModColumn(t *testing.T) {
	for id, want := range map[string]bool{
		"review-age.age": true, "pr-ci.ci2": true,
		"state": false, ".age": false, "review-age.": false, "a.b.c": false, "Review.age": false, "a b.c": false,
	} {
		if got := IsModColumn(id); got != want {
			t.Errorf("IsModColumn(%q) = %v", id, got)
		}
	}
}

// TestViewColumns: a view checks the size of its column list and keeps
// none for the default order.
func TestViewColumns(t *testing.T) {
	v := View{Name: Main, Columns: make([]string, MaxColumns+1)}
	if v.Valid() == nil {
		t.Error("too many columns valid")
	}
	v.Columns = []string{strings.Repeat("a", 200)}
	if v.Valid() == nil {
		t.Error("a too long column id valid")
	}
	v.Columns = slices.Clone(DefaultColumns)
	v.Normalize()
	if v.Columns != nil {
		t.Errorf("the default order kept: %q", v.Columns)
	}
	v.Columns = []string{"needs", "nope"}
	v.Normalize()
	if want := []string{"needs", "state", "paused", "inactive"}; !slices.Equal(v.Columns, want) {
		t.Errorf("normalized %q, want %q", v.Columns, want)
	}
}
