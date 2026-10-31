package tui

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Acting on a paired server's need (docs/SPEC.md §16.16): while that
// server's needs switch for this one is answer, enter on its need in the
// all-projects NEEDS YOU opens this dialog: the question's options, the
// dialog's options that allow once, accept or send back a task in
// review, or have its coordinator open its questions. A pick asks first,
// then goes to that server as this server's user (peer.need-act), with
// the words shown as the guard: it refuses when the need changed, and
// always an escape's.

// peerNeedRef is what a row knows of a paired server's need to act on
// it.
type peerNeedRef struct {
	alias, server, project string
	need                   proto.PeerNeed
	// answer: that server's needs switch for this one is answer.
	answer bool
}

// peerNeedChoice is one pick in the dialog: its words and the act. A
// multi-select's options tick (box: the option's index, from 1) and its
// submit sends the ones ticked.
type peerNeedChoice struct {
	label  string
	act    proto.PeerAct
	box    int
	submit bool
}

// peerNeedView is the dialog for one paired server's need.
type peerNeedView struct {
	ref     peerNeedRef
	text    []string
	choices []peerNeedChoice
	sel     int
	ticked  map[int]bool // a multi-select's options ticked, by box
}

// peerNeedChoices are what the dialog offers on need n of project slug:
// the first open question's options, a dialog's options that allow once
// (another is that server's user's to pick, in the pane), accept and
// send back, or opening the questions; and the words that say what it
// asks.
func peerNeedChoices(slug string, n proto.PeerNeed) (text []string, out []peerNeedChoice) {
	base := proto.PeerAct{Project: slug, Need: n.ID}
	for _, a := range proto.ActsOf(n) {
		act := base
		act.Act = a
		switch a {
		case proto.ActAnswer:
			for i, it := range n.Question.Questions {
				if it.Answered {
					continue
				}
				text = append(text, it.Question)
				if it.MultiSelect {
					act.Index, act.Question = i, it.Question
					for j, o := range it.Options {
						out = append(out, peerNeedChoice{label: oneLine(o.Label), act: act, box: j + 1})
					}
					out = append(out, peerNeedChoice{label: "submit the options ticked", act: act, submit: true})
					break
				}
				for _, o := range it.Options {
					act.Index, act.Question, act.Answer = i, it.Question, o.Label
					out = append(out, peerNeedChoice{label: oneLine(o.Label), act: act})
				}
				break
			}
		case proto.ActApprove:
			text = append(text, n.Approval.Question)
			for i, o := range n.Approval.Options {
				if i < len(n.Approval.Scopes) && n.Approval.Scopes[i] != "once" {
					continue
				}
				act.Choice, act.Option = i+1, o
				out = append(out, peerNeedChoice{label: oneLine(o), act: act})
			}
		case proto.ActAccept:
			act.Status = n.Status
			text = append(text, joinSp(n.Task, n.Title, "is in review"))
			out = append(out, peerNeedChoice{label: "accept: the coordinator marks it done", act: act})
		case proto.ActSendBack:
			act.Status = n.Status
			out = append(out, peerNeedChoice{label: "send back, with a note", act: act})
		case proto.ActQuestions:
			if q := n.Question; q != nil && len(q.Questions) > 0 {
				text = append(text, q.Questions[0].Question)
			}
			out = append(out, peerNeedChoice{label: "open the coordinator's questions in its pane", act: act})
		}
	}
	return text, out
}

// openPeerNeed opens the dialog for the need on row r, or says why it
// can't be answered from here.
func (m *dash) openPeerNeed(ref peerNeedRef) tea.Cmd {
	text, choices := peerNeedChoices(ref.project, ref.need)
	switch {
	case ref.need.Escape:
		m.msg = "that asks to run a command outside the sandbox: only " + ref.server + "'s user answers it, there"
		return nil
	case len(choices) == 0:
		return m.showProject(peerFold(ref.alias, ref.project))
	case !ref.answer:
		m.msg = ref.server + " shows you its needs but takes no answers: on " + ref.server + ", tm peer needs <this server> answer"
		return m.showProject(peerFold(ref.alias, ref.project))
	}
	m.push(&peerNeedView{ref: ref, text: text, choices: choices})
	return nil
}

func (v *peerNeedView) key(m *dash, k tea.KeyPressMsg) tea.Cmd {
	switch s := k.String(); s {
	case "esc", "q":
		m.pop()
	case "up", "down":
		v.sel = moveSel(v.sel, scrollKeys[s], len(v.choices))
	case "enter":
		return v.pick(m, v.sel)
	case "space":
		v.tick(v.sel)
	default:
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(v.choices) {
			return v.pick(m, n-1)
		}
	}
	return nil
}

