package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// The migration of a config.toml of tm 0.37 and before (T574 §6): the
// server runs Migrate once, at its first start of the release, and
// writes the file it gives (server/accessmigrate.go keeps a backup and
// tells the human). It turns
//
//   - the old switches into their rules (a project's false into
//     drops of them, local ports off);
//   - a project's excluded_commands or allow list, which replaced all
//     projects' (an empty one too), into its entries plus drops of those
//     of all projects' it lacked;
//   - a Bash(...) allow rule covering an excluded command into that
//     command's ask = false;
//   - Edit, Write, MultiEdit and NotebookEdit allow rules, and the guard's
//     worktree-only path allowances, into write folders, where
//     CheckWriteFolder takes the folder;
//
// and leaves the rest as they were, listed in Kept for the human: a Bash
// rule that covers no excluded command, a file rule or allowance whose
// path is no write folder. It refuses (Differs) when any project
// without yolo would get other rules from the new file than from the
// old one, read as tm 0.37 read it. The file then marks itself
// (AccessMarker), which no writer of tm 0.37 wrote: a file without it
// whose projects set those lists is still read with tm 0.37's meaning
// (SandboxOf gives the same lists and drops), and EditSandbox refuses
// to change it.

// The marker a migrated file has, before its first table.
const (
	accessMarkerKey   = "sandbox_lists"
	accessMarkerValue = "add"
	accessMarker      = "# tm 0.38 moved this file to its access rules: a project's sandbox lists add to all\n" +
		"# projects', and its drop takes one of theirs back. tm writes this line.\n" +
		accessMarkerKey + " = \"" + accessMarkerValue + "\"\n"
)

// ErrLegacy is a sandbox write refused because config.toml waits for
// its migration.
var ErrLegacy = errors.New("config.toml still has tm 0.37's sandbox lists, where a project's list replaced all projects': the server moves them to the new rules when it starts (tm server start); when it couldn't, its inbox note says why")

// legacyReplaceKeys are the lists whose project list replaced all
// projects' in tm 0.37.
var legacyReplaceKeys = []string{SandboxExcluded, SandboxAllow}

// replaces reports whether a project's table sets one of
// legacyReplaceKeys, an empty list too.
func (r *rawSandbox) replaces() bool {
	return r != nil && slices.ContainsFunc(legacyReplaceKeys, func(k string) bool { return r.list(k) != nil })
}

// unreplaced is a project's table r of a file of tm 0.37 as the lists and
// drops that give its threads the same rules with all projects' table
// all: each all projects' entry of a list r sets that r lacks, or has
// asking otherwise, goes in drop, and an entry of another list a drop of
// the same text would take is kept as the project's own.
func unreplaced(all, r *rawSandbox) SandboxTable {
	a, own := all.table(false), r.table(true)
	var drops []string
	for _, k := range legacyReplaceKeys {
		if r.list(k) == nil {
			continue
		}
		for _, v := range a.Lists[k] {
			if !own.Has(k, v) || k == SandboxExcluded && slices.Contains(a.NoAsk, v) != slices.Contains(own.NoAsk, v) {
				drops = append(drops, v)
			}
		}
	}
	for _, d := range drops {
		if own.Has(SandboxDrop, d) {
			continue
		}
		own.Add(SandboxDrop, d)
		for _, k := range SandboxKeys {
			if !a.Has(k, d) || own.Has(k, d) || slices.Contains(legacyReplaceKeys, k) && r.list(k) != nil {
				continue
			}
			own.Add(k, d) // given before, by all projects
			if k == SandboxExcluded && slices.Contains(a.NoAsk, d) {
				own.SetAsk(d, false)
			}
		}
	}
	return own
}

// rawTable is what Migrate and EditSandbox read of a [defaults] or
// [projects.<slug>] table.
type rawTable struct {
	Sandbox    *rawSandbox   `toml:"sandbox"`
	GuardAllow *[]GuardAllow `toml:"guard_allow"`
}

// rawTables are a file's tables as rawTable.
type rawTables struct {
	SandboxLists string `toml:"sandbox_lists"`
	Defaults     rawTable
	Projects     map[string]rawTable
}

