package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The mods' commands (mods design §2 tier 4, §9): an enabled code mod's
// [provides].commands, each run by its key on the dashboard, by
// prefix+key anywhere (in a session too, attachcommands.go), by its
// entry in the ≡ menu (after the core's entries, with the mods' full
// views, each with its mod's id; a session's ≡ menu too) or by tm mod run;
// the mod gets
// command.run with the project and the row selected (mod.command). The
// core's keys come first: a command whose key the core (or a mod
// enabled earlier) has keeps its menu entry, and the Mods tab says key
// conflict. The help lists them under Mod commands.

// modCommands are the commands of the mods that run: a user mod's none
// in safe mode.
func (m *dash) modCommands() []mod.ModCommand {
	return runningCommands(m.data.ModCommands, m.data.NoMods)
}

// runningCommands are the commands of list that run: a user mod's none
// in safe mode (noMods).
func runningCommands(list []mod.ModCommand, noMods bool) []mod.ModCommand {
	var out []mod.ModCommand
	for _, c := range list {
		if !noMods || c.Builtin {
			out = append(out, c)
		}
	}
	return out
}

// coreKey is the core's action (or "locked", or its prefix command) on
// key k on the dashboard and after the prefix, "" when no core key is k:
// a command may have it then.
func coreKey(k, prefix string) string {
	if slices.Contains(lockedKeys, k) || k == prefix || k == "N" {
		return "locked"
	}
	if prefixCommands[k] || paneCommands[k] {
		// prefix+k is the core's, even with its action bound elsewhere.
		return "prefix+" + k
	}
	for _, tb := range []struct {
		t    keyTable
		rows []bindable
	}{{binds.list, listBindables()}, {binds.side, sideBindables()}, {binds.panel, panelBindables()}} {
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
func (m *dash) commandKey(c mod.ModCommand) string { return commandKeyOf(c, m.prefix) }

// commandKeyOf is c's key with prefix the prefix key: the same on the
// dashboard (the key, or prefix+key) and in a session (prefix+key),
// since the core's dashboard keys hold every prefix command's.
func commandKeyOf(c mod.ModCommand, prefix string) string {
	if c.Key == "" || coreKey(c.Key, prefix) != "" {
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
		m.msg = c.Title + " (" + c.Mod + ") doesn't run for " + slug
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
	m.msg = "ran " + c.Title
	src := m.src
	return func() tea.Msg { return modInputMsg{err: src.ModCommand(p)} }
}

// sessionTarget is session s as a command's selection in project slug;
// nil when it is another project's.
func sessionTarget(s proto.SessionInfo, slug string) *proto.ModTarget {
	if slug == "" || s.Project != slug {
		return nil
	}
	return &proto.ModTarget{Project: slug, Session: s.ID, Thread: s.Thread, Task: s.Task}
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
		out = append(out, menuItem{label: c.Title + " · " + c.Mod, key: keyShown(m.commandKey(c)),
			run: func(m *dash) tea.Cmd { return m.runCommand(c) }})
	}
	return out
}

// commandHelp are the mods' commands as the help lists them, for the
// commands that run for the project shown: the keys (the dashboard's
// and prefix+key; none when it has none or lost it, then its ≡ menu
// entry, or none) and the title with the mod's name. The keys are the
// effective ones: a key the core (or a keymap) holds isn't the command's.
// A last line says where the keys inside a mod's own view are.
func (m *dash) commandHelp() []keyHelp {
	var out []keyHelp
	slug := m.projectHere()
	for _, c := range m.modCommands() {
		if !c.Runs(slug) {
			continue
		}
		name := c.Name
		if name == "" {
			name = c.Mod
		}
		help := c.Title + " · " + name + " (tm mod run " + c.Mod + " " + c.ID + ")"
		keys := "≡ menu"
		switch k := m.commandKey(c); {
		case k != "":
			keys = keyShown(k) + " prefix+" + keyShown(k)
		case c.Key != "":
			help += "; its key " + c.Key + " is taken"
		case !c.InMenu():
			keys = "tm mod run"
		}
		out = append(out, keyHelp{keys, help})
	}
	if len(out) > 0 {
		out = append(out, keyHelp{"in a mod's view", "the keys a mod's own view takes are in that view's hint"})
	}
	return out
}

// commandKeyed says whether key k runs a mod's command here.
func (m *dash) commandKeyed(k string) bool {
	_, ok := m.commandFor(k)
	return ok
}
