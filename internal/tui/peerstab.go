package tui

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Peers tab of the , popup (docs/SPEC.md §4, §16.8, §16.9), laid
// out as the other settings tabs are, in two sections the cursor moves
// over as one list:
//
//   - SHARED PROJECTS: every active project on this server, with its
//     Shared with paired servers setting (shareRow, the same as its own
//     Settings tab's) as a box space or a click toggles; sharing asks
//     first. It shows with nothing paired too.
//   - PAIRED SERVERS: this server's id, then per server as tm peer list
//     has them (Source.Peers) its link line (glyph, alias, link, tm) and
//     two setting rows: panes, off · view · write side by side with the
//     current one marked, which ← → or a click on a word changes as tm
//     peer panes does (asking before it lets the server do more), and
//     its relay, which e (or enter or a click on it) changes as tm peer
//     relay does (the relay moved, or has a new key), with the relay's
//     tm once known; u asks that relay to update itself (peerrelay.go).
//
// x removes the selected server, asking first, as tm peer remove does;
// n pairs with another server and j joins one's pairing (peerpair.go),
// as tm peer pair and join do.
//
//	"SHARED PROJECTS"
//	"[x] acme-api"
//	"[ ] blog"
//	""
//	"PAIRED SERVERS · this server 4F2A-91C3"
//	"◉ mini  linked · 38 ms · tm 0.27.0 · shares 2 projects"
//	"    panes  off · view · [write]"
//	"           it can watch and type into …"
//	"    relay  relay.example.net · v0.27.0 · update available (u)"

// peersTab is the Peers tab: the list as last read, and the selection,
// an index into its items.
type peersTab struct {
	list   PeerList
	err    error
	loaded bool
	sel    int
}

// peerItem is a row the cursor stops on: a project's share box, or a
// paired server's panes or relay row.
type peerItem struct {
	kind int
	slug string // itemShare
	peer int    // itemPanes, itemRelay: the index in list.Peers
}

// The kinds of peerItem.
const (
	itemShare = iota
	itemPanes
	itemRelay
)

// peersKeys are the Peers tab's action row.
const peersKeys = "space toggle · ← → change · n pair · j join · e relay · u update relay · x remove · esc close"

// relayHint ends the empty tab: where a relay comes from.
const relayHint = "No relay yet? On a Linux server with a domain: sudo tm relay install --domain NAME; without one, on an address both servers reach: sudo tm relay install --ip (see the README)."

// panesLevels are the pane switch's levels, in the order the row shows.
var panesLevels = []string{proto.PanesOff, proto.PanesView, proto.PanesType}

// load reads the paired servers again.
func (pt *peersTab) load(m *dash) {
	pt.list, pt.err = m.src.Peers()
	pt.loaded = true
	pt.sel = moveSel(pt.sel, 0, len(pt.items(m)))
}

// shareable are the projects the tab offers to share: the active ones
// (the dashboard's data has no archived one).
func shareable(m *dash) []string {
	var out []string
	for _, p := range m.data.Projects {
		if p.Safety == nil || p.Safety.Active {
			out = append(out, p.Slug)
		}
	}
	return out
}

// items are the rows the cursor stops on, in order.
func (pt *peersTab) items(m *dash) []peerItem {
	var out []peerItem
	for _, slug := range shareable(m) {
		out = append(out, peerItem{kind: itemShare, slug: slug})
	}
	if pt.err == nil {
		for i := range pt.list.Peers {
			out = append(out, peerItem{kind: itemPanes, peer: i}, peerItem{kind: itemRelay, peer: i})
		}
	}
	return out
}

// selected is the item under the cursor; ok false with none.
func (pt *peersTab) selected(m *dash) (peerItem, bool) {
	items := pt.items(m)
	if pt.sel < 0 || pt.sel >= len(items) {
		return peerItem{}, false
	}
	return items[pt.sel], true
}

// panesIndent is how far a server's setting rows sit in; panesLabel
// their labels' width.
const (
	panesIndent = 4
	panesLabel  = 5
)

// aliasMax is the most of a server's alias its link line shows.
const aliasMax = 24

