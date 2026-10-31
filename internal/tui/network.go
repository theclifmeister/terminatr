package tui

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Network popup (docs/SPEC.md §4, §16.8, §16.9): this server and
// the servers paired with it, the projects this one shares and those
// they share with it, in one tree (networktree.go), with the selected
// row's every setting in the details panel beside it (networkpanel.go).
// N opens it on the dashboard, prefix+N over a session, and a click on a
// server's header in the sidebar opens it on that server. Every act on a
// server is the human's, as tm peer's are:
//
//   - P pairs with another server and J joins one's pairing
//     (peerpair.go), as tm peer pair and join do;
//   - on a paired server, E sets its display name (tm peer name), V moves
//     its pane switch along off · view · write (tm peer panes; more asks
//     first), U asks its relay to update itself (peerrelay.go), X unpairs
//     it after asking (tm peer remove), S asks its user to share a
//     project (networkshare.go, tm peer ask-share); its relay is set
//     from the panel's relay row (tm peer relay);
//   - space on a project here shares it with every paired server, asking
//     first, or stops sharing it at once (its Shared with paired servers
//     setting).
//
// On screen the servers' link is their connection: link means a project
// link only.

// networkView is the Network popup: the paired servers as last read,
// the selection (by its row's key, so a read keeps it), the folds and
// whether the details panel has the keyboard.
type networkView struct {
	list   PeerList
	err    error
	loaded bool
	sel    string
	// at is the selection's index, where it goes when its row went.
	at     int
	top    int
	folded map[string]bool
	// ltop is the NEEDS YOU list's first row shown.
	ltop int
	// panel: the details panel has the keyboard, on its item psel.
	panel bool
	psel  int
	ptop  int
	// lines is what each body line of the last drawing shows, for the
	// mouse: a tree row (its index, -1 for none) and a panel line.
	lines []netLine
	treeW int
	// logs are the links' messages and files by "<slug>><other>", read
	// at logsAt (logOf).
	logs   map[string]linkLog
	logsAt time.Time
	// devs are the paired devices as last read (devsAt), devErr why they
	// can't be (networkphones.go).
	devs   DeviceList
	devErr error
	devsAt time.Time
}

// netLine is a body line as last drawn.
type netLine struct {
	row   int
	panel *panelLine
}

// netWide is the popup's inner width from which the details panel shows
// beside the tree.
const netWide = 96

// openNetwork opens the Network popup, on the row keyed at ("g:" this
// server, "g:<alias>" a paired one; "" the first).
func (m *dash) openNetwork(at string) {
	nv := &networkView{sel: cmp.Or(at, "g:"), folded: map[string]bool{}}
	nv.load(m)
	m.push(nv)
}

// network is the Network popup open in the stack, nil for none.
func (m *dash) network() *networkView {
	for _, o := range m.stack {
		if nv, ok := o.(*networkView); ok {
			return nv
		}
	}
	return nil
}

// load reads the paired servers again.
func (nv *networkView) load(m *dash) {
	nv.list, nv.err = m.src.Peers()
	nv.loadDevices(m)
	nv.loaded = true
}

// selected is the selected row and its index among rows; ok false with
// none to select.
func (nv *networkView) selected(m *dash, rows []netRow) (netRow, int, bool) {
	i := slices.IndexFunc(rows, func(r netRow) bool { return r.selectable() && nv.rowKey(r) == nv.sel })
	if i < 0 {
		// The row went: the nearest one from where it was.
		for d := 0; d < len(rows); d++ {
			for _, j := range []int{nv.at + d, nv.at - d} {
				if j >= 0 && j < len(rows) && rows[j].selectable() && i < 0 {
					i = j
				}
			}
		}
	}
	if i < 0 {
		return netRow{}, -1, false
	}
	nv.sel, nv.at = nv.rowKey(rows[i]), i
	return rows[i], i, true
}

