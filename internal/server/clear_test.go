package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestClearSession: session.clear is the human's; a session whose
// agent has no clear prompt (a shell) is refused, an unknown one not
// found.
func TestClearSession(t *testing.T) {
	bare := &Server{log: log.New(os.Stderr, "", 0)}
	if _, perr := bare.clearSession("s-1", caller.Caller{Kind: caller.Coordinator, Project: "demo"}); perr == nil || perr.Code != "human-only" {
		t.Fatalf("a coordinator's clear: %v", perr)
	}
	p := testPaths(t)
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{testSh(t)}, Cwd: t.TempDir()}, &started)
	code := func(err error) string {
		var perr *proto.Error
		if errors.As(err, &perr) {
			return perr.Code
		}
		return ""
	}
	if err := c.Call(proto.MethodSessionClear, proto.SessionIDParams{ID: started.Session.ID}, nil); code(err) != proto.ErrRefused || !strings.Contains(err.Error(), "no clear prompt") {
		t.Errorf("clear of a shell: %v", err)
	}
	if err := c.Call(proto.MethodSessionClear, proto.SessionIDParams{ID: "s-999"}, nil); code(err) != proto.ErrUnknownSession {
		t.Errorf("clear of no session: %v", err)
	}
}

// TestSaveClear: coordinator.clear is refused for an unknown project,
// one with no coordinator running, and a coordinator whose agent has no
// clear prompt, so the user hears why at once.
func TestSaveClear(t *testing.T) {
	p := testPaths(t)
	newWatchProject(t)
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, inject := range map[string]string{"noclear": "", "clears": "[inject]\nclear = \"/clear\"\n"} {
		manifest := "manifest_version = 1\nname = " + strconv.Quote(name) + "\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n" + inject
		if err := os.WriteFile(filepath.Join(p.AgentsDir(), name+".toml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
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
	if err := c.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: "nope"}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("unknown project: %v", err)
	}
	if err := c.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: "demo"}, nil); code(err) != proto.ErrRefused || !strings.Contains(err.Error(), "no coordinator running") {
		t.Errorf("no coordinator: %v", err)
	}
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "noclear", Cwd: t.TempDir(), Role: proto.RoleCoordinator, Project: "demo"}, &started)
	if err := c.Call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: "demo"}, nil); code(err) != proto.ErrRefused || !strings.Contains(err.Error(), "(noclear) has no clear prompt ([inject] clear)") {
		t.Errorf("a coordinator without a clear: %v", err)
	}
	call(t, c, proto.MethodSessionStop, proto.SessionIDParams{ID: started.Session.ID}, nil)
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "clears", Cwd: t.TempDir(), Role: proto.RoleCoordinator, Project: "demo"}, &started)
	var res proto.CoordinatorClearResult
	call(t, c, proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: "demo"}, &res)
	if res.Session != started.Session.ID {
		t.Errorf("asked %q, want %s", res.Session, started.Session.ID)
	}
}

// TestAnswerBound: an answer from a console's question.answer binding
// names the question it answers; it is refused once the menu asks
// another, for an option the question doesn't have, for a multi-select
// question, and for one answered already.
func TestAnswerBound(t *testing.T) {
	var a asks
	a.put("s-1", menu)
	for _, c := range []struct {
		i            int
		ans, q, code string
	}{
		{0, "Blue", "Which color?", proto.ErrNoQuestion},
		{0, "Green", "Which colour?", proto.ErrBadParams},
		{0, "blue", "Which colour?", proto.ErrBadParams},
		{1, "Apple", "Which fruits?", proto.ErrBadParams},
	} {
		if _, perr := a.answer("s-1", c.i, c.ans, c.q); perr == nil || perr.Code != c.code {
			t.Errorf("answer %d %q to %q: %v, want %s", c.i, c.ans, c.q, perr, c.code)
		}
	}
	if res, perr := a.answer("s-1", 0, "Blue", "Which colour?"); perr != nil || res.Remaining != 1 {
		t.Fatalf("answer: %+v %v", res, perr)
	}
	if _, perr := a.answer("s-1", 0, "Red", "Which colour?"); perr == nil || perr.Code != proto.ErrRefused {
		t.Errorf("answered twice: %v", perr)
	}
	if q := a.of("s-1"); q == nil || q.Questions[0].Answer != "Blue" {
		t.Errorf("menu %+v", q)
	}
}

// labActions is lab's manifest listing the actions that need a grant of
// their own.
const labActions = `id = "lab"
name = "Lab"
version = "0.1.0"
api = "1.3"
[provides]
main = "main.js"
[permissions]
project_read = true
notify = true
actions = ["session.clear", "question.answer"]
`

// TestModBindsConsent: session.clear and question.answer are bound only
// by a mod that lists them under [permissions] actions, with the grant:
// $.actions.available, a notification's answer button and a console's
// press are refused without it.
func TestModBindsConsent(t *testing.T) {
	for _, c := range []struct {
		manifest, grants string
		denied           bool
	}{
		{labManifest, `"project_read"`, true},
		{labActions, `"project_read", "notify"`, true},
		{labActions, `"project_read", "notify", "actions"`, false},
	} {
		s := modActionsSetupWith(t, c.manifest, c.grants)
		s.notices = modnotify.New(nil)
		got, err := callLab(s, "actions.available", map[string]any{"id": "session.clear", "target": map[string]string{"session": "s-none"}})
		if denied := brokerCode(err) == modbroker.CodeDenied; denied != c.denied || !denied && (err != nil || got != false) {
			t.Errorf("%s: available = %v, %v; want denied %v", c.grants, got, err, c.denied)
		}
		answer := map[string]string{"session": "s-none", "question": "Which colour?", "option": "Blue"}
		raw, _ := json.Marshal(map[string]any{"title": "Asks", "actions": []any{map[string]any{"id": "question.answer", "target": answer}}})
		_, err = s.CallMod(context.Background(), "lab", "ui.notify", raw)
		if denied := brokerCode(err) == modbroker.CodeDenied; denied != c.denied || err == nil {
			// Granted, it is still refused: no session s-none asks it.
			t.Errorf("%s: notify = %v; want denied %v", c.grants, err, c.denied)
		}
		_, perr := s.modActionCheck(proto.ModActionParams{Mod: "lab", Action: "question.answer", Target: answer}, os.Getpid(), s.callerOf(os.Getpid()))
		if perr == nil || c.denied != strings.Contains(perr.Message, "[permissions] actions") {
			t.Errorf("%s: check = %v; want denied %v", c.grants, perr, c.denied)
		}
	}
}
