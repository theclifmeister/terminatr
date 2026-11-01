package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/tasks"
)

// pickPriority asks for slug's task t's priority (P on a task): must,
// should, could or none, the one it has marked; the pick is the
// human's, as tm task edit --priority sets it. b is the mod's binding
// that pressed it, nil for the core's own key.
func (m *dash) pickPriority(slug string, t *tasks.Task, b *binding) tea.Cmd {
	ref := t.Ref()
	var items []menuItem
	for _, p := range append(append([]tasks.Priority{}, tasks.Priorities...), tasks.NoPriority) {
		label := p.String()
		if p == t.Priority {
			label += " ✓"
		}
		items = append(items, menuItem{label: label, run: func(m *dash) tea.Cmd { return m.setPriority(slug, ref, t.ID, p, b) }})
	}
	m.push(&menuView{title: "Priority · " + ref + " " + oneLine(t.Title), items: items, centred: true})
	return nil
}

// setPriority sets it, saying what it did.
func (m *dash) setPriority(slug, ref string, id int, p tasks.Priority, b *binding) tea.Cmd {
	src := m.src
	run := func() (actionMsg, bool) {
		changed, err := src.SetPriority(slug, id, p)
		switch {
		case err != nil:
			return actionMsg{err: err}, false
		case !changed:
			return actionMsg{msg: ref + " is " + p.String() + " already"}, true
		}
		return actionMsg{msg: ref + " is " + p.String() + " now"}, true
	}
	return m.actVia(b, run)
}
