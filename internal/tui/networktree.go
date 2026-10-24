package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The Network popup's tree (network.go): this server first, with the
// projects it may share, each with its share box; then each paired
// server, its connection and the panes you give it, with the projects it
// shares with you from the mirror (stale while it is offline).
//
//	"▾ ◉ studio · this server 4F2A-91C3                 shares 1 of 2"
//	"   ■ terminatr          [x] shared · seen by work, nas"
//	"   ■ blog               [ ] not shared"
//	""
//	"▾ ◉ work (del-02-0468-lt) · connected 38 ms        panes view"
//	"   ■ cowork             shared with you · + link (L)    ● working"
//	""
//	"▾ ○ nas · offline 4m · relay                       panes off"
//	"     shares nothing with you yet"

// The kinds of netRow.
const (
	netServer  = iota // a server: this one (peer -1) or a paired one
	netProject        // a project of this server's, with its share box
	netTheirs         // a project a paired server shares with this one
	netNote           // faint words, which the cursor skips
	netGap            // a blank line between servers
	netLink           // a link of a project of this server's (networklinks.go)
	netNeed           // what a link needs of the user, under it
)

// netRow is one row of the tree.
type netRow struct {
	kind int
	// peer is the paired server's index in the list (PeerList.Peers),
	// -1 for this server.
	peer int
	// slug is a project's; project its mirror (netTheirs).
	slug    string
	project *proto.PeerMirrorProject
	// text is a note's words; bad draws them as an error.
	text string
	bad  bool
	// other is a link's other project (link.Row.Peer), link its row as
	// the last poll has it; last: the project's last child, whose need
	// rows go without the tree's bar. need is a need row's kind (needHeld
	// …), msg and file what it waits on; listed: the need's row in the
	// NEEDS YOU list over the tree.
	other  string
	link   link.Row
	last   bool
	need   int
	msg    *link.Message
	file   link.Transfer
	listed bool
}

// selectable says whether the cursor stops on r.
func (r netRow) selectable() bool { return r.kind != netNote && r.kind != netGap }

// key names r as the selection keeps it across reads: "g:" this
// server, "g:<alias>" a paired one (as the sidebar's peerKey),
// "p:<slug>" a project here, "t:<alias>/<slug>" one of theirs,
// "l:<slug>><other>" a link and "n:<slug>><other>><need>" what it
// needs (needKey), "!n:…" in the NEEDS YOU list.
func (nv *networkView) rowKey(r netRow) string {
	alias := ""
	if r.peer >= 0 {
		alias = nv.list.Peers[r.peer].Alias
	}
	switch r.kind {
	case netServer:
		return "g:" + alias
	case netProject:
		return "p:" + r.slug
	case netTheirs:
		return "t:" + alias + "/" + r.slug
	case netLink:
		return "l:" + r.slug + ">" + r.other
	case netNeed:
		k := needKey(r.slug, r.other, r.need, r.id())
		if r.listed {
			k = "!" + k
		}
		return k
	}
	return ""
}

// relayHint ends the tree with nothing paired: where a relay comes from.
const relayHint = "No relay yet? On a Linux server with a domain: sudo tm relay install --domain NAME; without one, on an address both servers reach: sudo tm relay install --ip (see the README)."

// hasLinks says whether project slug has rows under it in the tree.
func (nv *networkView) hasLinks(m *dash, slug string) bool {
	if p := m.projectData(slug); p != nil {
		return slices.ContainsFunc(p.Links, func(r link.Row) bool { return shownLink(r) || r.Asks != nil })
	}
	return false
}

// shareable are the projects this server offers to share: the active
// ones (the dashboard's data has no archived one).
func shareable(m *dash) []string {
	var out []string
	for _, p := range m.data.Projects {
		if p.Safety == nil || p.Safety.Active {
			out = append(out, p.Slug)
		}
	}
	return out
}

// shared says whether this server shares slug.
func shared(m *dash, slug string) bool {
	p := m.projectData(slug)
	return p != nil && p.Safety != nil && p.Safety.Share
}

// sharedSlugs are the projects this server shares.
func sharedSlugs(m *dash) []string {
	var out []string
	for _, slug := range shareable(m) {
		if shared(m, slug) {
			out = append(out, slug)
		}
	}
	return out
}

// mirror is the dashboard's mirror of paired server e, nil while it has
// none.
func mirror(m *dash, e PeerEntry) *proto.PeerMirror {
	for i := range m.data.Peers {
		if m.data.Peers[i].ID == e.ID {
			return &m.data.Peers[i]
		}
	}
	return nil
}

// netName is a paired server's name here: its display name, else its
// alias.
func netName(e PeerEntry) string { return cmp.Or(e.Name, e.Alias) }

