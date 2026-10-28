package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/theclifmeister/terminatr/internal/device"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Network popup's phones (docs/SPEC.md §16.15): after the paired
// servers, the phones paired with this server, each with its connection
// (or when it was last seen) and its level, as tm device list and the
// Mac app's Settings › Devices show them. Every act on them is the
// human's, as tm device's are:
//
//   - D pairs a phone (devicepair.go): a QR code to scan with the
//     terminatr app, as tm device pair shows it;
//   - on a phone, V picks its level (view · answer · write; above view
//     it needs its write key), E renames it, X removes it after asking;
//     the details panel's level switch moves it with ← →, asking first
//     when it lets the phone do more.
//
//	"▾ phones · 2 paired                                D pairs a phone"
//	"   ◉ Clif's iPhone · connected                        level answer"
//	"   ○ iPad · last seen 2h ago                            level view"

// devicesEvery is how often the open popup reads the phones again: their
// links change on their own.
const devicesEvery = 3 * time.Second

// devicesMsg is the phones as read in the background.
type devicesMsg struct {
	list DeviceList
	err  error
}

// loadDevices reads the phones again.
func (nv *networkView) loadDevices(m *dash) {
	nv.devs, nv.devErr = m.src.Devices()
	nv.devsAt = time.Now()
}

// pollDevices reads the phones again in the background, every
// devicesEvery while the popup is open.
func (m *dash) pollDevices() tea.Cmd {
	nv := m.network()
	if nv == nil || time.Since(nv.devsAt) < devicesEvery {
		return nil
	}
	nv.devsAt = time.Now()
	src := m.src
	return func() tea.Msg {
		l, err := src.Devices()
		return devicesMsg{list: l, err: err}
	}
}

// tookDevices takes the phones as read.
func (m *dash) tookDevices(msg devicesMsg) {
	if nv := m.network(); nv != nil {
		nv.devs, nv.devErr = msg.list, msg.err
	}
}

// phoneRows are the tree's phones: their header, then each phone.
func (nv *networkView) phoneRows() []netRow {
	out := []netRow{{kind: netGap}, {kind: netPhones, peer: -1}}
	switch {
	case nv.folded["d:"]:
		return out
	case nv.devErr != nil:
		return append(out, netRow{kind: netNote, peer: -1, phone: true, bad: true, text: "The phones can't be read: " + oneLine(nv.devErr.Error())})
	case len(nv.devs.Devices) == 0:
		return append(out, netRow{kind: netNote, peer: -1, phone: true, text: "No phone yet. D pairs one: a QR code to scan with the terminatr app on the iPhone."})
	}
	for i := range nv.devs.Devices {
		out = append(out, netRow{kind: netDevice, peer: -1, dev: i})
	}
	return out
}

// device is phone row r's device.
func (nv *networkView) device(r netRow) DeviceEntry { return nv.devs.Devices[r.dev] }

// levelWord is a level as rows say it.
func levelWord(level string) string {
	if level == device.LevelView {
		return "view only"
	}
	return level
}

// levelWhat says what a level lets a phone do, as tm device access and
// the Mac app say it.
func levelWhat(level string) string {
	switch level {
	case device.LevelAnswer:
		return "It reads, answers questions and approvals (never one that runs a command outside the sandbox), and accepts or sends back tasks, each act signed by its write key."
	case device.LevelWrite:
		return "It does what the Mac app does: prompts, new tasks, delegating, starting a coordinator, pane keys and the rest, each act signed by its write key; never the guard's answers or the devices, and never an approval that runs a command outside the sandbox."
	}
	return "It reads projects, sessions, conversations and panes, and acts on nothing."
}

// levelLimit says why phone e runs below its level, or can't be raised;
// "" for neither.
func levelLimit(e DeviceEntry) string {
	switch {
	case !e.WriteKey:
		return "No write key: it can only view. Pair it again from the terminatr app to let it answer or write."
	case e.Effective != e.Level:
		return levelWord(e.Level) + " isn't in this release: it runs at " + levelWord(e.Effective) + "."
	}
	return ""
}

