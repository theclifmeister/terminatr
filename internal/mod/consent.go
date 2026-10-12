package mod

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/theclifmeister/terminatr/internal/modaction"
)

// What the human sees before a mod is installed, enabled or updated
// (mods design §7 and §9, Consent): each permission in plain words,
// every rules addendum, agent manifest, agent tool and guard rule in
// full (text an agent obeys or reads, and commands tm runs), then a y/n
// question. A mod's own text is shown with control characters removed,
// so it can't drive the terminal, and its files inside a delimited
// block.

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
// details-panel section "Review".
func (m *Manifest) ViewWords() []string {
	var out []string
	for _, v := range m.Provides.Views {
		where := map[string]string{SlotProjectTab: "a project-popup tab", SlotDashboardCard: "a dashboard card", SlotFull: "a full view",
			SlotDetailsSection: "a details-panel section", SlotCoordinatorSection: "a coordinator-panel section"}[v.Slot]
		out = append(out, fmt.Sprintf("%s %q", where, Clean(v.Title, false)))
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

// Clean is s without control characters (escape sequences, carriage
// returns), each a space; newlines and tabs stay when lines.
func Clean(s string, lines bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case lines && (r == '\n' || r == '\t'):
			return r
		case unicode.IsControl(r) || r == ' ' || r == ' ' || unicode.Is(unicode.Bidi_Control, r):
			return ' '
		}
		return r
	}, s)
}

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
	asked := m.Permissions.Requested()
	if len(asked) == 0 {
		fmt.Fprintln(&b, "Permissions: none (safe)")
	} else {
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

// Shown is every rules addendum, agent manifest, agent tool and guard
// rule of m, in folder dir, in full, as the consent screen shows them;
// "" when it has none.
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
// its rules addenda by role, its agent manifests, then its tools (the
// description and input an agent is given) and its guard rules (what
// each refuses, and the message an agent reads), each with its text.
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
	// Text are its rules addenda and agent manifests that are new or
	// changed, in full.
	Text []shownFile
}

// Asks reports whether the update needs the human's consent again.
func (c Changes) Asks() bool { return len(c.NewPerms) > 0 || len(c.Text) > 0 }

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
	if old != nil {
		before = old.agentText(os.DirFS(oldDir))
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
