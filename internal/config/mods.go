package config

import (
	"errors"
	"fmt"
	"maps"
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
	// EnabledSet: the table sets enabled, true or false. A built-in mod
	// is on unless its table sets enabled = false.
	EnabledSet bool
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
	// ProjectSettings are [projects.<slug>.mods.<id>.settings] by slug,
	// the mod's options for one project, each over Settings key by key;
	// internal/mod checks them as it does Settings.
	ProjectSettings map[string]map[string]any
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

// oldModsKeys are the older [mods] keys of the removed agent mod's
// settings, later [agent_mod]'s.
var oldModsKeys = []string{"enabled", "band", "pane"}

// readMods splits the [mods] table into the older agent mod keys
// (enabled, band, pane: old, as "mods.enabled", read whatever their
// value) and the user mods' [mods.<id>] tables; a bad mod table is that
// mod's Err. unknown lists the other keys directly under [mods]
// ("mods.sidebar").
func readMods(md toml.MetaData, prims map[string]toml.Primitive) (old []string, mods map[string]Mod, unknown []string) {
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
			md.PrimitiveDecode(p, new(any))
			old = append(old, "mods."+id)
			continue
		}
		// A table, or one only its [mods.<id>.settings] defines (no type).
		if t := md.Type("mods", id); t != "Hash" && t != "" {
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
		mods[id] = Mod{Enabled: r.Enabled != nil && *r.Enabled, EnabledSet: r.Enabled != nil, Grants: r.Grants, Projects: r.Projects, Settings: r.Settings, Order: order[id]}
	}
	return old, mods, unknown
}

// readProjectMods adds each project's [projects.<slug>.mods.<id>]
// table to mods[id].ProjectSettings, adding an entry for a mod that has
// no [mods.<id>] table (a built-in one, or one not installed). Such a
// table holds only [settings]: another key, or a value that isn't a
// table, is that mod's Err, as a bad [mods.<id>] table is. A project's
// mods that isn't a table is an error: no mod can be named.
func readProjectMods(md toml.MetaData, projects map[string]rawSafety, mods map[string]Mod) error {
	var errs []error
	for _, slug := range slices.Sorted(maps.Keys(projects)) {
		if !md.IsDefined("projects", slug, "mods") {
			continue
		}
		var v any
		md.PrimitiveDecode(projects[slug].Mods, &v)
		tables, ok := v.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Errorf("projects.%s.mods: must be tables, [projects.%s.mods.<id>.settings]", slug, slug))
			continue
		}
		for _, id := range slices.Sorted(maps.Keys(tables)) {
			m, ok := mods[id]
			if !ok {
				m = Mod{Order: len(mods)}
			}
			var err error
			raw, ok := tables[id].(map[string]any)
			if !ok {
				err = fmt.Errorf("must be a table, [projects.%s.mods.%s.settings]", slug, id)
			}
			set, isTable := raw["settings"].(map[string]any)
			for _, k := range slices.Sorted(maps.Keys(raw)) {
				if k != "settings" && err == nil {
					err = fmt.Errorf("%s is not a key here; a mod's settings for a project are [projects.%s.mods.%s.settings]", k, slug, id)
				}
			}
			if err == nil && raw["settings"] != nil && !isTable {
				err = fmt.Errorf("settings must be a table")
			}
			switch {
			case err != nil && m.Err == nil:
				m.Err = fmt.Errorf("projects.%s.mods.%s: %w", slug, id, err)
			case err == nil && set != nil:
				if m.ProjectSettings == nil {
					m.ProjectSettings = map[string]map[string]any{}
				}
				m.ProjectSettings[slug] = set
			}
			mods[id] = m
		}
	}
	return errors.Join(errs...)
}

// ProjectModsTable is the config.toml table of mod id's settings for
// project slug.
func ProjectModsTable(slug, id string) string {
	return "projects." + slug + ".mods." + id + ".settings"
}

// ModsTable is the config.toml table of mod id.
func ModsTable(id string) string { return "mods." + id }

// RemoveModsTable removes mod id's [mods.<id>] table and the tables
// under it ([mods.<id>.settings]), and each project's
// [projects.<slug>.mods.<id>], from config.toml, everything else kept
// (tm mod remove, and tm mod install of an id whose table was left
// behind, so a new install starts disabled and ungranted). ErrForm when
// the file sets them in a form the line editor doesn't change.
func RemoveModsTable(id string) error {
	return edit(func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, nil
		}
		var file struct {
			Projects map[string]struct {
				Mods map[string]any `toml:"mods"`
			} `toml:"projects"`
		}
		// A file that doesn't read so is refused by DropTable below.
		toml.Decode(string(data), &file)
		out, err := DropTable(data, ModsTable(id))
		for _, slug := range slices.Sorted(maps.Keys(file.Projects)) {
			if _, ok := file.Projects[slug].Mods[id]; ok && err == nil {
				out, err = DropTable(out, "projects."+slug+".mods."+id)
			}
		}
		return out, err
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
	for _, l := range statements(data) {
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
