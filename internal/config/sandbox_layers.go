package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// How the sandbox tables add up (T574 §3.2): a list adds down the
// layers, all projects' first, then the project's own; a project's drop
// names entries of all projects' lists its threads don't take.
// local_ports is the project's when it sets it, else all projects',
// else off. An entry that fails its check is refused on its own: the
// rest still hold (P10).
//
// Until the release's migration (T584) rewrites config.toml, a
// project's excluded_commands and allow keep tm 0.37's meaning: the
// project's list, when it sets one (an empty one too), replaces all
// projects' (ReplaceKeys). The migration turns each such list into the
// project's entries plus drops of all projects' it lacked, and empties
// ReplaceKeys.

// ReplaceKeys are the lists whose project list replaces all projects'
// rather than adding to it, as in tm 0.37, until the migration.
var ReplaceKeys = []string{SandboxExcluded, SandboxAllow}

// SandboxTable is one sandbox table's rules: its lists by key, only
// those with entries (a project's drops under SandboxDrop), and its
// local ports when it sets them. The old preset switches it still has
// read as the rules they unfold into. Replaces are the ReplaceKeys a
// project's table sets, an empty list too: its own list of each
// replaces all projects'.
type SandboxTable struct {
	Lists      map[string][]string
	LocalPorts *bool
	Replaces   []string
}

// Replacing reports whether the table's list of key replaces all
// projects'.
func (t SandboxTable) Replacing(key string) bool { return slices.Contains(t.Replaces, key) }

// OwnList makes the table's list of key (a ReplaceKeys key) its own,
// starting from given, what its threads get now, so nothing changes
// until it is edited.
func (t *SandboxTable) OwnList(key string, given []string) {
	if t.Replacing(key) || !slices.Contains(ReplaceKeys, key) {
		return
	}
	t.Replaces = append(slices.Clone(t.Replaces), key)
	for _, v := range given {
		t.Add(key, v)
	}
}

// StartOwnLists makes each ReplaceKeys list a project's table next
// starts (it has entries, and the table before didn't replace all
// projects' list) its own, starting from what its threads got with all
// projects' table all and its table before, the new entries after
// them: the others stay.
func StartOwnLists(all, before SandboxTable, next *SandboxTable) {
	given := MergeSandbox(all, before)
	for _, k := range ReplaceKeys {
		if added := next.Lists[k]; len(added) > 0 && !next.Replacing(k) {
			delete(next.Lists, k)
			next.OwnList(k, given.Given(k))
			for _, v := range added {
				next.Add(k, v)
			}
		}
	}
}

// Follow drops the table's own list of key, so its threads get all
// projects' again.
func (t *SandboxTable) Follow(key string) {
	delete(t.Lists, key)
	t.Replaces = slices.DeleteFunc(slices.Clone(t.Replaces), func(k string) bool { return k == key })
}

// Has reports whether the table's list of key holds v.
func (t SandboxTable) Has(key, v string) bool { return slices.Contains(t.Lists[key], v) }

// Add puts v last in the table's list of key, unless it is there.
func (t *SandboxTable) Add(key, v string) {
	if t.Lists == nil {
		t.Lists = map[string][]string{}
	}
	if !t.Has(key, v) {
		t.Lists[key] = append(slices.Clone(t.Lists[key]), v)
	}
}

// Remove takes v out of the table's list of key.
func (t *SandboxTable) Remove(key, v string) {
	l := slices.DeleteFunc(slices.Clone(t.Lists[key]), func(e string) bool { return e == v })
	if len(l) == 0 {
		delete(t.Lists, key)
		return
	}
	t.Lists[key] = l
}

// AddRule adds preset rule r: an entry, or local ports on.
func (t *SandboxTable) AddRule(r PresetRule) {
	if r.Key == SandboxLocalPorts {
		on := true
		t.LocalPorts = &on
		return
	}
	t.Add(r.Key, r.Value)
}

// Clone is a copy of t to change.
func (t SandboxTable) Clone() SandboxTable {
	out := SandboxTable{Lists: map[string][]string{}}
	for k, l := range t.Lists {
		out.Lists[k] = slices.Clone(l)
	}
	if t.LocalPorts != nil {
		on := *t.LocalPorts
		out.LocalPorts = &on
	}
	out.Replaces = slices.Clone(t.Replaces)
	return out
}

// table is r's rules, its old switches unfolded: on, a preset's rules;
// a project's off, drops of them (local ports off).
func (r *rawSandbox) table(project bool) SandboxTable {
	t := SandboxTable{Lists: map[string][]string{}}
	if r == nil {
		return t
	}
	for _, k := range append(slices.Clone(SandboxKeys), SandboxDrop) {
		if l := r.list(k); l != nil {
			for _, e := range *l {
				t.Add(k, e)
			}
		}
	}
	if r.LocalPorts != nil {
		on := *r.LocalPorts
		t.LocalPorts = &on
	}
	for _, k := range ReplaceKeys {
		if project && r.list(k) != nil {
			t.Replaces = append(t.Replaces, k)
		}
	}
	for _, s := range oldSwitches {
		on := r.oldSwitch(s.key)
		if on == nil || (!*on && !project) {
			continue
		}
		p, _ := PresetNamed(s.preset)
		for _, rule := range p.Rules {
			switch {
			case rule.Key == SandboxLocalPorts:
				if t.LocalPorts == nil {
					v := *on
					t.LocalPorts = &v
				}
			case *on:
				t.Add(rule.Key, rule.Value)
			default:
				t.Add(SandboxDrop, rule.Value)
			}
		}
	}
	return t
}