// move moves the selection by d selectable rows.
func (nv *networkView) move(m *dash, d int) {
	rows := nv.rows(m)
	_, i, ok := nv.selected(m, rows)
	if !ok {
		return
	}
	step := 1
	if d < 0 {
		step = -1
	}
	for n := d * step; n > 0; n-- {
		j := i + step
		for j >= 0 && j < len(rows) && !rows[j].selectable() {
			j += step
		}
		if j < 0 || j >= len(rows) {
			break
		}
		i = j
	}
	nv.sel, nv.at = nv.rowKey(rows[i]), i
}

func (nv *networkView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	rows := nv.rows(m)
	r, _, ok := nv.selected(m, rows)
	if nv.panel {
		if cmd, done := nv.panelKey(m, r, ok, s); done {
			return cmd
		}
	}
	switch s {
	case "esc":
		m.pop()
		return nil
	case "P":
		m.pairPrompt(nv.lastRelay())
		return nil
	case "J":
		m.joinPrompt("")
		return nil
	case "D":
		return m.pairPhone("")
	case "?":
		m.push(&netKeysView{})
		return nil
	case "!":
		nv.nextNeed(m, rows)
		return nil
	case "home":
		nv.move(m, -len(rows))
		return nil
	case "end":
		nv.move(m, len(rows))
		return nil
	}
	if d, ok := scrollKeys[s]; ok {
		nv.move(m, d)
		return nil
	}
	if !ok {
		return nil
	}
	if r.kind == netLink || r.kind == netNeed {
		if cmd, done := nv.linkKey(m, r, s); done {
			return cmd
		}
	}
	if cmd, done := nv.phoneKey(m, r, s); done {
		return cmd
	}
	if r.kind == netAsked || r.kind == netShareReq {
		if cmd, done := nv.shareKey(m, r, s); done {
			return cmd
		}
	}
	switch {
	case s == "tab":
		if len(panelItems(nv.panelLines(m, r, ok, 40))) > 0 {
			nv.panel, nv.psel, nv.ptop = true, 0, 0
		} else {
			m.msg = "nothing to change in the details of this row"
		}
	case (r.kind == netServer || r.kind == netPhones) && (s == "enter" || s == "left" && !nv.folded[nv.rowKey(r)] || s == "right" && nv.folded[nv.rowKey(r)]):
		nv.folded[nv.rowKey(r)] = !nv.folded[nv.rowKey(r)]
	case r.kind == netProject && nv.hasLinks(m, r.slug) && (s == "enter" || s == "left" && !nv.folded[nv.rowKey(r)] || s == "right" && nv.folded[nv.rowKey(r)]):
		nv.folded[nv.rowKey(r)] = !nv.folded[nv.rowKey(r)]
	case s == "left" && r.listed && r.kind == netShareReq:
		// To the request in the tree, unfolding its server.
		alias := nv.list.Peers[r.peer].Alias
		nv.folded["g:"+alias] = false
		nv.sel = shareKeyOf(alias, r)
	case s == "left" && r.listed:
		// To the need in the tree, unfolding its project.
		nv.folded["g:"], nv.folded["p:"+r.slug] = false, false
		nv.sel = needKey(r.slug, r.other, r.need, r.id())
	case s == "left" && r.kind != netServer && r.kind != netPhones:
		// Up to the row's project (a link's, a need's), a device's header,
		// else its server.
		up := netServer
		switch r.kind {
		case netLink, netNeed:
			up = netProject
		case netDevice:
			up = netPhones
		}
		for j := nv.at - 1; j >= 0; j-- {
			if rows[j].kind == up {
				nv.sel, nv.at = nv.rowKey(rows[j]), j
				break
			}
		}
	case r.kind == netProject && (s == "space" || s == " "):
		return nv.toggle(m, r.slug)
	case r.kind == netProject && s == "L":
		nv.pick(m, r.slug, "")
	case r.kind == netTheirs && s == "enter":
		return nv.showTheirs(m, r)
	case r.kind == netTheirs && s == "L":
		nv.pick(m, "", nv.list.Peers[r.peer].Alias+"/"+r.slug)
	case r.kind == netServer && r.peer < 0 && s == "E":
		nv.selfName(m)
	case r.kind == netServer && r.peer >= 0:
		e := nv.list.Peers[r.peer]
		switch s {
		case "S":
			nv.askShare(m, e)
		case "E":
			nv.name(m, e)
		case "V":
			i := (slices.Index(panesLevels, panesLevel(e)) + 1) % len(panesLevels)
			return nv.setPanes(m, e, panesLevels[i])
		case "C":
			return nv.setConversations(m, e, e.ConversationsOff)
		case "N":
			i := (slices.Index(needsLevels, needsLevel(e)) + 1) % len(needsLevels)
			return nv.setNeeds(m, e, needsLevels[i])
		case "U":
			nv.relayUpdate(m, e)
		case "X":
			nv.unpair(m, e)
		}
	}
	return nil
}

