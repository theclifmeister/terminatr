package history

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// releaseProject makes a project in a fresh home with these repos and a
// first record on 2026-10-05.
func releaseProject(t *testing.T, repos ...string) *project.Project {
	t.Helper()
	p, err := project.New(project.Options{Slug: "web", Repos: repos})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n2026-10-05T10:00:00Z human project.new web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// tagAt makes a commit on the repo, dated, with these lightweight tags.
func tagAt(t *testing.T, repo, when string, tags ...string) {
	t.Helper()
	env := []string{"GIT_COMMITTER_DATE=" + when, "GIT_AUTHOR_DATE=" + when}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, append(os.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("commit", "--allow-empty", "-m", "at "+when)
	for _, tag := range tags {
		run("tag", tag)
	}
}

const after, before = "2026-10-06T12:00:00Z", "2026-09-01T12:00:00Z"

func versions(r proto.HistoryRepo) string {
	var out []string
	for _, x := range r.Releases {
		out = append(out, strings.TrimSpace(x.Part+" "+x.Version))
	}
	return strings.Join(out, ", ")
}

func readHistory(t *testing.T, p *project.Project) proto.History {
	t.Helper()
	h, err := ReadIn(p, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// spyGit puts a git on PATH that logs each call and runs the real one.
func spyGit(t *testing.T) (log string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nexec %q \"$@\"\n", log, real)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestPlainFolderCallsNoGit(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	log := spyGit(t)
	h := readHistory(t, releaseProject(t))
	if h.Releases != nil {
		t.Errorf("releases: %+v", h.Releases)
	}
	if data, err := os.ReadFile(log); err == nil {
		t.Errorf("git ran: %s", data)
	}
}

func TestRepoWithNoTags(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, _ := gittest.Repo(t)
	if h := readHistory(t, releaseProject(t, repo)); len(h.Releases) != 0 {
		t.Errorf("releases: %+v", h.Releases)
	}
}

// Several repos, with the same version in two: both kept, named, not
// summed. Two repos of one folder name get their parents' names too.
func TestSeveralRepos(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	a, _ := gittest.Repo(t)
	b, _ := gittest.Repo(t)
	none, _ := gittest.Repo(t)
	tagAt(t, a, after, "v1.0.0")
	tagAt(t, b, after, "v1.0.0")
	tagAt(t, b, after, "v1.1.0")
	h := readHistory(t, releaseProject(t, a, none, b))
	if len(h.Releases) != 2 {
		t.Fatalf("repos: %+v", h.Releases)
	}
	if h.Releases[0].Repo != filepath.Base(a) || h.Releases[0].Count != 1 || h.Releases[1].Repo != filepath.Base(b) || h.Releases[1].Count != 2 {
		t.Errorf("repos: %+v", h.Releases)
	}
	if got := versions(h.Releases[0]); got != "1.0.0" {
		t.Errorf("first repo: %s", got)
	}
	if got := versions(h.Releases[1]); got != "1.0.0, 1.1.0" {
		t.Errorf("second repo: %s", got)
	}
}

// No remote, or another host: the tags are read locally, and nothing
// else differs. A remote that can't be reached would fail a fetch.
func TestNoRemoteOrAnotherHost(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	local, _ := gittest.Repo(t)
	tagAt(t, local, after, "v1.0.0")
	other, inOther := gittest.Repo(t)
	inOther("remote", "add", "origin", "https://git.invalid/o/r.git")
	tagAt(t, other, after, "v1.0.0")
	log := spyGit(t)
	h := readHistory(t, releaseProject(t, local, other))
	if len(h.Releases) != 2 {
		t.Fatalf("repos: %+v", h.Releases)
	}
	data, _ := os.ReadFile(log)
	for _, verb := range []string{"fetch", "ls-remote", "pull"} {
		if strings.Contains(string(data), verb) {
			t.Errorf("%s ran:\n%s", verb, data)
		}
	}
}

func TestMonorepoTags(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, _ := gittest.Repo(t)
	tagAt(t, repo, after, "api/v1.2", "web/2.0.1")
	tagAt(t, repo, after, "pkg@1.3.0", "api/v1.3")
	h := readHistory(t, releaseProject(t, repo))
	if len(h.Releases) != 1 || h.Releases[0].Count != 4 {
		t.Fatalf("repos: %+v", h.Releases)
	}
	got := fmt.Sprintf("%+v", h.Releases[0].Parts)
	if got != "[{Part:api Count:2} {Part:pkg Count:1} {Part:web Count:1}]" {
		t.Errorf("parts: %s", got)
	}
	if h.Releases[0].Releases[0].Part == "" {
		t.Errorf("no part: %+v", h.Releases[0].Releases[0])
	}
}

// One commit gives one release, its most specific version, and only tags
// since the first record count. An annotated tag dates from its tagger.
func TestOldTagsAndOneCommit(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, in := gittest.Repo(t)
	tagAt(t, repo, before, "v0.9.0")
	tagAt(t, repo, after, "v1.2", "v1.2.0")
	in("tag", "-a", "v1.3.0", "-m", "annotated", "HEAD~1") // on the old commit, tagged today
	h := readHistory(t, releaseProject(t, repo))
	if len(h.Releases) != 1 {
		t.Fatalf("repos: %+v", h.Releases)
	}
	if got := versions(h.Releases[0]); got != "1.2.0, 1.3.0" {
		t.Errorf("versions: %s", got)
	}
}

// A repo with 300 old tags and 5,000 in all: only the newer count, the
// newest listed, and the second read asks for no dates.
func TestManyTags(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, _ := gittest.Repo(t)
	var stream strings.Builder
	old, _ := time.Parse(time.RFC3339, before)
	recent, _ := time.Parse(time.RFC3339, after)
	for i := range 5000 {
		at := recent.Add(time.Duration(i) * time.Minute)
		if i < 300 {
			at = old.Add(time.Duration(i) * time.Minute)
		}
		fmt.Fprintf(&stream, "commit refs/tags/v1.%d.0\ncommitter t <t@t> %d +0000\ndata 1\nx\n\n", i, at.Unix())
	}
	cmd := exec.Command("git", "fast-import", "--quiet")
	cmd.Dir, cmd.Stdin = repo, strings.NewReader(stream.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v %s", err, out)
	}
	p := releaseProject(t, repo)
	log := spyGit(t)
	start := time.Now()
	h := readHistory(t, p)
	cold := time.Since(start)
	if len(h.Releases) != 1 || h.Releases[0].Count != 4700 {
		t.Fatalf("repos: %d %+v", len(h.Releases), h.Releases[0].Count)
	}
	r := h.Releases[0]
	if len(r.Releases) != proto.HistoryReleases || r.Releases[len(r.Releases)-1].Version != "1.4999.0" {
		t.Errorf("listed %d, the last %+v", len(r.Releases), r.Releases[len(r.Releases)-1])
	}
	start = time.Now()
	again := readHistory(t, p)
	warm := time.Since(start)
	if again.Releases[0].Count != 4700 {
		t.Errorf("again: %d", again.Releases[0].Count)
	}
	t.Logf("cold %v, warm %v", cold, warm)
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "--sort=-creatordate"); n != 1 {
		t.Errorf("the dates were read %d times:\n%s", n, data)
	}
	// A new tag is a new read; so is one that moved.
	tagAt(t, repo, after, "v2.0.0")
	if third := readHistory(t, p); third.Releases[0].Count != 4701 {
		t.Errorf("a new tag: %d", third.Releases[0].Count)
	}
	data, _ = os.ReadFile(log)
	if n := strings.Count(string(data), "--sort=-creatordate"); n != 2 {
		t.Errorf("after a new tag, the dates were read %d times", n)
	}
}

// A moved or missing repo path: no releases and no error, and back when
// it returns.
func TestMovedRepo(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, _ := gittest.Repo(t)
	tagAt(t, repo, after, "v1.0.0")
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	p := releaseProject(t, repo, gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if h := readHistory(t, p); len(h.Releases) != 1 {
		t.Fatalf("before: %+v", h.Releases)
	}
	moved := repo + "-moved"
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	if h := readHistory(t, p); h.Releases != nil {
		t.Errorf("moved: %+v", h.Releases)
	}
	if err := os.Rename(moved, repo); err != nil {
		t.Fatal(err)
	}
	if h := readHistory(t, p); len(h.Releases) != 1 {
		t.Errorf("back: %+v", h.Releases)
	}
}

// A folder that isn't a checkout of its own, git absent, and git failing
// give none too.
func TestGitAbsentOrFailing(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, _ := gittest.Repo(t)
	tagAt(t, repo, after, "v1.0.0")
	p := releaseProject(t, repo, t.TempDir())
	if h := readHistory(t, p); len(h.Releases) != 1 {
		t.Fatalf("with git: %+v", h.Releases)
	}
	t.Setenv("PATH", t.TempDir())
	if h := readHistory(t, p); h.Releases != nil {
		t.Errorf("no git: %+v", h.Releases)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if h := readHistory(t, p); h.Releases != nil {
		t.Errorf("git failing: %+v", h.Releases)
	}
}

func TestSettingDecides(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, _ := gittest.Repo(t)
	tagAt(t, repo, after, "v1.0.0")
	tagAt(t, repo, after, "prod-2026-10-06")
	p := releaseProject(t, repo)
	if got := versions(readHistory(t, p).Releases[0]); got != "1.0.0" {
		t.Errorf("the rule: %s", got)
	}
	if err := config.SetProject("web", "release_tags", []string{"prod-*"}); err != nil {
		t.Fatal(err)
	}
	if got := versions(readHistory(t, p).Releases[0]); got != "prod-2026-10-06" {
		t.Errorf("patterns: %s", got)
	}
	if err := config.SetProject("web", "release_tags", []string{config.ReleaseTagsOff}); err != nil {
		t.Fatal(err)
	}
	log := spyGit(t)
	if h := readHistory(t, p); h.Releases != nil {
		t.Errorf("none: %+v", h.Releases)
	}
	if data, err := os.ReadFile(log); err == nil {
		t.Errorf("none ran git: %s", data)
	}
}

// Records, what a paired server gets, runs no git and has no releases.
func TestRecordsHaveNoReleases(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	repo, _ := gittest.Repo(t)
	tagAt(t, repo, after, "v1.0.0")
	p := releaseProject(t, repo)
	log := spyGit(t)
	h, err := Records(p)
	if err != nil {
		t.Fatal(err)
	}
	if h.Releases != nil {
		t.Errorf("releases: %+v", h.Releases)
	}
	if data, err := os.ReadFile(log); err == nil {
		t.Errorf("git ran: %s", data)
	}
}
