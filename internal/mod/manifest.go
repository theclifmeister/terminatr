// Package mod reads the user's mods: each one's manifest (mod.toml), the
// mod API check, and the registry that joins the mods installed under
// ~/.terminatr/mods with their [mods.<id>] tables in config.toml (mods
// design §5, §7 and §8; docs/SPEC.md §5.3). Mods are global, each limited
// to some projects when its table lists them. Enabling a mod, granting
// it permissions and changing its settings are human calls (write.go).
//
// Nothing here runs a mod: the host, the supervisor and the broker come
// later and take their mods from the Registry.
package mod

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/theclifmeister/terminatr/internal/update"
	"github.com/theclifmeister/terminatr/internal/view"
)

// ManifestFile is the manifest's name in a mod's folder.
const ManifestFile = "mod.toml"

// Manifest is a parsed, checked mod.toml. Keys tm doesn't know are
// ignored: within an API major a newer manifest only adds fields.
type Manifest struct {
	ID          string   `toml:"id"`
	Name        string   `toml:"name"`
	Version     string   `toml:"version"`
	API         string   `toml:"api"`
	MinTM       string   `toml:"min_tm"`
	Description string   `toml:"description"`
	Authors     []string `toml:"authors"`
	Homepage    string   `toml:"homepage"`

	Provides    Provides           `toml:"provides"`
	Permissions Permissions        `toml:"permissions"`
	Settings    map[string]Setting `toml:"settings"`
}

// Provides is what tm loads from the mod, any mix of tiers. Paths are
// relative to the mod's folder and stay inside it.
type Provides struct {
	// Main is a code mod's entry point (.ts or .js), run in a mod host.
	Main    string   `toml:"main"`
	Columns []Column `toml:"columns"`
	Views   []View   `toml:"views"`
	// Tools are tools the mod offers agents, through the agent mod and
	// tm mcp; its code answers each call ($.tool.handle).
	Tools []Tool `toml:"tools"`
	// Commands are what the human can run from the dashboard (a key, a
	// ≡ menu entry) or tm mod run; its code gets command.run.
	Commands []Command `toml:"commands"`
	// Data files: a theme, an icon set, a keymap, a layout (.toml).
	Theme  string `toml:"theme"`
	Icons  string `toml:"icons"`
	Keymap string `toml:"keymap"`
	Layout string `toml:"layout"`
	// Rules are rules addenda by role (coordinator, thread) and policies
	// that replace a role's built-in policy part (coordinator_policy,
	// thread_policy), .md files.
	Rules map[string]string `toml:"rules"`
	// Templates and Agents are folders: task templates, agent manifests.
	Templates string `toml:"templates"`
	Agents    string `toml:"agents"`
}

// Column is a column a code mod publishes cells into, named
// <mod id>.<column id> in layouts.
type Column struct {
	ID    string   `toml:"id"`
	Title string   `toml:"title"`
	Width int      `toml:"width"`
	Where []string `toml:"where"`
}

// View is a panel a code mod draws (the views permission).
type View struct {
	ID    string `toml:"id"`
	Title string `toml:"title"`
	Slot  string `toml:"slot"`
}

// Command is a command of a code mod (mods design §2, tier 4): the
// human runs it with its key on the dashboard, from the ≡ menu's Mods
// group or with tm mod run <mod> <id>, and the mod gets command.run.
// It is named <mod id>:<command id> (CommandName).
type Command struct {
	ID    string `toml:"id"`
	Title string `toml:"title"`
	// Key is its key on the dashboard's list, "" for none. A key the
	// core (or a mod enabled earlier) has is a key conflict: the command
	// keeps its menu entry and tm mod run.
	Key string `toml:"key"`
	// Menu false leaves it out of the ≡ menu.
	Menu *bool `toml:"menu"`
}

// InMenu says whether the command has a ≡ menu entry.
func (c Command) InMenu() bool { return c.Menu == nil || *c.Menu }

