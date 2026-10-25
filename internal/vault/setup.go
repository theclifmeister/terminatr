package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
)

// tm vault init and off (T720, docs/SPEC.md §5.1, Vault). The projects
// folder is the vault; init gives it what Obsidian needs to read it the
// way tm writes it, and turns the derived notes on:
//
//   - .obsidian/: tm's keys merged into app.json (ignore filters, link
//     form, new notes in _human), core-plugins.json (Bases, Canvas,
//     graph, backlinks, properties, … on; Sync, Publish and daily notes
//     as the user has them), graph.json (colour groups, default filter),
//     types.json (property types), appearance.json (the tm snippet on),
//     and snippets/tm.css whole;
//   - _tm/: the Bases (bases/*.base) and Home.md, rendered again by tm
//     vault render as projects come and go;
//   - _human/, the folder Obsidian puts new notes in, which tm never
//     writes;
//   - the mark state/vault (On), after which the ticker renders the
//     derived notes; init renders them all once.
//
// .obsidian/tm.json keeps what tm wrote: each key's value, each list
// entry tm added, each file's hash. A later init updates what is still
// as tm left it and keeps what the user changed since, listing it.
// Off removes the mark, the derived notes and _tm/; .obsidian/ and
// _human/ stay (they're the user's app and notes), and so do YAML and
// links, which are the format, not the vault.

// Folders of the vault that can't be a project's slug (project.ValidSlug).
const (
	obsidianDir = ".obsidian"
	tmDir       = "_tm"
	humanDir    = "_human"
)

// Change is one file init or render writes, or keeps because the user
// changed it since tm wrote it.
type Change struct {
	Rel  string // its path in the vault, with forward slashes
	What string // "new", "updated", or the keys merged ("alwaysUpdateLinks, …")
	// Kept is why tm leaves it as it is (the keys or the file the user
	// changed); the file may still get other keys.
	Kept string
	data []byte
}

// Plan is what init would do, from the vault as it is: Changes lists
// every file it writes or keeps, Reports the reports stored before the
// vault guard that it guards. Nothing is written until Init.
type Plan struct {
	Root    string
	Changes []Change
	Reports []File
	state   tmState
}

// tmState is .obsidian/tm.json.
type tmState struct {
	// Keys are the values tm last set, per settings file and key.
	Keys map[string]map[string]any `json:"keys"`
	// Entries are the entries tm added to a list, per settings file and
	// key (userIgnoreFilters, enabledCssSnippets, colorGroups' queries).
	Entries map[string]map[string][]string `json:"entries"`
	// Files are the hashes of the files tm wrote whole.
	Files map[string]string `json:"files"`
}

func readState(root string) tmState {
	var st tmState
	if b, err := os.ReadFile(filepath.Join(root, obsidianDir, "tm.json")); err == nil {
		json.Unmarshal(b, &st)
	}
	if st.Keys == nil {
		st.Keys = map[string]map[string]any{}
	}
	if st.Entries == nil {
		st.Entries = map[string]map[string][]string{}
	}
	if st.Files == nil {
		st.Files = map[string]string{}
	}
	return st
}

// IgnoreFilters are app.json's userIgnoreFilters tm adds: what stays
// openable but out of search, the graph and the quick switcher.
var IgnoreFilters = []string{"*/inbox/done/", "*/threads/archive/", "*/journal/", "*/AGENTS.md", "*/CLAUDE.md", "*/JOURNAL.md", "*/review2/"}

// Ignored reports whether rel, a path in the vault (a folder's ending
// in "/"), is one IgnoreFilters hides: a log or an archive, not a note
// the graph or the link check looks at.
func Ignored(rel string) bool {
	_, rest, ok := strings.Cut(rel, "/")
	if !ok {
		return false
	}
	for _, f := range IgnoreFilters {
		f = strings.TrimPrefix(f, "*/")
		if strings.HasSuffix(f, "/") && strings.HasPrefix(rest, f) || rest == f {
			return true
		}
	}
	return false
}

// appKeys are app.json's keys tm sets: links as tm writes them, new
// notes in _human, and links never rewritten behind tm's back (a rename
// in Obsidian would otherwise edit tm's files).
var appKeys = map[string]any{
	"alwaysUpdateLinks":    false,
	"newFileLocation":      "folder",
	"newFileFolderPath":    humanDir,
	"showUnsupportedFiles": false,
	"useMarkdownLinks":     false,
	"newLinkFormat":        "absolute",
}

// corePlugins are the core plugins tm turns on; Sync, Publish and daily
// notes stay as the user has them.
var corePlugins = []string{"bases", "canvas", "graph", "backlink", "outgoing-link", "properties", "tag-pane", "file-explorer", "global-search"}

