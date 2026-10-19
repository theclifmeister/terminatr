package config

import (
	"maps"
	"slices"
	"strings"
)

// The guides migration (T605): a file whose guides name tm 0.38's texts
// (guides = ["browser-testing"]) gets them written out, each a title
// and the body tm 0.38 worded for the table's threads, to read and edit
// as any other guide. A project whose threads were worded otherwise
// (other hosts or local ports than all projects') drops all projects'
// and gets its own, so every project's threads get the briefs they got:
// the check compares them, and every other rule, and refuses (Differs)
// on any difference. A plain string that names no text of tm 0.38 gave
// threads nothing and goes. The server runs it with the access
// migration (MigrateFile), once the file has the access rules.

// needsGuides reports whether a table of raw has a guide without a
// body: a name of tm 0.38, or a plain string it refused.
func (r rawTables) needsGuides() bool {
	tables := append([]rawTable{r.Defaults}, slices.Collect(maps.Values(r.Projects))...)
	return slices.ContainsFunc(tables, func(t rawTable) bool {
		return t.Sandbox != nil && t.Sandbox.Guides != nil && slices.ContainsFunc(*t.Sandbox.Guides, func(g Guide) bool { return g.Body == "" })
	})
}

// MigrateGuides is data with tm 0.38's guide names written out (see the
// top of this file); nil when it has none, or still waits for the
// access migration. An error when data doesn't load, or a table is in a
// form the line editor doesn't change.
func MigrateGuides(data []byte) (*Migration, error) {
	raw, err := decodeTables(data)
	if err != nil || !raw.needsGuides() || raw.legacy() {
		return nil, err
	}
	old, err := parse("config.toml", data)
	if err != nil {
		return nil, err
	}
	m := &Migration{Data: data, Changes: map[string][]string{}, Kept: map[string][]string{}, Widened: map[string][]string{}, Guides: true}
	out := data
	slugs := slices.Sorted(maps.Keys(raw.Projects))
	var allNext SandboxTable
	for _, slug := range append([]string{""}, slugs...) {
		table, r := DefaultsTable, raw.Defaults.Sandbox
		if slug != "" {
			table, r = "projects."+slug, raw.Projects[slug].Sandbox
		}
		file := r.table(slug != "")
		next := m.writeOut(slug, file, old.ThreadSandbox(slug))
		if slug == "" {
			allNext = next
		} else {
			var ok bool
			if next, ok = fitGuides(allNext, next, old.ThreadSandbox(slug).Guides()); !ok {
				m.Differs = slug + ": its guidance can't be written as guides that add to all projects'"
				return m, nil
			}
		}
		m.describe(slug, r, file, next)
		if out, err = writeSandbox(out, table, r, file, next, checkSandboxEntry); err != nil {
			return nil, err
		}
	}
	out = withMarker(out)
	if m.Differs = compareGuides(old, out, slugs); m.Differs == "" {
		m.Data = out
	}
	return m, nil
}

// writeOut is table slug's rules t with its guides without a body gone:
// tm 0.38's names written out as the texts the table's threads (ts) got,
// a plain string tm refused removed, and a project's drop of a name.
func (m *Migration) writeOut(slug string, t SandboxTable, ts ThreadSandbox) SandboxTable {
	next := t.Clone()
	var names []string
	for _, g := range t.Lists[SandboxGuides] {
		if t.Guides[g] != "" {
			continue
		}
		next.Remove(SandboxGuides, g)
		if !slices.Contains(legacyGuideNames, g) {
			m.Changes[slug] = append(m.Changes[slug], "guide name "+g+", which names no text tm has and gave threads nothing, is removed")
			continue
		}
		names = append(names, g)
	}
	for _, d := range t.Lists[SandboxDrop] {
		if slices.Contains(legacyGuideNames, d) {
			next.Remove(SandboxDrop, d)
		}
	}
	if slug != "" {
		// A project's are worded in fitGuides, from what its threads got.
		return next
	}
	given := ts.Given(SandboxGuides)
	names = slices.DeleteFunc(names, func(n string) bool { return !slices.Contains(given, n) })
	for _, g := range LegacyGuides(names, ts.Given(SandboxHosts), ts.LocalPorts) {
		next.SetGuide(g)
	}
	if len(names) > 0 {
		m.Changes[slug] = append(m.Changes[slug], "guidance "+strings.Join(names, " and ")+" is written out as its text, to read and edit in the Access tab")
	}
	return next
}

// fitGuides is a project's own rules own changed so that, with all
// projects' table all, its threads get the guides want, in its order:
// a guide they get and want doesn't removed (the project's) or dropped
// (all projects'), one they lack added as the project's own, dropping
// all projects' of the same title. ok false when it doesn't get there.
func fitGuides(all, own SandboxTable, want []Guide) (SandboxTable, bool) {
	own = own.Clone()
	for range 4 {
		ts := MergeSandbox(all, own)
		got := ts.Guides()
		if slices.Equal(got, want) {
			return own, true
		}
		for _, r := range ts.Rules {
			if r.Key != SandboxGuides || !r.Given() || slices.Contains(want, Guide{r.Value, r.Body}) {
				continue
			}
			if r.Own {
				own.Remove(SandboxGuides, r.Value)
			} else {
				own.Add(SandboxDrop, r.Value)
			}
		}
		for _, g := range want {
			if slices.Contains(got, g) {
				continue
			}
			if all.Has(SandboxGuides, g.Title) {
				own.Add(SandboxDrop, g.Title)
			}
			own.SetGuide(g)
		}
	}
	return own, false
}

// compareGuides is why the new file out gives a project of slugs, or
// all projects, other guidance or other rules than old, "" when none
// does.
func compareGuides(old *Config, out []byte, slugs []string) string {
	nu, err := parse("config.toml", out)
	if err != nil {
		return "the new file doesn't load: " + err.Error()
	}
	for _, slug := range append([]string{""}, slugs...) {
		name := slug
		if slug == "" {
			name = AllProjectsName
		}
		before, after := old.ThreadSandbox(slug), nu.ThreadSandbox(slug)
		if !slices.Equal(before.Guides(), after.Guides()) {
			return name + ": its threads would get other guidance"
		}
		b, a := withoutGuides(modelOf(before, nil)), withoutGuides(modelOf(after, nil))
		if !b.same(a) {
			return name + ": its threads would get other rules (" + modelDiff(b, a) + ")"
		}
	}
	return ""
}

// withoutGuides is m without its guides, which compareGuides compares
// as the brief has them.
func withoutGuides(m accessModel) accessModel {
	maps.DeleteFunc(m.Rules, func(id string, _ bool) bool { return strings.HasPrefix(id, SandboxGuides+" ") })
	return m
}
