package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A project's membership of a paired server's program, on the child's
// side (the programs design §9.1): the Messages tab's rows of the
// program's invite (accept or decline it) and of the membership (leave
// it), and the project settings' Program section, where the program
// keys change, each with its own confirm. All of it is there only
// while programs are on: the source shows no invite and no membership
// while off.

// inviteRows are the Messages tab's rows of project invites invs, the
// project's side: one that waits (a need), a membership, and one that
// ended.
func inviteRows(invs []link.Invite) []msgNeed {
	var out []msgNeed
	for i := range invs {
		r := &invs[i]
		if r.Out || !(r.Pending() || r.State == link.ReqAccepted || r.State == link.InviteLeft) {
			continue
		}
		out = append(out, msgNeed{inv: r})
	}
	return out
}

// pendingInvites counts the rows of needs that wait for the user.
func pendingInvites(needs []msgNeed) int {
	n := 0
	for _, x := range needs {
		if x.inv != nil && x.inv.Pending() {
			n++
		}
	}
	return n
}

// programRowText is row n's words and keys.
func programRowText(peers []proto.PeerMirror, n msgNeed) (mark, words, keys string) {
	r := n.inv
	prog := programOfKey(peers, r.Peer)
	switch {
	case r.Pending():
		return "⚑ ", "invites it into program " + prog + " (" + oneLine(r.ID) + ")", " · A accept · X decline"
	case r.State == link.ReqAccepted:
		w := "member of program " + prog
		if len(r.Granted) > 0 {
			w += ": " + strings.Join(r.Granted, ", ")
		}
		return "  ", w, " · L leave"
	}
	return "  ", "left program " + prog + strings.TrimPrefix(r.Words(), "left"), ""
}

// programOfKey is "acme on studio" for the paired program key
// "studio/acme".
func programOfKey(peers []proto.PeerMirror, key string) string {
	alias, slug, ok := config.Remote(key)
	if !ok {
		return key
	}
	return slug + " on " + peerShownKey(peers, alias)
}

// programRowKey runs key s on program row n of project slug's Messages
// tab.
func (m *dash) programRowKey(slug string, n msgNeed, s string) tea.Cmd {
	r := n.inv
	switch {
	case r.Pending() && s == "A":
		m.push(newInviteView(m, slug, *r))
	case r.Pending() && s == "X":
		id, from := r.ID, programOfKey(m.data.Peers, r.Peer)
		m.confirm("Decline a program invite", "Decline "+from+"'s invite of "+slug+"? Nothing here changes; its server hears of it.",
			func() tea.Cmd { return m.programAct(slug, "decline", id, link.AnswerOpts{}) })
	case r.State == link.ReqAccepted && s == "L":
		return m.leaveProgram(slug, r.Peer)
	case s == "enter" || s == "space":
		_, words, keys := programRowText(m.data.Peers, n)
		m.msg = words + keys
	}
	return nil
}

// leaveProgram asks, then takes project slug out of the paired program
// key: its program keys go, and the program's server hears of it.
func (m *dash) leaveProgram(slug, key string) tea.Cmd {
	from := programOfKey(m.data.Peers, key)
	m.confirm("Leave a program", "Take "+slug+" out of program "+from+"? Its director no longer files tasks, messages or starts anything here, and "+slug+"'s program settings go; its server hears of it.",
		func() tea.Cmd { return m.programAct(slug, "leave", key, link.AnswerOpts{}) })
	return nil
}

// programAct runs a program act of project slug in the background, then
// reads its messages again.
func (m *dash) programAct(slug, verb, arg string, o link.AnswerOpts) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		out, err := src.ProgramAct(slug, verb, arg, o)
		if err != nil {
			return actionMsg{err: settingsErr(err), then: func(m *dash) { m.reloadMessages() }}
		}
		return actionMsg{msg: out, then: func(m *dash) { m.reloadMessages() }}
	})
}

// inviteView is the dialog of accepting a paired program's invite: what
// its director may do here, and the switches that make its rolled-up
// needs work. Share and needs view come ticked, answer doesn't (decision
// 14); starting threads is off, and ticking it asks first. enter accepts,
// esc leaves the invite waiting.
type inviteView struct {
	slug string
	inv  link.Invite
	o    link.AnswerOpts
	// shared: the project is shared with paired servers already.
	shared bool
	sel    int
}

// Rows of the dialog.
const (
	invShare = iota
	invNeedsView
	invNeedsAnswer
	invTasks
	invStatus
	invStarts
	invRows
)

