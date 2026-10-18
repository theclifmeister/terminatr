package mod

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/home"
)

var (
	human = caller.Caller{Kind: caller.Human}
	coord = caller.Caller{Kind: caller.Coordinator, Project: "demo"}
)

// nordToml is a data mod: no code, no permissions.
const nordToml = `
id = "nord"
name = "Nord"
version = "0.4.0"
api = "1.0"
[provides]
theme = "theme.toml"
rules = { thread = "rules/thread.md" }
`

// withHome points TERMINATR_HOME at a fresh folder and returns it.
func withHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv(home.Env, h)
	return h
}

func registryOf(t *testing.T) *Registry {
	t.Helper()
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	r, err := Load(c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func installFrom(t *testing.T, src Source) *Manifest {
	t.Helper()
	st, err := Fetch(src)
	if err != nil {
		t.Fatalf("fetch %s: %v", src, err)
	}
	defer st.Close()
	if err := Install(human, st); err != nil {
		t.Fatal(err)
	}
	return st.Manifest
}

func TestParseSource(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "nord"), 0o755)
	for _, c := range []struct{ arg, git, ref, path string }{
		{arg: "https://github.com/ada/tm-nord", git: "https://github.com/ada/tm-nord"},
		{arg: "https://github.com/ada/tm-nord.git@v0.4.0", git: "https://github.com/ada/tm-nord.git", ref: "v0.4.0"},
		// An @ before the last slash is part of the path.
		{arg: "https://example.com/x@y/z", git: "https://example.com/x@y/z"},
		{arg: "nord", path: filepath.Join(dir, "nord")},
	} {
		s, err := ParseSource(c.arg, dir)
		if err != nil || s.Git != c.git || s.Ref != c.ref || s.Path != c.path {
			t.Errorf("%s: got %+v %v", c.arg, s, err)
		}
	}
	for _, bad := range []string{"", "-x", "http://example.com/x", "git://example.com/x", "missing", "https://example.com/x@-v1"} {
		if _, err := ParseSource(bad, dir); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestSourceCredentials(t *testing.T) {
	for _, bad := range []string{
		"https://ada@example.com/tm-nord",
		"https://ada:s3cret@example.com/tm-nord@v1",
		"https://ghp_TOKEN@github.com/ada/tm-nord//mods/x",
		"https://github.com/ada/tm-nord", // control: below
		"ssh://git@example.com/x",
		"https:///user:TOKEN@github.com/ada/mods",
		"https:////user:TOKEN@github.com/ada/mods",
		`https://\user:TOKEN@github.com/ada/mods`,
		`https://github.com\@evil.com/x`,
		"https://",
		"https:///github.com/ada/mods",
		"https://github.com/ada/mods x",
		"https://github.com/ada/mo\tds",
		"https://github.com/ada/\x00mods",
		"ssh://example.com/x",
		"git@github.com:ada/tm-nord.git",
		"ada@example.com:mods.git//nord@v1",
		"https://:tok@example.com/x",
	} {
		_, err := ParseSource(bad, "")
		if bad == "https://github.com/ada/tm-nord" {
			if err != nil {
				t.Errorf("%s: %v", bad, err)
			}
			continue
		}
		if !errors.Is(err, ErrNotPublic) {
			t.Errorf("%s: err %v, want ErrNotPublic", bad, err)
		}
	}
	// A record installed over SSH is shown as is and never fetched.
	ssh := Source{Git: "git@example.com:ada/mods.git", Ref: "v1"}
	if ssh.String() != "git@example.com:ada/mods.git@v1" {
		t.Errorf("ssh record shown as %s", ssh.String())
	}
	if _, err := Fetch(ssh); !errors.Is(err, ErrNotPublic) {
		t.Errorf("Fetch ssh: %v", err)
	}
	if _, err := Next(ssh); err == nil {
		t.Error("Next ssh: no error")
	}
	if _, _, err := Latest(ssh); err == nil {
		t.Error("Latest ssh: no error")
	}
	// A record from before: shown redacted, never fetched.
	s := Source{Git: "https://ada:s3cret@example.com/tm-nord", Ref: "v1", Subdir: "m", Commit: "abcdef1234"}
	for _, out := range []string{s.String(), s.Words()} {
		if strings.Contains(out, "s3cret") || strings.Contains(out, "ada") || !strings.Contains(out, "***@example.com") {
			t.Errorf("not redacted: %s", out)
		}
	}
	if _, err := Fetch(s); !errors.Is(err, ErrNotPublic) {
		t.Errorf("Fetch: %v", err)
	}
	if _, err := Next(s); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("Next: %v", err)
	}
	if _, _, err := Latest(s); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("Latest: %v", err)
	}
}

