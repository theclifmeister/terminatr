package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The Network popup's details panel (network.go): the selected row's
// every setting spelled out, beside the tree when the popup's inner
// width is netWide or more (tab gives it the keyboard), in the tree's
// place under that (tab). Its switches take a click; on a paired
// server it reads, without changing anything, what that server may do
// here as a ladder, and then the other way.
//
//	"work (del-02-0468-lt)"
//	"──────────────────────────────"
//	"name          work"
//	"connection    ◉ connected · 38 ms"
//	"id · tm       9C1B-77E0 · 0.50.0"
//	"relay         relay.example.net · v0.48.0 · update (U)"
//	""
//	"WHAT WORK MAY DO HERE"
//	"  ● paired: your projects and its can be linked"
//	"  ● sees what you share: terminatr, web"
//	"  ● watches your shared panes, popups read-only"
//	"  ○ types in them"
//	"  panes (V)   off · [view] · write"
//	"AND THE OTHER WAY"
//	"  work lets you watch its panes · its popup read-only"
//	"  it shares cowork, gatehouse"

// The panel's items: the rows its keyboard stops on.
const (
	itemNone  = iota
	itemName  // a paired server's display name: enter edits it
	itemRelay // its relay: enter edits it
	itemPanes // its pane switch: ← → or a click on a word
	itemShare // a project's share box: enter, space or a click
)

// panelLine is one line of the panel: its text, the item it belongs to
// (itemNone for none) and, on a switch, the column its words start at.
type panelLine struct {
	text string
	item int
	x    int
}

// panelLabel is the width of the panel's labels.
const panelLabel = 14

