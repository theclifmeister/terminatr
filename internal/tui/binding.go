package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// The action binding (mods design §11, docs/SPEC.md §5.3): a mod's row
// or notification button names a core action and a target; the human's
// key or click runs it here, never the mod. The press counts only once
// the row's bindings had been still for 300 ms (widget.View); then the
// console loads the live board, re-checks the action's precondition
// with modaction.Why (the check the server's $.actions.available runs),
// asks with the core's own confirmation, which names the target from
// live state, and runs the same code path as the core's own key, as the
// human. The mod hears of it as action.done.

// binding is a pressed action a mod bound: the mod's id, the action and
// its target as the mod published them.
type binding struct {
	mod    string
	action widget.Action
}

func (b *binding) target() modaction.Target { return widget.ActionTarget(b.action) }

// bindingMsg is the live state a press is checked against, loaded off
// the update loop.
type bindingMsg struct {
	b     binding
	board *tasks.Board
	err   error
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
		if t.Project != "" && t.Task != "" {
			msg.board, msg.err = src.Board(t.Project)
		}
		return msg
	}
}

// runBinding re-checks a pressed binding against live state and runs
// it as the core's own key does.
func (m *dash) runBinding(msg bindingMsg) tea.Cmd {
	b := msg.b
	t := b.target()
	if _, ok := modaction.Lookup(b.action.ID); !ok {
		// A view's tree can't carry one (widget.Check); nothing to tell.
		m.msg = "not available: " + b.action.ID + " is not an action a mod may bind"
		return nil
	}
	if msg.err != nil {
		m.msg = "not available: " + msg.err.Error()
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
		return tea.Batch(m.showTask(t.Project, task.ID), m.bindingDone(&b, true))
	case "task.delegate":
		return m.delegate(t.Project, task, &b)
	case "task.accept":
		return m.accept(t.Project, task, &b)
	case "task.send-back":
		return m.sendBack(t.Project, task, &b)
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
			_ = src.ModActionDone(b.mod, b.action.ID, b.action.Target, ok)
		}
		return msg
	})
}

// bindingDone tells mod b.mod its bound action ran (ok) or was refused.
func (m *dash) bindingDone(b *binding, ok bool) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		_ = src.ModActionDone(b.mod, b.action.ID, b.action.Target, ok)
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
