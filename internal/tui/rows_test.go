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
	if c := tv.lines(60); len(c) != 3 {
		t.Errorf("after the body changed: %q", c)
	}
}

// TestNeedsYouLinkRows: NEEDS YOU has a row per thing a link waits on;
// enter on a held message or a file opens its dialog, on a request or a
// paused link the Network popup at its row; the header counts another
// project's links as network.
func TestNeedsYouLinkRows(t *testing.T) {
	m, _ := linkNet(t, 120, 30)
	m.current = "terminatr"
	m.setData(m.data)
	out := screen(m)
	for _, want := range []string{"1 file waits", "from mini/acme-api · enter opens it", "a link request", "from nas/media · enter opens Network", "1 message waits", "to web · enter opens it", "link paused", "with web · enter opens Network", "NEEDS YOU 4 · 1 network"} {
		if !strings.Contains(out, want) {
			t.Errorf("NEEDS YOU lacks %q:\n%s", want, out)
		}
	}
	for key, want := range map[string]string{
		"nl:terminatr>web>0":           "msg M-3",
		"nl:terminatr>mini/acme-api>1": "msg M-9C1B-1",
		"nl:terminatr>nas/media>2":     "net n:terminatr>nas/media>ask",
		"nl:terminatr>web>3":           "net n:terminatr>web>paused",
	} {
		m.stack = nil
		m.sel = key
		press(m, "enter")
		got := ""
		switch v := m.top().(type) {
		case *msgView:
			got = "msg " + v.e.msg.ID
		case *networkView:
			got = "net " + v.sel
		}
		if got != want {
			t.Errorf("enter on %s: %s (%T)", key, got, m.top())
		}
	}
}
