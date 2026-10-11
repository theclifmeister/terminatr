package config

import (
	"fmt"
	"slices"
	"strings"

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
	// Order is the table's place among the [mods.<id>] tables, from 0:
	// the order they were enabled in, as each enable appends its table.
	// Where two mods want one thing (a key), the first wins.
	Order int
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
	order := map[string]int{}
	for _, k := range md.Keys() {
		if len(k) == 2 && k[0] == "mods" {
			if _, ok := order[k[1]]; !ok {
				order[k[1]] = len(order)
			}
		}
	}
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
			mods[id] = Mod{Order: order[id], Err: fmt.Errorf("mods.%s: %w", id, err)}
			continue
		}
		mods[id] = Mod{Enabled: r.Enabled != nil && *r.Enabled, Grants: r.Grants, Projects: r.Projects, Settings: r.Settings, Order: order[id]}
	}
	return old, mods, unknown, nil
}

// ModsTable is the config.toml table of mod id.
func ModsTable(id string) string { return "mods." + id }

// RemoveModsTable removes mod id's [mods.<id>] table and the tables
// under it ([mods.<id>.settings]) from config.toml, everything else kept
// (tm mod remove, and tm mod install of an id whose table was left
// behind, so a new install starts disabled and ungranted). ErrForm when
// the file sets them in a form the line editor doesn't change.
func RemoveModsTable(id string) error {
	return edit(func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, nil
		}
		return DropTable(data, ModsTable(id))
	})
}

// DropTable returns data without table and the tables below it: their
// header lines and every line up to the next table that isn't one of
// them. A file without them comes back as it was.
func DropTable(data []byte, table string) ([]byte, error) {
	want := splitTable(table)
	under := func(h []string) bool { return len(h) >= len(want) && equal(h[:len(want)], want) }
	var out []string
	in := false
	for _, l := range strings.SplitAfter(string(data), "\n") {
		if h, ok := header(l); ok {
			in = under(h)
		} else if m := arrayHeaderRE.FindStringSubmatch(strings.TrimRight(l, "\r\n")); m != nil {
			in = under(splitTable(m[1]))
		}
		if !in {
			out = append(out, l)
		}
	}
	res := []byte(strings.Join(out, ""))
	var got map[string]any
	if _, err := toml.Decode(string(res), &got); err != nil {
		return nil, fmt.Errorf("the settings can't be read: %w", err)
	}
	v := any(got)
	for _, k := range want {
		m, ok := v.(map[string]any)
		if !ok {
			return res, nil
		}
		if v, ok = m[k]; !ok {
			return res, nil
		}
	}
	return nil, ErrForm
}
