package config

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

func TestThreadSandboxMerge(t *testing.T) {
	write(t, accessMarker+`
[defaults.sandbox]
excluded_commands = ["docker compose *"]
allow = ["Bash(docker compose *)"]
hosts = ["registry.npmjs.org"]

[projects.web.sandbox]
excluded_commands = ["npx playwright *", "docker compose *"]
local_ports = true

[projects.none.sandbox]
drop = ["Bash(docker compose *)", "registry.npmjs.org", "nothing here"]

[projects.empty.sandbox]
allow = []
hosts = ["x.dev"]

[projects.plain]
yolo = false
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	all := func(key, v string) SandboxRule { return SandboxRule{Key: key, Value: v} }
	own := func(key, v string) SandboxRule { return SandboxRule{Key: key, Value: v, Own: true} }
	dropped := func(key, v string) SandboxRule { return SandboxRule{Key: key, Value: v, Dropped: true} }
	// all projects' Bash rule, of a file written before, covers their
	// excluded command: it runs without asking, as it did.
	noAsk := func(r SandboxRule) SandboxRule { r.NoAsk = true; return r }
	common := []SandboxRule{all(SandboxHosts, "registry.npmjs.org"), noAsk(all(SandboxExcluded, "docker compose *")), all(SandboxAllow, "Bash(docker compose *)")}
	for slug, want := range map[string]ThreadSandbox{
		// A project's lists add to all projects', an entry of theirs it
		// lists too given once.
		"web": {Rules: []SandboxRule{all(SandboxHosts, "registry.npmjs.org"), noAsk(all(SandboxExcluded, "docker compose *")), own(SandboxExcluded, "npx playwright *"), all(SandboxAllow, "Bash(docker compose *)")},
			LocalPorts: true, LocalPortsSet: true, LocalPortsOwn: true},
		// An empty list takes nothing away.
		"empty": {Rules: []SandboxRule{all(SandboxHosts, "registry.npmjs.org"), own(SandboxHosts, "x.dev"), noAsk(all(SandboxExcluded, "docker compose *")), all(SandboxAllow, "Bash(docker compose *)")}},
		// A drop takes one of all projects' entries away; one that
		// matches nothing stays listed.
		"none": {Rules: []SandboxRule{dropped(SandboxHosts, "registry.npmjs.org"), all(SandboxExcluded, "docker compose *"), dropped(SandboxAllow, "Bash(docker compose *)")},
			Drops: []string{"Bash(docker compose *)", "registry.npmjs.org", "nothing here"}},
		"plain": {Rules: common},
		"other": {Rules: common},
	} {
		if got := c.ThreadSandbox(slug); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v\nwant %+v", slug, got, want)
		}
	}
	if got := c.ThreadSandbox("none").Given(SandboxAllow); len(got) != 0 {
		t.Errorf("none's allow rules: %v", got)
	}
	if got := c.SandboxOf("web"); !reflect.DeepEqual(got.Lists, map[string][]string{SandboxExcluded: {"npx playwright *", "docker compose *"}}) || got.LocalPorts == nil {
		t.Errorf("SandboxOf(web) = %+v", got)
	}
	if got := c.SandboxOf("plain"); len(got.Lists) != 0 {
		t.Errorf("SandboxOf(plain) = %v", got)
	}
	if got := c.SandboxOf(""); len(got.Lists) != 3 {
		t.Errorf("SandboxOf(all) = %v", got)
	}
	var nilCfg *Config
	if got := nilCfg.ThreadSandbox("web"); !got.Empty() {
		t.Errorf("nil config: %+v", got)
	}
}

// TestReplaceNeverWider: before the migration, a project that opted out
// of all projects' excluded commands with an empty list, or listed its
// own, gets none of theirs, as in tm 0.37; only a drop or a list it
// doesn't set lets all projects' through. Such a file isn't written
// until it is migrated, which keeps the opt-out.
func TestReplaceNeverWider(t *testing.T) {
	write(t, `
[defaults.sandbox]
excluded_commands = ["docker *"]
allow = ["Bash(docker *)"]

[projects.optout.sandbox]
excluded_commands = []
allow = []

[projects.own.sandbox]
excluded_commands = ["make e2e"]
hosts = ["x.dev"]
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for slug, want := range map[string][2][]string{"optout": {nil, nil}, "own": {{"make e2e"}, {"Bash(docker *)"}}} {
		got := c.ThreadSandbox(slug)
		if !slices.Equal(got.Given(SandboxExcluded), want[0]) || !slices.Equal(got.Given(SandboxAllow), want[1]) {
			t.Errorf("%s: excluded %q allow %q", slug, got.Given(SandboxExcluded), got.Given(SandboxAllow))
		}
	}
	if err := EditSandbox("projects.optout", func(t *SandboxTable) error { t.Add(SandboxHosts, "x.dev"); return nil }); !errors.Is(err, ErrLegacy) {
		t.Fatalf("a write before the migration: %v", err)
	}
	migrateFile(t)
	if err := EditSandbox("projects.optout", func(t *SandboxTable) error { t.Add(SandboxHosts, "x.dev"); return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.ThreadSandbox("optout"); len(got.Given(SandboxExcluded))+len(got.Given(SandboxAllow)) != 0 {
		t.Errorf("optout after a write: %+v", got)
	}
}

// TestThreadSandboxRefused: an entry that fails its check loads, and is
// refused on its own; the rest hold (T574 P10).
func TestThreadSandboxRefused(t *testing.T) {
	write(t, `
[defaults.sandbox]
allow = ["Bash", "Bash(make e2e)"]

[projects.web.sandbox]
excluded_commands = ["*", "make e2e"]
hosts = ["https://example.com", "example.com"]
guides = ["browser-tests"]
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := c.ThreadSandbox("web")
	for _, k := range []string{SandboxAllow, SandboxExcluded, SandboxHosts} {
		if g := got.Given(k); len(g) != 1 {
			t.Errorf("%s given: %v", k, g)
		}
	}
	var refused []string
	for _, r := range got.Rules {
		if r.Refused != "" {
			refused = append(refused, r.Value)
		}
	}
	if want := []string{"https://example.com", "browser-tests", "*", "Bash"}; !reflect.DeepEqual(refused, want) {
		t.Errorf("refused %q, want %q", refused, want)
	}
}

func TestThreadSandboxLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"unknown key":   "[projects.web.sandbox]\nexclude = [\"x\"]\n",
		"not a list":    "[projects.web.sandbox]\nallow = \"Bash(a *)\"\n",
		"ports a list":  "[projects.web.sandbox]\nlocal_ports = [\"on\"]\n",
		"all projects'": "[defaults.sandbox]\ndrop = [\"x\"]\n",
	} {
		write(t, body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

// TestCheckHostsAndGuides: a host is a name, never an address, a URL or
// one word; a guide is a title of one line and a text in lines, or a
// name of tm 0.38's texts.
func TestCheckHostsAndGuides(t *testing.T) {
	for _, h := range []string{"registry.npmjs.org", "cdn.playwright.dev", "*.example.com", "a-b.c0.io", "*.example.co.uk", "me.github.io", "storage.googleapis.com"} {
		if err := CheckHost(h); err != nil {
			t.Errorf("%q: %v", h, err)
		}
	}
	for _, h := range []string{"", "localhost", "com", "*.com", "*", "https://x.dev", "x.dev:443", "x.dev/path", "10.0.0.1", "X.dev",
		"-x.dev", "x..dev", "x.localhost", "*.*.dev", "x_y.dev", " x.dev", "0x7f.0x1", "127.0.0.0x1", "0x7f.0.0.0x1", "localhost.localdomain",
		"nip.io", "127.0.0.1.nip.io", "*.sslip.io", "lvh.me", "app.localtest.me", "x.xip.io", "co.uk", "*.co.uk", "*.github.io", "github.io"} {
		if err := CheckHost(h); err == nil {
			t.Errorf("%q passed", h)
		}
	}
	if SandboxWarning(SandboxHosts, "storage.googleapis.com") == "" || SandboxWarning(SandboxHosts, "*.x.dev") == "" || SandboxWarning(SandboxHosts, "x.dev") != "" {
		t.Error("host warnings")
	}
	for _, g := range []Guide{{"browser-testing", ""}, {"browser-downloads", ""}, {"Test data", "Run make fixtures.\n\n\tNever edit testdata/."},
		LegacyGuides([]string{legacyBrowserTesting}, BrowserDomains, true)[0]} {
		if err := CheckGuide(g); err != nil {
			t.Errorf("%q: %v", g.Title, err)
		}
	}
	for _, g := range []Guide{{"browser_testing", ""}, {"Test data", ""}, {"Test data", " \n "}, {"", "x"}, {"a\nb", "x"}, {strings.Repeat("t", 121), "x"},
		{"T", strings.Repeat("x", MaxGuideBody+1)}, {"T", "a\x1b[2Jb"}, {"T", "a\r\nb"}, {"T", "a\u202eb"}} {
		if err := CheckGuide(g); err == nil {
			t.Errorf("%q passed", g)
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
		// T560's security review: a rule in another case.
		"bash(npx *)", "POWERSHELL(x)",
		// #655's review: a parenthesis would split the Bash(...) rule tm
		// writes for one that runs without asking.
		"npx playwright *) Bash(*", "make (x)", "x )"} {
		if err := CheckExcludedCommand(e); err == nil {
			t.Errorf("excluded %q passed", e)
		}
	}
	// A program left open is the human's to set (T619): taken, with its
	// warning.
	for _, e := range []string{"*e*", "*-*", "* --version", "python* -c x"} {
		if err := CheckExcludedCommand(e); err != nil || !strings.Contains(SandboxWarning(SandboxExcluded, e), "leaves the program open") {
			t.Errorf("excluded %q: %v, warning %q", e, err, SandboxWarning(SandboxExcluded, e))
		}
	}
	for _, e := range []string{"WebFetch(domain:playwright.dev)", "mcp__browser__open", "mcp__browser", "WebSearch", "Read(~/Library/Caches/ms-playwright/**)"} {
		if err := CheckSandboxAllow(e); err != nil {
			t.Errorf("allow %q: %v", e, err)
		}
	}
	// Rules that run commands or write files: a file written before
	// still has them (Load), a new one is refused (T574 case 1, 2).
	for _, e := range []string{"Bash(npx playwright *)", "Bash(npx playwright:*)", "bash(make e2e)", "PowerShell(npx playwright *)", "Edit(/tmp/pw/**)", "Write(/tmp/pw/x)", "MultiEdit(/tmp/pw/**)", "NotebookEdit(/tmp/pw/**)", "edit(/tmp/pw/**)"} {
		if err := CheckSandboxAllow(e); err == nil {
			t.Errorf("new allow %q passed", e)
		}
		if err := CheckNewSandboxEntry(SandboxAllow, e); err == nil {
			t.Errorf("new allow entry %q passed", e)
		}
		if err := CheckSandboxList(SandboxAllow, []string{e}); err != nil {
			t.Errorf("allow %q in a file: %v", e, err)
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
		{SandboxAllow, "Bash(npx playwright *)", true}, // allow rules no longer take it
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

func TestEditSandbox(t *testing.T) {
	write(t, "[projects.web]\nyolo = false\n")
	add := func(key, v string) func(*SandboxTable) error {
		return func(t *SandboxTable) error { t.Add(key, v); return nil }
	}
	if err := EditSandbox("projects.web", add(SandboxExcluded, "npx playwright *")); err != nil {
		t.Fatal(err)
	}
	if err := EditSandbox(DefaultsTable, add(SandboxAllow, "WebFetch(domain:playwright.dev)")); err != nil {
		t.Fatal(err)
	}
	if err := EditSandbox("projects.web", add(SandboxDrop, "WebFetch(domain:playwright.dev)")); err != nil {
		t.Fatal(err)
	}
	// A project's excluded command adds to all projects'; removing it
	// leaves theirs, and the file marked as one whose lists add.
	if err := EditSandbox(DefaultsTable, add(SandboxExcluded, "docker compose *")); err != nil {
		t.Fatal(err)
	}
	if err := EditSandbox("projects.app", add(SandboxExcluded, "make e2e")); err != nil {
		t.Fatal(err)
	}
	c0, _ := Load()
	if got := c0.ThreadSandbox("app").Given(SandboxExcluded); !reflect.DeepEqual(got, []string{"docker compose *", "make e2e"}) {
		t.Fatalf("app's excluded commands: %q", got)
	}
	if err := EditSandbox("projects.app", func(t *SandboxTable) error { t.Remove(SandboxExcluded, "make e2e"); return nil }); err != nil {
		t.Fatal(err)
	}
	c0, _ = Load()
	if got := c0.ThreadSandbox("app").Given(SandboxExcluded); !reflect.DeepEqual(got, []string{"docker compose *"}) || c0.legacy {
		t.Fatalf("app after removing its own: %q", got)
	}
	if b, _ := os.ReadFile(c0.Path); !strings.HasPrefix(string(b), accessMarker) {
		t.Errorf("no marker:\n%s", b)
	}
	for _, bad := range []struct {
		table string
		fn    func(*SandboxTable) error
	}{
		{"keys", add(SandboxAllow, "Bash(a *)")},
		{DefaultsTable, add(SandboxAllow, "Bash(npx playwright *)")},
		{DefaultsTable, add(SandboxAllow, "Edit(/tmp/pw/**)")},
		{"projects.web", add(SandboxWrite, "~")},
		{"projects.web", add(SandboxExcluded, "*")},
		{"projects.web", add(SandboxHosts, "localhost")},
		{DefaultsTable, add(SandboxDrop, "x")},
		{"projects.web", func(t *SandboxTable) error {
			t.Lists[SandboxGuides] = []string{GuideBrowserTesting, GuideBrowserTesting}
			return nil
		}},
		{"projects.web", func(t *SandboxTable) error { t.SetGuide(Guide{"Test data", ""}); return nil }},
		{"projects.web", func(t *SandboxTable) error { t.SetGuide(Guide{"Test data", "a\x1bb"}); return nil }},
	} {
		if err := EditSandbox(bad.table, bad.fn); err == nil {
			t.Errorf("%s: written", bad.table)
		}
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := c.ThreadSandbox("web")
	if !reflect.DeepEqual(got.Given(SandboxExcluded), []string{"docker compose *", "npx playwright *"}) || len(got.Given(SandboxAllow)) != 0 {
		t.Fatalf("got %+v", got)
	}
	if err := EditSandbox("projects.web", func(t *SandboxTable) error { t.Remove(SandboxDrop, "WebFetch(domain:playwright.dev)"); return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.ThreadSandbox("web"); !reflect.DeepEqual(got.Given(SandboxAllow), []string{"WebFetch(domain:playwright.dev)"}) {
		t.Fatalf("after undrop: %+v", got)
	}
	data, _ := os.ReadFile(c.Path)
	if !strings.Contains(string(data), "[projects.web.sandbox]") || !strings.Contains(string(data), "yolo = false") || strings.Contains(string(data), "drop =") {
		t.Fatalf("file:\n%s", data)
	}
}

// TestEditSandboxKeepsRefused: an entry the file has that fails its
// check doesn't stop a write, and can be removed.
func TestEditSandboxKeepsRefused(t *testing.T) {
	write(t, accessMarker+"[projects.web.sandbox]\nexcluded_commands = [\"*\"]\n")
	if err := EditSandbox("projects.web", func(t *SandboxTable) error { t.Add(SandboxExcluded, "make e2e"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := EditSandbox("projects.web", func(t *SandboxTable) error { t.Remove(SandboxExcluded, "*"); return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ := Load()
	if got := c.SandboxOf("web").Lists[SandboxExcluded]; !reflect.DeepEqual(got, []string{"make e2e"}) {
		t.Fatalf("got %q", got)
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

// TestOldSwitches: tm 0.37's switches read as the rules they
// unfold into, a project's off as drops of them; a write of the table
// replaces them with those rules.
func TestOldSwitches(t *testing.T) {
	write(t, `
[defaults.sandbox]
browser_testing = true
browser_downloads = true
local_dev_servers = true

[projects.web.sandbox]
browser_downloads = false
local_dev_servers = false
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	other := c.ThreadSandbox("other")
	if !reflect.DeepEqual(other.Given(SandboxHosts), BrowserDomains) || !reflect.DeepEqual(other.Given(SandboxGuides), legacyGuideNames) || !other.LocalPorts {
		t.Errorf("other: %+v", other)
	}
	web := c.ThreadSandbox("web")
	if len(web.Given(SandboxHosts)) != 0 || !reflect.DeepEqual(web.Given(SandboxGuides), []string{legacyBrowserTesting}) || web.LocalPorts || !web.LocalPortsOwn {
		t.Errorf("web: %+v", web)
	}
	if err := EditSandbox("projects.web", func(t *SandboxTable) error { t.Add(SandboxHosts, "x.dev"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := EditSandbox(DefaultsTable, func(*SandboxTable) error { return nil }); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(c2.Path)
	for _, k := range OldSwitchKeys() {
		if strings.Contains(string(data), k) {
			t.Errorf("%s left:\n%s", k, data)
		}
	}
	for _, slug := range []string{"web", "other"} {
		got, was := c2.ThreadSandbox(slug), c.ThreadSandbox(slug)
		if !reflect.DeepEqual(got.Given(SandboxGuides), was.Given(SandboxGuides)) || got.LocalPorts != was.LocalPorts {
			t.Errorf("%s changed: %+v, was %+v", slug, got, was)
		}
	}
	if got := c2.ThreadSandbox("web").Given(SandboxHosts); !reflect.DeepEqual(got, []string{"x.dev"}) {
		t.Errorf("web's hosts: %v", got)
	}
}

// TestOldSwitchRules: the rules each old switch unfolds into pass their
// checks.
func TestOldSwitchRules(t *testing.T) {
	for _, s := range oldSwitches {
		if len(s.rules) == 0 {
			t.Errorf("%s: no rules", s.key)
		}
		for _, r := range s.rules {
			if r.key == SandboxLocalPorts || r.key == SandboxGuides && slices.Contains(legacyGuideNames, r.value) {
				continue
			}
			if err := checkSandboxEntry(r.key, r.value); err != nil {
				t.Errorf("%s: %v", s.key, err)
			}
		}
	}
}

// TestSandboxApplies: every key has its platforms; hosts, guides and
// local ports are macOS's only (T561), the lists every platform's.
func TestSandboxApplies(t *testing.T) {
	for _, k := range append(slices.Clone(SandboxKeys), SandboxLocalPorts) {
		if len(SandboxSettings[k].OS) == 0 {
			t.Errorf("%s has no platforms", k)
		}
	}
	for _, c := range []struct {
		key, goos string
		want      bool
	}{
		{SandboxGuides, OSMac, true}, {SandboxGuides, OSLinux, true}, {SandboxHosts, OSMac, true}, {SandboxHosts, OSWindows, false}, {SandboxLocalPorts, OSWindows, false},
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

// TestExcludedAsk: an excluded command asks first, or with ask = false
// runs without asking; EditSandbox keeps each one's choice, a new one
// asks first; SetExcludedAsk changes it; a file's legacy Bash rule
// stays, and still lets the command it covers run without asking.
func TestExcludedAsk(t *testing.T) {
	write(t, accessMarker+`
[defaults.sandbox]
excluded_commands = ["docker compose *", { command = "npx playwright *", ask = false }]
allow = ["Bash(make e2e)"]

[projects.web.sandbox]
excluded_commands = [{ command = "make e2e", ask = true }, { command = "pnpm test *", ask = false }]
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ThreadSandbox("other"); !reflect.DeepEqual(got.Given(SandboxExcluded), []string{"docker compose *", "npx playwright *"}) || !reflect.DeepEqual(got.WithoutAsking(), []string{"npx playwright *"}) {
		t.Errorf("other: %+v", got)
	}
	// make e2e asks in its own table, but all projects' Bash rule covers
	// it: it ran without asking before, and still does.
	if got := c.ThreadSandbox("web").WithoutAsking(); !reflect.DeepEqual(got, []string{"npx playwright *", "make e2e", "pnpm test *"}) {
		t.Errorf("web: %q", got)
	}
	if got := c.SandboxOf("").NoAsk; !reflect.DeepEqual(got, []string{"npx playwright *"}) {
		t.Errorf("all projects' NoAsk = %q", got)
	}
	add := func(key, v string) func(*SandboxTable) error {
		return func(t *SandboxTable) error { t.Add(key, v); return nil }
	}
	// The allow list keeps its legacy rule when another is added.
	if err := EditSandbox(DefaultsTable, add(SandboxAllow, "WebSearch")); err != nil {
		t.Fatal(err)
	}
	if err := EditSandbox(DefaultsTable, func(t *SandboxTable) error {
		t.Remove(SandboxExcluded, "docker compose *")
		t.Add(SandboxExcluded, "cargo bench")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetExcludedAsk("projects.web", "make e2e", false); err != nil {
		t.Fatal(err)
	}
	if err := SetExcludedAsk("projects.web", "absent", false); err == nil {
		t.Error("SetExcludedAsk of a command the list lacks passed")
	}
	data, _ := os.ReadFile(c.Path)
	for _, want := range []string{
		`excluded_commands = [{ command = "npx playwright *", ask = false }, "cargo bench"]`,
		`excluded_commands = [{ command = "make e2e", ask = false }, { command = "pnpm test *", ask = false }]`,
		`allow = ["Bash(make e2e)", "WebSearch"]`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("no %s in:\n%s", want, data)
		}
	}
	// A project's own one asks first; all projects' keep their choice.
	if err := EditSandbox("projects.app", add(SandboxExcluded, "make bench")); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got, all := c.SandboxOf("app").NoAsk, c.ThreadSandbox("app").WithoutAsking(); len(got) != 0 || !reflect.DeepEqual(all, []string{"npx playwright *"}) {
		t.Errorf("app: own NoAsk %q, without asking %q", got, all)
	}
	for name, body := range map[string]string{
		"no command":  "[defaults.sandbox]\nexcluded_commands = [{ ask = false }]\n",
		"unknown key": "[defaults.sandbox]\nexcluded_commands = [{ command = \"make e2e\", asks = false }]\n",
		"ask text":    "[defaults.sandbox]\nexcluded_commands = [{ command = \"make e2e\", ask = \"no\" }]\n",
		"number":      "[defaults.sandbox]\nexcluded_commands = [1]\n",
	} {
		write(t, body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	// A table entry that fails its check is refused on its own.
	write(t, "[defaults.sandbox]\nexcluded_commands = [{ command = \"*\", ask = false }, \"make e2e\"]\n")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ThreadSandbox("x").Given(SandboxExcluded); !reflect.DeepEqual(got, []string{"make e2e"}) {
		t.Errorf("refused entry: %q", got)
	}
}

// TestWriteFolderCode: no write folder where files are loaded or run as
// code: ~/Library but its caches, toolchains' and package managers'
// folders, the system's, and a folder on PATH or holding one; names
// compare with case folded where the file system ignores case (#655
// security review).
func TestWriteFolderCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/x/tools/bin:"+home+"/src/tools/bin")
	for _, e := range []string{"~/Library/Caches/ms-playwright", "~/src/fixtures", "~/src/tools-data", "/x/data/fixtures"} {
		if err := CheckWriteFolder(e); err != nil {
			t.Errorf("%q: %v", e, err)
		}
	}
	for _, e := range []string{"~/Library/Preferences/x", "~/Library/Application Support/x/plugins", "~/Library/Caches", "~/.cargo/bin", "~/go/bin", "~/.local/share/x",
		"~/.docker/cli-plugins", "~/.oh-my-zsh/custom", "~/.npm-global/lib", "~/.bun/bin", "~/.deno/bin", "~/bin/x", "/opt/homebrew/lib", "/usr/local/lib",
		"/etc/paths.d", "/x/tools/bin", "/x/tools/bin/sub", "~/src/tools", "~/src/tools/bin", home + "/.cargo/env", home + "/src/tools"} {
		if err := CheckWriteFolder(e); err == nil {
			t.Errorf("%q passed", e)
		}
	}
	// More folders on PATH (the server's login shell's) count too.
	SetMorePath(func() []string { return []string{"/y/login/bin"} })
	defer SetMorePath(func() []string { return nil })
	for _, e := range []string{"/y/login/bin/x", "/y/login", "~/.vscode/extensions", "~/.cursor/x", "~/.gradle/init.d", "~/.ipython/profile_default", "~/.jupyter/x",
		"~/.m2/repository", "~/.config/x", "~/.cache/x", "~/AppData/Roaming/x", "~/Documents/PowerShell/x", "~/Documents/WindowsPowerShell/x"} {
		if err := CheckWriteFolder(e); err == nil {
			t.Errorf("%q passed", e)
		}
	}
	if err := CheckWriteFolder("~/Documents/fixtures"); err != nil {
		t.Errorf("~/Documents/fixtures: %v", err)
	}
	// A cache is allowed, saying tools run what is in it.
	if w := SandboxWarning(SandboxWrite, "~/Library/Caches/ms-playwright"); !strings.Contains(w, "plant code") {
		t.Errorf("cache warning %q", w)
	}
	if caps.CaseFold {
		// The whole home, or a folder one below it, in another case
		// (#655 re-review).
		for _, e := range []string{"/USERS/sam", "/USERS/sam/Work", "/users/sam/Work", "/Home/me/x"} {
			if err := CheckWriteFolder(e); err == nil {
				t.Errorf("%q passed with case folded", e)
			}
		}
		for _, e := range []string{"~/library/launchagents/x", "~/LIBRARY/Preferences/x", "~/src/repo/.GIT/hooks", "~/src/x/.SSH/k", "~/.CLAUDE/commands", "~/.Config/git", "~/src/x/LaunchAgents", "~/.Cargo/bin", "/USR/local/x", "~/src/TOOLS/bin"} {
			if err := CheckWriteFolder(e); err == nil {
				t.Errorf("%q passed with case folded", e)
			}
		}
	}
}

// TestWriteFolders: a write folder is one plain folder, not high up nor
// where files start programs; the lists add down the layers.
func TestWriteFolders(t *testing.T) {
	for _, e := range []string{"~/src/fixtures", "/Volumes/data/fixtures", "/opt/fixtures", "/Users/me/src/fixtures", `C:\Users\me\src\fixtures`} {
		if err := CheckWriteFolder(e); err != nil {
			t.Errorf("%q: %v", e, err)
		}
	}
	for _, e := range []string{"", "~", "~/", "~/src", "/", "/opt", "/Users/me", "/Users/me/src", "/home/me/x", `C:\Users\me`, "src/fixtures", "./x", "//net/share/x",
		"~/src/*", "~/src/x/**", "~/src/../.ssh", "~/src/./x", "~/src//x", "~/src/x/", "~/src/$X", "~/.ssh/keys", "~/src/repo/.git", "~/src/repo/.git/hooks", "~/.config/x", "~/.terminatr/x", "~/.claude/x", "~/Library/LaunchAgents/x"} {
		if err := CheckWriteFolder(e); err == nil {
			t.Errorf("%q passed", e)
		}
	}
	write(t, "[defaults.sandbox]\nwrite = [\"~/src/fixtures\", \"~\"]\n\n[projects.web.sandbox]\nwrite = [\"~/src/web-data\"]\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ThreadSandbox("web").Given(SandboxWrite); !reflect.DeepEqual(got, []string{"~/src/fixtures", "~/src/web-data"}) {
		t.Errorf("web: %q", got)
	}
	if SandboxWarning(SandboxWrite, "~/src/fixtures") == "" {
		t.Error("a write folder doesn't say what it opens")
	}
}

// migrateFile migrates the test's config.toml in place.
func migrateFile(t *testing.T) *Migration {
	t.Helper()
	path, _ := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Migrate(data)
	if err != nil || m == nil || m.Differs != "" {
		t.Fatalf("migrate: %+v, %v", m, err)
	}
	if err := os.WriteFile(path, m.Data, 0o600); err != nil {
		t.Fatal(err)
	}
	return m
}
