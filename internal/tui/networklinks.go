package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Network popup's links (docs/SPEC.md §4, §7.9, §16.11, §16.13):
// under each project of this server's its links, each with this side's
// direction, ask first and files, and the other side's state; a link
// between two projects here shows under both, each with its own side.
// What a link needs of the user hangs under it, flagged ⚑: a message
// held for the OK, a file offered, a paired server's link request, a
// link the breaker paused. A paired server's project shows the links
// your projects have to it.
//
//	"   ■ terminatr          [x] shared · seen by work           2 links ⚑"
//	"     ├─ ⇄ beta          both · files: ask me                 paused ⚑"
//	"     │  ⚑ paused: too many messages without you"
//	"     └─ ⇄ work/acme-api   both · ask first · files: ask me  linked back"
//	"        ⚑ M-4F2A-31 waits for your OK · Draft of the API notes"
//
// enter on a link, and L on a project (then a picker of the projects
// to link it with), open the link dialog (linkDialog): nothing is
// written until Y. Here one act sets both sides, the other's direction
// the mirror; to a paired server's project only this side, and the
// dialog can ask that server's user to set theirs.

// The kinds of a need row.
const (
	needHeld   = iota // a message of this side's held for the OK
	needFile          // a file offered here
	needAsk           // a paired server's link request
	needPaused        // the link, paused by the breaker
)

// needKey is a need row's key: "n:<slug>><other>><kind>[:<id>]".
func needKey(slug, other string, need int, id string) string {
	k := "n:" + slug + ">" + other + ">" + []string{"held", "file", "ask", "paused"}[need]
	if id != "" {
		k += ":" + id
	}
	return k
}

// id is the message or file a need row waits on, "" for none.
func (r netRow) id() string {
	switch {
	case r.need == needHeld && r.msg != nil:
		return r.msg.ID
	case r.need == needFile:
		return r.file.ID
	}
	return ""
}

// linkLog is what a link's messages and files say, as last read.
type linkLog struct {
	msgs  []*link.Message
	files []link.Transfer
	back  link.Back
}

// logOf is link slug ⇄ r.Peer's messages and files, read at most every
// 2 s, and only for a link to a paired server (what the other side says
// and what waits to go are in its messages), one with something waiting
// or one with messages its panel shows.
func (nv *networkView) logOf(m *dash, slug string, r link.Row) linkLog {
	if nv.logs == nil || time.Since(nv.logsAt) > 2*time.Second {
		nv.logs, nv.logsAt = map[string]linkLog{}, time.Now()
	}
	key := slug + ">" + r.Peer
	if l, ok := nv.logs[key]; ok {
		return l
	}
	var l linkLog
	if r.Remote || r.Held > 0 || r.FilesWaiting > 0 || r.Last != "" {
		if msgs, err := m.src.LinkLog(slug, r.Peer); err == nil {
			l.msgs, l.back = msgs, link.TheirSide(msgs)
		}
	}
	if r.FilesWaiting > 0 {
		l.files, _ = m.src.LinkFiles(slug, r.Peer)
	}
	nv.logs[key] = l
	return l
}

// relog has the links' messages read again at the next drawing: an
// act's then.
func (nv *networkView) relog(m *dash) { nv.logsAt = time.Time{} }

// shownLink says whether link row r is a link the tree shows: an entry
// on either side.
func shownLink(r link.Row) bool { return r.HasMine || r.HasTheirs }

// linkRows are project slug's children in the tree: its links, each
// with its needs, and a paired server's request for a link it has none
// of yet.
func (nv *networkView) linkRows(m *dash, slug string) []netRow {
	p := m.projectData(slug)
	if p == nil {
		return nil
	}
	var out []netRow
	for _, r := range p.Links {
		if !shownLink(r) && r.Asks == nil {
			continue
		}
		if shownLink(r) {
			out = append(out, netRow{kind: netLink, peer: -1, slug: slug, other: r.Peer, link: r})
		}
		need := func(kind int) netRow {
			return netRow{kind: netNeed, peer: -1, slug: slug, other: r.Peer, link: r, need: kind}
		}
		if r.Held > 0 || r.FilesWaiting > 0 {
			l := nv.logOf(m, slug, r)
			for _, msg := range l.msgs {
				if msg.Out && msg.State == link.StateHeld {
					n := need(needHeld)
					n.msg = msg
					out = append(out, n)
				}
			}
			for _, t := range l.files {
				if !t.Out && t.State == link.FileOffered {
					n := need(needFile)
					n.file = t
					out = append(out, n)
				}
			}
		}
		if r.Asks != nil {
			out = append(out, need(needAsk))
		}
		if r.Paused {
			out = append(out, need(needPaused))
		}
	}
	// The last child goes without the tree's bar below it, and so do
	// its needs.
	for i := len(out) - 1; i >= 0; i-- {
		out[i].last = true
		if out[i].kind == netLink || out[i].need == needAsk && !shownLink(out[i].link) {
			break
		}
	}
	return out
}

// isNeed says whether the cursor's ! stops on r: a need in the tree,
// where it shows what it belongs to.
func (r netRow) isNeed() bool { return (r.kind == netNeed || r.kind == netShareReq) && !r.listed }

// listRows are the NEEDS YOU list's rows over the tree, with the popup
// netWide or wider: every project's needs, folded or not, then the
// paired servers' share requests, as the tree has them, each answered
// on its row with the tree row's keys.
func (nv *networkView) listRows(m *dash) []netRow {
	if m.inner(viewWidth) < netWide {
		return nil
	}
	var out []netRow
	for _, slug := range shareable(m) {
		for _, r := range nv.linkRows(m, slug) {
			if r.kind == netNeed {
				r.listed = true
				out = append(out, r)
			}
		}
	}
	for i, e := range nv.list.Peers {
		for _, r := range shareRows(mirror(m, e), i) {
			if r.kind == netShareReq {
				r.listed = true
				out = append(out, r)
			}
		}
	}
	return out
}

