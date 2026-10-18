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

// TestSandboxPresetsLaunch: a preset on maps to local binding where it
// applies (macOS) and is skipped with a note elsewhere, config.toml
// keeping it; only local dev servers open local binding, and only
// browser downloads the download hosts: a v0.35 browser_testing = true
// opens none (it never asked about them). Browser testing adds its
// guidance after the brief, which says whether downloads and local
// ports are open.
func TestSandboxPresetsLaunch(t *testing.T) {
	testPaths(t)
	writeConfig(t, `[defaults.sandbox]
browser_testing = true
excluded_commands = ["npx playwright *"]
`)
	lc := loadLaunchConfig()
	lists, presets, skipped := lc.sandbox("demo", config.OSMac)
	if lists.LocalBinding || !slices.Equal(presets, []string{config.SandboxBrowser}) || len(skipped) != 0 ||
		!slices.Equal(lists.Excluded, []string{"npx playwright *"}) || lists.Domains != nil {
		t.Errorf("macOS: %+v %q %q", lists, presets, skipped)
	}
	lists, presets, skipped = lc.sandbox("demo", config.OSLinux)
	if lists.LocalBinding || lists.Domains != nil || len(presets) != 0 || !slices.Equal(lists.Excluded, []string{"npx playwright *"}) ||
		!slices.Equal(skipped, []string{"sandbox preset browser testing is set but doesn't apply on Linux: skipped"}) {
		t.Errorf("Linux: %+v %q %q", lists, presets, skipped)
	}

	writeConfig(t, `[defaults.sandbox]
browser_testing = true

[projects.demo.sandbox]
browser_downloads = true
`)
	lc = loadLaunchConfig()
	if lists, presets, _ = lc.sandbox("demo", config.OSMac); !slices.Equal(lists.Domains, config.BrowserDomains) || !slices.Equal(presets, []string{config.SandboxBrowser, config.SandboxDownloads}) {
		t.Errorf("browser downloads: %+v %q", lists, presets)
	}
	if lists, _, _ = lc.sandbox("other", config.OSMac); lists.Domains != nil {
		t.Errorf("browser testing alone opened %q", lists.Domains)
	}
	if lists, _, skipped = lc.sandbox("demo", config.OSLinux); lists.Domains != nil || len(skipped) != 2 {
		t.Errorf("Linux: %+v %q", lists, skipped)
	}

	rt := t.TempDir()
	brief := filepath.Join(t.TempDir(), "brief.md")
	os.WriteFile(brief, []byte("# Thread t-0001\n"), 0o600)
	if got, err := presetBrief(rt, brief, nil); err != nil || got != brief {
		t.Errorf("no presets: %q, %v", got, err)
	}
	if got, err := presetBrief(rt, brief, []string{config.SandboxDevServers}); err != nil || got != brief {
		t.Errorf("dev servers only: %q, %v", got, err)
	}
	got, err := presetBrief(rt, brief, []string{config.SandboxBrowser})
	b, _ := os.ReadFile(got)
	if err != nil || filepath.Dir(got) != rt || !strings.HasPrefix(string(b), "# Thread t-0001\n") || !strings.Contains(string(b), "--single-process") ||
		!strings.Contains(string(b), "reuseContext: true") || !strings.Contains(string(b), browserPortsClosed) || !strings.Contains(string(b), browserNoDownloads) || strings.Contains(string(b), "%!") {
		t.Errorf("browser testing: %q, %v\n%s", got, err, b)
	}
	got, _ = presetBrief(rt, brief, []string{config.SandboxBrowser, config.SandboxDownloads, config.SandboxDevServers})
	if b, _ = os.ReadFile(got); !strings.Contains(string(b), browserPortsOpen) || !strings.Contains(string(b), browserDownloads) {
		t.Errorf("browser testing with downloads and local dev servers:\n%s", b)
	}
	got, _ = presetBrief(rt, brief, []string{config.SandboxDownloads})
	if b, _ = os.ReadFile(got); !strings.Contains(string(b), "## Sandbox: browser downloads") || !strings.Contains(string(b), browserDownloads) || strings.Contains(string(b), "--single-process") {
		t.Errorf("browser downloads alone:\n%s", b)
	}
	if got, err = presetBrief(rt, "", []string{config.SandboxBrowser}); err != nil || got == "" {
		t.Errorf("no brief: %q, %v", got, err)
	}
	big := filepath.Join(t.TempDir(), "big.md")
	os.WriteFile(big, make([]byte, maxBrief+1), 0o600)
	if _, err = presetBrief(rt, big, []string{config.SandboxBrowser}); err == nil {
		t.Errorf("a brief over %d bytes was read", maxBrief)
	}
}

// TestSandboxDroppedSaid: an entry that fails the checks drops the
// project's sandbox lists and presets (fails closed), and the launch
// says why: project, key, the entry and its rule.
func TestSandboxDroppedSaid(t *testing.T) {
	testPaths(t)
	writeConfig(t, `[defaults.sandbox]
browser_testing = true

[projects.demo.sandbox]
excluded_commands = ["make a$X"]
allow = ["Bash(npx playwright *)"]
`)
	lists, presets, notes := loadLaunchConfig().sandbox("demo", config.OSMac)
	if lists.Excluded != nil || lists.Allow != nil || lists.LocalBinding || lists.Domains != nil || len(presets) != 0 {
		t.Errorf("not failed closed: %+v %q", lists, presets)
	}
	if len(notes) != 1 {
		t.Fatalf("notes %q", notes)
	}
	for _, want := range []string{"demo", "projects.demo.sandbox.excluded_commands", "make a$X", "has a variable", "dropped"} {
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
write = ["~/src/fixtures", "/`+strings.TrimPrefix(filepath.ToSlash(p.Home), "/")+`/sub"]
`)
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
	if !strings.Contains(logged.String(), "write folder") {
		t.Errorf("the folder in tm's own isn't said to be skipped: %s", logged.String())
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
