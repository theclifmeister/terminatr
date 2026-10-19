package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// How the sandbox tables add up (T574 §3.2): a list adds down the
// layers, all projects' first, then the project's own; a project's drop
// names entries of all projects' lists its threads don't take.
// local_ports is the project's when it sets it, else all projects',
// else off. An entry that fails its check is refused on its own: the
// rest still hold (P10).
//
// A file of tm 0.37 and before (no AccessMarker, sandbox_migrate.go)
// is read as it meant then: there a project's excluded_commands and
// allow, when it set them, replaced all projects'. SandboxOf turns such
// a table into the lists and drops that give the same, until the
// migration writes them.

// SandboxTable is one sandbox table's rules: its lists by key, only
// those with entries (a project's drops under SandboxDrop), and its
// local ports when it sets them. The old preset switches it still has
// read as the rules they unfold into. NoAsk are the table's excluded
// commands that run without asking (ask = false); the others ask first.
type SandboxTable struct {
	Lists      map[string][]string
	LocalPorts *bool
	NoAsk      []string
}

// SetAsk makes the table's excluded command v ask first, or run without
// asking.
func (t *SandboxTable) SetAsk(v string, ask bool) {
	t.NoAsk = slices.DeleteFunc(slices.Clone(t.NoAsk), func(x string) bool { return x == v })
	if !ask {
		t.NoAsk = append(t.NoAsk, v)
	}
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
	if key == SandboxExcluded {
		t.SetAsk(v, true)
	}
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
	out.NoAsk = slices.Clone(t.NoAsk)
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
	if r.ExcludedCommands != nil {
		t.NoAsk = r.ExcludedCommands.withoutAsking()
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
	// Refused is why the entry fails its check; not given.
	Refused string
	// NoAsk: an excluded command that runs without asking, by its
	// table's ask = false or an allow rule of a file written before that
	// covers it; the others ask first.
	NoAsk bool
}

// Given reports whether threads get the rule.
func (r SandboxRule) Given() bool { return !r.Dropped && r.Refused == "" }

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

// WithoutAsking are the excluded commands threads get that run without
// asking; the others they get ask first.
func (t ThreadSandbox) WithoutAsking() []string {
	var out []string
	for _, r := range t.Rules {
		if r.Key == SandboxExcluded && r.Given() && r.NoAsk {
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
		own = c.SandboxOf(slug)
	}
	return MergeSandbox(c.SandboxOf(""), own)
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
		for _, v := range all.Lists[k] {
			out.Rules = append(out.Rules, SandboxRule{Key: k, Value: v, Dropped: slices.Contains(drops, v), Refused: refused(k, v),
				NoAsk: k == SandboxExcluded && slices.Contains(all.NoAsk, v)})
		}
		for _, v := range own.Lists[k] {
			if slices.Contains(all.Lists[k], v) && !slices.Contains(drops, v) {
				continue // all projects give it already
			}
			out.Rules = append(out.Rules, SandboxRule{Key: k, Value: v, Own: true, Refused: refused(k, v),
				NoAsk: k == SandboxExcluded && slices.Contains(own.NoAsk, v)})
		}
	}
	// An allow rule of a file written before that covers an excluded
	// command lets it run without asking, as it did.
	allow := out.Given(SandboxAllow)
	for i := range out.Rules {
		if r := &out.Rules[i]; r.Key == SandboxExcluded && SandboxCovered(r.Value, allow) != "" {
			r.NoAsk = true
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
	r := c.projects[slug].Sandbox
	if c.legacy {
		return unreplaced(c.defaults.Sandbox, r)
	}
	return r.table(true)
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
// as Load's users check it, none twice. The old preset switches go in
// the same write, their rules written as plain ones, and the file gets
// AccessMarker. ErrForm when the file sets a key in a form the line
// editor doesn't change; ErrLegacy when it still waits for its
// migration.
func EditSandbox(table string, fn func(*SandboxTable) error) error {
	if _, _, err := sandboxTableOf(table); err != nil {
		return err
	}
	return edit(func(data []byte) ([]byte, error) {
		raw, err := decodeTables(data)
		if err != nil {
			return nil, err
		}
		if raw.legacy() {
			return nil, ErrLegacy
		}
		r := raw.sandbox(table)
		before := r.table(table != DefaultsTable)
		next := before.Clone()
		if err := fn(&next); err != nil {
			return nil, err
		}
		if data, err = writeSandbox(data, table, r, before, next, CheckNewSandboxEntry); err != nil {
			return nil, err
		}
		return withMarker(data), nil
	})
}

// writeSandbox is data with table's sandbox table, r as the file has it
// and before as read from it, written as next: each entry next adds
// checked by check, none twice; the old switches removed.
func writeSandbox(data []byte, table string, r *rawSandbox, before, next SandboxTable, check func(key, e string) error) ([]byte, error) {
	t, project, err := sandboxTableOf(table)
	if err != nil {
		return nil, err
	}
	if !project && len(next.Lists[SandboxDrop]) > 0 {
		return nil, fmt.Errorf("only a project drops rules (of all projects')")
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
			if err := check(k, e); err != nil {
				return nil, err
			}
		}
		old := r.list(k)
		if k == SandboxExcluded && len(l) > 0 {
			// Its entries keep whether they ask first.
			var was []ExcludedCommand
			if r != nil && r.ExcludedCommands != nil {
				was = *r.ExcludedCommands
			}
			if now := excludedOf(l, next.NoAsk); old == nil || !slices.Equal(was, now) {
				if data, err = Edit(data, t, k, now); err != nil {
					return nil, err
				}
			}
			continue
		}
		switch {
		case len(l) == 0 && old == nil:
		case len(l) == 0:
			if data, err = remove(data, t, k); err != nil {
				return nil, err
			}
		case old == nil || !slices.Equal(*old, l):
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
