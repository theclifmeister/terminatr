package config

import (
	"fmt"
	"maps"
	"slices"
)

// One rule of a sandbox table changed in place (T619): its value, and
// with it its options (an excluded command's ask, a guide's text), in
// one write, checked as an entry added is. A project's drops follow all
// projects' rule they name, so a project that dropped it drops it under
// its new value too.

// SandboxEdit changes the entry Old of Key's list into New, where it is
// in the list. Ask, when set, is whether an excluded command asks
// first, Body a guide's text; nil keeps the old entry's.
type SandboxEdit struct {
	Key, Old, New string
	Ask           *bool
	Body          *string
}

// Changed reports whether e changes anything of entry r, the entry Old
// as the list has it.
func (e SandboxEdit) Changed(r SandboxRule) bool {
	return e.New != e.Old || e.Ask != nil && *e.Ask == r.NoAsk || e.Body != nil && GuideBody(*e.Body) != r.Body
}

// Apply changes table t by e: the entry Old into New, its ask and its
// guide's text with it. An error when t lacks Old or has New already.
func (e SandboxEdit) Apply(t *SandboxTable) error {
	if !t.Has(e.Key, e.Old) {
		return fmt.Errorf("%s isn't listed", e.Old)
	}
	if e.New != e.Old && t.Has(e.Key, e.New) {
		return fmt.Errorf("%s is listed already", e.New)
	}
	noAsk, body := slices.Contains(t.NoAsk, e.Old), t.Guides[e.Old]
	l := slices.Clone(t.Lists[e.Key])
	l[slices.Index(l, e.Old)] = e.New
	t.Lists[e.Key] = l
	switch e.Key {
	case SandboxExcluded:
		if e.Ask != nil {
			noAsk = !*e.Ask
		}
		t.SetAsk(e.Old, true)
		t.SetAsk(e.New, !noAsk)
	case SandboxGuides:
		if e.Body != nil {
			body = GuideBody(*e.Body)
		}
		delete(t.Guides, e.Old)
		t.SetGuide(Guide{e.New, body})
	}
	return nil
}

// MoveDrop makes a project's table own, which may drop all projects'
// Old, drop it under its new value once all projects' table is all (e
// applied): in Old's place, or beside it while another of all projects'
// lists keeps Old. It reports whether own changed.
func (e SandboxEdit) MoveDrop(own *SandboxTable, all SandboxTable) bool {
	if e.New == e.Old || !own.Has(SandboxDrop, e.Old) {
		return false
	}
	kept := slices.ContainsFunc(SandboxKeys, func(k string) bool { return all.Has(k, e.Old) })
	switch {
	case own.Has(SandboxDrop, e.New) && kept:
		return false
	case own.Has(SandboxDrop, e.New):
		own.Remove(SandboxDrop, e.Old)
	case kept:
		own.Add(SandboxDrop, e.New)
	default:
		l := slices.Clone(own.Lists[SandboxDrop])
		l[slices.Index(l, e.Old)] = e.New
		own.Lists[SandboxDrop] = l
	}
	return true
}

// EditSandboxRule changes one entry of table's sandbox table (DefaultsTable
// or "projects.<slug>") by e in one write, checked as EditSandbox checks
// an entry added; a project's New that all projects give already is
// refused, as adding it is. On all projects' table, every project that
// dropped Old drops New in the same write: moved are their slugs.
func EditSandboxRule(table string, e SandboxEdit) (moved []string, err error) {
	if _, _, err := sandboxTableOf(table); err != nil {
		return nil, err
	}
	err = edit(func(data []byte) ([]byte, error) {
		moved = nil
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
		if err := e.Apply(&next); err != nil {
			return nil, err
		}
		if all := raw.Defaults.Sandbox.table(false); table != DefaultsTable && e.New != e.Old && all.Has(e.Key, e.New) {
			if before.Has(SandboxDrop, e.New) {
				return nil, fmt.Errorf("%s is all projects', dropped here: take the drop back instead", e.New)
			}
			return nil, fmt.Errorf("all projects give %s already", e.New)
		}
		if data, err = writeSandbox(data, table, r, before, next, CheckNewSandboxEntry); err != nil {
			return nil, err
		}
		if table == DefaultsTable {
			for _, slug := range slices.Sorted(maps.Keys(raw.Projects)) {
				pr := raw.Projects[slug].Sandbox
				pb := pr.table(true)
				pn := pb.Clone()
				if !e.MoveDrop(&pn, next) {
					continue
				}
				if data, err = writeSandbox(data, "projects."+slug, pr, pb, pn, CheckNewSandboxEntry); err != nil {
					return nil, err
				}
				moved = append(moved, slug)
			}
		}
		return withMarker(data), nil
	})
	return moved, err
}