// pairedNames are the paired servers' names, joined.
func (nv *networkView) pairedNames() string {
	var out []string
	for _, e := range nv.list.Peers {
		out = append(out, netName(e))
	}
	return strings.Join(out, ", ")
}

// rows are the tree's rows, in order, after the NEEDS YOU list's when
// the popup is netWide or wider.
func (nv *networkView) rows(m *dash) []netRow {
	out := append(nv.listRows(m), netRow{kind: netServer, peer: -1})
	if !nv.folded["g:"] {
		slugs := shareable(m)
		if len(slugs) == 0 {
			out = append(out, netRow{kind: netNote, text: "no active project here to share"})
		}
		for _, slug := range slugs {
			out = append(out, netRow{kind: netProject, peer: -1, slug: slug})
			if !nv.folded["p:"+slug] {
				out = append(out, nv.linkRows(m, slug)...)
			}
		}
	}
	switch {
	case nv.err != nil:
		return append(out, netRow{kind: netGap}, netRow{kind: netNote, bad: true, text: "The paired servers can't be read: " + oneLine(nv.err.Error())})
	case len(nv.list.Peers) == 0:
		return append(out, netRow{kind: netGap},
			netRow{kind: netNote, text: "No paired server yet. P pairs with one: it shows a code to pass on, and J on the other server takes it (or tm peer pair and tm peer join in a terminal); both of you then compare six digits."},
			netRow{kind: netNote, text: relayHint})
	}
	if nv.list.LinkErr != "" {
		out = append(out, netRow{kind: netGap}, netRow{kind: netNote, bad: true, text: "The server can't connect: " + oneLine(nv.list.LinkErr)})
	}
	for i, e := range nv.list.Peers {
		out = append(out, netRow{kind: netGap}, netRow{kind: netServer, peer: i})
		if nv.folded["g:"+e.Alias] {
			continue
		}
		pm := mirror(m, e)
		if pm == nil || len(pm.Projects) == 0 {
			out = append(out, netRow{kind: netNote, peer: i, text: "shares nothing with you yet"})
			continue
		}
		for j := range pm.Projects {
			out = append(out, netRow{kind: netTheirs, peer: i, slug: pm.Projects[j].Project, project: &pm.Projects[j]})
		}
	}
	return out
}

// spread draws left and right on one line w cells wide, right at its
// end; left gives way, then right goes.
func spread(left, right string, w int) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	switch {
	case right == "":
		return fit(left, w)
	case lw+1+rw <= w:
		return left + strings.Repeat(" ", w-lw-rw) + right
	case w-rw-1 >= w/2:
		return fit(left, w-rw-1) + " " + right
	}
	return fit(left, w)
}

// connLook is a paired server's connection glyph, its look and its
// words: from the mirror while the dashboard polls it (it follows the
// connection as it goes), else as the list read it.
func (nv *networkView) connLook(m *dash, e PeerEntry) (string, lipgloss.Style, string) {
	var pl *peerLink
	if pm := mirror(m, e); pm != nil {
		pl = &peerLink{state: pm.State, reason: pm.Reason, since: pm.Since, rtt: pm.RTT}
	} else if e.Link != nil {
		pl = &peerLink{state: e.Link.State, reason: e.Link.Reason, since: e.Link.Since, rtt: e.Link.RTT}
	}
	if pl == nil {
		words := "no connection yet"
		switch {
		case !nv.list.Running:
			words = "the server isn't running"
		case nv.list.LinkErr != "":
			words = "not connected"
		}
		g, st := linkLook(&peerLink{state: proto.LinkOffline})
		return g, st, words
	}
	g, st := linkLook(pl)
	words := linkWords(pl, sideClock())
	if pl.state == proto.LinkUp {
		words = "connected"
		if pl.rtt > 0 {
			words += " " + rttWords(pl.rtt)
		}
	}
	return g, st, words
}

// peerVersion is the tm paired server e runs, as far as known.
func peerVersion(m *dash, e PeerEntry) string {
	v := ""
	if e.Link != nil {
		v = e.Link.Version
	}
	if pm := mirror(m, e); pm != nil && pm.Version != "" {
		v = pm.Version
	}
	return textsafe.Line(strings.TrimPrefix(v, "v"), 40)
}

// netAliasMax is the most of a server's name and alias its row shows.
const netAliasMax = 32

// whoLook is a paired server's name as rows draw it: its display name
// with the alias beside it, or the alias.
func whoLook(e PeerEntry) string {
	if e.Name == "" {
		return th().Head.Render(fit(e.Alias, min(ansi.StringWidth(e.Alias), netAliasMax)))
	}
	who := peer.Both(e.Name, e.Alias)
	return th().Head.Render(e.Name) + th().Faint.Render(fit(strings.TrimPrefix(who, e.Name), min(ansi.StringWidth(who)-ansi.StringWidth(e.Name), netAliasMax)))
}

