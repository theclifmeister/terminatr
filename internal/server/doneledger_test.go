package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The ledger's fixtures (design board-library, B4 and B12), as of ledgerNow.
var ledgerNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// ledgerProject has one done task per name, its repo, done today from
// 10:00 down by the hour, and a first record before any tag.
func ledgerProject(t *testing.T, slug string, repos []string, names ...string) *project.Project {
	t.Helper()
	p, err := project.New(project.Options{Slug: slug, Repos: repos})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("# Tasks\n\n## Done\n\n")
	for i, name := range names {
		fmt.Fprintf(&b, "### T%d Task %d\nstatus: done · repo: %s · created: 2026-01-01 · updated: 2026-11-06\ndone_at: 2026-11-06T%02d:00:00Z\n\n", i+1, i+1, name, 10-i)
	}
	if err := os.WriteFile(p.Tasks().TasksPath(), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ledgerTag tags repo's HEAD with an annotated tag dated at.
func ledgerTag(t *testing.T, repo, tag, at string) {
	t.Helper()
	t.Setenv("GIT_COMMITTER_DATE", at)
	gittest.Run(t, repo, "tag", "-a", "-m", tag, tag)
}

// lineWords says a group's rows and lines: "T1/repo", "1.0 09:30@1".
func lineWords(g modapi.DoneGroup) string {
	var out []string
	for _, r := range g.Tasks {
		out = append(out, r.ID+"/"+r.Repo)
	}
	for _, l := range g.Releases {
		out = append(out, fmt.Sprintf("%s %s@%d", l.Label, l.Time, l.Offset))
	}
	return strings.Join(out, ", ")
}

// A plain folder and a repo with no tags: no lines, no filter, no names,
// and nothing that says releases are missing.
func TestDoneLedgerNoLines(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	plain := ledgerProject(t, "plain", nil, "none", "none")
	bare, _ := gittest.Repo(t)
	notags := ledgerProject(t, "notags", []string{bare}, filepath.Base(bare), filepath.Base(bare))
	for _, p := range []*project.Project{plain, notags} {
		got, err := modDone(p, modapi.DoneOptions{Groups: true}, ledgerNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Repos) != 0 || len(got.Groups) != 1 || len(got.Groups[0].Releases) != 0 || got.Total != 2 {
			t.Errorf("%s: %+v", p.Slug, got)
		}
		if w := lineWords(got.Groups[0]); w != "T1/, T2/" {
			t.Errorf("%s: %s", p.Slug, w)
		}
	}
}

// Several repos, one on another host and one with no remote: two lines at
// one minute, each naming its repo, and a filter to each that drops the
// names. Tasks with no repo stay under all and have their own entry.
func TestDoneLedgerSeveralRepos(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	a, _ := gittest.Repo(t)
	b, _ := gittest.Repo(t)
	gittest.Run(t, b, "remote", "add", "origin", "https://gitlab.example/o/b")
	ledgerTag(t, a, "v1.0.0", "2026-11-06T09:30:00Z")
	ledgerTag(t, b, "api/v2.0", "2026-11-06T09:30:00Z")
	an, bn := filepath.Base(a), filepath.Base(b)
	p := ledgerProject(t, "several", []string{a, b}, an, bn, "none")
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("2026-01-01T00:00:00Z tm task.add T1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := modDone(p, modapi.DoneOptions{Groups: true}, ledgerNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Groups) != 1 {
		t.Fatalf("groups: %+v", all.Groups)
	}
	at := "09:30"
	want := fmt.Sprintf("T1/%s, T2/%s, T3/none, %s 1.0.0 %s@1, %s api 2.0 %s@1", an, bn, an, at, bn, at)
	if got := lineWords(all.Groups[0]); got != want {
		t.Errorf("all repos:\n got %s\nwant %s", got, want)
	}
	entries := []modapi.DoneRepo{{Repo: an, Done: 1, Releases: 1}, {Repo: bn, Done: 1, Releases: 1}, {Repo: "none", Done: 1}}
	if !slices.Equal(all.Repos, entries) || all.Total != 3 {
		t.Errorf("repos: %+v, total %d", all.Repos, all.Total)
	}
	for _, c := range []struct {
		repo, want string
		total      int
	}{
		{an, "T1/, 1.0.0 " + at + "@1", 1},
		{bn, "T2/, api 2.0 " + at + "@0", 1},
		{"none", "T3/", 1},
		{"gone", "", 0},
	} {
		got, err := modDone(p, modapi.DoneOptions{Groups: true, Repo: c.repo}, ledgerNow)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.Repos, entries) || got.Total != c.total {
			t.Errorf("%s: repos %+v, total %d", c.repo, got.Repos, got.Total)
		}
		w := ""
		if len(got.Groups) > 0 {
			w = lineWords(got.Groups[0])
		}
		if w != c.want {
			t.Errorf("%s: %s, want %s", c.repo, w, c.want)
		}
		// A range follows the filter too.
		page, err := modDone(p, modapi.DoneOptions{From: "2026-11-06", To: "2026-11-06", Repo: c.repo}, ledgerNow)
		if err != nil || page.Total != c.total {
			t.Errorf("%s: range %+v %v", c.repo, page, err)
		}
	}
	// task.done says the same as $.tasks.done.
	s := &Server{}
	res, perr := s.taskDone(proto.TaskDoneParams{Project: "several", Groups: true})
	if perr != nil {
		t.Fatal(perr)
	}
	d := res.(TaskDone)
	if len(d.Repos) != 3 || d.Repos[0].Releases != 1 || d.Groups[0].Tasks[0].Repo != an || len(d.Groups[0].Releases) != 2 {
		t.Errorf("task.done: %+v", d)
	}
	res, perr = s.taskDone(proto.TaskDoneParams{Project: "several", Groups: true, Repo: an})
	if perr != nil {
		t.Fatal(perr)
	}
	if d := res.(TaskDone); d.Total != 1 || d.Groups[0].Tasks[0].Repo != "" || d.Groups[0].Releases[0].Label != "1.0.0" {
		t.Errorf("task.done of one repo: %+v", d)
	}
	// A path that moved gives no lines and no error, and the rest stands.
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	moved, err := modDone(p, modapi.DoneOptions{Groups: true}, ledgerNow)
	if err != nil || len(moved.Groups[0].Releases) != 1 || moved.Total != 3 {
		t.Errorf("a moved repo: %+v %v", moved, err)
	}
}
