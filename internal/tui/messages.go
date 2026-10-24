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
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The Messages tab (docs/SPEC.md §4, §7.9): every message of the
// project's links in one list, newest first, the same for a link here
// and one to a paired server's project; a paired server's popup of its
// project has the same list, read-only, of the headers that crossed.
// Above the messages, the links that need the user (a link request, a
// paused link) are rows of their own, under a NEEDS YOU line that
// counts all that waits. enter or a click on a message opens its
// dialog (msgView): the text, the headers, the file, and the keys that
// act on it.
//
//	⚑ NEEDS YOU 2 · 1 message waits for your OK · 1 file waits for you
//	  project      id      title                                  date
//	⚑ → beta       M-3     Held one                               Oct 14 15:12
//	  beta         M-1     Hello                                  Oct 14 15:10

// msgShown is how many of the newest messages the list shows.
const msgShown = 200

// msgEntry is a message of the list: other is the project at the other
// end of its link (link.Row.Peer), file its file's transfer, if any.
type msgEntry struct {
	other   string
	msg     *link.Message
	file    link.Transfer
	hasFile bool
}

// needsYou says whether the message waits for the user: held for the OK,
// or its file offered here.
func (e msgEntry) needsYou() bool {
	return e.msg.Out && e.msg.State == link.StateHeld || e.hasFile && !e.file.Out && e.file.State == link.FileOffered
}

// msgNeed is a link that needs the user, a row above the messages: a
// paired server's link request (ask), or a link the breaker paused.
type msgNeed struct {
	row link.Row
	ask bool
}

func msgNeeds(links []link.Row) []msgNeed {
	var out []msgNeed
	for _, r := range links {
		if r.Asks != nil {
			out = append(out, msgNeed{row: r, ask: true})
		}
		if r.Paused {
			out = append(out, msgNeed{row: r})
		}
	}
	return out
}

// msgList is a project's messages, read at most every 2 s.
type msgList struct {
	slug    string
	entries []msgEntry
	err     error
	loaded  time.Time
}

func (l *msgList) stale(slug string) bool {
	return l.slug != slug || time.Since(l.loaded) > 2*time.Second
}

// load reads the messages and files of each of links of project slug.
func (l *msgList) load(m *dash, slug string, links []link.Row) {
	var es []msgEntry
	l.err = nil
	for _, r := range links {
		msgs, err := m.src.LinkLog(slug, r.Peer)
		if err != nil {
			l.err = err
			continue
		}
		files := map[string]link.Transfer{}
		if ts, err := m.src.LinkFiles(slug, r.Peer); err == nil {
			for _, t := range ts {
				files[t.Message] = t
			}
		}
		for _, msg := range msgs {
			e := msgEntry{other: r.Peer, msg: msg}
			if msg.File != "" {
				e.file, e.hasFile = files[msg.ID]
			}
			es = append(es, e)
		}
	}
	sortMsgs(es)
	l.slug, l.entries, l.loaded = slug, es, time.Now()
}

// sortMsgs puts the newest first; of two at the same time, the later in
// its log.
func sortMsgs(es []msgEntry) {
	slices.Reverse(es)
	slices.SortStableFunc(es, func(a, b msgEntry) int { return b.msg.Created.Compare(a.msg.Created) })
}

// remoteMsgEntries are the headers a paired server's popup carries, as
// the list's entries: no text, no file.
func remoteMsgEntries(ls []proto.PeerPopupLink) []msgEntry {
	var es []msgEntry
	for _, l := range ls {
		for _, h := range l.Messages {
			es = append(es, msgEntry{other: l.Project, msg: &link.Message{ID: h.ID, Out: h.Out, Created: h.Created,
				Re: h.Re, Bytes: h.Bytes, State: h.State, By: h.By, Peer: l.Project}})
		}
	}
	sortMsgs(es)
	return es
}

