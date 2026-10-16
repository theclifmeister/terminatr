package tui

import (
	"slices"
	"testing"
)

// TestHardLines: a read-only popup's text, its tabs as four spaces, cut
// to the width (never under 10), without the trailing newlines.
func TestHardLines(t *testing.T) {
	for _, c := range []struct {
		text string
		w    int
		want []string
	}{
		{"one\ntwo\n\n", 20, []string{"one", "two"}},
		{"\tx", 20, []string{"    x"}},
		{"abcdefghijkl", 5, []string{"abcdefghij", "kl"}},
		{"abcdef", 3, []string{"abcdef"}},
	} {
		if got := hardLines(c.text, c.w); !slices.Equal(got, c.want) {
			t.Errorf("hardLines(%q, %d) = %q, want %q", c.text, c.w, got, c.want)
		}
	}
}
