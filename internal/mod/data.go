package mod

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/view"
)

// Data mods (mods design §2, tiers 1-3): files tm reads, with no code
// and no host. The look (a theme, an icon set) and the sidebar's layout
// are one at a time, picked in General settings ([ui] theme, icons,
// layout); every enabled keymap applies, the first enabled winning a
// key; rules addenda, task templates, agent manifests and guard rules
// all add up. The look, keys, layout and agents are the console's and
// the machine's, so they follow a mod being enabled, not its projects;
// rules, templates and guard rules follow its projects too.

// Data kinds, as [provides] names them.
const (
	KindTheme     = "theme"
	KindIcons     = "icons"
	KindKeymap    = "keymap"
	KindLayout    = "layout"
	KindRules     = "rules"
	KindTemplates = "templates"
	KindAgents    = "agents"
	KindGuard     = "guard"
)

// MaxDataFile caps a data file tm reads from a mod: a theme, an icon
// set, a keymap, a layout, a rules addendum or a template.
const MaxDataFile = 64 << 10

// Has reports whether the mod's manifest provides kind.
func (m Mod) Has(kind string) bool {
	if m.Manifest == nil {
		return false
	}
	p := m.Manifest.Provides
	switch kind {
	case KindTheme:
		return p.Theme != ""
	case KindIcons:
		return p.Icons != ""
	case KindKeymap:
		return p.Keymap != ""
	case KindLayout:
		return p.Layout != ""
	case KindRules:
		return len(p.Rules) > 0
	case KindTemplates:
		return p.Templates != ""
	case KindAgents:
		return p.Agents != ""
	case KindGuard:
		return len(p.Guard) > 0
	}
	return false
}