func decodeTables(data []byte) (rawTables, error) {
	var raw rawTables
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return raw, fmt.Errorf("the settings can't be read: %w", err)
	}
	return raw, nil
}

// sandbox is table's sandbox table (DefaultsTable or "projects.<slug>").
func (r rawTables) sandbox(table string) *rawSandbox {
	if table == DefaultsTable {
		return r.Defaults.Sandbox
	}
	return r.Projects[strings.TrimPrefix(table, "projects.")].Sandbox
}

// marked reports whether the file has AccessMarker.
func (r rawTables) marked() bool { return r.SandboxLists == accessMarkerValue }

// legacy reports whether the file is read with tm 0.37's meaning.
func (r rawTables) legacy() bool {
	return !r.marked() && slices.ContainsFunc(slices.Collect(maps.Values(r.Projects)), func(t rawTable) bool { return t.Sandbox.replaces() })
}

// withMarker is data with AccessMarker before its first table.
func withMarker(data []byte) []byte {
	if raw, err := decodeTables(data); err == nil && raw.marked() {
		return data
	}
	out := []byte(accessMarker)
	if data = bytes.TrimLeft(data, "\n"); len(bytes.TrimSpace(data)) > 0 {
		out = append(out, '\n')
	}
	return append(out, data...)
}

// NeedsMigration reports whether Migrate has anything to do in data: a
// file without AccessMarker that has an old switch, a project list of
// tm 0.37's meaning, a file or shell allow rule, or a worktree-only path
// allowance.
func NeedsMigration(data []byte) (bool, error) {
	raw, err := decodeTables(data)
	if err != nil || raw.marked() {
		return false, err
	}
	if raw.legacy() {
		return true, nil
	}
	tables := append([]rawTable{raw.Defaults}, slices.Collect(maps.Values(raw.Projects))...)
	for _, t := range tables {
		if t.Sandbox.hasOldSwitch() {
			return true, nil
		}
		if l := t.Sandbox.list(SandboxAllow); l != nil && slices.ContainsFunc(*l, func(a string) bool { return legacyAllow(a) != "" }) {
			return true, nil
		}
		if t.GuardAllow != nil && slices.ContainsFunc(*t.GuardAllow, func(a GuardAllow) bool { return a.Rule == "worktree-only" && a.Path != "" }) {
			return true, nil
		}
	}
	return false, nil
}

// legacyAllow is the tool of an allow rule the list no longer takes,
// lower case ("bash", "edit", …), or "".
func legacyAllow(a string) string {
	m := allowRule.FindStringSubmatch(a)
	if m == nil {
		return ""
	}
	if t := strings.ToLower(m[1]); slices.Contains(commandTools, t) || slices.Contains(editTools, t) {
		return t
	}
	return ""
}

// editFolder is the write folder file rule target what names (Claude's
// //abs/** or ~/x/**: a folder and all below it; a path without /** may
// be one file), when CheckWriteFolder takes it.
func editFolder(what string) (string, bool) {
	p := what
	switch {
	case strings.HasPrefix(p, "//"):
		p = p[1:]
	case strings.HasPrefix(p, "~/"):
	default:
		return "", false
	}
	p, ok := strings.CutSuffix(p, "/**")
	if !ok || strings.ContainsAny(p, "*?[]{}") || CheckWriteFolder(p) != nil {
		return "", false
	}
	return p, true
}

// guardFolder is the write folder of a worktree-only path allowance
// (~/x/*: the folder and all below it), when CheckWriteFolder takes it.
func guardFolder(a GuardAllow) (string, bool) {
	p, ok := strings.CutSuffix(a.Path, "/*")
	if a.Rule != "worktree-only" || a.Exact || !ok || strings.Contains(p, "*") || CheckWriteFolder(p) != nil {
		return "", false
	}
	return p, true
}

// covers are the excluded commands of excluded that Bash allow rule a
// lets run without a prompt.
func covers(a string, excluded []string) []string {
	var out []string
	for _, e := range excluded {
		if SandboxCovered(e, []string{a}) != "" {
			out = append(out, e)
		}
	}
	return out
}

