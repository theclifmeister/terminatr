package tui

import (
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// prefixed presses the prefix, then key: the list's command on a
// coordinator's row, where a plain printable key types through.
func prefixed(m *dash, key string) tea.Cmd {
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	return keyPress(m, key)
}

// typedText is the text of keys, as typed.
func typedText(keys []Key) string {
	var b strings.Builder
	for _, k := range keys {
		switch {
		case k.Text != "":
			b.WriteString(k.Text)
		case k.Code == tea.KeyEnter:
			b.WriteString("⏎")
		default:
			b.WriteString("<" + k.String() + ">")
		}
	}
	return b.String()
}

// TestTypeThroughList: on a running coordinator's row of the list, the
// first printable key attaches the coordinator and every key since,
// capitals, digits, space, / and enter among them, goes to its pane in
// order; the footer says so and names the list's keys with the prefix.
// A thread's row keeps the guard: the key is the list's, and enter
// attaches it first.
func TestTypeThroughList(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "alpha"}})
	m.setData(src.data)
	m.sel = "p:alpha" // alpha's coordinator runs: s-1
	if foot := m.footKeys(); !strings.Contains(foot, typeThroughHint) || !strings.Contains(foot, "prefix+A project") {
		t.Fatalf("footer %q", foot)
	}
	cmd := keyPress(m, "h")
	for _, k := range []string{"i", " ", "A", "1", "/", "?", "enter"} {
		keyPress(m, k)
	}
	if m.top() != nil {
		t.Fatalf("a key opened %T", m.top())
	}
	// The attach waits for the typing to pause, then ends the dashboard.
	drain(t, m, cmd, reflect.TypeOf(typeSettledMsg{}))
	if m.result.Attach != "s-1" || !m.settling {
		t.Fatalf("result %+v", m.result)
	}
	if got := typedText(m.typed); got != "hi A1/?⏎" {
		t.Fatalf("typed %q", got)
	}
	if m.focus != areaMain {
		t.Fatalf("focus %d: the keyboard stays out of the pane", m.focus)
	}

	// A thread's row: T is the tasks key, nothing is typed.
	src = &fakeSource{data: testData()}
	m = newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "beta"}})
	m.setData(src.data)
	m.sel = "th:beta:t-0005"
	if strings.Contains(m.footKeys(), typeThroughHint) {
		t.Fatalf("a thread's footer offers typing: %q", m.footKeys())
	}
	keyPress(m, "h")
	if m.typing || len(m.typed) > 0 || m.result.Attach != "" {
		t.Fatalf("h on a thread typed %q", typedText(m.typed))
	}
	keyPress(m, "T")
	if _, ok := m.top().(*projectView); !ok {
		t.Fatalf("T on a thread: %T", m.top())
	}
}

// TestTypeThroughPrefix: on a coordinator's row the list's commands are
// the prefix's, and keys that aren't printable keep their meaning.
func TestTypeThroughPrefix(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "alpha"}})
	m.setData(src.data)
	m.sel = "p:alpha"
	prefixed(m, "a")
	if _, ok := m.top().(*projectView); !ok || m.typing {
		t.Fatalf("prefix+a: %T, typing %v", m.top(), m.typing)
	}
	m.pop()
	keyPress(m, "down")
	if m.typing || m.sel == "p:alpha" {
		t.Fatalf("down typed (%v) or didn't move (%s)", m.typing, m.sel)
	}
}

// TestTypeThroughSidebar: in the dashboard's sidebar with the keyboard,
// a key on a running coordinator's row types through to it; on a
// thread's row it is the sidebar's (or nothing).
func TestTypeThroughSidebar(t *testing.T) {
	m, _ := sideDash(t) // the sidebar has the keyboard
	m.sideSel = "c:alpha"
	if bar := sideKeyBar(&treeRow{kind: treeCoordinator, slug: "alpha", session: "s-1", state: "idle"}, true, false, 200); !strings.Contains(ansi.Strip(bar), "type to write to it") {
		t.Fatalf("key bar %q", bar)
	}
	keyPress(m, "W")
	keyPress(m, "x")
	if !m.typing || typedText(m.typed) != "Wx" || m.focus != areaMain {
		t.Fatalf("typing %v %q focus %d", m.typing, typedText(m.typed), m.focus)
	}

	m, _ = sideDash(t)
	m.sideSel = "t:beta/t-0005"
	keyPress(m, "x")
	if m.typing || m.focus != areaSide {
		t.Fatalf("x on a thread's row typed: %v, focus %d", m.typing, m.focus)
	}
}

// pipedInput gives p a connection and returns what the client sends the
// program, as it comes.
func pipedInput(t *testing.T, p *pane) <-chan string {
	near, far := net.Pipe()
	t.Cleanup(func() { far.Close() })
	p.conn = tmclient.StreamOver(near)
	out := make(chan string, 64)
	go func() {
		r := tmclient.StreamOver(far)
		for {
			typ, b, err := r.ReadFrame()
			if err != nil {
				return
			}
			if typ == proto.FrameInput {
				out <- string(b)
			}
		}
	}()
	return out
}

// received collects what the program got until it is n bytes.
func received(t *testing.T, in <-chan string, n int) string {
	t.Helper()
	var got string
	for len(got) < n {
		select {
		case s := <-in:
			got += s
		case <-time.After(2 * time.Second):
			t.Fatalf("the program got %q", got)
		}
	}
	return got
}

// TestTypeThroughReplay: the attach runs the keys typed through on the
// dashboard first, into the focused pane, in order.
func TestTypeThroughReplay(t *testing.T) {
	c, p := selClient(t, "")
	in := pipedInput(t, p)
	c.typeAhead = []Key{{Code: 'H', Text: "H"}, {Code: 'i', Text: "i"}, {Code: ' ', Text: " "}, {Code: '/', Text: "/"}}
	c.replay(t.Context())
	if got := received(t, in, 4); got != "Hi /" || c.typeAhead != nil {
		t.Fatalf("replayed %q", got)
	}
}

// TestTypeThroughAttachSidebar: in a session, with the sidebar's cursor
// on a running coordinator's row, a printable key and those after it
// wait until that coordinator's pane has the focus, then reach it in
// order; the keyboard is the pane's.
func TestTypeThroughAttachSidebar(t *testing.T) {
	c, p := selClient(t, "")
	in := pipedInput(t, p)
	c.keyQueue = make(chan uv.Event, 8)
	c.focus = nil // the view shows another session yet
	c.mu.Lock()
	c.typeFor, c.typeKeys, c.typeBy = "s-1", []Key{{Code: 'G', Text: "G"}}, time.Now().Add(time.Minute)
	c.kb = areaSide
	c.mu.Unlock()
	c.key(Key{Code: 'o', Text: "o"})
	c.flushTyped() // not yet: no focus
	c.mu.Lock()
	if len(c.typeKeys) != 2 {
		t.Fatalf("held %d keys", len(c.typeKeys))
	}
	c.focus = p
	c.typedFocus()
	c.mu.Unlock()
	if ev := <-c.keyQueue; ev != (typedReady{}) {
		t.Fatalf("event %v", ev)
	}
	c.flushTyped()
	if got := received(t, in, 2); got != "Go" || c.kb != areaMain || c.typeFor != "" {
		t.Fatalf("got %q, keyboard %d", got, c.kb)
	}
}
