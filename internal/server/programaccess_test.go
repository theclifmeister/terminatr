package server

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// programFixture makes projects demo and web, programs acme (demo is a
// member) and beta, and a server over them.
func programFixture(t *testing.T) (*Server, string) {
	t.Helper()
	testPaths(t)
	home := os.Getenv("TERMINATR_HOME")
	newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"acme", "beta"} {
		if _, err := program.New(program.Options{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{log: log.New(os.Stderr, "", 0), opts: Options{Paths: client.Paths{Home: home}}}
	return s, realPath(filepath.Join(home, "projects", "_programs"))
}

const programsOn = "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\"]\n[programs.beta]\nprojects = [\"web\"]\n"

// TestProgramAccessOff: with programs off, every role's grants are
// today's exactly, and no director launches.
func TestProgramAccessOff(t *testing.T) {
	s, _ := programFixture(t)
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	off := map[string]any{}
	for _, role := range []string{proto.RoleCoordinator, proto.RoleThread} {
		a, err := s.accessFor(role, "demo", nil, loadLaunchConfig())
		if err != nil {
			t.Fatal(err)
		}
		off[role] = a
	}
	writeConfig(t, "")
	for _, role := range []string{proto.RoleCoordinator, proto.RoleThread} {
		a, err := s.accessFor(role, "demo", nil, loadLaunchConfig())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, off[role]) {
			t.Errorf("%s: off with a program %+v, without %+v", role, off[role], a)
		}
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	_, err := s.accessFor(proto.RoleDirector, "acme", nil, loadLaunchConfig())
	var pe *proto.Error
	if !errors.As(err, &pe) || pe.Code != config.ProgramsOff {
		t.Fatalf("director while off: %v", err)
	}
}

// TestProgramAccessRoles: threads get nothing in the programs folder, a
// coordinator reads its own program's folder and writes none, the
// director writes its own but for tm's files, and no project's folder
// and no other program's.
func TestProgramAccessRoles(t *testing.T) {
	s, root := programFixture(t)
	writeConfig(t, programsOn)
	lc := loadLaunchConfig()
	acme, beta := filepath.Join(root, "acme"), filepath.Join(root, "beta")
	projects := filepath.Dir(root)

	thr, err := s.accessFor(proto.RoleThread, "demo", nil, lc)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(thr.NoWrite, root) || slices.ContainsFunc(thr.Read, func(p string) bool { _, ok := fsx.Within(root, p); return ok }) {
		t.Errorf("thread: read %q, no-write %q", thr.Read, thr.NoWrite)
	}

	co, err := s.accessFor(proto.RoleCoordinator, "demo", nil, lc)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(co.Read, acme) || slices.Contains(co.Read, beta) || !slices.Contains(co.NoWrite, root) {
		t.Errorf("coordinator of demo: read %q, no-write %q", co.Read, co.NoWrite)
	}
	// web's coordinator reads beta, not acme; a project in no program reads none.
	if co, _ := s.accessFor(proto.RoleCoordinator, "web", nil, lc); !slices.Contains(co.Read, beta) || slices.Contains(co.Read, acme) {
		t.Errorf("coordinator of web: read %q", co.Read)
	}
	writeConfig(t, "[features]\nprograms = true\n")
	if co, _ := s.accessFor(proto.RoleCoordinator, "demo", nil, loadLaunchConfig()); slices.ContainsFunc(co.Read, func(p string) bool { _, ok := fsx.Within(root, p); return ok }) {
		t.Errorf("coordinator of no program: read %q", co.Read)
	}
	writeConfig(t, programsOn)

	dir, err := s.accessFor(proto.RoleDirector, "acme", nil, lc)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(dir.Read, []string{acme}) {
		t.Errorf("director read %q", dir.Read)
	}
	for _, d := range []string{filepath.Join(projects, "demo"), filepath.Join(projects, "web"), beta, filepath.Join(acme, "inbox"), filepath.Join(acme, "tasks")} {
		if !slices.Contains(dir.NoWrite, d) {
			t.Errorf("director may write %s: %q", d, dir.NoWrite)
		}
	}
	for _, f := range []string{"JOURNAL.md", "TASKS.md", "questions.json"} {
		if !slices.Contains(dir.NoWriteFiles, filepath.Join(acme, f)) {
			t.Errorf("director may write %s", f)
		}
	}
	for _, w := range []string{acme, root, projects, filepath.Join(acme, "CONTEXT.md"), filepath.Join(acme, "memory"), filepath.Join(acme, "PROGRAM.md")} {
		if slices.Contains(dir.NoWrite, w) || slices.Contains(dir.NoWriteFiles, w) {
			t.Errorf("director closed from %s", w)
		}
	}
	if !slices.Contains(dir.NoWriteFiles, filepath.Join(realPath(os.Getenv("TERMINATR_HOME")), "config.toml")) {
		t.Error("director may write config.toml")
	}
}

// TestProgramGrantsNotProjects: no project grant reaches into
// _programs/: a project's folder is projects/<slug>, and _ is not a slug
// character, so neither a coordinator's nor a thread's write grants nor
// what they may write name a program path.
func TestProgramGrantsNotProjects(t *testing.T) {
	s, root := programFixture(t)
	writeConfig(t, programsOn)
	acme := filepath.Join(root, "acme", "CONTEXT.md")
	for _, role := range []string{proto.RoleCoordinator, proto.RoleThread} {
		a, err := s.accessFor(role, "demo", nil, loadLaunchConfig())
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range append(slices.Clone(a.Write), a.WriteFiles...) {
			if _, ok := fsx.Within(w, acme); ok {
				t.Errorf("%s: write grant %s holds %s", role, w, acme)
			}
		}
	}
	if project.ValidSlug(config.ProgramsDirName) {
		t.Fatal("_programs is a project slug")
	}
	if _, err := project.Dir(config.ProgramsDirName); err == nil {
		t.Fatal("project.Dir(_programs)")
	}
}

// TestProgramGuardFloor: the guard's floor holds program paths: a
// thread writes nowhere in the programs folder (nor removes it, by glob
// or whole), a coordinator writes no program, and a director's floor
// has the project folders, the other programs and tm's files in its own.
func TestProgramGuardFloor(t *testing.T) {
	s, root := programFixture(t)
	writeConfig(t, programsOn)
	projects := filepath.Dir(root)
	tm := filepath.Dir(projects)
	lc := loadLaunchConfig()
	judge := func(role, slug, cwd, cmd string) *guard.Denial {
		r := guard.Rules{On: true, Role: role, Cwd: cwd, Writable: []string{cwd}, NoWrite: s.guardNoWrite(SessionRecord{Role: role, Project: slug, Cwd: cwd}, lc),
			Tools: map[string]guard.Tool{"Bash": {Kind: guard.KindShell, Fields: []string{"command"}}, "Write": {Kind: guard.KindWrite, Fields: []string{"file_path"}}}}
		return r.Judge("Bash", map[string]any{"command": cmd})
	}
	refused := func(d *guard.Denial) bool { return d != nil && d.Rule == guard.RuleTMFiles }
	wt := t.TempDir()
	for _, cmd := range []string{
		"rm -rf " + filepath.Join(root, "acme"),
		"rm -rf " + root,
		"rm -rf " + tm + "/proj*",
		"echo x > " + filepath.Join(root, "acme", "CONTEXT.md"),
		"cp a " + filepath.Join(root, "beta", "memory", "x.md"),
	} {
		if d := judge(proto.RoleThread, "demo", wt, cmd); !refused(d) {
			t.Errorf("thread: %s: %+v", cmd, d)
		}
		if d := judge(proto.RoleCoordinator, "demo", filepath.Join(projects, "demo"), cmd); !refused(d) {
			t.Errorf("coordinator: %s: %+v", cmd, d)
		}
	}
	// The coordinator still writes its own folder's CONTEXT.md.
	if d := judge(proto.RoleCoordinator, "demo", filepath.Join(projects, "demo"), "echo x > "+filepath.Join(projects, "demo", "CONTEXT.md")); d != nil {
		t.Errorf("coordinator's own CONTEXT.md: %+v", d)
	}
	acme := filepath.Join(root, "acme")
	for _, cmd := range []string{
		"echo x > " + filepath.Join(projects, "demo", "TASKS.md"),
		"rm -rf " + filepath.Join(projects, "web"),
		"echo x > " + filepath.Join(root, "beta", "CONTEXT.md"),
		"echo x >> " + filepath.Join(acme, "JOURNAL.md"),
		"rm " + filepath.Join(acme, "TASKS.md"),
		"rm -rf " + tm + "/proj*",
	} {
		if d := judge(proto.RoleDirector, "acme", acme, cmd); !refused(d) {
			t.Errorf("director: %s: %+v", cmd, d)
		}
	}
	for _, cmd := range []string{
		"echo x > " + filepath.Join(acme, "CONTEXT.md"),
		"echo x > " + filepath.Join(acme, "memory", "sso.md"),
	} {
		if d := judge(proto.RoleDirector, "acme", acme, cmd); d != nil {
			t.Errorf("director: %s: %+v", cmd, d)
		}
	}
	// Off: the floor is today's: nothing about programs in it.
	writeConfig(t, "")
	for _, role := range []string{proto.RoleThread, proto.RoleCoordinator} {
		for _, p := range s.guardNoWrite(SessionRecord{Role: role, Project: "demo", Cwd: wt}, loadLaunchConfig()) {
			if _, ok := fsx.Within(root, p); ok {
				t.Errorf("%s: off, floor has %s", role, p)
			}
		}
	}
}
