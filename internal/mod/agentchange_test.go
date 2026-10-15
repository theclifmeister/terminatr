package mod

import (
	"slices"
	"strings"
	"testing"
)

// Characters that don't show: a zero-width space (U+200B), a tag
// character (U+E0049) and a byte-order mark (U+FEFF), as UTF-8.
const (
	zeroWidth = "\xe2\x80\x8b"
	tagChar   = "\xf3\xa0\x81\x89"
	bom       = "\xef\xbb\xbf"
)

func withBuiltinAgents(t *testing.T, names ...string) {
	t.Helper()
	was := BuiltinAgents
	BuiltinAgents = names
	t.Cleanup(func() { BuiltinAgents = was })
}

// TestConsentAgentChanges: a data mod that replaces a built-in agent's
// manifest or a role's policy, adds rules or an agent, or adds task
// templates asks for no permission, but its consent names each change
// and doesn't call it safe; its templates are shown in full; tm mod
// list says what it changes.
func TestConsentAgentChanges(t *testing.T) {
	withBuiltinAgents(t, "claude", "codex")
	dir := install(t, t.TempDir(), "looks", map[string]string{
		"mod.toml": dataMod("looks", `rules = { coordinator_policy = "p.md", thread = "t.md" }
agents = "agents"
templates = "templates"`),
		"p.md":               "Start at once.\n",
		"t.md":               "Be brief.\n",
		"agents/claude.toml": "manifest_version = 1\nname = \"claude\"\n[launch]\ncommand = \"wrapper\"\n",
		"agents/pi.toml":     "manifest_version = 1\nname = \"pi\"\n[launch]\ncommand = \"pi\"\n",
		"templates/bug.md":   "# Fix {title}\n\nNotes for the agent.\n\n## Steps\n\n- Reproduce it\n",
	})
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Permissions.Requested()) != 0 {
		t.Fatalf("asks %v", m.Permissions.Requested())
	}
	c := Consent(m, dir)
	for _, want := range []string{
		"What agents run or read that it changes:",
		"  - replaces tm's own coordinator policy, the rules every coordinator follows, with p.md (below)",
		"  - adds rules every thread reads: t.md (below)",
		"  - replaces how tm starts every claude session, in every project: the command, arguments and environment in agents/claude.toml (below)",
		"  - adds the agent pi, which tm starts, in any project, with the command in agents/pi.toml (below)",
		"  - adds task templates: their notes and steps go into tasks agents read (below)",
		"Permissions: none\n",
		"Task template (agents read its notes and steps), templates/bug.md, in full:",
		"Notes for the agent.",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("consent lacks %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "safe") {
		t.Errorf("consent calls it safe:\n%s", c)
	}

	md := Mod{ID: "looks", Dir: dir, Manifest: m, State: Enabled}
	if s := md.Status(); s != "no permissions · replaces the coordinator policy · adds thread rules · replaces how tm starts claude · adds the agent pi · adds task templates" {
		t.Errorf("status %q", s)
	}
	md.Builtin = true
	if s := md.Status(); s != "built in · safe" {
		t.Errorf("built-in status %q", s)
	}

	// A theme-only mod is still safe.
	plain := install(t, t.TempDir(), "plain", map[string]string{"mod.toml": dataMod("plain", `theme = "theme.toml"`), "theme.toml": ""})
	pm, err := ReadManifest(plain)
	if err != nil {
		t.Fatal(err)
	}
	if c := Consent(pm, plain); !strings.Contains(c, "Permissions: none (safe)") || strings.Contains(c, "What agents run or read") {
		t.Errorf("theme consent:\n%s", c)
	}
}