// needsList is the NEEDS YOU list as drawn over the tree, w cells wide
// in a body of n lines: its count line, its rows (as many as a third of
// the body holds, scrolled to the selection, sel its index in rows) and
// a rule, with the row each line draws (-1 for none); nothing with
// nothing listed.
func (nv *networkView) needsList(m *dash, rows []netRow, sel, w, n int) ([]string, []int) {
	var at []int
	for i, r := range rows {
		if r.listed {
			at = append(at, i)
		}
	}
	if len(at) == 0 {
		return nil, nil
	}
	h := min(len(at), max(n/3-2, 1))
	if j := slices.Index(at, sel); j >= 0 {
		nv.ltop = min(nv.ltop, j)
		nv.ltop = max(nv.ltop, j-h+1)
	}
	nv.ltop = min(max(nv.ltop, 0), len(at)-h)
	var links []link.Row
	for _, slug := range shareable(m) {
		if p := m.projectData(slug); p != nil {
			links = append(links, p.Links...)
		}
	}
	shares := 0
	for _, i := range at {
		if rows[i].kind == netShareReq {
			shares++
		}
	}
	head := needsLineShares(links, shares)
	if head == "" {
		head = th().Warn.Render("⚑ NEEDS YOU")
	}
	next := th().Accent.Render("!") + th().Faint.Render(" next in the tree")
	if ansi.StringWidth(head)+1+ansi.StringWidth(next) > w {
		next = "" // the count first
	}
	lines := []string{spread(head, next, w)}
	of := []int{-1}
	for k, i := range at[nv.ltop : nv.ltop+h] {
		l := nv.listLine(m, rows[i], w)
		if i == sel {
			l = selRow(l, w)
		}
		if k == 0 && nv.ltop > 0 {
			l = fit(l, w-1) + th().Faint.Render("↑")
		}
		if k == h-1 && nv.ltop+h < len(at) {
			l = fit(l, w-1) + th().Faint.Render("↓")
		}
		lines, of = append(lines, l), append(of, i)
	}
	return append(lines, th().Faint.Render(strings.Repeat("─", w))), append(of, -1)
}

// listLine is the NEEDS YOU list's row r drawn w cells wide: the link
// it is on (a share request's server), then what it waits for.
func (nv *networkView) listLine(m *dash, r netRow, w int) string {
	if r.kind == netShareReq {
		what := ""
		if r.share.For != "" {
			what = " (for its " + oneLine(r.share.For) + ")"
		}
		on := fit(th().Head.Render(netName(nv.list.Peers[r.peer])), 30)
		return fit(" "+th().Warn.Render("⚑")+" "+on+" asks you to share a project"+th().Faint.Render(what+noteWords(r.share)), w)
	}
	glyph := "·"
	if r.link.HasMine {
		glyph = dirGlyph(r.link.Mine.Allow)
	}
	on := fit(th().Head.Render(r.slug)+" "+th().Accent.Render(glyph)+" "+peerShownKey(m.data.Peers, r.other), 30)
	return fit(" "+th().Warn.Render("⚑")+" "+on+" "+needWords(m, r, true), w)
}

// The direction, ask and files of a link in words.
var (
	dirGlyphs  = map[string]string{config.LinkBoth: "⇄", config.LinkSend: "→", config.LinkReceive: "←"}
	filesPlain = map[string]string{config.FilesAsk: "ask me", config.FilesAuto: "take them", config.FilesOff: "refuse"}
)

// dirGlyph is a direction's mark, · for none.
func dirGlyph(allow string) string {
	if g, ok := dirGlyphs[allow]; ok {
		return g
	}
	return "·"
}

// dirWords is a direction in words: "both ways", "send only".
func dirWords(allow string) string { return linkAllowWords(config.Link{Allow: allow}) }

// sideWords are a link entry's settings, short or in full: "both · ask
// first · files: ask me", "send · ask · refuse".
func sideWords(l config.Link, has, short bool) string {
	if !has {
		return "no entry on this side"
	}
	var out []string
	switch {
	case short:
		out = append(out, l.Allow)
	case l.Allow == config.LinkBoth:
		out = append(out, "both")
	default:
		out = append(out, dirWords(l.Allow))
	}
	switch {
	case l.Ask && short:
		out = append(out, "ask")
	case l.Ask:
		out = append(out, "ask first")
	}
	if short {
		return strings.Join(append(out, filesPlain[l.FilesMode()]), " · ")
	}
	return strings.Join(append(out, "files: "+filesPlain[l.FilesMode()]), " · ")
}

// theirMirror is the paired server alias's mirror, nil for none.
func theirMirror(m *dash, alias string) *proto.PeerMirror {
	for i := range m.data.Peers {
		if m.data.Peers[i].Alias == alias {
			return &m.data.Peers[i]
		}
	}
	return nil
}

// linkedBack says whether a paired server's project links back: its
// user accepted this side's request, or messages crossed both ways.
func linkedBack(r link.Row, back link.Back) bool {
	w := otherSide(r, back)
	return w == "linked back both ways" || strings.HasPrefix(w, "accepted")
}