// tick toggles choice i's box, when it is a multi-select's option.
func (v *peerNeedView) tick(i int) bool {
	if i < 0 || i >= len(v.choices) || v.choices[i].box == 0 {
		return false
	}
	if v.ticked == nil {
		v.ticked = map[int]bool{}
	}
	b := v.choices[i].box
	v.ticked[b] = !v.ticked[b]
	return true
}

// tickedAct is the multi-select's act with the options ticked, in the
// menu's order, and their labels as its guard; false with none ticked.
func (v *peerNeedView) tickedAct(act proto.PeerAct) (proto.PeerAct, string, bool) {
	var labels []string
	act.Picks = nil
	for _, c := range v.choices {
		if c.box > 0 && v.ticked[c.box] {
			act.Picks = append(act.Picks, c.box-1)
			labels = append(labels, c.label)
		}
	}
	act.Answer = strings.Join(labels, ", ")
	return act, act.Answer, len(labels) > 0
}

// pick asks first, then sends choice i (a note first for send back).
func (v *peerNeedView) pick(m *dash, i int) tea.Cmd {
	if v.tick(i) {
		return nil
	}
	c, ref := v.choices[i], v.ref
	if c.submit {
		act, label, ok := v.tickedAct(c.act)
		if !ok {
			m.msg = "tick one option or more first: 1-9 or space"
			return nil
		}
		c.act, c.label = act, label
	}
	m.pop()
	who := ref.server + " › " + ref.project
	if c.act.Act == proto.ActSendBack {
		task := ref.need.Task
		m.promptNo("Send "+task+" back on "+ref.server, "What should change? "+ref.server+"'s coordinator passes your note on.", "",
			task+" not sent back", project.MaxSendBackNote, func(note string) tea.Cmd {
				c.act.Note = note
				return m.sendPeerAct(ref, c.act, "sent "+task+" back on "+ref.server)
			})
		return nil
	}
	question := fmt.Sprintf("On %s: %s? It goes to %s as your answer.", who, c.label, ref.server)
	m.confirmNo("Answer on "+ref.server, question, "not answered", func() tea.Cmd {
		return m.sendPeerAct(ref, c.act, "answered on "+ref.server+": "+c.label)
	})
	return nil
}

// sendPeerAct sends act on ref's need; the footer says what came of it.
func (m *dash) sendPeerAct(ref peerNeedRef, act proto.PeerAct, done string) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		res, err := src.ActOnNeed(proto.PeerNeedActParams{Peer: ref.alias, PeerAct: act})
		if err != nil {
			return actionMsg{err: err}
		}
		switch {
		case res.Act == proto.ActAccept && !res.Asked:
			done = ref.need.Task + " is already waiting on " + ref.server + "'s coordinator"
		case res.Act == proto.ActQuestions:
			done = fmt.Sprintf("asked %s's coordinator to open its %d question(s)", ref.server, res.Open)
		}
		return actionMsg{msg: done}
	})
}

func (v *peerNeedView) render(m *dash) string {
	w := m.inner(dialogWidth)
	var body []string
	for _, t := range v.text {
		body = append(body, wrapLines(t, w)...)
	}
	if len(body) > 0 {
		body = append(body, "")
	}
	head := len(body)
	hits := make([]int, head)
	for i := range hits {
		hits[i] = noHit
	}
	multi := false
	for i, c := range v.choices {
		switch {
		case c.box > 0:
			box := "[ ]"
			if v.ticked[c.box] {
				box = "[x]"
			}
			body = append(body, fmt.Sprintf("  %d  %s %s", i+1, box, c.label))
			multi = true
		case c.submit:
			body = append(body, "     "+c.label)
		default:
			body = append(body, fmt.Sprintf("  %d  %s", i+1, c.label))
		}
		hits = append(hits, i)
	}
	body = append(body, "", "On "+v.ref.server+": it checks your answer again, and refuses it if this changed meanwhile.")
	hits = append(hits, noHit, noHit)
	keys := "1-9 or enter pick · esc cancel"
	if multi {
		keys = "1-9 or space tick · enter on submit sends · esc cancel"
	}
	title := cmp.Or(v.ref.need.Task, v.ref.need.Thread, "coordinator")
	return m.popup(box{title: v.ref.server + " › " + v.ref.project + ": " + title, body: body, sel: head + v.sel, hits: hits,
		keys: keys, dialog: true})
}