// accessModel is what a project's threads get from the sandbox lists, as
// the migration compares it: each rule given (a Bash allow rule covering
// an excluded command left out, its work being that command's no-ask; a
// file allow rule or worktree-only allowance with a write folder as that
// write folder), which excluded commands run without asking, and local
// ports. Covering are the Bash allow rules left out, which fit uses
// and the comparison doesn't: those of all projects' that moved are no
// longer given.
type accessModel struct {
	Rules, NoAsk, Covering map[string]bool
	LocalPorts             bool
}

// same reports whether m and o give the same.
func (m accessModel) same(o accessModel) bool {
	return reflect.DeepEqual(m.Rules, o.Rules) && reflect.DeepEqual(m.NoAsk, o.NoAsk) && m.LocalPorts == o.LocalPorts
}

// ruleID is a rule's key in accessModel.Rules.
func ruleID(key, v string) string { return key + " " + v }

// modelRule is rule r of a set whose given excluded commands are
// excluded as accessModel has it, ok false for one it leaves out.
func modelRule(r SandboxRule, excluded []string) (key, v string, ok bool) {
	if r.Key != SandboxAllow {
		return r.Key, r.Value, true
	}
	switch m := allowRule.FindStringSubmatch(r.Value); legacyAllow(r.Value) {
	case "bash":
		return r.Key, r.Value, len(covers(r.Value, excluded)) == 0
	case "edit", "write", "multiedit", "notebookedit":
		if f, ok := editFolder(m[2]); ok {
			return SandboxWrite, f, true
		}
	}
	return r.Key, r.Value, true
}

// modelOf is ts as accessModel, with the guard allowances guard.
func modelOf(ts ThreadSandbox, guard []GuardAllow) accessModel {
	m := accessModel{Rules: map[string]bool{}, NoAsk: map[string]bool{}, Covering: map[string]bool{}, LocalPorts: ts.LocalPorts}
	excluded := ts.Given(SandboxExcluded)
	for _, r := range ts.Rules {
		if !r.Given() {
			continue
		}
		if k, v, ok := modelRule(r, excluded); ok {
			m.Rules[ruleID(k, v)] = true
		} else {
			m.Covering[r.Value] = true
		}
		if r.Key == SandboxExcluded && r.NoAsk {
			m.NoAsk[r.Value] = true
		}
	}
	for _, a := range guard {
		if f, ok := guardFolder(a); ok {
			m.Rules[ruleID(SandboxWrite, f)] = true
		}
	}
	return m
}

// fit is own changed so that, with all projects' table all, its threads
// get want: what they get and want doesn't is removed or dropped, what
// they lack is added as the project's own, each excluded command asking
// as want has it. ok false when it doesn't get there.
func fit(all, own SandboxTable, want accessModel) (SandboxTable, bool) {
	own = own.Clone()
	for range 8 {
		ts := MergeSandbox(all, own)
		got := modelOf(ts, nil)
		if got.same(want) {
			return own, true
		}
		excluded := ts.Given(SandboxExcluded)
		for _, r := range ts.Rules {
			k, v, ok := modelRule(r, excluded)
			switch id := ruleID(k, v); {
			case !r.Given():
			case !ok && want.Covering[r.Value]:
			case !ok && r.Own:
				own.Remove(r.Key, r.Value)
			case !ok:
				own.Add(SandboxDrop, r.Value)
			case !want.Rules[id] && r.Own:
				own.Remove(r.Key, r.Value)
			case !want.Rules[id]:
				own.Add(SandboxDrop, r.Value)
			case r.Key == SandboxExcluded && got.NoAsk[v] != want.NoAsk[v]:
				if !r.Own {
					own.Add(SandboxDrop, v)
				}
				own.Add(SandboxExcluded, v)
				own.SetAsk(v, !want.NoAsk[v])
			}
		}
		for _, id := range slices.Sorted(maps.Keys(want.Rules)) {
			if got.Rules[id] {
				continue
			}
			k, v, _ := strings.Cut(id, " ")
			own.Add(k, v)
			if k == SandboxExcluded {
				own.SetAsk(v, !want.NoAsk[v])
			}
		}
		if got.LocalPorts != want.LocalPorts {
			on := want.LocalPorts
			own.LocalPorts = &on
		}
	}
	return own, false
}

