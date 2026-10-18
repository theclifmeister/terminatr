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
	for _, e := range []string{"*", "**", " * ", "npx * ", "a; b", "a && b", "a > f", "$(x)", "`x`", "${X} x", "echo $X", "make a$X", "x ok $1", "x $@", "x $*", "x $$", "x $?", "x $# y", "ok $ $_a", "x a$-", "?", "x", "Bash(npx *)", "PowerShell(x)",
		// T560's security review: a program left open, a rule in another case.
		"*e*", "*-*", "* --version", "python* -c x", "bash(npx *)", "POWERSHELL(x)"} {
		if err := CheckExcludedCommand(e); err == nil {
			t.Errorf("excluded %q passed", e)
		}
	}
	for _, e := range []string{"Bash(npx playwright *)", "Bash(npx playwright:*)", "WebFetch(domain:playwright.dev)", "Edit(/tmp/pw/**)", "mcp__browser__open", "mcp__browser", "WebSearch", "Read(~/Library/Caches/ms-playwright/**)"} {
		if err := CheckSandboxAllow(e); err != nil {
			t.Errorf("allow %q: %v", e, err)
		}
	}
	for _, e := range []string{"Bash", "Bash(*)", "Bash(**)", "Bash(:*)", "PowerShell", "Edit", "Edit(**)", "Edit(/**)", "Edit(//**)", "Edit(~/**)", "Write(*)", "Bash()", "bash x", "Bash(npx *", "(x)",
		// T560's security review: inner wildcards, any case, target-less
		// rules of tools that take a target.
		"Bash(*e*)", "Bash(*-*)", "Bash(*:*)", "bash", "bash(*)", "BASH(*e*)", "edit(~/**)", "Read", "read(//**)", "WebFetch", "webfetch",
		"WebFetch(domain:*)", "Glob", "Grep(**)"} {
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
		{SandboxExcluded, "sh -c *", true},
		{SandboxExcluded, "bash -c *", true},
		{SandboxExcluded, "python3 -c *", true},
		{SandboxExcluded, "/bin/sh *", true},
		{SandboxExcluded, "env sh *", true},
		{SandboxExcluded, "env FOO=1 bash -c *", true},
		{SandboxExcluded, "timeout 10 node -e *", true},
		{SandboxExcluded, "/usr/bin/python3.12 -m *", true},
		{SandboxExcluded, "pnpm exec playwright test *", false},
		{SandboxExcluded, "sh scripts/e2e.sh", false},
		{SandboxExcluded, "make e2e", false},
		{SandboxAllow, "Bash(npx playwright *)", false},
		{SandboxAllow, "Bash(npx:*)", true},
		{SandboxAllow, "Bash(sh *)", true},
		{SandboxAllow, "WebFetch", true},
		{SandboxAllow, "WebFetch(domain:playwright.dev)", false},
		{SandboxAllow, "Edit(/tmp/pw/**)", true},
		{SandboxAllow, "Bash(sh -c *)", true},
		{SandboxAllow, "bash(env sh:*)", true},
		{SandboxAllow, "mcp__browser", true},
		{SandboxAllow, "mcp__browser__open", false},
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

// TestEditWarning: every Edit rule warns; plainer high up a tree or
// where files start programs.
func TestEditWarning(t *testing.T) {
	for path, want := range map[string]string{
		"/tmp/pw/**":                     "",
		"//Users/me/Work/repo/out/**":    "",
		"~/Work/**":                      "whole tree high up",
		"//Users/me/**":                  "whole tree high up",
		"~/*/repo/**":                    "whole tree high up",
		"~/.zshrc":                       "run code as you outside the sandbox",
		"//Users/me/Work/r/.git/hooks/*": "run code as you outside the sandbox",
		"~/.claude/settings.json":        "run code as you outside the sandbox",
		"//Users/me/Work/r/../../**":     "climbs out",
	} {
		got := SandboxWarning(SandboxAllow, "Edit("+path+")")
		if !strings.Contains(got, "without asking") || !strings.Contains(got, want) {
			t.Errorf("Edit(%s): %q, want %q", path, got, want)
		}
	}
}

// TestSandboxCovered: an excluded command with an allow rule that takes
// it runs outside the sandbox without asking.
func TestSandboxCovered(t *testing.T) {
	allow := []string{"WebFetch(domain:x.dev)", "Bash(npx playwright:*)", "Bash(make e2e)"}
	for e, want := range map[string]string{
		"npx playwright *":      "Bash(npx playwright:*)",
		"npx playwright test *": "Bash(npx playwright:*)",
		"npx playwright":        "Bash(npx playwright:*)",
		"make e2e":              "Bash(make e2e)",
		"make e2e *":            "",
		"docker compose *":      "",
	} {
		if got := SandboxCovered(e, allow); got != want {
			t.Errorf("%q: covered by %q, want %q", e, got, want)
		}
	}
}

// TestSandboxPresets: a project's switch replaces all projects'; the
// switches it doesn't set follow them; SetSandboxPreset writes and
// removes one.
func TestSandboxPresets(t *testing.T) {
	write(t, `
[defaults.sandbox]
browser_testing = true

[projects.web.sandbox]
browser_testing = false
local_dev_servers = true
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := c.ThreadSandbox("web"); !reflect.DeepEqual(got.Presets, []string{SandboxDevServers}) {
		t.Errorf("web: %+v", got)
	}
	if got, _ := c.ThreadSandbox("other"); !reflect.DeepEqual(got.Presets, []string{SandboxBrowser}) || got.Empty() {
		t.Errorf("other: %+v", got)
	}
	if got := c.SandboxPresetsOf("web"); !reflect.DeepEqual(got, map[string]bool{SandboxBrowser: false, SandboxDevServers: true}) {
		t.Errorf("SandboxPresetsOf(web) = %v", got)
	}
	on := true
	if err := SetSandboxPreset("projects.app", SandboxBrowser, &on); err != nil {
		t.Fatal(err)
	}
	if err := SetSandboxPreset("projects.web", SandboxBrowser, nil); err != nil {
		t.Fatal(err)
	}
	if err := SetSandboxPreset("keys", SandboxBrowser, &on); err == nil {
		t.Error("a preset outside a project or all projects was written")
	}
	if err := SetSandboxPreset(DefaultsTable, "mach_register", &on); err == nil {
		t.Error("an unknown preset was written")
	}
	c, _ = Load()
	if got := c.SandboxPresetsOf("app"); !reflect.DeepEqual(got, map[string]bool{SandboxBrowser: true}) {
		t.Errorf("app: %v", got)
	}
	if got, _ := c.ThreadSandbox("web"); !reflect.DeepEqual(got.Presets, []string{SandboxBrowser, SandboxDevServers}) {
		t.Errorf("web after unset: %+v", got)
	}
}

// TestSandboxApplies: every key has its platforms; the presets are
// macOS's only (T561), the lists every platform's.
func TestSandboxApplies(t *testing.T) {
	for _, k := range append(append([]string{}, SandboxKeys...), SandboxPresets...) {
		if len(SandboxSettings[k].OS) == 0 {
			t.Errorf("%s has no platforms", k)
		}
	}
	for _, c := range []struct {
		key, goos string
		want      bool
	}{
		{SandboxBrowser, OSMac, true}, {SandboxBrowser, OSLinux, false}, {SandboxDownloads, OSMac, true}, {SandboxDownloads, OSWindows, false}, {SandboxDevServers, OSWindows, false},
		{SandboxExcluded, OSWindows, true}, {SandboxAllow, OSLinux, true}, {"nope", OSMac, false},
	} {
		if got := SandboxApplies(c.key, c.goos); got != c.want {
			t.Errorf("%s on %s: %v", c.key, c.goos, got)
		}
	}
}

// TestSandboxProblems: each entry's reason, for the Access tab; a lone
// $ is plain text.
func TestSandboxProblems(t *testing.T) {
	got := SandboxProblems(SandboxExcluded, []string{"make e2e", "x $1", "make e2e", "*", "price $"})
	if got[0] != "" || got[4] != "" || !strings.Contains(got[1], "variable") || !strings.Contains(got[2], "twice") || !strings.Contains(got[3], "every command") {
		t.Errorf("%q", got)
	}
}