// rowLines are row r's lines in a tree w cells wide; only a note wraps.
func (nv *networkView) rowLines(m *dash, r netRow, w int) []string {
	name := 22
	if w >= 100 {
		name = 26
	}
	i := ic()
	switch r.kind {
	case netGap:
		return []string{""}
	case netNote:
		pad := 0
		if r.peer >= 0 {
			pad = 5
		}
		var out []string
		for _, l := range wrapLines(r.text, w-pad) {
			st := th().Faint
			if r.bad {
				st = th().Bad
			}
			out = append(out, strings.Repeat(" ", pad)+st.Render(l))
		}
		return out
	case netServer:
		fold := i.unfold
		if nv.folded[nv.rowKey(r)] {
			fold = i.fold
		}
		fold = th().Faint.Render(fold)
		if r.peer < 0 {
			g, st := linkLook(&peerLink{local: true})
			left := fold + " " + st.Render(g) + " " + th().Head.Render(thisHost()) + th().Faint.Render(" · this server")
			if nv.list.Short != "" {
				left += th().Faint.Render(" " + nv.list.Short)
			}
			right := ""
			if w >= 50 {
				right = th().Faint.Render(fmt.Sprintf("shares %d of %d", len(sharedSlugs(m)), len(shareable(m))))
			}
			return []string{spread(left, right, w)}
		}
		e := nv.list.Peers[r.peer]
		g, st, words := nv.connLook(m, e)
		left := fold + " " + st.Render(g) + " " + whoLook(e)
		switch {
		case strings.HasPrefix(words, "connected"):
			left += " · " + words
		case words == "connecting":
			left += " · " + th().Warn.Render(words)
		default:
			left += th().Faint.Render(" · " + words)
		}
		if v := peerVersion(m, e); v != "" && w >= 70 {
			left += th().Faint.Render(" · tm " + v)
		}
		right := ""
		if w >= 50 {
			right = th().Faint.Render("panes ") + th().Accent.Render(proto.PanesWord(panesLevel(e)))
		}
		return []string{spread(left, right, w)}
	case netLink:
		return []string{nv.linkLine(m, r, w)}
	case netNeed:
		return []string{needLine(m, r, w)}
	case netProject:
		box := th().Accent.Render("[ ]") + th().Faint.Render(" not shared")
		if shared(m, r.slug) {
			box = th().Accent.Render("[x]") + " shared"
			if len(nv.list.Peers) > 0 && w >= 50 {
				box += th().Faint.Render(" · seen by " + nv.pairedNames())
			}
		}
		left := "   " + th().Accent.Render(i.folder) + " " + fit(th().Head.Render(r.slug), name-5) + " " + box
		right := ""
		if w >= 50 {
			right = linkCount(m, r.slug)
		}
		if ansi.StringWidth(left)+1+ansi.StringWidth(right) > w {
			right = ""
		}
		return []string{spread(left, right, w)}
	case netTheirs:
		p := r.project
		slug := th().Head.Render(r.slug)
		if p.Stale {
			slug = th().Faint.Render(r.slug + " · stale")
		}
		links := theirLinks(m, nv.list.Peers[r.peer].Alias, r.slug)
		if links == "" {
			links = th().Faint.Render("shared with you · ") + th().Accent.Render("+ link") + th().Faint.Render(" (L)")
		}
		left := "   " + th().Accent.Render(i.folder) + " " + fit(slug, name-5) + " " + links
		right := ""
		if g, st, word := theirCoord(p); word != "" && w >= 50 {
			if p.Stale {
				st = th().Faint
			}
			right = st.Render(g) + " " + th().Faint.Render(word)
		}
		if ansi.StringWidth(left)+1+ansi.StringWidth(right) > w {
			right = "" // the link hint first
		}
		return []string{spread(left, right, w)}
	}
	return nil
}

// theirCoord is a paired server's project's coordinator as its row
// shows it: its state's glyph, look and word; "" while unknown.
func theirCoord(p *proto.PeerMirrorProject) (string, lipgloss.Style, string) {
	i := slices.IndexFunc(p.Sessions, func(s proto.PeerSession) bool { return s.Role == proto.RoleCoordinator })
	if i < 0 {
		if p.Watch == nil {
			return "", th().Faint, ""
		}
		return ic().none, th().Faint, "not running"
	}
	state := peerState(p.Sessions[i])
	g, st := stateLook(state)
	word, _, _ := strings.Cut(state, " ")
	return g, st, word
}