// coreDefaults are Obsidian's own defaults, which a new core-plugins.json
// also gets (a plugin it doesn't name is off); never Sync or Publish.
var coreDefaults = []string{"switcher", "page-preview", "command-palette", "editor-status", "outline", "word-count", "file-recovery", "bookmarks", "note-composer", "templates", "daily-notes"}

// propertyTypes are types.json's types for tm's properties, so Bases
// sort and filter them as what they are.
var propertyTypes = map[string]any{
	"created": "datetime", "resolved": "datetime", "updated": "date",
	"steps": "number", "steps_done": "number", "reports": "number", "turns": "number", "cost_usd": "number", "bytes": "number",
	"archived": "checkbox", "standing": "checkbox", "needs_user": "checkbox", "out": "checkbox",
	"touches": "multitext", "after": "multitext",
}

// colorGroup is one of graph.json's colour groups.
type colorGroup struct {
	Query string
	RGB   int
}

// GraphGroups are the graph's colour groups tm adds: tasks blue, threads
// green, memory amber, days grey, what needs you red, messages rose.
var GraphGroups = []colorGroup{
	{"tag:#needs-you", 0xe93147},
	{"tag:#task", 0x086ddd},
	{"tag:#thread", 0x08b94e},
	{"path:/memory/", 0xe0ac00},
	{"tag:#day", 0x8a8a8a},
	{"tag:#from-link OR tag:#from-peer", 0xd53984},
}

// GraphFilter is the graph's default filter.
const GraphFilter = "-path:inbox/done -path:threads/archive -file:TASKS -file:JOURNAL -path:_tm/bases"

