package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPRRefAzure(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/o/r/pull/7":                     "#7",
		"https://dev.azure.com/o/p/_git/r/pullrequest/34":   "#34",
		"https://o.visualstudio.com/p/_git/r/pullrequest/2": "#2",
	} {
		if got := prRef(url); got != want {
			t.Errorf("prRef(%q) = %q, want %q", url, got, want)
		}
	}
}

// The progress bar is drawn only when the whole row fits.
func TestRowBarFits(t *testing.T) {
	r := row{what: "a task title", state: "working", pct: 40, mark: markTop}
	if !strings.Contains(r.text(100), "▰") {
		t.Errorf("no bar in a wide row: %q", r.text(100))
	}
	narrow := r.text(40)
	if strings.Contains(narrow, "▰") || ansi.StringWidth(narrow) > 40 {
		t.Errorf("bar drawn in a narrow row: %q", narrow)
	}
	if got := (row{what: "x", state: "working", pct: -1}).text(100); strings.Contains(got, "▰") {
		t.Errorf("bar without a percent: %q", got)
	}
}

// A text view wraps its body once per width, and again when either changes.
func TestTextViewWrapCached(t *testing.T) {
	tv := &textView{body: strings.Repeat("word ", 40) + "\nsecond", note: "n"}
	a := tv.lines(30)
	if len(a) < 4 {
		t.Fatalf("not wrapped: %q", a)
	}
	first := &tv.wrapped[0]
	tv.lines(30)
	if &tv.wrapped[0] != first {
		t.Error("wrapped again at the same width")
	}
	if b := tv.lines(60); len(b) >= len(a) {
		t.Errorf("a wider view has %d lines, not fewer than %d", len(b), len(a))
	}
	tv.body = "short"
	if c := tv.lines(60); len(c) != 2 {
		t.Errorf("after the body changed: %q", c)
	}
}
