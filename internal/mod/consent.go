package mod

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// What the human sees before a mod is installed, enabled or updated
// (mods design §7 and §9, Consent): what it changes in what agents run
// or read, each permission in plain words, every rules addendum, agent
// manifest, task template, agent tool and guard rule in full (text an
// agent obeys or reads, and commands tm runs), then a y/n question. A
// mod's own text is shown with control characters removed, so it can't
// drive the terminal, and its files inside a delimited block.

// Kinds names what the mod provides, for tm mod list and the Mods tab:
// code, column, view, tools, code host, commands, theme, icons, keymap,
// layout, rules, templates, agents, guard, defaults.
func (m *Manifest) Kinds() []string {
	p := m.Provides
	var out []string
	add := func(on bool, k string) {
		if on {
			out = append(out, k)
		}
	}
	add(p.Main != "", "code")
	add(len(p.Columns) > 0, "column")
	add(len(p.Views) > 0, "view")
	add(len(p.Tools) > 0, "tools")
	add(p.CodeHost != "", "code host")
	add(len(p.Commands) > 0, "commands")
	add(p.Theme != "", "theme")
	add(p.Icons != "", "icons")
	add(p.Keymap != "", "keymap")
	add(p.Layout != "", "layout")
	add(len(p.Rules) > 0, "rules")
	add(p.Templates != "", "templates")
	add(p.Agents != "", "agents")
	add(len(p.Guard) > 0, "guard")
	add(len(m.Defaults) > 0, "defaults")
	return out
}

// ViewWords are the panels m draws, in the consent screen's words: a
// project-popup tab "Review queue", a dashboard card "Cost", a
// details-panel section "Review" (on the dashboard and beside a thread's
// pane), a sidebar section "Ticker" (above or below your projects, on
// every screen).
func (m *Manifest) ViewWords() []string {
	var out []string
	for _, v := range m.Provides.Views {
		where := map[string]string{SlotProjectTab: "a project-popup tab", SlotDashboardCard: "a dashboard card", SlotFull: "a full view",
			SlotDetailsSection: "a details-panel section", SlotCoordinatorSection: "a coordinator-panel section",
			SlotSidebarSection: "a sidebar section", SlotProjectSection: "a section under each project"}[v.Slot]
		w := fmt.Sprintf("%s %q", where, Clean(v.Title, false))
		switch v.Slot {
		case SlotSidebarSection:
			w += " (above or below your projects, on every screen)"
		case SlotProjectSection:
			w += " (in the sidebar)"
		case SlotCoordinatorSection:
			w += " (beside the coordinator's pane)"
		case SlotDetailsSection:
			w += " (on the dashboard and beside a thread's pane)"
		}
		out = append(out, w)
	}
	return out
}

// CommandWords are m's commands as the consent lists them: "Relabel
// (g)", "Sync now".
func (m *Manifest) CommandWords() []string {
	var out []string
	for _, c := range m.Provides.Commands {
		w := strings.TrimSpace(Clean(c.Title, false))
		if c.Key != "" {
			w += " (" + c.Key + ")"
		}
		out = append(out, w)
	}
	return out
}

// PermWords is permission perm in the consent screen's words: "reads
// your projects", "talks to api.linear.app", "runs glab".
func (p Permissions) PermWords(perm string) string {
	switch perm {
	case PermProjectRead:
		return "reads your projects"
	case PermViews:
		return "draws its own panels"
	case PermNotify:
		return "shows notifications"
	case PermInbox:
		return "tells the coordinator"
	case PermTools:
		return "offers tools to agents"
	case PermNet:
		return "talks to " + strings.Join(p.Net, ", ")
	case PermCommands:
		return "runs " + strings.Join(p.Commands, ", ")
	case PermActions:
		var parts []string
		for _, id := range p.Actions {
			if a, ok := modaction.Lookup(id); ok && a.Consent != "" {
				parts = append(parts, a.Consent+" ("+id+")")
			}
		}
		return "offers you keys and buttons that " + strings.Join(parts, "; ") + "; tm asks you before each one runs"
	case PermFiles:
		var parts []string
		if len(p.Files.Read) > 0 {
			parts = append(parts, "reads "+strings.Join(p.Files.Read, ", "))
		}
		if len(p.Files.Write) > 0 {
			parts = append(parts, "writes "+strings.Join(p.Files.Write, ", "))
		}
		return Clean(strings.Join(parts, "; "), false)
	}
	return perm
}

