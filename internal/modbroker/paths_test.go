package modbroker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// fixture is a home folder with secrets, tm's folder, a mod's own folder,
// a read root (notes) with links out of it, and a write root (out).
type fixture struct {
	home string
	p    Paths
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		".ssh/id_rsa":                         "KEY",
		".config/gh/hosts.yml":                "TOKEN",
		".config/glab-cli/config.yml":         "TOKEN",
		".terminatr/config.toml":              "[defaults]",
		".terminatr/state/mods/m/own.txt":     "mine",
		".terminatr/state/mods/other/x.txt":   "theirs",
		".claude/.credentials.json":           "TOKEN",
		"notes/a.txt":                         "a",
		"notes/sub/b.txt":                     "b",
		"notes/repo/.git/config":              "[core]",
		"notes/.env":                          "SECRET=1",
		"notes/.env.local":                    "SECRET=1",
		"out/keep.txt":                        "k",
		"elsewhere/c.txt":                     "c",
		"Library/Keychains/login.keychain-db": "KEYS",
	}
	for name, text := range files {
		p := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{
		"notes/link-ssh":  "../.ssh",
		"notes/link-tm":   "../.terminatr",
		"notes/link-else": "../elsewhere",
		"notes/link-up":   "..",
		"notes/dangling":  "../.ssh/new_key",
		"out/to-notes":    "../notes",
		"elsewhere/in":    "../notes",
	}
	for name, target := range links {
		if err := os.Symlink(filepath.FromSlash(target), filepath.Join(home, filepath.FromSlash(name))); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
	}
	getenv := func(string) string { return "" }
	p := Paths{
		Home: home, TMHome: filepath.Join(home, ".terminatr"), Own: filepath.Join(home, ".terminatr", "state", "mods", "m"),
		Read: []string{"~/notes"}, Write: []string{"~/out"},
		Secrets: Real(append(agent.Secrets(nil, home, getenv), Keychains(home, getenv)...)),
	}
	return fixture{home: home, p: p.resolve()}
}

func TestPathsCheck(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		path  string
		write bool
		ok    bool
	}{
		{"own.txt", false, true},
		{"own.txt", true, true},
		{"new/deep/file.json", true, true},
		{"../other/x.txt", false, false},
		{"../../../config.toml", false, false},
		{"~/.terminatr/config.toml", false, false},
		{"~/.terminatr/state/mods/m/own.txt", false, true},
		{"~/notes/a.txt", false, true},
		{"~/notes/a.txt", true, false},
		{"~/notes/sub/b.txt", false, true},
		{"~/notes/../.ssh/id_rsa", false, false},
		{"~/notes/link-ssh/id_rsa", false, false},
		{"~/notes/link-tm/config.toml", false, false},
		{"~/notes/link-else/c.txt", false, false},
		{"~/notes/link-up/elsewhere/c.txt", false, false},
		{"~/elsewhere/in/a.txt", false, false}, // by name outside the roots, wherever it leads
		{"~/notes/dangling", true, false},
		{"~/notes/repo/.git/config", false, false},
		{"~/notes/.env", false, false},
		{"~/notes/.env.local", false, false},
		{"~/.ssh/id_rsa", false, false},
		{"~/.config/gh/hosts.yml", false, false},
		{"~/.config/glab-cli/config.yml", false, false},
		{"~/.claude/.credentials.json", false, false},
		{"~/Library/Keychains/login.keychain-db", false, false},
		{"~/out/keep.txt", true, true},
		{"~/out/new.txt", true, true},
		{"~/out/keep.txt", false, true},
		{"~/out/to-notes/a.txt", true, false},
		{"~/out/../notes/a.txt", true, false},
		{"~/elsewhere/c.txt", false, false},
		{"/etc/passwd", false, false},
		{"", false, false},
		{"own\x00.txt", false, false},
		{"~", false, false},
	}
	if caps.Windows {
		// A rooted path without a drive is on the own folder's drive,
		// outside, as on Unix; not a file of the own folder (T301 B10).
		cases = append(cases, []struct {
			path  string
			write bool
			ok    bool
		}{
			{`/Windows/win.ini`, false, false},
			{`\Windows\win.ini`, true, false},
		}...)
	}
	if caps.CaseFold {
		cases = append(cases, []struct {
			path  string
			write bool
			ok    bool
		}{
			{"~/.SSH/id_rsa", false, false},
			{"~/notes/repo/.GIT/config", false, false},
			{"~/notes/.ENV", false, false},
			{"~/.Terminatr/config.toml", false, false},
			{"~/NOTES/a.txt", false, true},
		}...)
	}
	for _, c := range cases {
		real, err := f.p.Check(c.path, c.write)
		if (err == nil) != c.ok {
			t.Errorf("Check(%q, write %v) = %q, %v; want ok %v", c.path, c.write, real, err, c.ok)
		}
		if err == nil {
			checkOracle(t, f, c.path, c.write, real)
		}
	}
}