// panelKey runs a key while the details panel has the keyboard; done is
// false for one it leaves to the tree's keys.
func (nv *networkView) panelKey(m *dash, r netRow, ok bool, s string) (tea.Cmd, bool) {
	items := panelItems(nv.panelLines(m, r, ok, 40))
	if len(items) == 0 {
		nv.panel = false
		return nil, false
	}
	nv.psel = min(max(nv.psel, 0), len(items)-1)
	switch s {
	case "tab", "shift+tab":
		nv.panel = false
		return nil, true
	case "up", "down":
		nv.psel = moveSel(nv.psel, scrollKeys[s], len(items))
		return nil, true
	}
	if cmd, done := nv.phonePanelKey(m, r, items[nv.psel], s); done {
		return cmd, true
	}
	switch item := items[nv.psel]; {
	case item == itemSelf && s == "enter":
		nv.selfName(m)
	case item == itemName && s == "enter":
		nv.name(m, nv.list.Peers[r.peer])
	case item == itemRelay && s == "enter":
		nv.relay(m, nv.list.Peers[r.peer])
	case item == itemPanes && (s == "left" || s == "right" || s == "enter"):
		e := nv.list.Peers[r.peer]
		i := slices.Index(panesLevels, panesLevel(e)) + map[string]int{"left": -1, "right": 1, "enter": 1}[s]
		return nv.setPanes(m, e, panesLevels[min(max(i, 0), len(panesLevels)-1)]), true
	case item == itemConv && (s == "left" || s == "right" || s == "enter"):
		e := nv.list.Peers[r.peer]
		on := map[string]bool{"left": true, "right": false, "enter": e.ConversationsOff}[s]
		return nv.setConversations(m, e, on), true
	case item == itemNeeds && (s == "left" || s == "right" || s == "enter"):
		e := nv.list.Peers[r.peer]
		i := slices.Index(needsLevels, needsLevel(e)) + map[string]int{"left": -1, "right": 1, "enter": 1}[s]
		if s == "enter" {
			i %= len(needsLevels)
		}
		return nv.setNeeds(m, e, needsLevels[min(max(i, 0), len(needsLevels)-1)]), true
	case item >= itemPages && item-itemPages < len(sharedSlugs(m)) && (s == "left" || s == "right" || s == "enter" || s == "space" || s == " "):
		e, slug := nv.list.Peers[r.peer], sharedSlugs(m)[item-itemPages]
		on := map[string]bool{"left": false, "right": true}[s]
		if s == "enter" || s == "space" || s == " " {
			on = !slices.Contains(e.Pages, slug)
		}
		return nv.setPages(m, e, slug, on), true
	case item == itemShare && (s == "enter" || s == "space" || s == " "):
		return nv.toggle(m, r.slug), true
	case (item == itemDir || item == itemAsk || item == itemFiles) && (s == "left" || s == "right"):
		return nv.panelLinkKey(m, r, item, s), true
	case (item == itemDir || item == itemAsk || item == itemFiles) && s == "enter":
		nv.linkDialog(m, r.slug, r.other)
	case s == "left" || s == "right" || s == "enter" || s == "space" || s == " " || s == "pgup" || s == "pgdown":
	default:
		return nil, false
	}
	return nil, true
}