// Providers lists the enabled mods that provide kind, in the order they
// were enabled (their tables' order in config.toml), then by id.
func (r *Registry) Providers(kind string) []Mod {
	var out []Mod
	for _, m := range r.Mods {
		if m.State == Enabled && m.Has(kind) {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// Provider is the enabled mod id when it provides kind: the one a [ui]
// pick names. It says why not otherwise.
func (r *Registry) Provider(kind, id string) (Mod, error) {
	m, ok := r.Get(id)
	switch {
	case !ok:
		return m, fmt.Errorf("no mod %q is installed", id)
	case m.State != Enabled:
		return m, fmt.Errorf("%s is %s", id, m.State)
	case !m.Has(kind):
		return m, fmt.Errorf("%s has no %s", id, kind)
	}
	return m, nil
}

// ReadFile reads rel, a file the manifest names, from the mod's folder.
// It is checked again to be inside it (the folder may have changed since
// the mod loaded) and at most MaxDataFile.
func (m Mod) ReadFile(rel string) ([]byte, error) {
	var f fs.File
	var err error
	if m.FS != nil {
		if err := inside("", rel, rel, false); err != nil {
			return nil, fmt.Errorf("%s: %w", m.ID, err)
		}
		f, err = m.FS.Open(rel)
	} else {
		if err := inside(m.Dir, rel, rel, false); err != nil {
			return nil, fmt.Errorf("%s: %w", m.ID, err)
		}
		f, err = os.Open(filepath.Join(m.Dir, filepath.FromSlash(rel)))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.ID, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxDataFile+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", m.ID, err)
	}
	if len(b) > MaxDataFile {
		return nil, fmt.Errorf("%s: %s is over %d KiB", m.ID, rel, MaxDataFile>>10)
	}
	return b, nil
}

// Data reads the mod's file for kind: its theme, icons, keymap or
// layout.
func (m Mod) Data(kind string) ([]byte, error) {
	if !m.Has(kind) {
		return nil, fmt.Errorf("%s has no %s", m.ID, kind)
	}
	p := m.Manifest.Provides
	return m.ReadFile(map[string]string{KindTheme: p.Theme, KindIcons: p.Icons, KindKeymap: p.Keymap, KindLayout: p.Layout}[kind])
}

// Keymaps

// ParseKeymap reads a keymap: each action id to its keys, id = ["key",
// …]. An id's dots may be written as TOML's (task.delegate = […]) or
// quoted ("task.delegate" = […]); whether the ids and keys can be bound
// is the console's to say (tui.ApplyKeymap).
func ParseKeymap(data []byte) (map[string][]string, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	var errs []error
	var walk func(prefix string, t map[string]any)
	walk = func(prefix string, t map[string]any) {
		for _, k := range slices.Sorted(maps.Keys(t)) {
			id := k
			if prefix != "" {
				id = prefix + "." + k
			}
			switch v := t[k].(type) {
			case map[string]any:
				walk(id, v)
			case []any:
				keys := make([]string, 0, len(v))
				for _, e := range v {
					s, ok := e.(string)
					if !ok {
						errs = append(errs, fmt.Errorf("%s: keys are strings", id))
						keys = nil
						break
					}
					keys = append(keys, s)
				}
				if keys != nil {
					out[id] = keys
				}
			default:
				errs = append(errs, fmt.Errorf("%s: want a list of keys, id = [\"key\", …]", id))
			}
		}
	}
	walk("", raw)
	return out, errors.Join(errs...)
}

// Keymap is the keymaps of the enabled keymap mods, joined, but the
// built-in default-keymap's, the base they apply over: each action id
// to its keys, from the first mod (in the order they were enabled) that
// binds it, and from none whose keymap doesn't parse. A key bound by two
// mods goes to the first; the later mod's action keeps its default
// keys. errs say what was left out, each naming its mod.
func (r *Registry) Keymap() (km map[string][]string, from map[string]string, errs []error) {
	km, from = map[string][]string{}, map[string]string{}
	keyOwner := map[string]string{} // key → the id the first mod bound it to
	for _, m := range r.Providers(KindKeymap) {
		if m.Builtin {
			continue
		}
		data, err := m.Data(KindKeymap)
		var one map[string][]string
		if err == nil {
			one, err = ParseKeymap(data)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s keymap: %w", m.ID, err))
			if one == nil {
				continue
			}
		}
	ids:
		for _, id := range slices.Sorted(maps.Keys(one)) {
			if prev, ok := from[id]; ok {
				errs = append(errs, fmt.Errorf("%s keymap: %s is bound by %s already", m.ID, id, prev))
				continue
			}
			for _, k := range one[id] {
				if o, ok := keyOwner[k]; ok {
					errs = append(errs, fmt.Errorf("%s keymap: key conflict: %s is %s's %s", m.ID, k, from[o], o))
					continue ids
				}
			}
			km[id], from[id] = one[id], m.ID
			for _, k := range one[id] {
				keyOwner[k] = id
			}
		}
	}
	return km, from, errs
}

// Layouts

// Layout is a layout mod's file: the order of the sidebar's columns, the
// built-in ids (view.DefaultColumns) and mods' ("review-age.age").
type Layout struct {
	Sidebar []string `toml:"sidebar"`
}

// ParseLayout reads and checks a layout. A safety column it leaves out
// is still shown, at the end (view.Columns).
func ParseLayout(data []byte) (Layout, error) {
	var l Layout
	md, err := toml.Decode(string(data), &l)
	if err != nil {
		return l, err
	}
	var errs []error
	for _, k := range md.Undecoded() {
		errs = append(errs, fmt.Errorf("unknown key %s", k))
	}
	if len(l.Sidebar) > view.MaxColumns {
		errs = append(errs, fmt.Errorf("sidebar: %d columns, at most %d", len(l.Sidebar), view.MaxColumns))
	}
	seen := map[string]bool{}
	for _, c := range l.Sidebar {
		switch {
		case !slices.Contains(view.DefaultColumns, c) && !view.IsModColumn(c):
			errs = append(errs, fmt.Errorf("sidebar: no column %q (built in: %s; a mod's: <mod>.<column>)", c, strings.Join(view.DefaultColumns, ", ")))
		case seen[c]:
			errs = append(errs, fmt.Errorf("sidebar: %s is listed twice", c))
		}
		seen[c] = true
	}
	return l, errors.Join(errs...)
}

// Rules addenda and policy

// Rules keys in [provides].rules: an addendum for each role, and a
// policy that replaces the role's built-in policy part.
const (
	RulesCoordinator       = "coordinator"
	RulesThread            = "thread"
	RulesCoordinatorPolicy = "coordinator_policy"
	RulesThreadPolicy      = "thread_policy"
)

var rulesKeys = []string{RulesCoordinator, RulesThread, RulesCoordinatorPolicy, RulesThreadPolicy}

// Addendum is one mod's rules text for a role.
type Addendum struct {
	Mod  string
	Text string
	// Builtin: it is a built-in mod's (default-policy), tm's own text.
	Builtin bool
}

// Rules are the mods' rules for role (coordinator or thread) in project
// slug ("" for any): the policy of the first enabled policy mod that
// runs there (the user's before the built-in default-policy; nil: none)
// and every addendum, by mod id.
// errs say which files couldn't be read, or which policy mods lost to
// an earlier one.
func (r *Registry) Rules(role, slug string) (policy *Addendum, addenda []Addendum, errs []error) {
	for _, m := range r.Providers(KindRules) {
		if slug != "" && !m.Runs(slug) {
			continue
		}
		if f := m.Manifest.Provides.Rules[role+"_policy"]; f != "" {
			switch b, err := m.ReadFile(f); {
			case err != nil:
				errs = append(errs, err)
			case policy != nil && m.Builtin:
				// The built-in policy is the one a mod's replaces.
			case policy != nil:
				errs = append(errs, fmt.Errorf("%s: its %s policy is not used: %s's came first", m.ID, role, policy.Mod))
			default:
				policy = &Addendum{Mod: m.ID, Text: string(b), Builtin: m.Builtin}
			}
		}
	}
	for _, m := range r.Mods {
		if m.State != Enabled || !m.Has(KindRules) || slug != "" && !m.Runs(slug) {
			continue
		}
		if f := m.Manifest.Provides.Rules[role]; f != "" {
			b, err := m.ReadFile(f)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if t := strings.TrimSpace(string(b)); t != "" {
				addenda = append(addenda, Addendum{Mod: m.ID, Text: t})
			}
		}
	}
	return policy, addenda, errs
}

// ModGuardRule is a mod's guard rule, with the mod's id.
type ModGuardRule struct {
	Mod string
	GuardRule
}

// GuardRules are the guard rules the enabled mods add for sessions of
// role in project slug, by mod id then the manifest's order. They only
// add refusals (mods design §7, Tighten only); the guard serves them
// with the core's when the human has it on.
func (r *Registry) GuardRules(role, slug string) []ModGuardRule {
	var out []ModGuardRule
	for _, m := range r.Mods {
		if !m.Runs(slug) || !m.Has(KindGuard) {
			continue
		}
		for _, g := range m.Manifest.Provides.Guard {
			if g.For(role) {
				out = append(out, ModGuardRule{Mod: m.ID, GuardRule: g})
			}
		}
	}
	return out
}

// Task templates

// Template is a task template (templates/<name>.md): a title pattern,
// notes and steps.
//
//	# Bug: {title}
//
//	What happens, what should, how to see it.
//
//	## Steps
//
//	- Reproduce it in a test
//	- Fix it
//
// The first line is the title, where {title} is the title given; the
// text up to "## Steps" is the notes; that section's list items are the
// steps.
type Template struct {
	Mod, Name    string
	Title, Notes string
	Steps        []string
	// Builtin: a built-in mod's (default-templates); a user mod's
	// template of the same name comes first.
	Builtin bool
}

// MaxTemplateSteps caps a template's steps.
const MaxTemplateSteps = 50

var (
	templateNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	stepItemRE     = regexp.MustCompile(`^\s*[-*]\s+(\[[ xX]\]\s+)?(.*\S)\s*$`)
)

// ParseTemplate reads template name from its file's bytes.
func ParseTemplate(name string, data []byte) (Template, error) {
	t := Template{Name: name}
	if !templateNameRE.MatchString(name) {
		return t, fmt.Errorf("template %q: a name is lowercase letters, digits and dashes", name)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) || !strings.HasPrefix(lines[i], "# ") {
		return t, fmt.Errorf("template %s: the first line is the title, # <title with {title}>", name)
	}
	t.Title = strings.TrimSpace(lines[i][2:])
	if strings.Count(t.Title, "{title}") != 1 {
		return t, fmt.Errorf("template %s: the title %q needs {title} once, where the given title goes", name, t.Title)
	}
	var notes []string
	steps := false
	for _, l := range lines[i+1:] {
		if strings.HasPrefix(l, "## ") {
			steps = strings.EqualFold(strings.TrimSpace(l[3:]), "steps")
			if steps {
				continue
			}
		}
		if !steps {
			notes = append(notes, l)
			continue
		}
		if m := stepItemRE.FindStringSubmatch(l); m != nil {
			t.Steps = append(t.Steps, m[2])
		} else if strings.TrimSpace(l) != "" {
			return t, fmt.Errorf("template %s: under ## Steps, %q isn't a list item (- step)", name, strings.TrimSpace(l))
		}
	}
	if len(t.Steps) > MaxTemplateSteps {
		return t, fmt.Errorf("template %s: %d steps, at most %d", name, len(t.Steps), MaxTemplateSteps)
	}
	t.Notes = strings.TrimSpace(strings.Join(notes, "\n"))
	return t, nil
}

// Apply is the template filled with title: the task's title, notes and
// steps.
func (t Template) Apply(title string) (string, string, []string) {
	return strings.Replace(t.Title, "{title}", title, 1), t.Notes, slices.Clone(t.Steps)
}

// Ref is how a command names the template: <mod>:<name>.
func (t Template) Ref() string { return t.Mod + ":" + t.Name }

// Templates are the task templates of the enabled mods that run in
// project slug, by mod and name; errs say which didn't read.
func (r *Registry) Templates(slug string) (out []Template, errs []error) {
	for _, m := range r.Mods {
		if !m.Runs(slug) || !m.Has(KindTemplates) {
			continue
		}
		ts, terrs := m.templates()
		out, errs = append(out, ts...), append(errs, terrs...)
	}
	return out, errs
}

// templates are the mod's task templates; errs say which didn't read.
func (m Mod) templates() (out []Template, errs []error) {
	dir := m.Manifest.Provides.Templates
	var files []string
	if m.FS != nil {
		files, _ = fs.Glob(m.FS, path.Join(dir, "*.md"))
	} else {
		if err := inside(m.Dir, "provides.templates", dir, true); err != nil {
			return nil, []error{fmt.Errorf("%s: %w", m.ID, err)}
		}
		files, _ = filepath.Glob(filepath.Join(m.Dir, filepath.FromSlash(dir), "*.md"))
	}
	slices.Sort(files)
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".md")
		b, err := m.ReadFile(path.Join(dir, filepath.Base(f)))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		t, err := ParseTemplate(name, b)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.ID, err))
			continue
		}
		t.Mod, t.Builtin = m.ID, m.Builtin
		out = append(out, t)
	}
	return out, errs
}