// lines draws the tab w cells wide: its lines, the selected item's
// last line, and what a click on each line picks (an item's index).
func (pt *peersTab) lines(m *dash, w int) (out []string, sel int, hits []int) {
	sel = -1
	if !pt.loaded {
		pt.load(m)
	}
	pt.sel = moveSel(pt.sel, 0, len(pt.items(m)))
	add := func(hit int, lines ...string) {
		for _, l := range lines {
			out = append(out, l)
			hits = append(hits, hit)
		}
	}
	item := 0 // the next item's index
	row := func(text string, styled string) {
		if item == pt.sel {
			sel = len(out)
			add(item, th().Sel.Render(fit(text, w)))
		} else {
			add(item, styled)
		}
		item++
	}

	add(noHit, th().Head.Render("SHARED PROJECTS"))
	slugs := shareable(m)
	if len(slugs) == 0 {
		add(noHit, faintLines("No active project on this server to share.", w)...)
	}
	for _, slug := range slugs {
		box := "[ ]"
		if p := m.projectData(slug); p != nil && p.Safety != nil && p.Safety.Share {
			box = "[x]"
		}
		row(box+" "+slug, th().Accent.Render(box)+" "+fit(slug, max(w-4, 1)))
	}
	add(noHit, faintLines("A shared project shows, read-only, in the sidebar of every paired server: what waits for you, the inbox, threads, tasks on deck and its sessions' states; never its files. Space toggles it, as Shared with paired servers in the project's own Settings tab; sharing asks first.", w)...)
	add(noHit, "")

	l := pt.list
	head := "PAIRED SERVERS"
	if pt.err == nil && l.ID != "" {
		head += th().Faint.Render(" · this server " + l.Short)
	}
	add(noHit, th().Head.Render(head))
	switch {
	case pt.err != nil:
		add(noHit, th().Bad.Render(fit("The paired servers can't be read: "+oneLine(pt.err.Error()), w)))
		return out, sel, hits
	case l.ID == "":
		add(noHit, faintLines("This server isn't paired with another. Press n to pair: it shows a code to pass on, and j on the other server takes it (or tm peer pair and tm peer join in a terminal); both of you then compare six digits.", w)...)
		add(noHit, faintLines(relayHint, w)...)
		return out, sel, hits
	}
	if l.LinkErr != "" {
		add(noHit, th().Bad.Render(fit("The server can't link: "+oneLine(l.LinkErr), w)))
	}
	if len(l.Peers) == 0 {
		add(noHit, faintLines("No paired servers. Press n to pair with one, or j to join the code another shows.", w)...)
		add(noHit, faintLines(relayHint, w)...)
		return out, sel, hits
	}
	pad := strings.Repeat(" ", panesIndent)
	for i, e := range l.Peers {
		if i > 0 {
			add(noHit, "")
		}
		g, gst, words := peerLinkWords(m, e, l)
		about := peerAbout(m, e)
		// A long host name is cut, so the link's words still show.
		line := gst.Render(g) + " " + th().Head.Render(fit(e.Alias, min(ansi.StringWidth(e.Alias), aliasMax))) + "  " + words
		if about != "" {
			line += th().Faint.Render(" · " + about)
		}
		// A click on the link line selects the server's panes row.
		add(item, fitANSI(line, w))
		level := panesLevel(e)
		row(pad+fit("panes", panesLabel)+"  "+panesChoices(level, false),
			pad+th().Head.Render(fit("panes", panesLabel))+"  "+panesChoices(level, true))
		for _, d := range faintLines(panesWhat(level), max(w-panesIndent-panesLabel-2, 8)) {
			add(item-1, strings.Repeat(" ", panesIndent+panesLabel+2)+d)
		}
		if item-1 == pt.sel {
			sel = len(out) - 1
		}
		relay := relayWords(e.Relay)
		if v := relayVersionWords(m, e); v != "" {
			relay += " · " + strings.TrimPrefix(v, "relay ")
		}
		row(pad+fit("relay", panesLabel)+"  "+relay,
			pad+th().Head.Render(fit("relay", panesLabel))+"  "+fit(relay, max(w-panesIndent-panesLabel-2, 1)))
	}
	add(noHit, "")
	add(noHit, faintLines("panes sets what a server may do with this server's panes, the coordinators and threads of the projects you share: nothing, watch them, or watch and type into them. ← → or a click on a word changes it; more asks first, less applies at once.", w)...)
	add(noHit, faintLines("e gives the selected server another relay, as tm peer relay does: when the relay moved or was set up again with a new key (its link refuses a relay whose key changed); set it on the other server too.", w)...)
	add(noHit, faintLines("u asks the selected server's relay to update itself to the latest official release, as tm peer relay-update does: it restarts only for a newer one, and takes one request an hour from any server linked through it.", w)...)
	add(noHit, faintLines("x removes the selected server, as tm peer remove does: its link closes and its projects leave the sidebar; remove this server there too.", w)...)
	return out, sel, hits
}

