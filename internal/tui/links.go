package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Links between projects' coordinators (docs/SPEC.md §7.9, §4): the
// Settings tab's Linked projects row and its list, where the human sets
// this side of each link; and what the Messages tab (messages.go) says
// of a message and its file. The text of a message is shown to the
// human only; coordinators read it framed, with tm link read.

// linkRow is the Settings tab's Linked projects row of project slug.
func linkRow(slug string) setting {
	return setting{label: "Linked projects", help: "The other projects whose coordinators this one may write to and hear from (tm link). Linking sets both projects at once: send here is receive there; with a paired server's project, only this side. Enter lists the projects, this server's and the paired servers'.",
		value: func(m *dash) string { return linksWords(m.projectData(slug)) },
		change: func(m *dash) tea.Cmd {
			m.push(&linksView{slug: slug})
			return nil
		}}
}

// linksWords is the Linked projects row's value: each link with its
// direction, or none.
func linksWords(p *ProjectData) string {
	if p == nil {
		return "none"
	}
	var parts []string
	for _, r := range p.Links {
		switch {
		case r.Send || r.Receive:
			parts = append(parts, r.Peer+" "+r.Arrow())
		case r.Half():
			parts = append(parts, r.Peer+" (half-linked)")
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " · ")
}

// linksView lists every other project with what each side allows:
// enter steps the link through off, both, send and receive, writing both
// sides at once (the other side's entry the mirror of this one's); a
// turns this side's ask first on or off. A half-linked pair (a hand edit
// of config.toml) shows as such, and enter makes it whole. Under them,
// a group per paired server lists the projects it shares now (the
// mirror, peer.list), any this side still has an entry for, marked once
// that server's list no longer has it, and any a link request waits on:
// there enter and a write this side only, a new entry asking first,
// since that server's user sets its own (§16.11); what the messages tell
// of that side shows beside it.
type linksView struct {
	slug string
	sel  int
	// back is what the messages say of each remote link's other side,
	// read at most every 2 s.
	back   map[string]link.Back
	loaded time.Time
}

// linkItem is one row of the list: a project here (alias "") or the
// project slug on the paired server alias, key its link's key; gone:
// this side's entry names a project that server no longer shares.
type linkItem struct {
	key, alias, slug string
	gone             bool
}

// linkGroup is a paired server's rows, with its link's state.
type linkGroup struct {
	alias, state string
	items        []linkItem
}

// items are the projects the list shows: every project here but this
// one, then each paired server's group; sel indexes them all in order.
func (v *linksView) items(m *dash) ([]linkItem, []linkGroup) {
	var out []linkItem
	for _, p := range m.data.Projects {
		if p.Slug != v.slug {
			out = append(out, linkItem{key: p.Slug, slug: p.Slug})
		}
	}
	slices.SortFunc(out, func(a, b linkItem) int { return strings.Compare(a.key, b.key) })
	groups := map[string]*linkGroup{}
	var order []string
	group := func(alias string) *linkGroup {
		if g := groups[alias]; g != nil {
			return g
		}
		groups[alias] = &linkGroup{alias: alias, state: "not paired"}
		order = append(order, alias)
		return groups[alias]
	}
	add := func(alias, slug string, gone bool) {
		g, key := group(alias), alias+"/"+slug
		if !slices.ContainsFunc(g.items, func(it linkItem) bool { return it.key == key }) {
			g.items = append(g.items, linkItem{key: key, alias: alias, slug: slug, gone: gone})
		}
	}
	// listed are the paired servers whose shared projects are known
	// (a peer.projects answer, perhaps the last before the link went
	// down): a project missing there isn't shared.
	listed := map[string]bool{}
	for _, pm := range m.data.Peers {
		listed[pm.Alias] = pm.Listed
		group(pm.Alias).state = map[string]string{proto.LinkUp: "linked", proto.LinkConnecting: "connecting"}[pm.State]
		if pm.State != proto.LinkUp && pm.State != proto.LinkConnecting {
			group(pm.Alias).state = "offline"
		}
		for _, pp := range pm.Projects {
			add(pm.Alias, pp.Project, false)
		}
	}
	// An unshared project stays while a link to it is set, so the user
	// can set it off, or while a request waits; an answered request of
	// this side's keeps none.
	if p := m.projectData(v.slug); p != nil {
		for _, r := range p.Links {
			alias, slug, ok := config.Remote(r.Peer)
			if ok && (r.HasMine || r.Asks != nil || r.Asked != nil && r.Asked.Pending()) {
				add(alias, slug, r.HasMine && listed[alias])
			}
		}
	}
	var gs []linkGroup
	for _, a := range order {
		g := groups[a]
		if len(g.items) == 0 {
			continue
		}
		slices.SortFunc(g.items, func(a, b linkItem) int { return strings.Compare(a.key, b.key) })
		gs = append(gs, *g)
		out = append(out, g.items...)
	}
	return out, gs
}

// entry is this side's and the other side's entries for other, from the
// last poll.
func (v *linksView) entry(m *dash, other string) (row link.Row) {
	row.Peer = other
	_, _, row.Remote = config.Remote(other)
	if p := m.projectData(v.slug); p != nil {
		for _, r := range p.Links {
			if r.Peer == other {
				return r
			}
		}
	}
	return row
}

// theirSide is what the messages say of the remote link key's other
// side; the logs are read again at most every 2 s.
func (v *linksView) theirSide(m *dash, items []linkItem, key string) link.Back {
	if v.back == nil || time.Since(v.loaded) > 2*time.Second {
		v.back, v.loaded = map[string]link.Back{}, time.Now()
		for _, it := range items {
			if it.alias == "" {
				continue
			}
			if msgs, err := m.src.LinkLog(v.slug, it.key); err == nil {
				v.back[it.key] = link.TheirSide(msgs)
			}
		}
	}
	return v.back[key]
}

// otherSide is the other side's column of remote row r: the answer to
// this side's request while nothing newer tells otherwise, else what the
// messages say (back).
func otherSide(r link.Row, back link.Back) string {
	q := r.Asked
	switch {
	case q == nil:
	case q.Pending(), q.State == link.ReqDeclined:
		return q.Words()
	case q.State == link.ReqAccepted && !back.Refuses:
		return q.Words()
	case back == link.Back{}:
		return q.Words()
	}
	return back.Words()
}

// nextAllow is the direction after cur: off, both, send, receive, off.
func nextAllow(cur string, has bool) string {
	if !has {
		return config.LinkBoth
	}
	switch cur {
	case config.LinkBoth:
		return config.LinkSend
	case config.LinkSend:
		return config.LinkReceive
	}
	return config.LinkOff
}

func (v *linksView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	items, _ := v.items(m)
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "down":
		v.sel = moveSel(v.sel, scrollKeys[s], len(items))
	case "enter", "space", " ":
		if v.sel >= len(items) || m.busy {
			break
		}
		// One act sets both sides here: this side's direction, the
		// other's the mirror; off removes both. A paired server's
		// project gets this side only, asking first when new.
		it := items[v.sel]
		other, r := it.key, v.entry(m, it.key)
		allow := nextAllow(r.Mine.Allow, r.HasMine)
		if it.gone {
			// No message goes to a project its server doesn't share.
			allow = config.LinkOff
		}
		src, slug := m.src, v.slug
		return m.act(func() actionMsg {
			if err := src.SetLink(slug, other, allow); err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			if it.alias != "" {
				l := config.Link{Allow: allow, Ask: !r.HasMine || r.Mine.Ask}
				return actionMsg{msg: slug + " → " + other + ": " + linkAllowWords(l) + " on this side; " + it.alias + " decides its side"}
			}
			return actionMsg{msg: slug + " and " + other + ": " + linkAllowWords(config.Link{Allow: allow})}
		})
	case "R":
		if v.sel >= len(items) || m.busy {
			break
		}
		return v.request(m, items[v.sel])
	case "F":
		if v.sel >= len(items) || m.busy {
			break
		}
		return v.files(m, items[v.sel])
	case "A":
		if v.sel >= len(items) || m.busy {
			break
		}
		other, r := items[v.sel].key, v.entry(m, items[v.sel].key)
		if !r.HasMine {
			m.msg = "link " + other + " first (enter)"
			break
		}
		ask := !r.Mine.Ask
		src, slug := m.src, v.slug
		return m.act(func() actionMsg {
			if err := src.SetLinkAsk(slug, other, ask); err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: slug + "'s messages to " + other + ": " + map[bool]string{true: "ask first", false: "sent freely"}[ask]}
		})
	}
	return nil
}