// CommandName is "<mod>:<command>", how the Mods tab, the help and tm
// mod run's messages name a command.
func CommandName(mod, cmd string) string { return mod + ":" + cmd }

// Limits of a mod's commands.
const (
	MaxCommands     = 16
	MaxCommandTitle = 48
)

// LockedCommandKeys are keys no command takes: the escape hatches (esc,
// ctrl+c, the prefix commands d, q and r), the area keys and the
// sidebar's numbers. The prefix itself is the console's to refuse.
var LockedCommandKeys = []string{"esc", "ctrl+c", "d", "q", "r", "tab", "shift+tab", "enter",
	"1", "2", "3", "4", "5", "6", "7", "8", "9"}

// CheckCommandKey says why key can't be a command's, nil if it can.
func CheckCommandKey(key string) error {
	switch {
	case key == "" || len(key) > 16 || strings.ContainsAny(key, " \t\n") || strings.ToLower(key) != key && len(key) > 1:
		return fmt.Errorf("%q isn't a key (\"g\", \"G\", \"ctrl+g\", \"f5\")", key)
	case strings.HasPrefix(key, "prefix+"):
		return fmt.Errorf("%s: a command has no prefix key", key)
	case slices.Contains(LockedCommandKeys, key):
		return fmt.Errorf("%s is locked (esc, ctrl+c, d, q, r, tab, enter and 1-9 stay the core's)", key)
	}
	return nil
}

// Tool is a tool a code mod offers agents (mods design §2, tier 5): an
// agent calls it as <mod id>_<tool id> (ToolName), and the call goes
// agent → agent mod (or tm mcp) → server → the mod's host. Its
// description and input are text the agent reads, so the consent shows
// them in full.
type Tool struct {
	ID          string `toml:"id"`
	Description string `toml:"description"`
	// Roles are the sessions that get it: thread, coordinator; empty
	// for both.
	Roles []string  `toml:"roles"`
	Input ToolInput `toml:"input"`
}

// ToolInput is a tool's input as a JSON schema of an object: its
// properties, each a schema with a type (string, integer, number,
// boolean, array or object) and whatever else JSON schema allows, and
// the required ones. Other properties are refused.
type ToolInput struct {
	Properties map[string]map[string]any `toml:"properties"`
	Required   []string                  `toml:"required"`
}

// Session roles a tool may be for.
const (
	RoleThread      = "thread"
	RoleCoordinator = "coordinator"
)

// Limits of a mod's tools. MaxToolName keeps an agent's full name for
// it within 64 characters (Claude's mcp__terminatr__<name>).
const (
	MaxTools       = 16
	MaxToolName    = 48
	MaxToolDescLen = 2048
)

// ToolName is the name agents call mod id's tool by: "<mod>_<tool>".
// A mod id has no underscore, so the first one splits it (ParseToolName),
// and terminatr's own tools (report, status, …) have none.
func ToolName(mod, tool string) string { return mod + "_" + tool }

// ParseToolName splits a mod tool's name into the mod's id and the
// tool's; ok is false for a name that isn't one.
func ParseToolName(name string) (mod, tool string, ok bool) {
	mod, tool, ok = strings.Cut(name, "_")
	return mod, tool, ok && idRE.MatchString(mod) && keyRE.MatchString(tool)
}

// For says whether the tool is offered to a session of role.
func (t Tool) For(role string) bool { return len(t.Roles) == 0 || slices.Contains(t.Roles, role) }

// Schema is the tool's input as the JSON schema agents are given.
func (t Tool) Schema() map[string]any {
	props := map[string]any{}
	for k, v := range t.Input.Properties {
		props[k] = v
	}
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(t.Input.Required) > 0 {
		s["required"] = slices.Clone(t.Input.Required)
	}
	return s
}

// jsonTypes are the types a tool's input property may have.
var jsonTypes = []string{"string", "integer", "number", "boolean", "array", "object"}

// View slots (mods design §6).
const (
	SlotProjectTab    = "project.tab"
	SlotDashboardCard = "dashboard.card"
	SlotFull          = "full"
)