// title names the popup: this server and how many it is paired with.
func (nv *networkView) title() string {
	if n := len(nv.list.Peers); n > 0 {
		return fmt.Sprintf("Network · %s + %d paired", thisHost(), n)
	}
	return "Network · " + thisHost()
}

func (nv *networkView) render(m *dash) string {
	if !nv.loaded {
		nv.load(m)
	}
	iw := m.inner(viewWidth)
	wide := iw >= netWide
	rows := nv.rows(m)
	r, sel, ok := nv.selected(m, rows)
	keys := nv.rowKeys(m, r, ok)
	if nv.panel {
		keys = "↑ ↓ move · enter change · ← → switch · tab tree · esc close"
	}
	b := box{title: nv.title(), sel: -1, keys: keys}
	n := m.boxRows(b)
	var list []string
	var listOf []int
	if wide {
		list, listOf = nv.needsList(m, rows, sel, iw, n)
		n -= len(list)
	}
	treeW := iw
	if wide {
		treeW = iw * 55 / 100
	}
	nv.treeW = treeW

	// The tree's lines, and the row each draws.
	var tree []string
	var of []int
	for i, row := range rows {
		if row.listed {
			continue
		}
		for _, l := range nv.rowLines(m, row, treeW) {
			if i == sel {
				l = selRow(l, treeW)
			}
			tree = append(tree, l)
			of = append(of, i)
		}
	}
	treeH := n
	if !wide {
		treeH = max(n-3, 1)
	}
	first := slices.Index(of, sel)
	if first >= 0 {
		nv.top = min(nv.top, first)
		nv.top = max(nv.top, first-treeH+1)
	}
	nv.top = min(max(nv.top, 0), max(len(tree)-treeH, 0))

	var pl []panelLine
	if wide || nv.panel {
		pw := iw - treeW - 3
		if !wide {
			pw = iw
		}
		pl = nv.panelLines(m, r, ok, pw)
		nv.scrollPanel(pl, n)
	}

	nv.lines = nv.lines[:0]
	var body []string
	for i, l := range list {
		body = append(body, l)
		nv.lines = append(nv.lines, netLine{row: listOf[i]})
	}
	for i := range n {
		var line string
		nl := netLine{row: -1}
		switch {
		case !wide && nv.panel:
			line = nv.panelAt(pl, i, &nl)
		case i < treeH:
			line = fit("", treeW)
			if j := nv.top + i; j < len(tree) {
				line, nl.row = fit(tree[j], treeW), of[j]
				if !rows[of[j]].selectable() {
					nl.row = -1
				}
			}
			if i == 0 && nv.top > 0 {
				line = fit(line, treeW-1) + th().Faint.Render("↑")
			}
			if i == treeH-1 && nv.top+treeH < len(tree) {
				line = fit(line, treeW-1) + th().Faint.Render("↓")
			}
			if wide {
				line += th().Faint.Render(" │ ") + nv.panelAt(pl, i, &nl)
			}
		case i == treeH:
			line = th().Faint.Render(strings.Repeat("─", iw))
		default:
			line = nv.detail(m, r, iw)[i-treeH-1]
		}
		body = append(body, line)
		nv.lines = append(nv.lines, nl)
	}
	b.body = body
	b.hits = make([]int, len(body))
	for i := range b.hits {
		b.hits[i] = i
	}
	return m.popup(b)
}

// selRow is line l as the selected row, w cells wide: its words plain
// under the selection's look across the whole row, as the sidebar draws
// its highlighted row. The row's own colours go: each of their resets
// would end the highlight where it stands.
func selRow(l string, w int) string {
	return th().Sel.Render(fit(ansi.Strip(l), w))
}

