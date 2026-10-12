package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// The action binding (mods design §11, docs/SPEC.md §5.3): a mod's row
// or notification button names a core action and a target; the human's
// key or click runs it here, never the mod. The press counts only once
// the row's bindings had been still for 300 ms (widget.View); then the
// server checks the binding is in the mod's reach (its projects), the
// console loads the live board, re-checks the action's precondition
// with modaction.Why (the check the server's $.actions.available runs),
// asks with the core's own confirmation, which names the target from
// live state, and runs the same code path as the core's own key, as the
// human. The mod hears of it as action.done.

// binding is a pressed action a mod bound: the mod's id, the action and
// its target as the mod published them, and the press token the
// server's check gave, which the action.done for this press presents.
type binding struct {
	mod    string
	action widget.Action
	token  string
}

func (b *binding) target() modaction.Target { return widget.ActionTarget(b.action) }

// bindingMsg is the live state a press is checked against, loaded off
// the update loop.
type bindingMsg struct {
	b     binding
	board *tasks.Board
	err   error
	// refused: the server refused the binding as outside its mod's
	// reach; the mod is told nothing.
	refused bool
	// explain: a core key the row doesn't bind was pressed; the
	// console only says why the action isn't available (whyNot).
	explain bool
}

// pressBinding handles ev, an EventAction from mod's view (or a
// notification button made into one): nil for any other event.
func (m *dash) pressBinding(mod string, ev *widget.Event) tea.Cmd {
	if ev == nil || ev.Kind != widget.EventAction || ev.Action == nil {
		return nil
	}
	if !ev.Stable {
		m.msg = "the row just changed: look again, then press again"
		return nil
	}
	b := binding{mod: mod, action: *ev.Action}
	t, src := b.target(), m.src
	return func() tea.Msg {
		msg := bindingMsg{b: b}
		tok, err := src.ModActionCheck(b.mod, b.action.ID, b.action.Target)
		if err != nil {
			msg.err, msg.refused = err, true
			return msg
		}
		msg.b.token = tok
		if t.Project != "" && t.Task != "" {
			msg.board, msg.err = src.Board(t.Project)
		}
		return msg
	}
}

// whyNot says why action a, a core key the row under a mod view's
// cursor doesn't bind (widget.View.Unbound), isn't available on the
// row's task, as the core's own Tasks tab did: from the live board, the
// same check a press runs. It never runs the action, nor tells the mod.
func (m *dash) whyNot(a widget.Action) tea.Cmd {
	b := binding{action: a}
	t, src := b.target(), m.src
	return func() tea.Msg {
		msg := bindingMsg{b: b, explain: true}
		msg.board, msg.err = src.Board(t.Project)
		return msg
	}
}

// runBinding re-checks a pressed binding against live state and runs
// it as the core's own key does.
func (m *dash) runBinding(msg bindingMsg) tea.Cmd {
	b := msg.b
	t := b.target()
	if msg.explain {
		why := ""
		if msg.err == nil {
			why = modaction.Why(b.action.ID, dashActions{m: m, board: msg.board}, t)
		}
		if a, _ := modaction.Lookup(b.action.ID); why == "" {
			why = t.Task + " has no " + a.Label + " here"
		}
		m.msg = "not available: " + why
		return nil
	}
	if _, ok := modaction.Lookup(b.action.ID); !ok {
		// A view's tree can't carry one (widget.Check); nothing to tell.
		m.msg = "not available: " + b.action.ID + " is not an action a mod may bind"
		return nil
	}
	if msg.err != nil {
		m.msg = "not available: " + msg.err.Error()
		if msg.refused {
			return nil
		}
		return m.bindingDone(&b, false)
	}
	st := dashActions{m: m, board: msg.board}
	if why := modaction.Why(b.action.ID, st, t); why != "" {
		m.msg = "not available: " + why
		return m.bindingDone(&b, false)
	}
	task := st.Task(t.Project, t.Task)
	switch b.action.ID {
	case "task.show":
		// From the project's own popup (its Tasks tab mod's row), the
		// task opens over it, as enter on the core Tasks tab does.
		if pv, ok := m.top().(*projectView); ok && pv.slug == t.Project {
			return tea.Batch(m.openTask(pv, task), m.bindingDone(&b, true))
		}
		return tea.Batch(m.showTask(t.Project, task.ID), m.bindingDone(&b, true))
	case "task.delegate":
		return m.delegate(t.Project, task, &b)
	case "task.accept":
		return m.accept(t.Project, task, &b)
	case "task.send-back":
		return m.sendBack(t.Project, task, &b)
	case "task.new":
		return tea.Batch(m.newTask(t.Project), m.bindingDone(&b, true))
	case "task.coordinator":
		return tea.Batch(m.openProject(t.Project), m.bindingDone(&b, true))
	case "project.activate":
		p := m.projectData(t.Project)
		if p == nil || p.Safety == nil {
			m.msg = "not available: " + t.Project + "'s settings can't be read"
			return m.bindingDone(&b, false)
		}
		return m.toggleActiveVia(t.Project, p.inactive(), &b)
	case "session.attach":
		s, _ := m.session(t.Session)
		return m.actVia(&b, func() (actionMsg, bool) {
			return actionMsg{attach: s.ID, current: s.Project}, true
		})
	case "session.clear":
		s, _ := m.session(t.Session)
		return m.clearSession(s, &b)
	case "question.answer":
		s, _ := m.session(t.Session)
		return m.answerQuestion(s, t.Question, t.Option, &b)
	}
	m.msg = "not available: " + b.action.ID
	return m.bindingDone(&b, false)
}

