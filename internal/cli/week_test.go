package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/history"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// weekBoard is a board with two tasks done today, one open and one
// dropped, and a record older than the stretch.
func weekBoard(now time.Time) string {
	today := now.UTC().Format(time.RFC3339)
	return fmt.Sprintf(`# Tasks

## Now

### T4 Open work
status: ready · created: 2026-10-04 · updated: 2026-10-06

## Done

### T1 First
status: done · created: 2026-10-04 · updated: 2026-10-05

done_at: %[1]s

### T2 Second
status: done · created: 2026-10-05 · updated: 2026-10-06

done_at: %[1]s
`, today)
}

func writeBoard(t *testing.T, p *project.Project, board string) {
	t.Helper()
	if err := os.WriteFile(p.Tasks().TasksPath(), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Path("JOURNAL.md"), []byte("# Journal\n\n2026-10-04T10:00:00Z human project.new web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWeekSetups: a plain folder, a repo with no tags and a repo on
// another host with no pull requests answer the same text and JSON, byte
// for byte, and neither has a releases line, an empty range or chapters.
// So does any of them with git absent or failing.
func TestWeekSetups(t *testing.T) {
	h := newHarness(t)
	human := caller.Caller{Kind: caller.Human}
	noTags, _ := gittest.Repo(t)
	host, inHost := gittest.Repo(t)
	inHost("remote", "add", "origin", "https://git.example.com/o/r.git")
	setups := map[string][]string{"plain-folder": nil, "no-tags": {noTags}, "another-host": {host}}
	board := weekBoard(time.Now())
	for slug, repos := range setups {
		p, err := project.New(project.Options{Slug: slug, Repos: repos})
		if err != nil {
			t.Fatal(err)
		}
		writeBoard(t, p, board)
	}
	run := func(slug string, extra ...string) string {
		t.Helper()
		code, stdout, stderr := h.run(human, "", append([]string{"week", "--project", slug}, extra...)...)
		if code != 0 {
			t.Fatalf("tm week %s %v: %d %s", slug, extra, code, stderr)
		}
		return stdout
	}
	wantJSON, wantText := run("plain-folder", "--json"), run("plain-folder")
	var w history.Week
	if err := json.Unmarshal([]byte(wantJSON), &w); err != nil {
		t.Fatal(err)
	}
	if w.Done != 2 || w.StillOpen != 1 || len(w.Days) != 1 || w.Weeks != 1 {
		t.Errorf("numbers: %s", wantJSON)
	}
	for _, word := range []string{"releases", "designs", "last_finish"} {
		if strings.Contains(wantJSON, word) {
			t.Errorf("JSON has %s: %s", word, wantJSON)
		}
	}
	for _, word := range []string{"Releases", "Designs", "→", "Dropped", "Nothing finished"} {
		if strings.Contains(wantText, word) {
			t.Errorf("text has %s: %s", word, wantText)
		}
	}
	if !strings.Contains(wantText, ", so far 2 done\n") || !strings.Contains(wantText, " 2\n") || !strings.Contains(wantText, "Still open 1 task\n") {
		t.Errorf("text: %s", wantText)
	}
	check := func(when string) {
		t.Helper()
		for slug := range setups {
			if got := run(slug, "--json"); got != wantJSON {
				t.Errorf("%s, %s: JSON\n%s\nwant\n%s", slug, when, got, wantJSON)
			}
			if got := run(slug); got != wantText {
				t.Errorf("%s, %s: text\n%s\nwant\n%s", slug, when, got, wantText)
			}
		}
	}
	check("with git")
	t.Setenv("PATH", t.TempDir())
	check("no git")
	if runtime.GOOS != "windows" {
		spy := t.TempDir()
		if err := os.WriteFile(filepath.Join(spy, "git"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", spy)
		check("git failing")
	}
}

// TestWeekSeveralRepos: each repo has its line and its range, in the text
// and the JSON, and no total.
func TestWeekSeveralRepos(t *testing.T) {
	h := newHarness(t)
	human := caller.Caller{Kind: caller.Human}
	web, inWeb := gittest.Repo(t)
	inWeb("commit", "--allow-empty", "-m", "one")
	inWeb("tag", "v1.0.0")
	inWeb("commit", "--allow-empty", "-m", "two")
	inWeb("tag", "v1.1.0")
	site, inSite := gittest.Repo(t)
	inSite("commit", "--allow-empty", "-m", "one")
	inSite("tag", "v1.0.0")
	p, err := project.New(project.Options{Slug: "both", Repos: []string{web, site}})
	if err != nil {
		t.Fatal(err)
	}
	writeBoard(t, p, weekBoard(time.Now()))
	_, stdout, _ := h.run(human, "", "week", "--project", "both", "--json")
	var w history.Week
	if err := json.Unmarshal([]byte(stdout), &w); err != nil {
		t.Fatal(err)
	}
	want := []history.WeekRelease{
		{Repo: filepath.Base(web), Count: 2, First: "1.0.0", Last: "1.1.0"},
		{Repo: filepath.Base(site), Count: 1, First: "1.0.0", Last: "1.0.0"},
	}
	if fmt.Sprint(w.Releases) != fmt.Sprint(want) {
		t.Fatalf("releases: %s", stdout)
	}
	_, text, _ := h.run(human, "", "week", "--project", "both")
	wantText := "\nReleases\n  " + filepath.Base(web) + " 2 1.0.0 → 1.1.0\n  " + filepath.Base(site) + " 1 1.0.0\n"
	if !strings.Contains(text, wantText) {
		t.Errorf("text %q, want %q", text, wantText)
	}
	if strings.Contains(text, " 3 ") {
		t.Errorf("a total: %q", text)
	}
}

// TestWeekEmptyText: an empty week says so with the last finish date and
// how to see more, and a project that never finished says that. A week
// with only dropped tasks is empty too, and says it dropped them.
func TestWeekEmptyText(t *testing.T) {
	h := newHarness(t)
	human := caller.Caller{Kind: caller.Human}
	week := func(slug string, extra ...string) string {
		t.Helper()
		code, stdout, stderr := h.run(human, "", append([]string{"week", "--project", slug}, extra...)...)
		if code != 0 {
			t.Fatalf("%d %s", code, stderr)
		}
		return stdout
	}
	now := time.Now()
	old := now.AddDate(0, 0, -30)
	lastDay := old.Format("2 Jan")
	if old.Year() != now.Year() {
		lastDay = old.Format("2 Jan 2006")
	}
	quiet := fmt.Sprintf(`# Tasks

## Done

### T1 First
status: done · created: 2026-01-01 · updated: 2026-01-01

done_at: %s
`, old.UTC().Format(time.RFC3339))

	empty, err := project.New(project.Options{Slug: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	writeBoard(t, empty, quiet)
	text := week("empty")
	if !strings.Contains(text, "Nothing finished. The last task finished on "+lastDay+".\n") ||
		!strings.Contains(text, "tm week --weeks 4 covers the last four weeks") || strings.Contains(text, "so far") || strings.Contains(text, "█") {
		t.Errorf("an empty week: %q", text)
	}
	if long := week("empty", "--weeks", "8"); !strings.Contains(long, ", so far 1 done") || strings.Contains(long, "Nothing finished") || !strings.HasPrefix(long, "WEEKS ") {
		t.Errorf("eight weeks: %q", long)
	}

	dropped, err := project.New(project.Options{Slug: "dropped"})
	if err != nil {
		t.Fatal(err)
	}
	writeBoard(t, dropped, quiet+fmt.Sprintf(`
### T2 Second
status: dropped · created: 2026-01-01 · updated: %s
`, now.Format(time.DateOnly)))
	text = week("dropped")
	if !strings.Contains(text, "Nothing finished. The last task finished on "+lastDay+".\n") || !strings.Contains(text, "Dropped 1 task\n") || !strings.Contains(text, "Still open 0 tasks\n") {
		t.Errorf("only dropped: %q", text)
	}

	if _, err := project.New(project.Options{Slug: "new"}); err != nil {
		t.Fatal(err)
	}
	if text := week("new"); !strings.Contains(text, "Nothing finished yet.\n") {
		t.Errorf("a new project: %q", text)
	}
}

// TestWeekDesignsText: the designs the week touched, by title, none of
// the tasks' names or numbers: a page copied from tm week has none.
func TestWeekDesignsText(t *testing.T) {
	h := newHarness(t)
	human := caller.Caller{Kind: caller.Human}
	p, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	title := "Board and Library"
	if _, _, err := p.SetDesign("board", project.DesignEdit{Title: &title}); err != nil {
		t.Fatal(err)
	}
	board := strings.Replace(weekBoard(time.Now()), "status: done · created: 2026-10-04", "status: done · design: board · created: 2026-10-04", 1)
	writeBoard(t, p, board)
	_, text, _ := h.run(human, "", "week", "--project", "web")
	if !strings.Contains(text, "\nDesigns\n  Board and Library · 1 done, 0 open\n") {
		t.Errorf("designs: %q", text)
	}
	_, asJSON, _ := h.run(human, "", "week", "--project", "web", "--json")
	numbers := regexp.MustCompile(`\b[Tt]-?[0-9]+\b|First|Second|Open work`)
	for _, out := range []string{text, asJSON} {
		if m := numbers.FindString(out); m != "" {
			t.Errorf("names a task or thread (%s): %s", m, out)
		}
	}
}

// TestWeekFlags: --weeks takes a whole number in range.
func TestWeekFlags(t *testing.T) {
	h := newHarness(t)
	human := caller.Caller{Kind: caller.Human}
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"0", "-1", "x", "1.5", fmt.Sprint(history.MaxWeeks + 1)} {
		if code, _, _ := h.run(human, "", "week", "--project", "web", "--weeks="+bad); code != 2 {
			t.Errorf("--weeks %s: exit %d", bad, code)
		}
	}
	if code, _, _ := h.run(human, "", "week", "--project", "web", "extra"); code != 2 {
		t.Errorf("an argument: exit %d", code)
	}
	if code, _, stderr := h.run(human, "", "week", "--project", "web", "--weeks", fmt.Sprint(history.MaxWeeks)); code != 0 {
		t.Errorf("the most weeks: %d %s", code, stderr)
	}
	if code, stdout, _ := h.run(human, "", "week", "--help"); code != 0 || !strings.Contains(stdout, "usage: tm week") {
		t.Errorf("help: %d %q", code, stdout)
	}
}

// TestWeekSeat: a thread reads its own project's numbers, in the server
// as the forward runs it, and not another's.
func TestWeekSeat(t *testing.T) {
	h := newHarness(t)
	for _, slug := range []string{"web", "other"} {
		if _, err := project.New(project.Options{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	th := caller.Caller{Kind: caller.Thread, Project: "web", Thread: "t-0001"}
	h.env[caller.EnvProject] = "web"
	if code, _, stderr := h.run(th, "", "week"); code != 0 {
		t.Errorf("bare, in its project: %d %s", code, stderr)
	}
	if code, _, _ := h.run(th, "", "week", "--project", "other"); code == 0 {
		t.Errorf("another project's numbers")
	}
	_, want, _ := h.run(caller.Caller{Kind: caller.Human}, "", "week", "--project", "web", "--json")
	res := RunInServer(proto.CLIRunParams{Args: []string{"week", "--project", "web", "--json"}, Cwd: h.cwd}, th)
	if res.Code != 0 || res.Stdout != want {
		t.Errorf("in the server: %d %q %q, want %q", res.Code, res.Stdout, res.Stderr, want)
	}
	res = RunInServer(proto.CLIRunParams{Args: []string{"week", "--project", "other"}, Cwd: h.cwd}, th)
	if res.Code == 0 {
		t.Errorf("in the server, another project's numbers: %q", res.Stdout)
	}
}

// TestWeekSpans: the heading says what its two days don't share.
func TestWeekSpans(t *testing.T) {
	d := func(s string) time.Time {
		v, err := time.Parse(time.DateOnly, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct{ from, to, want string }{
		{"2026-11-02", "2026-11-06", "Mon 2 – Fri 6 Nov"},
		{"2026-11-02", "2026-11-02", "Mon 2 Nov"},
		{"2026-10-26", "2026-11-06", "Mon 26 Oct – Fri 6 Nov"},
		{"2026-12-28", "2027-01-06", "Mon 28 Dec 2026 – Wed 6 Jan 2027"},
	} {
		if got := spanWords(d(c.from), d(c.to)); got != c.want {
			t.Errorf("%s to %s: %q, want %q", c.from, c.to, got, c.want)
		}
	}
}

// TestWeekInReviewTemplate: the template files a task that says where the
// numbers come from, that the page has no task or thread numbers and how
// it reaches the Library.
func TestWeekInReviewTemplate(t *testing.T) {
	h := newHarness(t)
	human := caller.Caller{Kind: caller.Human}
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := h.run(human, "", "task", "add", "week 45", "--project", "web", "--template", "week-in-review"); code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	code, stdout, stderr := h.run(human, "", "task", "show", "T1", "--project", "web")
	if code != 0 {
		t.Fatalf("%d %s", code, stderr)
	}
	for _, want := range []string{"Week in review: week 45", "tm week", "no task or thread numbers", "tm report --attach", "tm library add", "reaches the Library", "own release range", "last finish date"} {
		if !strings.Contains(strings.Join(strings.Fields(stdout), " "), want) {
			t.Errorf("no %q in\n%s", want, stdout)
		}
	}
}
