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
	return modActionsSetupWith(t, labManifest, grants)
}

// modActionsSetupWith is modActionsSetup with lab's manifest.
func modActionsSetupWith(t *testing.T, manifest, grants string) *Server {
	t.Helper()
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "lab")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": manifest, "main.js": ""} {
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

// TestModActionsNeedRead: without project_read, available is refused,
// on a task and on a session alike.
func TestModActionsNeedRead(t *testing.T) {
	s := modActionsSetup(t, "")
	if _, err := callLab(s, "actions.available", map[string]any{"id": "task.accept", "target": map[string]string{"project": "demo", "task": "T2"}}); brokerCode(err) != modbroker.CodeDenied {
		t.Errorf("available without project_read: %v", err)
	}
	if _, err := callLab(s, "actions.available", map[string]any{"id": "session.attach", "target": map[string]string{"session": "s-1"}}); brokerCode(err) != modbroker.CodeDenied {
		t.Errorf("available on a session without project_read: %v", err)
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
	press := proto.ModActionParams{Mod: "lab", Action: "task.accept", Target: target}
	tok := checkToken(t, s, press, pid)
	if _, perr := s.modActionDone(proto.ModActionDoneParams{ModActionParams: press, OK: true, Token: tok}, pid); perr != nil {
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
		{ModActionParams: proto.ModActionParams{Mod: "nope", Action: "task.accept", Target: target}},
		{ModActionParams: proto.ModActionParams{Mod: "lab", Action: "thread.merge", Target: target}},
		{ModActionParams: proto.ModActionParams{Mod: "lab", Action: "task.accept", Target: map[string]string{"project": "demo"}}},
		{ModActionParams: proto.ModActionParams{Mod: "lab", Action: "task.accept", Target: map[string]string{"project": "other", "task": "T1"}}},
		{ModActionParams: proto.ModActionParams{Mod: "lab", Action: "session.attach", Target: map[string]string{"session": "s-none"}}},
	} {
		if _, perr := s.modActionDone(p, pid); perr == nil {
			t.Errorf("%+v accepted", p)
		}
		// The console's check before it runs a binding refuses the same.
		if _, perr := s.modActionCheck(p.ModActionParams, pid); perr == nil {
			t.Errorf("check %+v accepted", p)
		}
	}
	if _, perr := s.modActionCheck(press, pid); perr != nil {
		t.Errorf("check of a binding in reach: %v", perr)
	}
}

func checkToken(t *testing.T, s *Server, p proto.ModActionParams, pid int) string {
	t.Helper()
	res, perr := s.modActionCheck(p, pid)
	r, _ := res.(proto.ModActionCheckResult)
	if perr != nil || r.Token == "" {
		t.Fatalf("check %+v = %+v, %v", p, res, perr)
	}
	return r.Token
}

// TestModActionDoneToken: mod.action.done needs the press token its
// console's mod.action.check gave for that binding, once (T288): a done
// with none, a used one, one for another binding or console, or an
// expired one is refused and the mod hears nothing.
func TestModActionDoneToken(t *testing.T) {
	s := modActionsSetup(t, `"project_read"`)
	sub := s.SubscribeMod("lab", []string{"project_read"}, func(slug string) bool { return slug == "demo" })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ev, err := sub.Next(ctx); err != nil || ev.Name != modbus.ModReady {
		t.Fatalf("first event %v, %v", ev.Name, err)
	}
	pid := os.Getpid()
	accept := proto.ModActionParams{Mod: "lab", Action: "task.accept", Target: map[string]string{"project": "demo", "task": "T2"}}
	show := proto.ModActionParams{Mod: "lab", Action: "task.show", Target: map[string]string{"project": "demo", "task": "T2"}}
	done := func(p proto.ModActionParams, tok string, pid int) *proto.Error {
		_, perr := s.modActionDone(proto.ModActionDoneParams{ModActionParams: p, OK: true, Token: tok}, pid)
		return perr
	}
	if done(accept, "", pid) == nil {
		t.Error("a done with no token")
	}
	if done(accept, "0123456789abcdef0123456789abcdef", pid) == nil {
		t.Error("a done with a made-up token")
	}
	tok := checkToken(t, s, accept, pid)
	if done(show, tok, pid) == nil {
		t.Error("a token for another action")
	}
	if done(accept, tok, pid) == nil {
		t.Error("a token tried on another binding is still good")
	}
	tok = checkToken(t, s, accept, pid)
	if done(accept, tok, pid+1) == nil {
		t.Error("a token from another console")
	}
	tok = checkToken(t, s, accept, pid)
	if perr := done(accept, tok, pid); perr != nil {
		t.Fatalf("the press's own token: %v", perr)
	}
	if done(accept, tok, pid) == nil {
		t.Error("a token used twice")
	}
	tok = checkToken(t, s, accept, pid)
	s.presses.mu.Lock()
	pr := s.presses.tokens[tok]
	pr.made = pr.made.Add(-proto.ModActionTokenTTL)
	s.presses.tokens[tok] = pr
	s.presses.mu.Unlock()
	if done(accept, tok, pid) == nil {
		t.Error("an expired token")
	}
	// Only the one good done reached the mod.
	if ev, err := sub.Next(ctx); err != nil || ev.Name != modbus.ActionDone {
		t.Fatalf("action.done = %+v, %v", ev, err)
	}
	if n := sub.Len(); n != 0 {
		t.Errorf("%d more events for refused dones", n)
	}
}

// TestPressTokensCap: one console holds at most maxPresses tokens; a
// new one drops its oldest, and other consoles' are kept.
func TestPressTokensCap(t *testing.T) {
	var p pressTokens
	now := time.Now()
	other := p.issue(2, "k", now)
	first := p.issue(1, "k", now)
	for i := 1; i <= maxPresses; i++ {
		p.issue(1, "k", now.Add(time.Duration(i)*time.Millisecond))
	}
	if len(p.tokens) != maxPresses+1 {
		t.Errorf("%d tokens held", len(p.tokens))
	}
	if p.take(first, 1, "k", now) {
		t.Error("the oldest token outlived the cap")
	}
	if !p.take(other, 2, "k", now) {
		t.Error("another console's token was dropped")
	}
}