// countedAs is n things in words: "1 file waits", "2 files wait".
func countedAs(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// needsLine is the NEEDS YOU line over the list: how many things wait
// for the user, and what; "" for none.
func needsLine(links []link.Row) string { return needsLineShares(links, 0) }

// needsLineShares is needsLine with the paired servers' share requests
// that wait (the Network popup's).
func needsLineShares(links []link.Row, shares int) string {
	var held, files, paused, asks int
	for _, r := range links {
		held += r.Held
		files += r.FilesWaiting
		if r.Paused {
			paused++
		}
		if r.Asks != nil {
			asks++
		}
	}
	var words []string
	if held > 0 {
		words = append(words, countedAs(held, "message waits for your OK", "messages wait for your OK"))
	}
	if files > 0 {
		words = append(words, countedAs(files, "file waits for you", "files wait for you"))
	}
	if paused > 0 {
		words = append(words, countedAs(paused, "paused link", "paused links"))
	}
	if asks > 0 {
		words = append(words, countedAs(asks, "link request", "link requests"))
	}
	if shares > 0 {
		words = append(words, countedAs(shares, "share request", "share requests"))
	}
	if len(words) == 0 {
		return ""
	}
	return th().Warn.Render(fmt.Sprintf("⚑ NEEDS YOU %d", held+files+paused+asks+shares)) + th().Faint.Render(" · "+strings.Join(words, " · "))
}

// msgTitle is a message's first line of text, or for one without (a
// header from a paired server) a dash and its size.
func msgTitle(msg *link.Message) string {
	for l := range strings.SplitSeq(msg.Text, "\n") {
		if l = strings.TrimSpace(oneLine(ansi.Strip(l))); l != "" {
			return l
		}
	}
	return th().Faint.Render("— " + sizeWords(int64(msg.Bytes)))
}

// msgDate is when a message was written, as the list shows it.
func msgDate(t time.Time) string { return t.Local().Format("Jan 2 15:04") }

// msgLines are the list's lines in width w: the NEEDS YOU line and the
// rows of links (whose needs these are; nil in a paired server's popup),
// then the newest msgShown of es, a paired server by its display name
// in peers (nil names none). sel indexes the link rows, then the
// messages. It returns the lines, the selected one's and what each
// picks.
func msgLines(peers []proto.PeerMirror, links []link.Row, es []msgEntry, sel, w int) ([]string, int, []int) {
	var lines []string
	var hits []int
	at := -1
	add := func(l string, hit int) {
		lines, hits = append(lines, l), append(hits, hit)
	}
	needs := msgNeeds(links)
	shown := es[:min(len(es), msgShown)]
	// The project column fits the longest name, a paired server's
	// "<server>/<slug>" whole, up to a third of the width; the id
	// column the longest id.
	pc, ic := len("project"), len("id")
	for _, n := range needs {
		pc = max(pc, ansi.StringWidth(peerShownKey(peers, n.row.Peer)))
	}
	for _, e := range shown {
		name := peerShownKey(peers, e.other)
		if e.msg.Out {
			name = "→ " + name
		}
		pc = max(pc, ansi.StringWidth(name))
		ic = max(ic, ansi.StringWidth(oneLine(e.msg.ID)))
	}
	pc, ic = min(pc, max(w/3, 12)), min(ic, 16)
	const dc = 12 // "Oct 14 15:12"
	tc := max(w-2-pc-2-ic-2-2-dc, 8)
	row := func(i int, mark, text string) {
		if i == sel {
			at = len(lines)
			add(th().Sel.Render(fit(mark+ansi.Strip(text), w)), i)
			return
		}
		if mark != "  " {
			mark = th().Warn.Render(mark)
		}
		add(fit(mark+text, w)+reset, i)
	}
	if l := needsLine(links); l != "" {
		add(fit(l, w)+reset, noHit)
	}
	for i, n := range needs {
		words, keys := "link paused: too many messages without you", " · R resumes it"
		if n.ask {
			words, keys = "asks to link ("+oneLine(n.row.Asks.ID)+")", " · A accept · X decline"
		}
		row(i, "⚑ ", th().Warn.Render(fit(peerShownKey(peers, n.row.Peer), pc)+"  "+words)+th().Faint.Render(keys))
	}
	if len(needs) > 0 {
		add("", noHit)
	}
	if len(shown) > 0 {
		add(th().Faint.Render(fit("  "+fit("project", pc)+"  "+fit("id", ic)+"  "+fit("title", tc)+"  "+"date", w)), noHit)
	}
	for j, e := range shown {
		name := peerShownKey(peers, e.other)
		if e.msg.Out {
			name = "→ " + name
		}
		mark := "  "
		if e.needsYou() {
			mark = "⚑ "
		}
		text := fit(name, pc) + "  " + fit(oneLine(e.msg.ID), ic) + "  " + fit(msgTitle(e.msg), tc) + reset + "  " + th().Faint.Render(msgDate(e.msg.Created))
		row(len(needs)+j, mark, text)
	}
	if len(es) == 0 {
		add(th().Faint.Render("no messages yet"), noHit)
	}
	if more := len(es) - len(shown); more > 0 {
		add("", noHit)
		add(th().Faint.Render(fmt.Sprintf("%s not shown; tm link log <project> lists every one", countedAs(more, "older message", "older messages"))), noHit)
	}
	return lines, at, hits
}

// msgView is a message's dialog over the Messages tab: who it went
// between, when, its id, what it answers, its state, its attachments and
// file with the transfer's state, then its text, scrolling. On a
// message here the keys act on it; one from a paired server's popup
// (alias set) is a header only and read-only.
type msgView struct {
	// slug is the project whose list it is from: a paired server's
	// "<server>/<slug>" in its popup; other the one at the other end.
	slug, other string
	e           msgEntry
	alias       string
	err         error
	loaded      time.Time
	scroll      int
}

// openMessage opens e's dialog over the list of project slug; alias is
// the paired server whose popup the list is, "" for one here.
func (m *dash) openMessage(slug string, e msgEntry, alias string) tea.Cmd {
	m.push(&msgView{slug: slug, other: e.other, e: e, alias: alias, loaded: time.Now()})
	return nil
}

// load reads the message and its file again.
func (v *msgView) load(m *dash) {
	v.loaded = time.Now()
	msgs, err := m.src.LinkLog(v.slug, v.other)
	if v.err = err; err != nil {
		return
	}
	for _, msg := range msgs {
		if msg.ID == v.e.msg.ID {
			v.e.msg = msg
		}
	}
	v.e.hasFile = false
	if v.e.msg.File == "" {
		return
	}
	if ts, err := m.src.LinkFiles(v.slug, v.other); err == nil {
		for _, t := range ts {
			if t.Message == v.e.msg.ID {
				v.e.file, v.e.hasFile = t, true
			}
		}
	}
}

// row is the link the message went over, as the last poll has it.
func (v *msgView) row(m *dash) link.Row {
	if p := m.projectData(v.slug); p != nil && v.alias == "" {
		for _, r := range p.Links {
			if r.Peer == v.other {
				return r
			}
		}
	}
	return link.Row{Peer: v.other}
}

// reloaded is what runs after an act: the dialog and the list read
// again.
func (v *msgView) reloaded(m *dash) {
	v.load(m)
	m.reloadMessages()
}

func (v *msgView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if d, ok := scrollKeys[s]; ok {
		v.scroll = max(v.scroll+d, 0)
		return nil
	}
	switch s {
	case "esc", "q":
		m.pop()
		return nil
	case "home":
		v.scroll = 0
		return nil
	}
	if v.alias != "" {
		if strings.Contains("AXCRM", s) && len(s) == 1 || s == "enter" {
			m.msg = "the links of " + peerShownKey(m.data.Peers, v.slug) + " are " + v.alias + "'s: change it on " + v.alias
		}
		return nil
	}
	act := func(verb, arg, text, done string) tea.Cmd {
		src, slug := m.src, v.slug
		return m.act(func() actionMsg {
			out, err := src.LinkAct(slug, verb, arg, text)
			if err != nil {
				return actionMsg{err: err, then: v.reloaded}
			}
			return actionMsg{msg: strings.TrimSpace(cmpStr(out, done)), then: v.reloaded}
		})
	}
	cur, t, hasFile := v.e.msg, v.e.file, v.e.hasFile
	other := peerShownKey(m.data.Peers, v.other)
	switch s {
	case "A", "X":
		if hasFile && !t.Out && t.State == link.FileOffered && !(cur.Out && cur.State == link.StateHeld) {
			if s == "A" {
				return act("accept-file", t.ID, "", "accepted "+t.ID)
			}
			id := t.ID
			m.confirm("Decline a file", "Decline "+oneLine(t.Name)+" ("+link.MB(t.Size)+") from "+other+"? "+other+" hears of it.", func() tea.Cmd { return act("decline-file", id, "", "declined "+id) })
			return nil
		}
		held := cur.Out && cur.State == link.StateHeld
		if !held && (s == "A" || !droppable(cur)) {
			m.msg = "only a message that waits for your OK can be approved, or one that waits over the rate dropped, or a file offered to you answered"
			return nil
		}
		if s == "A" {
			return act("approve", cur.ID, "", "sent "+cur.ID)
		}
		id := cur.ID
		m.confirm("Drop a message", "Drop "+id+" to "+other+"? It is never sent.", func() tea.Cmd { return act("drop", id, "", "dropped "+id) })
	case "C":
		if !hasFile || !t.Open() {
			m.msg = "only a file still on its way can be cancelled"
			return nil
		}
		id := t.ID
		m.confirm("Cancel a file", "Cancel "+oneLine(t.Name)+" ("+link.MB(t.Size)+")? "+other+" hears of it; nothing partial is kept.", func() tea.Cmd { return act("cancel-file", id, "", "cancelled "+id) })
	case "enter":
		if hasFile && !t.Out && t.State == link.FileReceived {
			return m.openLinkFile(v.slug, v.other, cur)
		}
	case "R":
		if !v.row(m).Paused {
			m.msg = "the link isn't paused"
			return nil
		}
		return act("resume", v.other, "", "resumed the link with "+other)
	}
	return nil
}

func (v *msgView) wheel(m *dash, d int) { v.scroll = max(v.scroll+3*d, 0) }

// ends are the projects the message went from and to, each with who
// wrote it when the user did: "(you)" on this server's side, "(its
// user)" on a paired server's.
func (v *msgView) ends() (string, string) {
	msg := v.e.msg
	// In a paired server's popup its project (slug) is the far one,
	// else the other end is when it names a paired server.
	_, _, otherFar := config.Remote(v.other)
	from, fromFar, to := v.other, otherFar, v.slug
	if v.alias != "" {
		fromFar = false
	}
	if msg.Out {
		from, fromFar, to = v.slug, v.alias != "", v.other
	}
	switch {
	case msg.By != "human":
	case fromFar:
		from += " (its user)"
	default:
		from += " (you)"
	}
	return from, to
}

func (v *msgView) lines(m *dash, w int) []string {
	msg := v.e.msg
	field := func(label, value string) string {
		return fit(th().Faint.Render(fmt.Sprintf("%-10s", label))+value, w) + reset
	}
	from, to := v.ends()
	from, to = peerShownKey(m.data.Peers, from), peerShownKey(m.data.Peers, to)
	state := messageState(msg, peerShownKey(m.data.Peers, v.other))
	if msg.Refused != "" {
		state += " · " + oneLine(msg.Refused)
	}
	if msg.State == link.StateHeld {
		state = th().Warn.Render(state)
	}
	out := []string{
		field("From", from),
		field("To", to),
		field("Date", msg.Created.Local().Format("Mon Jan 2 2006 15:04")),
		field("Id", oneLine(msg.ID)),
	}
	if msg.Re != "" {
		out = append(out, field("Re", oneLine(msg.Re)))
	}
	out = append(out, field("State", state), field("Size", sizeWords(int64(msg.Bytes))))
	if len(msg.Attachments) > 0 {
		out = append(out, field("Attached", oneLine(strings.Join(msg.Attachments, ", "))))
	}
	if msg.File != "" {
		words := fileWords(msg, v.e.file, v.e.hasFile)
		if v.e.hasFile && !v.e.file.Out && v.e.file.State == link.FileOffered {
			words = th().Warn.Render(words)
		}
		out = append(out, field("File", words))
	}
	if v.err != nil {
		out = append(out, th().Bad.Render(fit("error: "+oneLine(v.err.Error()), w)))
	}
	out = append(out, "")
	if v.alias != "" {
		return append(out, th().Faint.Render(fit("Only its header crosses; its text is in "+peerShownKey(m.data.Peers, v.other)+"'s Messages tab here.", w)))
	}
	for _, para := range strings.Split(msg.Text, "\n") {
		out = append(out, wrapLines(ansi.Strip(para), w)...)
	}
	return out
}

// keys are the dialog's key bar: what applies to the message.
func (v *msgView) keys(m *dash) string {
	if v.alias != "" {
		return "↑ ↓ scroll · esc close"
	}
	var keys []string
	cur, t, hasFile := v.e.msg, v.e.file, v.e.hasFile
	if cur.Out && cur.State == link.StateHeld {
		keys = append(keys, "A send", "X drop")
	} else if droppable(cur) {
		keys = append(keys, "X drop")
	}
	if hasFile {
		switch {
		case !t.Out && t.State == link.FileOffered:
			keys = append(keys, "A accept the file", "X decline")
		case !t.Out && t.State == link.FileReceived:
			keys = append(keys, "enter read the file")
		}
		if t.Open() {
			keys = append(keys, "C cancel the file")
		}
	}
	if v.row(m).Paused {
		keys = append(keys, "R resume the link")
	}
	return strings.Join(append(keys, "↑ ↓ scroll", "esc close"), " · ")
}

func (v *msgView) box(m *dash) box {
	if v.alias == "" && time.Since(v.loaded) > 2*time.Second {
		v.load(m)
	}
	from, to := peerShownKey(m.data.Peers, v.other), peerShownKey(m.data.Peers, v.slug)
	if v.e.msg.Out {
		from, to = to, from
	}
	b := box{title: oneLine(v.e.msg.ID) + " · " + from + " → " + to, body: v.lines(m, m.inner(viewWidth)), sel: -1, keys: v.keys(m)}
	v.scroll = min(v.scroll, max(len(b.body)-m.boxRows(b), 0))
	b.scroll = v.scroll
	return b
}

func (v *msgView) render(m *dash) string { return m.popup(v.box(m)) }

// writeMessage asks for a message the user writes for project slug, to
// one of its links that send: the selected row's (first) when it has
// one, else a menu of them.
func (m *dash) writeMessage(slug string, links []link.Row, first string) tea.Cmd {
	var to []string
	for _, r := range links {
		if r.Send {
			to = append(to, r.Peer)
		}
	}
	if len(to) == 0 {
		m.msg = slug + " has no link it may write on; the Network popup (N) sets them"
		return nil
	}
	write := func(other string) tea.Cmd {
		m.prompt("Write to "+other, "Your message, sent as the user for "+slug+":", "", func(text string) tea.Cmd {
			if strings.TrimSpace(text) == "" {
				return nil
			}
			src := m.src
			return m.act(func() actionMsg {
				out, err := src.LinkAct(slug, "send", other, text)
				if err != nil {
					return actionMsg{err: err, then: func(m *dash) { m.reloadMessages() }}
				}
				return actionMsg{msg: strings.TrimSpace(out), then: func(m *dash) { m.reloadMessages() }}
			})
		})
		return nil
	}
	if len(to) == 1 {
		return write(to[0])
	}
	if i := slices.Index(to, first); i > 0 {
		to = append(append([]string{first}, to[:i]...), to[i+1:]...)
	}
	var items []menuItem
	for _, other := range to {
		items = append(items, menuItem{label: other, run: func(m *dash) tea.Cmd { return write(other) }})
	}
	m.push(&menuView{title: "Write as you for " + slug + " to", items: items, centred: true})
	return nil
}

// reloadMessages has the open popup's Messages tab read its messages
// again at its next drawing.
func (m *dash) reloadMessages() {
	if pv := m.projectPopupView(); pv != nil {
		pv.msgs.loaded = time.Time{}
	}
	if nv := m.network(); nv != nil {
		nv.relog(m)
	}
}

// resumeLink resumes project slug's link with other, which the breaker
// paused.
func (m *dash) resumeLink(slug, other string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		out, err := src.LinkAct(slug, "resume", other, "")
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{msg: strings.TrimSpace(cmpStr(out, "resumed the link with "+other)), then: func(m *dash) { m.reloadMessages() }}
	})
}

