package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// legacyMerge is what threads got in tm 0.37 from all projects' table
// all and a project's own: a project's list of a legacyReplaceKeys key
// it sets (in replaces) replaced all projects'; the other lists added,
// less drops. Kept here, apart from unreplaced, to check it.
func legacyMerge(all, own SandboxTable, replaces []string) ThreadSandbox {
	var out ThreadSandbox
	drops := own.Lists[SandboxDrop]
	for _, k := range SandboxKeys {
		replaced := slices.Contains(replaces, k)
		for _, v := range all.Lists[k] {
			r := SandboxRule{Key: k, Value: v, Dropped: replaced || slices.Contains(drops, v), NoAsk: k == SandboxExcluded && slices.Contains(all.NoAsk, v)}
			if err := checkSandboxEntry(k, v); err != nil {
				r.Refused = err.Error()
			}
			out.Rules = append(out.Rules, r)
		}
		for _, v := range own.Lists[k] {
			if !replaced && slices.Contains(all.Lists[k], v) && !slices.Contains(drops, v) {
				continue
			}
			r := SandboxRule{Key: k, Value: v, Own: true, NoAsk: k == SandboxExcluded && slices.Contains(own.NoAsk, v)}
			if err := checkSandboxEntry(k, v); err != nil {
				r.Refused = err.Error()
			}
			out.Rules = append(out.Rules, r)
		}
	}
	allow := out.Given(SandboxAllow)
	for i := range out.Rules {
		if r := &out.Rules[i]; r.Key == SandboxExcluded && SandboxCovered(r.Value, allow) != "" {
			r.NoAsk = true
		}
	}
	switch {
	case own.LocalPorts != nil:
		out.LocalPorts = *own.LocalPorts
	case all.LocalPorts != nil:
		out.LocalPorts = *all.LocalPorts
	}
	return out
}

// legacyModel is what slug's threads got from data in tm 0.37, as
// accessModel.
func legacyModel(t *testing.T, data []byte, slug string) accessModel {
	t.Helper()
	raw, err := decodeTables(data)
	if err != nil {
		t.Fatal(err)
	}
	c, err := parse("config.toml", data)
	if err != nil {
		t.Fatal(err)
	}
	var own SandboxTable
	var replaces []string
	if r := raw.Projects[slug].Sandbox; slug != "" {
		own = r.table(true)
		for _, k := range legacyReplaceKeys {
			if r.list(k) != nil {
				replaces = append(replaces, k)
			}
		}
	}
	s, err := c.Safety(slug)
	if err != nil {
		t.Fatal(err)
	}
	return modelOf(legacyMerge(raw.Defaults.Sandbox.table(false), own, replaces), s.GuardAllow)
}

