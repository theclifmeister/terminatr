package mod

import (
	"fmt"
	"slices"
	"sort"
)

// Commands (mods design §2 tier 4, §8): a code mod's [provides].commands
// are run by the human, with a key on the dashboard, from the ≡ menu's
// Mods group or with tm mod run; the mod gets command.run. A key two
// mods bind goes to the one enabled first; the other keeps its menu
// entry and tm mod run, and the Mods tab says "key conflict".

// ModCommand is one command of an enabled code mod, as the dashboard
// offers it.
type ModCommand struct {
	Command
	Mod string
	// Name is the mod's name, for the menu and the help.
	Name string
	// Builtin: a bundled mod's, which runs in safe mode too.
	Builtin bool
	// Projects are the mod's projects; empty for all.
	Projects []string
	// Conflict says why the command lost its key ("" when it has it):
	// Key is then "".
	Conflict string
}

// Commands are the commands of the enabled code mods, in the order the
// mods were enabled (then by id), each mod's in its manifest's order.
func (r *Registry) Commands() []ModCommand {
	var mods []Mod
	for _, m := range r.Mods {
		if m.State == Enabled && m.Manifest != nil && m.Manifest.IsCode() && len(m.Manifest.Provides.Commands) > 0 {
			mods = append(mods, m)
		}
	}
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].Order < mods[j].Order })
	var out []ModCommand
	owner := map[string]string{} // key → the command that has it
	for _, m := range mods {
		for _, c := range m.Manifest.Provides.Commands {
			mc := ModCommand{Command: c, Mod: m.ID, Name: Clean(m.Manifest.Name, false), Builtin: m.Builtin, Projects: m.Projects}
			mc.Title = Clean(c.Title, false)
			if o, ok := owner[c.Key]; ok && c.Key != "" {
				mc.Conflict = fmt.Sprintf("key conflict: %s is %s's", c.Key, o)
				mc.Key = ""
			} else if c.Key != "" {
				owner[c.Key] = CommandName(m.ID, c.ID)
			}
			out = append(out, mc)
		}
	}
	return out
}

// Runs says whether the command's mod runs for project slug ("": any).
func (c ModCommand) Runs(slug string) bool {
	return slug == "" || len(c.Projects) == 0 || slices.Contains(c.Projects, slug)
}

// DeclaredCommand is command id of the manifest m.
func (m *Manifest) DeclaredCommand(id string) (Command, bool) {
	if m == nil {
		return Command{}, false
	}
	for _, c := range m.Provides.Commands {
		if c.ID == id {
			return c, true
		}
	}
	return Command{}, false
}
