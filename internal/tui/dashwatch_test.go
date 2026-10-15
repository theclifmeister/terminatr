//go:build unix

package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modstore"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// dashProjects are two linked projects with what the dashboard shows of
// them: tasks with steps, a thread with a report's PR and one resolved
// with its usage, inbox items, a question, a failing PR host, and a
// mod's badge. It returns alpha.
func dashProjects(t *testing.T) *project.Project {
	t.Helper()
	human := caller.Caller{Kind: caller.Human}
	alpha, err := project.New(project.Options{Slug: "alpha", Goal: "Ship it"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.New(project.Options{Slug: "beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := alpha.Tasks().Add(human, []tasks.NewTask{
		{Title: "Build", Status: "started", Steps: []string{"a", "b"}},
		{Title: "Check", Status: "review"},
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := thread.Create(alpha, thread.Record{Title: "build", Task: "T1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := thread.StoreReport(alpha, rec.ID, "PR: https://github.com/o/r/pull/7\n## Report\nDone.\n## Next\nMerge it\n", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	old, err := thread.Create(alpha, thread.Record{Title: "old", Task: "T2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := thread.Update(alpha, old.ID, func(r *thread.Record) error {
		r.State, r.Usage = thread.Resolved, thread.Usage{Turns: 3, Input: 100, CostUSD: 0.5}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := alpha.AddItem("report", rec.ID, "T1: reported", false); err != nil {
		t.Fatal(err)
	}
	if _, err := alpha.AddItem(link.KindMessage, "beta", "a message", false); err != nil {
		t.Fatal(err)
	}
	if _, err := alpha.AddQuestion(project.Question{Header: "Ship", Question: "ship?", Options: []project.QuestionOption{{Label: "yes"}, {Label: "no"}}}); err != nil {
		t.Fatal(err)
	}
	if err := link.Set(human, "alpha", "beta", "both", nil); err != nil {
		t.Fatal(err)
	}
	modFolderAt(t, "look", map[string]string{
		"mod.toml": "id = \"look\"\nname = \"Look\"\nversion = \"1.0.0\"\napi = \"1.5\"\n[provides]\nmain = \"main.ts\"\n[permissions]\nproject_read = true\n",
		"main.ts":  "",
	})
	if err := mod.Enable(human, "look", ""); err != nil {
		t.Fatal(err)
	}
	state, err := home.ModStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := modstore.Annotate(filepath.Join(state, "look"), "alpha", modstore.Task("T1"), "ci", []widget.Span{{Text: "green"}}); err != nil {
		t.Fatal(err)
	}
	return alpha
}

// TestDashboardWatch: with a server, the dashboard's Load shows the
// projects the server's projects.watch with detail sends, and they are
// what the dashboard reads from the files itself (loadProjects), but
// for a report's text: the rows show its PR line alone.
func TestDashboardWatch(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	alpha := dashProjects(t)
	paths := startServer(t)
	ts := ticker.StatePath(paths.Sessions)
	if err := os.WriteFile(ts, []byte(`{"projects": {"alpha": {"gh_fails": 2}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	src := &ServerSource{Paths: paths, Caller: caller.Caller{Kind: caller.Human}}
	defer src.Close()

	files := func() string {
		t.Helper()
		sc := src.slow.load(paths.AgentsDir())
		d := Data{Mods: sc.reg.Mods}
		src.loadProjects(&d, sc.cfg)
		for i := range d.Projects {
			pd := &d.Projects[i]
			for j, th := range pd.Threads {
				if th.Report != nil {
					pd.Threads[j].Report = &thread.Report{PR: th.Report.PR}
				}
			}
			// None is none, on the wire.
			if len(pd.Checkouts) == 0 {
				pd.Checkouts = nil
			}
			if len(pd.TaskUsage) == 0 {
				pd.TaskUsage = nil
			}
		}
		b, _ := json.MarshalIndent(d.Projects, "", " ")
		return string(b)
	}
	watched := func() (string, bool) {
		ps, ok := src.watched()
		b, _ := json.MarshalIndent(ps, "", " ")
		return string(b), ok
	}
	await := func(what string) {
		t.Helper()
		var got, want string
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			var ok bool
			if got, ok = watched(); ok && got == files() {
				return
			}
		}
		want = files()
		t.Fatalf("%s: the watch sent\n%s\nthe files have\n%s", what, got, want)
	}
	await("the projects")

	d := src.Load()
	if len(d.Projects) != 2 {
		t.Fatalf("Load: %d projects, want 2", len(d.Projects))
	}
	a := d.Projects[0]
	if a.Goal != "Ship it" || a.Counts["needs_you"] != 1 || a.Unread != 2 || a.Questions != 1 ||
		a.Usage.Turns != 3 || len(a.Badges[modstore.Task("T1")]) != 1 || len(a.Links) != 1 || a.Links[0].Unread != 1 {
		t.Errorf("Load's alpha: %+v", a)
	}
	if len(a.Threads) != 1 || a.Threads[0].Report == nil || a.Threads[0].Report.PR != "https://github.com/o/r/pull/7" ||
		a.Threads[0].TaskRec == nil || len(a.Threads[0].TaskRec.Steps) != 2 {
		t.Errorf("Load's alpha threads: %+v", a.Threads)
	}

	// What changes behind the server's back streams too.
	if _, err := alpha.Tasks().Add(caller.Caller{Kind: caller.Human}, []tasks.NewTask{{Title: "More", Status: "ready"}}); err != nil {
		t.Fatal(err)
	}
	if err := link.Set(caller.Caller{Kind: caller.Human}, "alpha", "beta", "send", nil); err != nil {
		t.Fatal(err)
	}
	state, err := home.ModStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := modstore.Annotate(filepath.Join(state, "look"), "alpha", modstore.Task("T2"), "ci", []widget.Span{{Text: "red"}}); err != nil {
		t.Fatal(err)
	}
	await("a task, a link and a badge changed")
}

// TestDashboardTouch: after an action of the source changes project
// files, Load reads them itself until the watch sent what came after,
// touchWait at most; without a watch it always does.
func TestDashboardTouch(t *testing.T) {
	f := &sideFeed{}
	if _, ok := f.current(time.Time{}, time.Second); ok {
		t.Error("a feed that never ran is current")
	}
	f.putList([]ProjectData{{Slug: "a"}})
	if ps, ok := f.current(time.Time{}, time.Second); !ok || len(ps) != 1 {
		t.Errorf("a live feed: %v, %v", ps, ok)
	}
	if _, ok := f.current(time.Now(), time.Second); ok {
		t.Error("a list older than the action is current")
	}
	if _, ok := f.current(time.Now().Add(-2*time.Second), time.Second); !ok {
		t.Error("a list older than the action is not current after the wait")
	}
	since := time.Now()
	f.putList([]ProjectData{{Slug: "a"}, {Slug: "b"}})
	if ps, ok := f.current(since, time.Second); !ok || len(ps) != 2 {
		t.Errorf("a list sent after the action: %v, %v", ps, ok)
	}
	ps, _ := f.current(since, time.Second)
	ps[0].Slug = "changed"
	if again, _ := f.current(since, time.Second); again[0].Slug != "a" {
		t.Error("current shares the feed's list")
	}
}
