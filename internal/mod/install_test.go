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
		{arg: "https://ada@example.com/tm-nord", git: "https://ada@example.com/tm-nord"},
		{arg: "git@github.com:ada/tm-nord.git", git: "git@github.com:ada/tm-nord.git"},
		{arg: "git@github.com:ada/tm-nord.git@main", git: "git@github.com:ada/tm-nord.git", ref: "main"},
		// An @ before the last slash is part of the path.
		{arg: "ssh://git@example.com/x@release/1", git: "ssh://git@example.com/x@release/1"},
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
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", "-A")
	run("commit", "-q", "-m", "one")
	if tag != "" {
		run("tag", tag)
	}
	return dir, func(files map[string]string, tag string) {
		for name, body := range files {
			os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
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