// FindTemplate picks the template ref names from ts: <mod>:<name>, or a
// name only one mod has (a built-in one giving way to a user mod's).
func FindTemplate(ts []Template, ref string) (Template, error) {
	modID, name, scoped := strings.Cut(ref, ":")
	if !scoped {
		name, modID = modID, ""
	}
	var hits []Template
	for _, t := range ts {
		if t.Name == name && (modID == "" || t.Mod == modID) {
			hits = append(hits, t)
		}
	}
	if len(hits) > 1 {
		hits = slices.DeleteFunc(hits, func(t Template) bool { return t.Builtin })
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		var have []string
		for _, t := range ts {
			have = append(have, t.Ref())
		}
		if len(have) == 0 {
			return Template{}, fmt.Errorf("no template %q: no enabled mod has templates here", ref)
		}
		return Template{}, fmt.Errorf("no template %q; there are %s", ref, strings.Join(have, ", "))
	}
	refs := make([]string, len(hits))
	for i, t := range hits {
		refs[i] = t.Ref()
	}
	return Template{}, fmt.Errorf("template %q is in more than one mod: name one of %s", ref, strings.Join(refs, ", "))
}

// Agent manifests

// AgentDir is a mod's folder of agent manifests (agents/<name>.toml).
type AgentDir struct {
	Mod, Dir string
}