// linkState is a link's health as its row ends: paused, half-linked or
// linked here; for a paired server's project why nothing crosses (that
// server offline, with what waits in the outbox; a message undelivered;
// this side's request unanswered), else its other side as far as known.
// short is the row's form under 70 cells.
func (nv *networkView) linkState(m *dash, slug string, r link.Row, short bool) (string, lipgloss.Style) {
	if r.Paused {
		return "paused ⚑", th().Warn
	}
	alias, other, remote := config.Remote(r.Peer)
	if !remote {
		switch {
		case r.Half() && short:
			return "half-linked", th().Warn
		case r.Half():
			return r.HalfWords(), th().Warn
		}
		return "linked", th().Good
	}
	name := peerShown(m.data.Peers, alias)
	pm := theirMirror(m, alias)
	l := nv.logOf(m, slug, r)
	b := link.BacklogOf(l.msgs)
	switch {
	case pm == nil:
		return name + " isn't paired", th().Warn
	case r.HasMine && pm.Listed && !slices.ContainsFunc(pm.Projects, func(p proto.PeerMirrorProject) bool { return p.Project == other }):
		return "not shared any more", th().Warn
	case pm.State != proto.LinkUp && b.Queued > 0 && short:
		return fmt.Sprintf("offline · %d queued", b.Queued), th().Faint
	case pm.State != proto.LinkUp && b.Queued > 0:
		return fmt.Sprintf("%s offline · %d queued in the outbox", name, b.Queued), th().Faint
	case pm.State != proto.LinkUp:
		return name + " offline", th().Faint
	case b.Undelivered > 0 && short:
		return "undelivered", th().Warn
	case b.Undelivered > 0:
		return "undelivered (see the inbox)", th().Warn
	case r.Asked != nil && r.Asked.Pending() && short:
		return "waiting on " + name, th().Warn
	case r.Asked != nil && r.Asked.Pending():
		return "waiting on " + name + "'s user", th().Warn
	}
	if linkedBack(r, l.back) {
		return "linked back", th().Good
	}
	return otherSide(r, l.back), th().Faint
}

// linkLine is link row r drawn w cells wide.
func (nv *networkView) linkLine(m *dash, r netRow, w int) string {
	name := 22
	if w >= 100 {
		name = 26
	}
	tree := "├─"
	if r.last {
		tree = "└─"
	}
	glyph := "·"
	if r.link.HasMine {
		glyph = dirGlyph(r.link.Mine.Allow)
	}
	head := "     " + th().Faint.Render(tree) + " " + fit(th().Accent.Render(glyph)+" "+peerShownKey(m.data.Peers, r.other), name-6) + " "
	state, st := nv.linkState(m, r.slug, r.link, w < 70)
	// This side's settings shorten before the state gives way.
	left := head + th().Faint.Render(sideWords(r.link.Mine, r.link.HasMine, false))
	if w < 70 || ansi.StringWidth(left)+1+ansi.StringWidth(state) > w {
		left = head + th().Faint.Render(sideWords(r.link.Mine, r.link.HasMine, true))
	}
	return spread(left, st.Render(state), w)
}

// needLine is need row r drawn w cells wide.
func needLine(m *dash, r netRow, w int) string {
	bar := th().Faint.Render("│") + "  "
	if r.last {
		bar = "   "
	}
	flag := th().Warn.Render("⚑") + " "
	line := needWords(m, r, false)
	if r.need == needAsk {
		if !shownLink(r.link) {
			tree := "├─"
			if r.last {
				tree = "└─"
			}
			return fit("     "+th().Faint.Render(tree)+flag+line, w)
		}
	}
	return fit("     "+bar+flag+line, w)
}

// needWords say what need row r waits for; listed, beside the link it
// is on.
func needWords(m *dash, r netRow, listed bool) string {
	switch r.need {
	case needHeld:
		return oneLine(r.msg.ID) + th().Warn.Render(" waits for your OK") + th().Faint.Render(" · "+msgTitle(r.msg))
	case needFile:
		return oneLine(r.file.Name) + " " + link.MB(r.file.Size) + th().Warn.Render(" offered") + th().Faint.Render(" · with "+oneLine(r.file.Message))
	case needPaused:
		return th().Warn.Render("paused: too many messages without you")
	}
	asks := th().Warn.Render("asks to link") + th().Faint.Render(" ("+dirWords(r.link.Asks.Allow)+" there, "+oneLine(r.link.Asks.ID)+")")
	if listed {
		return asks
	}
	return peerShownKey(m.data.Peers, r.other) + " " + asks
}

// theirLinks are the links your projects have to a paired server's
// project, as its row shows them: "⇄ terminatr · → web".
func theirLinks(m *dash, alias, slug string) string {
	key := alias + "/" + slug
	var out []string
	for _, p := range m.data.Projects {
		for _, r := range p.Links {
			if r.Peer == key && r.HasMine {
				out = append(out, th().Accent.Render(dirGlyph(config.Mirror(r.Mine.Allow)))+" "+p.Slug)
			}
		}
	}
	return strings.Join(out, th().Faint.Render(" · "))
}

// linkCount is how many links project slug has, and whether one needs
// you: its row's right end.
func linkCount(m *dash, slug string) string {
	p := m.projectData(slug)
	n := 0
	if p != nil {
		for _, r := range p.Links {
			if shownLink(r) {
				n++
			}
		}
	}
	words := th().Faint.Render("no links")
	if n > 0 {
		words = th().Faint.Render(counted(n, "link"))
	}
	if p != nil && p.LinkWaits > 0 {
		words += " " + th().Warn.Render("⚑")
	}
	return words
}

