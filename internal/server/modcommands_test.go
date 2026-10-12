package server

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const cmdManifest = `id = "lab"
name = "Labeller"
version = "0.1.0"
api = "1.0"
[provides]
main = "main.js"
views = [{ id = "out", title = "Out", slot = "full" }]
[[provides.commands]]
id = "relabel"
title = "Relabel"
key = "L"
[permissions]
views = true
`

// cmdServer is a bare Server with mod lab installed and enabled for
// project demo.
func cmdServer(t *testing.T) *Server {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "lab")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mod.toml": cmdManifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[mods.lab]\nenabled = true\ngrants = [\"views\"]\nprojects = [\"demo\"]\n[mods.memory-tab]\nenabled = false\n"
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	newWatchProject(t)
	return &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}
}

// TestModCommand: the human's mod.command reaches the mod as
// command.run with the project and selection; from a console's key it
// lets the mod open a view, from tm mod run it doesn't; an agent, an
// unknown command and a project the mod doesn't run for are refused.
func TestModCommand(t *testing.T) {
	s := cmdServer(t)
	sub := s.SubscribeMod("lab", []string{"views"}, nil)
	if ev := next(t, sub); ev.Name != modbus.ModReady {
		t.Fatalf("first event %s", ev.Name)
	}
	sel := &proto.ModTarget{Project: "demo", Task: "T3"}
	if _, perr := s.modCommand(proto.ModCommandParams{Mod: "lab", Command: "relabel", Project: "demo", Selection: sel}, 7, human); perr != nil {
		t.Fatal(perr)
	}
	ev := next(t, sub)
	c, ok := ev.Data.(modbus.Command)
	if ev.Name != modbus.CommandRun || !ok || c.Command != "relabel" || c.Project != "demo" || c.Selection == nil || c.Selection.Task != "T3" {
		t.Fatalf("event %s %+v", ev.Name, ev.Data)
	}
	if s.modViewStore().Open("lab", "out") {
		t.Fatal("tm mod run let the mod open a view")
	}
	res, perr := s.modCommand(proto.ModCommandParams{Mod: "lab", Command: "relabel", Console: true}, 7, human)
	if perr != nil {
		t.Fatal(perr)
	}
	if ev := next(t, sub); ev.Data.(modbus.Command).Selection != nil {
		t.Fatalf("selection %+v", ev.Data)
	}
	if !s.modViewStore().Open("lab", "out") {
		t.Fatal("a console's command didn't let the mod open a view")
	}
	// The open comes after the answer's After, for a session's console
	// to tell it from older ones.
	after := res.(proto.ModCommandResult).After
	if o := s.modViewStore().Opens(7); len(o) != 1 || o[0].Seq <= after {
		t.Fatalf("opens %+v after %d", o, after)
	}
	for name, tc := range map[string]struct {
		p    proto.ModCommandParams
		c    caller.Caller
		code string
	}{
		"agent":         {proto.ModCommandParams{Mod: "lab", Command: "relabel"}, caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}, "human-only"},
		"no mod":        {proto.ModCommandParams{Mod: "nope", Command: "relabel"}, human, proto.ErrBadParams},
		"no command":    {proto.ModCommandParams{Mod: "lab", Command: "nope"}, human, proto.ErrBadParams},
		"other project": {proto.ModCommandParams{Mod: "lab", Command: "relabel", Project: "other"}, human, proto.ErrBadParams},
		"selection":     {proto.ModCommandParams{Mod: "lab", Command: "relabel", Project: "demo", Selection: &proto.ModTarget{Project: "x"}}, human, proto.ErrBadParams},
	} {
		if _, perr := s.modCommand(tc.p, 7, tc.c); perr == nil || perr.Code != tc.code {
			t.Errorf("%s: %v, want %s", name, perr, tc.code)
		}
	}
	if sub.Len() != 0 {
		t.Fatal("a refused command reached the mod")
	}
}
