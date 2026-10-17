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

// TestSandboxPresetsLaunch: a preset on maps to local binding where it
// applies (macOS) and is skipped with a note elsewhere, config.toml
// keeping it; only local dev servers open local binding; browser testing adds its guidance after the brief.
func TestSandboxPresetsLaunch(t *testing.T) {
	testPaths(t)
	writeConfig(t, `[defaults.sandbox]
browser_testing = true
excluded_commands = ["npx playwright *"]
`)
	lc := loadLaunchConfig()
	lists, presets, skipped := lc.sandbox("demo", config.OSMac)
	if lists.LocalBinding || !slices.Equal(presets, []string{config.SandboxBrowser}) || len(skipped) != 0 ||
		!slices.Equal(lists.Excluded, []string{"npx playwright *"}) {
		t.Errorf("macOS: %+v %q %q", lists, presets, skipped)
	}
	lists, presets, skipped = lc.sandbox("demo", config.OSLinux)
	if lists.LocalBinding || len(presets) != 0 || !slices.Equal(lists.Excluded, []string{"npx playwright *"}) ||
		!slices.Equal(skipped, []string{"sandbox preset browser testing is set but doesn't apply on Linux: skipped"}) {
		t.Errorf("Linux: %+v %q %q", lists, presets, skipped)
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
	if err != nil || filepath.Dir(got) != rt || !strings.HasPrefix(string(b), "# Thread t-0001\n") || !strings.Contains(string(b), "--single-process") {
		t.Errorf("browser testing: %q, %v\n%s", got, err, b)
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
	if lists.Excluded != nil || lists.Allow != nil || lists.LocalBinding || len(presets) != 0 {
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
