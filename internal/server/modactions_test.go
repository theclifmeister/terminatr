package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// modActionsSetup installs mod lab, enabled for project demo with
// grants, and a demo board: T1 started, T2 in review.
func modActionsSetup(t *testing.T, grants string) *Server {
	t.Helper()
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "lab")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": labManifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[mods.lab]\nenabled = true\ngrants = [" + grants + "]\nprojects = [\"demo\"]\n"
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	p := newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "One", Status: "started"}, {Title: "Two", Status: "review"}}); err != nil {
		t.Fatal(err)
	}
	return &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
}

func callLab(s *Server, method string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return s.CallMod(context.Background(), "lab", method, raw)
}

func brokerCode(err error) int {
	if e, ok := err.(*modbroker.Error); ok {
		return e.Code
	}
	return 0
}

// TestModActions: $.actions.list is the core's table; available checks
// the live board, and needs project_read and a project the mod runs
// for, so it can't be used to read a board.
func TestModActions(t *testing.T) {
	s := modActionsSetup(t, `"project_read"`)
	got, err := callLab(s, "actions.list", nil)
	list, _ := got.([]modapi.Action)
	if err != nil || len(list) == 0 || list[1].ID != "task.delegate" || list[1].Key != "D" || !list[1].Confirm {
		t.Fatalf("actions.list = %+v, %v", got, err)
	}
	for _, c := range []struct {
		id, task string
		want     bool
	}{
		{"task.accept", "T2", true},
		{"task.accept", "T1", false},
		{"task.delegate", "T1", false},
		{"task.show", "T9", false},
		{"thread.merge", "T2", false},
	} {
		got, err := callLab(s, "actions.available", map[string]any{"id": c.id, "target": map[string]string{"project": "demo", "task": c.task}})
		if err != nil || got != c.want {
			t.Errorf("available %s %s = %v, %v; want %v", c.id, c.task, got, err, c.want)
		}
	}
	// Once accepted, it waits on the coordinator: no longer available.
	p, _ := project.Open("demo")
	if _, err := p.AskAccept(human, "T2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := callLab(s, "actions.available", map[string]any{"id": "task.accept", "target": map[string]string{"project": "demo", "task": "T2"}}); got != false {
		t.Error("accept still available while an item asks it")
	}
	if _, err := callLab(s, "actions.available", map[string]any{"id": "task.accept", "target": map[string]string{"project": "other", "task": "T1"}}); brokerCode(err) != modbroker.CodeDenied {
		t.Errorf("a project it doesn't run for: %v", err)
	}
	if _, err := callLab(s, "actions.available", map[string]any{"id": "task.accept", "target": map[string]string{"project": "demo"}}); err == nil {
		t.Error("a target of the wrong shape")
	}
	if got, err := callLab(s, "actions.available", map[string]any{"id": "session.attach", "target": map[string]string{"session": "s-none"}}); err != nil || got != false {
		t.Errorf("a session that isn't running: %v, %v", got, err)
	}
}

// TestModActionsNeedRead: without project_read, available on a task is
// refused; on a session it isn't.
func TestModActionsNeedRead(t *testing.T) {
	s := modActionsSetup(t, "")
	if _, err := callLab(s, "actions.available", map[string]any{"id": "task.accept", "target": map[string]string{"project": "demo", "task": "T2"}}); brokerCode(err) != modbroker.CodeDenied {
		t.Errorf("available without project_read: %v", err)
	}
	if _, err := callLab(s, "actions.available", map[string]any{"id": "session.attach", "target": map[string]string{"session": "s-1"}}); err != nil {
		t.Errorf("available on a session: %v", err)
	}
	if _, err := callLab(s, "actions.list", nil); err != nil {
		t.Errorf("actions.list: %v", err)
	}
}

// TestModActionDone: a console's mod.action.done reaches the mod whose
// row was pressed as action.done; a bad mod, action, target or project
// is refused.
func TestModActionDone(t *testing.T) {
	s := modActionsSetup(t, `"project_read"`)
	sub := s.SubscribeMod("lab", []string{"project_read"}, func(slug string) bool { return slug == "demo" })
	other := s.SubscribeMod("other-mod", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ev, err := sub.Next(ctx); err != nil || ev.Name != modbus.ModReady {
		t.Fatalf("first event %v, %v", ev.Name, err)
	}
	pid := os.Getpid()
	target := map[string]string{"project": "demo", "task": "T2"}
	if _, perr := s.modActionDone(proto.ModActionDoneParams{Mod: "lab", Action: "task.accept", Target: target, OK: true}, pid); perr != nil {
		t.Fatal(perr)
	}
	ev, err := sub.Next(ctx)
	d, _ := ev.Data.(modbus.Done)
	if err != nil || ev.Name != modbus.ActionDone || d.Action != "task.accept" || d.Target.Task != "T2" || !d.OK {
		t.Fatalf("action.done = %+v, %v", ev, err)
	}
	if other.Len() != 0 {
		t.Error("another mod heard of lab's action")
	}
	for _, p := range []proto.ModActionDoneParams{
		{Mod: "nope", Action: "task.accept", Target: target},
		{Mod: "lab", Action: "thread.merge", Target: target},
		{Mod: "lab", Action: "task.accept", Target: map[string]string{"project": "demo"}},
		{Mod: "lab", Action: "task.accept", Target: map[string]string{"project": "other", "task": "T1"}},
	} {
		if _, perr := s.modActionDone(p, pid); perr == nil {
			t.Errorf("%+v accepted", p)
		}
	}
}
