package server

import (
	"errors"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestDesignList: design.list gives each design as the mod API's
// designs.list does, its build in after order (an archived task too),
// and refuses an unknown project.
func TestDesignList(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Design it", Status: "review"},
		{Title: "Second", Status: "ready"}, {Title: "First", Status: "done"}}); err != nil {
		t.Fatal(err)
	}
	design := "vault"
	for id, after := range map[int][]string{2: {"T3"}, 3: nil} {
		if _, err := pr.Tasks().SetPlan(human, id, &design, after); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pr.Tasks().Archive(human, 3); err != nil {
		t.Fatal(err)
	}
	title, task, answers := "Obsidian vault", "T1", []string{"YAML"}
	if _, _, err := pr.SetDesign("vault", project.DesignEdit{Title: &title, Task: &task, Answers: &answers}); err != nil {
		t.Fatal(err)
	}
	app, err := client.DialAs(p, proto.ClientApp)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var res proto.DesignListResult
	if err := app.Call(proto.MethodDesignList, proto.DesignListParams{Project: "web"}, &res); err != nil || len(res.Designs) != 1 {
		t.Fatalf("design.list: %+v %v", res, err)
	}
	d := res.Designs[0]
	if d.Name != "vault" || d.Title != title || d.Status != project.DesignBuilding || d.Task != "T1" || d.TaskStatus != "review" ||
		!slices.Equal(d.Tasks, []string{"T2", "T3"}) || d.TasksDone != 1 || !slices.Equal(d.Answers, answers) || len(d.Questions) != 0 {
		t.Errorf("design %+v", d)
	}
	if len(d.Build) != 2 || d.Build[0].ID != "T3" || !d.Build[0].Archived || d.Build[0].Status != "done" ||
		d.Build[1].ID != "T2" || d.Build[1].Title != "Second" || !slices.Equal(d.Build[1].After, []string{"T3"}) {
		t.Errorf("build %+v", d.Build)
	}
	if d.Working || d.Build[0].Working || d.Build[1].Working {
		t.Errorf("working without a thread running a turn: %+v", d)
	}
	var perr *proto.Error
	if err := app.Call(proto.MethodDesignList, proto.DesignListParams{Project: "nope"}, nil); !errors.As(err, &perr) || perr.Code != proto.ErrBadParams {
		t.Errorf("design.list of no project: %v", err)
	}
}

// TestDesignWorking: a design is working, and so is its build task,
// only while a thread of this server on one of its live build tasks
// runs a turn; idle, blocked or exited threads, another project's, a
// paired server's, an archived task's and a coordinator don't count.
func TestDesignWorking(t *testing.T) {
	testPaths(t)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "One", Status: "started"},
		{Title: "Two", Status: "started"}, {Title: "Old", Status: "done"}}); err != nil {
		t.Fatal(err)
	}
	design := "vault"
	for id := 1; id <= 3; id++ {
		if _, err := pr.Tasks().SetPlan(human, id, &design, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pr.Tasks().Archive(human, 3); err != nil {
		t.Fatal(err)
	}
	working := func(sessions ...proto.SessionInfo) (bool, map[string]bool) {
		t.Helper()
		res, perr := designsOf(pr, sessions)
		if perr != nil || len(res.Designs) != 1 {
			t.Fatalf("designs: %+v %v", res, perr)
		}
		tasks := map[string]bool{}
		for _, b := range res.Designs[0].Build {
			tasks[b.ID] = b.Working
		}
		return res.Designs[0].Working, tasks
	}
	th := func(task, state string) proto.SessionInfo {
		return proto.SessionInfo{ID: "s-" + task + state, Role: proto.RoleThread, Project: "web", Thread: "t-1", Task: task, State: state}
	}
	far := th("T1", "working")
	far.Server = "mini"
	other := th("T1", "working")
	other.Project = "api"
	coord := th("T1", "working")
	coord.Role = proto.RoleCoordinator
	if w, ts := working(th("T1", "idle"), th("T2", "blocked"), th("T3", "working"), th("T2", "exited"), far, other, coord); w || ts["T1"] || ts["T2"] || ts["T3"] {
		t.Errorf("working with no live build task's thread running a turn: %v %v", w, ts)
	}
	if w, ts := working(th("T1", "idle"), th("T2", "working")); !w || ts["T1"] || !ts["T2"] {
		t.Errorf("T2's thread runs a turn: %v %v", w, ts)
	}
}