// Permissions are what the mod asks for. The human grants each by name
// in [mods.<id>] grants; a mod runs with granted ∩ requested. The lists
// (net hosts, commands, file roots) come with their name's grant.
type Permissions struct {
	ProjectRead bool     `toml:"project_read"`
	Views       bool     `toml:"views"`
	Notify      bool     `toml:"notify"`
	Inbox       bool     `toml:"inbox"`
	Tools       bool     `toml:"tools"` // offer agents [provides].tools
	Net         []string `toml:"net"`
	Commands    []string `toml:"commands"`
	Files       Files    `toml:"files"`
}

// Files are the roots a mod may read, or write, outside its own folder.
type Files struct {
	Read  []string `toml:"read"`
	Write []string `toml:"write"`
}

// Permission names, as [mods.<id>] grants lists them. There is none for
// credentials, ever.
const (
	PermProjectRead = "project_read"
	PermViews       = "views"
	PermNotify      = "notify"
	PermInbox       = "inbox"
	PermTools       = "tools"
	PermFiles       = "files"
	PermNet         = "net"
	PermCommands    = "commands"
)

// Perms are all the permission names, in the consent screen's order.
var Perms = []string{PermProjectRead, PermViews, PermNotify, PermInbox, PermTools, PermFiles, PermNet, PermCommands}

// Requested lists the permissions the manifest asks for, in Perms order.
func (p Permissions) Requested() []string {
	asks := map[string]bool{
		PermProjectRead: p.ProjectRead, PermViews: p.Views, PermNotify: p.Notify, PermInbox: p.Inbox, PermTools: p.Tools,
		PermFiles: len(p.Files.Read)+len(p.Files.Write) > 0, PermNet: len(p.Net) > 0, PermCommands: len(p.Commands) > 0,
	}
	var out []string
	for _, n := range Perms {
		if asks[n] {
			out = append(out, n)
		}
	}
	return out
}

// Setting is one of the mod's own options, shown on its settings page
// and set in [mods.<id>.settings].
type Setting struct {
	// Type is int, bool, string or choice (one of Options).
	Type    string   `toml:"type"`
	Default any      `toml:"default"`
	Min     *int64   `toml:"min"`
	Max     *int64   `toml:"max"`
	Options []string `toml:"options"`
	Label   string   `toml:"label"`
}

// Setting types.
const (
	SettingInt    = "int"
	SettingBool   = "bool"
	SettingString = "string"
	SettingChoice = "choice"
)

// Check returns v as the setting's value (an int64, a bool or a string)
// or why it isn't one.
func (s Setting) Check(v any) (any, error) {
	switch s.Type {
	case SettingInt:
		var n int64
		switch x := v.(type) {
		case int64:
			n = x
		case int:
			n = int64(x)
		default:
			return nil, fmt.Errorf("must be a whole number")
		}
		if s.Min != nil && n < *s.Min || s.Max != nil && n > *s.Max {
			return nil, fmt.Errorf("must be %s", s.span())
		}
		return n, nil
	case SettingBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("must be true or false")
	case SettingString:
		if str, ok := v.(string); ok {
			return str, nil
		}
		return nil, fmt.Errorf("must be text")
	case SettingChoice:
		if str, ok := v.(string); ok && slices.Contains(s.Options, str) {
			return str, nil
		}
		return nil, fmt.Errorf("must be one of %s", strings.Join(s.Options, ", "))
	}
	return nil, fmt.Errorf("has an unknown type %q", s.Type)
}

func (s Setting) span() string {
	switch {
	case s.Min != nil && s.Max != nil:
		return fmt.Sprintf("%d to %d", *s.Min, *s.Max)
	case s.Min != nil:
		return fmt.Sprintf("at least %d", *s.Min)
	}
	return fmt.Sprintf("at most %d", *s.Max)
}

// MaxColumnWidth caps a mod column, in cells: the width the sidebar
// draws at most (view.MaxModColumnWidth).
const MaxColumnWidth = view.MaxModColumnWidth

