package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// TestProjectsWatch: projects.watch answers every project but the
// archived ones, with what the sidebar shows, and every watcher sees
// the changes: a new thread, its question, its PR's checks and failing
// PR polls, a task to review, a question to the user, a new project.
func TestProjectsWatch(t *testing.T) {
	paths := testPaths(t)
	p := newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetProject("old", "archived", true); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envWatchPoll, "50ms")
	startServer(t, paths)
	var streams []chan []SideProject
	for i := range 2 {
		st, first, err := WatchProjects(paths, false)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		if i == 0 && (len(first) != 1 || first[0].Slug != "demo" || first[0].Safety == nil) {
			t.Fatalf("first answer: %+v, want demo alone, with its settings", first)
		}
		ch := make(chan []SideProject, 64)
		go func() {
			defer close(ch)
			for {
				ps, err := st.Next()
				if err != nil {
					return
				}
				ch <- ps
			}
		}()
		streams = append(streams, ch)
	}
	demo := func(ps []SideProject) SideProject {
		for _, sp := range ps {
			if sp.Slug == "demo" {
				return sp
			}
		}
		return SideProject{}
	}
	thread1 := func(ps []SideProject) SideThread {
		if th := demo(ps).Threads; len(th) > 0 {
			return th[0]
		}
		return SideThread{}
	}
	await := func(what string, ok func([]SideProject) bool) {
		t.Helper()
		for i, ch := range streams {
			deadline := time.After(3 * time.Second)
			for done := false; !done; {
				select {
				case ps, open := <-ch:
					if !open {
						t.Fatalf("watcher %d: stream ended waiting for %s", i, what)
					}
					done = ok(ps)
				case <-deadline:
					t.Fatalf("watcher %d never saw %s", i, what)
				}
			}
		}
	}

	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "Work", Status: "started"}}); err != nil {
		t.Fatal(err)
	}
	rec, err := thread.Create(p, thread.Record{Title: "work on T1", Task: "T1"})
	if err != nil {
		t.Fatal(err)
	}
	await("the new thread", func(ps []SideProject) bool {
		th := thread1(ps)
		return th.Record != nil && th.Record.ID == rec.ID && th.Record.Task == "T1"
	})

	if _, err := thread.UpdateStatus(p, rec.ID, func(st *thread.Status) error {
		st.NeedsYou = "which port?"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	await("the thread's question", func(ps []SideProject) bool {
		st := thread1(ps).Status
		return st != nil && st.NeedsYou == "which port?"
	})

	ts := ticker.StatePath(paths.Sessions)
	if err := os.WriteFile(ts, []byte(`{"projects": {"demo": {"gh_fails": 3}}, "threads": {"demo/`+rec.ID+`": {"pr": {"number": 8, "url": "https://x/8", "state": "OPEN", "checks": "fail"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	await("the ticker's PR", func(ps []SideProject) bool { return thread1(ps).PR == 8 && thread1(ps).Checks == "fail" })

	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "Check it", Status: "review", Steps: []string{"look"}}}); err != nil {
		t.Fatal(err)
	}
	await("the task to review", func(ps []SideProject) bool {
		n := demo(ps).NeedsYou
		return len(n) == 1 && n[0].Title == "Check it" && len(n[0].Steps) == 1 && n[0].Task().Ref() == "T2"
	})

	if _, err := p.AddQuestion(project.Question{Header: "Ship", Question: "ship?", Options: []project.QuestionOption{{Label: "yes"}, {Label: "no"}}}); err != nil {
		t.Fatal(err)
	}
	await("the question", func(ps []SideProject) bool { return demo(ps).Questions == 1 })

	if _, err := project.New(project.Options{Slug: "next"}); err != nil {
		t.Fatal(err)
	}
	await("the new project", func(ps []SideProject) bool { return len(ps) == 2 && ps[1].Slug == "next" })
}

// TestProjectsWatchHostFails: the ticker's failing PR polls count for a
// project with a PR host only, as the coordinator's panel has it.
func TestProjectsWatchHostFails(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	snap := &projectSnap{p: p, timing: ticker.Timing{GHFailing: true, GHFails: 2}, noPRHost: true}
	if n := snap.side(SideProject{}).PRHostFails; n != 0 {
		t.Errorf("no PR host: %d fails, want 0", n)
	}
	snap.noPRHost = false
	if n := snap.side(SideProject{}).PRHostFails; n != 2 {
		t.Errorf("a PR host: %d fails, want 2", n)
	}
}

// TestSideHubShared: the watches share one look a poll: a watch asking
// again within it gets what the last look saw, without looking; a wake
// (a new generation) looks at once. A wake reads again only the
// projects whose files changed.
func TestSideHubShared(t *testing.T) {
	paths := testPaths(t)
	newWatchProject(t)
	s := &Server{opts: Options{Paths: paths}}
	b, v := s.sideProjects(0, time.Hour, false)
	var ps []SideProject
	if err := json.Unmarshal(b, &ps); err != nil || len(ps) != 1 {
		t.Fatalf("first look: %s, %v", b, err)
	}
	if _, err := project.New(project.Options{Slug: "next"}); err != nil {
		t.Fatal(err)
	}
	if _, v2 := s.sideProjects(0, time.Hour, false); v2 != v {
		t.Errorf("a look within the poll: version %d, want the last look's %d", v2, v)
	}
	b, v2 := s.sideProjects(1, time.Hour, false)
	if v2 == v {
		t.Fatalf("a wake didn't look: %s", b)
	}
	if _, v3 := s.sideProjects(1, time.Hour, false); v3 != v2 {
		t.Errorf("an unchanged look: version %d, want %d", v3, v2)
	}

	ts := ticker.StatePath(paths.Sessions)
	snap, err := s.projects.stamped("demo", ts, 1)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.projects.stamped("demo", ts, 2); again != snap {
		t.Error("a wake read an unchanged project again")
	}
	if err := os.WriteFile(filepath.Join(snap.p.Dir, "TASKS.md"), []byte("# Tasks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.projects.stamped("demo", ts, 2); again == snap {
		t.Error("a changed project wasn't read again")
	}
}

// TestSideHubDetail: the dashboard's look (detail) carries the
// project's goal, counts, inbox, usage and its threads' report PR and
// task; the sidebars' look carries none of it.
func TestSideHubDetail(t *testing.T) {
	paths := testPaths(t)
	p := newWatchProject(t)
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "Work", Status: "started", Steps: []string{"a"}}}); err != nil {
		t.Fatal(err)
	}
	rec, err := thread.Create(p, thread.Record{Title: "work", Task: "T1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := thread.StoreReport(p, rec.ID, "PR: https://github.com/o/r/pull/3\n## Report\nDone.\n## Next\nMerge it\n", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddItem("report", rec.ID, "T1: reported", false); err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: paths}}
	look := func(detail bool) SideProject {
		t.Helper()
		b, _ := s.sideProjects(0, time.Hour, detail)
		var ps []SideProject
		if err := json.Unmarshal(b, &ps); err != nil || len(ps) != 1 {
			t.Fatalf("look: %s, %v", b, err)
		}
		return ps[0]
	}
	d := look(true)
	if d.Counts["in_motion"] != 1 || len(d.Items) != 1 || len(d.Threads) != 1 ||
		d.Threads[0].ReportPR != "https://github.com/o/r/pull/3" || d.Threads[0].Task == nil || len(d.Threads[0].Task.Steps) != 1 {
		t.Errorf("detail: %+v", d)
	}
	sp := look(false)
	if sp.Counts != nil || sp.Items != nil || len(sp.Threads) != 1 || sp.Threads[0].ReportPR != "" || sp.Threads[0].Task != nil {
		t.Errorf("the sidebars' look has the dashboard's: %+v", sp)
	}
}