// panelAt is the panel's line i as shown, scrolled and with its
// selected item marked while it has the keyboard; nl notes it.
func (nv *networkView) panelAt(pl []panelLine, i int, nl *netLine) string {
	j := nv.ptop + i
	if j >= len(pl) {
		return ""
	}
	l := pl[j]
	nl.panel = &pl[j]
	if items := panelItems(pl); nv.panel && nv.psel < len(items) && l.item == items[nv.psel] {
		return selRow(l.text, ansi.StringWidth(l.text))
	}
	return l.text
}

// scrollPanel keeps the panel's selected item in its n lines.
func (nv *networkView) scrollPanel(pl []panelLine, n int) {
	if !nv.panel {
		nv.ptop = 0
		return
	}
	items := panelItems(pl)
	if nv.psel >= len(items) {
		return
	}
	first := slices.IndexFunc(pl, func(l panelLine) bool { return l.item == items[nv.psel] })
	nv.ptop = min(nv.ptop, first)
	nv.ptop = max(nv.ptop, first-n+1)
	nv.ptop = min(max(nv.ptop, 0), max(len(pl)-n, 0))
}

// click selects the tree's row under a click (a double click is enter
// on it), or works the panel's line: a switch's word or box changes it,
// a name or relay row edits it once selected.
func (nv *networkView) click(m *dash, item, col int, double bool) tea.Cmd {
	if item < 0 || item >= len(nv.lines) {
		return nil
	}
	nl := nv.lines[item]
	rows := nv.rows(m)
	alone := nv.treeW == m.inner(viewWidth) // the panel in the tree's place
	if nl.panel == nil || !alone && col < nv.treeW+3 {
		if nl.row < 0 || nl.row >= len(rows) {
			return nil
		}
		nv.panel = false
		nv.sel, nv.at = nv.rowKey(rows[nl.row]), nl.row
		if double {
			return nv.key(m, keyMsg("enter"))
		}
		return nil
	}
	if !alone {
		col -= nv.treeW + 3
	}
	r, _, ok := nv.selected(m, rows)
	if !ok || nl.panel.item == itemNone {
		return nil
	}
	items := panelItems(nv.panelLines(m, r, ok, 40))
	was := nv.panel && nv.psel < len(items) && items[nv.psel] == nl.panel.item
	nv.panel, nv.psel = true, slices.Index(items, nl.panel.item)
	if r.kind == netDevice {
		return nv.phoneClick(m, r, nl.panel, col, was || double)
	}
	switch nl.panel.item {
	case itemShare:
		return nv.toggle(m, r.slug)
	case itemPanes:
		e := nv.list.Peers[r.peer]
		if to := panesAt(panesLevel(e), col, nl.panel.x); to != "" {
			return nv.setPanes(m, e, to)
		}
	case itemDir, itemAsk, itemFiles:
		words, cur := linkSwitch(nl.panel.item, r.link.Mine)
		if i := switchAt(words, cur, col, nl.panel.x); i >= 0 {
			return nv.panelSet(m, r, nl.panel.item, i)
		}
	case itemSelf:
		if was || double {
			nv.selfName(m)
		}
	case itemName:
		if was || double {
			nv.name(m, nv.list.Peers[r.peer])
		}
	case itemRelay:
		if was || double {
			nv.relay(m, nv.list.Peers[r.peer])
		}
	}
	return nil
}

func (nv *networkView) wheel(m *dash, d int) { nv.move(m, d) }

// showTheirs shows a paired server's project's read-only dashboard.
func (nv *networkView) showTheirs(m *dash, r netRow) tea.Cmd {
	t := Target{Project: peerFold(nv.list.Peers[r.peer].Alias, r.slug)}
	var back tea.Cmd
	if m.over != nil {
		m.dropOver()
		back = m.toDashboard()
	}
	return tea.Batch(back, m.openTarget(t))
}

// lastRelay is the relay of the server paired last (peers.json keeps
// them in that order), for P to start from.
func (nv *networkView) lastRelay() string {
	if n := len(nv.list.Peers); n > 0 {
		return nv.list.Peers[n-1].Relay
	}
	return ""
}

// reload is an action's then: the list read again.
func (nv *networkView) reload(m *dash) { nv.load(m) }