// checkOracle is an allowed path's invariants, worked out without Paths:
// the resolved file is in the mod's own folder or a root its grant may
// use, never in a secret, .git, a .env file or the rest of tm's folder.
func checkOracle(t testing.TB, f fixture, path string, write bool, real string) {
	t.Helper()
	norm := func(s string) string {
		s = filepath.ToSlash(s)
		if caps.CaseFold {
			s = strings.ToLower(s)
		}
		return s
	}
	if !filepath.IsAbs(real) || filepath.Clean(real) != real {
		t.Fatalf("Check(%q) = %q: not absolute and clean", path, real)
	}
	if r, err := filepath.EvalSymlinks(real); err == nil && norm(r) != norm(real) {
		t.Fatalf("Check(%q) = %q: links left in it (%q)", path, real, r)
	}
	home := norm(f.home)
	rel, ok := strings.CutPrefix(norm(real), home+"/")
	if !ok {
		t.Fatalf("Check(%q) = %q: outside the fixture's home", path, real)
	}
	roots := []string{".terminatr/state/mods/m", "out"}
	if !write {
		roots = append(roots, "notes")
	}
	in := false
	for _, r := range roots {
		in = in || rel == r || strings.HasPrefix(rel, r+"/")
	}
	if !in {
		t.Fatalf("Check(%q, write %v) = %q: outside the allowed roots", path, write, real)
	}
	for _, c := range strings.Split(rel, "/") {
		if c == ".git" || c == ".env" || c == ".envrc" || strings.HasPrefix(c, ".env.") {
			t.Fatalf("Check(%q) = %q: a .git or .env name", path, real)
		}
	}
	for _, bad := range []string{".ssh", ".config", ".claude", "library", "Library", ".terminatr/config.toml", ".terminatr/state/mods/other"} {
		if rel == bad || strings.HasPrefix(rel, bad+"/") {
			t.Fatalf("Check(%q) = %q: reaches %s", path, real, bad)
		}
	}
}

// FuzzPathsCheck: whatever the path, an allowed one keeps the oracle's
// invariants.
func FuzzPathsCheck(f *testing.F) {
	for _, s := range []string{
		"own.txt", "../other/x.txt", "~/notes/a.txt", "~/notes/../.ssh/id_rsa", "~/notes/link-ssh/id_rsa",
		"~/notes/link-tm/config.toml", "~/notes/link-up/.ssh/id_rsa", "~/notes/repo/.git/config", "~/notes/.env",
		"~/notes//sub/./b.txt", "~/notes/sub/../../.terminatr/config.toml", "~/out/to-notes/a.txt", "~/.SSH/id_rsa",
		"/etc/passwd", "~", "~/", "./../../..", "~/notes/dangling", "~/elsewhere/in/a.txt", "~/notes/\u212a.txt",
	} {
		f.Add(s, false)
		f.Add(s, true)
	}
	fx := newFixture(f)
	f.Fuzz(func(t *testing.T, path string, write bool) {
		real, err := fx.p.Check(path, write)
		if err == nil {
			checkOracle(t, fx, path, write, real)
		}
	})
}

