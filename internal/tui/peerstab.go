package tui

import (
	"fmt"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Peers tab of the , popup (docs/SPEC.md §4, §16.8): the servers
// paired with this one, as tm peer list has them (Source.Peers): each
// one's alias, server id, link and relay, and its pane switch; p steps
// the switch off → view → write → off (asking before it lets the server
// do more), as tm peer panes does; x removes the selected one, asking
// first, as tm peer remove does; n pairs with another server and j
// joins one's pairing (peerpair.go), as tm peer pair and join do; e
// gives the selected one another relay, as tm peer relay does (the relay
// moved, or has a new key); u asks its relay to update itself
// (peerrelay.go). A project is shared from its own Settings tab.
//
//	" This server 4F2A-91C3"
//	""
//	" mini    91C3-77AB  ◉ linked · 38 ms"
//	"         relay relay.example.net · shares 2 projects · tm 0.27.0"
//	"         panes: write · it can watch and type into this server's …"
//
// Each server is a block of lines under its own: its settings go there.

// peersTab is the Peers tab: the list as last read, and the selection.
type peersTab struct {
	list   PeerList
	err    error
	loaded bool
	sel    int
}

// peersKeys are the Peers tab's action row.
const peersKeys = "n pair · j join · p panes · e relay · u update relay · x remove · ↑ ↓ move · ← → tabs · esc close"

// relayHint ends the empty tab: where a relay comes from.
const relayHint = "No relay yet? On a Linux server with a domain: sudo tm relay install --domain NAME; without one, on an address both servers reach: sudo tm relay install --ip (see the README)."

// load reads the paired servers again.
func (pt *peersTab) load(m *dash) {
	pt.list, pt.err = m.src.Peers()
	pt.loaded = true
	pt.sel = min(pt.sel, max(len(pt.list.Peers)-1, 0))
}

// lines draws the tab w cells wide: its lines, the selected server's
// first line, and what a click on each line picks (a server's index).
func (pt *peersTab) lines(m *dash, w int) (out []string, sel int, hits []int) {
	sel = -1
	if !pt.loaded {
		pt.load(m)
	}
	defer func() {
		for len(hits) < len(out) {
			hits = append(hits, noHit)
		}
	}()
	l := pt.list
	switch {
	case pt.err != nil:
		return []string{th().Bad.Render("The paired servers can't be read: " + oneLine(pt.err.Error()))}, -1, nil
	case l.ID == "":
		return append(faintLines("This server isn't paired with another. Press n to pair: it shows a code to pass on, and j on the other server takes it (or tm peer pair and tm peer join in a terminal); both of you then compare six digits.", w), faintLines(relayHint, w)...), -1, nil
	}
	out = append(out, th().Faint.Render("This server ")+l.Short)
	if l.LinkErr != "" {
		out = append(out, th().Bad.Render("The server can't link: "+oneLine(l.LinkErr)))
	}
	out = append(out, "")
	if len(l.Peers) == 0 {
		out = append(out, faintLines("No paired servers. Press n to pair with one, or j to join the code another shows.", w)...)
		out = append(out, faintLines(relayHint, w)...)
		return out, -1, nil
	}
	nameW := 4
	for _, e := range l.Peers {
		nameW = max(nameW, ansi.StringWidth(e.Alias))
	}
	nameW = min(nameW, 20)
	for i, e := range l.Peers {
		for len(hits) < len(out) {
			hits = append(hits, noHit)
		}
		g, gst, words := peerLinkWords(m, e, l)
		head := fit(e.Alias, nameW) + "  " + e.Short + "  "
		if i == pt.sel {
			sel = len(out)
			out = append(out, th().Sel.Render(fit(head+g+" "+words, max(w-1, 1))))
		} else {
			out = append(out, th().Head.Render(fit(e.Alias, nameW))+"  "+th().Faint.Render(e.Short)+"  "+gst.Render(g)+" "+words)
		}
		hits = append(hits, i)
		for _, d := range faintLines(peerAbout(m, e), max(w-nameW-2, 8)) {
			out = append(out, strings.Repeat(" ", nameW+2)+d)
			hits = append(hits, i)
		}
		for _, d := range faintLines(panesWords(e), max(w-nameW-2, 8)) {
			out = append(out, strings.Repeat(" ", nameW+2)+d)
			hits = append(hits, i)
		}
		out = append(out, "")
	}
	out = append(out, faintLines("p sets what the selected server may do with this server's panes, the coordinators and threads of the projects you share: nothing, watch them, or watch and type into them.", w)...)
	out = append(out, faintLines("e gives the selected server another relay, as tm peer relay does: when the relay moved or was set up again with a new key (its link refuses a relay whose key changed); set it on the other server too.", w)...)
	out = append(out, faintLines("u asks the selected server's relay to update itself to the latest official release, as tm peer relay-update does: it restarts only for a newer one, and takes one request an hour from any server linked through it.", w)...)
	out = append(out, faintLines("x removes the selected server, as tm peer remove does: its link closes and its projects leave the sidebar; remove this server there too. A project is shared from its own Settings tab (a on the dashboard).", w)...)
	return out, sel, hits
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

// peerAbout is a paired server's second line: its relay, what it shares
// with this server and its tm, as far as known.
func peerAbout(m *dash, e PeerEntry) string {
	bits := []string{"relay " + relayWords(e.Relay)}
	if v := relayVersionWords(m, e); v != "" {
		bits = append(bits, v)
	}
	for _, pm := range m.data.Peers {
		if pm.ID == e.ID && pm.Watch {
			bits = append(bits, "shares "+counted(len(pm.Projects), "project"))
		}
	}
	if e.Link != nil && e.Link.Version != "" {
		bits = append(bits, "tm "+strings.TrimPrefix(e.Link.Version, "v"))
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

// panesWords is a paired server's pane line: its switch and what that
// lets it do.
func panesWords(e PeerEntry) string {
	what := map[string]string{
		proto.PanesOff:  "it can't open this server's panes",
		proto.PanesView: "it can watch the coordinators and threads of the projects you share, not type",
		proto.PanesType: "it can watch and type into the coordinators and threads of the projects you share",
	}[panesLevel(e)]
	return "panes: " + proto.PanesWord(panesLevel(e)) + " · " + what
}

// nextPanes is the level p steps to from level.
func nextPanes(level string) string {
	switch level {
	case proto.PanesView:
		return proto.PanesType
	case proto.PanesType:
		return proto.PanesOff
	}
	return proto.PanesView
}

// panes steps the selected server's pane switch: asking first when it
// lets that server do more, at once when less.
func (pt *peersTab) panes(m *dash, e PeerEntry) tea.Cmd {
	to := nextPanes(panesLevel(e))
	set := func() tea.Cmd {
		src, alias := m.src, e.Alias
		return m.act(func() actionMsg {
			err := src.SetPeerPanes(alias, to)
			return actionMsg{err: err, msg: "panes " + proto.PanesWord(to) + " for " + alias, then: func(m *dash) { pt.load(m) }}
		})
	}
	if to == proto.PanesOff {
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

// key runs a key on the tab; ok is false for one it doesn't take.
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
		pt.sel = moveSel(pt.sel, d, len(pt.list.Peers))
		return nil, true
	}
	if s != "x" && s != "p" && s != "e" && s != "u" {
		return nil, false
	}
	if pt.sel >= len(pt.list.Peers) {
		return nil, true
	}
	switch s {
	case "p":
		return pt.panes(m, pt.list.Peers[pt.sel]), true
	case "e":
		pt.relay(m, pt.list.Peers[pt.sel])
		return nil, true
	case "u":
		pt.relayUpdate(m, pt.list.Peers[pt.sel])
		return nil, true
	}
	pt.remove(m, pt.list.Peers[pt.sel].Alias)
	return nil, true
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

// selectAlias selects the server called alias.
func (pt *peersTab) selectAlias(alias string) {
	for i, e := range pt.list.Peers {
		if e.Alias == alias {
			pt.sel = i
		}
	}
}

// click selects the server under a click.
func (pt *peersTab) click(item int) {
	if item >= 0 && item < len(pt.list.Peers) {
		pt.sel = item
	}
}