// TestMigrateMatrix: each file of tm 0.37 migrates to one whose
// projects, all projects and a project without a table get the same
// rules as tm 0.37 gave them; the file read before its migration gives
// them too; a migrated file needs no other.
func TestMigrateMatrix(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	cases := map[string]struct {
		file    string
		changes []string // in the new file
		kept    []string // in Kept, by their text
		yolo    []string
	}{
		"switches": {file: `
[defaults.sandbox]
browser_testing = true
browser_downloads = true
local_dev_servers = true

[projects.a.sandbox]
browser_downloads = false

[projects.b.sandbox]
local_dev_servers = false
browser_testing = false

[projects.c.sandbox]
browser_testing = true
`, changes: []string{`guides = ["browser-testing", "browser-downloads"]`, `local_ports = true`, `drop = ["registry.npmjs.org"`, "local_ports = false"}},
		"switch off, all projects off": {file: `
[projects.a.sandbox]
browser_downloads = false
local_dev_servers = false
`, changes: []string{"local_ports = false"}},
		"replaced excluded": {file: `
[defaults.sandbox]
excluded_commands = ["docker compose *", { command = "npx playwright *", ask = false }, "make e2e"]

[projects.own.sandbox]
excluded_commands = ["make e2e", "cargo bench"]

[projects.optout.sandbox]
excluded_commands = []

[projects.asks.sandbox]
excluded_commands = ["npx playwright *"]

[projects.dropped.sandbox]
excluded_commands = ["make e2e"]
drop = ["make e2e"]

[projects.follows.sandbox]
drop = ["docker compose *"]
`, changes: []string{`drop = ["docker compose *", "npx playwright *"]`, `drop = ["docker compose *", "npx playwright *", "make e2e"]`}},
		"replaced allow": {file: `
[defaults.sandbox]
excluded_commands = ["docker compose *"]
allow = ["WebFetch(domain:playwright.dev)", "Bash(docker compose *)"]

[projects.own.sandbox]
allow = ["WebSearch"]

[projects.empty.sandbox]
allow = []

[projects.same.sandbox]
allow = ["Bash(docker compose *)"]
`, changes: []string{`{ command = "docker compose *", ask = false }`}},
		"bash rules": {file: `
[defaults.sandbox]
excluded_commands = ["npx playwright *"]
allow = ["Bash(npx playwright:*)", "Bash(git status)", "PowerShell(npx *)"]

[projects.p.sandbox]
excluded_commands = ["npx playwright *", "make e2e"]
allow = ["Bash(make e2e)", "Bash(npx playwright *)"]

[projects.q.sandbox]
excluded_commands = ["make bench"]
allow = ["Bash(make *)"]
`, kept: []string{"Bash(git status)", "PowerShell(npx *)"}},
		"project leaving out all projects' Bash rule": {file: `
[defaults.sandbox]
allow = ["Bash(make *)"]

[projects.p.sandbox]
excluded_commands = ["make e2e"]
allow = []
`, changes: []string{`drop = ["Bash(make *)"]`}},
		"bash rule covering only a project's": {file: `
[defaults.sandbox]
allow = ["Bash(make *)"]

[projects.p.sandbox]
excluded_commands = ["make e2e"]
`, kept: []string{"Bash(make *)"}},
		"file rules": {file: `
[defaults.sandbox]
allow = ["Edit(~/src/fixtures/**)", "Write(//Users/someone/src/shared/out/**)", "Edit(~/.ssh/**)", "Edit(src/**)", "MultiEdit(~/src/one)"]

[projects.p.sandbox]
allow = ["NotebookEdit(~/src/notes/**)", "Edit(~/src/*.txt)"]
write = ["~/src/fixtures"]
`, changes: []string{`write = ["~/src/fixtures", "/Users/someone/src/shared/out", "~/src/one"]`, `write = ["~/src/fixtures", "~/src/notes"]`},
			kept: []string{"Edit(~/.ssh/**)", "Edit(src/**)", "Edit(~/src/*.txt)"}},
		"guard allowances": {file: `
[defaults]
guard_allow = [{ rule = "worktree-only", path = "~/src/shared/*" }, { rule = "force-push", command = "git push -f origin tm/*" }]

[projects.p]
guard_allow = [{ rule = "worktree-only", path = "~/.zshrc" }, { rule = "worktree-only", path = "~/src/p-fixtures/*" }]
`, changes: []string{`write = ["~/src/shared"]`, `write = ["~/src/p-fixtures"]`, `guard_allow = [{ rule = "force-push"`}, kept: []string{"~/.zshrc"}},
		"same text in two lists": {file: `
[defaults.sandbox]
hosts = ["build.dev"]
excluded_commands = ["build.dev"]

[projects.p.sandbox]
excluded_commands = []
`, changes: []string{`hosts = ["build.dev"]`, `drop = ["build.dev"]`}},
		"refused entries": {file: `
[defaults.sandbox]
excluded_commands = ["*", "make e2e"]

[projects.p.sandbox]
excluded_commands = ["bash(x)"]
`},
		"yolo": {file: `
[projects.y]
yolo = true

[projects.y.sandbox]
excluded_commands = []
local_dev_servers = true
`, yolo: []string{"y"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			old := []byte(c.file)
			m, err := Migrate(old)
			if err != nil || m == nil {
				t.Fatalf("Migrate: %+v, %v", m, err)
			}
			if m.Differs != "" {
				t.Fatalf("refused: %s\n%s", m.Differs, m.Data)
			}
			out := string(m.Data)
			if !strings.HasPrefix(out, accessMarker) {
				t.Errorf("no marker:\n%s", out)
			}
			for _, w := range c.changes {
				if !strings.Contains(out, w) {
					t.Errorf("no %s in:\n%s", w, out)
				}
			}
			for _, k := range OldSwitchKeys() {
				if strings.Contains(out, k) {
					t.Errorf("old switch %s left:\n%s", k, out)
				}
			}
			var kept []string
			for _, l := range m.Kept {
				kept = append(kept, l...)
			}
			for _, w := range c.kept {
				if !slices.ContainsFunc(kept, func(k string) bool { return strings.Contains(k, w) }) {
					t.Errorf("%s not in Kept %q", w, kept)
				}
			}
			if !slices.Equal(m.Yolo, c.yolo) {
				t.Errorf("yolo %q", m.Yolo)
			}
			oldCfg, err := parse("config.toml", old)
			if err != nil {
				t.Fatal(err)
			}
			nu, err := parse("config.toml", m.Data)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := decodeTables(old)
			slugs := append([]string{"", "untabled"}, slices.Sorted(func(yield func(string) bool) {
				for s := range raw.Projects {
					if !yield(s) {
						return
					}
				}
			})...)
			for _, slug := range slugs {
				if slices.Contains(c.yolo, slug) {
					continue
				}
				want := legacyModel(t, old, slug)
				s, _ := nu.Safety(slug)
				if got := modelOf(nu.ThreadSandbox(slug), s.GuardAllow); !got.same(want) {
					t.Errorf("%q migrated: %s\n%s", slug, modelDiff(want, got), out)
				}
				s0, _ := oldCfg.Safety(slug)
				if got := modelOf(oldCfg.ThreadSandbox(slug), s0.GuardAllow); !got.same(want) {
					t.Errorf("%q read before the migration: %s", slug, modelDiff(want, got))
				}
			}
			if again, err := Migrate(m.Data); err != nil || again != nil {
				t.Errorf("migrated again: %+v, %v", again, err)
			}
		})
	}
}

