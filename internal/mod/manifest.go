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

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/modaction"
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
	// Defaults are values the mod suggests for all projects' non-safety
	// settings (DefaultKeys), as config.toml's [defaults] names them
	// (mods design §7, Tighten only): nothing changes until the human
	// applies them, and a key config.toml sets stays the human's.
	Defaults map[string]any `toml:"defaults"`
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
	// CodeHost is the code host kind the mod serves ("gitlab", mods
	// design §2 tier 5): its code answers the codehost.* methods
	// ($.codehost.handle) for the projects whose code_host names it. It
	// runs the host's CLI, so it needs the commands permission.
	CodeHost string `toml:"code_host"`
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
	// Guard are guard rules the mod adds (mods design §7, Tighten only).
	Guard []GuardRule `toml:"guard"`
}

// GuardRule is a guard rule a mod adds to the core's (docs/SPEC.md
// §8.6, Guard): it only refuses. It names commands a shell tool may not
// run and files no file tool may write or read, with the sentence the
// agent reads; the guard serves it labelled with the mod (mod:<mod
// id>.<rule id>). Nothing in it can turn a core rule off: a manifest
// with a key a rule doesn't have is invalid.
//
//	[[provides.guard]]
//	id = "no-destroy"
//	commands = ["terraform destroy", "kubectl delete namespace*"]
//	writes = ["~/infra/prod"]
//	message = "infrastructure is the human's to change. Say what you need in your report."
type GuardRule struct {
	ID string `toml:"id"`
	// Commands are the commands refused, each words: the command's name
	// (as typed, without its folder), then words its arguments include in
	// this order, others between them allowed. A word ending in * matches
	// any word it starts.
	Commands []string `toml:"commands"`
	// Writes are the files and folders no write or patch tool writes in,
	// Reads those no read or glob tool reads: absolute, or from ~/.
	Writes []string `toml:"writes"`
	Reads  []string `toml:"reads"`
	// Roles are the sessions it guards: thread, coordinator; empty for
	// both.
	Roles []string `toml:"roles"`
	// Message is what the agent reads when the rule refuses a call: why,
	// and what to do instead.
	Message string `toml:"message"`
}

// For says whether the rule guards a session of role.
func (g GuardRule) For(role string) bool { return len(g.Roles) == 0 || slices.Contains(g.Roles, role) }

// guardRuleKeys are the keys a [[provides.guard]] rule may have.
var guardRuleKeys = []string{"id", "commands", "writes", "reads", "roles", "message"}

// Limits of a mod's guard rules.
const (
	maxGuardRules      = 32
	maxGuardPatterns   = 32
	maxGuardMessageLen = 400
)

// DefaultKeys are the all-projects settings a mod may suggest a value
// for ([defaults] in mod.toml): how threads close, PRs are followed and
// the board is kept. Never a safety setting (SafetyKeys), and never
// which agent or model runs or who reaches the coordinator remotely.
var DefaultKeys = []string{"auto_close", "auto_close_days", "pr_followup", "pr_poll_seconds", "auto_clear", "fast_forward_checkout",
	"archive_tasks_days", "archive_threads_days", "archive_inbox_days", "archive_journal_days"}

// SafetyKeys are config.toml's safety settings (mods design §7, The
// rule): only a human call changes them, so a manifest that names one
// as a key anywhere (a setting's name included) but a tool's input
// schema is invalid. A mod may tighten, never loosen.
var SafetyKeys = []string{"yolo", "guard", "guard_off", "merge", "merge_method", "coordinator_merges", "complete_tasks", "start_threads", "models", "parallel_threads", "global_parallel_threads", "coordinator_approves"}

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
	// Description is a line about the view (mod API 1.7), shown on the
	// sidebar's details line for a sidebar section.
	Description string `toml:"description"`
	// Opens is a section's companion view (mod API 1.7): one of the same
	// mod's views in a project.tab, dashboard.card or full, which enter
	// on the section's header asks the mod to open (ui.click).
	Opens string `toml:"opens"`
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
	maxCommands     = 16
	maxCommandTitle = 48
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

