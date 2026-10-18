package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// legacySandbox is launchConfig.sandbox as tm 0.37.0 had it, before
// the rules (T578): the oracle the resolver's launch settings must equal.
// sandbox is project slug's sandbox lists and presets for its threads on
// platform goos (docs/SPEC.md §5.2), the presets on that apply there, and
// a note for each setting that doesn't, which the launch skips:
// config.toml keeps it. A config.toml that doesn't load, or an entry
// that fails the sandbox checks, opens nothing (fails closed) and gives
// one note saying why, for the log and the launch's warnings.
func legacySandbox(l launchConfig, slug, goos string) (lists agent.SandboxLists, presets, skipped []string) {
	err := l.err
	var t config.ThreadSandbox
	if err == nil {
		t, err = l.cfg.ThreadSandbox(slug)
	}
	if err != nil {
		return lists, nil, []string{fmt.Sprintf("project %s: sandbox lists and presets dropped, the thread starts without them (fix or remove the entry: tm settings, Sandbox tab): %v", slug, err)}
	}
	skip := func(key, what string) bool {
		if config.SandboxApplies(key, goos) {
			return false
		}
		skipped = append(skipped, fmt.Sprintf("sandbox %s is set but doesn't apply on %s: skipped", what, config.OSName(goos)))
		return true
	}
	if len(t.ExcludedCommands) > 0 && !skip(config.SandboxExcluded, "excluded commands") {
		lists.Excluded = t.ExcludedCommands
	}
	if len(t.Allow) > 0 && !skip(config.SandboxAllow, "allow rules") {
		lists.Allow = t.Allow
	}
	for _, k := range t.Presets {
		if skip(k, "preset "+strings.ReplaceAll(k, "_", " ")) {
			continue
		}
		presets = append(presets, k)
		switch k {
		case config.SandboxDownloads:
			lists.Domains = config.BrowserDomains
		case config.SandboxDevServers:
			lists.LocalBinding = true
		}
	}
	return lists, presets, skipped
}

// accessConfigs are the config.toml files of TestAccessLaunchEqual:
// every way the sandbox lists, presets, yolo, escapes and the
// coordinator's merge rule combine today.
var accessConfigs = []string{
	"",
	`[defaults.sandbox]
allow = ["Bash(npx playwright *)"]

[projects.demo.sandbox]
excluded_commands = ["npx playwright *"]
`,
	`[defaults.sandbox]
browser_testing = true
excluded_commands = ["npx playwright *", "docker compose *"]
allow = ["Bash(npx playwright *)", "WebFetch(domain:playwright.dev)"]
`,
	`[defaults.sandbox]
browser_testing = true

[projects.demo.sandbox]
browser_downloads = true
local_dev_servers = true
`,
	`[defaults.sandbox]
browser_testing = true
browser_downloads = true
local_dev_servers = true
excluded_commands = ["docker compose *"]

[projects.demo.sandbox]
browser_downloads = false
excluded_commands = []
`,
	`[defaults.sandbox]
excluded_commands = ["make a$X"]
`,
	`[defaults]
yolo = true
sandbox_escapes = false
coordinator_merges = true
guard_mode = "learn"
guard_off = ["push-default"]
guard_allow = [{ rule = "force-push", command = "git push --force-with-lease origin tm/*" }]

[defaults.env]
NODE_AUTH_TOKEN = { from = "gh-auth-token" }

[projects.demo]
sandbox_escapes = true
guard = false
`,
	`[projects.demo]
coordinator_merges = true
merge = "thread"
`,
	"this is = not [toml",
}

