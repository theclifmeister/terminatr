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

func init() {
	// The deadline's tick is drained in tests: a short one.
	typeDeadline = 20 * time.Millisecond
}

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
	c.typeAhead, c.typeAheadFor = []Key{{Code: 'H', Text: "H"}, {Code: 'i', Text: "i"}, {Code: ' ', Text: " "}, {Code: '/', Text: "/"}}, "s-1"
	c.replay(t.Context())
	if got := received(t, in, 4); got != "Hi /" || c.typeAhead != nil {
		t.Fatalf("replayed %q", got)
	}
}

// TestTypeThroughReplayOtherPane: keys typed to one coordinator never
// reach another pane the shared view focused meanwhile (another console
// attached a thread, say): they are dropped, and the status bar says so.
func TestTypeThroughReplayOtherPane(t *testing.T) {
	c, p := selClient(t, "") // s-1 focused
	in := pipedInput(t, p)
	c.typeAhead, c.typeAheadFor = []Key{{Code: 'h', Text: "h"}, {Code: tea.KeyEnter}}, "s-7"
	c.replay(t.Context())
	select {
	case got := <-in:
		t.Fatalf("the other pane got %q", got)
	case <-time.After(200 * time.Millisecond):
	}
	if !strings.Contains(c.flash, "what you typed was dropped") || c.typeAhead != nil {
		t.Fatalf("flash %q", c.flash)
	}
}

// TestTypeThroughOtherFocus: the dashboard hands the keys over only when
// the session it attaches is the coordinator they were typed to; when
// the view shows another, it drops them, and the session says so.
func TestTypeThroughOtherFocus(t *testing.T) {
	src := &fakeSource{data: testData()}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "alpha"}})
	m.setData(src.data)
	m.sel = "p:alpha"
	keyPress(m, "h")
	keyPress(m, "enter")
	if keys, to := m.typedFor("s-1"); typedText(keys) != "h⏎" || to != "s-1" {
		t.Fatalf("for s-1: %q to %q", typedText(keys), to)
	}
	if keys, to := m.typedFor("s-2"); keys != nil || to != "" || !strings.Contains(m.result.Message, "was dropped") {
		t.Fatalf("for s-2: %q to %q, message %q", typedText(keys), to, m.result.Message)
	}
}

// TestTypeThroughCancel: while keys are typed through, ctrl+c cancels
// the typing and the prefix ends it, both dropping the keys and saying
// so; the prefix then starts its command. Past the deadline with no
// attach the keys are dropped too, and the list takes keys again.
func TestTypeThroughCancel(t *testing.T) {
	newM := func() *dash {
		src := &fakeSource{data: testData()}
		m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 30, State: DashState{Current: "alpha"}})
		m.setData(src.data)
		m.sel = "p:alpha"
		keyPress(m, "h")
		keyPress(m, "i")
		return m
	}
	m := newM()
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); quits(cmd) || m.typing || m.typed != nil ||
		m.msg != "typing to the coordinator cancelled: 2 keys dropped" {
		t.Fatalf("ctrl+c: typing %v typed %q msg %q", m.typing, typedText(m.typed), m.msg)
	}

	m = newM()
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if m.typing || !m.prefixed || !strings.Contains(m.msg, "ended by the prefix: 2 keys dropped") {
		t.Fatalf("prefix: typing %v prefixed %v msg %q", m.typing, m.prefixed, m.msg)
	}
	keyPress(m, "a")
	if _, ok := m.top().(*projectView); !ok {
		t.Fatalf("prefix+a after it: %T", m.top())
	}

	m = newM()
	m.busy = false                               // the attach answered; no view version came
	m.Update(typeTimeoutMsg{seq: m.typeSeq - 1}) // an older one's
	if !m.typing {
		t.Fatal("an old deadline ended the typing")
	}
	m.Update(typeTimeoutMsg{seq: m.typeSeq})
	if m.typing || !strings.Contains(m.msg, "timed out") {
		t.Fatalf("deadline: typing %v msg %q", m.typing, m.msg)
	}
	keyPress(m, "down")
	if m.sel == "p:alpha" {
		t.Fatal("the list takes no keys after the deadline")
	}
}