// openLinkNeed opens what project slug's link to other waits on, as
// NEEDS YOU's row of kind need (needHeld …) says: a held message's or
// a file's dialog (its first, oldest), else the Network popup on that
// row.
func (m *dash) openLinkNeed(slug, other string, need int) tea.Cmd {
	if need == needAsk || need == needPaused {
		m.openNetwork(needKey(slug, other, need, ""))
		return nil
	}
	msgs, err := m.src.LinkLog(slug, other)
	if err != nil {
		m.fail(err)
		return nil
	}
	if need == needHeld {
		for _, msg := range msgs {
			if msg.Out && msg.State == link.StateHeld {
				return m.openMessage(slug, msgEntry{other: other, msg: msg}, "")
			}
		}
		m.msg = "no message to " + peerShownKey(m.data.Peers, other) + " waits for your OK now"
		return nil
	}
	ts, _ := m.src.LinkFiles(slug, other)
	for _, t := range ts {
		if t.Out || t.State != link.FileOffered || t.Peer != "" && t.Peer != other {
			continue
		}
		return m.openMessage(slug, fileEntry(other, t, msgs), "")
	}
	m.msg = "no file from " + peerShownKey(m.data.Peers, other) + " waits for you now"
	return nil
}

// fileEntry is file t offered on the link to other as its message's
// entry, the message from msgs (just its file when it isn't there).
func fileEntry(other string, t link.Transfer, msgs []*link.Message) msgEntry {
	e := msgEntry{other: other, msg: &link.Message{ID: t.Message, Peer: other, File: t.Name, FileSize: t.Size}, file: t, hasFile: true}
	for _, msg := range msgs {
		if msg.ID == t.Message && !msg.Out {
			e.msg = msg
		}
	}
	return e
}