// toggle flips slug's Shared with paired servers, as its Settings tab's
// row does (asking before it shares), waiting its turn behind a save.
func (nv *networkView) toggle(m *dash, slug string) tea.Cmd {
	run := shareRow(slug).change
	if m.busy {
		m.queued = append(m.queued, run)
		return nil
	}
	return run(m)
}

// panesLevels are the pane switch's levels, in the order it shows them.
var panesLevels = []string{proto.PanesOff, proto.PanesView, proto.PanesType}

// panesLevel is e's pane switch, off when unset.
func panesLevel(e PeerEntry) string {
	if level, ok := proto.ParsePanes(e.Panes); ok {
		return level
	}
	return proto.PanesOff
}

// panesWhat says what a pane switch at level lets the server do.
func panesWhat(level string) string {
	return map[string]string{
		proto.PanesOff:  "it can't open this server's panes",
		proto.PanesView: "it can watch the coordinators and threads of the projects you share, not type",
		proto.PanesType: "it can watch and type into the coordinators and threads of the projects you share",
	}[level]
}

// setPanes sets server e's pane switch to level: asking first when it
// lets that server do more, at once when less.
func (nv *networkView) setPanes(m *dash, e PeerEntry, to string) tea.Cmd {
	from := panesLevel(e)
	if to == from {
		return nil
	}
	name := netName(e)
	set := func() tea.Cmd {
		src, alias := m.src, e.Alias
		return m.act(func() actionMsg {
			err := src.SetPeerPanes(alias, to)
			return actionMsg{err: err, msg: "panes " + proto.PanesWord(to) + " for " + name, then: nv.reload}
		})
	}
	if slices.Index(panesLevels, to) < slices.Index(panesLevels, from) {
		return set()
	}
	q := map[string]string{
		proto.PanesView: fmt.Sprintf("Let %s watch the coordinator and thread panes of the projects this server shares? It sees whatever they show, and opens their popups, read-only.", name),
		proto.PanesType: fmt.Sprintf("Let %s type into the coordinator and thread panes of the projects this server shares? Typing into an agent can run anything here; the first keys into a thread tell its coordinator.", name),
	}[to]
	m.confirmNo("Panes for "+name, q, "panes unchanged for "+name, set)
	return nil
}

// setConversations turns server e's conversations switch on or off:
// asking first when on, at once when off.
func (nv *networkView) setConversations(m *dash, e PeerEntry, on bool) tea.Cmd {
	if on == !e.ConversationsOff {
		return nil
	}
	name := netName(e)
	set := func() tea.Cmd {
		src, alias := m.src, e.Alias
		return m.act(func() actionMsg {
			err := src.SetPeerConversations(alias, on)
			return actionMsg{err: err, msg: "conversations " + convWord(on) + " for " + name, then: nv.reload}
		})
	}
	if !on {
		return set()
	}
	m.confirmNo("Conversations for "+name, fmt.Sprintf("Let %s read the conversations of the coordinators and threads of the projects this server shares, in its Mac app, while its panes are view or write? They hold everything the agents ran and read: command output and file contents.", name),
		"conversations unchanged for "+name, set)
	return nil
}

var needsLevels = []string{proto.NeedsOff, proto.NeedsView, proto.NeedsAnswer}

func needsLevel(e PeerEntry) string {
	if level, ok := proto.ParseNeeds(e.Needs); ok {
		return level
	}
	return proto.NeedsOff
}

// needsWhat says what a needs switch at level lets the server see.
func needsWhat(level string) string {
	return map[string]string{
		proto.NeedsOff:    "it sees how many needs your shared projects have, not what they ask",
		proto.NeedsView:   "it sees each need in full: questions, permission dialogs, tasks in review",
		proto.NeedsAnswer: "as view, and its user answers them from there: never a sandbox escape",
	}[level]
}