func TestWindowsName(t *testing.T) {
	for _, c := range []struct {
		path string
		bad  bool
	}{
		{`C:\Users\a\notes\x.txt`, false},
		{`C:\Users\a\notes\x.txt:secret`, true},
		{`C:\Users\a\notes.\x.txt`, true},
		{`C:\Users\a\notes \x.txt`, true},
		{`C:\Users\a\NOTES~1\x.txt`, true},
		{`\\server\share\x.txt`, true},
		{`\\?\C:\x.txt`, true},
	} {
		if !caps.Windows {
			continue // VolumeName reads Windows paths only on Windows
		}
		if got := windowsName(c.path) != ""; got != c.bad {
			t.Errorf("windowsName(%q) = %v, want %v", c.path, got, c.bad)
		}
	}
}

// TestRootless: a Windows path rooted without a drive, which
// filepath.IsAbs calls relative there (T301 B10).
func TestRootless(t *testing.T) {
	for path, want := range map[string]bool{
		`/Windows/win.ini`:   true,
		`\Windows\win.ini`:   true,
		`C:\Windows\win.ini`: false,
		`\\server\share\x`:   false,
		`//server/share/x`:   false,
		`own.txt`:            false,
		`sub\own.txt`:        false,
	} {
		if got := rootless(path); got != want {
			t.Errorf("rootless(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestPathsCheckIn: a path in a thread's worktree (thread.changes) is
// relative and stays inside it, and names no .git, .env, credential
// store, or tm's own folder unless the worktree is in it.
func TestPathsCheckIn(t *testing.T) {
	f := newFixture(t)
	wt := filepath.Join(f.home, ".terminatr", "worktrees", "p", "t-0001-x")
	for _, name := range []string{"a.go", "sub/b.go", ".git/config", ".env"} {
		p := filepath.Join(wt, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte("x"), 0o600)
	}
	os.Symlink(filepath.Join(f.home, ".ssh"), filepath.Join(wt, "ssh"))
	os.Symlink(filepath.Join(f.home, "notes"), filepath.Join(wt, "notes"))
	cases := []struct {
		root, path, want string
	}{
		{wt, "a.go", "a.go"},
		{wt, "sub/../sub/b.go", "sub/b.go"},
		{wt, "gone.go", "gone.go"}, // deleted files have patches too
		{wt, "../t-0002-y/a.go", ""},
		{wt, "/etc/passwd", ""},
		{wt, "..", ""},
		{wt, ".", ""},
		{wt, "", ""},
		{wt, "a\x00.go", ""},
		{wt, `sub\b.go`, ""},
		{wt, ".git/config", ""},
		{wt, "sub/.ENV", map[bool]string{true: "", false: "sub/.ENV"}[caps.CaseFold]},
		{wt, ".env", ""},
		{wt, "ssh/id_rsa", ""},                 // a link out of the worktree
		{wt, "notes/a.txt", ""},                // also out
		{f.home, ".ssh/id_rsa", ""},            // a repository at ~ still reaches no secret
		{f.home, ".terminatr/config.toml", ""}, // nor tm's folder
		{f.home, "notes/a.txt", "notes/a.txt"},
	}
	for _, c := range cases {
		got, err := f.p.CheckIn(c.root, c.path)
		if got != c.want || (err == nil) != (c.want != "") {
			t.Errorf("CheckIn(%s, %q) = %q, %v; want %q", c.root, c.path, got, err, c.want)
		}
	}
}

// TestKeychainGroups: each group of private places is listed, per OS.
func TestKeychainGroups(t *testing.T) {
	home := filepath.Join(t.TempDir(), "h")
	app, local := filepath.Join(t.TempDir(), "Roaming"), filepath.Join(t.TempDir(), "Local")
	env := map[string]string{"APPDATA": app, "LOCALAPPDATA": local}
	got := Keychains(home, func(k string) string { return env[k] })
	groups := map[string][]string{
		"keychains and password managers": {"Library/Keychains", ".password-store", ".local/share/keyrings", ".config/op",
			"Library/Application Support/1Password", ".config/Bitwarden", ".config/keepassxc", ".pgpass"},
		"browser profiles": {"Library/Application Support/Google/Chrome", "Library/Application Support/Firefox",
			"Library/Safari", "Library/Application Support/Microsoft Edge", "Library/Application Support/BraveSoftware",
			"Library/Application Support/Arc", ".config/google-chrome", ".mozilla", ".config/BraveSoftware", ".config/microsoft-edge"},
		"shell histories": {".bash_history", ".zsh_history", ".local/share/fish", ".python_history", ".psql_history"},
		"package-manager credentials": {".npmrc", ".yarnrc.yml", ".m2/settings.xml", ".gradle/gradle.properties", ".pypirc",
			".cargo/credentials", ".cargo/credentials.toml", ".docker/config.json", ".kube/config", ".nuget/NuGet/NuGet.Config"},
		"cloud CLIs": {".aws", ".azure", ".config/gcloud", ".config/doctl", ".oci"},
	}
	for name, rels := range groups {
		for _, rel := range rels {
			if want := filepath.Join(home, filepath.FromSlash(rel)); !slices.Contains(got, want) {
				t.Errorf("%s: no %s in Keychains", name, rel)
			}
		}
	}
	windows := map[string][]string{
		"browser profiles": {filepath.Join(local, "Google", "Chrome"), filepath.Join(local, "Microsoft", "Edge"),
			filepath.Join(local, "BraveSoftware"), filepath.Join(app, "Mozilla", "Firefox")},
		"shell histories":   {filepath.Join(app, "Microsoft", "Windows", "PowerShell", "PSReadLine")},
		"package-manager":   {filepath.Join(app, "NuGet", "NuGet.Config")},
		"cloud CLIs":        {filepath.Join(app, "gcloud"), filepath.Join(app, "Azure")},
		"password managers": {filepath.Join(app, "Bitwarden"), filepath.Join(local, "1Password"), filepath.Join(app, "Microsoft", "Credentials")},
	}
	for name, ps := range windows {
		for _, p := range ps {
			if !slices.Contains(got, p) {
				t.Errorf("windows %s: no %s in Keychains", name, p)
			}
		}
	}
	if n := len(Keychains("", func(string) string { return "" })); n != 2 {
		t.Errorf("without a home or app data: %d entries, want the 2 system keychains", n)
	}
}

// TestPathsCheckPrivate: a grant over the home's folders never reaches a
// private place in any group, by name or through a link.
func TestPathsCheckPrivate(t *testing.T) {
	home := t.TempDir()
	getenv := func(string) string { return "" }
	files := []string{
		"docs/ok.txt", "Library/Application Support/Google/Chrome/Default/Cookies", ".mozilla/firefox/p/logins.json",
		".zsh_history", ".local/share/fish/fish_history", ".m2/settings.xml", ".gradle/gradle.properties",
		".config/gcloud/credentials.db", ".azure/accessTokens.json", "Library/Application Support/Bitwarden/data.json",
	}
	for _, f := range files {
		full := filepath.Join(home, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := Paths{
		Home: home, TMHome: filepath.Join(home, ".terminatr"), Own: filepath.Join(home, ".terminatr", "m"),
		Read:    []string{"~/docs", "~/Library", "~/.mozilla", "~/.local", "~/.m2", "~/.gradle", "~/.config", "~/.azure"},
		Secrets: Real(append(agent.Secrets(nil, home, getenv), Keychains(home, getenv)...)),
	}.resolve()
	for _, f := range files[1:] {
		if _, err := p.Check("~/"+f, false); err == nil {
			t.Errorf("~/%s was reachable", f)
		}
	}
	if _, err := p.Check("~/docs/ok.txt", false); err != nil {
		t.Errorf("~/docs/ok.txt: %v", err)
	}
}
