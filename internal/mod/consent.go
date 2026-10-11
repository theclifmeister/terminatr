package mod

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// What the human sees before a mod is installed, enabled or updated
// (mods design §7 and §9, Consent): each permission in plain words,
// every rules addendum and agent manifest in full (text an agent obeys
// and commands tm runs), then a y/n question. A mod's own text is shown
// with control characters removed, so it can't drive the terminal, and
// its files inside a delimited block.

// Kinds names what the mod provides, for tm mod list and the Mods tab:
// code, column, view, theme, icons, keymap, layout, rules, templates,
// agents.
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
	add(p.Theme != "", "theme")
	add(p.Icons != "", "icons")
	add(p.Keymap != "", "keymap")
	add(p.Layout != "", "layout")
	add(len(p.Rules) > 0, "rules")
	add(p.Templates != "", "templates")
	add(p.Agents != "", "agents")
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
	case PermNet:
		return "talks to " + strings.Join(p.Net, ", ")
	case PermCommands:
		return "runs " + strings.Join(p.Commands, ", ")
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
func Consent(m *Manifest, dir string) string {
	var b strings.Builder
	fmt.Fprintln(&b, m.Headline())
	if d := strings.TrimSpace(Clean(m.Description, false)); d != "" {
		fmt.Fprintln(&b, d)
	}
	if m.Homepage != "" {
		fmt.Fprintln(&b, Clean(m.Homepage, false))
	}
	fmt.Fprintf(&b, "Provides: %s\n", strings.Join(m.Kinds(), " · "))
	asked := m.Permissions.Requested()
	if len(asked) == 0 {
		fmt.Fprintln(&b, "Permissions: none (safe)")
	} else {
		fmt.Fprintln(&b, "Permissions it asks for:")
		for _, w := range m.Permissions.Words(asked) {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	b.WriteString(m.Shown(dir))
	return b.String()
}

// Shown is every rules addendum and agent manifest of m, in folder dir,
// in full, as the consent screen shows them; "" when it has none.
func (m *Manifest) Shown(dir string) string {
	var b strings.Builder
	for _, f := range m.agentText(dir) {
		writeFile(&b, f)
	}
	return b.String()
}

// shownFile is a mod file the consent shows in full.
type shownFile struct {
	Title, Rel, Text string
}

// agentText lists the files of m that agents obey or tm runs: its rules
// addenda by role, then its agent manifests, each with its text.
func (m *Manifest) agentText(dir string) []shownFile {
	var out []shownFile
	for _, role := range sortedKeys(m.Provides.Rules) {
		rel := m.Provides.Rules[role]
		out = append(out, shownFile{"Rules it adds for every " + role, rel, readText(dir, rel)})
	}
	if a := m.Provides.Agents; a != "" {
		root := filepath.Join(dir, filepath.FromSlash(a))
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			rel = filepath.ToSlash(rel)
			out = append(out, shownFile{"Agent manifest (tm runs what it says)", rel, readText(dir, rel)})
			return nil
		})
	}
	return out
}

func readText(dir, rel string) string {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
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
		before = old.agentText(oldDir)
	}
	for _, f := range new.agentText(newDir) {
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