// linkDetail is the two lines under the tree about link or need row r.
func (nv *networkView) linkDetail(m *dash, r netRow, w int) [2]string {
	other := peerShownKey(m.data.Peers, r.other)
	alias, _, remote := config.Remote(r.other)
	name := peerShown(m.data.Peers, alias)
	l := r.link
	switch {
	case r.kind == netNeed && r.need == needHeld:
		return [2]string{fit(oneLine(r.msg.ID)+" · "+r.slug+" → "+other+" · "+sizeWords(int64(r.msg.Bytes))+" · "+th().Warn.Render("waits for your OK"), w),
			th().Faint.Render(fit("enter reads it · A sends it as your OK · X drops it", w))}
	case r.kind == netNeed && r.need == needFile:
		return [2]string{fit(oneLine(r.file.Name)+" · "+link.MB(r.file.Size)+" · offered by "+other+" with "+oneLine(r.file.Message)+" · files: "+filesPlain[l.Mine.FilesMode()], w),
			th().Faint.Render(fit("A accepts it (it lands beside its message) · X declines · enter reads its message", w))}
	case r.kind == netNeed && r.need == needAsk:
		q := l.Asks
		return [2]string{fit(other+" asks to link with "+r.slug+" ("+oneLine(q.ID)+"): "+dirWords(q.Allow)+" on its side, so "+dirWords(config.Mirror(q.Allow))+" here", w),
			th().Faint.Render(fit("A accepts with a direction you pick, writing only this server's side · X declines, nothing changes", w))}
	case r.kind == netNeed:
		return [2]string{fit(r.slug+" ⇄ "+other+" · "+th().Warn.Render("paused: too many messages each way without you"), w),
			th().Faint.Render(fit("R resumes it: the count starts again", w))}
	}
	l1 := r.slug + " " + dirGlyph(l.Mine.Allow) + " " + other + " · this side " + sideWords(l.Mine, l.HasMine, false)
	var l2 string
	switch {
	case l.Paused:
		l2 = th().Warn.Render("paused: too many messages without you · R resumes it")
	case remote:
		state, _ := nv.linkState(m, r.slug, l, false)
		l2 = name + " decides its side: " + state
		if !linkedBack(l, nv.logOf(m, r.slug, l).back) {
			l2 += " · R asks its user"
		}
		l2 = th().Faint.Render(l2)
	default:
		l2 = th().Faint.Render("one act sets both sides · " + r.other + "'s side: " + sideWords(l.Theirs, l.HasTheirs, false))
	}
	if l.Last != "" {
		l2 += th().Faint.Render(" · last " + oneLine(l.Last))
	}
	return [2]string{fit(l1, w), fit(l2, w)}
}

// netMapWide is the details panel's width from which a project's links
// draw as a map.
const netMapWide = 40

// linkMap is project slug's links in its details panel, w cells wide:
// from netMapWide a map whose lines say each link's state, ━ linked,
// ┄ waiting on the other side, ─ its server offline, in the warning
// colour while paused, with a legend; under that each link in a word.
//
//	"LINKS 2"
//	" terminatr ┄┄⇄┄┄ mini/acme-api ⚑ ask · files: ask me"
//	"           ━━⇄━━ web           ⚑ files: ask me"
//	"━ linked  ┄ waiting  ─ offline  ━ paused"
func (nv *networkView) linkMap(m *dash, slug string, w int) []string {
	var links []link.Row
	if p := m.projectData(slug); p != nil {
		links = slices.DeleteFunc(slices.Clone(p.Links), func(r link.Row) bool { return !shownLink(r) })
	}
	out := []string{th().Faint.Render(fmt.Sprintf("LINKS %d", len(links)))}
	if len(links) == 0 {
		return append(out, th().Faint.Render("none · L links it"))
	}
	col := 0 // the other ends' column: the longest, to 18
	for _, r := range links {
		col = max(col, min(ansi.StringWidth(peerShownKey(m.data.Peers, r.Peer)), 18))
	}
	for i, r := range links {
		glyph := "·"
		if r.HasMine {
			glyph = dirGlyph(r.Mine.Allow)
		}
		other := peerShownKey(m.data.Peers, r.Peer)
		flag := "  "
		if r.Held+r.FilesWaiting > 0 || r.Paused || r.Asks != nil {
			flag = " " + th().Warn.Render("⚑")
		}
		if w < netMapWide {
			out = append(out, fit(th().Accent.Render(glyph)+" "+other+strings.TrimRight(flag, " "), w))
			continue
		}
		head := ""
		if i == 0 {
			head = " " + slug
		}
		c, st := nv.mapLine(m, slug, r)
		settings := "files: " + filesPlain[r.Mine.FilesMode()]
		if r.Mine.Ask {
			settings = "ask · " + settings
		}
		if !r.HasMine {
			settings = "no entry here"
		}
		line := fit(head, min(ansi.StringWidth(slug)+2, 16)) + st.Render(c+c+glyph+c+c) + " " + fit(other, col) + flag + " " + th().Faint.Render(settings)
		out = append(out, fit(line, w))
	}
	if w >= netMapWide {
		legend := th().Good.Render("━") + th().Faint.Render(" linked  ") + th().Warn.Render("┄") + th().Faint.Render(" waiting  ") +
			th().Faint.Render("─ offline  ") + th().Warn.Render("━") + th().Faint.Render(" paused")
		out = append(out, fit(legend, w))
	}
	return out
}

// mapLine is link r's line in the map and its look: paused, its server
// offline, waiting on the other side (half-linked here, not linked back
// there), or linked.
func (nv *networkView) mapLine(m *dash, slug string, r link.Row) (string, lipgloss.Style) {
	alias, _, remote := config.Remote(r.Peer)
	switch {
	case r.Paused:
		return "━", th().Warn
	case !remote && r.Half():
		return "┄", th().Warn
	case !remote:
		return "━", th().Good
	}
	if pm := theirMirror(m, alias); pm == nil || pm.State != proto.LinkUp {
		return "─", th().Faint
	}
	if linkedBack(r, nv.logOf(m, slug, r).back) {
		return "━", th().Good
	}
	return "┄", th().Warn
}

// lastMessages are the headers of link slug ⇄ r.Peer's last three
// messages, newest first, w cells wide, under a line that says where
// the rest are; none without messages.
func (nv *networkView) lastMessages(m *dash, slug string, r link.Row, w int) []string {
	msgs := nv.logOf(m, slug, r).msgs
	if len(msgs) == 0 {
		return nil
	}
	out := []string{th().Faint.Render(fit("CONVERSATION · all of it in the Messages tab", w))}
	for i := len(msgs) - 1; i >= max(len(msgs)-3, 0); i-- {
		msg := msgs[i]
		dir := th().Accent.Render("←")
		if msg.Out {
			dir = th().Head.Render("→")
		}
		when := " " + age(max(sideClock().Sub(msg.Created), 0))
		id := oneLine(msg.ID)
		title := fit(msgTitle(msg), max(w-ansi.StringWidth(id)-ansi.StringWidth(when)-6, 1))
		out = append(out, spread("  "+dir+" "+th().Faint.Render(id)+" "+title, th().Faint.Render(when), w))
	}
	return out
}