// A Migration is what Migrate made of a file of tm 0.37 or before.
type Migration struct {
	// Data is the new file; the old one when Differs.
	Data []byte
	// Changes are what moved, in words, by table: "" for all projects,
	// else the project's slug. Kept are the rules left as they were, for
	// the human to decide, by table too.
	Changes, Kept map[string][]string
	// Widened are the write folders a file allow rule or a worktree-only
	// allowance became, by table: they reach further than it did (the
	// thread's sandboxed commands and Codex threads write there too),
	// which the check can't see, so the human is asked about each.
	Widened map[string][]string
	// Yolo are the projects with yolo on, whose rules aren't compared:
	// their threads don't use them.
	Yolo []string
	// Differs is why the migration is refused: the project whose threads
	// would get other rules, and how. "" when it holds.
	Differs string
	// Access: the file moved to the access rules (Migrate); Guides: its
	// guide names were written out (MigrateGuides).
	Access, Guides bool
}

// join is m, which moved the file to the access rules, followed by g,
// which wrote out its guides; either may be nil.
func join(m, g *Migration) *Migration {
	switch {
	case m == nil:
		return g
	case g == nil:
		return m
	}
	for _, x := range []struct{ to, from map[string][]string }{{m.Changes, g.Changes}, {m.Kept, g.Kept}, {m.Widened, g.Widened}} {
		for k, v := range x.from {
			x.to[k] = append(x.to[k], v...)
		}
	}
	m.Data, m.Differs, m.Guides = g.Data, g.Differs, true
	return m
}

// orEmpty is l, or an empty list for nil.
func orEmpty(l *[]string) *[]string {
	if l == nil {
		return &[]string{}
	}
	return l
}

// KindName is how messages name an entry of a sandbox list.
func KindName(key string) string {
	switch key {
	case SandboxHosts:
		return "host"
	case SandboxGuides:
		return "guide"
	case SandboxWrite:
		return "write folder"
	case SandboxSockets:
		return "socket"
	case SandboxApproved:
		return "approved command"
	case SandboxExcluded:
		return "excluded command"
	case SandboxAllow:
		return "allow rule"
	case SandboxDrop:
		return "drop"
	case SandboxLocalPorts:
		return "local ports"
	}
	return key
}

