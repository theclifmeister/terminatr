package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/mod"
)

// The new-task prompt (+ in the project popup's Tasks tab): the task
// templates of the mods that run for the project first, when there are
// any (mods design §2 tier 3, the bundled default-templates' bug, spike
// and chore among them), then the title. The task is the human's, added
// as tm task add [--template] adds it; the coordinator sees it on its
// board.

// newTask asks for a new task in project slug: its template, then its
// title.
func (m *dash) newTask(slug string) tea.Cmd {
	ts := m.src.Templates(slug)
	if len(ts) == 0 {
		m.newTaskTitle(slug, nil)
		return nil
	}
	items := []menuItem{{label: "no template", run: func(m *dash) tea.Cmd { m.newTaskTitle(slug, nil); return nil }}}
	for _, t := range ts {
		items = append(items, menuItem{label: templateLabel(t, ts), run: func(m *dash) tea.Cmd { m.newTaskTitle(slug, &t); return nil }})
	}
	m.push(&menuView{title: "New task · " + slug, items: items, centred: true})
	return nil
}

// templateLabel is template t as the menu offers it: its name (its
// <mod>:<name> when another of ts has the name) and its title pattern.
func templateLabel(t mod.Template, ts []mod.Template) string {
	name := t.Name
	for _, o := range ts {
		if o.Name == t.Name && o.Mod != t.Mod {
			name = t.Ref()
			break
		}
	}
	return name + " · " + oneLine(strings.Replace(t.Title, "{title}", "…", 1))
}

// newTaskTitle asks for the new task's title, then adds it with tpl.
func (m *dash) newTaskTitle(slug string, tpl *mod.Template) {
	title, label := "New task · "+slug, "Its title:"
	if tpl != nil {
		title += " · " + tpl.Name
		label = "Its title, for " + oneLine(tpl.Title) + " (with its notes and steps):"
	}
	m.promptNo(title, label, "no task added", 0, func(t string) tea.Cmd {
		src := m.src
		return m.act(func() actionMsg {
			ref, err := src.AddTask(slug, t, tpl)
			if err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{msg: "added " + ref + " to " + slug}
		})
	})
}