// TestMigrateNothing: a file with nothing of tm 0.37's, or marked,
// needs no migration and stays as it is.
func TestMigrateNothing(t *testing.T) {
	for _, f := range []string{"", "[projects.a]\nyolo = true\n", "[defaults.sandbox]\nexcluded_commands = [\"make e2e\"]\nallow = [\"WebSearch\"]\n",
		accessMarker + "[projects.a.sandbox]\nexcluded_commands = []\nallow = [\"Bash(x *)\"]\n"} {
		if m, err := Migrate([]byte(f)); m != nil || err != nil {
			t.Errorf("%q: %+v, %v", f, m, err)
		}
	}
	if _, err := parse("config.toml", []byte("sandbox_lists = \"replace\"\n")); err == nil {
		t.Error("another marker value loaded")
	}
}

// TestMigrateRefuses: the before/after check refuses a new file that
// gives a project other rules, saying what; a refused Migration keeps
// the old file. A project dropping all projects' Bash rule while its
// own covers the same command fits.
func TestMigrateRefuses(t *testing.T) {
	old := []byte("[defaults.sandbox]\nexcluded_commands = [\"make e2e\"]\nallow = [\"Bash(make *)\"]\n\n[projects.p.sandbox]\nallow = [\"Bash(make e2e)\"]\ndrop = [\"Bash(make *)\"]\n")
	m, err := Migrate(old)
	if err != nil || m.Differs != "" {
		t.Fatalf("%+v, %v", m, err)
	}
	nu, _ := parse("config.toml", m.Data)
	if got, want := modelOf(nu.ThreadSandbox("p"), nil), legacyModel(t, old, "p"); !got.same(want) {
		t.Errorf("p: %s", modelDiff(want, got))
	}
	oldCfg, _ := parse("config.toml", old)
	var yolo []string
	if why := compareMigrated(oldCfg, withMarker([]byte("[defaults.sandbox]\nexcluded_commands = [\"make e2e\", \"rm *\"]\n")), []string{"p"}, &yolo); !strings.Contains(why, "rm *") {
		t.Errorf("differs: %q", why)
	}
	if why := compareMigrated(oldCfg, withMarker([]byte("[defaults]\nyolo = true\n")), nil, &yolo); !strings.Contains(why, "settings would change") {
		t.Errorf("differs: %q", why)
	}
}

// TestLegacyNotWritten: a sandbox write to a file waiting for its
// migration is refused; once migrated, it goes through.
func TestLegacyNotWritten(t *testing.T) {
	write(t, "[defaults.sandbox]\nexcluded_commands = [\"docker *\"]\n\n[projects.p.sandbox]\nexcluded_commands = []\n")
	c, _ := Load()
	if len(c.OldAccess()) == 0 {
		t.Error("tm doctor isn't told")
	}
	if err := SetExcludedAsk(DefaultsTable, "docker *", false); err != ErrLegacy {
		t.Fatalf("written: %v", err)
	}
	migrateFile(t)
	if err := SetExcludedAsk(DefaultsTable, "docker *", false); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if got := c.ThreadSandbox("p").Given(SandboxExcluded); len(got) != 0 || len(c.OldAccess()) != 0 {
		t.Errorf("p: %q, doctor %q", got, c.OldAccess())
	}
	data, _ := os.ReadFile(c.Path)
	if !strings.Contains(string(data), `drop = ["docker *"]`) {
		t.Errorf("file:\n%s", data)
	}
}