// NewPlan reads the vault and plans init: the settings to merge, the
// files to write, the reports to guard.
func NewPlan() (*Plan, error) {
	root, err := home.ProjectsDir()
	if err != nil {
		return nil, err
	}
	pl := &Plan{Root: root, state: readState(root)}
	settings := []struct {
		name  string
		keys  map[string]any
		lists map[string][]any
		fresh map[string]any // a new file's other keys
	}{
		{"app.json", appKeys, map[string][]any{"userIgnoreFilters": anys(IgnoreFilters)}, nil},
		{"core-plugins.json", onKeys(corePlugins), nil, onKeys(coreDefaults)},
		{"graph.json", map[string]any{"search": GraphFilter}, map[string][]any{"colorGroups": graphGroups()}, nil},
		{"types.json", nil, nil, nil},
		{"appearance.json", nil, map[string][]any{"enabledCssSnippets": {"tm"}}, nil},
	}
	for _, s := range settings {
		keys := s.keys
		var nested string
		if s.name == "types.json" {
			keys, nested = propertyTypes, "types"
		}
		c, err := pl.merge(s.name, keys, nested, s.lists, s.fresh)
		if err != nil {
			return nil, err
		}
		if c != nil {
			pl.Changes = append(pl.Changes, *c)
		}
	}
	for _, f := range ownedFiles() {
		if c := pl.whole(f.Rel, f.data); c != nil {
			pl.Changes = append(pl.Changes, *c)
		}
	}
	pl.Reports, err = Unguarded()
	return pl, err
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func onKeys(names []string) map[string]any {
	m := map[string]any{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func graphGroups() []any {
	var out []any
	for _, g := range GraphGroups {
		out = append(out, map[string]any{"query": g.Query, "color": map[string]any{"a": 1, "rgb": g.RGB}})
	}
	return out
}

// entryKey is how a list entry is known: a string as itself, a colour
// group by its query.
func entryKey(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case map[string]any:
		if q, ok := v["query"].(string); ok {
			return q
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// same compares JSON values as they read back.
func same(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	var va, vb any
	json.Unmarshal(ja, &va)
	json.Unmarshal(jb, &vb)
	return reflect.DeepEqual(va, vb)
}

// merge plans settings file name in .obsidian: each of tm's keys set
// (under nested, when not ""), unless the user changed it since tm set
// it; each of tm's list entries added unless the user removed it since,
// and the entries tm added that it no longer has removed. fresh are the
// other keys of a file that isn't there yet. A file that isn't JSON is
// an error: Obsidian couldn't read it either.
func (pl *Plan) merge(name string, keys map[string]any, nested string, lists map[string][]any, fresh map[string]any) (*Change, error) {
	rel := obsidianDir + "/" + name
	path := filepath.Join(pl.Root, filepath.FromSlash(rel))
	cur := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		for k, v := range fresh {
			cur[k] = v
		}
	case err != nil:
		return nil, err
	case len(bytes.TrimSpace(data)) > 0:
		if err := json.Unmarshal(data, &cur); err != nil || cur == nil {
			return nil, fmt.Errorf("%s: not a JSON object, which Obsidian can't read either: fix or remove it", rel)
		}
	}
	into := cur
	if nested != "" {
		m, ok := cur[nested].(map[string]any)
		if !ok {
			m = map[string]any{}
		}
		cur[nested] = m
		into = m
	}
	prev := pl.state.Keys[name]
	set := map[string]any{}
	var changed, kept []string
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		v := keys[k]
		old, had := into[k]
		last, wrote := prev[k]
		switch {
		case had && same(old, v):
		case had && wrote && !same(old, last):
			kept = append(kept, k)
			continue
		default:
			into[k] = v
			changed = append(changed, k)
		}
		set[k] = v
	}
	entries := map[string][]string{}
	for _, k := range slices.Sorted(maps.Keys(lists)) {
		list, _ := cur[k].([]any)
		have := map[string]bool{}
		for _, e := range list {
			have[entryKey(e)] = true
		}
		mine := map[string]bool{}
		for _, e := range lists[k] {
			mine[entryKey(e)] = true
		}
		before := pl.state.Entries[name][k]
		// Entries tm added before and no longer has go.
		n := len(list)
		list = slices.DeleteFunc(list, func(e any) bool {
			key := entryKey(e)
			return slices.Contains(before, key) && !mine[key]
		})
		edited := len(list) != n
		var removedByUser []string
		for _, e := range lists[k] {
			key := entryKey(e)
			switch {
			case have[key]:
			case slices.Contains(before, key):
				removedByUser = append(removedByUser, key)
				continue
			default:
				list = append(list, e)
				edited = true
			}
			entries[k] = append(entries[k], key)
		}
		if edited {
			changed = append(changed, k)
		}
		if len(removedByUser) > 0 {
			kept = append(kept, k+" without "+strings.Join(removedByUser, ", "))
		}
		if list == nil {
			list = []any{}
		}
		cur[k] = list
	}
	pl.state.Keys[name], pl.state.Entries[name] = set, entries
	if len(set) == 0 {
		delete(pl.state.Keys, name)
	}
	if len(entries) == 0 {
		delete(pl.state.Entries, name)
	}
	if len(changed) == 0 && len(kept) == 0 && data != nil {
		return nil, nil
	}
	out, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return nil, err
	}
	c := &Change{Rel: rel, data: append(out, '\n')}
	switch {
	case data == nil:
		c.What = "new"
	case len(changed) > 0:
		c.What = strings.Join(changed, ", ")
	}
	if nested != "" && len(changed) > 0 && data != nil {
		c.What = nested + ": " + c.What
	}
	if len(changed) == 0 {
		c.data = nil // nothing to write: only kept
	}
	if len(kept) > 0 {
		c.Kept = "your " + strings.Join(kept, ", ")
	}
	return c, nil
}

// whole plans a file tm writes whole: written when missing or as tm
// left it, kept when the user changed it since.
func (pl *Plan) whole(rel string, data []byte) *Change {
	path := filepath.Join(pl.Root, filepath.FromSlash(rel))
	old, err := os.ReadFile(path)
	h := hash(data)
	switch {
	case err != nil:
		pl.state.Files[rel] = h
		return &Change{Rel: rel, What: "new", data: data}
	case bytes.Equal(old, data):
		pl.state.Files[rel] = h
		return nil
	case pl.state.Files[rel] == hash(old):
		pl.state.Files[rel] = h
		return &Change{Rel: rel, What: "updated", data: data}
	}
	return &Change{Rel: rel, Kept: "your edit (remove the file to get tm's)"}
}

// Init does what pl planned: guards the reports (originals packed into
// state/migrate/report-guard-<time>.tar.gz first), writes the settings
// and files, makes _human/, writes .obsidian/tm.json and the mark, and
// renders every project's derived notes. Each project is journaled
// (vault.init). It returns the derived notes written.
func Init(pl *Plan, who caller.Caller, now time.Time) (written int, err error) {
	var errs []error
	if len(pl.Reports) > 0 {
		if err := guardReports(pl.Reports, who, now); err != nil {
			return 0, err
		}
	}
	for _, c := range pl.Changes {
		if c.data == nil {
			continue
		}
		path := filepath.Join(pl.Root, filepath.FromSlash(c.Rel))
		if err := fsx.MkdirNoLink(filepath.Dir(path), 0o755); err != nil {
			return 0, err
		}
		if err := fsx.WriteAtomic(path, c.data, 0o644); err != nil {
			return 0, err
		}
	}
	if err := fsx.MkdirNoLink(filepath.Join(pl.Root, humanDir), 0o755); err != nil {
		return 0, err
	}
	st, err := json.MarshalIndent(&pl.state, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := fsx.WriteAtomic(filepath.Join(pl.Root, obsidianDir, "tm.json"), append(st, '\n'), 0o644); err != nil {
		return 0, err
	}
	mark, err := onPath()
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		return 0, err
	}
	if err := fsx.WriteAtomic(mark, []byte("vault on "+now.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return 0, err
	}
	for _, p := range projects() {
		if On() {
			n, _, err := Render(p)
			written += n
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", p.Slug, err))
			}
		}
		if err := p.Journal(who, "vault.init", p.Slug, "derived notes on, .obsidian and _tm written"); err != nil {
			errs = append(errs, err)
		}
	}
	return written, errors.Join(errs...)
}

// projects opens every project that reads.
func projects() []*project.Project {
	list, _ := project.List()
	var out []*project.Project
	for _, s := range list {
		if s.Error != "" {
			continue
		}
		if p, err := project.Open(s.Slug); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// RenderShared writes the vault-wide files tm renders (_tm/bases, Home.md)
// that changed since, as init does, keeping those the user edited, and
// returns what it wrote or kept.
func RenderShared() ([]Change, error) {
	root, err := home.ProjectsDir()
	if err != nil {
		return nil, err
	}
	pl := &Plan{Root: root, state: readState(root)}
	for _, f := range ownedFiles() {
		if c := pl.whole(f.Rel, f.data); c != nil {
			pl.Changes = append(pl.Changes, *c)
		}
	}
	for _, c := range pl.Changes {
		if c.data == nil {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(c.Rel))
		if err := fsx.MkdirNoLink(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := fsx.WriteAtomic(path, c.data, 0o644); err != nil {
			return nil, err
		}
	}
	if len(pl.Changes) > 0 {
		st, err := json.MarshalIndent(&pl.state, "", "  ")
		if err == nil {
			err = fsx.WriteAtomic(filepath.Join(root, obsidianDir, "tm.json"), append(st, '\n'), 0o644)
		}
		if err != nil {
			return nil, err
		}
	}
	return pl.Changes, nil
}

// Off turns the derived notes off: removes the mark first (the ticker
// stops), then, under the ledger's lock, every derived note the ledger
// has, the ledger and _tm/. .obsidian/, _human/, YAML and links stay.
// Each project is journaled (vault.off). It returns the notes removed.
func Off(who caller.Caller) (removed int, err error) {
	mark, err := onPath()
	if err != nil {
		return 0, err
	}
	if err := os.Remove(mark); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	root, err := home.ProjectsDir()
	if err != nil {
		return 0, err
	}
	file, err := ledgerPath()
	if err != nil {
		return 0, err
	}
	var errs []error
	if _, err := os.Lstat(file); err == nil {
		unlock, err := mdfile.Lock(file)
		if err != nil {
			return 0, err
		}
		l := readLedger(file)
		for _, key := range slices.Sorted(maps.Keys(l.Files)) {
			slug, rel, ok := strings.Cut(key, "/")
			if !ok || !project.ValidSlug(slug) || !derived(rel) {
				continue
			}
			path := filepath.Join(root, slug, filepath.FromSlash(rel))
			if err := os.Remove(path); err == nil {
				removed++
			} else if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			os.Remove(mdfile.LockPath(path))
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
		unlock()
	}
	// _tm is tm's alone; a link there is removed, never followed.
	if err := os.RemoveAll(filepath.Join(root, tmDir)); err != nil {
		errs = append(errs, err)
	}
	st := readState(root)
	for rel := range st.Files {
		if strings.HasPrefix(rel, tmDir+"/") {
			delete(st.Files, rel)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, obsidianDir, "tm.json")); err == nil {
		if b, err := json.MarshalIndent(&st, "", "  "); err == nil {
			if err := fsx.WriteAtomic(filepath.Join(root, obsidianDir, "tm.json"), append(b, '\n'), 0o644); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, p := range projects() {
		if err := p.Journal(who, "vault.off", p.Slug, "derived notes off and removed"); err != nil {
			errs = append(errs, err)
		}
	}
	return removed, errors.Join(errs...)
}

// readLedger reads the hash ledger at file; empty when it can't.
func readLedger(file string) ledger {
	l := ledger{Files: map[string]string{}}
	if b, err := os.ReadFile(file); err == nil {
		json.Unmarshal(b, &l)
		if l.Files == nil {
			l.Files = map[string]string{}
		}
	}
	return l
}