func newInviteView(m *dash, slug string, r link.Invite) *inviteView {
	v := &inviteView{slug: slug, inv: r, o: link.AnswerOpts{Tasks: config.ProgramTasksFile, Share: true, NeedsView: true,
		Status: hasFeature(r.Features, proto.ProgramFeatureStatus)}}
	if p := m.projectData(slug); p != nil && p.Safety != nil && p.Safety.Share {
		v.shared = true
	}
	return v
}

func hasFeature(fs []string, f string) bool {
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

// usable says whether row i applies: status and starts only when the
// invite asks for them, share only while the project isn't shared.
func (v *inviteView) usable(i int) bool {
	switch i {
	case invShare:
		return !v.shared
	case invStatus:
		return hasFeature(v.inv.Features, proto.ProgramFeatureStatus)
	case invStarts:
		return hasFeature(v.inv.Features, proto.ProgramFeatureStarts)
	}
	return true
}

func (v *inviteView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
		m.msg = "the invite still waits"
	case "up", "down":
		d := map[string]int{"up": -1, "down": 1}[s]
		for j := v.sel + d; j >= 0 && j < invRows; j += d {
			if v.usable(j) {
				v.sel = j
				break
			}
		}
	case "space", " ", "left", "right":
		v.toggle(m)
	case "enter":
		o, slug, id := v.o, v.slug, v.inv.ID
		from := programOfKey(m.data.Peers, v.inv.Peer)
		m.pop()
		m.confirm("Join a program", "Put "+slug+" in program "+from+"? "+acceptWords(o, v.shared, peerShownKey(m.data.Peers, aliasOf(v.inv.Peer)))+" Its server hears of it.",
			func() tea.Cmd { return m.programAct(slug, "accept", id, o) })
	}
	return nil
}

func aliasOf(key string) string {
	alias, _, _ := config.Remote(key)
	return alias
}

// toggle changes the selected row; starting threads on asks first.
func (v *inviteView) toggle(m *dash) {
	switch v.sel {
	case invShare:
		v.o.Share = !v.o.Share
	case invNeedsView:
		v.o.NeedsView = !v.o.NeedsView
		if !v.o.NeedsView {
			v.o.NeedsAnswer = false
		}
	case invNeedsAnswer:
		v.o.NeedsAnswer = !v.o.NeedsAnswer
		if v.o.NeedsAnswer {
			v.o.NeedsView = true
		}
	case invTasks:
		v.o.Tasks = map[string]string{config.ProgramTasksFile: config.ProgramTasksPropose, config.ProgramTasksPropose: config.ProgramTasksFile}[v.o.Tasks]
	case invStatus:
		v.o.Status = !v.o.Status
	case invStarts:
		if v.o.Starts {
			v.o.Starts = false
			return
		}
		m.confirm("Let the director start threads", startsQuestion(v.slug, programOfKey(m.data.Peers, v.inv.Peer)), func() tea.Cmd {
			v.o.Starts = true
			return nil
		})
	}
}

// startsQuestion asks before a paired program's director may start
// threads in project slug.
func startsQuestion(slug, from string) string {
	return "Let program " + from + "'s director start threads in " + slug + "? Each start still needs that program to delegate, and " + slug + "'s own caps, pause and start rules hold; the threads are " + slug + "'s like any other."
}

// acceptWords says what accepting with o does.
func acceptWords(o link.AnswerOpts, shared bool, server string) string {
	var ws []string
	if o.Tasks == config.ProgramTasksPropose {
		ws = append(ws, "Its director's tasks arrive as items your coordinator proposes to you.")
	} else {
		ws = append(ws, "Its director's tasks land on the board.")
	}
	if o.Starts {
		ws = append(ws, "It may start threads here.")
	}
	if o.Status {
		ws = append(ws, "The status of the tasks it filed or watches goes back to it.")
	}
	if o.Share && !shared {
		ws = append(ws, "The project is shared with paired servers.")
	}
	switch {
	case o.NeedsAnswer:
		ws = append(ws, server+" sees its needs in full and may answer them.")
	case o.NeedsView:
		ws = append(ws, server+" sees its needs in full.")
	}
	return strings.Join(ws, " ")
}