// fitANSI cuts a styled line to w cells.
func fitANSI(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, max(w-1, 0), "…")
}

// panesChoices is the panes row's words, off · view · write, the
// current one in brackets (and, styled, in the accent).
func panesChoices(level string, styled bool) string {
	var parts []string
	for _, l := range panesLevels {
		word := proto.PanesWord(l)
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

// panesAt is the level whose word a click at col of a panes row is on,
// "" for none.
func panesAt(level string, col int) string {
	x := panesIndent + panesLabel + 2
	for _, l := range panesLevels {
		n := ansi.StringWidth(proto.PanesWord(l))
		if l == level {
			n += 2
		}
		if col >= x && col < x+n {
			return l
		}
		x += n + 3
	}
	return ""
}

// peerLinkWords are a paired server's link glyph, its look and its
// words: from the mirror while the dashboard polls it (it follows the
// link as it goes), else as the list read it.
func peerLinkWords(m *dash, e PeerEntry, l PeerList) (string, lipgloss.Style, string) {
	var pl *peerLink
	for _, pm := range m.data.Peers {
		if pm.ID == e.ID {
			pl = &peerLink{state: pm.State, reason: pm.Reason, since: pm.Since, rtt: pm.RTT}
		}
	}
	if pl == nil && e.Link != nil {
		pl = &peerLink{state: e.Link.State, reason: e.Link.Reason, since: e.Link.Since, rtt: e.Link.RTT}
	}
	if pl == nil {
		words := "no link yet"
		switch {
		case !l.Running:
			words = "the server isn't running"
		case l.LinkErr != "":
			words = "not linked"
		}
		g, st := linkLook(&peerLink{state: proto.LinkOffline})
		return g, st, th().Faint.Render(words)
	}
	g, st := linkLook(pl)
	words := linkWords(pl, sideClock())
	if pl.state == proto.LinkUp {
		words = "linked · " + words
		if pl.rtt <= 0 {
			words = "linked"
		}
	}
	return g, st, words
}

// peerAbout ends a paired server's link line: its tm, what it shares
// with this server and the link's last error, as far as known.
func peerAbout(m *dash, e PeerEntry) string {
	var bits []string
	if e.Link != nil && e.Link.Version != "" {
		bits = append(bits, "tm "+strings.TrimPrefix(e.Link.Version, "v"))
	}
	for _, pm := range m.data.Peers {
		if pm.ID == e.ID && pm.Watch {
			bits = append(bits, "shares "+counted(len(pm.Projects), "project"))
		}
	}
	if e.Link != nil && e.Link.Error != "" && e.Link.State != proto.LinkUp {
		bits = append(bits, oneLine(e.Link.Error))
	}
	return strings.Join(bits, " · ")
}

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
func (pt *peersTab) setPanes(m *dash, e PeerEntry, to string) tea.Cmd {
	from := panesLevel(e)
	if to == from {
		return nil
	}
	set := func() tea.Cmd {
		src, alias := m.src, e.Alias
		return m.act(func() actionMsg {
			err := src.SetPeerPanes(alias, to)
			return actionMsg{err: err, msg: "panes " + proto.PanesWord(to) + " for " + alias, then: func(m *dash) { pt.load(m) }}
		})
	}
	if slices.Index(panesLevels, to) < slices.Index(panesLevels, from) {
		return set()
	}
	q := map[string]string{
		proto.PanesView: fmt.Sprintf("Let %s watch the coordinator and thread panes of the projects this server shares? It sees whatever they show.", e.Alias),
		proto.PanesType: fmt.Sprintf("Let %s type into the coordinator and thread panes of the projects this server shares? Typing into an agent can run anything here; the first keys into a thread tell its coordinator.", e.Alias),
	}[to]
	m.confirmNo("Panes for "+e.Alias, q, "panes unchanged for "+e.Alias, set)
	return nil
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

// key runs a key on the tab; ok is false for one it doesn't take (← →
// off a panes row switch tabs).
func (pt *peersTab) key(m *dash, k tea.KeyPressMsg) (tea.Cmd, bool) {
	s := k.String()
	switch s {
	case "n":
		m.pairPrompt(pt.lastRelay())
		return nil, true
	case "j":
		m.joinPrompt("")
		return nil, true
	}
	if d, ok := scrollKeys[s]; ok {
		pt.sel = moveSel(pt.sel, d, len(pt.items(m)))
		return nil, true
	}
	it, ok := pt.selected(m)
	switch {
	case !ok:
		return nil, s == "x" || s == "e" || s == "u" || s == "space" || s == " " || s == "enter"
	case it.kind == itemShare && (s == "space" || s == " " || s == "enter"):
		return pt.toggle(m, it.slug), true
	case it.kind == itemPanes && (s == "left" || s == "right"):
		e := pt.list.Peers[it.peer]
		i := slices.Index(panesLevels, panesLevel(e)) + map[string]int{"left": -1, "right": 1}[s]
		return pt.setPanes(m, e, panesLevels[min(max(i, 0), len(panesLevels)-1)]), true
	case it.kind != itemShare && s == "x":
		pt.remove(m, pt.list.Peers[it.peer].Alias)
		return nil, true
	case it.kind != itemShare && s == "e", it.kind == itemRelay && s == "enter":
		pt.relay(m, pt.list.Peers[it.peer])
		return nil, true
	case it.kind != itemShare && s == "u":
		pt.relayUpdate(m, pt.list.Peers[it.peer])
		return nil, true
	}
	return nil, s == "x" || s == "e" || s == "u" || s == "space" || s == " " || s == "enter"
}

// toggle flips slug's Shared with paired servers, as its Settings tab's
// row does (asking before it shares), waiting its turn behind a save.
func (pt *peersTab) toggle(m *dash, slug string) tea.Cmd {
	run := shareRow(slug).change
	if m.busy {
		m.queued = append(m.queued, run)
		return nil
	}
	return run(m)
}

// relay asks for the selected server's new relay, starting from its
// current one, and sets it as tm peer relay does.
func (pt *peersTab) relay(m *dash, e PeerEntry) {
	alias := e.Alias
	m.push(&inputView{title: "Relay for " + alias, text: e.Relay, cancel: "relay unchanged for " + alias,
		label: "The relay " + alias + "'s link goes through (a host name, or wss://…; a relay without a domain with its #sha256= pin). Set the same on " + alias + " for this server.",
		submit: func(s string) tea.Cmd {
			src := m.src
			return m.act(func() actionMsg {
				warn, err := src.SetPeerRelay(alias, s)
				msg := "relay for " + alias + " set; set it on " + alias + " too"
				if warn != "" {
					msg += " · " + warn
				}
				return actionMsg{err: err, msg: msg, then: func(m *dash) { pt.load(m) }}
			})
		}})
}

// remove asks, then unpins alias and has the server close its link; the
// tab reads the list again after.
func (pt *peersTab) remove(m *dash, alias string) {
	q := fmt.Sprintf("Remove %s? Its link closes at once and its projects leave the sidebar; it refuses this server until the two pair again. Remove this server on %s too (tm peer remove there).", alias, alias)
	m.confirmNo("Remove "+alias, q, alias+" kept", func() tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			err := src.RemovePeer(alias)
			return actionMsg{err: err, msg: "removed " + alias + "; its link closes. Remove this server on " + alias + " too.",
				then: func(m *dash) { pt.load(m) }}
		})
	})
}

