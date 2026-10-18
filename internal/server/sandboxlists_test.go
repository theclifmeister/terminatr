package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestSandboxListsLaunch: a Claude thread's launch settings get the
// project's sandbox lists from config.toml, read by the server; a
// coordinator's never do (Codex's launch warning: codex_test.go).
func TestSandboxListsLaunch(t *testing.T) {
	p := testPaths(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	newWatchProject(t)
	writeConfig(t, `[defaults.sandbox]
allow = ["Bash(npx playwright *)"]

[projects.demo.sandbox]
excluded_commands = ["npx playwright *"]
`)
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	var logged lockedBuffer
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin"}},
		log: log.New(&logged, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	settingsOf := func(rec SessionRecord) (excluded, allow []string) {
		t.Helper()
		pl, perr := s.prepareLaunch(agentLaunch{rec: rec, kick: "Start."}, launchView{agents: reg})
		if perr != nil {
			t.Fatal(perr)
		}
		t.Cleanup(pl.abandon)
		b, err := os.ReadFile(filepath.Join(pl.rt, "claude-settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		var st struct {
			Permissions struct{ Allow []string }
			Sandbox     struct{ ExcludedCommands []string }
		}
		if err := json.Unmarshal(b, &st); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return st.Sandbox.ExcludedCommands, st.Permissions.Allow
	}
	excluded, allow := settingsOf(SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: "demo", Thread: "t-0001", Cwd: t.TempDir()})
	if !slices.Equal(excluded, []string{"npx playwright *"}) || !slices.Contains(allow, "Bash(npx playwright *)") {
		t.Errorf("thread: excluded %q, allow %q", excluded, allow)
	}
	excluded, allow = settingsOf(SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Agent: "claude", Project: "demo", Cwd: t.TempDir()})
	if excluded != nil || slices.Contains(allow, "Bash(npx playwright *)") {
		t.Errorf("coordinator: excluded %q, allow %q", excluded, allow)
	}
}

// TestSandboxRulesLaunch: hosts, local ports and guidance map to the
// launch where they apply (macOS) and are skipped with a note
// elsewhere, config.toml keeping them; a project's rules add to all
// projects', and a drop takes one away. tm 0.37's switches launch as
// their rules: browser_testing alone opens no host.
func TestSandboxRulesLaunch(t *testing.T) {
	testPaths(t)
	writeConfig(t, `[defaults.sandbox]
guides = ["browser-testing"]
excluded_commands = ["npx playwright *"]
`)
	lc := loadLaunchConfig()
	lists, guides, skipped := lc.sandbox("demo", config.OSMac)
	if lists.LocalBinding || !slices.Equal(guides, []string{config.GuideBrowserTesting}) || len(skipped) != 0 ||
		!slices.Equal(lists.Excluded, []string{"npx playwright *"}) || lists.Domains != nil {
		t.Errorf("macOS: %+v %q %q", lists, guides, skipped)
	}
	lists, guides, skipped = lc.sandbox("demo", config.OSLinux)
	if lists.LocalBinding || lists.Domains != nil || len(guides) != 0 || !slices.Equal(lists.Excluded, []string{"npx playwright *"}) ||
		!slices.Equal(skipped, []string{"sandbox guidance texts skipped: doesn't apply on Linux"}) {
		t.Errorf("Linux: %+v %q %q", lists, guides, skipped)
	}

	writeConfig(t, `[defaults.sandbox]
browser_testing = true
hosts = ["npm.pkg.github.com"]

[projects.demo.sandbox]
browser_downloads = true
local_ports = true
drop = ["npm.pkg.github.com"]
`)
	lc = loadLaunchConfig()
	lists, guides, _ = lc.sandbox("demo", config.OSMac)
	if !slices.Equal(lists.Domains, config.BrowserDomains) || !lists.LocalBinding ||
		!slices.Equal(guides, []string{config.GuideBrowserTesting, config.GuideBrowserDownloads}) {
		t.Errorf("demo: %+v %q", lists, guides)
	}
	if lists, _, _ = lc.sandbox("other", config.OSMac); !slices.Equal(lists.Domains, []string{"npm.pkg.github.com"}) || lists.LocalBinding {
		t.Errorf("other: %+v", lists)
	}
	if lists, _, skipped = lc.sandbox("demo", config.OSLinux); lists.Domains != nil || lists.LocalBinding || len(skipped) != 3 {
		t.Errorf("Linux: %+v %q", lists, skipped)
	}
}

// TestGuideBrief: the guidance texts follow the brief, worded from the
// thread's other rules: which download hosts are open, and local
// ports.
func TestGuideBrief(t *testing.T) {
	rt := t.TempDir()
	brief := filepath.Join(t.TempDir(), "brief.md")
	os.WriteFile(brief, []byte("# Thread t-0001\n"), 0o600)
	none := agent.SandboxLists{}
	all := agent.SandboxLists{Domains: config.BrowserDomains, LocalBinding: true}
	if got, err := guideBrief(rt, brief, nil, all); err != nil || got != brief {
		t.Errorf("no guides: %q, %v", got, err)
	}
	got, err := guideBrief(rt, brief, []string{config.GuideBrowserTesting}, none)
	b, _ := os.ReadFile(got)
	if err != nil || filepath.Dir(got) != rt || !strings.HasPrefix(string(b), "# Thread t-0001\n") || !strings.Contains(string(b), "--single-process") ||
		!strings.Contains(string(b), "reuseContext: true") || !strings.Contains(string(b), browserPortsClosed) || !strings.Contains(string(b), browserNoDownloads) || strings.Contains(string(b), "%!") {
		t.Errorf("browser testing: %q, %v\n%s", got, err, b)
	}
	got, _ = guideBrief(rt, brief, []string{config.GuideBrowserTesting, config.GuideBrowserDownloads}, all)
	if b, _ = os.ReadFile(got); !strings.Contains(string(b), browserPortsOpen) || !strings.Contains(string(b), "the human opened registry.npmjs.org") ||
		strings.Contains(string(b), ", not ") || strings.Contains(string(b), "%!") {
		t.Errorf("browser testing with downloads and local ports:\n%s", b)
	}
	some := agent.SandboxLists{Domains: []string{"registry.npmjs.org", "x.dev"}}
	got, _ = guideBrief(rt, brief, []string{config.GuideBrowserDownloads}, some)
	if b, _ = os.ReadFile(got); !strings.Contains(string(b), "## Sandbox: browser downloads") || !strings.Contains(string(b), "not cdn.playwright.dev") ||
		strings.Contains(string(b), "x.dev") || strings.Contains(string(b), "--single-process") {
		t.Errorf("browser downloads alone, some hosts:\n%s", b)
	}
	if got, err = guideBrief(rt, "", []string{config.GuideBrowserTesting}, none); err != nil || got == "" {
		t.Errorf("no brief: %q, %v", got, err)
	}
	big := filepath.Join(t.TempDir(), "big.md")
	os.WriteFile(big, make([]byte, maxBrief+1), 0o600)
	if _, err = guideBrief(rt, big, []string{config.GuideBrowserTesting}, none); err == nil {
		t.Errorf("a brief over %d bytes was read", maxBrief)
	}
}

// TestSandboxRefusedSaid: an entry that fails its check is refused on
// its own, the rest kept (T574 P10), and the launch says why: the
// entry and its rule.
func TestSandboxRefusedSaid(t *testing.T) {
	testPaths(t)
	writeConfig(t, `[defaults.sandbox]
guides = ["browser-testing"]

[projects.demo.sandbox]
excluded_commands = ["make a$X", "make e2e"]
allow = ["Bash(npx playwright *)"]
`)
	lists, guides, notes := loadLaunchConfig().sandbox("demo", config.OSMac)
	if !slices.Equal(lists.Excluded, []string{"make e2e"}) || !slices.Equal(lists.Allow, []string{"Bash(npx playwright *)"}) || len(guides) != 1 {
		t.Errorf("not kept: %+v %q", lists, guides)
	}
	if len(notes) != 1 {
		t.Fatalf("notes %q", notes)
	}
	for _, want := range []string{"excluded", "make a$X", "has a variable", "refused"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("note lacks %q: %s", want, notes[0])
		}
	}
}