var (
	idRE    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	apiRE   = regexp.MustCompile(`^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})$`)
	whereRE = regexp.MustCompile(`^[a-z]+\.[a-z]+$`)
	// hostRE is a DNS name, optionally *.-prefixed; an IP address isn't one.
	hostRE = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	progRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	keyRE  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// MaxIDLen caps a mod id; it names folders, config tables and namespaces.
const MaxIDLen = 40

// reservedIDs are the older [mods] keys config.toml still reads as
// [agent_mod]'s (enabled, band, pane): a mod by one of these names
// couldn't have a [mods.<id>] table.
var reservedIDs = []string{"enabled", "band", "pane"}

// CheckID reports why id can't name a mod, nil if it can.
func CheckID(id string) error {
	switch {
	case id == "":
		return errors.New("has no id")
	case len(id) > MaxIDLen:
		return fmt.Errorf("id %q is longer than %d characters", id, MaxIDLen)
	case !idRE.MatchString(id):
		return fmt.Errorf("id %q must be lowercase letters, digits and single dashes", id)
	case slices.Contains(reservedIDs, id):
		return fmt.Errorf("id %q is reserved", id)
	}
	return nil
}

// ParseManifest parses and checks a manifest's bytes. dir is the mod's
// folder, to check that the files it names are there; "" skips that.
func ParseManifest(data []byte, dir string) (*Manifest, error) {
	var m Manifest
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, fmt.Errorf("%s doesn't parse: %w", ManifestFile, err)
	}
	if err := m.check(dir); err != nil {
		return &m, err
	}
	return &m, nil
}

// ReadManifest reads and checks dir/mod.toml.
func ReadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("has no %s", ManifestFile)
	}
	if err != nil {
		return nil, err
	}
	return ParseManifest(data, dir)
}

