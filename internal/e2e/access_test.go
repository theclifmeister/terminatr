package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAccessCommands: a thread's launch keeps its access in its folder;
// tm access show lists it with each rule's source and marks what a
// config.toml change would bring at a restart; tm access explain says
// what a thread does with a command (T574 phase 1).
func TestAccessCommands(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	writeConfig(t, env, "[projects.demo.sandbox]\nexcluded_commands = [\"npx playwright *\"]\nallow = [\"Bash(npx playwright *)\"]\n")
	env.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, env, "demo", "t-0001")
	env.WaitState(th, "idle", agentWait)
	if _, err := os.Stat(filepath.Join(projDir, "threads", "t-0001", "access.json")); err != nil {
		t.Fatalf("no launch record: %v", err)
	}

	var res proto.AccessShowResult
	if err := json.Unmarshal([]byte(env.MustCLI("access", "show", "demo", "--thread", "t-0001", "--json")), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Recorded || res.Changes != 0 || res.Agent != "claude" || res.Launched.IsZero() {
		t.Fatalf("show: %+v", res.AccessSet)
	}
	found := false
	for _, r := range res.Rules {
		if r.Kind == proto.AccessExcluded && r.Value == "npx playwright *" && r.Source == "demo" {
			found = true
		}
	}
	if !found {
		t.Errorf("no excluded command from demo in %+v", res.Rules)
	}

	writeConfig(t, env, "[projects.demo.sandbox]\nexcluded_commands = [\"make bench\"]\n")
	out := env.MustCLI("access", "show", "demo", "--thread", "t-0001")
	for _, want := range []string{"Effective access · demo · t-0001 (claude", "RESTART TO APPLY: removed since launch", "RESTART TO APPLY: added since launch",
		"3 changes since launch: tm thread restart t-0001 applies them", "worktree"} {
		if !strings.Contains(out, want) {
			t.Errorf("show lacks %q:\n%s", want, out)
		}
	}
	if out := env.MustCLI("access", "show", "demo"); !strings.Contains(out, "a new thread") || !strings.Contains(out, "make bench") {
		t.Errorf("project:\n%s", out)
	}

	out = env.MustCLI("access", "explain", "demo", "--", "make", "bench")
	if !strings.Contains(out, "runs OUTSIDE the sandbox") || !strings.Contains(out, "asks") {
		t.Errorf("explain:\n%s", out)
	}
	if r := env.CLI("access", "explain", "demo"); r.Code != 2 {
		t.Errorf("explain without a command: %+v", r)
	}
}