// Words lists the permissions perms in the consent screen's words; none
// is "safe".
func (p Permissions) Words(perms []string) []string {
	if len(perms) == 0 {
		return []string{"safe"}
	}
	out := make([]string, len(perms))
	for i, n := range perms {
		out[i] = p.PermWords(n)
	}
	return out
}

// BuiltinAgents are the names of the agents tm has built in (claude,
// codex), set by the agent package: a mod's agent manifest of one of
// these names replaces it.
var BuiltinAgents []string

// AgentChange is something a mod changes in what agents run or read
// without a permission (its data): a rules file, an agent manifest,
// task templates. Short is for one line (tm mod list), Long for the
// consent screen.
type AgentChange struct {
	Short, Long string
}

// AgentChanges are what the installed mod changes in what agents run
// or read, each shown in full on its consent screen; none for a
// built-in one, which is tm's own.
func (m Mod) AgentChanges() []AgentChange {
	if m.Builtin || m.Manifest == nil {
		return nil
	}
	return m.Manifest.agentChanges(m.files())
}

func (m *Manifest) agentChanges(fsys fs.FS) []AgentChange {
	var out []AgentChange
	for _, role := range sortedKeys(m.Provides.Rules) {
		rel := Clean(m.Provides.Rules[role], false)
		if r, ok := strings.CutSuffix(role, "_policy"); ok {
			out = append(out, AgentChange{"replaces the " + r + " policy", "replaces tm's own " + r + " policy, the rules every " + r + " follows, with " + rel + " (below)"})
		} else {
			out = append(out, AgentChange{"adds " + role + " rules", "adds rules every " + role + " reads: " + rel + " (below)"})
		}
	}
	for _, rel := range m.agentFiles(fsys) {
		name := Clean(strings.TrimSuffix(path.Base(rel), ".toml"), false)
		if slices.Contains(BuiltinAgents, name) {
			out = append(out, AgentChange{"replaces how tm starts " + name, "replaces how tm starts every " + name + " session, in every project: the command, arguments and environment in " + Clean(rel, false) + " (below)"})
		} else {
			out = append(out, AgentChange{"adds the agent " + name, "adds the agent " + name + ", which tm starts, in any project, with the command in " + Clean(rel, false) + " (below)"})
		}
	}
	if m.Provides.Templates != "" {
		out = append(out, AgentChange{"adds task templates", "adds task templates: their notes and steps go into tasks agents read (below)"})
	}
	return out
}

// Clean is s with what could be read as other than text each a space
// (textsafe.Clean); newlines stay when lines.
func Clean(s string, lines bool) string { return textsafe.Clean(s, lines) }

// Headline is "Review age 0.3.1 (review-age) by Ada".
func (m *Manifest) Headline() string {
	s := fmt.Sprintf("%s %s (%s)", Clean(m.Name, false), m.Version, m.ID)
	if len(m.Authors) > 0 {
		s += " by " + Clean(strings.Join(m.Authors, ", "), false)
	}
	return s
}

// Consent is the consent screen for the mod m in folder dir, without
// the question: what it is and provides, what each permission it asks
// for lets it do, and every rules addendum and agent manifest in full.
func Consent(m *Manifest, dir string) string { return consent(m, os.DirFS(dir)) }

// Consent is the installed mod's consent screen (Consent), its files
// read from its folder, or from tm for a built-in one.
func (m Mod) Consent() string { return consent(m.Manifest, m.files()) }

// Shown is the installed mod's rules addenda and agent manifests in
// full (Manifest.Shown).
func (m Mod) Shown() string { return m.Manifest.shown(m.files()) }

