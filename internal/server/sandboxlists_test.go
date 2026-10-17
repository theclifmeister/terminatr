package server

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
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