// Limits of a mod's tools. maxToolName keeps an agent's full name for
// it within 64 characters (Claude's mcp__terminatr__<name>).
const (
	maxTools       = 16
	maxToolName    = 48
	maxToolDescLen = 2048
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

// View slots (mods design §6). A details.section is a section at the
// bottom of the dashboard's details panel, for the project, task or
// thread it shows; a coordinator.section one at the bottom of the
// coordinator's info panel, for its project (mods design §12, Phase 2).
// A sidebar.section is a section in the band above or below the
// projects sidebar's tree, for the current project; a
// sidebar.project.section one under each active project's threads, for
// that project (mod API 1.7, T348). Sections show; they take no keys.
const (
	SlotProjectTab         = "project.tab"
	SlotDashboardCard      = "dashboard.card"
	SlotFull               = "full"
	SlotDetailsSection     = "details.section"
	SlotCoordinatorSection = "coordinator.section"
	SlotSidebarSection     = "sidebar.section"
	SlotProjectSection     = "sidebar.project.section"
)

// Slots are the view slots, in the manifest check's words' order.
var Slots = []string{SlotProjectTab, SlotDashboardCard, SlotFull, SlotDetailsSection, SlotCoordinatorSection,
	SlotSidebarSection, SlotProjectSection}

// IsSection says whether slot is a panel section's.
func IsSection(slot string) bool {
	switch slot {
	case SlotDetailsSection, SlotCoordinatorSection, SlotSidebarSection, SlotProjectSection:
		return true
	}
	return false
}

// IsSidebar says whether slot is a sidebar section's.
func IsSidebar(slot string) bool { return slot == SlotSidebarSection || slot == SlotProjectSection }

// IsPanel says whether slot is a right-pane section's: the coordinator's
// info panel, or the details panel (and a thread's info panel).
func IsPanel(slot string) bool { return slot == SlotDetailsSection || slot == SlotCoordinatorSection }

// opensSlots are the slots a section's companion view (opens) may be
// in: those a view opens into.
var opensSlots = []string{SlotProjectTab, SlotDashboardCard, SlotFull}

// Limits of a mod's views.
const (
	// SidebarAPIMinor is the mod API minor the sidebar slots need, and
	// opens, a badge and a height on the right pane's sections.
	SidebarAPIMinor = 7
	// maxSidebarViews is how many sidebar sections one mod declares.
	maxSidebarViews = 2
	// MaxViewDescription bounds a view's description, in characters.
	MaxViewDescription = 80
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
	// Actions are the core actions its rows and buttons may bind that
	// need a grant of their own (modaction.Action.Consent: session.clear,
	// question.answer; mod API 1.3). The human still confirms each press.
	Actions []string `toml:"actions"`
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
	PermActions     = "actions"
)

// Perms are all the permission names, in the consent screen's order.
var Perms = []string{PermProjectRead, PermViews, PermNotify, PermInbox, PermTools, PermFiles, PermNet, PermCommands, PermActions}

// Requested lists the permissions the manifest asks for, in Perms order.
func (p Permissions) Requested() []string {
	asks := map[string]bool{
		PermProjectRead: p.ProjectRead, PermViews: p.Views, PermNotify: p.Notify, PermInbox: p.Inbox, PermTools: p.Tools,
		PermFiles: len(p.Files.Read)+len(p.Files.Write) > 0, PermNet: len(p.Net) > 0, PermCommands: len(p.Commands) > 0,
		PermActions: len(p.Actions) > 0,
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

// maxColumnWidth caps a mod column, in cells: the width the sidebar
// draws at most (view.MaxModColumnWidth).
const maxColumnWidth = view.MaxModColumnWidth

var (
	idRE    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	apiRE   = regexp.MustCompile(`^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})$`)
	whereRE = regexp.MustCompile(`^[a-z]+\.[a-z]+$`)
	// hostRE is a DNS name, optionally *.-prefixed; an IP address isn't one.
	hostRE = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	progRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	keyRE  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// maxIDLen caps a mod id; it names folders, config tables and namespaces.
const maxIDLen = 40

// reservedIDs are the older [mods] keys config.toml still reads as
// [agent_mod]'s (enabled, band, pane): a mod by one of these names
// couldn't have a [mods.<id>] table.
var reservedIDs = []string{"enabled", "band", "pane"}

// CheckID reports why id can't name a mod, nil if it can.
func CheckID(id string) error {
	switch {
	case id == "":
		return errors.New("has no id")
	case len(id) > maxIDLen:
		return fmt.Errorf("id %q is longer than %d characters", id, maxIDLen)
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
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, fmt.Errorf("%s doesn't parse: %w", ManifestFile, err)
	}
	if err := errors.Join(loosens(md), m.check(dir)); err != nil {
		return &m, err
	}
	return &m, nil
}

// loosens reports each key of a manifest that reaches for a safety
// setting (SafetyKeys) and each key of a guard rule it doesn't have: a
// mod only tightens (mods design §7, Tighten only). Tools' input schemas
// are the agent's, not tm's settings: their names aren't checked.
func loosens(md toml.MetaData) error {
	var errs []error
	seen := map[string]bool{}
	for _, k := range md.Keys() {
		at := strings.Join(k, ".")
		switch {
		case len(k) > 1 && k[0] == "provides" && k[1] == "tools":
		case len(k) == 3 && k[0] == "provides" && k[1] == "guard" && !slices.Contains(guardRuleKeys, k[2]):
			errs = append(errs, fmt.Errorf("%s: a guard rule only refuses: it has %s", at, strings.Join(guardRuleKeys, ", ")))
		default:
			for i, e := range k {
				if i == 1 && k[0] == "provides" && e == "guard" || !slices.Contains(SafetyKeys, e) {
					continue
				}
				if at := strings.Join(k[:i+1], "."); !seen[at] {
					seen[at] = true
					errs = append(errs, fmt.Errorf("%s: a mod can't set %s: safety settings are the human's, in config.toml, and a mod only tightens", at, e))
				}
				break
			}
		}
	}
	return errors.Join(errs...)
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
	return errors.Join(slices.Concat(
		m.checkMeta(),
		m.checkFiles(dir),
		m.checkColumns(),
		m.checkViews(),
		m.checkGuard(),
		m.checkTools(),
		m.checkCommands(),
		m.checkProvided(),
		m.checkPermissions(),
		m.checkSettings(),
		m.checkDefaults(),
	)...)
}

// problems collects a check's errors.
type problems []error

func (ps *problems) add(format string, a ...any) { *ps = append(*ps, fmt.Errorf(format, a...)) }

// An idForm is the form the ids of a provides list take: re, said as says.
type idForm struct {
	re   *regexp.Regexp
	says string
}

var (
	dashIDs  = idForm{idRE, "lowercase letters, digits and dashes"}
	underIDs = idForm{keyRE, "lowercase letters, digits and underscores, starting with a letter"}
)

// checkIDs checks the ids of the entries of the provides list list:
// each in form, and listed once. needs, when not empty, is why an
// entry needs provides.main, reported in place of its id's problems in
// a data mod; also, when not nil, says what else is wrong with an id
// in form ("" for nothing), reported before a second listing. Entry i
// is entryAt(list, i).
func (m *Manifest) checkIDs(list, needs string, ids []string, form idForm, also func(id string) string) []error {
	var ps problems
	seen := map[string]bool{}
	for i, id := range ids {
		at := entryAt(list, i)
		var more string
		if also != nil && form.re.MatchString(id) {
			more = also(id)
		}
		switch {
		case needs != "" && !m.IsCode():
			ps.add("%s: %s", at, needs)
		case !form.re.MatchString(id):
			ps.add("%s: id %q must be %s", at, id, form.says)
		case more != "":
			ps.add("%s: %s", at, more)
		case seen[id]:
			ps.add("%s: id %q is listed twice", at, id)
		}
		seen[id] = true
	}
	return ps
}

// checkRoles checks the roles a guard rule or a tool is for.
func checkRoles(at string, roles []string) []error {
	var ps problems
	for _, r := range roles {
		if r != RoleThread && r != RoleCoordinator {
			ps.add("%s: role %q must be %s or %s", at, r, RoleThread, RoleCoordinator)
		}
	}
	return ps
}

// entryAt names entry i of the provides list list.
func entryAt(list string, i int) string { return fmt.Sprintf("provides.%s[%d]", list, i) }

// idsOf are the ids of a provides list's entries.
func idsOf[E any](es []E, id func(E) string) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = id(e)
	}
	return out
}

// checkMeta checks who the mod is: id, name, version, api, min_tm and
// homepage.
func (m *Manifest) checkMeta() []error {
	var ps problems
	if err := CheckID(m.ID); err != nil {
		ps = append(ps, err)
	}
	if strings.TrimSpace(m.Name) == "" {
		ps.add("has no name")
	}
	if _, ok := update.Newer(m.Version, m.Version); !ok || strings.HasPrefix(m.Version, "v") {
		ps.add("version %q isn't MAJOR.MINOR.PATCH", m.Version)
	}
	if !apiRE.MatchString(m.API) {
		ps.add("api %q isn't MAJOR.MINOR", m.API)
	}
	if m.MinTM != "" {
		if _, ok := update.Newer(m.MinTM, m.MinTM); !ok {
			ps.add("min_tm %q isn't a tm version", m.MinTM)
		}
	}
	if m.Homepage != "" && !strings.HasPrefix(m.Homepage, "https://") && !strings.HasPrefix(m.Homepage, "http://") {
		ps.add("homepage %q isn't a web address", m.Homepage)
	}
	return ps
}

// checkFiles checks the files and folders provides names, in dir.
func (m *Manifest) checkFiles(dir string) []error {
	var ps problems
	p := m.Provides
	if p.Main != "" {
		if ext := path.Ext(p.Main); ext != ".ts" && ext != ".js" {
			ps.add("provides.main %q isn't a .ts or .js file", p.Main)
		}
		ps = append(ps, inside(dir, "provides.main", p.Main, false))
	}
	for k, f := range map[string]string{"theme": p.Theme, "icons": p.Icons, "keymap": p.Keymap, "layout": p.Layout} {
		if f != "" {
			ps = append(ps, inside(dir, "provides."+k, f, false))
		}
	}
	for _, role := range sortedKeys(p.Rules) {
		if !slices.Contains(rulesKeys, role) {
			ps.add("provides.rules.%s: rules are coordinator or thread (addenda), coordinator_policy or thread_policy", role)
		}
		ps = append(ps, inside(dir, "provides.rules."+role, p.Rules[role], false))
	}
	for k, d := range map[string]string{"templates": p.Templates, "agents": p.Agents} {
		if d != "" {
			ps = append(ps, inside(dir, "provides."+k, d, true))
		}
	}
	return ps
}

// checkColumns checks provides.columns.
func (m *Manifest) checkColumns() []error {
	cs := m.Provides.Columns
	ps := problems(m.checkIDs("columns", "a column needs provides.main, the code that fills it", idsOf(cs, func(c Column) string { return c.ID }), dashIDs, nil))
	for i, c := range cs {
		at := entryAt("columns", i)
		if c.Width < 1 || c.Width > maxColumnWidth {
			ps.add("%s: width must be 1 to %d cells", at, maxColumnWidth)
		}
		if len(c.Where) == 0 {
			ps.add("%s: where names no slot", at)
		}
		for _, w := range c.Where {
			if !whereRE.MatchString(w) {
				ps.add("%s: where %q isn't a slot like sidebar.thread", at, w)
			}
		}
	}
	return ps
}

// checkViews checks provides.views.
func (m *Manifest) checkViews() []error {
	vs := m.Provides.Views
	ps := problems(m.checkIDs("views", "a view needs provides.main, the code that draws it", idsOf(vs, func(v View) string { return v.ID }), dashIDs, nil))
	for i, v := range vs {
		at := entryAt("views", i)
		if strings.TrimSpace(v.Title) == "" {
			ps.add("%s: has no title", at)
		}
		if !slices.Contains(Slots, v.Slot) {
			ps.add("%s: slot %q must be one of %s", at, v.Slot, strings.Join(Slots, ", "))
		}
		if maj, min, ok := splitAPI(m.API); IsSidebar(v.Slot) && ok && maj == 1 && min < SidebarAPIMinor {
			ps.add(`%s: slot %s needs api = "1.%d"`, at, v.Slot, SidebarAPIMinor)
		}
		if len([]rune(strings.TrimSpace(v.Description))) > MaxViewDescription {
			ps.add("%s: description is longer than %d characters", at, MaxViewDescription)
		}
		if v.Opens != "" {
			i := slices.IndexFunc(vs, func(o View) bool { return o.ID == v.Opens })
			switch {
			case !IsSection(v.Slot):
				ps.add("%s: opens is a section's: only a %s, %s, %s or %s opens a view", at, SlotSidebarSection, SlotProjectSection, SlotCoordinatorSection, SlotDetailsSection)
			case m.Before(SidebarAPIMinor):
				ps.add(`%s: opens needs api = "1.%d"`, at, SidebarAPIMinor)
			case i < 0:
				ps.add("%s: opens %q, which isn't one of its views", at, v.Opens)
			case !slices.Contains(opensSlots, vs[i].Slot):
				ps.add("%s: opens %q, a %s: it must be a %s", at, v.Opens, vs[i].Slot, strings.Join(opensSlots, ", "))
			}
		}
	}
	if n := len(slices.DeleteFunc(slices.Clone(vs), func(v View) bool { return !IsSidebar(v.Slot) })); n > maxSidebarViews {
		ps.add("provides.views: at most %d sidebar sections (%s, %s), not %d", maxSidebarViews, SlotSidebarSection, SlotProjectSection, n)
	}
	return ps
}

// checkGuard checks provides.guard.
func (m *Manifest) checkGuard() []error {
	gs := m.Provides.Guard
	var ps problems
	if len(gs) > maxGuardRules {
		ps.add("provides.guard: at most %d rules", maxGuardRules)
	}
	ps = append(ps, m.checkIDs("guard", "", idsOf(gs, func(g GuardRule) string { return g.ID }), dashIDs, func(id string) string {
		if slices.Contains(config.GuardRules, id) {
			return fmt.Sprintf("id %q is a core rule's: a mod adds rules, it doesn't change the core's", id)
		}
		return ""
	})...)
	for i, g := range gs {
		at := entryAt("guard", i)
		if len(g.Commands)+len(g.Writes)+len(g.Reads) == 0 {
			ps.add("%s: refuses nothing: list commands, writes or reads", at)
		}
		if len(g.Commands) > maxGuardPatterns || len(g.Writes) > maxGuardPatterns || len(g.Reads) > maxGuardPatterns {
			ps.add("%s: at most %d commands, writes and reads each", at, maxGuardPatterns)
		}
		for _, c := range g.Commands {
			if w := strings.Fields(c); len(w) == 0 || !progRE.MatchString(w[0]) {
				ps.add("%s: command %q must start with a program's name (no path, no *)", at, c)
			}
		}
		for _, r := range slices.Concat(g.Writes, g.Reads) {
			if !strings.HasPrefix(r, "~/") && !filepath.IsAbs(r) && !path.IsAbs(r) {
				ps.add("%s: %q must be absolute or start with ~/", at, r)
			} else if slices.Contains(strings.Split(filepath.ToSlash(r), "/"), "..") {
				ps.add("%s: %q has a .. in it", at, r)
			}
		}
		ps = append(ps, checkRoles(at, g.Roles)...)
		if msg := strings.TrimSpace(g.Message); msg == "" {
			ps.add("%s: has no message: say why, and what to do instead", at)
		} else if len(msg) > maxGuardMessageLen {
			ps.add("%s: message is longer than %d bytes", at, maxGuardMessageLen)
		}
	}
	return ps
}

// checkTools checks provides.tools.
func (m *Manifest) checkTools() []error {
	ts := m.Provides.Tools
	var ps problems
	if len(ts) > maxTools {
		ps.add("provides.tools: at most %d tools", maxTools)
	}
	ps = append(ps, m.checkIDs("tools", "a tool needs provides.main, the code that answers it", idsOf(ts, func(t Tool) string { return t.ID }), underIDs, func(id string) string {
		if n := ToolName(m.ID, id); len(n) > maxToolName {
			return fmt.Sprintf("%s is longer than %d characters", n, maxToolName)
		}
		return ""
	})...)
	for i, t := range ts {
		at := entryAt("tools", i)
		if d := strings.TrimSpace(t.Description); d == "" {
			ps.add("%s: has no description", at)
		} else if len(d) > maxToolDescLen {
			ps.add("%s: description is longer than %d bytes", at, maxToolDescLen)
		}
		ps = append(ps, checkRoles(at, t.Roles)...)
		for _, k := range sortedKeys(t.Input.Properties) {
			if !keyRE.MatchString(k) {
				ps.add("%s: input property %q must be lowercase letters, digits and underscores", at, k)
			}
			if typ, _ := t.Input.Properties[k]["type"].(string); !slices.Contains(jsonTypes, typ) {
				ps.add("%s: input property %q needs a type: %s", at, k, strings.Join(jsonTypes, ", "))
			}
		}
		for _, k := range t.Input.Required {
			if _, ok := t.Input.Properties[k]; !ok {
				ps.add("%s: required %q isn't an input property", at, k)
			}
		}
	}
	return ps
}

// checkCommands checks provides.commands.
func (m *Manifest) checkCommands() []error {
	cs := m.Provides.Commands
	var ps problems
	if len(cs) > maxCommands {
		ps.add("provides.commands: at most %d commands", maxCommands)
	}
	ps = append(ps, m.checkIDs("commands", "a command needs provides.main, the code that runs it", idsOf(cs, func(c Command) string { return c.ID }), dashIDs, nil)...)
	keys := map[string]string{}
	for i, c := range cs {
		at := entryAt("commands", i)
		if t := strings.TrimSpace(c.Title); t == "" {
			ps.add("%s: has no title", at)
		} else if len([]rune(t)) > maxCommandTitle {
			ps.add("%s: title is longer than %d characters", at, maxCommandTitle)
		}
		if c.Key != "" {
			if err := CheckCommandKey(c.Key); err != nil {
				ps.add("%s: key %v", at, err)
			} else if o, ok := keys[c.Key]; ok {
				ps.add("%s: key %s is %s's already", at, c.Key, o)
			}
			keys[c.Key] = c.ID
		}
	}
	return ps
}

// checkProvided checks what provides needs of permissions, and the
// code host.
func (m *Manifest) checkProvided() []error {
	var ps problems
	p, perm := m.Provides, m.Permissions
	if len(p.Tools) > 0 && !perm.Tools {
		ps.add("provides.tools needs permissions.tools")
	}
	if perm.Tools && len(p.Tools) == 0 {
		ps.add("permissions.tools offers no tool: list them in provides.tools")
	}
	if p.CodeHost != "" {
		switch {
		case !m.IsCode():
			ps.add("provides.code_host: a code host needs provides.main, the code that answers it")
		case !codehost.ModKind(p.CodeHost):
			ps.add("provides.code_host: %q must be lowercase letters, digits and dashes, and not github or azure (tm's own hosts)", p.CodeHost)
		}
		if len(perm.Commands) == 0 {
			ps.add("provides.code_host needs permissions.commands: the host's CLI it runs (glab)")
		}
	}
	if len(p.Views) > 0 && !perm.Views {
		ps.add("provides.views needs permissions.views")
	}
	return ps
}

// checkPermissions checks permissions.
func (m *Manifest) checkPermissions() []error {
	var ps problems
	perm := m.Permissions
	if !m.IsCode() && len(perm.Requested()) > 0 {
		ps.add("permissions are for code mods (provides.main); a data mod needs none")
	}
	for _, h := range perm.Net {
		if !hostRE.MatchString(h) {
			ps.add("permissions.net: %q isn't a host name (no scheme, port or path)", h)
		}
	}
	for _, c := range perm.Commands {
		if !progRE.MatchString(c) {
			ps.add("permissions.commands: %q isn't a program name (no path, no arguments)", c)
		}
	}
	for i, id := range perm.Actions {
		a, ok := modaction.Lookup(id)
		switch {
		case !ok:
			ps.add("permissions.actions: %q is not an action a mod may bind", id)
		case a.Consent == "":
			ps.add("permissions.actions: %s needs no grant of its own; list only %s", id, strings.Join(ConsentActions(), ", "))
		case slices.Contains(perm.Actions[:i], id):
			ps.add("permissions.actions: %s twice", id)
		}
	}
	if len(perm.Actions) > 0 && !perm.ProjectRead {
		ps.add("permissions.actions needs permissions.project_read: a binding is checked against the projects it acts in")
	}
	home := userHome()
	for _, r := range slices.Concat(perm.Files.Read, perm.Files.Write) {
		if !strings.HasPrefix(r, "~/") && !filepath.IsAbs(r) {
			ps.add("permissions.files: %q must be absolute or start with ~/", r)
		} else if slices.Contains(strings.Split(filepath.ToSlash(r), "/"), "..") {
			ps.add("permissions.files: %q has a .. in it", r)
		} else if holdsHome(r, home) {
			ps.add("permissions.files: %q is the home folder, holds it or is a disk's root; name the folder the mod needs", r)
		}
	}
	return ps
}

// checkSettings checks settings.
func (m *Manifest) checkSettings() []error {
	var ps problems
	for _, k := range sortedKeys(m.Settings) {
		s := m.Settings[k]
		at := "settings." + k
		if !keyRE.MatchString(k) {
			ps.add("%s: a setting's name is lowercase letters, digits and underscores", at)
		}
		if s.Type == SettingChoice && len(s.Options) == 0 {
			ps.add("%s: a choice needs options", at)
		}
		if s.Type != SettingInt && (s.Min != nil || s.Max != nil) {
			ps.add("%s: min and max are for an int", at)
		}
		if s.Min != nil && s.Max != nil && *s.Min > *s.Max {
			ps.add("%s: min is above max", at)
		}
		if s.Default == nil {
			ps.add("%s: has no default", at)
		} else if _, err := s.Check(s.Default); err != nil {
			ps.add("%s: default %v", at, err)
		} else if err := checkText(s.Default); err != nil {
			ps.add("%s: default %v", at, err)
		}
		if err := checkText(s.Label); err != nil {
			ps.add("%s: label %v", at, err)
		}
		for _, o := range s.Options {
			if err := checkText(o); err != nil {
				ps.add("%s: option %q %v", at, o, err)
			}
		}
	}
	return ps
}

// checkDefaults checks defaults: the config a mod suggests.
func (m *Manifest) checkDefaults() []error {
	var ps problems
	for _, k := range sortedKeys(m.Defaults) {
		at := "defaults." + k
		switch {
		case slices.Contains(SafetyKeys, k):
			// loosens says why.
		case !slices.Contains(DefaultKeys, k):
			ps.add("%s: a mod may suggest only %s", at, strings.Join(DefaultKeys, ", "))
		default:
			if err := config.CheckSetting(k, settingValue(m.Defaults[k])); err != nil {
				ps.add("%s: %v", at, err)
			}
		}
	}
	return ps
}

// settingValue is a value TOML decoded as config's setters take it: an
// int for an integer.
func settingValue(v any) any {
	if n, ok := v.(int64); ok {
		return int(n)
	}
	return v
}

// Suggestion is a default a mod suggests for all projects, beside what
// they follow now.
type Suggestion struct {
	Key string
	// Value is the mod's; Now is all projects' value today, config.toml's
	// [defaults] or tm's own.
	Value, Now any
	// Yours: config.toml's [defaults] sets it, so it stays as it is.
	Yours bool
}

// Applies says whether applying the suggestion changes config.toml: the
// human hasn't set the key and the value differs.
func (s Suggestion) Applies() bool { return !s.Yours && fmt.Sprint(s.Value) != fmt.Sprint(s.Now) }

// Suggestions are the defaults m suggests (DefaultKeys order), each
// beside c's all-projects value; a nil c is tm's own defaults.
func (m *Manifest) Suggestions(c *config.Config) []Suggestion {
	now, _ := c.AllProjects()
	yours := c.OwnDefaults()
	var out []Suggestion
	for _, k := range DefaultKeys {
		v, ok := m.Defaults[k]
		if !ok {
			continue
		}
		out = append(out, Suggestion{Key: k, Value: settingValue(v), Now: now.Value(k), Yours: slices.Contains(yours, k)})
	}
	return out
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

// ConsentActions are the ids of the actions a mod binds only with
// [permissions] actions listing them (modaction.Action.Consent).
func ConsentActions() []string {
	var out []string
	for _, a := range modaction.Actions {
		if a.Consent != "" {
			out = append(out, a.ID)
		}
	}
	return out
}

// Binds reports whether the mod may bind action a: one that needs no
// grant of its own, or one its manifest lists under [permissions]
// actions, with the actions grant.
func (m Mod) Binds(a modaction.Action) bool {
	if a.Consent == "" {
		return true
	}
	return m.Manifest != nil && m.Granted(PermActions) && slices.Contains(m.Manifest.Permissions.Actions, a.ID)
}
