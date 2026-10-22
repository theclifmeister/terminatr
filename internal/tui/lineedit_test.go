package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
)

// editKeys are the line-editing keys as a terminal sends them, by the
// names lineEdit takes.
var editKeys = map[string]tea.KeyPressMsg{
	"left":          {Code: tea.KeyLeft},
	"right":         {Code: tea.KeyRight},
	"home":          {Code: tea.KeyHome},
	"end":           {Code: tea.KeyEnd},
	"ctrl+a":        {Code: 'a', Mod: tea.ModCtrl},
	"ctrl+e":        {Code: 'e', Mod: tea.ModCtrl},
	"alt+left":      {Code: tea.KeyLeft, Mod: tea.ModAlt},
	"alt+right":     {Code: tea.KeyRight, Mod: tea.ModAlt},
	"alt+b":         {Code: 'b', Mod: tea.ModAlt},
	"alt+f":         {Code: 'f', Mod: tea.ModAlt},
	"backspace":     {Code: tea.KeyBackspace},
	"alt+backspace": {Code: tea.KeyBackspace, Mod: tea.ModAlt},
	"delete":        {Code: tea.KeyDelete},
	"ctrl+w":        {Code: 'w', Mod: tea.ModCtrl},
	"ctrl+k":        {Code: 'k', Mod: tea.ModCtrl},
	"ctrl+u":        {Code: 'u', Mod: tea.ModCtrl},
}

// editKey presses an editing key, or types text.
func editKey(m *dash, k string) {
	msg, ok := editKeys[k]
	if !ok {
		msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
	}
	if msg.String() != k && ok {
		panic("key " + k + " is " + msg.String())
	}
	m.Update(msg)
}

// TestInputEditKeys: an edit dialog edits at its cursor: typing goes in
// there, the cursor moves by a character, a word and to either end, and
// the keys that delete take what they name.
func TestInputEditKeys(t *testing.T) {
	cases := []struct {
		start string
		keys  []string
		want  string
	}{
		{"", []string{"a", "b", "c"}, "abc█"},
		{"hello world", []string{"left", "left", "X"}, "hello worX█ld"},
		{"hello world", []string{"home", "X"}, "X█hello world"},
		{"hello world", []string{"ctrl+a", "right", "ctrl+e", "!"}, "hello world!█"},
		{"hello world", []string{"home", "end", "left", "backspace"}, "hello wor█d"},
		{"hello world", []string{"home", "delete"}, "█ello world"},
		{"hello world", []string{"end", "delete"}, "hello world█"},
		{"hello world", []string{"home", "backspace"}, "█hello world"},
		{"a/b-c d", []string{"alt+left"}, "a/b-c █d"},
		{"a/b-c d", []string{"alt+left", "alt+left", "alt+b"}, "a/█b-c d"},
		{"a/b-c d", []string{"home", "alt+right", "alt+f"}, "a/b█-c d"},
		{"one two  ", []string{"ctrl+w"}, "one █"},
		{"one two", []string{"alt+backspace"}, "one █"},
		{"one two", []string{"left", "left", "ctrl+w", "ctrl+w"}, "█wo"},
		{"one two", []string{"home", "right", "ctrl+k"}, "o█"},
		// ctrl+u clears the line, wherever the cursor is, as before.
		{"one two", []string{"left", "left", "ctrl+u", "x"}, "x█"},
	}
	for _, c := range cases {
		src := &fakeSource{data: testData()}
		m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
		m.setData(src.data)
		m.prompt("Edit", "Its value:", c.start, func(string) tea.Cmd { return nil })
		for _, k := range c.keys {
			editKey(m, k)
		}
		in := m.top().(*inputView)
		if got, _ := in.lines(80); got[0] != c.want {
			t.Errorf("%q then %v: %q, want %q", c.start, c.keys, got[0], c.want)
		}
		if out := screen(m); !strings.Contains(out, c.want) {
			t.Errorf("%q then %v: no %q on screen:\n%s", c.start, c.keys, c.want, out)
		}
	}
}

// TestInputPrefilled: a prefilled value stays, the cursor at its end:
// typing adds to it rather than replacing it, and enter sends it as
// edited.
func TestInputPrefilled(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	var got string
	m.prompt("Edit", "Its value:", "wss://relay.example.net", func(s string) tea.Cmd { got = s; return nil })
	for _, k := range []string{":", "1", "home", "ctrl+right", "s"} {
		editKey(m, k)
	}
	keyPress(m, "enter")
	if got != "wsss://relay.example.net:1" {
		t.Errorf("sent %q", got)
	}
}

// TestInputPlaceholder: an example shows faint in an empty field, is
// gone once anything is typed, and is never the value: enter on an
// empty field still cancels.
func TestInputPlaceholder(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	prefixed(m, "+")
	if out := m.render(); !strings.Contains(out, th().Faint.Render("demo-app")) || !strings.Contains(screen(m), "│ █demo-app") {
		t.Fatalf("no faint example:\n%s", screen(m))
	}
	editKey(m, "x")
	if out := screen(m); strings.Contains(out, "demo-app") || !strings.Contains(out, "│ x█") {
		t.Fatalf("example stays after typing:\n%s", out)
	}
	editKey(m, "backspace")
	if !strings.Contains(screen(m), "demo-app") {
		t.Fatalf("no example on an emptied field:\n%s", screen(m))
	}
	if cmd := keyPress(m, "enter"); cmd != nil || m.top() != nil {
		t.Fatalf("enter on an empty field with an example did something: %T", m.top())
	}
}

// TestInputLongValue: a value longer than the dialog has room for wraps,
// and the popup keeps the cursor's line in view at either end.
func TestInputLongValue(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	long := "start" + strings.Repeat("x", 1500) + "finish"
	m.prompt("Edit", "Its value:", long, func(string) tea.Cmd { return nil })
	if out := screen(m); !strings.Contains(out, "finish█") || strings.Contains(out, "start") {
		t.Fatalf("cursor at the end not in view:\n%s", out)
	}
	editKey(m, "home")
	if out := screen(m); !strings.Contains(out, "█start") || strings.Contains(out, "finish") {
		t.Fatalf("cursor at the start not in view:\n%s", out)
	}
}

// TestTextEditorEditKeys: the text editor shares the line-editing keys,
// line by line: ctrl+k and home stop at the line's end and start.
func TestTextEditorEditKeys(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	m.editText("Guide", "Its text:", "one two\nthree four", 0, func(string) tea.Cmd { return nil })
	ed := m.top().(*textEditor)
	for _, k := range []string{"up", "home", "alt+right", "ctrl+k", "end", "ctrl+w", "right", "alt+f", "ctrl+u"} {
		if msg, ok := editKeys[k]; ok {
			m.Update(msg)
		} else {
			keyPress(m, k)
		}
	}
	if got := ed.value(); got != "\n four" {
		t.Errorf("edited to %q", got)
	}
}

// TestBranchNamesDefault: Branch names at its default opens empty, the
// default faint, and enter keeps the default.
func TestBranchNamesDefault(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 100, Height: 30})
	m.setData(src.data)
	newSafetyRows("").branchRow().change(m)
	in, ok := m.top().(*inputView)
	if !ok || in.value() != "" || in.placeholder != config.DefaultBranchName {
		t.Fatalf("opened %T (placeholder %q)", m.top(), in.placeholder)
	}
	if !strings.Contains(screen(m), "█"+config.DefaultBranchName) {
		t.Errorf("no default shown:\n%s", screen(m))
	}
}