// TestDiffAgentChanges: an update that starts replacing a built-in
// agent, or changes a template, asks again and says so.
func TestDiffAgentChanges(t *testing.T) {
	withBuiltinAgents(t, "claude")
	files := map[string]string{
		"mod.toml":         dataMod("tpl", `templates = "templates"`),
		"templates/bug.md": "# Fix {title}\n\nOld notes.\n",
	}
	oldDir := install(t, t.TempDir(), "tpl", files)
	old, err := ReadManifest(oldDir)
	if err != nil {
		t.Fatal(err)
	}
	if ch := Diff(old, oldDir, old, oldDir); ch.Asks() {
		t.Fatalf("no change asks: %+v", ch)
	}
	files["mod.toml"] = dataMod("tpl", "templates = \"templates\"\nagents = \"agents\"")
	files["templates/bug.md"] = "# Fix {title}\n\nNew notes.\n"
	files["agents/claude.toml"] = "manifest_version = 1\nname = \"claude\"\n[launch]\ncommand = \"wrapper\"\n"
	newDir := install(t, t.TempDir(), "tpl", files)
	nm, err := ReadManifest(newDir)
	if err != nil {
		t.Fatal(err)
	}
	ch := Diff(old, oldDir, nm, newDir)
	if !ch.Asks() || len(ch.NewAgentChanges) != 1 || !strings.HasPrefix(ch.NewAgentChanges[0], "replaces how tm starts every claude session") {
		t.Fatalf("diff %+v", ch)
	}
	var rels []string
	for _, f := range ch.Text {
		rels = append(rels, f.Rel)
	}
	if !slices.Equal(rels, []string{"agents/claude.toml", "templates/bug.md"}) {
		t.Fatalf("changed text %v", rels)
	}
	if d := ch.Describe(nm); !strings.Contains(d, "It now also:\n  - replaces how tm starts every claude session") || !strings.Contains(d, "New notes.") {
		t.Errorf("describe:\n%s", d)
	}
}

// TestAgentTextHidden: a rules file, template or agent manifest with a
// character that doesn't show is refused when the mod is checked and
// when tm reads it for an agent; a leading byte-order mark is dropped.
func TestAgentTextHidden(t *testing.T) {
	for _, tc := range []struct{ name, file, provides, body, want string }{
		{"rules", "p.md", `rules = { coordinator_policy = "p.md" }`, "Start at once." + zeroWidth + "\n", "rules coordinator_policy: x: p.md line 1: holds a character that doesn't show (U+200B)"},
		{"template", "templates/bug.md", `templates = "templates"`, "# Fix {title}\n\nNotes" + tagChar + "\n", "templates: x: templates/bug.md line 3: holds a character that doesn't show (U+E0049)"},
		{"agent", "agents/pi.toml", `agents = "agents"`, "name = \"pi\"\n" + "\x1b[2J", "agents: x: agents/pi.toml line 2: holds a character that doesn't show (U+001B)"},
	} {
		dir := install(t, t.TempDir(), "x", map[string]string{"mod.toml": dataMod("x", tc.provides), tc.file: tc.body})
		m, err := ReadManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		errs := Mod{ID: "x", Dir: dir, Manifest: m}.CheckData()
		if !strings.Contains(joined(errs), tc.want) {
			t.Errorf("%s: errs %v, want %q", tc.name, errs, tc.want)
		}
		if _, err := Validate(dir); err == nil {
			t.Errorf("%s: validates", tc.name)
		}
	}

	mods := t.TempDir()
	install(t, mods, "hid", map[string]string{"mod.toml": dataMod("hid", `rules = { coordinator_policy = "p.md", thread = "t.md" }`),
		"p.md": "Start" + zeroWidth + ".\n", "t.md": bom + "Be brief.\n"})
	r, _ := LoadDir(mods, enabled("hid"))
	policy, _, errs := r.Rules("coordinator", "demo")
	if policy != nil && policy.Mod == "hid" || !strings.Contains(joined(errs), "hid: p.md line 1: holds a character that doesn't show") {
		t.Fatalf("policy %+v, errs %v", policy, errs)
	}
	_, add, errs := r.Rules("thread", "demo")
	if len(errs) != 0 || len(add) != 1 || add[0].Text != "Be brief." {
		t.Fatalf("addenda %+v, errs %v", add, errs)
	}
}