func TestSourceRedactsOnDisk(t *testing.T) {
	dir := t.TempDir()
	rec := Source{Git: "https:///ada:s3cret@example.com/tm-nord", Commit: "abcdef1234"}
	if err := writeSource(dir, rec); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadSource(dir)
	if !ok || strings.Contains(got.Git, "s3cret") {
		t.Fatalf("read %+v %v", got, ok)
	}
	data, _ := os.ReadFile(filepath.Join(dir, SourceFile))
	if strings.Contains(string(data), "s3cret") || strings.Contains(string(data), "ada") {
		t.Errorf("secret still on disk: %s", data)
	}
	if _, err := Fetch(got); !errors.Is(err, ErrNotPublic) {
		t.Errorf("redacted record fetched: %v", err)
	}
}

func TestGitIgnoresUserConfig(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, "gitconfig")
	os.WriteFile(cfg, []byte("[url \"https://secret.example/\"]\n\tinsteadOf = https://public.example/\n[http]\n\textraHeader = X-Token: abc\n"), 0o644)
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[url \"https://secret.example/\"]\n\tinsteadOf = https://public.example/\n"), 0o644)
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_SYSTEM", cfg)
	out, err := git("", "ls-remote", "--get-url", "https://public.example/x")
	if err != nil || out != "https://public.example/x" {
		t.Errorf("config applied: %q %v", out, err)
	}
	if out, _ := git("", "config", "--get-all", "http.extraHeader"); out != "" {
		t.Errorf("extraHeader set: %q", out)
	}
	if out, _ := git("", "config", "--get", "credential.helper"); out != "" {
		t.Errorf("helper set: %q", out)
	}
}

func TestParseSourceSubdir(t *testing.T) {
	for _, c := range []struct{ arg, git, ref, sub, str string }{
		{arg: "https://github.com/theclifmeister/terminatr/tree/v0.20.0/examples/mods/standup",
			git: "https://github.com/theclifmeister/terminatr", ref: "v0.20.0", sub: "examples/mods/standup"},
		{arg: "https://github.com/ada/mods/tree/main/nord/", git: "https://github.com/ada/mods", ref: "main", sub: "nord",
			str: "https://github.com/ada/mods/tree/main/nord"},
		{arg: "https://github.com/ada/tm-nord/tree/v1", git: "https://github.com/ada/tm-nord", ref: "v1",
			str: "https://github.com/ada/tm-nord@v1"},
		{arg: "https://github.com/ada/mods//nord", git: "https://github.com/ada/mods", sub: "nord"},
		{arg: "https://github.com/ada/mods//nord@v0.4.0", git: "https://github.com/ada/mods", ref: "v0.4.0", sub: "nord",
			str: "https://github.com/ada/mods/tree/v0.4.0/nord"},
		{arg: "https://example.com/ada/mods.git//themes/nord@main", git: "https://example.com/ada/mods.git", ref: "main", sub: "themes/nord"},
		{arg: "file:///srv/mods//nord", git: "file:///srv/mods", sub: "nord"},
		{arg: "file:///srv/mods", git: "file:///srv/mods"},
	} {
		s, err := ParseSource(c.arg, "")
		if err != nil || s.Git != c.git || s.Ref != c.ref || s.Subdir != c.sub {
			t.Errorf("%s: got %+v %v", c.arg, s, err)
			continue
		}
		want := c.str
		if want == "" {
			want = c.arg
		}
		if s.String() != want {
			t.Errorf("%s: String %s, want %s", c.arg, s, want)
		}
		if back, err := ParseSource(s.String(), ""); err != nil || back != s {
			t.Errorf("%s: doesn't read back: %+v %v", s, back, err)
		}
	}
	for _, bad := range []string{
		"https://example.com/mods//../etc",
		"https://example.com/mods//nord/../../x",
		"https://example.com/mods///etc/passwd",
		"https://example.com/mods//nord//x",
		"https://example.com/mods//./nord",
		"https://example.com/mods//.git",
		"https://example.com/mods//nord/.GIT/hooks",
		"https://example.com/mods//-nord",
		"https://example.com/mods//%2e%2e/x",
		"https://example.com/mods//a\\..\\b",
		"https://example.com/mods//c:/x",
		"https://github.com/ada/mods/tree/main/../x",
		"https://github.com/ada/mods/tree/main//x",
		"https://github.com/ada/mods/tree/main/nord@v1",
		"https://github.com/ada/mods/tree/-x/nord",
		"https://github.com/ada/mods/blob/main/nord/mod.toml",
		"file:////nord",
	} {
		if s, err := ParseSource(bad, ""); err == nil && s.Subdir != "" || err == nil && strings.Contains(bad, "github.com") {
			t.Errorf("%q: no error: %+v", bad, s)
		}
	}
	for _, bad := range []string{"", "/x", "..", "a/..", "a/./b", "a/", ".git", "a/.Git", "-a", "a b", "a:b", "a\\b", "a%2fb"} {
		if CheckSubdir(bad) == nil {
			t.Errorf("CheckSubdir(%q) passed", bad)
		}
	}
}