// files are the mod's files: its folder's, or a built-in mod's.
func (m Mod) files() fs.FS {
	if m.FS != nil {
		return m.FS
	}
	return os.DirFS(m.Dir)
}

func consent(m *Manifest, fsys fs.FS) string {
	var b strings.Builder
	fmt.Fprintln(&b, m.Headline())
	if d := strings.TrimSpace(Clean(m.Description, false)); d != "" {
		fmt.Fprintln(&b, d)
	}
	if m.Homepage != "" {
		fmt.Fprintln(&b, Clean(m.Homepage, false))
	}
	fmt.Fprintf(&b, "Provides: %s\n", strings.Join(m.Kinds(), " · "))
	if v := m.ViewWords(); len(v) > 0 {
		fmt.Fprintf(&b, "Panels it draws: %s\n", strings.Join(v, " · "))
	}
	if k := m.Provides.CodeHost; k != "" {
		fmt.Fprintf(&b, "Code host %s: for projects whose code_host is %q, tm asks it about their pull requests and has it merge one tm thread merge checked (tm still checks the merge and anything it removes)\n", k, k)
	}
	if c := m.CommandWords(); len(c) > 0 {
		fmt.Fprintf(&b, "Commands you can run: %s\n", strings.Join(c, " · "))
	}
	changes := m.agentChanges(fsys)
	if len(changes) > 0 {
		fmt.Fprintln(&b, "What agents run or read that it changes:")
		for _, c := range changes {
			fmt.Fprintf(&b, "  - %s\n", c.Long)
		}
	}
	asked := m.Permissions.Requested()
	switch {
	case len(asked) == 0 && len(changes) > 0:
		fmt.Fprintln(&b, "Permissions: none")
	case len(asked) == 0:
		fmt.Fprintln(&b, "Permissions: none (safe)")
	default:
		fmt.Fprintln(&b, "Permissions it asks for:")
		for _, w := range m.Permissions.Words(asked) {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	if m.Permissions.Tools {
		fmt.Fprintln(&b, ToolsAgentModNote)
	}
	if d := m.DefaultWords(); len(d) > 0 {
		fmt.Fprintf(&b, "%s:\n", DefaultsNote)
		for _, w := range d {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	b.WriteString(m.shown(fsys))
	return b.String()
}

// ToolsAgentModNote is the consent's line for a mod that offers agents
// tools: Claude reaches a mod's tools through the Agent mod only, which
// is early access and off by default (T301 L1).
const ToolsAgentModNote = "Claude threads get its tools only while the Agent mod is on (early access, off by default: Settings, Agent mod, or [agent_mod] enabled = true)"

// DefaultsNote heads the defaults a mod suggests in its consent (mods
// design §7, Tighten only).
const DefaultsNote = "Defaults it suggests for all projects (nothing changes until you apply them, with d in the Mods tab or tm mod defaults; a setting your config.toml sets stays yours)"

// DefaultWords are the defaults m suggests as the consent lists them:
// auto_close = "days".
func (m *Manifest) DefaultWords() []string {
	var out []string
	for _, s := range m.Suggestions(nil) {
		out = append(out, s.Key+" = "+SettingWords(s.Value))
	}
	return out
}

// SettingWords is a setting's value as config.toml writes it: a string
// quoted, control characters removed.
func SettingWords(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", Clean(s, false))
	}
	return fmt.Sprint(v)
}

// Shown is every rules addendum, agent manifest, task template, agent
// tool and guard rule of m, in folder dir, in full, as the consent
// screen shows them; "" when it has none.
func (m *Manifest) Shown(dir string) string { return m.shown(os.DirFS(dir)) }

func (m *Manifest) shown(fsys fs.FS) string {
	var b strings.Builder
	for _, f := range m.agentText(fsys) {
		writeFile(&b, f)
	}
	return b.String()
}

// shownFile is a mod file the consent shows in full.
type shownFile struct {
	Title, Rel, Text string
}

// agentText lists the text of m that agents obey or read, or tm runs:
// its rules addenda by role, its agent manifests, its task templates,
// then its tools (the description and input an agent is given) and its
// guard rules (what each refuses, and the message an agent reads), each
// with its text.
func (m *Manifest) agentText(fsys fs.FS) []shownFile {
	var out []shownFile
	for _, role := range sortedKeys(m.Provides.Rules) {
		rel := m.Provides.Rules[role]
		out = append(out, shownFile{"Rules it adds for every " + role, rel, readText(fsys, rel)})
	}
	if a := path.Clean(m.Provides.Agents); m.Provides.Agents != "" && fs.ValidPath(a) {
		fs.WalkDir(fsys, a, func(rel string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			out = append(out, shownFile{"Agent manifest (tm runs what it says)", rel, readText(fsys, rel)})
			return nil
		})
	}
	if d := path.Clean(m.Provides.Templates); m.Provides.Templates != "" && fs.ValidPath(d) {
		files, _ := fs.Glob(fsys, path.Join(d, "*.md"))
		slices.Sort(files)
		for _, rel := range files {
			out = append(out, shownFile{"Task template (agents read its notes and steps)", rel, readText(fsys, rel)})
		}
	}
	for _, t := range m.Provides.Tools {
		out = append(out, shownFile{"Tool it offers your " + toolRoles(t) + " (they read its description)", ToolName(m.ID, t.ID), toolText(t)})
	}
	for _, g := range m.Provides.Guard {
		out = append(out, shownFile{"Guard rule it adds for your " + guardRoles(g) + " (it only refuses; they read its message)", "mod:" + m.ID + "." + g.ID, guardText(g)})
	}
	return out
}

// guardRoles is "threads", "coordinators" or "threads and coordinators".
func guardRoles(g GuardRule) string {
	return toolRoles(Tool{Roles: g.Roles})
}

// guardText is a guard rule as the consent shows it: what it refuses,
// then the message.
func guardText(g GuardRule) string {
	var b strings.Builder
	for _, l := range []struct {
		what string
		list []string
	}{{"Refuses the commands", g.Commands}, {"Refuses writes in", g.Writes}, {"Refuses reads of", g.Reads}} {
		if len(l.list) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", l.what, strings.Join(l.list, " · "))
		}
	}
	fmt.Fprintf(&b, "Message: %s\n", strings.TrimSpace(g.Message))
	return b.String()
}

// toolRoles is "threads", "coordinators" or "threads and coordinators".
func toolRoles(t Tool) string {
	switch {
	case !t.For(RoleCoordinator):
		return "threads"
	case !t.For(RoleThread):
		return "coordinators"
	}
	return "threads and coordinators"
}

// toolText is a tool as its agent is given it: the description, then
// each input property with its schema.
func toolText(t Tool) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(t.Description) + "\n")
	if len(t.Input.Properties) == 0 {
		b.WriteString("Input: none\n")
		return b.String()
	}
	b.WriteString("Input:\n")
	for _, k := range sortedKeys(t.Input.Properties) {
		spec, _ := json.Marshal(t.Input.Properties[k])
		req := ""
		if slices.Contains(t.Input.Required, k) {
			req = " (required)"
		}
		fmt.Fprintf(&b, "  %s%s: %s\n", k, req, spec)
	}
	return b.String()
}

