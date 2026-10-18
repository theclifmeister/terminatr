package server

import (
	"context"
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
	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// wantSandbox is what a thread's launch gets from config.ThreadSandbox,
// worked out apart from the rules: the oracle the resolver's launch
// settings must equal. Each list's entries given where it applies,
// local ports when on; a note per kind that doesn't apply, and per
// entry refused.
func wantSandbox(l launchConfig, slug, goos string) (lists agent.SandboxLists, guides, skipped []string) {
	if l.err != nil {
		return lists, nil, []string{fmt.Sprintf("project %s: sandbox rules left out, the thread starts without them (fix config.toml: tm settings, Access tab): %v", slug, l.err)}
	}
	t := l.cfg.ThreadSandbox(slug)
	kind := map[string]string{config.SandboxHosts: "host", config.SandboxGuides: "guide", config.SandboxExcluded: "excluded", config.SandboxAllow: "allow"}
	words := map[string]string{config.SandboxHosts: "hosts", config.SandboxGuides: "guidance texts", config.SandboxExcluded: "excluded commands", config.SandboxAllow: "allow rules"}
	said := map[string]bool{}
	skip := func(key, what string) bool {
		if config.SandboxApplies(key, goos) {
			return false
		}
		if !said[key] {
			said[key] = true
			skipped = append(skipped, fmt.Sprintf("sandbox %s skipped: doesn't apply on %s", what, config.OSName(goos)))
		}
		return true
	}
	for _, r := range t.Rules {
		switch {
		case r.Dropped || r.Replaced:
		case r.Refused != "":
			skipped = append(skipped, fmt.Sprintf("sandbox %s %q refused, the thread starts without it: %s", kind[r.Key], r.Value, r.Refused))
		case skip(r.Key, words[r.Key]):
		case r.Key == config.SandboxHosts:
			lists.Domains = append(lists.Domains, r.Value)
		case r.Key == config.SandboxGuides:
			guides = append(guides, r.Value)
		case r.Key == config.SandboxExcluded:
			lists.Excluded = append(lists.Excluded, r.Value)
		case r.Key == config.SandboxAllow:
			lists.Allow = append(lists.Allow, r.Value)
		}
	}
	if t.LocalPortsSet && !skip(config.SandboxLocalPorts, "local ports") {
		lists.LocalBinding = t.LocalPorts
	}
	return lists, guides, skipped
}

// accessConfigs are the config.toml files of TestAccessLaunchEqual:
// every way the sandbox rules (tm 0.37's preset switches too), drops,
// refused entries, yolo, escapes and the coordinator's merge rule
// combine.
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
excluded_commands = ["make a$X", "make e2e"]
hosts = ["registry.npmjs.org", "localhost"]
local_ports = true

[projects.demo.sandbox]
hosts = ["cdn.playwright.dev"]
guides = ["browser-downloads"]
local_ports = false
drop = ["make e2e"]
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

// pinTree is folder dir's checkout of repo (gitx.Pin), as threadTree
// gives a thread recorded there; nil when it is none.
func pinTree(repo, dir string) *gitx.Tree {
	t, err := gitx.Pin(context.Background(), repo, dir)
	if err != nil {
		return nil
	}
	return t
}

// TestAccessLaunchEqual: the launch settings the rules give (file
// access, sandbox lists and guidance, their notes, yolo and escapes,
// and the files and arguments each agent renders from them) are what
// config.toml says, for every config, platform, role and folder.
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
						tree := pinTree(repo, cwd)
						set, err := s.resolveAccess(accessInput{role: role, slug: slug, cwd: cwd, tree: tree, agent: "claude", goos: goos, yolo: flags[0], noEscapes: flags[1]}, lc)
						if err != nil {
							t.Fatalf("%s: %v", name, err)
						}
						want, err := s.accessFor(role, slug, tree, lc)
						if err != nil {
							t.Fatal(err)
						}
						got := launchAccess(set)
						if role == proto.RoleThread {
							// On top of accessFor's: the other threads' temp
							// folders and the shared ones (T587, tempRules).
							if !slices.Contains(got.NoRead, home.ThreadTempRoot()) {
								t.Errorf("%s: no read deny of %s: %q", name, home.ThreadTempRoot(), got.NoRead)
							}
							want.NoRead = got.NoRead
						}
						if !reflect.DeepEqual(got, want) {
							t.Errorf("%s: access\n got %+v\nwant %+v", name, got, want)
						}
						var lists agent.SandboxLists
						var presets, skipped []string
						wl, wp, ws := agent.SandboxLists{}, []string(nil), []string(nil)
						if role == proto.RoleThread {
							lists, presets, skipped = launchSandbox(set)
							wl, wp, ws = wantSandbox(lc, slug, goos)
							if flags[0] {
								// Yolo has no sandbox (T574 case 4): of the
								// lists only the allow rules are given.
								wl, wp = agent.SandboxLists{Allow: wl.Allow}, nil
							}
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

// TestAccessYolo: under yolo a thread has no sandbox (T574 case 4): what
// opens or leaves it shows as not used and isn't launched, its allow
// rules stay, the guard's settings but its floor show as not used, and
// the floor names the settings file and the project folder.
func TestAccessYolo(t *testing.T) {
	testPaths(t)
	t.Setenv("HOME", t.TempDir())
	newWatchProject(t)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	writeConfig(t, `[defaults]
guard_off = ["push-default"]

[defaults.sandbox]
browser_testing = true
browser_downloads = true
local_dev_servers = true
excluded_commands = ["npx playwright *"]
allow = ["WebFetch(domain:playwright.dev)"]
`)
	lc := loadLaunchConfig()
	set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", agent: "claude", goos: config.OSMac, yolo: true}, lc)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range set.Rules {
		seen[r.Kind] = true
		want := ""
		switch r.Kind {
		case proto.AccessExcluded, proto.AccessHost, proto.AccessLocalPorts, proto.AccessPreset, proto.AccessGuide, proto.AccessEscapes:
			want = proto.AccessYoloNoSandbox
		case proto.AccessGuardMode, proto.AccessGuardRuleOff:
			want = proto.AccessYoloFloor
		}
		if r.Skipped != want {
			t.Errorf("%s %s under yolo: skipped %q, want %q", r.Kind, r.Value, r.Skipped, want)
		}
		if r.Kind == proto.AccessGuardFloor && (!strings.Contains(r.Value, "writing your settings") || !strings.Contains(r.Value, "writing the project folder")) {
			t.Errorf("floor %q", r.Value)
		}
		if r.Kind == proto.AccessGuardOn && r.Note != proto.AccessYoloFloor {
			t.Errorf("guard row note %q", r.Note)
		}
	}
	for _, k := range []string{proto.AccessExcluded, proto.AccessHost, proto.AccessAllow, proto.AccessGuardMode, proto.AccessGuardRuleOff} {
		if !seen[k] {
			t.Errorf("no %s rule", k)
		}
	}
	lists, presets, skipped := launchSandbox(set)
	if !reflect.DeepEqual(lists, agent.SandboxLists{Allow: []string{"WebFetch(domain:playwright.dev)"}}) || presets != nil || skipped != nil {
		t.Errorf("yolo launch: %+v %q %q", lists, presets, skipped)
	}
}