// hasOldSwitch reports whether r sets an old preset switch.
func (r *rawSandbox) hasOldSwitch() bool {
	return slices.ContainsFunc(oldSwitches, func(s struct{ key, preset string }) bool { return r.oldSwitch(s.key) != nil })
}

// SandboxRule is one entry a project's threads get from the sandbox
// lists, or would but for a drop or its check.
type SandboxRule struct {
	Key, Value string
	// Own: the project's, else all projects'.
	Own bool
	// Dropped: all projects', which the project drops; not given.
	Dropped bool
	// Replaced: all projects', which the project's own list of a
	// ReplaceKeys key replaces; not given.
	Replaced bool
	// Refused is why the entry fails its check; not given.
	Refused string
}

// Given reports whether threads get the rule.
func (r SandboxRule) Given() bool { return !r.Dropped && !r.Replaced && r.Refused == "" }

// ThreadSandbox is what a project's threads get: each list's entries,
// all projects' first, with those the project drops or that fail their
// check marked; local ports and where they come from; and the project's
// drops, those that match nothing too.
type ThreadSandbox struct {
	Rules         []SandboxRule
	LocalPorts    bool
	LocalPortsSet bool // a table sets them, else tm's default (off)
	LocalPortsOwn bool // the project's
	Drops         []string
}

// Given are the entries of key's list threads get.
func (t ThreadSandbox) Given(key string) []string {
	var out []string
	for _, r := range t.Rules {
		if r.Key == key && r.Given() {
			out = append(out, r.Value)
		}
	}
	return out
}

// Has reports whether threads get preset rule r already.
func (t ThreadSandbox) Has(r PresetRule) bool {
	if r.Key == SandboxLocalPorts {
		return t.LocalPorts
	}
	return slices.Contains(t.Given(r.Key), r.Value)
}

// Empty reports whether it opens nothing.
func (t ThreadSandbox) Empty() bool {
	return !t.LocalPorts && !slices.ContainsFunc(t.Rules, SandboxRule.Given)
}

// ThreadSandbox is what project slug's threads get (slug "": what all
// projects' table gives). None for a nil Config.
func (c *Config) ThreadSandbox(slug string) ThreadSandbox {
	if c == nil {
		return ThreadSandbox{}
	}
	var own SandboxTable
	if slug != "" {
		own = c.projects[slug].Sandbox.table(true)
	}
	return MergeSandbox(c.defaults.Sandbox.table(false), own)
}

// MergeSandbox is what threads get from all projects' table all and a
// project's own (an empty one for all projects').
func MergeSandbox(all, own SandboxTable) ThreadSandbox {
	var out ThreadSandbox
	drops := own.Lists[SandboxDrop]
	out.Drops = slices.Clone(drops)
	refused := func(key, v string) string {
		if err := checkSandboxEntry(key, v); err != nil {
			return err.Error()
		}
		return ""
	}
	for _, k := range SandboxKeys {
		replaced := own.Replacing(k)
		for _, v := range all.Lists[k] {
			out.Rules = append(out.Rules, SandboxRule{Key: k, Value: v, Dropped: slices.Contains(drops, v), Replaced: replaced, Refused: refused(k, v)})
		}
		for _, v := range own.Lists[k] {
			if !replaced && slices.Contains(all.Lists[k], v) && !slices.Contains(drops, v) {
				continue // all projects give it already
			}
			out.Rules = append(out.Rules, SandboxRule{Key: k, Value: v, Own: true, Refused: refused(k, v)})
		}
	}
	switch {
	case own.LocalPorts != nil:
		out.LocalPorts, out.LocalPortsSet, out.LocalPortsOwn = *own.LocalPorts, true, true
	case all.LocalPorts != nil:
		out.LocalPorts, out.LocalPortsSet = *all.LocalPorts, true
	}
	return out
}

// StrayDrops are the project's drops that match none of all projects'
// rules: they drop nothing now.
func (t ThreadSandbox) StrayDrops() []string {
	var out []string
	for _, d := range t.Drops {
		if !slices.ContainsFunc(t.Rules, func(r SandboxRule) bool { return !r.Own && r.Value == d }) {
			out = append(out, d)
		}
	}
	return out
}

