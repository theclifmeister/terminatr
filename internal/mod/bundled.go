package mod

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/version"
)

// Bundled mods (mods design §11, docs/SPEC.md §5.3): tm's own defaults
// as data mods, embedded in the binary and listed with the installed
// ones as "built in": the built-in theme, the three icon sets, the
// default keymap, the sidebar's default layout, the coordinator's and
// the threads' policy, and task templates. tm draws its defaults from
// these files, so each default is a mod anyone can copy and change, and
// the loaders get their first users. They load from the binary, never
// from disk; they update with tm. They are enabled unless their
// [mods.<id>] table says enabled = false, and they come after the user's
// mods: an enabled keymap or policy mod of the user's goes first.
//
// The defaults tm can't do without (the look, the keys, the layout and
// the policy) can't be turned off: another mod replaces them (a pick in
// General settings, or an enabled keymap or policy mod). The templates
// can.

//go:generate go run ./bundlegen/cmd
//go:embed all:bundled
var bundledFS embed.FS

// The bundled mods' ids.
const (
	BundledTheme     = "default-theme"
	BundledKeymap    = "default-keymap"
	BundledLayout    = "default-layout"
	BundledPolicy    = "default-policy"
	BundledTemplates = "default-templates"
	// BundledMemory is the project popup's Memory tab, a code mod (proof
	// 2 of the mod API, design §11); its source is examples/mods/memory-tab.
	BundledMemory = "memory-tab"
)

// MemoryView is the Memory tab mod's project tab: the popup draws it in
// the core Memory tab's place while the mod runs.
const MemoryView = "memory"

// BundledIcons is the bundled icon set mod for a built-in set's name
// (unicode, nerd, ascii): "<name>-icons".
func BundledIcons(name string) string { return name + "-icons" }

// bundledOrder puts the bundled mods after every [mods.<id>] table.
const bundledOrder = math.MaxInt32

// replaceHow says, for each bundled mod that can't be turned off, what
// replaces it.
var replaceHow = map[string]string{
	BundledTheme:            "pick another theme in General settings",
	BundledIcons("unicode"): "pick another icon set in General settings",
	BundledIcons("nerd"):    "pick another icon set in General settings",
	BundledIcons("ascii"):   "pick another icon set in General settings",
	BundledKeymap:           "enable a keymap mod: its keys come first",
	BundledLayout:           "pick another layout in General settings",
	BundledPolicy:           "enable a policy mod: its policy comes first",
}

// ErrRequired refuses to turn off a bundled default tm can't do without.
type ErrRequired struct{ ID string }

func (e ErrRequired) Error() string {
	return fmt.Sprintf("%s is built in and tm's default: it can't be turned off; %s", e.ID, e.Replace())
}

// Replace says what replaces the mod: "pick another theme in General
// settings".
func (e ErrRequired) Replace() string { return replaceHow[e.ID] }

// BundledIDs are the bundled mods' ids, sorted. No installed mod may
// take one.
func BundledIDs() []string {
	ents, _ := bundledFS.ReadDir("bundled")
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out
}

// IsBundled reports whether id is a bundled mod's.
func IsBundled(id string) bool { return slices.Contains(BundledIDs(), id) }

// Bundled is bundled mod id as its [mods.<id>] table t (zero: none)
// leaves it: enabled unless t turns it off, and after the user's mods.
// A bundled mod that doesn't load is a bug in tm; it shows Invalid.
func Bundled(id string, t config.Mod) Mod {
	sub, err := fs.Sub(bundledFS, "bundled/"+id)
	m := Mod{ID: id, FS: sub, Builtin: true, Required: replaceHow[id] != "", State: Invalid,
		Projects: t.Projects, Order: bundledOrder}
	if err != nil {
		m.Err = err
		return m
	}
	data, err := fs.ReadFile(sub, ManifestFile)
	if err != nil {
		m.Err = err
		return m
	}
	man, err := ParseManifest(data, "")
	m.Manifest = man
	if err != nil {
		m.Err = err
		return m
	}
	// It is the version of the tm it ships in, when that is a release.
	if v := strings.TrimPrefix(version.Version, "v"); v != "" {
		if _, ok := update.Newer(v, v); ok {
			man.Version = v
		}
	}
	// It is part of tm: it runs with what it asks for, as tm does,
	// without a consent screen, and with its settings' defaults.
	m.Grants = man.Permissions.Requested()
	m.Settings = map[string]any{}
	for _, k := range sortedKeys(man.Settings) {
		m.Settings[k], _ = man.Settings[k].Check(man.Settings[k].Default)
	}
	m.State = Enabled
	if t.EnabledSet && !t.Enabled && !m.Required {
		m.State = Disabled
	}
	return m
}

// BundledData is the file of kind (theme, icons, keymap or layout) of
// bundled mod id: what tm's built-in look and keys are drawn from. It
// panics when tm was built without it.
func BundledData(id, kind string) []byte {
	m := Bundled(id, config.Mod{})
	b, err := m.Data(kind)
	if err != nil {
		panic(fmt.Sprintf("bundled mod %s: %v", id, err))
	}
	return b
}

// BundledPolicyText is the bundled policy for role (coordinator,
// thread): the role's built-in policy part, which an enabled policy mod
// replaces.
func BundledPolicyText(role string) (string, bool) {
	m := Bundled(BundledPolicy, config.Mod{})
	f := m.Manifest.Provides.Rules[role+"_policy"]
	if f == "" {
		return "", false
	}
	b, err := m.ReadFile(f)
	return string(b), err == nil
}

// withBundled adds the bundled mods to r, with their tables in tables,
// after the installed ones (which tm mod list and the Mods tab show
// first). An installed folder named as one is shadowed: it doesn't
// load, and Shadowed says so.
func (r *Registry) withBundled(tables map[string]config.Mod) {
	for _, id := range BundledIDs() {
		if i := slices.IndexFunc(r.Mods, func(m Mod) bool { return m.ID == id }); i >= 0 {
			r.Shadowed = append(r.Shadowed, id)
			r.Mods = slices.Delete(r.Mods, i, i+1)
		}
		r.Orphans = slices.DeleteFunc(r.Orphans, func(o string) bool { return o == id })
	}
	for _, id := range BundledIDs() {
		r.Mods = append(r.Mods, Bundled(id, tables[id]))
	}
}