// lastRelay is the relay of the server paired last (peers.json keeps
// them in that order), for n to start from.
func (pt *peersTab) lastRelay() string {
	if n := len(pt.list.Peers); n > 0 {
		return pt.list.Peers[n-1].Relay
	}
	return ""
}

// selectAlias selects the panes row of the server called alias.
func (pt *peersTab) selectAlias(m *dash, alias string) {
	for i, it := range pt.items(m) {
		if it.kind == itemPanes && pt.list.Peers[it.peer].Alias == alias {
			pt.sel = i
		}
	}
}

// click selects the item under a click; on the selected share box it
// toggles it, and on a panes row's word it sets that level.
func (pt *peersTab) click(m *dash, item, col int) tea.Cmd {
	items := pt.items(m)
	if item < 0 || item >= len(items) {
		return nil
	}
	it, was := items[item], item == pt.sel
	pt.sel = item
	switch {
	case it.kind == itemShare && was:
		return pt.toggle(m, it.slug)
	case it.kind == itemRelay && was:
		pt.relay(m, pt.list.Peers[it.peer])
	case it.kind == itemPanes:
		e := pt.list.Peers[it.peer]
		if to := panesAt(panesLevel(e), col); to != "" {
			return pt.setPanes(m, e, to)
		}
	}
	return nil
}