// nextFiles is the files setting after cur: ask, auto, off, ask.
func nextFiles(cur string) string {
	i := slices.Index(config.LinkFiles, cur)
	return config.LinkFiles[(i+1)%len(config.LinkFiles)]
}

// files steps what this side's link to it does with files: ask, auto,
// off (§16.13). Here the pair writer sets both sides at once. Raising it
// to auto asks first; lowering it saves at once.
func (v *linksView) files(m *dash, it linkItem) tea.Cmd {
	r := v.entry(m, it.key)
	if !r.HasMine {
		m.msg = "link " + it.key + " first (enter)"
		return nil
	}
	next := nextFiles(r.Mine.FilesMode())
	src, slug, other := m.src, v.slug, it.key
	set := func() tea.Cmd {
		return m.act(func() actionMsg {
			if err := src.SetLinkFiles(slug, other, next); err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: slug + " ⇄ " + other + ": " + filesWords(next)}
		})
	}
	mb := config.DefaultLinkFileMB
	if p := m.projectData(slug); p != nil && p.Safety != nil {
		mb = p.Safety.LinkFileMB
	}
	ts, _ := src.LinkFiles(slug, other)
	fit, over := link.CountOffers(ts, mb)
	switch {
	case next == config.FilesAuto:
		m.confirm("Take files without asking", fmt.Sprintf("Take files from %s without asking? Up to %d MB each land in %s's messages; the coordinator can read them.%s", other, mb, slug, waitingWords(fit, over, mb)), set)
	case next == config.FilesOff && fit+over > 0:
		m.confirm("Refuse files", fmt.Sprintf("Refuse files from %s? This declines %s.", other, counted(fit+over, "waiting file")), set)
	default:
		return set()
	}
	return nil
}

