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
// Settings tab's Linked projects row, a summary whose enter opens the
// Network popup on the project, where each link is set
// (networklinks.go); the acts on a link that the popup and the Messages
// tab share; and what the Messages tab (messages.go) says of a message
// and its file. The text of a message is shown to the human only;
// coordinators read it framed, with tm link read.

// linkRow is the Settings tab's Linked projects row of project slug.
func linkRow(slug string) setting {
	return setting{label: "Linked projects", help: "The other projects whose coordinators this one may write to and hear from (tm link), with ⚑ when one needs you. Enter opens the Network popup on this project, where each link is set: one act sets both projects here, send here is receive there; with a paired server's project, only this side.",
		value: func(m *dash) string { return linksWords(m.data.Peers, m.projectData(slug)) },
		change: func(m *dash) tea.Cmd {
			m.openNetwork("p:" + slug)
			return nil
		}}
}

// linksWords is the Linked projects row's value: each link with its
// direction, a paired server's project by the server's display name in
// peers, ⚑ after one that needs you; or none.
func linksWords(peers []proto.PeerMirror, p *ProjectData) string {
	if p == nil {
		return "none"
	}
	var parts []string
	for _, r := range p.Links {
		var part string
		switch {
		case r.Send || r.Receive:
			part = peerShownKey(peers, r.Peer) + " " + r.Arrow()
		case r.Half():
			part = peerShownKey(peers, r.Peer) + " (half-linked)"
		case r.Asks != nil:
			part = peerShownKey(peers, r.Peer) + " asks"
		default:
			continue
		}
		if r.Held+r.FilesWaiting > 0 || r.Paused || r.Asks != nil {
			part += " ⚑"
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " · ")
}

// otherSide is what is known of a link's other side on a paired
// server: the answer to this side's request while nothing newer tells
// otherwise, else what the messages say (back).
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

// nextFiles is the files setting after cur: ask, auto, off, ask.
func nextFiles(cur string) string {
	i := slices.Index(config.LinkFiles, cur)
	return config.LinkFiles[(i+1)%len(config.LinkFiles)]
}

// filesAsk is the question raising a link's files from from to to asks
// first (§16.13), "" for none: taking them without asking does, and so
// does refusing them while offers wait; title is its dialog's.
func filesAsk(m *dash, slug, other, from, to string) (title, question string) {
	if from == to {
		return "", ""
	}
	shown := peerShownKey(m.data.Peers, other)
	mb := config.DefaultLinkFileMB
	if p := m.projectData(slug); p != nil && p.Safety != nil {
		mb = p.Safety.LinkFileMB
	}
	ts, _ := m.src.LinkFiles(slug, other)
	fit, over := link.CountOffers(ts, mb)
	switch {
	case to == config.FilesAuto:
		return "Take files without asking", fmt.Sprintf("Take files from %s without asking? Up to %d MB each land in %s's messages; the coordinator can read them.%s", shown, mb, slug, waitingWords(fit, over, mb))
	case to == config.FilesOff && fit+over > 0:
		return "Refuse files", fmt.Sprintf("Refuse files from %s? This declines %s.", shown, counted(fit+over, "waiting file"))
	}
	return "", ""
}

// setFiles sets what slug's link to other does with files (the pair
// writer sets both sides here; to a paired server's project this side
// only), asking first as filesAsk says; then runs after it.
func (m *dash) setFiles(slug, other, to string, then func(*dash)) tea.Cmd {
	from := config.FilesAsk
	if p := m.projectData(slug); p != nil {
		for _, r := range p.Links {
			if r.Peer == other {
				from = r.Mine.FilesMode()
			}
		}
	}
	src, shown := m.src, peerShownKey(m.data.Peers, other)
	set := func() tea.Cmd {
		return m.act(func() actionMsg {
			if err := src.SetLinkFiles(slug, other, to); err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: slug + " ← " + shown + ": files: " + filesPlain[to], then: then}
		})
	}
	if title, q := filesAsk(m, slug, other, from, to); q != "" {
		m.confirmNo(title, q, "files unchanged", set)
		return nil
	}
	return set()
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

// askLinkBack asks the user of the paired server of link r of project
// slug to link back (link.Ask), after a Y: this side's direction, both
// when it has none, which it then sets, asking first.
func (m *dash) askLinkBack(slug string, r link.Row, then func(*dash)) tea.Cmd {
	alias, other, _ := config.Remote(r.Peer)
	name := peerShown(m.data.Peers, alias)
	if q := r.Asked; q != nil && q.Pending() {
		m.msg = "asked already (" + q.ID + "): it waits for " + name + "'s user"
		return nil
	}
	allow, q := r.Mine.Allow, "Ask "+name+"'s user to link "+name+"/"+other+" back with "+slug+"? This side is "+linkAllowWords(r.Mine)+"; they choose theirs."
	if !r.HasMine || allow == config.LinkOff {
		allow = config.LinkBoth
		q = "Link " + slug + " with " + name + "/" + other + " both ways on this side (asking first), and ask " + name + "'s user to link back?"
	}
	src, key := m.src, r.Peer
	m.confirmNo("Ask for a link", q, "not asked", func() tea.Cmd {
		return m.act(func() actionMsg {
			out, err := src.LinkAct(slug, "request", key, allow)
			if err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: out, then: then}
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
	s += filePlain(t)
	switch {
	case (t.State == link.FileReceiving || t.State == link.FileSending) && t.Rate > 0:
		left := time.Duration(float64(t.Size-t.Offset) / t.Rate * float64(time.Second)).Round(time.Second)
		s += fmt.Sprintf(" · %s/s · %s left", link.MB(int64(t.Rate)), clock(left))
	case t.State == link.FileReceived && !t.Out:
		s += " · sha256 ok"
	case t.State == link.FileFailed && t.Code != "":
		s = "▸ " + oneLine(msg.File) + "  " + link.MB(msg.FileSize) + "  " + filePlain(t)
	}
	return s
}

// filePlain is a file's state as the screen says it, with the link's
// files setting in its plain words where the state names it (the
// config and the CLI keep ask, auto and off).
func filePlain(t link.Transfer) string {
	switch {
	case t.State == link.FileOffered && !t.Out:
		return "offered · waits for you (files: " + filesPlain[config.FilesAsk] + ")"
	case t.State == link.FileAccepted && t.Auto:
		return "accepted (files: " + filesPlain[config.FilesAuto] + ")"
	case t.Code == "files-off":
		return t.State + " (files: " + filesPlain[config.FilesOff] + ")"
	}
	return link.FileState(t)
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
