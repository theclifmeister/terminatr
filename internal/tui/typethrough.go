package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// Type-through (docs/SPEC.md §4): on a running coordinator's row, the
// list's or the sidebar's with the keyboard, a printable key is the
// start of what you write to the coordinator. The first attaches it, and
// it and every key after it, typed while the attach happens, go to its
// pane in order (DashResult.Type, client.replay), so it lands in the
// agent's prompt. Every printable key does, capitals, digits, space and
// / among them; the list's commands are prefix+key there. A thread's row
// keeps the guard: enter attaches it first. A paired server's
// coordinator types through too, from the sidebar, when that server
// lets this one type into its panes (§16.12): its pane "<alias>/<id>".

// typeThrough starts typing through to the coordinator of the selected
// row with key k; ok is false when k or the row isn't for it.
func (m *dash) typeThrough(k tea.KeyPressMsg) (tea.Cmd, bool) {
	if !printable(k) || m.pressing {
		return nil, false
	}
	id, slug, ok := m.typeThroughRow()
	if !ok {
		return nil, false
	}
	m.typing, m.typed, m.typedAt, m.typeFor = true, []Key{Key(k)}, time.Now(), id
	m.typeSeq++
	seq := m.typeSeq
	m.focus = areaMain // the keyboard goes into the pane
	return tea.Batch(m.act(func() actionMsg { return actionMsg{attach: id, current: slug} }),
		tea.Tick(typeDeadline, func(time.Time) tea.Msg { return typeTimeoutMsg{seq} })), true
}

// typeThroughRow is the running coordinator's session (and its project)
// whose row has the keyboard: the list's selected row, or the sidebar's
// cursor while the sidebar has the keyboard and no filter is typed.
func (m *dash) typeThroughRow() (id, slug string, ok bool) {
	switch m.area() {
	case areaMain:
		if m.remoteHere() {
			return "", "", false
		}
		r, ok := m.selected()
		if !ok || r.session == "" || r.task != nil || r.card || r.remote != "" {
			return "", "", false
		}
		if s, found := m.session(r.session); found && s.Role == proto.RoleCoordinator {
			return s.ID, s.Project, true
		}
	case areaSide:
		if sideFilterNow().typing {
			return "", "", false
		}
		if rows, i := cursorAt(m.tree(), m.sideW(), m.sideSel); i >= 0 {
			return typeTarget(rows[i])
		}
	}
	return "", "", false
}

// typeTarget is the session a key on sidebar row r types through to,
// and its project: a running coordinator's of this server, or of a
// paired server that lets this one type into its panes, the session
// "<alias>/<id>" in its project "@alias/slug" (peerTarget); ok is false
// for any other row, a view-only remote pane's too.
func typeTarget(r treeRow) (id, project string, ok bool) {
	switch {
	case r.session == "":
	case r.kind == treeCoordinator && !r.isPeer():
		return r.session, r.slug, true
	case r.kind == treePeerCoord && r.link != nil && r.link.panes == proto.PanesType && paneWhyNot(r) == "":
		return r.peer + "/" + r.session, peerFold(r.peer, r.slug), true
	}
	return "", "", false
}