// waitingWords is what turning a link's files to auto does with the
// offers waiting on it: fit taken, over (above mb) left waiting.
func waitingWords(fit, over, mb int) string {
	s := ""
	if fit > 0 {
		s = " This takes " + counted(fit, "waiting file") + "."
	}
	if over > 0 {
		s += fmt.Sprintf(" %s over %d MB keeps waiting for you.", counted(over, "waiting file"), mb)
	}
	return s
}

// filesWords is a files setting in words.
func filesWords(files string) string {
	switch files {
	case config.FilesAuto:
		return "files taken without asking"
	case config.FilesOff:
		return "no files"
	}
	return "files ask first"
}

// request asks the user of the paired server it.alias to link the project back
// (link.Ask), after a y: this side's direction, both when it has none,
// which it then sets, asking first.
func (v *linksView) request(m *dash, it linkItem) tea.Cmd {
	if it.alias == "" {
		m.msg = "a link here sets both sides at once (enter); R asks a paired server's project"
		return nil
	}
	r := v.entry(m, it.key)
	if q := r.Asked; q != nil && q.Pending() {
		m.msg = "asked already (" + q.ID + "): it waits for " + it.alias + "'s user"
		return nil
	}
	allow, q := r.Mine.Allow, "Ask "+it.alias+"'s user to link "+it.key+" back with "+v.slug+"? This side is "+linkAllowWords(r.Mine)+"; they choose theirs."
	if !r.HasMine || allow == config.LinkOff {
		allow = config.LinkBoth
		q = "Link " + v.slug + " with " + it.key + " both ways on this side (asking first), and ask " + it.alias + "'s user to link back?"
	}
	src, slug, key := m.src, v.slug, it.key
	m.confirm("Ask for a link", q, func() tea.Cmd {
		return m.act(func() actionMsg {
			out, err := src.LinkAct(slug, "request", key, allow)
			if err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: out}
		})
	})
	return nil
}