// TestAccessLaunchEqual: the launch settings the rules give (file
// access, sandbox lists and presets, their notes, yolo and escapes, and
// the files and arguments each agent renders from them) are today's,
// for every config, platform, role and folder.
func TestAccessLaunchEqual(t *testing.T) {
	testPaths(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	newWatchProject(t)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME"), Socket: os.Getenv("TERMINATR_SOCKET")}}}
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	root := realPath(t.TempDir())
	repo, wt, plain := filepath.Join(root, "repo"), filepath.Join(root, "wt"), filepath.Join(root, "plain")
	os.MkdirAll(plain, 0o755)
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c"},
		{"-C", repo, "worktree", "add", "-q", "-b", "x", wt},
	} {
		if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	for i, body := range accessConfigs {
		writeConfig(t, body)
		lc := loadLaunchConfig()
		for _, goos := range []string{config.OSMac, config.OSLinux, config.OSWindows} {
			for _, role := range []string{proto.RoleThread, proto.RoleCoordinator, proto.RoleShell} {
				for _, cwd := range []string{wt, repo, plain} {
					for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
						name := strings.Join([]string{"config", string(rune('0' + i)), goos, role, filepath.Base(cwd)}, "/")
						slug := "demo"
						if role == proto.RoleShell {
							slug = ""
						}
						set, err := s.resolveAccess(accessInput{role: role, slug: slug, cwd: cwd, agent: "claude", goos: goos, yolo: flags[0], noEscapes: flags[1]}, lc)
						if err != nil {
							t.Fatalf("%s: %v", name, err)
						}
						want, err := s.accessFor(role, slug, cwd, lc)
						if err != nil {
							t.Fatal(err)
						}
						got := launchAccess(set)
						if !reflect.DeepEqual(got, want) {
							t.Errorf("%s: access\n got %+v\nwant %+v", name, got, want)
						}
						var lists agent.SandboxLists
						var presets, skipped []string
						wl, wp, ws := agent.SandboxLists{}, []string(nil), []string(nil)
						if role == proto.RoleThread {
							lists, presets, skipped = launchSandbox(set)
							wl, wp, ws = legacySandbox(lc, slug, goos)
						} else if !launchSandboxEmpty(set) {
							t.Errorf("%s: sandbox rules for a %s", name, role)
						}
						if !reflect.DeepEqual(lists, wl) || !slices.Equal(presets, wp) || !slices.Equal(skipped, ws) {
							t.Errorf("%s: sandbox\n got %+v %q %q\nwant %+v %q %q", name, lists, presets, skipped, wl, wp, ws)
						}
						if y, n := launchFlags(set); y != flags[0] || n != flags[1] {
							t.Errorf("%s: yolo %v, no escapes %v; want %v", name, y, n, flags)
						}
						for _, an := range reg.Names() {
							a, _ := reg.Get(an)
							spec := agent.LaunchSpec{Role: agent.Role(role), SessionID: "s-1", AgentSID: "00000000-0000-4000-8000-000000000000",
								Cwd: cwd, RuntimeDir: filepath.Join(root, "rt"), Kickoff: "Start.", TMBin: "/usr/local/bin/tm", Socket: s.opts.Paths.Socket}
							old, now := spec, spec
							old.Access, old.Sandbox, old.Yolo, old.NoSandboxEscapes = want, wl, flags[0], flags[1]
							now.Access, now.Sandbox = got, lists
							now.Yolo, now.NoSandboxEscapes = launchFlags(set)
							lo, err1 := a.Launch(old)
							ln, err2 := a.Launch(now)
							if (err1 == nil) != (err2 == nil) || !reflect.DeepEqual(lo.Argv, ln.Argv) || !reflect.DeepEqual(lo.Files, ln.Files) || !slices.Equal(lo.Warnings, ln.Warnings) {
								t.Errorf("%s: %s renders another launch (%v, %v)", name, an, err1, err2)
							}
						}
					}
				}
			}
		}
	}
}

// launchSandboxEmpty reports whether set has no sandbox rule.
func launchSandboxEmpty(set proto.AccessSet) bool {
	l, p, n := launchSandbox(set)
	return l.Empty() && len(p) == 0 && len(n) == 0
}