// Migrate is data, a config.toml of tm 0.37 or before, moved to the
// access rules (see the top of this file); nil when NeedsMigration says
// there is nothing to move. An error when data doesn't load, or a table
// is in a form the line editor doesn't change.
func Migrate(data []byte) (*Migration, error) {
	if need, err := NeedsMigration(data); err != nil || !need {
		return nil, err
	}
	raw, err := decodeTables(data)
	if err != nil {
		return nil, err
	}
	old, err := parse("config.toml", data)
	if err != nil {
		return nil, err
	}
	m := &Migration{Data: data, Changes: map[string][]string{}, Kept: map[string][]string{}, Widened: map[string][]string{}, Access: true}
	out := data
	slugs := slices.Sorted(maps.Keys(raw.Projects))
	var allNext SandboxTable
	for _, slug := range append([]string{""}, slugs...) {
		table, t := DefaultsTable, raw.Defaults
		if slug != "" {
			table, t = "projects."+slug, raw.Projects[slug]
		}
		file := t.Sandbox.table(slug != "")
		next := old.SandboxOf(slug)
		given := old.ThreadSandbox(slug)
		var guard []GuardAllow
		if t.GuardAllow != nil {
			guard = *t.GuardAllow
		}
		next, guard = m.moveRules(slug, next, guard, given.Given(SandboxExcluded))
		if slug == "" {
			allNext = next
		} else {
			s, _ := old.Safety(slug)
			var ok bool
			if next, ok = fit(allNext, next, modelOf(given, s.GuardAllow)); !ok {
				m.Differs = slug + ": its rules can't be written as lists that add to all projects'"
				return m, nil
			}
			for _, a := range next.Lists[SandboxAllow] {
				if !file.Has(SandboxAllow, a) && legacyAllow(a) != "" {
					m.Kept[slug] = append(m.Kept[slug], "allow rule "+a+" of all projects', which runs commands without a prompt, is now "+slug+"'s own, as its threads still got it")
				}
			}
			// A project's old switch off drops its rules: those all
			// projects don't give drop nothing.
			for _, d := range next.Lists[SandboxDrop] {
				if !slices.Contains(*orEmpty(t.Sandbox.list(SandboxDrop)), d) && !slices.ContainsFunc(SandboxKeys, func(k string) bool { return allNext.Has(k, d) }) {
					next.Remove(SandboxDrop, d)
				}
			}
		}
		m.describe(slug, t.Sandbox, file, next)
		if out, err = writeSandbox(out, table, t.Sandbox, file, next, checkSandboxEntry); err != nil {
			return nil, fmt.Errorf("[%s.sandbox]: %w", table, err)
		}
		if t.GuardAllow != nil && len(guard) != len(*t.GuardAllow) {
			if len(guard) == 0 {
				out, err = remove(out, table, "guard_allow")
			} else {
				out, err = Edit(out, table, "guard_allow", guard)
			}
			if err != nil {
				return nil, fmt.Errorf("[%s] guard_allow: %w", table, err)
			}
		}
	}
	out = withMarker(out)
	if m.Differs = compareMigrated(old, out, slugs, &m.Yolo); m.Differs == "" {
		m.Data = out
	}
	return m, nil
}

// moveRules moves table slug's rules that have a new form: a Bash allow
// rule covering one of excluded (the commands its threads get) goes,
// each of the table's own it covers then running without asking; file
// allow rules and worktree-only path allowances with a write folder
// become that write folder. What stays is listed in Kept.
func (m *Migration) moveRules(slug string, t SandboxTable, guard []GuardAllow, excluded []string) (SandboxTable, []GuardAllow) {
	next := t.Clone()
	for _, a := range t.Lists[SandboxAllow] {
		switch tool, mm := legacyAllow(a), allowRule.FindStringSubmatch(a); tool {
		case "":
		case "bash", "powershell":
			c := covers(a, excluded)
			if tool == "powershell" || len(c) == 0 {
				m.Kept[slug] = append(m.Kept[slug], "allow rule "+a+" runs commands without a prompt and covers no excluded command: kept as it is; an excluded command that runs without asking does this now, or none is needed inside the sandbox")
				continue
			}
			next.Remove(SandboxAllow, a)
			for _, e := range c {
				if next.Has(SandboxExcluded, e) {
					next.SetAsk(e, false)
				}
			}
		default:
			f, ok := editFolder(mm[2])
			if !ok {
				m.Kept[slug] = append(m.Kept[slug], "allow rule "+a+" names no folder tm can make a write folder: kept as it is")
				continue
			}
			next.Remove(SandboxAllow, a)
			next.Add(SandboxWrite, f)
			m.widen(slug, f)
			m.Changes[slug] = append(m.Changes[slug], "allow rule "+a+" is now write folder "+f+", which Codex threads, sandboxed commands and the guard take too")
		}
	}
	var keep []GuardAllow
	for _, a := range guard {
		f, ok := guardFolder(a)
		switch {
		case ok:
			next.Add(SandboxWrite, f)
			m.widen(slug, f)
			m.Changes[slug] = append(m.Changes[slug], "guard allowance worktree-only "+a.Path+" is now write folder "+f+", which sandboxed commands and Codex threads take too")
			continue
		case a.Rule == "worktree-only" && a.Path != "":
			m.Kept[slug] = append(m.Kept[slug], "guard allowance worktree-only "+a.Path+" names no folder tm can make a write folder: kept as it is")
		}
		keep = append(keep, a)
	}
	return next, keep
}

