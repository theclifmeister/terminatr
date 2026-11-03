package server

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestProgramRenameRestartsDirector: program.rename stops a running
// director, renames the program and starts the director again under the
// new name, in the new folder; a taken name is refused with nothing
// stopped.
func TestProgramRenameRestartsDirector(t *testing.T) {
	p := testPaths(t)
	newWatchProject(t)
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\"]\n")
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"sleeps\"\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "sleeps.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var started proto.SessionStartResult
	call(t, c, proto.MethodSessionStart, proto.SessionStartParams{Agent: "sleeps", Role: proto.RoleDirector, Project: "acme"}, &started)

	if err := c.Call(proto.MethodProgramRename, proto.ProgramRenameParams{From: "acme", To: "demo"}, nil); err == nil {
		t.Fatal("renamed onto a project")
	}
	var res proto.ProgramRenameResult
	call(t, c, proto.MethodProgramRename, proto.ProgramRenameParams{From: "acme", To: "studio"}, &res)
	if res.Director == "" || res.Director == started.Session.ID || len(res.Notes) > 0 {
		t.Fatalf("result %+v (first director %s)", res, started.Session.ID)
	}
	dir, _ := program.Dir("studio")
	if res.Dir != dir {
		t.Errorf("dir %q, want %q", res.Dir, dir)
	}
	var list proto.SessionListResult
	call(t, c, proto.MethodSessionList, nil, &list)
	found := false
	for _, s := range list.Sessions {
		if s.Role != proto.RoleDirector {
			continue
		}
		if s.ID == res.Director && s.Project == "studio" {
			found = true
		} else if s.Project == "acme" {
			t.Errorf("the old director still runs: %+v", s)
		}
	}
	if !found {
		t.Errorf("no director of studio in %+v", list.Sessions)
	}
	cfg, _ := config.Load()
	if cfg.ProgramOf("demo") != "studio" {
		t.Errorf("demo in %q", cfg.ProgramOf("demo"))
	}
}

// TestProgramRenamePairedMembers: a program with a paired member is
// renamed (no paired-members refusal any more); a member with no
// invite to tell is a note.
func TestProgramRenamePairedMembers(t *testing.T) {
	p := testPaths(t)
	newWatchProject(t)
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"mini/ios\"]\n")
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var res proto.ProgramRenameResult
	call(t, c, proto.MethodProgramRename, proto.ProgramRenameParams{From: "acme", To: "studio"}, &res)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "mini/ios") {
		t.Errorf("notes: %q", res.Notes)
	}
	cfg, _ := config.Load()
	if t2, ok := cfg.Program("studio"); !ok || !slices.Contains(t2.Projects, "mini/ios") {
		t.Errorf("studio: %+v %v", t2, ok)
	}
}