// phoneConn is phone e's connection glyph, look and words: connected,
// waiting for it or when it was last seen, offline and why.
func (nv *networkView) phoneConn(e DeviceEntry) (string, lipgloss.Style, string) {
	seen := ""
	if !e.LastUp.IsZero() {
		if d := max(sideClock().Sub(e.LastUp), 0); d < time.Minute {
			seen = "last seen just now"
		} else {
			seen = "last seen " + age(d) + " ago"
		}
	}
	l := e.Link
	if l == nil {
		words := "no link yet"
		if !nv.devs.Running {
			words = "the server isn't running"
		}
		if seen != "" {
			words += " · " + seen
		}
		g, st := linkLook(&peerLink{state: proto.LinkOffline})
		return g, st, words
	}
	g, st := linkLook(&peerLink{state: l.State, reason: l.Reason})
	switch {
	case l.State == proto.LinkUp:
		return g, st, "connected"
	case l.State == proto.LinkConnecting:
		return g, st, "connecting"
	case l.Reason == proto.LinkReasonPeer && seen != "":
		return g, st, seen
	case l.Reason == proto.LinkReasonPeer:
		return g, st, "waiting for it"
	}
	words := "offline"
	if why := linkReasonWords[l.Reason]; why != "" {
		words += " · " + why
	}
	if seen != "" {
		words += " · " + seen
	}
	return g, st, words
}

// phoneLines are phone row r's line w cells wide: the header or a phone.
func (nv *networkView) phoneLine(r netRow, w int) string {
	if r.kind == netPhones {
		fold := ic().unfold
		if nv.folded["d:"] {
			fold = ic().fold
		}
		left := th().Faint.Render(fold) + " " + th().Head.Render("phones")
		if n := len(nv.devs.Devices); n > 0 {
			left += th().Faint.Render(fmt.Sprintf(" · %d paired", n))
		}
		right := ""
		if w >= 50 {
			right = th().Faint.Render("D pairs a phone")
		}
		return spread(left, right, w)
	}
	e := nv.device(r)
	g, st, words := nv.phoneConn(e)
	left := "   " + st.Render(g) + " " + th().Head.Render(fit(e.Name, min(len([]rune(e.Name)), device.MaxName)))
	if words == "connected" {
		left += " · " + words
	} else {
		left += th().Faint.Render(" · " + words)
	}
	right := ""
	if w >= 50 {
		right = th().Faint.Render("level ") + th().Accent.Render(e.Level)
		if e.Effective != e.Level {
			right += th().Faint.Render(" (runs at " + e.Effective + ")")
		}
	}
	return spread(left, right, w)
}

// phoneDetail is the two lines w cells wide under the tree about phone
// row r, when the popup has no room for the details panel.
func (nv *networkView) phoneDetail(r netRow, w int) [2]string {
	if r.kind == netPhones {
		return [2]string{counted(len(nv.devs.Devices), "paired phone"),
			th().Faint.Render("D pairs a phone: a QR code to scan with the terminatr app")}
	}
	e := nv.device(r)
	_, _, words := nv.phoneConn(e)
	l2 := levelLimit(e)
	if l2 == "" {
		l2 = levelWhat(e.Effective)
	}
	return [2]string{joinFit([]string{e.Name, words, "device id " + e.Short, "relay " + relayWords(e.Relay)}, w), th().Faint.Render(fit(l2, w))}
}