// detail is the two lines w cells wide under the tree about row r, when the popup has
// no room for the details panel.
func (nv *networkView) detail(m *dash, r netRow, w int) [2]string {
	switch r.kind {
	case netLink, netNeed:
		return nv.linkDetail(m, r, w)
	case netServer:
		if r.peer < 0 {
			id := ""
			if nv.list.Short != "" {
				id = " · id " + nv.list.Short
			}
			shares := "shares nothing with the paired servers"
			if s := sharedSlugs(m); len(s) > 0 {
				shares = "shares " + strings.Join(s, ", ") + " with every paired server"
			}
			return [2]string{thisHost() + " · this server" + id + " · " + counted(len(nv.list.Peers), "paired server"),
				th().Faint.Render(shares + " · P pairs a new one")}
		}
		e := nv.list.Peers[r.peer]
		l1 := []string{peer.Both(e.Name, e.Alias), "id " + e.Short}
		if v := peerVersion(m, e); v != "" {
			l1 = append(l1, "tm "+v)
		}
		if e.Link != nil && e.Link.RelayVersion != "" {
			if _, behind := relayBehind(m.data.Server, e.Link.RelayVersion); behind {
				l1 = append(l1, th().Warn.Render("relay update (U)"))
			}
		}
		l1 = append(l1, "relay "+relayWords(e.Relay))
		return [2]string{joinFit(l1, w), joinFit(append([]string{"panes: " + th().Accent.Render(netName(e)+" "+panesMay(panesLevel(e))) + " (V)"}, otherWay(mirror(m, e))...), w)}
	case netProject:
		if shared(m, r.slug) {
			return [2]string{r.slug + " · shared with every paired server: they see its state, never its files",
				th().Faint.Render("space stops sharing it, at once · L links it with another project")}
		}
		return [2]string{r.slug + " · not shared: paired servers don't see it", th().Faint.Render("space shares it with every paired server, asking first · L links it")}
	case netTheirs:
		e := nv.list.Peers[r.peer]
		l1 := netName(e) + "'s " + r.slug + " · " + freshWords(r.project)
		if _, _, word := theirCoord(r.project); word != "" && !r.project.Stale {
			l1 += " · coordinator " + word
		}
		return [2]string{l1, th().Faint.Render("nothing here acts on it; enter shows its dashboard, read-only; L links one of yours with it")}
	}
	return [2]string{}
}

// panesMay says what a pane switch at level lets a server do with this
// server's panes, as "may view yours".
func panesMay(level string) string {
	switch level {
	case proto.PanesView:
		return "may watch yours"
	case proto.PanesType:
		return "may watch and type into yours"
	}
	return "may not open yours"
}

// otherWay is what a paired server (its mirror pm) lets this server do
// with its panes, and how many projects it shares, in a few words.
func otherWay(pm *proto.PeerMirror) []string {
	may := "you may not open its panes"
	switch {
	case pm == nil:
		return []string{"what it lets you do isn't known yet"}
	case !pm.Pane:
		may = "its tm has no remote panes"
	case pm.Panes == proto.PanesView:
		may = "you may watch its panes"
	case pm.Panes == proto.PanesType:
		may = "you may watch and type into its panes"
	}
	return []string{may, "it shares " + counted(len(pm.Projects), "project") + " with you"}
}

// joinFit joins parts with " · " as far as they fit in w cells; the
// first always shows, cut if it must.
func joinFit(parts []string, w int) string {
	out := parts[0]
	for _, p := range parts[1:] {
		if ansi.StringWidth(out+" · "+p) > w {
			break
		}
		out += " · " + p
	}
	return fit(out, w)
}

// freshWords say how fresh a paired server's project is here.
func freshWords(p *proto.PeerMirrorProject) string {
	switch {
	case p.Synced.IsZero():
		return "not read yet"
	case p.Stale:
		return "stale: last seen " + p.Synced.Local().Format("15:04")
	}
	return "synced " + age(max(sideClock().Sub(p.Synced), 0)) + " ago"
}

// rowKeys are the action row's keys on row r, with the tree holding the
// keyboard.
func (nv *networkView) rowKeys(m *dash, r netRow, ok bool) string {
	tail := "? keys · esc close"
	switch {
	case !ok:
		return "P pair · J join · " + tail
	case r.kind == netServer && r.peer < 0:
		return "enter fold · tab details · P pair · J join · " + tail
	case r.kind == netServer:
		e := nv.list.Peers[r.peer]
		u := ""
		if e.Link != nil && e.Link.RelayVersion != "" {
			if _, behind := relayBehind(m.data.Server, e.Link.RelayVersion); behind {
				u = "U update relay"
			}
		}
		return joinKeys("E name", "V panes", u, "X unpair", "enter fold", "tab details", "P pair", "J join", tail)
	case r.kind == netProject && shared(m, r.slug):
		return "space unshare · L link · tab details · " + tail
	case r.kind == netProject:
		return "space share · L link · tab details · " + tail
	case r.kind == netTheirs:
		return "enter its dashboard · L link one of yours · " + tail
	case r.kind == netLink, r.kind == netNeed:
		return nv.linkKeys(m, r)
	}
	return tail
}