// check reports every problem in m, joined.
func (m *Manifest) check(dir string) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if err := CheckID(m.ID); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(m.Name) == "" {
		bad("has no name")
	}
	if _, ok := update.Newer(m.Version, m.Version); !ok || strings.HasPrefix(m.Version, "v") {
		bad("version %q isn't MAJOR.MINOR.PATCH", m.Version)
	}
	if !apiRE.MatchString(m.API) {
		bad("api %q isn't MAJOR.MINOR", m.API)
	}
	if m.MinTM != "" {
		if _, ok := update.Newer(m.MinTM, m.MinTM); !ok {
			bad("min_tm %q isn't a tm version", m.MinTM)
		}
	}
	if m.Homepage != "" && !strings.HasPrefix(m.Homepage, "https://") && !strings.HasPrefix(m.Homepage, "http://") {
		bad("homepage %q isn't a web address", m.Homepage)
	}
	p := m.Provides
	if p.Main != "" {
		if ext := path.Ext(p.Main); ext != ".ts" && ext != ".js" {
			bad("provides.main %q isn't a .ts or .js file", p.Main)
		}
		errs = append(errs, inside(dir, "provides.main", p.Main, false))
	}
	for k, f := range map[string]string{"theme": p.Theme, "icons": p.Icons, "keymap": p.Keymap, "layout": p.Layout} {
		if f != "" {
			errs = append(errs, inside(dir, "provides."+k, f, false))
		}
	}
	for _, role := range sortedKeys(p.Rules) {
		if !slices.Contains(rulesKeys, role) {
			bad("provides.rules.%s: rules are coordinator or thread (addenda), coordinator_policy or thread_policy", role)
		}
		errs = append(errs, inside(dir, "provides.rules."+role, p.Rules[role], false))
	}
	for k, d := range map[string]string{"templates": p.Templates, "agents": p.Agents} {
		if d != "" {
			errs = append(errs, inside(dir, "provides."+k, d, true))
		}
	}
	code := p.Main != ""
	seen := map[string]bool{}
	for i, c := range p.Columns {
		at := fmt.Sprintf("provides.columns[%d]", i)
		switch {
		case !code:
			bad("%s: a column needs provides.main, the code that fills it", at)
		case !idRE.MatchString(c.ID):
			bad("%s: id %q must be lowercase letters, digits and dashes", at, c.ID)
		case seen["c:"+c.ID]:
			bad("%s: id %q is listed twice", at, c.ID)
		}
		seen["c:"+c.ID] = true
		if c.Width < 1 || c.Width > MaxColumnWidth {
			bad("%s: width must be 1 to %d cells", at, MaxColumnWidth)
		}
		if len(c.Where) == 0 {
			bad("%s: where names no slot", at)
		}
		for _, w := range c.Where {
			if !whereRE.MatchString(w) {
				bad("%s: where %q isn't a slot like sidebar.thread", at, w)
			}
		}
	}
	for i, v := range p.Views {
		at := fmt.Sprintf("provides.views[%d]", i)
		switch {
		case !code:
			bad("%s: a view needs provides.main, the code that draws it", at)
		case !idRE.MatchString(v.ID):
			bad("%s: id %q must be lowercase letters, digits and dashes", at, v.ID)
		case seen["v:"+v.ID]:
			bad("%s: id %q is listed twice", at, v.ID)
		}
		seen["v:"+v.ID] = true
		if strings.TrimSpace(v.Title) == "" {
			bad("%s: has no title", at)
		}
		if v.Slot != SlotProjectTab && v.Slot != SlotDashboardCard && v.Slot != SlotFull {
			bad("%s: slot %q must be %s, %s or %s", at, v.Slot, SlotProjectTab, SlotDashboardCard, SlotFull)
		}
	}
	if len(p.Tools) > MaxTools {
		bad("provides.tools: at most %d tools", MaxTools)
	}
	for i, t := range p.Tools {
		at := fmt.Sprintf("provides.tools[%d]", i)
		switch {
		case !code:
			bad("%s: a tool needs provides.main, the code that answers it", at)
		case !keyRE.MatchString(t.ID):
			bad("%s: id %q must be lowercase letters, digits and underscores, starting with a letter", at, t.ID)
		case len(ToolName(m.ID, t.ID)) > MaxToolName:
			bad("%s: %s is longer than %d characters", at, ToolName(m.ID, t.ID), MaxToolName)
		case seen["t:"+t.ID]:
			bad("%s: id %q is listed twice", at, t.ID)
		}
		seen["t:"+t.ID] = true
		if d := strings.TrimSpace(t.Description); d == "" {
			bad("%s: has no description", at)
		} else if len(d) > MaxToolDescLen {
			bad("%s: description is longer than %d bytes", at, MaxToolDescLen)
		}
		for _, r := range t.Roles {
			if r != RoleThread && r != RoleCoordinator {
				bad("%s: role %q must be %s or %s", at, r, RoleThread, RoleCoordinator)
			}
		}
		for _, k := range sortedKeys(t.Input.Properties) {
			if !keyRE.MatchString(k) {
				bad("%s: input property %q must be lowercase letters, digits and underscores", at, k)
			}
			if typ, _ := t.Input.Properties[k]["type"].(string); !slices.Contains(jsonTypes, typ) {
				bad("%s: input property %q needs a type: %s", at, k, strings.Join(jsonTypes, ", "))
			}
		}
		for _, k := range t.Input.Required {
			if _, ok := t.Input.Properties[k]; !ok {
				bad("%s: required %q isn't an input property", at, k)
			}
		}
	}
	if len(p.Commands) > MaxCommands {
		bad("provides.commands: at most %d commands", MaxCommands)
	}
	keys := map[string]string{}
	for i, c := range p.Commands {
		at := fmt.Sprintf("provides.commands[%d]", i)
		switch {
		case !code:
			bad("%s: a command needs provides.main, the code that runs it", at)
		case !idRE.MatchString(c.ID):
			bad("%s: id %q must be lowercase letters, digits and dashes", at, c.ID)
		case seen["k:"+c.ID]:
			bad("%s: id %q is listed twice", at, c.ID)
		}
		seen["k:"+c.ID] = true
		if t := strings.TrimSpace(c.Title); t == "" {
			bad("%s: has no title", at)
		} else if len([]rune(t)) > MaxCommandTitle {
			bad("%s: title is longer than %d characters", at, MaxCommandTitle)
		}
		if c.Key != "" {
			if err := CheckCommandKey(c.Key); err != nil {
				bad("%s: key %v", at, err)
			} else if o, ok := keys[c.Key]; ok {
				bad("%s: key %s is %s's already", at, c.Key, o)
			}
			keys[c.Key] = c.ID
		}
	}
	if len(p.Tools) > 0 && !m.Permissions.Tools {
		bad("provides.tools needs permissions.tools")
	}
	if m.Permissions.Tools && len(p.Tools) == 0 {
		bad("permissions.tools offers no tool: list them in provides.tools")
	}
	if len(p.Views) > 0 && !m.Permissions.Views {
		bad("provides.views needs permissions.views")
	}
	perm := m.Permissions
	if !code && len(perm.Requested()) > 0 {
		bad("permissions are for code mods (provides.main); a data mod needs none")
	}
	for _, h := range perm.Net {
		if !hostRE.MatchString(h) {
			bad("permissions.net: %q isn't a host name (no scheme, port or path)", h)
		}
	}
	for _, c := range perm.Commands {
		if !progRE.MatchString(c) {
			bad("permissions.commands: %q isn't a program name (no path, no arguments)", c)
		}
	}
	for _, r := range slices.Concat(perm.Files.Read, perm.Files.Write) {
		if !strings.HasPrefix(r, "~/") && !filepath.IsAbs(r) {
			bad("permissions.files: %q must be absolute or start with ~/", r)
		} else if slices.Contains(strings.Split(filepath.ToSlash(r), "/"), "..") {
			bad("permissions.files: %q has a .. in it", r)
		}
	}
	for _, k := range sortedKeys(m.Settings) {
		s := m.Settings[k]
		at := "settings." + k
		if !keyRE.MatchString(k) {
			bad("%s: a setting's name is lowercase letters, digits and underscores", at)
		}
		if s.Type == SettingChoice && len(s.Options) == 0 {
			bad("%s: a choice needs options", at)
		}
		if s.Type != SettingInt && (s.Min != nil || s.Max != nil) {
			bad("%s: min and max are for an int", at)
		}
		if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
			bad("%s: min is above max", at)
		}
		if s.Default == nil {
			bad("%s: has no default", at)
		} else if _, err := s.Check(s.Default); err != nil {
			bad("%s: default %v", at, err)
		}
	}
	return errors.Join(errs...)
}