// phonePanel is the details panel's lines w cells wide about phone row r.
func (nv *networkView) phonePanel(r netRow, w int) []panelLine {
	var out []panelLine
	add := func(item int, lines ...string) {
		for _, l := range lines {
			out = append(out, panelLine{text: l, item: item})
		}
	}
	row := func(item int, label, value string) {
		for i, l := range wrapLines(value, max(w-panelLabel, 8)) {
			if i > 0 {
				label = ""
			}
			add(item, fit(th().Faint.Render(label), panelLabel)+l)
		}
	}
	under := func(text string) {
		for _, l := range wrapLines(text, max(w-panelLabel, 8)) {
			add(itemNone, strings.Repeat(" ", panelLabel)+th().Faint.Render(l))
		}
	}
	rule := th().Faint.Render(strings.Repeat("─", max(w, 1)))
	if r.kind == netPhones {
		add(itemNone, th().Head.Render("phones")+th().Faint.Render(" · paired with this server"), rule)
		row(itemNone, "paired", counted(len(nv.devs.Devices), "phone"))
		relay := th().Faint.Render("none yet: D asks for one")
		if nv.devs.Relay != "" {
			relay = relayWords(nv.devs.Relay)
		}
		row(itemNone, "relay", relay)
		add(itemNone, "")
		add(itemNone, faintLines("D pairs a phone: it shows a QR code to scan with the terminatr app, which works once, for 10 minutes; you confirm the phone here before it is added, at view. A phone reads what this server shows, through the relay, and answers or writes only at the level you give it.", w)...)
		return out
	}
	e := nv.device(r)
	add(itemNone, th().Head.Render(e.Name)+th().Faint.Render(" · phone"), rule)
	row(itemDevName, "name (E)", e.Name)
	g, st, words := nv.phoneConn(e)
	row(itemNone, "connection", st.Render(g)+" "+words)
	if e.Link != nil && e.Link.Error != "" && e.Link.State != proto.LinkUp {
		under(oneLine(e.Link.Error))
	}
	row(itemNone, "device id", e.Short)
	row(itemNone, "relay", relayWords(e.Relay))
	add(itemNone, "")
	out = append(out, panelLine{text: fit(th().Faint.Render("level (V)"), panelLabel) + levelChoices(e.Level, true), item: itemLevel, x: panelLabel})
	if lim := levelLimit(e); lim != "" {
		under(lim)
	}
	under(levelWhat(e.Effective))
	add(itemNone, "")
	add(itemNone, faintLines("X removes it: its link closes and its key is no longer taken. On the phone, Forget this server deletes its keys.", w)...)
	return out
}

// levelChoices is the level switch's words, view · answer · write, the
// current one in brackets (and, styled, in the accent).
func levelChoices(level string, styled bool) string {
	var parts []string
	for _, l := range device.Levels {
		word := l
		switch {
		case l == level && styled:
			word = th().Accent.Render("[" + word + "]")
		case l == level:
			word = "[" + word + "]"
		case styled:
			word = th().Faint.Render(word)
		}
		parts = append(parts, word)
	}
	sep := " · "
	if styled {
		sep = th().Faint.Render(sep)
	}
	return strings.Join(parts, sep)
}

// phoneKey runs a key on phone row r; done is false for one it leaves
// to the tree's keys.
func (nv *networkView) phoneKey(m *dash, r netRow, s string) (tea.Cmd, bool) {
	if r.kind != netDevice {
		return nil, false
	}
	e := nv.device(r)
	switch s {
	case "V":
		m.push(&levelPickView{dev: e, sel: max(slices.Index(device.Levels, e.Level), 0)})
	case "E":
		nv.renamePhone(m, e)
	case "X":
		nv.removePhone(m, e)
	default:
		return nil, false
	}
	return nil, true
}

// phonePanelKey runs a key on the panel's phone item; done is false for
// one it leaves to the panel.
func (nv *networkView) phonePanelKey(m *dash, r netRow, item int, s string) (tea.Cmd, bool) {
	if r.kind != netDevice {
		return nil, false
	}
	e := nv.device(r)
	switch {
	case item == itemDevName && s == "enter":
		nv.renamePhone(m, e)
	case item == itemLevel && s == "enter":
		m.push(&levelPickView{dev: e, sel: max(slices.Index(device.Levels, e.Level), 0)})
	case item == itemLevel && (s == "left" || s == "right"):
		i := slices.Index(device.Levels, e.Level) + map[string]int{"left": -1, "right": 1}[s]
		return nv.setLevel(m, e, device.Levels[min(max(i, 0), len(device.Levels)-1)], true), true
	default:
		return nil, false
	}
	return nil, true
}

// phoneClick works a click on the panel's phone item at col.
func (nv *networkView) phoneClick(m *dash, r netRow, nl *panelLine, col int, again bool) tea.Cmd {
	e := nv.device(r)
	switch nl.item {
	case itemLevel:
		cur := slices.Index(device.Levels, e.Level)
		if i := switchAt(device.Levels, cur, col, nl.x); i >= 0 {
			return nv.setLevel(m, e, device.Levels[i], true)
		}
	case itemDevName:
		if again {
			nv.renamePhone(m, e)
		}
	}
	return nil
}