func readText(fsys fs.FS, rel string) string {
	if rel = path.Clean(rel); !fs.ValidPath(rel) {
		return "(can't be read: " + rel + " isn't a path inside the mod)"
	}
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return "(can't be read: " + err.Error() + ")"
	}
	return string(data)
}

func writeFile(b *strings.Builder, f shownFile) {
	fmt.Fprintf(b, "%s, %s, in full:\n", f.Title, f.Rel)
	fmt.Fprintf(b, "----- begin %s -----\n", f.Rel)
	t := Clean(f.Text, true)
	b.WriteString(t)
	if !strings.HasSuffix(t, "\n") {
		b.WriteByte('\n')
	}
	fmt.Fprintf(b, "----- end %s -----\n", f.Rel)
}

// Changes is what an update changes that the human must see again
// before it is applied (mods design §8, Update).
type Changes struct {
	// NewPerms are the permissions the new version asks for that the
	// old one didn't, or asks for more of (another host, command or
	// root), in Perms order.
	NewPerms []string
	// NewAgentChanges are what it changes in what agents run or read
	// that the old version didn't (AgentChanges), in the consent's words.
	NewAgentChanges []string
	// Text are its rules addenda, agent manifests, task templates, tools
	// and guard rules that are new or changed, in full.
	Text []shownFile
}

