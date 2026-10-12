package modbroker

import (
	"os"
	"path/filepath"
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