// setNeeds sets server e's needs switch to level: asking first when it
// lets that server see or do more, at once when less.
func (nv *networkView) setNeeds(m *dash, e PeerEntry, to string) tea.Cmd {
	from := needsLevel(e)
	if to == from {
		return nil
	}
	name := netName(e)
	set := func() tea.Cmd {
		src, alias := m.src, e.Alias
		return m.act(func() actionMsg {
			err := src.SetPeerNeeds(alias, to)
			return actionMsg{err: err, msg: "needs " + to + " for " + name, then: nv.reload}
		})
	}
	if slices.Index(needsLevels, to) < slices.Index(needsLevels, from) {
		return set()
	}
	q := map[string]string{
		proto.NeedsView:   fmt.Sprintf("Let %s see what the projects this server shares ask you, in full? Questions, permission dialogs (the commands they would run) and their options, tasks in review and the coordinators' questions cross to it. It answers none of them.", name),
		proto.NeedsAnswer: fmt.Sprintf("Let %s answer what the projects this server shares ask you, on its user's word? Until both run a tm that sends answers, it sees them as at view. A command outside the sandbox is never answered from there.", name),
	}[to]
	m.confirmNo("Needs for "+name, q, "needs unchanged for "+name, set)
	return nil
}

// setPages turns server e's project pages switch for slug on or off:
// asking first when on, at once when off.
func (nv *networkView) setPages(m *dash, e PeerEntry, slug string, on bool) tea.Cmd {
	if on == slices.Contains(e.Pages, slug) {
		return nil
	}
	name := netName(e)
	word := map[bool]string{true: "on", false: "off"}[on]
	set := func() tea.Cmd {
		src, alias := m.src, e.Alias
		return m.act(func() actionMsg {
			err := src.SetPeerPages(alias, slug, on)
			return actionMsg{err: err, msg: "pages " + word + " for " + name + " on " + slug, then: nv.reload}
		})
	}
	if !on {
		return set()
	}
	m.confirmNo("Pages for "+name, fmt.Sprintf("Let %s read %s's board (task titles, statuses and steps), its overview (goal and PROJECT.md) and its threads' library files? Never its memory, its tasks' notes or its repositories' paths, and nothing it does writes here.", name, slug),
		"pages unchanged for "+name, set)
	return nil
}

// convWord is the conversations switch as people read it.
func convWord(on bool) string {
	if on {
		return proto.ConversationsOn
	}
	return proto.ConversationsNot
}

// relayWords is a relay as people read it: its host for wss://, and
// "(pinned)" for one taken by its key's pin.
func relayWords(r string) string {
	if u, err := url.Parse(r); err == nil && u.Scheme == "wss" && u.Path == "" && u.Host != "" {
		if u.Fragment != "" {
			return u.Host + " (pinned)"
		}
		return u.Host
	}
	return r
}

// relay asks for server e's new relay, starting from its current one,
// and sets it as tm peer relay does.
func (nv *networkView) relay(m *dash, e PeerEntry) {
	alias, name := e.Alias, netName(e)
	m.push(&inputView{title: "Relay for " + name, lineEdit: newLine(e.Relay), cancel: "relay unchanged for " + name,
		label: "The relay " + name + "'s connection goes through (a host name, or wss://…; a relay without a domain with its #sha256= pin), when it moved or has a new key. Set the same on " + name + " for this server.",
		submit: func(s string) tea.Cmd {
			src := m.src
			return m.act(func() actionMsg {
				warn, err := src.SetPeerRelay(alias, s)
				msg := "relay for " + name + " set; set it on " + name + " too"
				if warn != "" {
					msg += " · " + warn
				}
				return actionMsg{err: err, msg: msg, then: nv.reload}
			})
		}})
}