// Asks reports whether the update needs the human's consent again.
func (c Changes) Asks() bool {
	return len(c.NewPerms) > 0 || len(c.NewAgentChanges) > 0 || len(c.Text) > 0
}

// Diff compares the installed mod (old, in oldDir) with its update (new,
// in newDir).
func Diff(old *Manifest, oldDir string, new *Manifest, newDir string) Changes {
	var c Changes
	had := map[string]string{}
	if old != nil {
		for _, p := range old.Permissions.Requested() {
			had[p] = old.Permissions.PermWords(p)
		}
	}
	for _, p := range new.Permissions.Requested() {
		if w, ok := had[p]; !ok || w != new.Permissions.PermWords(p) {
			c.NewPerms = append(c.NewPerms, p)
		}
	}
	var before []shownFile
	var hadChanges []AgentChange
	if old != nil {
		before = old.agentText(os.DirFS(oldDir))
		hadChanges = old.agentChanges(os.DirFS(oldDir))
	}
	for _, a := range new.agentChanges(os.DirFS(newDir)) {
		if !slices.Contains(hadChanges, a) {
			c.NewAgentChanges = append(c.NewAgentChanges, a.Long)
		}
	}
	for _, f := range new.agentText(os.DirFS(newDir)) {
		if !slices.ContainsFunc(before, func(o shownFile) bool { return o.Title == f.Title && o.Rel == f.Rel && o.Text == f.Text }) {
			c.Text = append(c.Text, f)
		}
	}
	return c
}

// Describe is the update's consent screen, without the question.
func (c Changes) Describe(m *Manifest) string {
	var b strings.Builder
	if len(c.NewPerms) > 0 {
		fmt.Fprintln(&b, "It now asks for:")
		for _, w := range m.Permissions.Words(c.NewPerms) {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	if len(c.NewAgentChanges) > 0 {
		fmt.Fprintln(&b, "It now also:")
		for _, w := range c.NewAgentChanges {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	for _, f := range c.Text {
		writeFile(&b, f)
	}
	return b.String()
}

// Reason is why the mod is invalid or incompatible, on one line.
func (m Mod) Reason() string {
	if m.Err == nil {
		return ""
	}
	line, _, _ := strings.Cut(m.Err.Error(), "\n")
	return Clean(line, false)
}

// Status is what m runs with, or why it doesn't run, in the consent
// screen's words: tm mod list's last column and the Mods tab's.
func (m Mod) Status() string {
	var s string
	switch m.State {
	case Invalid, Incompatible:
		return string(m.State) + ": " + m.Reason()
	case Disabled:
		s = "off"
	default:
		s = strings.Join(m.Manifest.Permissions.Words(m.Grants), " · ")
		if a := m.AgentChanges(); len(m.Grants) == 0 && len(a) > 0 {
			// Not "safe": it changes what agents run or read.
			s = "no permissions"
			for _, c := range a {
				s += " · " + c.Short
			}
		}
		if len(m.WantsMore) > 0 {
			s += " · wants more: " + strings.Join(m.Manifest.Permissions.Words(m.WantsMore), " · ")
		}
	}
	if m.Builtin {
		s = "built in · " + s
	}
	if len(m.Projects) > 0 {
		s += " · only " + strings.Join(m.Projects, ", ")
	}
	return s
}