func (v *inviteView) lines(m *dash, w int) []string {
	server := peerShownKey(m.data.Peers, aliasOf(v.inv.Peer))
	box := func(on bool) string {
		if on {
			return "[x] "
		}
		return "[ ] "
	}
	rows := []string{
		invShare:       box(v.o.Share || v.shared) + "Share " + v.slug + " with paired servers",
		invNeedsView:   box(v.o.NeedsView) + server + " sees its needs (view)",
		invNeedsAnswer: box(v.o.NeedsAnswer) + server + " may answer its needs (answer)",
		invTasks:       "    Director's tasks: " + map[string]string{config.ProgramTasksFile: "file them on the board", config.ProgramTasksPropose: "your coordinator proposes them"}[v.o.Tasks],
		invStatus:      box(v.o.Status) + "Send the status of its tasks back",
		invStarts:      box(v.o.Starts) + "Its director may start threads here",
	}
	if v.shared {
		rows[invShare] = "[x] Share " + v.slug + " with paired servers (it is already)"
	}
	out := wrapLines(programOfKey(m.data.Peers, v.inv.Peer)+" invites "+v.slug+" into its program, asking for "+strings.Join(v.inv.Features, ", ")+".", w)
	out = append(out, "")
	for i, l := range rows {
		switch {
		case i == v.sel:
			out = append(out, th().Sel.Render(fit(l, w)))
		case !v.usable(i):
			out = append(out, th().Faint.Render(fit(l, w)))
		default:
			out = append(out, fit(l, w))
		}
	}
	return out
}

func (v *inviteView) render(m *dash) string {
	w := m.inner(viewWidth)
	return m.popup(box{title: "Accept " + oneLine(v.inv.ID), body: v.lines(m, w), sel: -1, keys: "space change · enter accept · ↑ ↓ move · esc later"})
}

// programLinkRows are project slug's Program section in its settings:
// the paired program it is in, and the keys its director goes by, each
// changed with its own confirm. None unless it is in one, and programs
// are on.
func programLinkRows(m *dash, slug string) []setting {
	l, in := m.data.ProgramLinks[slug]
	if !in {
		return nil
	}
	from := programOfKey(m.data.Peers, l.Program)
	get := func(m *dash) config.ProgramLink { return m.data.ProgramLinks[slug] }
	set := func(key string, value any, words string) tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			if err := src.SetProgramLinkKey(slug, key, value); err != nil {
				return actionMsg{err: settingsErr(err)}
			}
			return actionMsg{msg: words}
		})
	}
	return []setting{
		{section: "Program", label: "Program", value: func(m *dash) string { return from },
			help:   "The paired server's program this project is in, since you accepted its invite. enter takes it out: the director no longer files tasks, messages or starts anything here, and its server hears of it.",
			change: func(m *dash) tea.Cmd { return m.leaveProgram(slug, l.Program) }},
		{label: "Director's tasks", value: func(m *dash) string {
			if get(m).Tasks == config.ProgramTasksPropose {
				return "proposed"
			}
			return "filed"
		}, help: "filed: the director's tasks land on the board. proposed: they arrive as items your coordinator proposes to you.",
			change: func(m *dash) tea.Cmd {
				next, words := config.ProgramTasksPropose, "the director's tasks arrive as items the coordinator proposes"
				if get(m).Tasks == config.ProgramTasksPropose {
					next, words = config.ProgramTasksFile, "the director's tasks land on the board"
				}
				m.confirm("Director's tasks", "From now on "+words+" in "+slug+"?", func() tea.Cmd { return set("program_tasks", next, words) })
				return nil
			}},
		{label: "Director starts threads", value: func(m *dash) string { return onOff(get(m).Starts) },
			help: "The director may start threads here; each start still needs its program to delegate, and this project's caps, pause and start rules.",
			change: func(m *dash) tea.Cmd {
				if get(m).Starts {
					m.confirm("Director starts threads", "Stop "+from+"'s director starting threads in "+slug+"?", func() tea.Cmd {
						return set("program_starts", false, "the director no longer starts threads in "+slug)
					})
					return nil
				}
				m.confirm("Let the director start threads", startsQuestion(slug, from), func() tea.Cmd {
					return set("program_starts", true, "the director may start threads in "+slug)
				})
				return nil
			}},
		{label: "Status to the program", value: func(m *dash) string { return onOff(get(m).Status) },
			help: "The status of the tasks the director filed or watches goes back to its program.",
			change: func(m *dash) tea.Cmd {
				on := !get(m).Status
				q := fmt.Sprintf("Send the status of %s's program tasks back to %s?", slug, from)
				if !on {
					q = fmt.Sprintf("Stop sending the status of %s's program tasks to %s?", slug, from)
				}
				m.confirm("Status to the program", q, func() tea.Cmd {
					return set("program_status", on, "status to "+from+" "+onOff(on))
				})
				return nil
			}},
	}
}
