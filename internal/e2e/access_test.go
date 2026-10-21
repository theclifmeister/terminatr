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
// what a thread does with a command (T574 phase 1). The Bash allow rule
// covering the excluded command is no rule of its own: it makes that one
// run without asking (#655), so the change is one rule out, one in.
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
	for _, want := range []string{"Effective access · demo · t-0001 (claude", "without asking", "RESTART TO APPLY: removed since launch", "RESTART TO APPLY: added since launch",
		"2 changes since launch: tm thread restart t-0001 applies them", "worktree"} {
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

// TestNoAccessPreset: tm has no presets (T642): tm access preset is a
// usage error, and a file with tm 0.37's switches still reads, as the
// rules they unfold into.
func TestNoAccessPreset(t *testing.T) {
	env, _, _ := threadEnv(t)
	writeConfig(t, env, "[defaults.sandbox]\nbrowser_downloads = true\nlocal_dev_servers = true\n")
	for _, args := range [][]string{{"access", "preset", "list"}, {"access", "preset", "add", "browser-downloads", "demo"}} {
		if r := env.CLI(args...); r.Code != 2 || strings.Contains(r.Stdout+r.Stderr, "preset add") {
			t.Errorf("%v: %+v", args, r)
		}
	}
	env.MustCLI("server", "start")
	if out := env.MustCLI("access", "show", "demo"); !strings.Contains(out, "local-ports on") || !strings.Contains(out, "host cdn.playwright.dev") || strings.Contains(out, "preset") {
		t.Errorf("show:\n%s", out)
	}
}