// setLevel sets phone e's level to to, as tm device access does; with
// ask, a level that lets it do more asks first. Above view it needs a
// write key.
func (nv *networkView) setLevel(m *dash, e DeviceEntry, to string, ask bool) tea.Cmd {
	if to == e.Level {
		return nil
	}
	if to != device.LevelView && !e.WriteKey {
		m.msg = e.Name + " has no write key: it can only view; pair it again from the terminatr app"
		return nil
	}
	set := func() tea.Cmd {
		src, id, name := m.src, e.ID, e.Name
		return m.act(func() actionMsg {
			err := src.SetDeviceLevel(id, to)
			return actionMsg{err: err, msg: name + " is " + levelWord(to) + " now", then: nv.reload}
		})
	}
	if !ask || slices.Index(device.Levels, to) < slices.Index(device.Levels, e.Level) {
		return set()
	}
	m.confirmNo("Level for "+e.Name, fmt.Sprintf("Let %s %s? %s", e.Name, map[string]string{
		device.LevelAnswer: "answer", device.LevelWrite: "write"}[to], levelWhat(to)), "level unchanged for "+e.Name, set)
	return nil
}

// renamePhone asks for phone e's new name and renames it, as tm device
// name does.
func (nv *networkView) renamePhone(m *dash, e DeviceEntry) {
	id, name := e.ID, e.Name
	m.push(&inputView{title: "Name for " + name, lineEdit: newLine(name), max: device.MaxName, cancel: "name unchanged for " + name,
		label: "What this server calls the phone: the journal names its acts so, human (iphone: <name>). Its link connects again under the new name:",
		submit: func(s string) tea.Cmd {
			if s == name {
				m.msg = "name unchanged for " + name
				return nil
			}
			if err := device.CheckName(s); err != nil {
				m.msg = err.Error()
				return nil
			}
			src := m.src
			return m.act(func() actionMsg {
				err := src.RenameDevice(id, s)
				return actionMsg{err: err, msg: name + " is called " + s + " now", then: nv.reload}
			})
		}})
}

// removePhone asks, then removes phone e, as tm device remove does.
func (nv *networkView) removePhone(m *dash, e DeviceEntry) {
	id, name := e.ID, e.Name
	q := fmt.Sprintf("Remove %s? Its link closes now and its key is no longer taken; to use it again, pair it again. On the phone, Forget this server deletes its keys.", name)
	m.confirmNo("Remove "+name, q, name+" kept", func() tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			err := src.RemoveDevice(id)
			return actionMsg{err: err, msg: "removed " + name + "; its link closes", then: nv.reload}
		})
	})
}

// levelPickView picks a phone's level: each level with what it lets the
// phone do; enter sets the one selected.
type levelPickView struct {
	dev DeviceEntry
	sel int
}

func (lv *levelPickView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); s {
	case "esc":
		m.pop()
		m.msg = "level unchanged for " + lv.dev.Name
	case "up", "down":
		lv.sel = moveSel(lv.sel, scrollKeys[s], len(device.Levels))
	case "enter":
		to := device.Levels[lv.sel]
		if to != device.LevelView && !lv.dev.WriteKey {
			m.msg = lv.dev.Name + " has no write key: it can only view; pair it again from the terminatr app"
			return nil
		}
		m.pop()
		if to == lv.dev.Level {
			m.msg = "level unchanged for " + lv.dev.Name
			return nil
		}
		if nv := m.network(); nv != nil {
			return nv.setLevel(m, lv.dev, to, false)
		}
	}
	return nil
}

func (lv *levelPickView) render(m *dash) string {
	w := m.inner(viewWidth)
	var body []string
	sel := 0
	for i, l := range device.Levels {
		mark := "( )"
		if l == lv.dev.Level {
			mark = "(•)"
		}
		what := levelWhat(l)
		if l != device.LevelView && !lv.dev.WriteKey {
			what = "Needs a write key, which this phone's app didn't send: pair it again."
		}
		if i == lv.sel {
			sel = len(body)
			body = append(body, selRow("› "+mark+" "+l, w))
		} else {
			body = append(body, "  "+th().Accent.Render(mark)+" "+l)
		}
		for _, x := range wrapLines(what, max(w-6, 8)) {
			body = append(body, "      "+th().Faint.Render(x))
		}
	}
	return m.popup(box{title: "Level for " + lv.dev.Name, body: body, sel: sel, keys: "↑ ↓ move · enter set · esc cancel"})
}
