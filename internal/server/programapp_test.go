package server

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestProgramAppMethods: the program methods the apps call act as the
// user, as tm program does: a route files tasks with their cross refs
// and an umbrella, task.get shows a cross ref's state from deps.json,
// program.get reads the dashboard, a member's task is edited, the
// program channel sends and reads; all refused programs-off while
// programs are off, but program.delete, which takes the user's confirm.
func TestProgramAppMethods(t *testing.T) {
	p := testPaths(t)
	demo := newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := demo.Tasks().Add(human, []tasks.NewTask{{Title: "The API", Status: "ready"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := program.New(program.Options{Slug: "acme", Goal: "Ship the shop"}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	code := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return perr.Code
		}
		return ""
	}

	var routed program.RouteResult
	call(t, c, proto.MethodProgramRoute, ProgramRouteParams{Program: "acme", Route: program.RouteRequest{Umbrella: "Ship it",
		Tasks: []program.RouteTask{{Project: "web", Title: "The page", After: []string{"demo/T1"}}}}}, &routed)
	if routed.Umbrella != "T1" || len(routed.Tasks) != 1 || routed.Tasks[0].Ref != "T1" || routed.Tasks[0].Result != "created" {
		t.Fatalf("route %+v", routed)
	}
	if err := c.Call(proto.MethodProgramRoute, ProgramRouteParams{Program: "acme", Route: program.RouteRequest{
		Tasks: []program.RouteTask{{Project: "elsewhere", Title: "x"}}}}, nil); err == nil {
		t.Error("routed to a project that isn't a member")
	}

	// The ticker's dependency states, as task.get shows them.
	if err := program.WriteDeps("acme", program.Snapshot{States: map[string]program.Dep{"demo/T1": {State: program.DepOpen, Status: "ready", Title: "The API"}},
		Waits: []program.Wait{{Task: "web/T1", Title: "The page", Status: "ready", After: "demo/T1"}}}); err != nil {
		t.Fatal(err)
	}
	var task SideTask
	call(t, c, proto.MethodTaskGet, proto.TaskGetParams{Project: "web", Task: "T1"}, &task)
	if len(task.After) != 1 || task.After[0].Ref != "demo/T1" || task.After[0].State != program.DepOpen || task.After[0].Title != "The API" || task.After[0].Word == "" {
		t.Errorf("task.get after %+v", task.After)
	}

	var d ProgramDetail
	call(t, c, proto.MethodProgramGet, proto.ProgramParams{Program: "acme"}, &d)
	if d.Slug != "acme" || d.Goal != "Ship the shop" || len(d.Members) != 2 || d.Members[1].Project != "web" || d.Director != "" {
		t.Errorf("program.get %+v", d)
	}
	if len(d.Umbrellas) != 1 || len(d.Umbrellas[0].Parts) != 1 || d.Umbrellas[0].Parts[0].Ref != "web/T1" {
		t.Errorf("umbrellas %+v", d.Umbrellas)
	}
	if d.Deps["demo/T1"].State != program.DepOpen || len(d.Waits) != 1 || d.Tasks["web/T1"].Title != "The page" || d.Tasks["demo/T1"].Title != "The API" {
		t.Errorf("deps %+v waits %+v tasks %+v", d.Deps, d.Waits, d.Tasks)
	}

	var edited proto.ProgramTaskEditResult
	call(t, c, proto.MethodProgramTaskEdit, proto.ProgramTaskEditParams{Program: "acme", Ref: "web/T1", Priority: "must", Note: "first"}, &edited)
	if edited.Result != "edited" {
		t.Errorf("edit %+v", edited)
	}
	if err := c.Call(proto.MethodProgramTaskEdit, proto.ProgramTaskEditParams{Program: "acme", Ref: "web/T1"}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("nothing to change: %v", err)
	}
	if err := c.Call(proto.MethodProgramTaskEdit, proto.ProgramTaskEditParams{Program: "acme", Ref: "web/T1", Status: "done"}, nil); err == nil {
		t.Error("the user's done went through a program edit")
	}

	var sent proto.ProgramSendResult
	call(t, c, proto.MethodProgramSend, proto.ProgramSendParams{Program: "acme", To: "web", Text: "Start with the page"}, &sent)
	if sent.ID == "" || sent.Bytes == 0 {
		t.Fatalf("send %+v", sent)
	}
	var read proto.ProgramReadResult
	call(t, c, proto.MethodProgramRead, proto.ProgramReadParams{Program: "acme", ID: sent.ID}, &read)
	if read.ID != sent.ID || !strings.Contains(read.Text, "Start with the page") {
		t.Errorf("read %+v", read)
	}

	// An umbrella not in review isn't the user's to accept yet; a
	// question that isn't open isn't answered.
	if err := c.Call(proto.MethodProgramUmbrellaAccept, proto.ProgramUmbrellaParams{Program: "acme", Task: "T1"}, nil); err == nil {
		t.Error("accepted an umbrella that isn't in review")
	}
	if err := c.Call(proto.MethodProgramAnswer, proto.ProgramAnswerParams{Program: "acme", Ref: "Q9", Other: "yes"}, nil); err == nil {
		t.Error("answered a question that isn't there")
	}

	writeConfig(t, "[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	for _, m := range []struct {
		name   string
		params any
	}{
		{proto.MethodProgramGet, proto.ProgramParams{Program: "acme"}},
		{proto.MethodProgramRoute, ProgramRouteParams{Program: "acme"}},
		{proto.MethodProgramTaskEdit, proto.ProgramTaskEditParams{Program: "acme", Ref: "web/T1", Note: "x"}},
		{proto.MethodProgramSend, proto.ProgramSendParams{Program: "acme", To: "web", Text: "x"}},
		{proto.MethodProgramRead, proto.ProgramReadParams{Program: "acme", ID: sent.ID}},
		{proto.MethodProgramAnswer, proto.ProgramAnswerParams{Program: "acme", Ref: "Q1", Other: "x"}},
		{proto.MethodProgramUmbrellaSendBack, proto.ProgramUmbrellaParams{Program: "acme", Task: "T1", Note: "x"}},
	} {
		if err := c.Call(m.name, m.params, nil); code(err) != config.ProgramsOff {
			t.Errorf("off: %s: %v", m.name, err)
		}
	}
	// Off, a cross ref is plain text: no state.
	task = SideTask{}
	call(t, c, proto.MethodTaskGet, proto.TaskGetParams{Project: "web", Task: "T1"}, &task)
	if len(task.After) != 1 || task.After[0].State != "" {
		t.Errorf("off: task.get after %+v", task.After)
	}

	if err := c.Call(proto.MethodProgramDelete, proto.ProgramDeleteParams{Program: "acme", Confirm: "acm"}, nil); code(err) != "not-confirmed" {
		t.Errorf("unconfirmed delete: %v", err)
	}
	var del proto.ProgramDeleteResult
	call(t, c, proto.MethodProgramDelete, proto.ProgramDeleteParams{Program: "acme", Confirm: "acme"}, &del)
	if del.Trash == "" {
		t.Errorf("delete %+v", del)
	}
	if dir, _ := program.Dir("acme"); dir != "" {
		if _, err := os.Stat(dir); err == nil {
			t.Error("the program's folder stayed")
		}
	}
	if cfg, _ := config.Load(); cfg.ProgramOf("web") != "" {
		t.Error("web is still in a program")
	}
}