func TestInstallFromPath(t *testing.T) {
	h := withHome(t)
	src := install(t, t.TempDir(), "nord", map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": "Be terse.\n", ".git/HEAD": "x"})
	// A table left by an earlier install must not enable or grant it.
	os.WriteFile(filepath.Join(h, "config.toml"), []byte("[mods.nord]\nenabled = true\ngrants = [\"project_read\"]\n"), 0o600)
	st, err := Fetch(Source{Path: src})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := Install(coord, st); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent installed a mod: %v", err)
	}
	if err := Install(human, st); err != nil {
		t.Fatal(err)
	}
	m, ok := registryOf(t).Get("nord")
	if !ok || m.State != Disabled {
		t.Fatalf("installed: %+v", m)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, ".git")); err == nil {
		t.Error(".git was copied in")
	}
	s, ok := ReadSource(m.Dir)
	want, _ := TreeHash(src)
	if !ok || s.Path != src || s.Hash != want || s.Installed.IsZero() {
		t.Errorf("source record %+v, hash want %s", s, want)
	}
	if next, behind, err := Latest(s); err != nil || behind {
		t.Errorf("fresh copy is behind: %q %v %v", next, behind, err)
	}
	os.WriteFile(filepath.Join(src, "theme.toml"), []byte("bg = 1\n"), 0o644)
	if next, behind, err := Latest(s); err != nil || !behind || next != "0.4.0" {
		t.Errorf("changed source: %q %v %v", next, behind, err)
	}
	st2, err := Fetch(Source{Path: src})
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if err := Install(human, st2); err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Errorf("second install: %v", err)
	}
	// Taken says so before the consent, naming what works instead.
	r := registryOf(t)
	for id, want := range map[string]string{
		"nord":       "nord is already installed: tm mod update nord",
		"memory-tab": "memory-tab is already installed: it is a built-in mod's id",
		"free":       "",
	} {
		if got := Taken(r, id); !strings.HasPrefix(got, want) || (want == "") != (got == "") {
			t.Errorf("Taken(%s) = %q, want %q…", id, got, want)
		}
	}
	os.Remove(filepath.Join(m.Dir, SourceFile))
	if got := Taken(registryOf(t), "nord"); !strings.Contains(got, "records no source") {
		t.Errorf("Taken of a mod with no source: %q", got)
	}
}

