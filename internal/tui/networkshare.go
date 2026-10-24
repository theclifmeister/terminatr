package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Share requests in the Network popup (docs/SPEC.md §16.9, Asking for a
// share): S on a paired server asks its user to share a project with
// this server, for one of yours or none in particular, with an optional
// note. The request waits under that server until it is answered:
//
//	"     └─ asked to share a project (S-4F2A-33, for acme)    waiting on mini's user"
//
// One from a paired server hangs under it, flagged, and counts with what
// needs you; A picks one of your active projects that isn't shared, and
// the usual share confirm shares it with every paired server; X
// declines. A request asks; only that Y shares.
//
//	"     └─⚑ asks you to share a project (for its acme) · "the API docs""

// shareRows are the share requests under paired server i (its mirror
// pm): yours while it waits, or how it ended unless it was accepted (the
// project it shared shows above); theirs while it waits for you.
func shareRows(pm *proto.PeerMirror, i int) []netRow {
	if pm == nil {
		return nil
	}
	var out []netRow
	for j := range pm.Shares {
		q := &pm.Shares[j]
		switch {
		case q.Out && q.Pending():
			out = append(out, netRow{kind: netAsked, peer: i, share: q})
		case q.Out && q.State != proto.ShareIsAccepted && q.Summary != "":
			out = append(out, netRow{kind: netNote, peer: i, text: q.Summary})
		case !q.Out && q.Pending():
			out = append(out, netRow{kind: netShareReq, peer: i, share: q})
		}
	}
	for j := len(out) - 1; j >= 0; j-- {
		if out[j].kind != netNote {
			out[j].last = true
			break
		}
	}
	return out
}

// asking is the request this server's user sent pm's server that still
// waits, nil for none.
func asking(pm *proto.PeerMirror) *proto.ShareRequest {
	if pm == nil {
		return nil
	}
	for j := range pm.Shares {
		if q := &pm.Shares[j]; q.Out && q.Pending() {
			return q
		}
	}
	return nil
}

// forWords is a request's project in words: ", for acme" (theirs: ",
// for its acme"), "" for none in particular.
func forWords(q *proto.ShareRequest) string {
	switch {
	case q.For == "":
		return ""
	case q.Out:
		return ", for " + oneLine(q.For)
	}
	return ", for its " + oneLine(q.For)
}

// noteWords is a request's note quoted, as data: ` · "the API docs"`.
func noteWords(q *proto.ShareRequest) string {
	if q.Note == "" {
		return ""
	}
	return ` · "` + oneLine(q.Note) + `"`
}

// shareLine is a share request's row w cells wide.
func (nv *networkView) shareLine(r netRow, w int) string {
	q := r.share
	tree := th().Faint.Render("├─")
	if r.last {
		tree = th().Faint.Render("└─")
	}
	name := netName(nv.list.Peers[r.peer])
	if r.kind == netAsked {
		right := th().Warn.Render("waiting on " + name + "'s user")
		if q.State == proto.ShareQueued {
			right = th().Faint.Render("not sent yet")
		}
		// The words give way before the request's id and project do.
		var left string
		for _, words := range []string{" asked to share a project", " asked to share", " asked"} {
			left = "     " + tree + words + th().Faint.Render(" ("+oneLine(q.ID)+forWords(q)+")")
			if ansi.StringWidth(left)+1+ansi.StringWidth(right) <= w {
				break
			}
		}
		return spread(left, right, w)
	}
	what := ""
	if q.For != "" {
		what = " (for its " + oneLine(q.For) + ")"
	}
	return fit("     "+tree+th().Warn.Render("⚑")+" asks you to share a project"+th().Faint.Render(what+noteWords(q)), w)
}

// shareDetail is the two lines under the tree about share request row r.
func (nv *networkView) shareDetail(r netRow, w int) [2]string {
	q := r.share
	name := netName(nv.list.Peers[r.peer])
	if r.kind == netAsked {
		return [2]string{joinFit([]string{"asked " + name + " to share a project (" + oneLine(q.ID) + forWords(q) + ")", "waiting on its user"}, w),
			th().Faint.Render(joinFit([]string{"its user picks which, if any", "one request per server at a time", "expires " + expires(q)}, w))}
	}
	parts := []string{name + " asks you to share a project (" + oneLine(q.ID) + ")" + forWords(q)}
	if q.Note != "" {
		parts = append(parts, `"`+oneLine(q.Note)+`"`)
	}
	return [2]string{joinFit(parts, w),
		th().Faint.Render(joinFit([]string{"A picks one of yours, shared with every paired server", "X declines"}, w))}
}

// expires is when share request q expires unanswered, as a date.
func expires(q *proto.ShareRequest) string {
	if q.Created.IsZero() {
		return "7 days after it was asked"
	}
	return q.Created.Add(link.RequestExpiry).Local().Format("2006-01-02 15:04")
}

// sharePanel is the details panel's lines w cells wide about share
// request row r.
func (nv *networkView) sharePanel(r netRow, w int) []panelLine {
	var out []panelLine
	add := func(lines ...string) {
		for _, l := range lines {
			out = append(out, panelLine{text: l})
		}
	}
	row := func(label, value string) {
		for i, l := range wrapLines(value, max(w-panelLabel, 8)) {
			if i > 0 {
				label = ""
			}
			add(fit(th().Faint.Render(label), panelLabel) + l)
		}
	}
	q := r.share
	name := netName(nv.list.Peers[r.peer])
	head, way := th().Faint.Render("ASKED TO SHARE"), "to"
	if r.kind == netShareReq {
		head, way = th().Warn.Render("⚑ NEEDS YOU"), "from"
	}
	add(head, th().Faint.Render(strings.Repeat("─", max(w, 1))))
	row("request", oneLine(q.ID))
	row(way, name+"'s user")
	what := th().Faint.Render("no project in particular")
	if q.For != "" {
		what = oneLine(q.For)
		if !q.Out {
			what = name + "'s " + what
		}
	}
	row("for", what)
	if q.Note != "" {
		row("note", `"`+oneLine(q.Note)+`"`)
	}
	if !q.Created.IsZero() {
		row("asked", q.Created.Local().Format("2006-01-02 15:04"))
		row("expires", expires(q))
	}
	add("")
	words := "Its user picks one of theirs to share with every server paired with it, or declines; nothing is shared without them. One request per server waits at a time."
	if r.kind == netShareReq {
		words = "A picks one of your active projects that isn't shared; the usual confirm shares it with every paired server, read-only, never its files. X declines: nothing is shared, and " + name + " hears of it."
	}
	add(faintLines(words, w)...)
	return out
}

// shareKeys are the action row's keys on share request row r.
func shareKeys(r netRow) string {
	if r.kind == netShareReq {
		return "A choose a project · X decline · ! next · esc close"
	}
	return "enter about it · tab details · ? keys · esc close"
}

// shareKey runs key s on share request row r; done is false for a key
// it leaves to the tree's.
func (nv *networkView) shareKey(m *dash, r netRow, s string) (tea.Cmd, bool) {
	q := r.share
	name := netName(nv.list.Peers[r.peer])
	switch {
	case s == "S":
		nv.askShare(m, nv.list.Peers[r.peer])
	case r.kind == netShareReq && s == "A":
		nv.chooseShare(m, name, *q)
	case r.kind == netShareReq && s == "X":
		id, src := q.ID, m.src
		m.confirmNo("Decline a share request", "Decline "+name+"'s request ("+oneLine(id)+")? Nothing is shared; "+name+" hears of it.", "the request still waits", func() tea.Cmd {
			return m.act(func() actionMsg {
				_, err := src.AnswerShare(id, false, "")
				return actionMsg{err: err, msg: "declined " + oneLine(id) + "; nothing was shared, " + name + " hears of it", then: nv.reload}
			})
		})
	case s == "enter":
		if r.kind == netShareReq {
			m.msg = oneLine(q.ID) + " from " + name + ": expires " + expires(q) + " · A chooses a project to share, X declines"
		} else {
			m.msg = oneLine(q.ID) + " waits on " + name + "'s user; it expires " + expires(q) + " · tm peer requests lists it"
		}
	default:
		return nil, false
	}
	return nil, true
}

// olderTM says whether paired server e's last hello shows a tm that
// takes messages but not share requests, one before link.ShareSince.
func olderTM(e PeerEntry) bool {
	return e.Link != nil && slices.Contains(e.Link.Caps, peer.CapMsg) && !slices.Contains(e.Link.Caps, peer.CapShare)
}

// askShare asks paired server e's user to share a project: refused at
// once while one waits or its tm takes none, else a menu of which of
// your projects it is for, then an optional note.
func (nv *networkView) askShare(m *dash, e PeerEntry) {
	name := netName(e)
	if q := asking(mirror(m, e)); q != nil {
		m.msg = "asked already (" + oneLine(q.ID) + "): it waits for " + name + "'s user; one share request waits per paired server"
		return
	}
	if olderTM(e) {
		m.msg = name + " runs a tm before " + link.ShareSince + ", which doesn't take share requests; ask its user yourself"
		return
	}
	note := func(forSlug string) tea.Cmd {
		what := "no project in particular"
		if forSlug != "" {
			what = forSlug
		}
		alias := e.Alias
		m.push(&inputView{title: "Ask " + name + " to share a project", placeholder: "what it's for (optional)", keep: true, max: link.ShareNoteMax,
			cancel: "nothing asked of " + name,
			label:  "For " + what + ". One line on what it's for, if you like; " + name + "'s user sees it as text from you, nothing more. enter asks; nothing is shared until that user shares one.",
			submit: func(s string) tea.Cmd {
				src := m.src
				return m.act(func() actionMsg {
					q, err := src.AskShare(alias, forSlug, strings.TrimSpace(s))
					msg := "asked " + name + " to share a project (" + oneLine(q.ID) + forWords(&q) + "); its user answers there"
					return actionMsg{err: err, msg: msg, then: func(m *dash) {
						nv.reload(m)
						nv.sel = "a:" + alias
					}}
				})
			}})
		return nil
	}
	var items []menuItem
	for _, slug := range shareable(m) {
		items = append(items, menuItem{label: "for " + slug, run: func(*dash) tea.Cmd { return note(slug) }})
	}
	items = append(items, menuItem{label: "for no project in particular", run: func(*dash) tea.Cmd { return note("") }})
	m.push(&menuView{title: "Ask " + name + " to share a project · which of yours is it for?", items: items, centred: true})
}

// chooseShare answers share request q from name: a menu of your active
// projects that aren't shared, then the usual share confirm, whose Y
// shares the one picked (peer.share-answer).
func (nv *networkView) chooseShare(m *dash, name string, q proto.ShareRequest) {
	var free []string
	for _, slug := range shareable(m) {
		if !shared(m, slug) {
			free = append(free, slug)
		}
	}
	if len(free) == 0 {
		m.msg = "every active project here is shared already; X declines the request"
		return
	}
	id := oneLine(q.ID)
	var items []menuItem
	for _, slug := range free {
		items = append(items, menuItem{label: slug, run: func(m *dash) tea.Cmd {
			ask := fmt.Sprintf("Share %s with every paired server? %s asked for a project (%s%s). They see its state read-only (what waits for you, the inbox, threads, tasks on deck), never its files.", slug, name, id, forWords(&q))
			m.confirmNo("Share "+slug, ask, "not shared; the request still waits", func() tea.Cmd {
				src := m.src
				return m.act(func() actionMsg {
					_, err := src.AnswerShare(q.ID, true, slug)
					return actionMsg{err: err, msg: slug + " is shared with every paired server, for " + name + "'s request " + id, then: func(m *dash) {
						nv.reload(m)
						nv.sel = "p:" + slug
					}}
				})
			})
			return nil
		}})
	}
	m.push(&menuView{title: "Share a project with " + name + "? · pick one of yours", items: items, centred: true})
}

// shareKeyOf is a share request row's key: "a:<alias>" yours, which
// waits one at a time, "s:<alias>:<id>" theirs.
func shareKeyOf(alias string, r netRow) string {
	if r.kind == netAsked {
		return "a:" + alias
	}
	return "s:" + alias + ":" + r.share.ID
}