// answerRequest accepts (a menu of the directions and ask, the
// request's mirror first) or declines (after a y) the paired server's
// link request on row r of project slug.
func (m *dash) answerRequest(slug string, r link.Row, accept bool) tea.Cmd {
	q := r.Asks
	if q == nil {
		m.msg = "no link request waits from " + r.Peer
		return nil
	}
	src, id := m.src, q.ID
	run := func(verb, text string) tea.Cmd {
		return m.act(func() actionMsg {
			out, err := src.LinkAct(slug, verb, id, text)
			if err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: out}
		})
	}
	if !accept {
		m.confirm("Decline a link request", "Decline "+r.Peer+"'s request to link with "+slug+"? Nothing here changes; "+r.Peer+" hears of it.", func() tea.Cmd { return run("decline", "") })
		return nil
	}
	mirror := config.Mirror(q.Allow)
	allows := []string{mirror}
	for _, a := range config.LinkAllows {
		if a != mirror {
			allows = append(allows, a)
		}
	}
	var items []menuItem
	for _, a := range allows {
		for _, ask := range []bool{true, false} {
			text := a
			if ask {
				text += " ask"
			}
			items = append(items, menuItem{label: linkAllowWords(config.Link{Allow: a, Ask: ask}), run: func(m *dash) tea.Cmd { return run("accept", text) }})
		}
	}
	m.push(&menuView{title: "Link " + slug + " with " + r.Peer + " · this side", items: items, centred: true})
	return nil
}

// linkAllowWords is an entry in words: "both ways, ask first".
func linkAllowWords(l config.Link) string {
	w := map[string]string{config.LinkBoth: "both ways", config.LinkSend: "send only", config.LinkReceive: "receive only", config.LinkOff: "off"}[l.Allow]
	if l.Ask && l.Allow != config.LinkOff {
		w += ", ask first"
	}
	return w
}

func (v *linksView) render(m *dash) string {
	lines, sel := v.lines(m, m.inner(viewWidth))
	return m.popup(box{title: "Linked projects · " + v.slug, body: lines, sel: sel, keys: "enter link: off › both › send › receive · A ask first · F files: ask › auto › off · R ask a paired server to link back · ↑ ↓ move · esc back"})
}