func TestFetchRefuses(t *testing.T) {
	withHome(t)
	outside := t.TempDir()
	src := install(t, t.TempDir(), "nord", map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": ""})
	os.Symlink(outside, filepath.Join(src, "escape"))
	if _, err := Fetch(Source{Path: src}); err == nil || !strings.Contains(err.Error(), "must stay inside") {
		t.Errorf("a link out: %v", err)
	}
	if _, err := Validate(src); err == nil {
		t.Error("validate passed a link out")
	}
	bad := install(t, t.TempDir(), "x", map[string]string{"mod.toml": `id = "x"`})
	st, err := Fetch(Source{Path: bad})
	if err == nil || st == nil {
		t.Fatalf("a broken manifest: %v", err)
	}
	st.Close()
	if ents, _ := os.ReadDir(filepath.Join(os.Getenv(home.Env), "mods")); len(ents) != 0 {
		t.Errorf("staging left behind: %v", ents)
	}
}

// TestLinksResolvedInside: a link is followed as the OS would, so links
// that read as inside but resolve out are refused.
func TestLinksResolvedInside(t *testing.T) {
	withHome(t)
	for name, links := range map[string][][2]string{
		"through a link to ..":   {{"rules/up", ".."}, {"rules/out", "up/../x"}},
		"through a link to .":    {{"here", "."}, {"out", "here/.."}},
		"a loop":                 {{"a", "b"}, {"b", "a"}},
		"a missing name then ..": {{"out", "missing/../../x"}},
		"absolute":               {{"abs", "/etc"}},
		"via a chain":            {{"rules/one", "two"}, {"rules/two", "../.."}},
	} {
		src := install(t, t.TempDir(), "nord", map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": ""})
		for _, l := range links {
			os.Symlink(l[1], filepath.Join(src, filepath.FromSlash(l[0])))
		}
		if _, err := Fetch(Source{Path: src}); err == nil || !strings.Contains(err.Error(), "must stay inside") {
			t.Errorf("%s: %v", name, err)
		}
	}
	src := install(t, t.TempDir(), "nord", map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": ""})
	for _, l := range [][2]string{{"rules/up", ".."}, {"rules/theme", "up/theme.toml"}, {"same", "rules/../theme.toml"}, {"later", "not-yet"}, {"here", "."}} {
		os.Symlink(l[1], filepath.Join(src, filepath.FromSlash(l[0])))
	}
	st, err := Fetch(Source{Path: src})
	if err != nil {
		t.Fatalf("links that stay inside: %v", err)
	}
	st.Close()
}

func TestEnableDisableProjects(t *testing.T) {
	withHome(t)
	src := install(t, t.TempDir(), "review-age", map[string]string{"mod.toml": reviewAge, "main.ts": ""})
	installFrom(t, Source{Path: src})
	if err := Enable(coord, "review-age", ""); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent enable: %v", err)
	}
	if err := Enable(human, "review-age", "demo"); err != nil {
		t.Fatal(err)
	}
	m, _ := registryOf(t).Get("review-age")
	if m.State != Enabled || !slices.Equal(m.Projects, []string{"demo"}) || !slices.Equal(m.Grants, []string{PermProjectRead, PermViews, PermNet}) {
		t.Fatalf("enabled for demo: %+v", m)
	}
	Enable(human, "review-age", "site")
	if err := Disable(human, "review-age", "demo"); err != nil {
		t.Fatal(err)
	}
	if m, _ := registryOf(t).Get("review-age"); m.State != Enabled || !slices.Equal(m.Projects, []string{"site"}) {
		t.Fatalf("demo taken off: %+v", m)
	}
	Disable(human, "review-age", "site")
	m, _ = registryOf(t).Get("review-age")
	if m.State != Disabled || len(m.Projects) != 0 || len(m.Grants) != 3 {
		t.Fatalf("last project off: %+v", m)
	}
	Enable(human, "review-age", "")
	if err := Disable(human, "review-age", "demo"); !errors.Is(err, ErrAllProjects) {
		t.Errorf("one project off an all-projects mod: %v", err)
	}
}

func TestRemoveKeepsData(t *testing.T) {
	h := withHome(t)
	src := install(t, t.TempDir(), "nord", map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": ""})
	installFrom(t, Source{Path: src})
	Enable(human, "nord", "")
	data := filepath.Join(h, "state", "mods", "nord")
	os.MkdirAll(data, 0o755)
	cache := filepath.Join(h, "cache", "mods", "nord")
	os.MkdirAll(cache, 0o755)
	if err := Remove(coord, "nord", false); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent remove: %v", err)
	}
	if err := Remove(human, "nord", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("remove kept the cache folder")
	}
	r := registryOf(t)
	if _, ok := r.Get("nord"); ok || len(r.Orphans) != 0 {
		t.Fatalf("after remove: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(data, removedFile)); err != nil {
		t.Fatal("data not marked removed")
	}
	SweepRemoved(time.Now())
	if _, err := os.Stat(data); err != nil {
		t.Fatal("data swept before 30 days")
	}
	// A reinstall finds its data again, unmarked.
	installFrom(t, Source{Path: src})
	if _, err := os.Stat(filepath.Join(data, removedFile)); err == nil {
		t.Error("reinstall kept the removal mark")
	}
	Remove(human, "nord", false)
	SweepRemoved(time.Now().Add(KeepRemoved + time.Hour))
	if _, err := os.Stat(data); err == nil {
		t.Error("data kept past 30 days")
	}
	installFrom(t, Source{Path: src})
	os.MkdirAll(data, 0o755)
	os.MkdirAll(cache, 0o755)
	if err := Remove(human, "nord", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); err == nil {
		t.Error("--purge kept the data")
	}
	if _, err := os.Stat(cache); err == nil {
		t.Error("--purge kept the cache folder")
	}
}

func TestConsentAndDiff(t *testing.T) {
	dir := install(t, t.TempDir(), "review-age", map[string]string{
		"mod.toml": reviewAge + "[provides.rules]\ncoordinator = \"rules/c.md\"\n", "main.ts": "",
		"rules/c.md": "Label tasks.\x1b]0;pwned\x07\n",
	})
	old, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := Consent(old, dir)
	for _, want := range []string{"Review age 0.3.1 (review-age) by Ada", "reads your projects", "draws its own panels", "talks to api.example.com", "----- begin rules/c.md -----", "Label tasks."} {
		if !strings.Contains(c, want) {
			t.Errorf("consent lacks %q:\n%s", want, c)
		}
	}
	if strings.ContainsRune(c, '\x1b') || strings.ContainsRune(c, '\x07') {
		t.Error("consent passes control characters through")
	}
	if ch := Diff(old, dir, old, dir); ch.Asks() {
		t.Errorf("no change asks: %+v", ch)
	}
	newDir := install(t, t.TempDir(), "review-age", map[string]string{
		"mod.toml":   strings.Replace(reviewAge, `net          = ["api.example.com"]`, "net = [\"api.example.com\", \"evil.example.com\"]\ninbox = true", 1) + "[provides.rules]\ncoordinator = \"rules/c.md\"\n",
		"main.ts":    "",
		"rules/c.md": "Label tasks, and more.\n",
	})
	nm, err := ReadManifest(newDir)
	if err != nil {
		t.Fatal(err)
	}
	ch := Diff(old, dir, nm, newDir)
	if !slices.Equal(ch.NewPerms, []string{PermInbox, PermNet}) || len(ch.Text) != 1 {
		t.Fatalf("diff: %+v", ch)
	}
	if d := ch.Describe(nm); !strings.Contains(d, "talks to api.example.com, evil.example.com") || !strings.Contains(d, "and more") {
		t.Errorf("describe:\n%s", d)
	}
}

// gitRepo makes a git repo holding the mod files, committed and tagged
// (tag "": none), and returns it with a func that commits more.
func gitRepo(t *testing.T, files map[string]string, tag string) (string, func(files map[string]string, tag string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := install(t, t.TempDir(), "repo", files)
	run := gittest.In(t, dir)
	run("init", "-q", "-b", "main")
	run("add", "-A")
	run("commit", "-q", "-m", "one")
	if tag != "" {
		run("tag", tag)
	}
	return dir, func(files map[string]string, tag string) {
		for name, body := range files {
			p := filepath.Join(dir, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(body), 0o644)
		}
		run("add", "-A")
		run("commit", "-q", "-m", "more")
		if tag != "" {
			run("tag", tag)
		}
	}
}

func TestInstallFromGit(t *testing.T) {
	withHome(t)
	repo, gitCommit := gitRepo(t, map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": ""}, "v0.4.0")
	src := Source{Git: "file://" + repo, Ref: "v0.4.0"}
	installFrom(t, src)
	m, ok := registryOf(t).Get("nord")
	if !ok || m.State != Disabled {
		t.Fatalf("installed: %+v", m)
	}
	s, _ := ReadSource(m.Dir)
	if s.Git != src.Git || s.Ref != "v0.4.0" || len(s.Commit) != 40 {
		t.Fatalf("source record %+v", s)
	}
	if _, behind, err := Latest(s); err != nil || behind {
		t.Fatalf("fresh install behind: %v %v", behind, err)
	}
	gitCommit(map[string]string{"mod.toml": strings.Replace(nordToml, "0.4.0", "0.5.0", 1)}, "v0.5.0")
	next, behind, err := Latest(s)
	if err != nil || !behind || next != "v0.5.0" {
		t.Fatalf("after a new tag: %q %v %v", next, behind, err)
	}
	n, err := Next(s)
	if err != nil || n.Ref != "v0.5.0" {
		t.Fatalf("next: %+v %v", n, err)
	}
	st, err := Fetch(n)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := Replace(human, "nord", st, nil); err != nil {
		t.Fatal(err)
	}
	m, _ = registryOf(t).Get("nord")
	if m.Manifest.Version != "0.5.0" {
		t.Fatalf("after update: %+v", m.Manifest)
	}
	// A branch is behind when it moves.
	b := Source{Git: "file://" + repo, Ref: "main", Commit: s.Commit}
	if _, behind, err := Latest(b); err != nil || !behind {
		t.Errorf("branch: %v %v", behind, err)
	}
}

// subdirRepo is a repository holding the nord mod in mods/nord, beside
// other files tm mustn't copy in.
func subdirRepo(t *testing.T) (string, func(files map[string]string, tag string)) {
	t.Helper()
	return gitRepo(t, map[string]string{
		"README.md": "mods\n", "other/secret.txt": "not the mod\n",
		"mods/nord/mod.toml": nordToml, "mods/nord/theme.toml": "", "mods/nord/rules/thread.md": "",
	}, "v0.4.0")
}

func TestInstallFromGitSubdir(t *testing.T) {
	withHome(t)
	repo, gitCommit := subdirRepo(t)
	src, err := ParseSource("file://"+repo+"//mods/nord@v0.4.0", "")
	if err != nil || src.Subdir != "mods/nord" || src.Ref != "v0.4.0" {
		t.Fatalf("parse: %+v %v", src, err)
	}
	installFrom(t, src)
	m, ok := registryOf(t).Get("nord")
	if !ok || m.State != Disabled {
		t.Fatalf("installed: %+v", m)
	}
	for _, f := range []string{"README.md", "other", "mods", ".git"} {
		if _, err := os.Lstat(filepath.Join(m.Dir, f)); err == nil {
			t.Errorf("%s was copied in", f)
		}
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "rules", "thread.md")); err != nil {
		t.Errorf("the mod's files: %v", err)
	}
	s, _ := ReadSource(m.Dir)
	if s.Git != src.Git || s.Subdir != "mods/nord" || s.Ref != "v0.4.0" || len(s.Commit) != 40 || len(s.Tree) != 40 {
		t.Fatalf("source record %+v", s)
	}
	if _, behind, err := Latest(s); err != nil || behind {
		t.Fatalf("fresh install behind: %v %v", behind, err)
	}
	// A new tag that changes only files outside the folder isn't news.
	gitCommit(map[string]string{"README.md": "more\n"}, "v0.5.0")
	if next, behind, err := Latest(s); err != nil || behind {
		t.Fatalf("a tag that didn't touch the mod: %q %v %v", next, behind, err)
	}
	b := Source{Git: s.Git, Ref: "main", Subdir: s.Subdir, Commit: s.Commit, Tree: s.Tree}
	if _, behind, err := Latest(b); err != nil || behind {
		t.Errorf("branch moved outside the mod: %v %v", behind, err)
	}
	gitCommit(map[string]string{"mods/nord/mod.toml": strings.Replace(nordToml, "0.4.0", "0.6.0", 1)}, "v0.6.0")
	next, behind, err := Latest(s)
	if err != nil || !behind || next != "v0.6.0" {
		t.Fatalf("after a new tag: %q %v %v", next, behind, err)
	}
	if _, behind, err := Latest(b); err != nil || !behind {
		t.Errorf("branch moved in the mod: %v %v", behind, err)
	}
	n, err := Next(s)
	if err != nil || n.Ref != "v0.6.0" || n.Subdir != "mods/nord" {
		t.Fatalf("next: %+v %v", n, err)
	}
	st, err := Fetch(n)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := Replace(human, "nord", st, nil); err != nil {
		t.Fatal(err)
	}
	m, _ = registryOf(t).Get("nord")
	s, _ = ReadSource(m.Dir)
	if m.Manifest.Version != "0.6.0" || s.Ref != "v0.6.0" || s.Subdir != "mods/nord" {
		t.Fatalf("after update: %+v %+v", m.Manifest, s)
	}
	if _, behind, err := Latest(s); err != nil || behind {
		t.Errorf("updated copy behind: %v %v", behind, err)
	}
}

func TestFetchSubdirRefuses(t *testing.T) {
	withHome(t)
	outside := t.TempDir()
	repo, gitCommit := subdirRepo(t)
	os.Symlink("../../other", filepath.Join(repo, "mods", "nord", "escape"))
	os.Symlink("../other", filepath.Join(repo, "mods", "link"))
	os.Symlink(outside, filepath.Join(repo, "mods", "abs"))
	gitCommit(map[string]string{"mods/file": "x"}, "")
	url := "file://" + repo
	for sub, want := range map[string]string{
		"mods/nord":    "must stay inside",
		"mods/link":    "is a link",
		"mods/abs":     "is a link",
		"mods/missing": "no folder",
		"mods/file":    "isn't a folder",
		"../repo":      "..",
		"/etc":         "absolute",
		".git":         ".git",
	} {
		st, err := Fetch(Source{Git: url, Subdir: sub})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", sub, err, want)
		}
		st.Close()
	}
	if ents, _ := os.ReadDir(filepath.Join(os.Getenv(home.Env), "mods")); len(ents) != 0 {
		t.Errorf("staging left behind: %v", ents)
	}
}

// TestExampleInstallLines: each example installs with one tm mod install
// of its folder in terminatr's repository at a release tag, or main.
func TestExampleInstallLines(t *testing.T) {
	for _, c := range [][3]string{{"release", "v0.21.0", "v0.21.0"}, {"snapshot", "v0.21.0", "main"}, {"", "dev", "main"}, {"release", "v0.21.0-rc1", "main"}} {
		if got := examplesRef(c[0], c[1]); got != c[2] {
			t.Errorf("examplesRef(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
	for _, x := range Examples {
		if _, err := os.Stat(filepath.Join("..", "..", ExamplesPath, x.ID, "mod.toml")); err != nil {
			t.Errorf("%s: %v", x.ID, err)
		}
		lines := InstallLines(x.ID)
		if IsBundled(x.ID) {
			continue
		}
		arg, ok := strings.CutPrefix(lines[0], "tm mod install ")
		s, err := ParseSource(arg, "")
		if len(lines) != 1 || !ok || err != nil || s.Git != ExamplesRepo || s.Ref != ExamplesRef || s.Subdir != ExamplesPath+"/"+x.ID || s.String() != arg {
			t.Errorf("%s: %q: %+v %v", x.ID, lines, s, err)
		}
	}
}

// A mod pinned to an annotated tag that isn't a version is not behind.
func TestLatestAnnotatedTag(t *testing.T) {
	withHome(t)
	repo, _ := gitRepo(t, map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": ""}, "")
	gittest.Run(t, repo, "tag", "-a", "stable", "-m", "stable")
	installFrom(t, Source{Git: "file://" + repo, Ref: "stable"})
	m, _ := registryOf(t).Get("nord")
	s, _ := ReadSource(m.Dir)
	if next, behind, err := Latest(s); err != nil || behind {
		t.Fatalf("fresh install of an annotated tag behind: %q %v %v", next, behind, err)
	}
}

// A newer tag over the same files is recorded, so the mod isn't
// reported behind again.
func TestUpdateUnchangedFilesRecordsTag(t *testing.T) {
	withHome(t)
	repo, _ := gitRepo(t, map[string]string{"mod.toml": nordToml, "theme.toml": "", "rules/thread.md": ""}, "v0.4.0")
	installFrom(t, Source{Git: "file://" + repo, Ref: "v0.4.0"})
	gittest.Run(t, repo, "commit", "-q", "--allow-empty", "-m", "nothing")
	gittest.Run(t, repo, "tag", "v0.5.0")
	m, _ := registryOf(t).Get("nord")
	if st, err := FetchUpdate(m); err != nil || st != nil {
		t.Fatalf("update of unchanged files: %v %v", st, err)
	}
	s, _ := ReadSource(m.Dir)
	if s.Ref != "v0.5.0" {
		t.Fatalf("recorded ref %q", s.Ref)
	}
	if next, behind, err := Latest(s); err != nil || behind {
		t.Fatalf("still behind: %q %v %v", next, behind, err)
	}
}