// inside reports why rel, named at in the manifest, isn't a file (or a
// folder, for dir) inside the mod's folder modDir. Symlinks may not lead
// out of it. With modDir "" only the path itself is checked.
func inside(modDir, at, rel string, dir bool) error {
	if rel == "" || filepath.IsAbs(rel) || path.IsAbs(rel) || strings.Contains(rel, `\`) || filepath.VolumeName(rel) != "" {
		return fmt.Errorf("%s: %q must be a path inside the mod's folder, with /", at, rel)
	}
	if slices.Contains(strings.Split(rel, "/"), "..") {
		return fmt.Errorf("%s: %q has a .. in it", at, rel)
	}
	if modDir == "" {
		return nil
	}
	root, err := filepath.EvalSymlinks(modDir)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Errorf("%s: %q isn't there", at, rel)
	}
	if r, err := filepath.Rel(root, real); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s: %q leads out of the mod's folder", at, rel)
	}
	fi, err := os.Stat(real)
	if err != nil {
		return err
	}
	if fi.IsDir() != dir {
		want := "a file"
		if dir {
			want = "a folder"
		}
		return fmt.Errorf("%s: %q isn't %s", at, rel, want)
	}
	return nil
}

// IsCode reports whether the mod has code, run in a mod host; a data
// mod only has files tm reads.
func (m *Manifest) IsCode() bool { return m.Provides.Main != "" }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
