package config

import (
	"fmt"
	"slices"

	"github.com/BurntSushi/toml"
)

// Mod is a user mod's [mods.<id>] table (mods design §7, Where grants
// live): written by the human's calls only (internal/mod), read here
// with the rest of the file.
type Mod struct {
	// Enabled is enabled: the mod runs. Off unless set true; a mod is
	// installed disabled.
	Enabled bool
	// Grants are the permissions the human accepted, by name; the mod
	// runs with these ∩ those its manifest asks for.
	Grants []string
	// Projects, when set, limits the mod to these projects; nil means
	// every project.
	Projects []string
	// Settings are [mods.<id>.settings], the mod's own options as
	// decoded (int64, bool, string, …); internal/mod checks them against
	// the manifest.
	Settings map[string]any
	// Err says why the table couldn't be read (a key of the wrong type);
	// such a mod doesn't run, and nothing else in tm minds.
	Err error
}

// rawMod is a [mods.<id>] table.
type rawMod struct {
	Enabled  *bool          `toml:"enabled"`
	Grants   []string       `toml:"grants"`
	Projects []string       `toml:"projects"`
	Settings map[string]any `toml:"settings"`
}

// oldModsKeys are the older [mods] keys, now [agent_mod]'s.
var oldModsKeys = []string{"enabled", "band", "pane"}

// readMods splits the [mods] table into the older [agent_mod] keys
// (enabled, band, pane) and the user mods' [mods.<id>] tables. Only an
// older key of the wrong type is an error, as it was before mods took
// the table; a bad mod table is that mod's Err. unknown lists the other
// keys directly under [mods] ("mods.sidebar").
func readMods(md toml.MetaData, prims map[string]toml.Primitive) (old rawAgentMod, mods map[string]Mod, unknown []string, err error) {
	mods = map[string]Mod{}
	for id, p := range prims {
		if slices.Contains(oldModsKeys, id) {
			var b bool
			if err := md.PrimitiveDecode(p, &b); err != nil {
				return old, nil, nil, fmt.Errorf("mods.%s must be true or false", id)
			}
			switch id {
			case "enabled":
				old.Enabled = &b
			case "band":
				old.Band = &b
			case "pane":
				old.Pane = &b
			}
			continue
		}
		if md.Type("mods", id) != "Hash" {
			unknown = append(unknown, "mods."+id)
			continue
		}
		var r rawMod
		if err := md.PrimitiveDecode(p, &r); err != nil {
			// Mark the table read, so its keys aren't also listed as
			// unknown: the error says what is wrong.
			md.PrimitiveDecode(p, new(map[string]any))
			mods[id] = Mod{Err: fmt.Errorf("mods.%s: %w", id, err)}
			continue
		}
		mods[id] = Mod{Enabled: r.Enabled != nil && *r.Enabled, Grants: r.Grants, Projects: r.Projects, Settings: r.Settings}
	}
	return old, mods, unknown, nil
}

// ModsTable is the config.toml table of mod id.
func ModsTable(id string) string { return "mods." + id }
