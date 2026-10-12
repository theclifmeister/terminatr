package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The mods' commands (mods design §2 tier 4, §9): an enabled code mod's
// [provides].commands, each run by its key on the dashboard, by its
// entry in the ≡ menu (after the core's entries, with the mods' full
// views, each marked mod: id) or by tm mod run; the mod gets
// command.run with the project and the row selected (mod.command). The
// core's keys come first: a command whose key the core (or a mod
// enabled earlier) has keeps its menu entry, and the Mods tab says key
// conflict. The help lists them under Mod commands.

// modCommands are the commands of the mods that run: a user mod's none
// in safe mode.
func (m *dash) modCommands() []mod.ModCommand {
	var out []mod.ModCommand
	for _, c := range m.data.ModCommands {
		if !m.data.NoMods || c.Builtin {
			out = append(out, c)
		}
	}
	return out
}

// coreKey is the core's action (or "locked") on key k on the
// dashboard, "" when no core key is k: a command may have it then.
func coreKey(k, prefix string) string {
	if slices.Contains(lockedKeys, k) || k == prefix || k == "N" {
		return "locked"
	}
	for _, tb := range []struct {
		t    keyTable
		rows []bindable
	}{{binds.list, listBindables()}, {binds.side, sideBindables()}} {
		for _, r := range tb.rows {
			if slices.Contains(tb.t.keys(r.id, r.keys), k) {
				return r.id
			}
		}
	}
	return ""
}

// commandKey is c's key when it has it: "" when it has none or lost it
// to the core or to a mod enabled earlier.
func (m *dash) commandKey(c mod.ModCommand) string {
	if c.Key == "" || coreKey(c.Key, m.prefix) != "" {
		return ""
	}
	return c.Key
}

// keyConflicts are mod id's commands that lost their key, as the Mods
// tab names them: "key conflict: g is lab:sync's", "key conflict: j is
// the core's list.down".
func (m *dash) keyConflicts(id string) []string {
	var out []string
	for _, c := range m.modCommands() {
		switch {
		case c.Mod != id:
		case c.Conflict != "":
			out = append(out, c.Conflict)
		case c.Key != "" && coreKey(c.Key, m.prefix) != "":
			out = append(out, "key conflict: "+c.Key+" is the core's "+coreKey(c.Key, m.prefix))
		}
	}
	return out
}

// commandFor is the command typed key k runs on the dashboard.
func (m *dash) commandFor(k string) (mod.ModCommand, bool) {
	for _, c := range m.modCommands() {
		if m.commandKey(c) == k {
			return c, true
		}
	}
	return mod.ModCommand{}, false
}

// runCommand sends c to its mod, in the project shown and with the
// selected row; a mod limited to other projects isn't told.
func (m *dash) runCommand(c mod.ModCommand) tea.Cmd {
	slug := m.projectHere()
	if !c.Runs(slug) {
		m.msg = c.Title + " (mod: " + c.Mod + ") doesn't run for " + slug
		return nil
	}
	p := proto.ModCommandParams{Mod: c.Mod, Command: c.ID, Project: slug, Console: true}
	if r, ok := m.selected(); ok && slug != "" && r.project == slug {
		sel := proto.ModTarget{Project: slug, Session: r.session}
		if r.task != nil {
			sel.Task = r.task.Ref()
		}
		if r.thread != nil {
			sel.Thread = r.thread.ID
		}
		p.Selection = &sel
	}
	m.msg = "ran " + c.Title + " · mod: " + c.Mod
	src := m.src
	return func() tea.Msg { return modInputMsg{err: src.ModCommand(p)} }
}

// commandItems are the ≡ menu's items running the mods' commands that
// run for the project shown.
func (m *dash) commandItems() []menuItem {
	var out []menuItem
	slug := m.projectHere()
	for _, c := range m.modCommands() {
		if !c.InMenu() || !c.Runs(slug) {
			continue
		}
		out = append(out, menuItem{label: c.Title + " · mod: " + c.Mod, key: keyShown(m.commandKey(c)),
			run: func(m *dash) tea.Cmd { return m.runCommand(c) }})
	}
	return out
}

// commandHelp are the mods' commands as the help lists them: the key
// (none when it has none or lost it) and the title, with the mod.
func (m *dash) commandHelp() []keyHelp {
	var out []keyHelp
	for _, c := range m.modCommands() {
		help := c.Title + " (mod: " + c.Mod + "; tm mod run " + c.Mod + " " + c.ID + ")"
		if c.Key != "" && m.commandKey(c) == "" {
			help += "; its key " + c.Key + " is taken"
		}
		out = append(out, keyHelp{keyShown(m.commandKey(c)), help})
	}
	return out
}
