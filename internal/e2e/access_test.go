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

// TestAccessPreset: tm access preset lists the presets and the rules
// each adds; add writes those a project's threads don't have yet as
// plain rules, and says which were there already (T574 §3.3).
func TestAccessPreset(t *testing.T) {
	env, _, _ := threadEnv(t)
	writeConfig(t, env, "[defaults.sandbox]\nhosts = [\"registry.npmjs.org\"]\n")
	if out := env.MustCLI("access", "preset", "list"); !strings.Contains(out, "browser-downloads") || !strings.Contains(out, "Local dev servers") {
		t.Errorf("list:\n%s", out)
	}
	if out := env.MustCLI("access", "preset", "show", "browser-downloads"); !strings.Contains(out, "host") || !strings.Contains(out, "storage.googleapis.com") || !strings.Contains(out, "guide") {
		t.Errorf("show:\n%s", out)
	}
	out := env.MustCLI("access", "preset", "add", "browser-downloads", "demo")
	for _, want := range []string{"already there for demo: host registry.npmjs.org", "added to demo: host cdn.playwright.dev", "added to demo: guide browser-downloads", "threads of demo get them from their next start"} {
		if !strings.Contains(out, want) {
			t.Errorf("add lacks %q:\n%s", want, out)
		}
	}
	data, err := os.ReadFile(filepath.Join(env.Home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[projects.demo.sandbox]") || !strings.Contains(string(data), `"cdn.playwright.dev"`) || strings.Contains(string(data), "browser_downloads") {
		t.Errorf("config.toml:\n%s", data)
	}
	if out := env.MustCLI("access", "preset", "add", "local-dev-servers", "--all"); !strings.Contains(out, "added to all projects: local-ports on") {
		t.Errorf("--all:\n%s", out)
	}
	env.MustCLI("server", "start")
	if out := env.MustCLI("access", "show", "demo"); !strings.Contains(out, "local-ports on") || !strings.Contains(out, "host cdn.playwright.dev") {
		t.Errorf("show after adding:\n%s", out)
	}
	if r := env.CLI("access", "preset", "add", "browser-testing", "demo", "--all"); r.Code != 2 {
		t.Errorf("a project and --all: %+v", r)
	}
	if r := env.CLI("access", "preset", "add", "nope", "demo"); r.Code == 0 || !strings.Contains(r.Stderr, "browser-testing") {
		t.Errorf("unknown preset: %+v", r)
	}
}
