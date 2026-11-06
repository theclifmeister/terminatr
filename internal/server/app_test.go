package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestAppHelloJournal: a control hello that names the Mac app's bridge
// (proto.ClientApp) keeps the human's rights on the human socket, and
// what it does is journaled as "human (app)"; the same call without it,
// as "human". task.ask checks the live board.
func TestAppHelloJournal(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Ship it"}, {Title: "Review me"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().SetStatus(human, 2, tasks.Review, ""); err != nil {
		t.Fatal(err)
	}

	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	plain, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()

	var who proto.CallerInfo
	if err := app.Call(proto.MethodCallerWho, nil, &who); err != nil || who.Kind != string(caller.Human) {
		t.Fatalf("caller.who through the app: %+v %v", who, err)
	}
	var res proto.TaskAskResult
	if err := app.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "T2", Kind: project.KindAccept}, &res); err != nil || !res.Asked {
		t.Fatalf("accept T2: %+v %v", res, err)
	}
	if err := app.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "T2", Kind: project.KindAccept}, &res); err != nil || res.Asked {
		t.Fatalf("accept T2 again: %+v %v", res, err)
	}
	if err := plain.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "1", Kind: project.KindDelegate}, &res); err != nil || !res.Asked {
		t.Fatalf("delegate T1: %+v %v", res, err)
	}
	var perr *proto.Error
	for _, bad := range []proto.TaskAskParams{
		{Project: "web", Task: "T1", Kind: project.KindAccept},   // not in review
		{Project: "web", Task: "T9", Kind: project.KindDelegate}, // no such task
		{Project: "web", Task: "T2", Kind: "delete"},
		{Project: "web", Task: "x", Kind: project.KindDelegate},
		{Project: "nope", Task: "T1", Kind: project.KindDelegate},
	} {
		if err := app.Call(proto.MethodTaskAsk, bad, nil); !errors.As(err, &perr) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	for _, want := range []string{" human (app) task.accept.ask T2", " human task.delegate.ask T1"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	if items, _ := pr.Inbox(); len(items) != 2 {
		t.Errorf("inbox %+v", items)
	}

	// The server socket vouches for no human: the app's hello there
	// gains nothing.
	srv, err := client.DialAt(p.Socket, proto.KindControl, proto.Protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if err := srv.Call(proto.MethodTaskAsk, proto.TaskAskParams{Project: "web", Task: "T1", Kind: project.KindDelegate}, nil); !onHumanSocketOnly(err) {
		t.Errorf("task.ask on the server socket: %v", err)
	}
}

// TestTaskActions: task.accept, task.send-back and task.delegate apply
// tm task accept's and send-back's checks, each refusal with its code,
// and journal the ask as the caller.
func TestTaskActions(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Ship it"}, {Title: "Review me"}, {Title: "Review me too"}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{2, 3} {
		if _, err := pr.Tasks().SetStatus(human, id, tasks.Review, ""); err != nil {
			t.Fatal(err)
		}
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	code := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return perr.Code
		}
		return ""
	}
	var res proto.TaskAskResult
	if err := app.Call(proto.MethodTaskAccept, proto.TaskActionParams{Project: "web", Task: "T2"}, &res); err != nil || !res.Asked {
		t.Fatalf("accept T2: %+v %v", res, err)
	}
	if err := app.Call(proto.MethodTaskAccept, proto.TaskActionParams{Project: "web", Task: "T2"}, &res); err != nil || res.Asked {
		t.Fatalf("accept T2 again: %+v %v", res, err)
	}
	if err := app.Call(proto.MethodTaskSendBack, proto.TaskActionParams{Project: "web", Task: "T3", Note: "the\ntests fail"}, &res); err != nil || !res.Asked {
		t.Fatalf("send back T3: %+v %v", res, err)
	}
	if err := app.Call(proto.MethodTaskDelegate, proto.TaskActionParams{Project: "web", Task: "1"}, &res); err != nil || !res.Asked {
		t.Fatalf("delegate T1: %+v %v", res, err)
	}
	for _, c := range []struct {
		method string
		p      proto.TaskActionParams
		code   string
	}{
		{proto.MethodTaskAccept, proto.TaskActionParams{Project: "web", Task: "T1"}, "not-accept"},
		{proto.MethodTaskDelegate, proto.TaskActionParams{Project: "web", Task: "T2"}, "not-delegate"},
		{proto.MethodTaskSendBack, proto.TaskActionParams{Project: "web", Task: "T1", Note: "x"}, "not-send-back"},
		{proto.MethodTaskSendBack, proto.TaskActionParams{Project: "web", Task: "T3", Note: " \n"}, "invalid-note"},
		{proto.MethodTaskSendBack, proto.TaskActionParams{Project: "web", Task: "T3", Note: strings.Repeat("x", project.MaxSendBackNote+1)}, "invalid-note"},
		{proto.MethodTaskSendBack, proto.TaskActionParams{Project: "web", Task: "T2", Note: "no"}, "already-asked"},
		{proto.MethodTaskAccept, proto.TaskActionParams{Project: "web", Task: "T2", Note: "x"}, proto.ErrBadParams},
		{proto.MethodTaskAccept, proto.TaskActionParams{Project: "web", Task: "x"}, proto.ErrBadParams},
		{proto.MethodTaskAccept, proto.TaskActionParams{Project: "nope", Task: "T2"}, proto.ErrBadParams},
		{proto.MethodTaskAccept, proto.TaskActionParams{Project: "web", Task: "T9"}, "unknown-task"},
	} {
		if err := app.Call(c.method, c.p, nil); code(err) != c.code {
			t.Errorf("%s %+v: %v, want %s", c.method, c.p, err, c.code)
		}
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	for _, want := range []string{" human (app) task.accept.ask T2", " human (app) task.sendback.ask T3 the tests fail", " human (app) task.delegate.ask T1"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	if items, _ := pr.Inbox(); len(items) != 3 {
		t.Errorf("inbox %+v", items)
	}
}

// TestTaskAdd: task.add adds a task as tm task add does, journaled as
// the caller; the same title again answers that task, and a bad title
// or project is refused with its code.
func TestTaskAdd(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var res proto.TaskAddResult
	if err := app.Call(proto.MethodTaskAdd, proto.TaskAddParams{Project: "web", Title: " Ship it ", Notes: "Soon."}, &res); err != nil || res.Task != "T1" || res.Existing {
		t.Fatalf("task.add: %+v %v", res, err)
	}
	if err := app.Call(proto.MethodTaskAdd, proto.TaskAddParams{Project: "web", Title: "Ship it"}, &res); err != nil || res.Task != "T1" || !res.Existing {
		t.Fatalf("task.add again: %+v %v", res, err)
	}
	for _, c := range []struct {
		p    proto.TaskAddParams
		code string
	}{
		{proto.TaskAddParams{Project: "web", Title: " "}, "empty-title"},
		{proto.TaskAddParams{Project: "web", Title: "a\nb"}, "invalid-title"},
		{proto.TaskAddParams{Project: "web", Title: "x", Notes: "## Done"}, "invalid-notes"},
		{proto.TaskAddParams{Project: "nope", Title: "x"}, proto.ErrBadParams},
	} {
		var perr *proto.Error
		if err := app.Call(proto.MethodTaskAdd, c.p, nil); !errors.As(err, &perr) || perr.Code != c.code {
			t.Errorf("task.add %+v: %v, want %s", c.p, err, c.code)
		}
	}
	b, err := pr.Tasks().Load()
	if err != nil || len(b.Tasks) != 1 || b.Tasks[0].Title != "Ship it" || b.Tasks[0].Notes != "Soon." {
		t.Fatalf("board %+v %v", b, err)
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	if want := " human (app) task.add T1 Ship it"; !strings.Contains(string(journal), want) {
		t.Errorf("journal lacks %q:\n%s", want, journal)
	}
}

// TestTaskPriority: task.priority sets a task's priority as tm task edit
// --priority does, journaled as the caller; again is unchanged, and a
// bad priority, task or project is refused with its code. task.get
// carries it.
func TestTaskPriority(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Ship it"}}); err != nil {
		t.Fatal(err)
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var res proto.TaskPriorityResult
	if err := app.Call(proto.MethodTaskPriority, proto.TaskPriorityParams{Project: "web", Task: "T1", Priority: "must"}, &res); err != nil || !res.Changed {
		t.Fatalf("task.priority: %+v %v", res, err)
	}
	if err := app.Call(proto.MethodTaskPriority, proto.TaskPriorityParams{Project: "web", Task: "T1", Priority: "must"}, &res); err != nil || res.Changed {
		t.Fatalf("task.priority again: %+v %v", res, err)
	}
	for _, c := range []struct {
		p    proto.TaskPriorityParams
		code string
	}{
		{proto.TaskPriorityParams{Project: "web", Task: "T1", Priority: "high"}, "invalid-priority"},
		{proto.TaskPriorityParams{Project: "web", Task: "T9", Priority: "must"}, "unknown-task"},
		{proto.TaskPriorityParams{Project: "web", Task: "x", Priority: "must"}, proto.ErrBadParams},
		{proto.TaskPriorityParams{Project: "nope", Task: "T1", Priority: "must"}, proto.ErrBadParams},
	} {
		var perr *proto.Error
		if err := app.Call(proto.MethodTaskPriority, c.p, nil); !errors.As(err, &perr) || perr.Code != c.code {
			t.Errorf("task.priority %+v: %v, want %s", c.p, err, c.code)
		}
	}
	var got SideTask
	if err := app.Call(proto.MethodTaskGet, proto.TaskGetParams{Project: "web", Task: "T1"}, &got); err != nil || got.Priority != tasks.Must {
		t.Fatalf("task.get: %+v %v", got, err)
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	if want := " human (app) task.priority T1 must"; !strings.Contains(string(journal), want) {
		t.Errorf("journal lacks %q:\n%s", want, journal)
	}
}

// TestTaskGet: task.get reads a task of the board with its notes and
// steps; an unknown task or project is refused with its code.
func TestTaskGet(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Later", Status: "open", Notes: "Why.", Steps: []string{"a", "b"}}}); err != nil {
		t.Fatal(err)
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var res SideTask
	if err := app.Call(proto.MethodTaskGet, proto.TaskGetParams{Project: "web", Task: "T1"}, &res); err != nil ||
		res.ID != 1 || res.Title != "Later" || res.Notes != "Why." || len(res.Steps) != 2 || res.Steps[1].Text != "b" {
		t.Fatalf("task.get: %+v %v", res, err)
	}
	for _, c := range []struct {
		p    proto.TaskGetParams
		code string
	}{
		{proto.TaskGetParams{Project: "web", Task: "T9"}, "unknown-task"},
		{proto.TaskGetParams{Project: "web", Task: "x"}, proto.ErrBadParams},
		{proto.TaskGetParams{Project: "nope", Task: "T1"}, proto.ErrBadParams},
	} {
		var perr *proto.Error
		if err := app.Call(proto.MethodTaskGet, c.p, nil); !errors.As(err, &perr) || perr.Code != c.code {
			t.Errorf("task.get %+v: %v, want %s", c.p, err, c.code)
		}
	}
}

// TestTaskDone: task.done lists the board's done tasks in board order
// without their notes and steps, and refuses an unknown project.
func TestTaskDone(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Shipped", Status: "done", Notes: "Why."},
		{Title: "Later", Status: "open"}, {Title: "Also shipped", Status: "done", Steps: []string{"a"}}}); err != nil {
		t.Fatal(err)
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var res TaskDone
	if err := app.Call(proto.MethodTaskDone, proto.TaskDoneParams{Project: "web"}, &res); err != nil || len(res.Tasks) != 2 ||
		res.Tasks[0].Title != "Also shipped" || res.Tasks[1].Title != "Shipped" || res.Tasks[0].Steps != nil || res.Tasks[1].Notes != "" ||
		res.Tasks[0].DoneAt == "" {
		t.Fatalf("task.done: %+v %v", res, err)
	}
	// Newest first by when each finished: not by priority, not by id, not
	// by updated; a task no journal line or thread dates comes by its day.
	if err := os.WriteFile(pr.Tasks().TasksPath(), []byte(`# Tasks

## Done

### T1 Must, oldest
status: done · updated: 2026-11-01
priority: must
done_at: 2026-10-01T10:00:00Z

### T2 Newest
status: done · updated: 2026-10-01
done_at: 2026-11-02T10:00:00Z

### T3 By its day
status: done · updated: 2026-10-20
done_at: 2026-10-20

### T4 Nothing but updated
status: done · updated: 2026-10-25

### T5 Not done
status: open · updated: 2026-11-03
`), 0o644); err != nil {
		t.Fatal(err)
	}
	var dated TaskDone
	if err := app.Call(proto.MethodTaskDone, proto.TaskDoneParams{Project: "web"}, &dated); err != nil {
		t.Fatal(err)
	}
	var ids []int
	for _, x := range dated.Tasks {
		ids = append(ids, x.ID)
	}
	if !slices.Equal(ids, []int{2, 4, 3, 1}) || dated.Tasks[0].DoneAt != "2026-11-02T10:00:00Z" || dated.Tasks[1].DoneAt != "" || dated.Tasks[2].DoneAt != "2026-10-20" {
		t.Fatalf("task.done's order: %v %+v", ids, dated.Tasks)
	}
	var perr *proto.Error
	if err := app.Call(proto.MethodTaskDone, proto.TaskDoneParams{Project: "nope"}, nil); !errors.As(err, &perr) || perr.Code != proto.ErrBadParams {
		t.Errorf("task.done of no project: %v", err)
	}
}

// TestTaskDoneGroups: task.done's groups and ranges, the archive counted,
// and read for a range only when asked; task.get finds an archived task.
// A plain folder, a repo with no tags, several repos and one on another
// host get the same groups, and git never runs.
func TestTaskDoneGroups(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	// Real repositories with no tags, one on another host.
	repo := func(remote string) string {
		t.Helper()
		dir := t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", args, err, out)
			}
		}
		return dir
	}
	a, b, c := repo("https://github.com/o/a"), repo("https://github.com/o/b"), repo("https://gitlab.example/o/c")
	bin := t.TempDir()
	mark := filepath.Join(bin, "git-ran")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\ntouch '"+mark+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	now := time.Now().UTC()
	today := now.Format(time.DateOnly)
	old := now.AddDate(-1, 0, 0).Format(time.DateOnly)
	var first []SideDoneGroup
	for slug, repos := range map[string][]string{"plain": nil, "one": {a}, "several": {a, b}, "other": {c}} {
		pr, err := project.New(project.Options{Slug: slug, Repos: repos})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pr.Tasks().TasksPath(), []byte("# Tasks\n\n## Done\n\n### T1 Today\nstatus: done · updated: "+today+"\ndone_at: "+today+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pr.Tasks().ArchivePath(), []byte("# Archive\n\n### T2 Old\nstatus: done · updated: "+old+"\ndone_at: "+old+"\n\nWhy.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Remove(mark) // New may look at the repos; task.done may not.
		var res TaskDone
		if err := app.Call(proto.MethodTaskDone, proto.TaskDoneParams{Project: slug, Groups: true, TZ: "UTC"}, &res); err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		if _, err := os.Stat(mark); err == nil {
			t.Errorf("%s: task.done ran git", slug)
		}
		if res.Total != 2 || len(res.Groups) != 2 || res.Groups[0].Title != "Today" || !res.Groups[0].Open || res.Groups[0].Tasks[0].ID != 1 ||
			res.Groups[1].Archived != 1 || !res.Groups[1].Open || res.Groups[1].Tasks[0].Notes != "" {
			t.Fatalf("%s: groups %+v", slug, res)
		}
		if first == nil {
			first = res.Groups
		} else if !reflect.DeepEqual(first, res.Groups) {
			t.Errorf("%s: %+v, another setup %+v", slug, res.Groups, first)
		}
		// The archive for a range only when asked.
		var board, both TaskDone
		if err := app.Call(proto.MethodTaskDone, proto.TaskDoneParams{Project: slug, From: old, To: today}, &board); err != nil || board.Total != 1 {
			t.Errorf("%s: the board's range: %+v %v", slug, board, err)
		}
		if err := app.Call(proto.MethodTaskDone, proto.TaskDoneParams{Project: slug, From: old, To: old, Archived: true, Limit: 1}, &both); err != nil ||
			both.Total != 1 || len(both.Tasks) != 1 || !both.Tasks[0].Archived || both.Next != 0 {
			t.Errorf("%s: the archive's range: %+v %v", slug, both, err)
		}
		var got SideTask
		if err := app.Call(proto.MethodTaskGet, proto.TaskGetParams{Project: slug, Task: "T2"}, &got); err != nil || !got.Archived || got.Notes != "Why." {
			t.Errorf("%s: task.get of an archived task: %+v %v", slug, got, err)
		}
	}
}

// TestTaskTemplates: task.templates lists the bundled bug, spike and
// chore templates, and task.add lays one on the title, its notes
// unless the params have some, its steps; an unknown one is refused.
func TestTaskTemplates(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var res proto.TaskTemplatesResult
	if err := app.Call(proto.MethodTaskTemplates, proto.TaskDoneParams{Project: "web"}, &res); err != nil {
		t.Fatal(err)
	}
	var bug *proto.TaskTemplate
	names := map[string]bool{}
	for i, tp := range res.Templates {
		names[tp.Name] = true
		if tp.Name == "bug" {
			bug = &res.Templates[i]
		}
	}
	if !names["bug"] || !names["spike"] || !names["chore"] || bug == nil || !strings.Contains(bug.Title, "{title}") || len(bug.Steps) == 0 {
		t.Fatalf("task.templates: %+v", res)
	}
	var added proto.TaskAddResult
	if err := app.Call(proto.MethodTaskAdd, proto.TaskAddParams{Project: "web", Title: "Login fails", Template: bug.Ref}, &added); err != nil {
		t.Fatalf("task.add with %s: %v", bug.Ref, err)
	}
	if err := app.Call(proto.MethodTaskAdd, proto.TaskAddParams{Project: "web", Title: "Clean up", Notes: "Mine.", Template: "chore"}, &added); err != nil {
		t.Fatalf("task.add with chore: %v", err)
	}
	var perr *proto.Error
	if err := app.Call(proto.MethodTaskAdd, proto.TaskAddParams{Project: "web", Title: "x", Template: "nope"}, nil); !errors.As(err, &perr) || perr.Code != "unknown-template" {
		t.Errorf("task.add with no template: %v", err)
	}
	b, err := pr.Tasks().Load()
	if err != nil || len(b.Tasks) != 2 {
		t.Fatalf("board %+v %v", b, err)
	}
	want, _, _ := mod.Template{Title: bug.Title}.Apply("Login fails")
	if got := b.Tasks[0]; got.Title != want || got.Notes != bug.Notes || len(got.Steps) != len(bug.Steps) {
		t.Errorf("bug task %+v, want title %q, notes %q, %d steps", got, want, bug.Notes, len(bug.Steps))
	}
	if got := b.Tasks[1]; got.Notes != "Mine." || len(got.Steps) == 0 {
		t.Errorf("chore task %+v, want its own notes and the template's steps", got)
	}
}

// TestProjectOpen: project.open starts the project's coordinator with
// its coordinator agent, journaled as the caller, answers the running
// one after, and refuses an inactive or unknown project.
func TestProjectOpen(t *testing.T) {
	p := testPaths(t)
	t.Setenv("PATH", nowhere+"/usr/bin:/bin")
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"fake\"\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "fake.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.New(project.Options{Slug: "off"}); err != nil {
		t.Fatal(err)
	}
	for slug, key := range map[string]string{"web": "coordinator_agent", "off": "coordinator_agent"} {
		if err := config.SetProject(slug, key, "fake"); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetProject("off", "active", false); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var res proto.ProjectOpenResult
	if err := app.Call(proto.MethodProjectOpen, proto.ProjectOpenParams{Project: "web", Cols: 100, Rows: 30}, &res); err != nil || !res.Started || res.Session == "" {
		t.Fatalf("open web: %+v %v", res, err)
	}
	var again proto.ProjectOpenResult
	if err := app.Call(proto.MethodProjectOpen, proto.ProjectOpenParams{Project: "web"}, &again); err != nil || again.Started || again.Session != res.Session {
		t.Fatalf("open web again: %+v %v", again, err)
	}
	var list proto.SessionListResult
	call(t, app, proto.MethodSessionList, nil, &list)
	for _, s := range list.Sessions {
		if s.ID == res.Session && (s.Role != proto.RoleCoordinator || s.Project != "web" || s.Agent != "fake") {
			t.Errorf("coordinator %+v", s)
		}
	}
	var perr *proto.Error
	if err := app.Call(proto.MethodProjectOpen, proto.ProjectOpenParams{Project: "off"}, nil); !errors.As(err, &perr) || perr.Code != "project-inactive" {
		t.Errorf("open an inactive project: %v", err)
	}
	if err := app.Call(proto.MethodProjectOpen, proto.ProjectOpenParams{Project: "nope"}, nil); !errors.As(err, &perr) {
		t.Errorf("open no project: %v", err)
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	if want := " human (app) coordinator.start " + res.Session; strings.Count(string(journal), want) != 1 {
		t.Errorf("journal lacks one %q:\n%s", want, journal)
	}
}

// TestProgramOpen: program.open opens a program's director for the app,
// as the sidebar's director row does: started once (its director_agent,
// role director, the program's folder), the running one after, the
// start journaled in the program as the app; refused while programs are
// off and for a program that isn't there.
func TestProgramOpen(t *testing.T) {
	p := testPaths(t)
	t.Setenv("PATH", nowhere+"/usr/bin:/bin")
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"fake\"\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "fake.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	g, err := program.New(program.Options{Slug: "acme", Goal: "Ship the shop"})
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"web\"]\ndirector_agent = \"fake\"\n")
	startServer(t, p)
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var perr *proto.Error
	if err := app.Call(proto.MethodProgramOpen, proto.ProgramOpenParams{Program: "acme"}, nil); !errors.As(err, &perr) || perr.Code != config.ProgramsOff {
		t.Fatalf("open with programs off: %v", err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\"]\ndirector_agent = \"fake\"\n")
	var res proto.ProgramOpenResult
	if err := app.Call(proto.MethodProgramOpen, proto.ProgramOpenParams{Program: "acme", Cols: 100, Rows: 30}, &res); err != nil || !res.Started || res.Session == "" {
		t.Fatalf("open acme: %+v %v", res, err)
	}
	var again proto.ProgramOpenResult
	if err := app.Call(proto.MethodProgramOpen, proto.ProgramOpenParams{Program: "acme"}, &again); err != nil || again.Started || again.Session != res.Session {
		t.Fatalf("open acme again: %+v %v", again, err)
	}
	var list proto.SessionListResult
	call(t, app, proto.MethodSessionList, nil, &list)
	found := false
	for _, s := range list.Sessions {
		if s.ID == res.Session {
			found = true
			if s.Role != proto.RoleDirector || s.Project != "acme" || s.Agent != "fake" {
				t.Errorf("director %+v", s)
			}
		}
	}
	if !found {
		t.Errorf("no session %s", res.Session)
	}
	if err := app.Call(proto.MethodProgramOpen, proto.ProgramOpenParams{Program: "nope"}, nil); !errors.As(err, &perr) || perr.Code != "unknown-program" {
		t.Errorf("open no program: %v", err)
	}
	if err := app.Call(proto.MethodProgramOpen, proto.ProgramOpenParams{}, nil); !errors.As(err, &perr) || perr.Code != proto.ErrBadParams {
		t.Errorf("open no name: %v", err)
	}
	journal, _ := os.ReadFile(g.Path("JOURNAL.md"))
	if want := " human (app) director.start " + res.Session; strings.Count(string(journal), want) != 1 {
		t.Errorf("journal lacks one %q:\n%s", want, journal)
	}
}