// SandboxOf is one table's rules: all projects' ([defaults.sandbox])
// for slug "", else the project's own. None for a nil Config.
func (c *Config) SandboxOf(slug string) SandboxTable {
	if c == nil {
		return SandboxTable{Lists: map[string][]string{}}
	}
	if slug == "" {
		return c.defaults.Sandbox.table(false)
	}
	return c.projects[slug].Sandbox.table(true)
}

// checkSandboxTable checks what Load must refuse in a table: a drop in
// all projects' table, which has nothing above it to drop from. Its
// entries are checked one by one where they are used.
func checkSandboxTable(r *rawSandbox, path, table string) error {
	if table == DefaultsTable && r != nil && r.Drop != nil {
		return fmt.Errorf("%s: %s.sandbox.drop: only a project drops rules (of all projects')", path, table)
	}
	return nil
}

// sandboxTableOf is table's sandbox table, DefaultsTable or a project's
// ("projects.<slug>").
func sandboxTableOf(table string) (string, bool, error) {
	if table == DefaultsTable {
		return table + ".sandbox", false, nil
	}
	if slug, ok := strings.CutPrefix(table, "projects."); ok && slug != "" && !strings.Contains(slug, ".") {
		return table + ".sandbox", true, nil
	}
	return "", false, fmt.Errorf("no sandbox rules in [%s]", table)
}

// EditSandbox changes table's sandbox table (DefaultsTable or
// "projects.<slug>") in one write: fn changes its rules as config.toml
// has them now, and they are written back, each entry fn added checked
// as Load's users check it, none twice. A project's entry added to a
// ReplaceKeys list it doesn't set yet starts its own list from what its
// threads get, so the others stay. The old preset switches go in
// the same write, their rules written as plain ones. ErrForm when the
// file sets a key in a form the line editor doesn't change.
func EditSandbox(table string, fn func(*SandboxTable) error) error {
	t, project, err := sandboxTableOf(table)
	if err != nil {
		return err
	}
	return edit(func(data []byte) ([]byte, error) {
		var raw struct {
			Defaults struct{ Sandbox *rawSandbox }
			Projects map[string]struct{ Sandbox *rawSandbox }
		}
		if _, err := toml.Decode(string(data), &raw); err != nil {
			return nil, fmt.Errorf("the settings can't be read: %w", err)
		}
		r := raw.Defaults.Sandbox
		if project {
			r = raw.Projects[strings.TrimPrefix(table, "projects.")].Sandbox
		}
		before := r.table(project)
		next := before.Clone()
		if err := fn(&next); err != nil {
			return nil, err
		}
		if !project && len(next.Lists[SandboxDrop]) > 0 {
			return nil, fmt.Errorf("only a project drops rules (of all projects')")
		}
		if project {
			StartOwnLists(raw.Defaults.Sandbox.table(false), before, &next)
		}
		for _, k := range append(slices.Clone(SandboxKeys), SandboxDrop) {
			l := next.Lists[k]
			for i, e := range l {
				if slices.Contains(l[:i], e) {
					return nil, fmt.Errorf("%q is listed twice", e)
				}
				if before.Has(k, e) {
					continue // the file had it: refused where used, removable here
				}
				if err := checkSandboxEntry(k, e); err != nil {
					return nil, err
				}
			}
			old := r.list(k)
			set := len(l) > 0 || project && next.Replacing(k) // an own empty list replaces too
			switch {
			case !set && old == nil:
			case !set:
				if data, err = remove(data, t, k); err != nil {
					return nil, err
				}
			case old == nil || !slices.Equal(*old, l):
				if l == nil {
					l = []string{}
				}
				if data, err = Edit(data, t, k, l); err != nil {
					return nil, err
				}
			}
		}
		var old *bool
		if r != nil {
			old = r.LocalPorts
		}
		switch {
		case next.LocalPorts == nil && old != nil:
			if data, err = remove(data, t, SandboxLocalPorts); err != nil {
				return nil, err
			}
		case next.LocalPorts != nil && (old == nil || *old != *next.LocalPorts):
			if data, err = Edit(data, t, SandboxLocalPorts, *next.LocalPorts); err != nil {
				return nil, err
			}
		}
		if r.hasOldSwitch() {
			for _, k := range OldSwitchKeys() {
				if data, err = remove(data, t, k); err != nil {
					return nil, err
				}
			}
		}
		return data, nil
	})
}

// remove is data without key in table; ErrForm when it is set in a form
// the line editor doesn't change.
func remove(data []byte, table, key string) ([]byte, error) {
	out := Remove(data, table, key)
	if string(out) == string(data) && sets(data, table, key) {
		return nil, ErrForm
	}
	return out, nil
}

// SandboxTables are every table that sets sandbox rules: "" for all
// projects', else the project's slug. For the Access tab's notes.
func (c *Config) SandboxTables() map[string]SandboxTable {
	out := map[string]SandboxTable{}
	if c == nil {
		return out
	}
	out[""] = c.SandboxOf("")
	for _, slug := range slices.Sorted(maps.Keys(c.projects)) {
		if c.projects[slug].Sandbox != nil {
			out[slug] = c.SandboxOf(slug)
		}
	}
	return out
}