// widen adds write folder f to table slug's Widened.
func (m *Migration) widen(slug, f string) {
	if !slices.Contains(m.Widened[slug], f) {
		m.Widened[slug] = append(m.Widened[slug], f)
	}
}

// describe adds to Changes what writing table slug (r as the file has
// it, file as read from it) as next changes, in words.
func (m *Migration) describe(slug string, r *rawSandbox, file, next SandboxTable) {
	for _, s := range oldSwitches {
		if on := r.oldSwitch(s.key); on != nil {
			m.Changes[slug] = append(m.Changes[slug], fmt.Sprintf("the old switch %s = %v is written as its rules", s.key, *on))
		}
	}
	for _, k := range append(slices.Clone(SandboxKeys), SandboxDrop) {
		for _, v := range next.Lists[k] {
			if !file.Has(k, v) {
				m.Changes[slug] = append(m.Changes[slug], "added "+KindName(k)+" "+v)
			}
		}
		for _, v := range file.Lists[k] {
			if !next.Has(k, v) {
				m.Changes[slug] = append(m.Changes[slug], "removed "+KindName(k)+" "+v)
			}
		}
	}
	for _, v := range next.NoAsk {
		if !slices.Contains(file.NoAsk, v) {
			m.Changes[slug] = append(m.Changes[slug], "excluded command "+v+" runs without asking (ask = false)")
		}
	}
	for _, v := range file.NoAsk {
		if next.Has(SandboxExcluded, v) && !slices.Contains(next.NoAsk, v) {
			m.Changes[slug] = append(m.Changes[slug], "excluded command "+v+" asks first")
		}
	}
	if slug != "" && r.replaces() {
		m.Changes[slug] = append(m.Changes[slug], "its excluded commands and allow rules now add to all projects': the drops above keep out those its own list left out")
	}
	if !reflect.DeepEqual(file.LocalPorts, next.LocalPorts) && next.LocalPorts != nil {
		m.Changes[slug] = append(m.Changes[slug], fmt.Sprintf("local_ports = %v", *next.LocalPorts))
	}
}

// compareMigrated is why the new file out gives a project of slugs, or
// all projects, other rules or settings than old, "" when none does;
// the projects with yolo on go in yolo, their rules not compared.
func compareMigrated(old *Config, out []byte, slugs []string, yolo *[]string) string {
	nu, err := parse("config.toml", out)
	if err != nil {
		return "the new file doesn't load: " + err.Error()
	}
	for _, slug := range append([]string{""}, slugs...) {
		name := slug
		if slug == "" {
			name = AllProjectsName
		}
		s0, err0 := old.Safety(slug)
		s1, err1 := nu.Safety(slug)
		if slug == "" {
			s0, err0 = old.AllProjects()
			s1, err1 = nu.AllProjects()
		}
		if (err0 == nil) != (err1 == nil) {
			return name + ": its settings no longer load"
		}
		guard0 := s0.GuardAllow
		s0.GuardAllow = slices.DeleteFunc(slices.Clone(s0.GuardAllow), func(a GuardAllow) bool { _, ok := guardFolder(a); return ok })
		if len(s0.GuardAllow) == 0 && len(s1.GuardAllow) == 0 {
			s0.GuardAllow, s1.GuardAllow = nil, nil
		}
		if !reflect.DeepEqual(s0, s1) {
			return name + ": its settings would change"
		}
		if s0.Yolo && slug != "" {
			*yolo = append(*yolo, slug)
			continue
		}
		before, after := modelOf(old.ThreadSandbox(slug), guard0), modelOf(nu.ThreadSandbox(slug), s1.GuardAllow)
		if !before.same(after) {
			return name + ": its threads would get other rules (" + modelDiff(before, after) + ")"
		}
		// A Bash rule covering an excluded command counts as its no-ask
		// in both; the new file may have fewer of them (moved), never one
		// the old didn't.
		for _, a := range slices.Sorted(maps.Keys(after.Covering)) {
			if !before.Covering[a] {
				return name + ": its threads would get the allow rule " + a + ", which they didn't"
			}
		}
	}
	return ""
}

