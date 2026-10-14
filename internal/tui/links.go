package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
)

// Links between projects' coordinators (docs/SPEC.md §7.9, §4): the
// Settings tab's Linked projects row and its list, where the human sets
// this side of each link; and the Messages tab, each link's conversation,
// where the human approves or drops a held message, resumes a paused
// link or writes on the project's behalf. The text of a message is shown
// to the human only; coordinators read it framed, with tm link read.

// linkRow is the Settings tab's Linked projects row of project slug.
func linkRow(slug string) setting {
	return setting{label: "Linked projects", help: "The other projects whose coordinators this one may write to and hear from (tm link). Linking sets both projects at once: send here is receive there. Enter lists the projects.",
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
// of config.toml) shows as such, and enter makes it whole.
type linksView struct {
	slug string
	sel  int
}

// others are the projects the list shows: every project but this one.
func (v *linksView) others(m *dash) []string {
	var out []string
	for _, p := range m.data.Projects {
		if p.Slug != v.slug {
			out = append(out, p.Slug)
		}
	}
	slices.Sort(out)
	return out
}

// entry is this side's and the other side's entries for other, from the
// last poll.
func (v *linksView) entry(m *dash, other string) (row link.Row) {
	row.Peer = other
	if p := m.projectData(v.slug); p != nil {
		for _, r := range p.Links {
			if r.Peer == other {
				return r
			}
		}
	}
	return row
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
	others := v.others(m)
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "k", "down", "j":
		v.sel = moveSel(v.sel, scrollKeys[s], len(others))
	case "enter", "space", " ":
		if v.sel >= len(others) || m.busy {
			break
		}
		// One act sets both sides: this side's direction, the other's
		// the mirror; off removes both.
		other, r := others[v.sel], v.entry(m, others[v.sel])
		allow := nextAllow(r.Mine.Allow, r.HasMine)
		src, slug := m.src, v.slug
		return m.act(func() actionMsg {
			if err := src.SetLink(slug, other, allow); err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: slug + " and " + other + ": " + linkAllowWords(config.Link{Allow: allow})}
		})
	case "a":
		if v.sel >= len(others) || m.busy {
			break
		}
		other, r := others[v.sel], v.entry(m, others[v.sel])
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

// linkAllowWords is an entry in words: "both ways, ask first".
func linkAllowWords(l config.Link) string {
	w := map[string]string{config.LinkBoth: "both ways", config.LinkSend: "send only", config.LinkReceive: "receive only", config.LinkOff: "off"}[l.Allow]
	if l.Ask && l.Allow != config.LinkOff {
		w += ", ask first"
	}
	return w
}

func (v *linksView) render(m *dash) string {
	w := m.inner(viewWidth)
	lines := []string{th().Faint.Render(fit("project", 20) + "  " + fit("this side", 22) + "  " + fit("other side", 22) + "  link")}
	others := v.others(m)
	sel := -1
	for i, o := range others {
		r := v.entry(m, o)
		mine, theirs := "—", "—"
		if r.HasMine {
			mine = linkAllowWords(r.Mine)
		}
		if r.HasTheirs {
			theirs = linkAllowWords(r.Theirs)
		}
		state := r.Words()
		l := fit(o, 20) + "  " + fit(mine, 22) + "  " + fit(theirs, 22) + "  "
		if i == v.sel {
			sel = len(lines)
			lines = append(lines, th().Sel.Render(fit(l+state, w)))
			continue
		}
		st := th().Faint
		switch {
		case r.Send || r.Receive:
			st = th().Good
		case r.Half():
			st = th().Warn
		}
		lines = append(lines, l+st.Render(state))
	}
	if len(others) == 0 {
		lines = append(lines, th().Faint.Render("no other projects"))
	}
	lines = append(lines, "", th().Faint.Render("A link is set on both sides at once: send here is receive there. Ask first holds each message this coordinator sends on the link until you approve it in the Messages tab."))
	return m.popup(box{title: "Linked projects · " + v.slug, body: lines, sel: sel, keys: "enter link: off › both › send › receive · a ask first · ↑ ↓ move · esc back"})
}

func (v *linksView) wheel(m *dash, d int) { v.key(m, arrow(d)) }

// messagesLines are the Messages tab's lines: one row per link, sel
// selected, and the row on each line.
func messagesLines(p ProjectData, sel, w int) ([]string, int, []int) {
	var lines []string
	var hits []int
	at := -1
	for i, r := range p.Links {
		counts := ""
		if r.Unread > 0 {
			counts += fmt.Sprintf(" · %d unread", r.Unread)
		}
		if r.Held > 0 {
			counts += fmt.Sprintf(" · %d waits for you", r.Held)
		}
		if r.Last != "" {
			counts += " · last " + r.Last + " " + age(time.Since(r.LastAt))
		}
		text := fit(r.Peer, 20) + "  " + r.Arrow() + " " + r.Words()
		hits = append(hits, i)
		if i == sel {
			at = len(lines)
			lines = append(lines, th().Sel.Render(fit(text+counts, w)))
			continue
		}
		st := th().Head
		if r.Held > 0 || r.Paused {
			st = th().Warn
		}
		lines = append(lines, fit(st.Render(text)+th().Faint.Render(counts), w)+reset)
	}
	if len(p.Links) == 0 {
		lines = append(lines, th().Faint.Render("no links; the Settings tab's Linked projects sets them"))
		hits = append(hits, noHit)
	}
	return lines, at, hits
}

// convView is one link's conversation: every message both ways, oldest
// first, ours marked →. A held message waits for the human's A or x.
type convView struct {
	slug, other string
	msgs        []*link.Message
	err         error
	loaded      time.Time
	sel         int
}

func (m *dash) openConversation(slug, other string) tea.Cmd {
	v := &convView{slug: slug, other: other}
	v.load(m)
	v.sel = max(len(v.msgs)-1, 0)
	m.push(v)
	return nil
}

func (v *convView) load(m *dash) {
	v.msgs, v.err = m.src.LinkLog(v.slug, v.other)
	v.loaded = time.Now()
	v.sel = min(v.sel, max(len(v.msgs)-1, 0))
}

func (v *convView) row(m *dash) link.Row {
	if p := m.projectData(v.slug); p != nil {
		for _, r := range p.Links {
			if r.Peer == v.other {
				return r
			}
		}
	}
	return link.Row{Peer: v.other}
}

func (v *convView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	act := func(verb, arg, text, done string) tea.Cmd {
		src, slug := m.src, v.slug
		return m.act(func() actionMsg {
			out, err := src.LinkAct(slug, verb, arg, text)
			if err != nil {
				return actionMsg{err: err, then: v.load}
			}
			return actionMsg{msg: strings.TrimSpace(cmpStr(out, done)), then: v.load}
		})
	}
	var cur *link.Message
	if v.sel < len(v.msgs) {
		cur = v.msgs[v.sel]
	}
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "k", "down", "j", "pgup", "pgdown":
		v.sel = moveSel(v.sel, scrollKeys[s], len(v.msgs))
	case "A", "x":
		if cur == nil || !cur.Out || cur.State != link.StateHeld {
			m.msg = "only a message that waits for your OK can be approved or dropped"
			return nil
		}
		if s == "A" {
			return act("approve", cur.ID, "", "sent "+cur.ID)
		}
		id := cur.ID
		m.confirm("Drop a message", "Drop "+id+" to "+v.other+"? It is never sent.", func() tea.Cmd { return act("drop", id, "", "dropped "+id) })
	case "R":
		if !v.row(m).Paused {
			m.msg = "the link isn't paused"
			return nil
		}
		return act("resume", v.other, "", "resumed the link with "+v.other)
	case "m":
		if !v.row(m).Send {
			m.msg = v.slug + " can't write to " + v.other + ": " + v.row(m).Words()
			return nil
		}
		other := v.other
		m.prompt("Write to "+other, "Your message, sent as the user for "+v.slug+":", "", func(text string) tea.Cmd {
			if strings.TrimSpace(text) == "" {
				return nil
			}
			return act("send", other, text, "")
		})
	}
	return nil
}

func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (v *convView) render(m *dash) string {
	if time.Since(v.loaded) > 2*time.Second {
		v.load(m)
	}
	w := m.inner(viewWidth)
	r := v.row(m)
	head := r.Arrow() + " " + r.Words()
	var lines []string
	sel := -1
	if v.err != nil {
		lines = append(lines, th().Bad.Render("error: "+oneLine(v.err.Error())))
	}
	for i, msg := range v.msgs {
		if i > 0 {
			lines = append(lines, "")
		}
		dir, who := "←", v.other
		if msg.Out {
			dir, who = "→", v.slug
		}
		if msg.By == "human" {
			who += " (you)"
		}
		meta := fmt.Sprintf("%s %s · %s · %s", dir, msg.ID, msg.Created.Local().Format("Jan 2 15:04"), who)
		if msg.Re != "" {
			meta += " · re " + msg.Re
		}
		state := map[string]string{link.StateSent: "sent", link.StateRead: "read", link.StateHeld: "waits for your OK", link.StateDropped: "dropped"}[msg.State]
		meta += " · " + state
		if len(msg.Attachments) > 0 {
			meta += " · " + strings.Join(msg.Attachments, ", ")
		}
		if i == v.sel {
			sel = len(lines)
			lines = append(lines, th().Sel.Render(fit(meta, w)))
		} else if msg.State == link.StateHeld {
			lines = append(lines, th().Warn.Render(fit(meta, w)))
		} else {
			lines = append(lines, th().Faint.Render(fit(meta, w)))
		}
		indent := "  "
		if msg.Out {
			indent = "      "
		}
		for _, para := range strings.Split(msg.Text, "\n") {
			for _, l := range wrapLines(ansi.Strip(para), w-len(indent)) {
				lines = append(lines, indent+l)
			}
		}
		if i == v.sel {
			sel = len(lines) - 1
		}
	}
	if len(v.msgs) == 0 && v.err == nil {
		lines = append(lines, th().Faint.Render("no messages yet"))
	}
	keys := []string{}
	if v.sel < len(v.msgs) && v.msgs[v.sel].Out && v.msgs[v.sel].State == link.StateHeld {
		keys = append(keys, "A approve", "x drop")
	}
	if r.Paused {
		keys = append(keys, "R resume")
	}
	if r.Send {
		keys = append(keys, "m write")
	}
	keys = append(keys, "↑ ↓ move", "esc back")
	return m.popup(box{title: v.slug + " · " + v.other + " · " + head, body: lines, sel: sel, keys: strings.Join(keys, " · ")})
}

func (v *convView) wheel(m *dash, d int) { v.key(m, arrow(d)) }