// TestTypeThroughAttachCancel: in a session, ctrl+c cancels the keys
// the sidebar holds for a coordinator and the prefix ends them, both
// dropping them; the prefix then arms.
func TestTypeThroughAttachCancel(t *testing.T) {
	c, _ := selClient(t, "")
	c.prefix, _ = parseChord(DefaultPrefixKey)
	hold := func() {
		c.mu.Lock()
		c.typeFor, c.typeKeys, c.typeBy = "s-9", []Key{{Code: 'G', Text: "G"}}, time.Now().Add(time.Minute)
		c.mu.Unlock()
	}
	hold()
	if !c.holdTyped(Key{Code: 'c', Mod: uv.ModCtrl}) || c.typeFor != "" || !strings.Contains(c.flash, "cancelled: 1 key dropped") {
		t.Fatalf("ctrl+c: for %q flash %q", c.typeFor, c.flash)
	}
	hold()
	c.key(Key{Code: 'b', Mod: uv.ModCtrl})
	if c.typeFor != "" || !c.pending {
		t.Fatalf("prefix: for %q pending %v", c.typeFor, c.pending)
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

// TestTypeThroughReplayFocusMoves: replay checks the focus before every
// key; when the shared view moves it off the coordinator midway, the
// rest are dropped, never sent to the pane that has it now.
func TestTypeThroughReplayFocusMoves(t *testing.T) {
	c, p := selClient(t, "") // s-1 focused
	in := pipedInput(t, p)
	n := 0
	beforeReplayKey = func(c *client) {
		if n++; n == 2 { // after the first key, another session takes the focus
			c.mu.Lock()
			c.focus = &pane{info: proto.SessionInfo{ID: "s-8"}}
			c.mu.Unlock()
		}
	}
	t.Cleanup(func() { beforeReplayKey = func(*client) {} })
	c.typeAhead, c.typeAheadFor = []Key{{Code: 'h', Text: "h"}, {Code: 'i', Text: "i"}, {Code: tea.KeyEnter}}, "s-1"
	c.replay(t.Context())
	if got := received(t, in, 1); got != "h" {
		t.Fatalf("got %q", got)
	}
	select {
	case got := <-in:
		t.Fatalf("after the focus moved, the pane got %q", got)
	case <-time.After(200 * time.Millisecond):
	}
	if !strings.Contains(c.flash, "what you typed was dropped") {
		t.Fatalf("flash %q", c.flash)
	}
	c.mu.Lock()
	c.focus = p // selClient's cleanup frees its pane
	c.mu.Unlock()
}

// TestUnfoldLetterOnlyUnfolds: a keymap's unshifted letter on
// side.unfold (vim-keys' l) unfolds a folded row and does nothing more:
// on a coordinator's row, which → opens, it neither opens a pane nor
// starts the coordinator, as typed text must not; → still does.
func TestUnfoldLetterOnlyUnfolds(t *testing.T) {
	if err := keymap(t, map[string][]string{"side.unfold": {"right", "l"}}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := foldDash(t)
	for _, sel := range []string{"c:alpha", headKey} {
		m.sideSel = sel
		if st, ok := sideKeyFor(m.sideKeysIn(), "l"); !ok || st.target != nil || st.into || st.toggle != "" {
			t.Fatalf("l on %s: %+v", sel, st)
		}
	}
	m.sideSel = "c:alpha"
	if st, _ := sideKeyFor(m.sideKeysIn(), "right"); st.target == nil {
		t.Fatalf("→ on the coordinator: %+v", st)
	}
	// On a folded project l unfolds it.
	m.sideSel = "p:alpha"
	keyPress(m, "left")
	if st, _ := sideKeyFor(m.sideKeysIn(), "l"); st.refold == nil {
		t.Fatalf("l on folded alpha: %+v", st)
	}
	// The divider, folded: l unfolds it too.
	m.sideSel = dividerKey
	if st, _ := sideKeyFor(m.sideKeysIn(), "l"); st.refold == nil {
		t.Fatalf("l on the folded divider: %+v", st)
	}
}