// actVia is act for binding b (nil for the core's own key): once fn has
// run, the mod hears whether it did (fn's ok) as action.done.
func (m *dash) actVia(b *binding, fn func() (actionMsg, bool)) tea.Cmd {
	src := m.src
	return m.act(func() actionMsg {
		msg, ok := fn()
		if b != nil {
			_ = src.ModActionDone(b.mod, b.action.ID, b.action.Target, ok, b.token)
		}
		return msg
	})
}

// bindingDone tells mod b.mod its bound action ran (ok) or was refused.
func (m *dash) bindingDone(b *binding, ok bool) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		_ = src.ModActionDone(b.mod, b.action.ID, b.action.Target, ok, b.token)
		return nil
	}
}

// dashActions is the console's live state for a precondition: the board
// just loaded, the inbox items and sessions of the last load.
type dashActions struct {
	m     *dash
	board *tasks.Board
}

func (d dashActions) Project(slug string) bool { return d.m.projectData(slug) != nil }

func (d dashActions) Task(slug, ref string) *tasks.Task {
	if d.board == nil {
		return nil
	}
	for _, t := range d.board.Tasks {
		if t.Ref() == ref {
			return t
		}
	}
	return nil
}

func (d dashActions) Asked(slug, ref string) string {
	if p := d.m.projectData(slug); p != nil {
		return project.TaskAsked(p.Items, ref)
	}
	return ""
}

func (d dashActions) Session(id string) bool {
	_, ok := d.m.session(id)
	return ok
}

func (d dashActions) Menu(id string) []modaction.MenuQuestion {
	s, ok := d.m.session(id)
	if !ok || s.Question == nil {
		return nil
	}
	out := make([]modaction.MenuQuestion, len(s.Question.Questions))
	for i, it := range s.Question.Questions {
		out[i] = modaction.MenuQuestion{Question: it.Question, Multi: it.MultiSelect, Answered: it.Answered}
		for _, o := range it.Options {
			out[i].Options = append(out[i].Options, o.Label)
		}
	}
	return out
}

// sessionWho names session s in a confirmation: "demo t-0003", "demo
// coordinator", or its id outside the projects.
func sessionWho(s proto.SessionInfo) string {
	switch {
	case s.Project == "":
		return s.ID
	case s.Thread != "":
		return s.Project + " " + s.Thread
	}
	return s.Project + " coordinator"
}

// clearSession asks, then clears session s's conversation (a mod's
// session.clear binding, b): tm pastes the agent's clear prompt once it
// is idle. The project's files hold its state.
func (m *dash) clearSession(s proto.SessionInfo, b *binding) tea.Cmd {
	who := sessionWho(s)
	question := "Clear " + who + "'s conversation? Its agent starts afresh once idle; the project's files keep its task and state."
	m.confirmNo("Clear "+who, question, who+" not cleared", func() tea.Cmd {
		src, id := m.src, s.ID
		return m.actVia(b, func() (actionMsg, bool) {
			if err := src.ClearSession(id); err != nil {
				return actionMsg{err: err}, false
			}
			return actionMsg{msg: "clearing " + who + "'s conversation once it is idle"}, true
		})
	})
	return nil
}

// answerQuestion asks, then answers question q of the menu open on
// session s with option (a mod's question.answer binding, b), in the
// words the live menu has; the server refuses it if the menu changed
// meanwhile.
func (m *dash) answerQuestion(s proto.SessionInfo, q, option string, b *binding) tea.Cmd {
	i, why := modaction.Answer(dashActions{m: m}.Menu(s.ID), q, option)
	if why != "" {
		m.msg = "not available: " + why
		return m.bindingDone(b, false)
	}
	who := sessionWho(s)
	question := fmt.Sprintf("Answer %s's question %q with %q? It goes to the agent as your answer.", who, oneLine(q), oneLine(option))
	m.confirmNo("Answer "+who, question, who+"'s question not answered", func() tea.Cmd {
		src, id := m.src, s.ID
		return m.actVia(b, func() (actionMsg, bool) {
			if err := src.AnswerQuestion(id, i, q, option); err != nil {
				return actionMsg{err: err}, false
			}
			return actionMsg{msg: "answered " + who + ": " + oneLine(option)}, true
		})
	})
	return nil
}

// BindKeys are the keys this console's keymap gives bindable action id,
// for a mod's view (widget.View.SetBindKeys).
func BindKeys(id string) []string {
	for _, a := range KeyActions() {
		if a.ID == id {
			return a.Keys
		}
	}
	return nil
}

// boundHints is the hint line for the actions the row under v's cursor
// binds, in the core's words ("A accept · enter show"), which a mod
// can't choose.
func boundHints(v *widget.View) string {
	var out []string
	for _, b := range v.Bound() {
		if a, ok := modaction.Lookup(b.Action.ID); ok {
			out = append(out, fmt.Sprintf("%s %s", keyShown(b.Key), a.Label))
		}
	}
	return strings.Join(slices.Compact(out), " · ")
}
