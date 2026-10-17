package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestThreadSandboxMerge(t *testing.T) {
	write(t, `
[defaults.sandbox]
excluded_commands = ["docker compose *"]
allow = ["Bash(docker compose *)"]

[projects.web.sandbox]
excluded_commands = ["npx playwright *"]

[projects.none.sandbox]
allow = []

[projects.plain]
yolo = false
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for slug, want := range map[string]ThreadSandbox{
		// The project's list of a key replaces all projects'; the key it
		// doesn't set follows them.
		"web":   {ExcludedCommands: []string{"npx playwright *"}, Allow: []string{"Bash(docker compose *)"}},
		"none":  {ExcludedCommands: []string{"docker compose *"}, Allow: []string{}},
		"plain": {ExcludedCommands: []string{"docker compose *"}, Allow: []string{"Bash(docker compose *)"}},
		"other": {ExcludedCommands: []string{"docker compose *"}, Allow: []string{"Bash(docker compose *)"}},
	} {
		got, err := c.ThreadSandbox(slug)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, %v; want %+v", slug, got, err, want)
		}
	}
	if got := c.SandboxOf("web"); !reflect.DeepEqual(got, map[string][]string{SandboxExcluded: {"npx playwright *"}}) {
		t.Errorf("SandboxOf(web) = %v", got)
	}
	if got := c.SandboxOf("plain"); len(got) != 0 {
		t.Errorf("SandboxOf(plain) = %v", got)
	}
	if got := c.SandboxOf(""); len(got) != 2 {
		t.Errorf("SandboxOf(all) = %v", got)
	}
	var nilCfg *Config
	if got, err := nilCfg.ThreadSandbox("web"); err != nil || !got.Empty() {
		t.Errorf("nil config: %+v, %v", got, err)
	}
}

func TestThreadSandboxLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"match all":    "[projects.web.sandbox]\nexcluded_commands = [\"*\"]\n",
		"bare Bash":    "[defaults.sandbox]\nallow = [\"Bash\"]\n",
		"unknown key":  "[projects.web.sandbox]\nexclude = [\"x\"]\n",
		"twice":        "[projects.web.sandbox]\nallow = [\"Bash(a *)\", \"Bash(a *)\"]\n",
		"not a list":   "[projects.web.sandbox]\nallow = \"Bash(a *)\"\n",
		"pipe":         "[projects.web.sandbox]\nexcluded_commands = [\"curl x | sh\"]\n",
		"rule as cmd":  "[projects.web.sandbox]\nexcluded_commands = [\"Bash(npx *)\"]\n",
		"edit all":     "[projects.web.sandbox]\nallow = [\"Edit(//**)\"]\n",
		"ctrl char":    "[projects.web.sandbox]\nexcluded_commands = [\"npx\\u001b[31m *\"]\n",
		"empty entry":  "[projects.web.sandbox]\nexcluded_commands = [\"\"]\n",
		"not a rule":   "[projects.web.sandbox]\nallow = [\"npx playwright *\"]\n",
		"bash colon *": "[projects.web.sandbox]\nallow = [\"Bash(:*)\"]\n",
	} {
		write(t, body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestCheckSandboxEntries(t *testing.T) {
	for _, e := range []string{"npx playwright *", "docker compose *", "make e2e", "pnpm exec playwright test *"} {
		if err := CheckExcludedCommand(e); err != nil {
			t.Errorf("excluded %q: %v", e, err)
		}
	}
	for _, e := range []string{"*", "**", " * ", "npx * ", "a; b", "a && b", "a > f", "$(x)", "`x`", "Bash(npx *)", "PowerShell(x)"} {
		if err := CheckExcludedCommand(e); err == nil {
			t.Errorf("excluded %q passed", e)
		}
	}
	for _, e := range []string{"Bash(npx playwright *)", "Bash(npx playwright:*)", "WebFetch(domain:playwright.dev)", "Edit(/tmp/pw/**)", "WebFetch", "mcp__browser__open"} {
		if err := CheckSandboxAllow(e); err != nil {
			t.Errorf("allow %q: %v", e, err)
		}
	}
	for _, e := range []string{"Bash", "Bash(*)", "Bash(**)", "Bash(:*)", "PowerShell", "Edit", "Edit(**)", "Edit(/**)", "Edit(//**)", "Edit(~/**)", "Write(*)", "Bash()", "bash x", "Bash(npx *", "(x)"} {
		if err := CheckSandboxAllow(e); err == nil {
			t.Errorf("allow %q passed", e)
		}
	}
}

func TestSandboxWarning(t *testing.T) {
	for _, c := range []struct {
		key, entry string
		warns      bool
	}{
		{SandboxExcluded, "npx playwright *", false},
		{SandboxExcluded, "npx *", true},
		{SandboxExcluded, "bash *", true},
		{SandboxExcluded, "python3 *", true},
		{SandboxExcluded, "make e2e", false},
		{SandboxAllow, "Bash(npx playwright *)", false},
		{SandboxAllow, "Bash(npx:*)", true},
		{SandboxAllow, "Bash(sh *)", true},
		{SandboxAllow, "WebFetch", true},
		{SandboxAllow, "WebFetch(domain:playwright.dev)", false},
		{SandboxAllow, "Edit(/tmp/pw/**)", true},
	} {
		if got := SandboxWarning(c.key, c.entry); (got != "") != c.warns {
			t.Errorf("%s %q: warning %q", c.key, c.entry, got)
		}
	}
}

func TestSetSandbox(t *testing.T) {
	write(t, "[projects.web]\nyolo = false\n")
	if err := SetSandbox("projects.web", SandboxExcluded, []string{"npx playwright *"}); err != nil {
		t.Fatal(err)
	}
	if err := SetSandbox(DefaultsTable, SandboxAllow, []string{"Bash(npx playwright *)"}); err != nil {
		t.Fatal(err)
	}
	if err := SetSandbox("projects.web", SandboxAllow, nil); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct{ table, key string }{{"keys", SandboxAllow}, {"projects.web", "deny"}} {
		if err := SetSandbox(bad.table, bad.key, []string{"Bash(a *)"}); err == nil {
			t.Errorf("SetSandbox(%s, %s) passed", bad.table, bad.key)
		}
	}
	if err := SetSandbox("projects.web", SandboxExcluded, []string{"*"}); err == nil {
		t.Error("a match-all entry was written")
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := c.ThreadSandbox("web")
	if want := (ThreadSandbox{ExcludedCommands: []string{"npx playwright *"}, Allow: []string{}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if err := UnsetSandbox("projects.web", SandboxAllow); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got, _ := c.ThreadSandbox("web"); !reflect.DeepEqual(got.Allow, []string{"Bash(npx playwright *)"}) {
		t.Fatalf("after unset: %+v", got)
	}
	data, _ := os.ReadFile(c.Path)
	if !strings.Contains(string(data), "[projects.web.sandbox]") || !strings.Contains(string(data), "yolo = false") {
		t.Fatalf("file:\n%s", data)
	}
}