// The panel's items on a link: its switches.
const (
	itemDir = itemShare + 1 + iota
	itemAsk
	itemFiles
)

// switchWords is a switch's words, the current one (cur) in brackets
// and the accent.
func switchWords(words []string, cur int) string {
	var parts []string
	for i, w := range words {
		if i == cur {
			parts = append(parts, th().Accent.Render("["+w+"]"))
			continue
		}
		parts = append(parts, th().Faint.Render(w))
	}
	return strings.Join(parts, th().Faint.Render(" · "))
}

// switchAt is the index of the word a click at col is on, the switch's
// words starting at column x; -1 for none.
func switchAt(words []string, cur, col, x int) int {
	for i, w := range words {
		n := ansi.StringWidth(w)
		if i == cur {
			n += 2
		}
		if col >= x && col < x+n {
			return i
		}
		x += n + 3
	}
	return -1
}

// The link switches' values and words, in the order they show.
var (
	dirVals    = config.LinkAllows
	dirLabels  = []string{"both", "send", "receive"}
	askVals    = []string{"on", "off"}
	filesVals  = config.LinkFiles
	filesWords = []string{"ask me", "take them", "refuse"}
)

// linkSwitch is the panel's switch item's words and current index on
// link r.
func linkSwitch(item int, l config.Link) ([]string, int) {
	switch item {
	case itemDir:
		return dirLabels, slices.Index(dirVals, l.Allow)
	case itemAsk:
		return askVals, map[bool]int{true: 0, false: 1}[l.Ask]
	}
	return filesWords, slices.Index(filesVals, l.FilesMode())
}

// linkPanel is the details panel's lines w cells wide about link or
// need row r.
func (nv *networkView) linkPanel(m *dash, r netRow, w int) []panelLine {
	var out []panelLine
	add := func(item int, lines ...string) {
		for _, l := range lines {
			out = append(out, panelLine{text: l, item: item})
		}
	}
	row := func(label, value string) {
		for i, l := range wrapLines(value, max(w-panelLabel, 8)) {
			if i > 0 {
				label = ""
			}
			add(itemNone, fit(th().Faint.Render(label), panelLabel)+l)
		}
	}
	rule := th().Faint.Render(strings.Repeat("─", max(w, 1)))
	other := peerShownKey(m.data.Peers, r.other)
	if r.kind == netNeed {
		add(itemNone, th().Warn.Render("⚑ NEEDS YOU"), rule)
		d := nv.linkDetail(m, r, 1<<16)
		for _, l := range d {
			// Without fit's padding, which a style can hold.
			l = ansi.Truncate(l, ansi.StringWidth(strings.TrimRight(ansi.Strip(l), " ")), "")
			add(itemNone, wrapLines(l, w)...)
		}
		return out
	}
	l := r.link
	add(itemNone, th().Head.Render(r.slug)+" "+th().Accent.Render(dirGlyph(l.Mine.Allow))+" "+th().Head.Render(other), rule)
	for _, it := range []struct {
		item  int
		label string
	}{{itemDir, "this side"}, {itemAsk, "ask first"}, {itemFiles, "files"}} {
		words, cur := linkSwitch(it.item, l.Mine)
		if !l.HasMine {
			cur = -1
		}
		out = append(out, panelLine{text: fit(th().Faint.Render(it.label), panelLabel) + switchWords(words, cur), item: it.item, x: panelLabel})
	}
	alias, _, remote := config.Remote(r.other)
	if remote {
		state, st := nv.linkState(m, r.slug, l, false)
		row("other side", st.Render(state))
		words := peerShown(m.data.Peers, alias) + " decides it"
		if !linkedBack(l, nv.logOf(m, r.slug, l).back) {
			words += " · R asks its user"
		}
		add(itemNone, strings.Repeat(" ", panelLabel)+th().Faint.Render(fit(words, max(w-panelLabel, 1))))
	} else {
		row("other side", sideWords(l.Theirs, l.HasTheirs, false))
	}
	state := "open"
	switch {
	case l.Paused:
		state = th().Warn.Render("paused · R resumes")
	case l.Half():
		state = th().Warn.Render(l.HalfWords())
	}
	row("state", state)
	msgs := th().Faint.Render("none yet")
	if l.Last != "" {
		msgs = "last " + oneLine(l.Last)
	}
	row("messages", msgs)
	help := "← → change a switch, writing at once (" + map[bool]string{true: "this side only", false: "both sides"}[remote] + "); enter edits the whole link"
	if conv := nv.lastMessages(m, r.slug, l, w); len(conv) > 0 {
		add(itemNone, "")
		add(itemNone, conv...)
		help += "."
	} else {
		help += "; all of the conversation is in the project's Messages tab."
	}
	add(itemNone, "")
	add(itemNone, faintLines(help, w)...)
	return out
}

// linkKeys are the action row's keys on link or need row r.
func (nv *networkView) linkKeys(m *dash, r netRow) string {
	tail := "tab details · ? keys · esc close"
	if r.kind == netNeed {
		switch r.need {
		case needHeld:
			return "enter read · A send · X drop · ! next · esc close"
		case needFile:
			return "enter read · A accept · X decline · ! next · esc close"
		case needAsk:
			return "A accept · X decline · ! next · esc close"
		}
		return "R resume · ! next · esc close"
	}
	r2 := ""
	_, _, remote := config.Remote(r.other)
	switch {
	case r.link.Paused:
		r2 = "R resume"
	case remote && !linkedBack(r.link, nv.logOf(m, r.slug, r.link).back):
		r2 = "R ask back"
	}
	m2 := ""
	if r.link.Send {
		m2 = "M write"
	}
	return joinKeys("enter edit", "K ask first", "F files", r2, m2, "X unlink", tail)
}