// panelLines are the panel's lines w cells wide about row r.
func (nv *networkView) panelLines(m *dash, r netRow, ok bool, w int) []panelLine {
	var out []panelLine
	add := func(item int, lines ...string) {
		for _, l := range lines {
			out = append(out, panelLine{text: l, item: item})
		}
	}
	row := func(item int, label, value string) {
		add(item, fit(th().Faint.Render(label), panelLabel)+value)
	}
	rule := th().Faint.Render(strings.Repeat("─", max(w, 1)))
	if !ok {
		return out
	}
	switch r.kind {
	case netServer:
		if r.peer < 0 {
			add(itemNone, th().Head.Render(thisHost())+th().Faint.Render(" · this server"), rule)
			if nv.list.Short != "" {
				row(itemNone, "id", nv.list.Short)
			}
			paired := nv.pairedNames()
			if paired == "" {
				paired = th().Faint.Render("none yet")
			}
			row(itemNone, "paired", paired)
			shares := strings.Join(sharedSlugs(m), ", ")
			if shares == "" {
				shares = th().Faint.Render("nothing")
			}
			row(itemNone, "shares", shares)
			add(itemNone, "")
			add(itemNone, faintLines("P pairs with another server, J joins one's pairing: the code works once, for 10 minutes, and both of you compare six digits before Y.", w)...)
			return out
		}
		e := nv.list.Peers[r.peer]
		name := netName(e)
		head := th().Head.Render(name)
		if e.Name != "" {
			head += th().Faint.Render(strings.TrimPrefix(peer.Both(e.Name, e.Alias), e.Name))
		}
		add(itemNone, head, rule)
		if e.Name != "" {
			row(itemName, "name", e.Name)
		} else {
			row(itemName, "name", th().Faint.Render("none: shown as "+e.Alias))
		}
		g, st, words := nv.connLook(m, e)
		row(itemNone, "connection", st.Render(g)+" "+words)
		idTM := e.Short
		if v := peerVersion(m, e); v != "" {
			idTM += " · " + v
		}
		row(itemNone, "id · tm", idTM)
		relay := relayWords(e.Relay)
		if e.Link != nil && e.Link.RelayVersion != "" {
			relay += " · v" + textsafe.Line(strings.TrimPrefix(e.Link.RelayVersion, "v"), 40)
			if _, behind := relayBehind(m.data.Server, e.Link.RelayVersion); behind {
				relay += th().Warn.Render(" · update (U)")
			}
		}
		row(itemRelay, "relay", relay)
		add(itemNone, "")
		level := panesLevel(e)
		lv := map[string]int{proto.PanesOff: 0, proto.PanesView: 1, proto.PanesType: 2}[level]
		rung := func(on bool, text string) {
			if on {
				add(itemNone, "  "+th().Good.Render("●")+" "+text)
			} else {
				add(itemNone, "  "+th().Faint.Render("○ "+text))
			}
		}
		add(itemNone, th().Faint.Render("WHAT "+strings.ToUpper(name)+" MAY DO HERE"))
		rung(true, "paired: your projects and its can be linked")
		if s := sharedSlugs(m); len(s) > 0 {
			rung(true, "sees what you share: "+strings.Join(s, ", "))
		} else {
			rung(false, "sees nothing: you share no project")
		}
		rung(lv >= 1, "watches your shared panes, popups read-only")
		rung(lv >= 2, "types in them")
		out = append(out, panelLine{text: "  " + fit(th().Faint.Render("panes (V)"), panelLabel-2) + panesChoices(level, true), item: itemPanes, x: panelLabel})
		add(itemNone, faintLines("  "+panesWhat(level), w)...)
		add(itemNone, th().Faint.Render("AND THE OTHER WAY"))
		add(itemNone, faintLines("  "+theirWords(mirror(m, e), name), w)...)
		theirs := th().Faint.Render("nothing yet")
		if pm := mirror(m, e); pm != nil && len(pm.Projects) > 0 {
			var s []string
			for _, p := range pm.Projects {
				s = append(s, p.Project)
			}
			theirs = strings.Join(s, ", ")
		}
		add(itemNone, "  "+th().Faint.Render("it shares ")+theirs)
	case netProject:
		add(itemNone, th().Head.Render(r.slug)+th().Faint.Render(" · this server"), rule)
		if shared(m, r.slug) {
			out = append(out, panelLine{text: fit(th().Faint.Render("shared"), panelLabel) + th().Accent.Render("[x]") + " with every paired server", item: itemShare, x: panelLabel})
			if len(nv.list.Peers) > 0 {
				add(itemNone, faintLines(strings.Repeat(" ", panelLabel)+"seen by "+nv.pairedNames()+": its state, never its files", w)...)
			}
		} else {
			out = append(out, panelLine{text: fit(th().Faint.Render("shared"), panelLabel) + th().Accent.Render("[ ]") + th().Faint.Render(" not shared"), item: itemShare, x: panelLabel})
		}
		add(itemNone, "")
		add(itemNone, faintLines("A shared project shows, read-only, in the sidebar of every paired server: what waits for you, the inbox, threads, tasks on deck and its sessions' states; never its files. Sharing asks first; it is the project's own Shared with paired servers setting.", w)...)
	case netTheirs:
		e := nv.list.Peers[r.peer]
		p := r.project
		add(itemNone, th().Head.Render(r.slug)+th().Faint.Render(" · on "+netName(e)), rule)
		if g, st, word := theirCoord(p); word != "" {
			row(itemNone, "coordinator", st.Render(g)+" "+word)
		}
		row(itemNone, "fresh", freshWords(p))
		pm := mirror(m, e)
		if pm != nil && pm.Popup && pm.Panes != proto.PanesOff {
			row(itemNone, "its popup", "read-only, from its dashboard")
		} else {
			row(itemNone, "its popup", th().Faint.Render("off: "+netName(e)+" doesn't let you view"))
		}
		add(itemNone, "")
		add(itemNone, faintLines("Nothing here acts on it: enter shows its dashboard, read-only. Linking a project of yours with it: its Settings tab (A, then 4), Linked projects, or tm project link.", w)...)
	}
	for i := range out {
		out[i].text = fit(out[i].text, w)
	}
	return out
}

// theirWords say what paired server name (its mirror pm) lets this
// server do with its panes and popups.
func theirWords(pm *proto.PeerMirror, name string) string {
	popups := ""
	if pm != nil && pm.Popup {
		popups = "; its popups open here, read-only"
	}
	switch {
	case pm == nil:
		return "what " + name + " lets you do isn't known yet"
	case !pm.Pane:
		return name + "'s tm has no remote panes"
	case pm.Panes == proto.PanesView:
		return name + " lets you watch its panes" + popups
	case pm.Panes == proto.PanesType:
		return name + " lets you watch and type into its panes" + popups
	}
	return name + " lets you see its shared projects only"
}

// panelItems are the items the panel's keyboard stops on, in order.
func panelItems(lines []panelLine) []int {
	var out []int
	for _, l := range lines {
		if l.item != itemNone && !slices.Contains(out, l.item) {
			out = append(out, l.item)
		}
	}
	return out
}

// panesChoices is the pane switch's words, off · view · write, the
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

// panesAt is the level whose word a click at col is on, the switch's
// words starting at column x; "" for none.
func panesAt(level string, col, x int) string {
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
