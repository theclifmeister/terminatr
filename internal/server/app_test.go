package server

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
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