// lines are the list's lines in width w, and the selected one's.
func (v *linksView) lines(m *dash, w int) ([]string, int) {
	lines := []string{th().Faint.Render(fit("project", 20) + "  " + fit("this side", 22) + "  " + fit("other side", 22) + "  link")}
	items, groups := v.items(m)
	sel := -1
	row := func(i int, it linkItem) {
		r := v.entry(m, it.key)
		mine, theirs := "—", "—"
		if r.HasMine {
			mine = linkAllowWords(r.Mine)
			if f := r.Mine.FilesMode(); f != config.FilesAsk {
				mine += ", files " + f
			}
		}
		if r.HasTheirs {
			theirs = linkAllowWords(r.Theirs)
		}
		state := r.Words()
		st := th().Faint
		switch {
		case r.Send || r.Receive:
			st = th().Good
		case r.Half():
			st = th().Warn
		}
		name := it.key
		if it.alias != "" {
			// This side's entry is the link here; the other side is
			// that server's user's, known from a request's answer and
			// the messages.
			back := v.theirSide(m, items, it.key)
			name, theirs, state = "  "+it.slug, otherSide(r, back), it.alias+" decides its side"
			if r.Asks != nil {
				state = "asks to link (Messages tab: A or x)"
				st = th().Warn
			}
			if r.Paused {
				state += ", paused"
			}
			if back.Refuses && r.Send {
				st = th().Warn
			}
			if it.gone {
				state, st = "not shared any more: enter sets off", th().Warn
			}
		}
		l := fit(name, 20) + "  " + fit(mine, 22) + "  " + fit(theirs, 22) + "  "
		if i == v.sel {
			sel = len(lines)
			lines = append(lines, th().Sel.Render(fit(l+state, w)))
			return
		}
		lines = append(lines, fit(l+st.Render(state), w)+reset)
	}
	i := 0
	for ; i < len(items) && items[i].alias == ""; i++ {
		row(i, items[i])
	}
	if i == 0 {
		lines = append(lines, th().Faint.Render("no other projects"))
	}
	for _, g := range groups {
		lines = append(lines, "", th().Head.Render(g.alias)+th().Faint.Render(" · paired server, "+g.state+" · its user sets its side of each link"))
		for range g.items {
			row(i, items[i])
			i++
		}
	}
	lines = append(lines, "", th().Faint.Render("A link here is set on both sides at once: send here is receive there. With a paired server's project only this side is set, asking first; that server's user sets the other (tm project link there); R asks them to, and the answer shows in the other side's column. Ask first holds each message this coordinator sends on the link until you approve it in the Messages tab. Files (F) sent with a message wait for your accept in the Messages tab, unless set to auto; off takes and sends none."))
	return lines, sel
}

func (v *linksView) wheel(m *dash, d int) { v.key(m, arrow(d)) }

// droppable is a message the user can still drop: waiting for the OK,
// or queued over the rate (in the outbox for a paired server it has no
// due time, and has left).
func droppable(m *link.Message) bool {
	return m != nil && m.Out && (m.State == link.StateHeld || m.State == link.StateQueued && !m.Due.IsZero())
}

// fileWords is a message's file line: its name, size and state, with
// the pace and time left while it moves, "sha256 ok" once it is here.
func fileWords(msg *link.Message, t link.Transfer, ok bool) string {
	s := "▸ " + oneLine(msg.File) + "  " + link.MB(msg.FileSize) + "  "
	if !ok {
		return s + "sent with the message"
	}
	s += link.FileState(t)
	switch {
	case (t.State == link.FileReceiving || t.State == link.FileSending) && t.Rate > 0:
		left := time.Duration(float64(t.Size-t.Offset) / t.Rate * float64(time.Second)).Round(time.Second)
		s += fmt.Sprintf(" · %s/s · %s left", link.MB(int64(t.Rate)), clock(left))
	case t.State == link.FileReceived && !t.Out:
		s += " · sha256 ok"
	case t.State == link.FileFailed && t.Code != "":
		s = "▸ " + oneLine(msg.File) + "  " + link.MB(msg.FileSize) + "  failed: " + t.Code
	}
	return s
}

// clock is a duration as m:ss.
func clock(d time.Duration) string {
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// messageState is msg's state in words; other is the project at the
// other end, a paired server's "<server>/<slug>" for queued and
// undelivered. One waiting over the link's rate says when it goes.
func messageState(msg *link.Message, other string) string {
	switch msg.State {
	case link.StateHeld:
		return "waits for your OK"
	case link.StateQueued:
		if !msg.Due.IsZero() {
			return "queued over the rate, goes " + link.DueWords(msg)
		}
		alias, _, _ := config.Remote(other)
		return "queued until " + alias + " takes it"
	case link.StateUndelivered:
		return "undelivered (see the inbox)"
	}
	return msg.State
}

func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