// linkKey runs key s on link or need row r; done is false for a key it
// leaves to the tree's.
func (nv *networkView) linkKey(m *dash, r netRow, s string) (tea.Cmd, bool) {
	if r.kind == netNeed {
		return nv.needKey(m, r, s)
	}
	l, slug, other := r.link, r.slug, r.other
	shown := peerShownKey(m.data.Peers, other)
	alias, _, remote := config.Remote(other)
	switch s {
	case "enter":
		nv.linkDialog(m, slug, other)
	case "K":
		if !l.HasMine {
			m.msg = "link " + shown + " on this side first (enter)"
			return nil, true
		}
		ask, src := !l.Mine.Ask, m.src
		return m.act(func() actionMsg {
			if err := src.SetLinkAsk(slug, other, ask); err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: slug + "'s messages to " + shown + ": " + map[bool]string{true: "ask first", false: "sent freely"}[ask], then: nv.relog}
		}), true
	case "F":
		if !l.HasMine {
			m.msg = "link " + shown + " on this side first (enter)"
			return nil, true
		}
		return m.setFiles(slug, other, nextFiles(l.Mine.FilesMode()), nv.relog), true
	case "R":
		switch {
		case l.Paused:
			return m.resumeLink(slug, other), true
		case !remote:
			m.msg = "a link here is set on both sides at once; R resumes it once paused"
		case linkedBack(l, nv.logOf(m, slug, l).back):
			m.msg = peerShown(m.data.Peers, alias) + "'s user linked " + shown + " back already"
		default:
			return m.askLinkBack(slug, l, nv.relog), true
		}
	case "M":
		return m.writeMessage(slug, []link.Row{l}, other), true
	case "X":
		q := "Unlink " + slug + " and " + shown + "? This removes both sides; the messages already sent stay in both projects."
		if remote {
			q = "Unlink " + slug + " from " + shown + "? This removes this side; " + peerShown(m.data.Peers, alias) + "'s stays its user's. The messages already sent stay."
		}
		m.confirmNo("Unlink", q, "link kept", func() tea.Cmd {
			src := m.src
			return m.act(func() actionMsg {
				if _, err := src.SaveLink(slug, other, config.Link{Allow: config.LinkOff}); err != nil {
					return actionMsg{err: settingsErr(err)}
				}
				return actionMsg{msg: slug + " and " + shown + " unlinked", then: nv.relog}
			})
		})
	case "L":
		nv.pick(m, slug, "")
	default:
		return nil, false
	}
	return nil, true
}

// needKey runs key s on need row r: a held message and a file go
// through their message's dialog (messages.go), a request through its
// menu of this side's directions, a paused link resumes.
func (nv *networkView) needKey(m *dash, r netRow, s string) (tea.Cmd, bool) {
	switch {
	case (r.need == needHeld || r.need == needFile) && (s == "enter" || s == "A" || s == "X"):
		e := msgEntry{other: r.other, msg: r.msg}
		if r.need == needFile {
			e = fileEntry(r.other, r.file, nv.logOf(m, r.slug, r.link).msgs)
		}
		m.openMessage(r.slug, e, "")
		if s == "enter" {
			return nil, true
		}
		return m.top().key(m, keyMsg(s)), true
	case r.need == needAsk && (s == "A" || s == "X"):
		return m.answerRequest(r.slug, r.link, s == "A"), true
	case r.need == needAsk && s == "enter":
		q := r.link.Asks
		m.msg = oneLine(q.ID) + " from " + peerShownKey(m.data.Peers, r.other) + ": " + dirWords(q.Allow) + " on its side · A accepts, X declines"
		return nil, true
	case r.need == needPaused && s == "R":
		return m.resumeLink(r.slug, r.other), true
	}
	return nil, false
}

// nextNeed selects the next need row after the selection, round the
// end.
func (nv *networkView) nextNeed(m *dash, rows []netRow) {
	_, at, _ := nv.selected(m, rows)
	for d := 1; d <= len(rows); d++ {
		if j := (at + d + len(rows)) % len(rows); rows[j].isNeed() {
			nv.sel, nv.at = nv.rowKey(rows[j]), j
			return
		}
	}
	m.msg = "nothing needs you here"
}

// panelLinkKey runs key s on the panel's switch item of link row r:
// ← → step it, writing at once (raising files to take them asks
// first).
func (nv *networkView) panelLinkKey(m *dash, r netRow, item int, s string) tea.Cmd {
	d := map[string]int{"left": -1, "right": 1, "enter": 1, "space": 1, " ": 1}[s]
	if d == 0 {
		return nil
	}
	words, cur := linkSwitch(item, r.link.Mine)
	return nv.panelSet(m, r, item, min(max(cur+d, 0), len(words)-1))
}

// panelSet sets the switch item of link row r to its value i.
func (nv *networkView) panelSet(m *dash, r netRow, item, i int) tea.Cmd {
	if !r.link.HasMine {
		m.msg = "link " + peerShownKey(m.data.Peers, r.other) + " on this side first (enter)"
		return nil
	}
	l := r.link.Mine
	switch item {
	case itemDir:
		if l.Allow == dirVals[i] {
			return nil
		}
		l.Allow = dirVals[i]
	case itemAsk:
		if l.Ask == (i == 0) {
			return nil
		}
		l.Ask = i == 0
	case itemFiles:
		if l.FilesMode() == filesVals[i] {
			return nil
		}
		return m.setFiles(r.slug, r.other, filesVals[i], nv.relog)
	}
	return m.saveLink(r.slug, r.other, l, false, nv.relog)
}