// modelDiff says how after differs from before.
func modelDiff(before, after accessModel) string {
	var out []string
	for _, id := range slices.Sorted(maps.Keys(after.Rules)) {
		if !before.Rules[id] {
			out = append(out, "would get "+id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(before.Rules)) {
		if !after.Rules[id] {
			out = append(out, "would lose "+id)
		}
	}
	for _, v := range slices.Sorted(maps.Keys(after.NoAsk)) {
		if !before.NoAsk[v] {
			out = append(out, v+" would run without asking")
		}
	}
	for _, v := range slices.Sorted(maps.Keys(before.NoAsk)) {
		if !after.NoAsk[v] {
			out = append(out, v+" would ask first")
		}
	}
	if before.LocalPorts != after.LocalPorts {
		out = append(out, fmt.Sprintf("local ports %v", after.LocalPorts))
	}
	return strings.Join(out, "; ")
}

// OldAccess lists, for tm doctor, what of the file still has a form of
// tm 0.37: its sandbox lists waiting for the migration, the old
// switches, allow rules that run commands or write files, worktree-only
// path allowances; and tm 0.38's guide names.
func (c *Config) OldAccess() []string {
	if c == nil {
		return nil
	}
	var out []string
	if c.legacy {
		out = append(out, "a project's excluded_commands or allow still replaces all projects' (tm 0.37's meaning): the server migrates the file when it starts")
	}
	tables := map[string]rawSafety{AllProjectsName: c.defaults}
	maps.Copy(tables, c.projects)
	for _, name := range slices.Sorted(maps.Keys(tables)) {
		t := tables[name]
		where := name
		if name != AllProjectsName {
			where = "project " + name
		}
		for _, k := range OldSwitchKeys() {
			if t.Sandbox.oldSwitch(k) != nil {
				out = append(out, where+": the old switch "+k+" (read as its rules; any sandbox change writes them)")
			}
		}
		if l := t.Sandbox.list(SandboxAllow); l != nil {
			for _, a := range *l {
				if legacyAllow(a) != "" {
					out = append(out, where+": allow rule "+a+" runs commands or writes files, which an excluded command or a write folder does now")
				}
			}
		}
		if t.Sandbox != nil && t.Sandbox.Guides != nil {
			for _, g := range *t.Sandbox.Guides {
				if g.Body == "" {
					out = append(out, where+": guide name "+g.Title+" of tm 0.38 (the server writes it out as its text when it starts)")
				}
			}
		}
		if t.GuardAllow != nil {
			for _, a := range *t.GuardAllow {
				if a.Rule == "worktree-only" && a.Path != "" {
					out = append(out, where+": guard allowance worktree-only "+a.Path+", which a write folder does now")
				}
			}
		}
	}
	return out
}

// MigrateFile migrates config.toml in place, under its lock: Migrate,
// then MigrateGuides of what it gives; first a copy of it beside it,
// config.toml.bak-<release> (another name when that is taken), named in
// the result. Nothing is written when there is nothing to move or a
// check refuses (Differs).
func MigrateFile(release string) (m *Migration, backup string, err error) {
	path, err := Path()
	if err != nil {
		return nil, "", err
	}
	err = edit(func(data []byte) ([]byte, error) {
		if m, err = Migrate(data); err != nil || m != nil && m.Differs != "" {
			return data, err
		}
		next := data
		if m != nil {
			next = m.Data
		}
		g, err := MigrateGuides(next)
		if err != nil {
			return data, err
		}
		if m = join(m, g); m == nil || m.Differs != "" {
			return data, nil
		}
		backup = path + ".bak-" + release
		// A new file, never one there (or a link planted there).
		f, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		for i := 2; errors.Is(err, fs.ErrExist) && i < 100; i++ {
			backup = fmt.Sprintf("%s.bak-%s-%d", path, release, i)
			f, err = os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		}
		if err != nil {
			return nil, fmt.Errorf("the backup: %w", err)
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, fmt.Errorf("the backup: %w", err)
		}
		return m.Data, nil
	})
	if err != nil {
		return nil, "", err
	}
	return m, backup, nil
}
