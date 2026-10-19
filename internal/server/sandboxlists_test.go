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
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestSandboxListsLaunch: a Claude thread's launch settings get the
// project's sandbox lists from config.toml, read by the server; a
// coordinator's never do (Codex's launch warning: codex_test.go).
func TestSandboxListsLaunch(t *testing.T) {
	p := testPaths(t)
	cleanThreadTemp(t, "demo", "t-0001")
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
	if lists.LocalBinding || len(guides) != 1 || guides[0].Title != config.GuideBrowserTesting || len(skipped) != 0 ||
		!slices.Equal(lists.Excluded, []string{"npx playwright *"}) || lists.Domains != nil {
		t.Errorf("macOS: %+v %q %q", lists, guides, skipped)
	}
	lists, guides, skipped = lc.sandbox("demo", config.OSLinux)
	// Guides are plain text, given on every platform.
	if lists.LocalBinding || lists.Domains != nil || len(guides) != 1 || !slices.Equal(lists.Excluded, []string{"npx playwright *"}) || len(skipped) != 0 {
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
	// tm 0.38's two names: browser testing's text, its downloads part
	// among it.
	if !slices.Equal(lists.Domains, config.BrowserDomains) || !lists.LocalBinding ||
		len(guides) != 1 || guides[0].Title != config.GuideBrowserTesting || !strings.Contains(guides[0].Body, "the human opened registry.npmjs.org") {
		t.Errorf("demo: %+v %q", lists, guides)
	}
	if lists, _, _ = lc.sandbox("other", config.OSMac); !slices.Equal(lists.Domains, []string{"npm.pkg.github.com"}) || lists.LocalBinding {
		t.Errorf("other: %+v", lists)
	}
	if lists, _, skipped = lc.sandbox("demo", config.OSLinux); lists.Domains != nil || lists.LocalBinding || len(skipped) != 2 {
		t.Errorf("Linux: %+v %q", lists, skipped)
	}
}

// TestGuideBrief: each guide follows the brief under its title, then
// the approved commands; a brief without either is the brief itself.
func TestGuideBrief(t *testing.T) {
	rt := t.TempDir()
	brief := filepath.Join(t.TempDir(), "brief.md")
	os.WriteFile(brief, []byte("# Thread t-0001\n"), 0o600)
	none := agent.SandboxLists{}
	if got, err := guideBrief(rt, brief, nil, none); err != nil || got != brief {
		t.Errorf("no guides: %q, %v", got, err)
	}
	guides := []config.Guide{{Title: "Test data", Body: "Run make fixtures.\n\n\tNever edit testdata/."}, config.BrowserTestingGuide(false)}
	got, err := guideBrief(rt, brief, guides, agent.SandboxLists{Approved: []string{"make e2e"}})
	b, _ := os.ReadFile(got)
	want := "# Thread t-0001\n\n## Test data\n\nRun make fixtures.\n\n\tNever edit testdata/.\n\n## Sandbox: browser testing\n\n"
	if err != nil || filepath.Dir(got) != rt || !strings.HasPrefix(string(b), want) || !strings.Contains(string(b), "--single-process") ||
		!strings.Contains(string(b), "\n## Approved commands") || !strings.Contains(string(b), "- make e2e\n") {
		t.Errorf("guides: %q, %v\n%s", got, err, b)
	}
	if got, err = guideBrief(rt, "", guides, none); err != nil || got == "" {
		t.Errorf("no brief: %q, %v", got, err)
	}
	big := filepath.Join(t.TempDir(), "big.md")
	os.WriteFile(big, make([]byte, maxBrief+1), 0o600)
	if _, err = guideBrief(rt, big, guides, none); err == nil {
		t.Errorf("a brief over %d bytes was read", maxBrief)
	}
}

// TestLegacyGuidesWorded: tm 0.38's names are worded at launch from the
// thread's other rules, as tm 0.38 did: which download hosts are open,
// and local ports.
func TestLegacyGuidesWorded(t *testing.T) {
	g := config.LegacyGuides([]string{"browser-testing"}, nil, false)
	if len(g) != 1 || !strings.Contains(g[0].Body, "hasn't opened local ports") || !strings.Contains(g[0].Body, "may not reach the npm registry") || strings.Contains(g[0].Body, "%!") {
		t.Errorf("browser testing: %q", g)
	}
	g = config.LegacyGuides([]string{"browser-testing", "browser-downloads"}, config.BrowserDomains, true)
	if len(g) != 1 || !strings.Contains(g[0].Body, "human also opened local ports") || !strings.Contains(g[0].Body, "the human opened registry.npmjs.org") || strings.Contains(g[0].Body, ", not ") {
		t.Errorf("browser testing with downloads and local ports: %q", g)
	}
	g = config.LegacyGuides([]string{"browser-downloads"}, []string{"registry.npmjs.org", "x.dev"}, false)
	if len(g) != 1 || g[0].Title != config.GuideBrowserDownloads || !strings.Contains(g[0].Body, "not cdn.playwright.dev") || strings.Contains(g[0].Body, "x.dev") || strings.Contains(g[0].Body, "--single-process") {
		t.Errorf("browser downloads alone, some hosts: %q", g)
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

// TestExcludedAskAndWriteLaunch: an excluded command that runs without
// asking gets its Bash(...) allow rule from tm, one that asks first
// none, and the guard asks about it, live and as the thread launched
// (T574 case 1); a write folder is one Edit(...) allow rule and the
// guard's writable list, never tm's folder (case 2). Not for a Codex
// thread's guard, nor under yolo.
func TestExcludedAskAndWriteLaunch(t *testing.T) {
	p := testPaths(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", t.TempDir())
	newWatchProject(t)
	writeConfig(t, `[projects.demo.sandbox]
excluded_commands = ["make e2e", { command = "npx playwright *", ask = false }]
write = ["~/src/fixtures", "/`+strings.TrimPrefix(filepath.ToSlash(p.Home), "/")+`/sub", "~/src/keys", "~/src/repo"]
`)
	// A folder that leads where no write folder may be, and one that
	// holds a repository of the project's: both left out.
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.MkdirAll(filepath.Join(home, "src"), 0o755)
	if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(home, "src", "keys")); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(home, "src", "repo", "app")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	if err := config.SetRepos("demo", []string{repo}); err != nil {
		t.Fatal(err)
	}
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	var logged lockedBuffer
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin"}},
		log: log.New(&logged, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	rec := SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: "demo", Thread: "t-0001", Cwd: t.TempDir()}
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
	fixtures := filepath.Join(home, "src", "fixtures")
	if !slices.Equal(st.Sandbox.ExcludedCommands, []string{"make e2e", "npx playwright *"}) {
		t.Errorf("excluded %q", st.Sandbox.ExcludedCommands)
	}
	if !slices.Contains(st.Permissions.Allow, "Bash(npx playwright *)") || slices.Contains(st.Permissions.Allow, "Bash(make e2e)") {
		t.Errorf("allow %q", st.Permissions.Allow)
	}
	if !slices.Contains(st.Permissions.Allow, "Edit(/"+filepath.ToSlash(fixtures)+"/**)") || slices.ContainsFunc(st.Permissions.Allow, func(a string) bool { return strings.Contains(a, "/sub") }) {
		t.Errorf("write folders: allow %q", st.Permissions.Allow)
	}
	for _, w := range []string{"/sub", ".ssh", "src/keys", "src/repo"} {
		if slices.ContainsFunc(st.Permissions.Allow, func(a string) bool { return strings.Contains(a, w) }) {
			t.Errorf("write folders: %s opened: allow %q", w, st.Permissions.Allow)
		}
	}
	for _, w := range []string{filepath.Join(p.Home, "sub"), filepath.Join(home, "src", "keys"), filepath.Join(home, "src", "repo")} {
		if !strings.Contains(logged.String(), "write-folder \""+w+"\" refused") {
			t.Errorf("%q isn't said to be refused: %s", w, logged.String())
		}
	}

	g := s.guardRulesFor(rec)
	if !slices.Equal(g.AskFirst, []string{"make e2e"}) || !slices.Contains(g.Writable, fixtures) || slices.Contains(g.Writable, filepath.Join(p.Home, "sub")) {
		t.Errorf("guard: ask first %q, writable %q", g.AskFirst, g.Writable)
	}
	if d := g.Judge("Bash", map[string]any{"command": "make e2e"}); d == nil || !d.Ask || d.Rule != guard.RuleExcluded {
		t.Errorf("make e2e: %+v", d)
	}
	if d := g.Judge("Write", map[string]any{"file_path": filepath.Join(fixtures, "a.json")}); d != nil {
		t.Errorf("a write in the write folder: %+v", d)
	}
	// What it launched with holds: config.toml no longer lists it.
	proj, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(thread.Path(proj, rec.Thread, accessRecord)), 0o755); err != nil {
		t.Fatal(err)
	}
	s.recordLaunch(rec, mustResolve(t, s, rec))
	if _, ok := readLaunchRecord(proj, rec.Thread); !ok {
		t.Fatalf("no launch record: %s", logged.String())
	}
	writeConfig(t, "[projects.demo.sandbox]\nexcluded_commands = [\"cargo bench\"]\n")
	if g := s.guardRulesFor(rec); !slices.Equal(g.AskFirst, []string{"cargo bench", "make e2e"}) || slices.Contains(g.Writable, fixtures) {
		t.Errorf("after the change: ask first %q, writable %q", g.AskFirst, g.Writable)
	}
	// The guard off asks still: the question is the user's, as escapes'.
	writeConfig(t, "[projects.demo]\nguard = false\n[projects.demo.sandbox]\nexcluded_commands = [\"cargo bench\"]\n")
	if g := s.guardRulesFor(rec); g.Floor != nil || g.Rules != nil || !slices.Contains(g.AskFirst, "cargo bench") {
		t.Errorf("guard off: %+v", g)
	} else if d := g.Judge("Bash", map[string]any{"command": "cargo bench"}); d == nil || !d.Ask {
		t.Errorf("guard off, cargo bench: %+v", d)
	}
	codex := rec
	codex.Agent, codex.Thread = "codex", "t-0002"
	if g := s.guardRulesFor(codex); g.AskFirst != nil {
		t.Errorf("codex: ask first %q", g.AskFirst)
	}
	yolo := rec
	yolo.Yolo = true
	if g := s.guardRulesFor(yolo); g.AskFirst != nil {
		t.Errorf("yolo: ask first %q", g.AskFirst)
	}
}

// mustResolve is rec's access set, as its launch resolves it.
func mustResolve(t *testing.T, s *Server, rec SessionRecord) proto.AccessSet {
	t.Helper()
	set, err := s.resolveAccess(accessInput{role: rec.Role, slug: rec.Project, cwd: rec.Cwd, agent: rec.Agent, goos: caps.OS}, loadLaunchConfig())
	if err != nil {
		t.Fatal(err)
	}
	return set
}