// saveLink writes slug's side of its link to other as l (both sides
// here), asking that server's user to link back with request; then
// runs after it.
func (m *dash) saveLink(slug, other string, l config.Link, request bool, then func(*dash)) tea.Cmd {
	src := m.src
	shown := peerShownKey(m.data.Peers, other)
	alias, _, remote := config.Remote(other)
	return m.act(func() actionMsg {
		out, err := src.SaveLink(slug, other, l)
		if err != nil {
			return actionMsg{err: settingsErr(err), then: then}
		}
		msg := slug + " " + dirGlyph(l.Allow) + " " + shown + ": " + linkAllowWords(l) + ", files: " + filesPlain[l.FilesMode()]
		if remote {
			msg += " on this side; " + peerShown(m.data.Peers, alias) + " decides its side"
		} else {
			msg += " on both sides"
		}
		if out != "" {
			msg += "; " + out
		}
		if request {
			words, err := src.LinkAct(slug, "request", other, l.Allow)
			if err != nil {
				return actionMsg{err: fmt.Errorf("%s; the request: %w", msg, settingsErr(err)), then: then}
			}
			msg = words
		}
		return actionMsg{msg: msg, then: then}
	})
}

// linkPickView is L's picker: the projects to link slug with (or, for a
// paired server's project theirs, the projects here to link with it),
// grouped by server, and last "a server not paired yet", which pairs
// first.
type linkPickView struct {
	nv          *networkView
	slug, their string
	items       []pickItem
	sel         int
}

// pickItem is a picker's row: a group's heading (head), a project to
// link (other), or the pairing entry (pair).
type pickItem struct {
	head, label, other string
	pair               bool
}

// pick opens L's picker on project slug here, or on a paired server's
// project their ("<alias>/<slug>").
func (nv *networkView) pick(m *dash, slug, their string) {
	var items []pickItem
	linked := func(slug, other string) bool {
		if p := m.projectData(slug); p != nil {
			for _, r := range p.Links {
				if r.Peer == other && r.HasMine {
					return true
				}
			}
		}
		return false
	}
	group := func(head string, rows []pickItem) {
		if len(rows) > 0 {
			items = append(append(items, pickItem{head: head}), rows...)
		}
	}
	var here []pickItem
	for _, s := range shareable(m) {
		switch {
		case their != "" && !linked(s, their):
			here = append(here, pickItem{label: s, other: s})
		case their == "" && s != slug && !linked(slug, s):
			here = append(here, pickItem{label: s, other: s})
		}
	}
	group(thisHost()+" · this server", here)
	if their == "" {
		for _, e := range nv.list.Peers {
			var rows []pickItem
			if pm := mirror(m, e); pm != nil {
				for _, p := range pm.Projects {
					if key := e.Alias + "/" + p.Project; !linked(slug, key) {
						label := p.Project
						if p.Stale {
							label += th().Faint.Render(" · stale")
						}
						rows = append(rows, pickItem{label: label, other: key})
					}
				}
			}
			group(netName(e)+" · paired server", rows)
		}
		group("not paired yet", []pickItem{{label: th().Accent.Render("+") + " a server not paired yet" + th().Faint.Render(" · pair it here"), pair: true}})
	}
	if len(items) == 0 {
		m.msg = "no project left to link " + peerShownKey(m.data.Peers, cmpStr(their, slug)) + " with"
		return
	}
	v := &linkPickView{nv: nv, slug: slug, their: their, items: items}
	v.sel = v.step(-1, 1)
	m.push(v)
}

// step is the selectable item d items on from i.
func (v *linkPickView) step(i, d int) int {
	for j := i + d; j >= 0 && j < len(v.items); j += d {
		if v.items[j].head == "" {
			return j
		}
	}
	return max(i, 0)
}

func (v *linkPickView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); s {
	case "up", "down":
		v.sel = v.step(v.sel, scrollKeys[s])
	case "esc":
		m.pop()
		m.msg = "nothing linked"
	case "enter", "space":
		return v.choose(m, v.sel)
	}
	return nil
}

// choose takes item i: the pairing entry pairs, a project opens the
// link dialog.
func (v *linkPickView) choose(m *dash, i int) tea.Cmd {
	if i < 0 || i >= len(v.items) || v.items[i].head != "" {
		return nil
	}
	m.close(v)
	switch it := v.items[i]; {
	case it.pair:
		m.pairPrompt(v.nv.lastRelay())
	case v.their != "":
		v.nv.linkDialog(m, it.other, v.their)
	default:
		v.nv.linkDialog(m, v.slug, it.other)
	}
	return nil
}

func (v *linkPickView) click(m *dash, item, _ int, _ bool) tea.Cmd {
	if item >= 0 && item < len(v.items) && v.items[item].head == "" {
		v.sel = item
		return v.choose(m, item)
	}
	return nil
}

func (v *linkPickView) wheel(m *dash, d int) { v.key(m, arrow(d)) }

func (v *linkPickView) render(m *dash) string {
	w := m.inner(viewWidth)
	var lines []string
	hits := make([]int, len(v.items))
	for i, it := range v.items {
		hits[i] = i
		switch {
		case it.head != "":
			lines = append(lines, th().Faint.Render(fit(it.head, w)))
		case i == v.sel:
			lines = append(lines, th().Sel.Render(fit("› "+ansi.Strip(it.label), w)))
		default:
			lines = append(lines, fit("  "+it.label, w))
		}
	}
	title := "Link " + v.slug + " with"
	if v.their != "" {
		title = "Link one of yours with " + peerShownKey(m.data.Peers, v.their)
	}
	return m.popup(box{title: title, body: lines, sel: v.sel, hits: hits, keys: "enter pick · ↑ ↓ move · esc cancel"})
}

// linkDialogView is the link dialog: this side's direction, ask first
// and files, and for a paired server's project whether to ask its user
// to link back; what crosses, as set. Y writes it, once.
type linkDialogView struct {
	nv          *networkView
	slug, other string
	// had: the link has an entry on this side, was.
	had    bool
	was    config.Link
	remote bool
	// vals are the fields' values' indexes: direction, ask, files and,
	// with asking, ask their user (0 yes).
	vals   []int
	asking bool
	sel    int
}