// printable says whether k types a character: with or without shift,
// never with ctrl, alt or the like (those keep their meaning).
func printable(k tea.KeyPressMsg) bool {
	return k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// typeTimeoutMsg is the deadline of type-through seq: past typeDeadline
// with the dashboard still here (no view version showed the session) the
// keys are dropped, and the dashboard takes keys again.
type typeTimeoutMsg struct{ seq int }

// typeDeadline is how long a type-through waits for its attach, as the
// session's sidebar does (replayWait); tests shorten it.
var typeDeadline = replayWait

// endTyping stops typing through, dropping the keys typed so far, and
// says why in the footer.
func (m *dash) endTyping(why string) {
	if n := len(m.typed); n > 0 {
		m.msg = fmt.Sprintf("typing to the coordinator %s: %d %s dropped", why, n, "key"+plural(n != 1, "s"))
	}
	m.typing, m.typed, m.typeFor = false, nil, ""
}

// typedFor are the keys typed through and their session, for the
// result: only when the session attached is the coordinator they were
// typed to, else none, with a note for the session's status bar (the
// shared view showed another, which another console attached).
func (m *dash) typedFor(attach string) ([]Key, string) {
	switch {
	case len(m.typed) == 0 || m.typeFor == "":
		return nil, ""
	case attach == m.typeFor:
		return m.typed, m.typeFor
	}
	if attach != "" && m.result.Message == "" {
		m.result.Message = "the view showed another session: what you typed to the coordinator was dropped"
	}
	return nil, ""
}

// typeSettle is how long the keys typed through must have paused before
// the dashboard hands them over and ends: keys the terminal sent but
// the dashboard's input hasn't delivered yet would end with it.
const typeSettle = 150 * time.Millisecond

// typeSettledMsg asks whether the keys typed through have paused.
type typeSettledMsg struct{}

// leave ends the dashboard for the session the result attaches: at once,
// or while keys are typed through, once they pause (typeSettle).
func (m *dash) leave() tea.Cmd {
	switch {
	case !m.typing:
		return tea.Quit
	case m.settling:
		return nil
	}
	m.settling = true
	return tea.Tick(typeSettle, func(time.Time) tea.Msg { return typeSettledMsg{} })
}

// settled ends the dashboard once the keys typed through paused, else
// looks again.
func (m *dash) settled() tea.Cmd {
	if wait := typeSettle - time.Since(m.typedAt); wait > 0 {
		return tea.Tick(wait, func(time.Time) tea.Msg { return typeSettledMsg{} })
	}
	return tea.Quit
}

// typeThroughHint is the footer's word for typing on a coordinator's row.
const typeThroughHint = "type to write to the coordinator"

// typedReady is the event that runs the keys held for a coordinator once
// its pane has the focus (client.flushTyped), in turn with the
// terminal's keys.
type typedReady struct{}

// sideTypeThrough starts typing through to the coordinator whose row the
// attached sidebar's cursor is on with key k: the keyboard goes into the
// pane, the view shows the coordinator, and k and the keys after it wait
// (holdTyped) until its pane has the focus. ok is false when k or the
// row isn't for it. c.mu held; released here when ok.
func (c *client) sideTypeThrough(k Key) bool {
	if !printableKey(k) || c.side == nil || sideFilterNow().typing {
		return false
	}
	rows, i := cursorAt(c.sideTree(), c.sideW, c.v.SideSel)
	if i < 0 {
		return false
	}
	id, _, ok := typeTarget(rows[i])
	if !ok {
		return false
	}
	st := enterStep(rows[i])
	if st.target == nil {
		return false
	}
	c.typeFor, c.typeKeys, c.typeBy = id, []Key{k}, time.Now().Add(replayWait)
	time.AfterFunc(replayWait, c.typedSoon)
	// Already showing (the cursor's preview): the pane takes them at once.
	ready := c.focus != nil && c.focus.info.ID == c.typeFor
	st.here = st.here && ready
	c.sideDo(st)
	if ready {
		go c.typedSoon()
	}
	return true
}

// printableKey is printable for a key of the outer terminal.
func printableKey(k Key) bool {
	return k.Text != "" && k.Mod&(uv.ModCtrl|uv.ModAlt|uv.ModMeta|uv.ModSuper|uv.ModHyper) == 0
}

// holdTyped keeps k for the coordinator keys are typed through to, while
// its pane hasn't the focus yet; true when it kept it (or used it). ctrl+c
// cancels the typing and the prefix ends it, both dropping the held keys;
// the prefix then starts its command as anywhere.
func (c *client) holdTyped(k Key) bool {
	if !c.lock() {
		return true
	}
	if c.typeFor == "" {
		c.mu.Unlock()
		return false
	}
	cancel, prefix := k.Mod == uv.ModCtrl && k.Code == 'c', c.prefix.match(k)
	if !cancel && !prefix {
		c.typeKeys = append(c.typeKeys, k)
		c.mu.Unlock()
		return true
	}
	why := "cancelled"
	if prefix {
		why = "ended by the prefix"
	}
	n := len(c.typeKeys)
	c.typeFor, c.typeKeys = "", nil
	c.flash = fmt.Sprintf("typing to the coordinator %s: %d key%s dropped", why, n, plural(n != 1, "s"))
	c.status()
	c.mu.Unlock()
	c.poke()
	return cancel
}

// typedFocus queues the held keys' run once the focus is on their
// coordinator's pane. c.mu held.
func (c *client) typedFocus() {
	if c.typeFor != "" && c.focus != nil && c.focus.info.ID == c.typeFor {
		go c.typedSoon()
	}
}

// typedSoon puts typedReady in the input's queue.
func (c *client) typedSoon() {
	c.mu.Lock()
	events := c.keyQueue
	c.mu.Unlock()
	if events == nil {
		return
	}
	select {
	case events <- typedReady{}:
	case <-time.After(replayWait): // the attach ended
	}
}

// flushTyped runs the held keys in the pane of their coordinator once it
// has the focus, in order; past typeBy without it they are dropped, and
// the status bar says so.
func (c *client) flushTyped() {
	if !c.lock() {
		return
	}
	if c.typeFor == "" {
		c.mu.Unlock()
		return
	}
	ready := c.focus != nil && c.focus.info.ID == c.typeFor
	if !ready && time.Now().Before(c.typeBy) {
		c.mu.Unlock()
		return
	}
	keys := c.typeKeys
	c.typeFor, c.typeKeys = "", nil
	if !ready {
		c.flash = "the coordinator's pane didn't show: what you typed was dropped"
		c.status()
		c.mu.Unlock()
		c.poke()
		return
	}
	c.kb = areaMain
	c.mu.Unlock()
	for _, k := range keys {
		c.key(k)
	}
}