// AgentDirs are the agent folders of the enabled mods, in the order they
// were enabled.
func (r *Registry) AgentDirs() []AgentDir {
	var out []AgentDir
	for _, m := range r.Providers(KindAgents) {
		if inside(m.Dir, "provides.agents", m.Manifest.Provides.Agents, true) == nil {
			out = append(out, AgentDir{Mod: m.ID, Dir: filepath.Join(m.Dir, filepath.FromSlash(m.Manifest.Provides.Agents))})
		}
	}
	return out
}

// Load checks

// DataChecks check the data files only the console can read whole: a
// theme's colours, an icon set's glyphs, a keymap's action ids. The
// console package (tui) sets them as it loads, so tm doctor and the Mods
// tab check what the console would refuse; a kind without one is
// checked as far as this package can (it parses).
var DataChecks = map[string]func([]byte) error{}

// CheckData loads every data file of the mod as tm would use it and
// says what doesn't load, each error naming the kind: a theme, icon
// set, keymap or layout that doesn't parse or check, a rules file that
// can't be read, a template that doesn't parse. Nil when all load (and
// for a mod whose manifest doesn't).
func (m Mod) CheckData() []error {
	if m.Manifest == nil {
		return nil
	}
	var errs []error
	for _, kind := range []string{KindTheme, KindIcons, KindKeymap, KindLayout} {
		if !m.Has(kind) {
			continue
		}
		data, err := m.Data(kind)
		if err == nil {
			switch kind {
			case KindLayout:
				_, err = ParseLayout(data)
			case KindKeymap:
				_, err = ParseKeymap(data)
			}
		}
		if check := DataChecks[kind]; err == nil && check != nil {
			err = check(data)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", kind, err))
		}
	}
	for _, role := range sortedKeys(m.Manifest.Provides.Rules) {
		if _, err := m.ReadFile(m.Manifest.Provides.Rules[role]); err != nil {
			errs = append(errs, fmt.Errorf("rules %s: %w", role, err))
		}
	}
	if m.Has(KindTemplates) {
		ts, terrs := m.templates()
		for _, err := range terrs {
			errs = append(errs, fmt.Errorf("templates: %w", err))
		}
		if len(ts) == 0 && len(terrs) == 0 {
			errs = append(errs, fmt.Errorf("templates: %s has no <name>.md", m.Manifest.Provides.Templates))
		}
	}
	return errs
}