// linkDialog opens the link dialog on slug's link to other, as the
// last poll has it.
func (nv *networkView) linkDialog(m *dash, slug, other string) {
	v := &linkDialogView{nv: nv, slug: slug, other: other}
	alias, _, remote := config.Remote(other)
	v.remote = remote
	var r link.Row
	if p := m.projectData(slug); p != nil {
		for _, x := range p.Links {
			if x.Peer == other {
				r = x
			}
		}
	}
	l := config.Link{Allow: config.LinkBoth, Ask: remote}
	if r.HasMine {
		v.had, v.was, l = true, r.Mine, r.Mine
	}
	_, cur := linkSwitch(itemAsk, l)
	v.vals = []int{max(slices.Index(dirVals, l.Allow), 0), cur, max(slices.Index(filesVals, l.FilesMode()), 0)}
	pending := r.Asked != nil && r.Asked.Pending()
	if remote && !pending && !linkedBack(r, nv.logOf(m, slug, r).back) && theirMirror(m, alias) != nil {
		v.asking = true
		v.vals = append(v.vals, map[bool]int{true: 1, false: 0}[r.HasMine])
	}
	m.push(v)
}

// link is the entry the dialog sets.
func (v *linkDialogView) link() config.Link {
	return config.Link{Allow: dirVals[v.vals[0]], Ask: v.vals[1] == 0, Files: filesVals[v.vals[2]]}
}

func (v *linkDialogView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); s {
	case "up", "down":
		v.sel = moveSel(v.sel, scrollKeys[s], len(v.vals))
	case "left", "right", "space":
		n, d := len(v.fieldWords(v.sel)), 1
		if s == "left" {
			d = len(v.fieldWords(v.sel)) - 1
		}
		v.vals[v.sel] = (v.vals[v.sel] + d) % n
	case "esc":
		m.pop()
		m.msg = "link unchanged"
	case "Y":
		return v.save(m)
	}
	return nil
}

// save writes the link as set, after asking first when its files go
// to take them, or to refuse while offers wait.
func (v *linkDialogView) save(m *dash) tea.Cmd {
	m.pop()
	l := v.link()
	request := v.asking && v.vals[3] == 0
	run := func() tea.Cmd { return m.saveLink(v.slug, v.other, l, request, v.nv.relog) }
	from := config.FilesAsk
	if v.had {
		from = v.was.FilesMode()
	}
	if title, q := filesAsk(m, v.slug, v.other, from, l.FilesMode()); q != "" {
		m.confirmNo(title, q, "link unchanged", run)
		return nil
	}
	return run()
}

// fieldWords are field i's words.
func (v *linkDialogView) fieldWords(i int) []string {
	switch i {
	case 0:
		return []string{"both ways", "send only", "receive only"}
	case 1:
		return askVals
	case 2:
		return filesWords
	}
	return []string{"yes", "no"}
}

func (v *linkDialogView) render(m *dash) string {
	w := m.inner(viewWidth)
	shown := peerShownKey(m.data.Peers, v.other)
	alias, _, _ := config.Remote(v.other)
	name := peerShown(m.data.Peers, alias)
	head := v.slug + " and " + shown + ", both on this server."
	if v.remote {
		head = v.slug + " on this server and " + shown + " on " + name + "."
	}
	if v.had {
		head = "Change this side of the link. " + head
	}
	lines := []string{fit(head, w), ""}
	labels := []string{"this side", "ask first", "files they send", "ask " + name + "'s user"}
	for i := range v.vals {
		l := fit(th().Faint.Render(labels[i]), 18) + switchWords(v.fieldWords(i), v.vals[i])
		if i == v.sel {
			lines = append(lines, th().Sel.Render(fit("› "+ansi.Strip(l), w)))
			continue
		}
		lines = append(lines, fit("  "+l, w))
	}
	l := v.link()
	kb, mb := 8, config.DefaultLinkFileMB
	if p := m.projectData(v.slug); p != nil && p.Safety != nil {
		kb, mb = p.Safety.LinkMaxKB, p.Safety.LinkFileMB
	}
	yes, no := th().Good.Render("✓")+" ", th().Bad.Render("✕")+" "
	msgs := fmt.Sprintf("messages %s, text only, up to %d KB each", dirWords(l.Allow), kb)
	if l.Ask && l.Sends() {
		msgs += "; each of " + v.slug + "'s after your OK"
	}
	files := no + "no files from them"
	switch {
	case !l.Receives():
		files = no + "no files from them: this side only sends"
	case l.FilesMode() == config.FilesAsk:
		files = yes + fmt.Sprintf("files they send, up to %d MB each, each after your OK", mb)
	case l.FilesMode() == config.FilesAuto:
		files = yes + fmt.Sprintf("files they send, up to %d MB each, taken without asking", mb)
	}
	lines = append(lines, "", th().Head.Render("WHAT CROSSES"), fit("  "+yes+msgs, w), fit("  "+files, w),
		fit("  "+no+"never the files, paths, repos or worktrees of either project", w), "")
	note := "One act sets both sides: send here is receive there; " + v.other + " keeps its own ask first."
	if v.remote {
		note = "This writes only this side; " + name + "'s user sets theirs."
		if v.asking && v.vals[3] == 0 {
			note += " Y also asks them to link back."
		}
	}
	lines = append(lines, faintLines(note, w)...)
	verb := "Y link"
	if v.had {
		verb = "Y save"
	}
	title := "Link " + v.slug + " with " + shown
	if v.had {
		title = "Link " + v.slug + " " + dirGlyph(v.was.Allow) + " " + shown
	}
	return m.popup(box{title: title, body: lines, sel: -1, keys: "↑ ↓ move · ← → change · " + verb + " · esc cancel"})
}