// name asks for server e's display name, the alias as the faint
// placeholder, and sets it as tm peer name does: empty clears it.
func (nv *networkView) name(m *dash, e PeerEntry) {
	alias := e.Alias
	m.push(&inputView{title: "Name for " + alias, placeholder: alias, keep: true, max: peer.MaxAlias, cancel: "name unchanged for " + alias,
		label: "What this server calls " + alias + " (a-z, 0-9 and -), shown in its alias's place; either names it in commands, as <name>/<project>. Empty shows the alias again:",
		submit: func(s string) tea.Cmd {
			s = strings.TrimSpace(s)
			if s == e.Name {
				m.msg = "name unchanged for " + alias
				return nil
			}
			src := m.src
			return m.act(func() actionMsg {
				err := src.SetPeerName(alias, s)
				msg := alias + " is called " + s + " here"
				if s == "" {
					msg = alias + " shows by its alias again"
				}
				return actionMsg{err: err, msg: msg, then: nv.reload}
			})
		}})
}

// selfName asks for this server's own name, the host label as the faint
// placeholder, and sets it as tm network name does: empty clears it.
func (nv *networkView) selfName(m *dash) {
	was := peer.OwnName()
	m.push(&inputView{title: "Name for this server", lineEdit: newLine(was), placeholder: peer.HostName(), keep: true, max: peer.MaxAlias, cancel: "name unchanged for this server",
		label: "What this server calls itself (a-z, 0-9 and -): its header here, and the paired servers show it in its alias's place, once they run a tm that knows it. Empty is the host name again:",
		submit: func(s string) tea.Cmd {
			s = strings.TrimSpace(s)
			if s == was {
				m.msg = "name unchanged for this server"
				return nil
			}
			src := m.src
			return m.act(func() actionMsg {
				err := src.SetSelfName(s)
				msg := "this server is called " + s + "; the paired servers hear of it"
				if s == "" {
					msg = "this server is called by its host name again"
				}
				return actionMsg{err: err, msg: msg, then: nv.reload}
			})
		}})
}

// unpair asks, then unpins server e and has the server close its
// connection, as tm peer remove does; the list is read again after.
func (nv *networkView) unpair(m *dash, e PeerEntry) {
	alias, name := e.Alias, netName(e)
	q := fmt.Sprintf("Unpair %s? Its connection closes at once, its key is forgotten here and its projects leave the sidebar; it refuses this server until the two pair again. Unpair this server on %s too (tm peer remove there).", name, name)
	m.confirmNo("Unpair "+name, q, name+" kept", func() tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			err := src.RemovePeer(alias)
			return actionMsg{err: err, msg: "unpaired " + name + "; its connection closes. Unpair this server on " + name + " too.", then: nv.reload}
		})
	})
}

// netKeysView is the Network popup's keys (?).
type netKeysView struct{}

// netKeys are the Network popup's keys, as its ? lists them.
var netKeys = []keyHelp{
	{"↑ ↓ ← →", "move; fold a server or project, ← up to its parent"},
	{"enter", "fold; on a link its dialog, on theirs its dashboard, on a ⚑ read it"},
	{"space L", "share a project of yours (asking first) / link it with another"},
	{"K F R M X", "on a link: ask first, files, resume or ask back, write, unlink"},
	{"A X", "on a ⚑: send, accept or choose / drop or decline"},
	{"P J", "pair with another server / join the code one shows"},
	{"E", "this server: its own name, which the paired servers show"},
	{"S E V C N", "a paired server: ask to share, name, panes, conversations, needs"},
	{"U X", "a paired server: relay update, unpair"},
	{"D", "pair a device: a QR code to scan with the terminatr app"},
	{"V E X", "a device: its level (view, answer, write), name, remove"},
	{"tab", "the keyboard to the details: enter edits, ← → switch"},
	{"! esc", "the next ⚑ that needs you / close"},
}

func (kv *netKeysView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	if s := k.String(); s == "esc" || s == "?" {
		m.pop()
	}
	return nil
}

func (kv *netKeysView) render(m *dash) string {
	w := m.inner(viewWidth)
	var body []string
	for _, k := range netKeys {
		body = append(body, th().Accent.Render(fit(k.keys, 10))+" "+fit(k.help, max(w-11, 1)))
	}
	return m.popup(box{title: "Network keys", body: body, sel: -1, keys: "esc close"})
}
