package mod

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
)

// State is where a mod stands in the registry: whether it may run, not
// whether a host is running it (the supervisor's, later).
type State string

const (
	// Invalid: the manifest is missing or broken, or its [mods.<id>]
	// table is; the mod stays installed and doesn't run.
	Invalid State = "invalid"
	// Incompatible: this tm doesn't speak the mod API it needs (CheckAPI).
	Incompatible State = "incompatible"
	// Disabled: installed and valid, not enabled. A mod is installed so.
	Disabled State = "disabled"
	// Enabled: the human enabled it; it runs for its projects.
	Enabled State = "enabled"
)

// Mod is one installed mod, its manifest joined with its [mods.<id>]
// table.
type Mod struct {
	// ID is the folder's name, which a valid manifest's id matches.
	ID  string
	Dir string
	// Manifest is nil when mod.toml is missing or doesn't parse.
	Manifest *Manifest
	// Err says why the mod is Invalid or Incompatible.
	Err   error
	State State
	// Projects limits the mod to these projects; empty means all.
	Projects []string
	// Grants are the permissions it runs with: granted ∩ requested, in
	// Perms order. WantsMore are those it asks for that the human hasn't
	// granted (after an update that asks for more, say).
	Grants, WantsMore []string
	// Settings are its options: each manifest default, overlaid with
	// [mods.<id>.settings] where that is valid. BadSettings say which
	// set values were refused (and the default used), or which names
	// the manifest doesn't have.
	Settings    map[string]any
	BadSettings []string
	// Order is its [mods.<id>] table's place in config.toml: the order
	// mods were enabled in (config.Mod.Order).
	Order int
	// Builtin: the mod ships inside tm (a bundled mod, mods design §11,
	// bundled.go). It is never removed or updated on its own: it updates
	// with tm. Its files are in FS, not in a folder (Dir is "").
	Builtin bool
	// Required: a built-in default tm can't do without (the look, the
	// keys, the layout, the policy); it can't be turned off, only
	// replaced by another mod.
	Required bool
	// FS holds a built-in mod's files; nil for an installed one, whose
	// files are in Dir.
	FS fs.FS
}

// Runs reports whether the mod runs for project slug: valid, compatible,
// enabled, and slug is one of its projects (any, when it lists none).
func (m Mod) Runs(slug string) bool {
	return m.State == Enabled && (len(m.Projects) == 0 || slices.Contains(m.Projects, slug))
}

// Granted reports whether the mod runs with permission perm.
func (m Mod) Granted(perm string) bool { return slices.Contains(m.Grants, perm) }

// Registry is the installed mods, by id, then the built-in ones, by id,
// and the [mods.<id>] tables of
// mods that aren't installed.
type Registry struct {
	Mods []Mod
	// Orphans are ids config.toml has a [mods.<id>] table for that no
	// installed mod has: tm doctor lists them.
	Orphans []string
	// Shadowed are installed folders named as a built-in mod: they
	// don't load (tm doctor lists them).
	Shadowed []string
}

// Get returns the mod id.
func (r *Registry) Get(id string) (Mod, bool) {
	for _, m := range r.Mods {
		if m.ID == id {
			return m, true
		}
	}
	return Mod{}, false
}

// For lists the mods that run for project slug, by id.
func (r *Registry) For(slug string) []Mod {
	var out []Mod
	for _, m := range r.Mods {
		if m.Runs(slug) {
			out = append(out, m)
		}
	}
	return out
}

// Load reads the mods installed in ~/.terminatr/mods, adds the bundled
// ones (bundled.go) and joins them with c's [mods.<id>] tables (c nil:
// none). A missing folder is no installed mods.
func Load(c *config.Config) (*Registry, error) {
	dir, err := home.ModsDir()
	if err != nil {
		return nil, err
	}
	var tables map[string]config.Mod
	if c != nil {
		tables = c.Mods
	}
	r, err := LoadDir(dir, tables)
	if err != nil {
		return nil, err
	}
	r.withBundled(tables)
	return r, nil
}

// LoadDir is Load from the folder dir with the tables given, without the
// bundled mods. One broken mod never stops the others loading: it is
// listed Invalid.
func LoadDir(dir string, tables map[string]config.Mod) (*Registry, error) {
	ents, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	r := &Registry{}
	seen := map[string]bool{}
	for _, e := range ents {
		// A folder, or a link to one (tm mod dev links a work folder).
		if e.Name()[0] == '.' {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			continue
		}
		seen[e.Name()] = true
		r.Mods = append(r.Mods, load(e.Name(), p, tables[e.Name()]))
	}
	sort.Slice(r.Mods, func(i, j int) bool { return r.Mods[i].ID < r.Mods[j].ID })
	for id := range tables {
		if !seen[id] {
			r.Orphans = append(r.Orphans, id)
		}
	}
	slices.Sort(r.Orphans)
	return r, nil
}

// load reads the mod in folder dir, named id, with its table t.
func load(id, dir string, t config.Mod) Mod {
	m := Mod{ID: id, Dir: dir, State: Invalid, Projects: t.Projects, Order: t.Order}
	if err := CheckID(id); err != nil {
		m.Err = fmt.Errorf("folder %q: %w", id, err)
		return m
	}
	man, err := ReadManifest(dir)
	m.Manifest = man
	switch {
	case man != nil && man.ID != "" && man.ID != id:
		m.Err = fmt.Errorf("its id is %q but its folder is %q", man.ID, id)
		return m
	case err != nil:
		m.Err = err
		return m
	case t.Err != nil:
		m.Err = t.Err
		return m
	}
	asked := man.Permissions.Requested()
	for _, p := range asked {
		if slices.Contains(t.Grants, p) {
			m.Grants = append(m.Grants, p)
		} else {
			m.WantsMore = append(m.WantsMore, p)
		}
	}
	m.Settings = map[string]any{}
	for _, k := range sortedKeys(man.Settings) {
		m.Settings[k], _ = man.Settings[k].Check(man.Settings[k].Default)
	}
	for _, k := range sortedKeys(t.Settings) {
		s, ok := man.Settings[k]
		if !ok {
			m.BadSettings = append(m.BadSettings, fmt.Sprintf("%s: %s has no such setting", k, id))
			continue
		}
		v, err := s.Check(t.Settings[k])
		if err != nil {
			m.BadSettings = append(m.BadSettings, fmt.Sprintf("%s %v; the default is used", k, err))
			continue
		}
		m.Settings[k] = v
	}
	if err := CheckAPI(man); err != nil {
		m.Err, m.State = err, Incompatible
		return m
	}
	m.State = Disabled
	if t.Enabled {
		m.State = Enabled
	}
	return m
}
