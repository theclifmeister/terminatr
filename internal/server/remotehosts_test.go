package server

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent/codex"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestRemoteHost: a remote URL's host name, in each form git takes; none
// for a local path, a file URL, an address or a one-word name.
func TestRemoteHost(t *testing.T) {
	for u, want := range map[string]string{
		"https://github.com/o/r.git":              "github.com",
		"https://git@GitHub.com:443/o/r":          "github.com",
		"ssh://git@ssh.dev.azure.com:22/v3/o/p/r": "ssh.dev.azure.com",
		"git@github.com:o/r.git":                  "github.com",
		"https://dev.azure.com/org/p/_git/r":      "dev.azure.com",
		"/srv/git/r.git":                          "",
		"../r.git":                                "",
		"file:///srv/r.git":                       "",
		`C:\repos\r`:                              "",
		"https://127.0.0.1/r":                     "",
		"https://[::1]/r":                         "",
		"git@localhost:r":                         "",
		"https://gitlab/r":                        "",
		"https://evil.localhost/r":                "",
		"https://exa_mple.com/r":                  "",
		"ext::sh -c touch% /tmp/x":                "",
	} {
		if got := remoteHost(u); got != want {
			t.Errorf("remoteHost(%q) = %q, want %q", u, got, want)
		}
	}
}

// TestRemoteHostRules: a Codex thread reaches its repo's remote hosts,
// read from the repository's config file (not a worktree config, which
// a thread writes), each once, as tm rules; a Claude thread doesn't get
// them, and under yolo they aren't used.
func TestRemoteHostRules(t *testing.T) {
	testPaths(t)
	t.Setenv("HOME", t.TempDir())
	newWatchProject(t)
	repo := t.TempDir()
	gittest.Run(t, repo, "init", "-q", "-b", "main")
	gittest.Run(t, repo, "remote", "add", "origin", "git@github.com:o/r.git")
	gittest.Run(t, repo, "remote", "add", "mirror", "https://dev.azure.com/org/p/_git/r")
	gittest.Run(t, repo, "remote", "set-url", "--push", "origin", "https://github.com/o/r.git")
	gittest.Run(t, repo, "remote", "add", "local", "/srv/r.git")
	common := filepath.Join(repo, ".git")
	os.WriteFile(filepath.Join(common, "config.worktree"), []byte("[remote \"x\"]\n\turl = https://evil.example.com/r\n"), 0o600)
	writeConfig(t, "[defaults.sandbox]\nbrowser_downloads = true\n")
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	tree := &gitx.Tree{Dir: repo, GitDir: common, CommonDir: common}
	hosts := func(agent string, yolo bool) (rules []string, domains []string) {
		t.Helper()
		set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", cwd: repo, tree: tree, agent: agent, goos: config.OSMac, yolo: yolo}, loadLaunchConfig())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range set.Rules {
			if r.Kind == proto.AccessHost && r.Note == remoteHostNote {
				if r.Source != proto.AccessFromTM || !slices.Equal(r.Agents, []string{"codex"}) || r.Where != proto.AccessInside {
					t.Errorf("rule %+v", r)
				}
				if r.Skipped == "" {
					rules = append(rules, r.Value)
				}
			}
		}
		l, _, _ := launchSandbox(set)
		return rules, l.Domains
	}
	rules, domains := hosts("codex", false)
	if !slices.Equal(rules, []string{"github.com", "dev.azure.com"}) {
		t.Errorf("codex rules %q", rules)
	}
	if !slices.Contains(domains, "github.com") || !slices.Contains(domains, "dev.azure.com") || !slices.Contains(domains, "registry.npmjs.org") || slices.Contains(domains, "evil.example.com") {
		t.Errorf("codex domains %q", domains)
	}
	if rules, domains := hosts("claude", false); rules != nil || slices.Contains(domains, "github.com") {
		t.Errorf("claude: rules %q domains %q", rules, domains)
	}
	if rules, domains := hosts("codex", true); rules != nil || domains != nil {
		t.Errorf("yolo: rules %q domains %q", rules, domains)
	}
	// On Windows and Linux a Codex thread keeps its open network: its
	// hosts show, not used, and a network rule says so, with the
	// launch's note.
	for goos, want := range map[string]string{config.OSWindows: "open", config.OSLinux: "open"} {
		set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", cwd: repo, tree: tree, agent: "codex", goos: goos}, loadLaunchConfig())
		if err != nil {
			t.Fatal(err)
		}
		var network []string
		for _, r := range set.Rules {
			if r.Kind == proto.AccessHost && r.Skipped == "" {
				t.Errorf("%s: host %s used", goos, r.Value)
			}
			if r.Kind == proto.AccessNetwork {
				network = append(network, r.Value)
				if r.Note != codex.OpenNote(goos) {
					t.Errorf("%s note %q", goos, r.Note)
				}
			}
		}
		if !slices.Equal(network, []string{want}) {
			t.Errorf("%s: network rules %q", goos, network)
		}
	}
	if set, _ := s.resolveAccess(accessInput{role: proto.RoleThread, slug: "demo", cwd: repo, tree: tree, agent: "claude", goos: config.OSLinux}, loadLaunchConfig()); slices.ContainsFunc(set.Rules, func(r proto.AccessRule) bool { return r.Kind == proto.AccessNetwork }) {
		t.Error("a Claude thread got a network rule")
	}
	// A host both a rule and a remote give is one domain.
	gittest.Run(t, repo, "remote", "set-url", "mirror", "https://registry.npmjs.org/x")
	if _, domains := hosts("codex", false); len(domains) != len(config.BrowserDomains)+1 {
		t.Errorf("once each: %q", domains)
	}
}
