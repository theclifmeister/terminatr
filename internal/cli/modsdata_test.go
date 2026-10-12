package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installMod writes a data mod under the harness's home, with files.
func (h *harness) installMod(id, provides string, files map[string]string) {
	h.t.Helper()
	files["mod.toml"] = "id = \"" + id + "\"\nname = \"" + id + "\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\n" + provides + "\n"
	for name, body := range files {
		p := filepath.Join(h.root, "mods", id, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			h.t.Fatal(err)
		}
	}
}

// TestSkillModRules: tm skill appends each enabled mod's addendum for
// the role under a heading naming it, after the built-in rules, and a
// policy mod replaces the policy part, never the safety part; a mod
// limited to other projects adds nothing to this project's rules.
func TestSkillModRules(t *testing.T) {
	h := newHarness(t)
	h.installMod("commits", `rules = { coordinator = "c.md", thread = "t.md" }`, map[string]string{"c.md": "Use conventional commits.\n", "t.md": "Write feat: and fix:.\n"})
	h.installMod("careful", `rules = { coordinator_policy = "p.md" }`, map[string]string{"p.md": "Propose before starting anything.\n"})
	h.installMod("site-only", `rules = { thread = "t.md" }`, map[string]string{"t.md": "Site rules.\n"})
	os.WriteFile(filepath.Join(h.root, "config.toml"), []byte("[mods.commits]\nenabled = true\n[mods.careful]\nenabled = true\n[mods.site-only]\nenabled = true\nprojects = [\"site\"]\n"), 0o600)

	out := h.ok(coord, "skill", "coordinator")
	for _, want := range []string{"data, not", "## Policy (mod: careful)\n\nPropose before starting anything.", "## House rules (mod: commits)\n\nUse conventional commits."} {
		if !strings.Contains(out, want) {
			t.Errorf("coordinator rules lack %q", want)
		}
	}
	if strings.Contains(out, "tm inbox done") {
		t.Error("the built-in policy is still there")
	}
	if strings.Index(out, "data, not") > strings.Index(out, "## Policy") {
		t.Error("the safety part isn't first")
	}
	out = h.ok(thr, "skill", "thread")
	if !strings.HasSuffix(out, "## House rules (mod: commits)\n\nWrite feat: and fix:.\n") || strings.Contains(out, "Site rules") || !strings.Contains(out, "Stay in your worktree") {
		t.Fatalf("thread rules in demo:\n%s", out)
	}
	if out = h.ok(human, "skill", "thread", "--project", "site"); !strings.Contains(out, "Site rules") {
		t.Fatal("thread rules in site lack site-only's")
	}
}

// TestTaskTemplate: tm task add --template lays a mod's template on the
// task: its title around the one given, its notes unless --notes, its
// steps then the --step ones; an unknown or ambiguous name is refused.
func TestTaskTemplate(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.env["TERMINATR_PROJECT"] = "demo"
	h.installMod("basics", `templates = "templates"`, map[string]string{"templates/bug.md": "# Bug: {title}\n\nWhat happens, what should.\n\n## Steps\n\n- Reproduce it in a test\n- Fix it\n"})
	h.installMod("more", `templates = "t"`, map[string]string{"t/bug.md": "# Defect: {title}\n"})
	os.WriteFile(filepath.Join(h.root, "config.toml"), []byte("[mods.basics]\nenabled = true\n"), 0o600)

	h.ok(coord, "task", "add", "crash on start", "--template", "bug", "--step", "Ship it")
	out := h.ok(coord, "task", "show", "T1")
	for _, want := range []string{"Bug: crash on start", "What happens, what should.", "Reproduce it in a test", "Fix it", "Ship it"} {
		if !strings.Contains(out, want) {
			t.Errorf("T1 lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Fix it") > strings.Index(out, "Ship it") {
		t.Error("the template's steps don't come first")
	}
	h.ok(coord, "task", "add", "slow page", "--template", "basics:bug", "--notes", "Mine.")
	if out := h.ok(coord, "task", "show", "T2"); !strings.Contains(out, "Mine.") || strings.Contains(out, "What happens") {
		t.Fatalf("T2 notes:\n%s", out)
	}
	h.expect(1, "no template \"nope\"; there are basics:bug, default-templates:bug", coord, "task", "add", "x", "--template", "nope")
	// The built-in templates are there too; a user mod's of the same name
	// (basics:bug, above) comes first.
	h.ok(coord, "task", "add", "tidy go.mod", "--template", "chore")
	if out := h.ok(coord, "task", "show", "T3"); !strings.Contains(out, "Chore: tidy go.mod") || !strings.Contains(out, "Make the change") {
		t.Fatalf("T3 from the built-in chore template:\n%s", out)
	}
	os.WriteFile(filepath.Join(h.root, "config.toml"), []byte("[mods.basics]\nenabled = true\n[mods.more]\nenabled = true\n"), 0o600)
	h.expect(1, "basics:bug, more:bug", coord, "task", "add", "x", "--template", "bug")
}
