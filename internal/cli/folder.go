package cli

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
)

// openFolder opens the folder tm inbox and tm memory act on: a
// project's, as openProject, or a program's while programs are on (the
// programs design §3.1: a program has the inbox and memory a project
// has). A director gets its own program's, and never a child's: it
// reads a child through tm context --project. The human gets a program
// that --project names (no project has a program's slug, §3.4) or whose
// folder the working directory is in.
func (e *Env) openFolder(flag string) (*project.Folder, error) {
	if e.Caller.Kind == caller.Director {
		if flag != "" && flag != e.Caller.Project {
			return nil, &project.Error{Code: "other-program", Msg: "a director's inbox and memory are its own program's; read a child project with tm context --project <slug>"}
		}
		return openProgramFolder(e.Caller.Project)
	}
	if flag == "" && !e.Caller.IsAgent() && e.Getenv(caller.EnvProject) == "" {
		if slug := programAt(e.Cwd); slug != "" {
			return openProgramFolder(slug)
		}
	}
	p, err := e.openProject(flag)
	if err == nil {
		return &p.Folder, nil
	}
	var pe *project.Error
	if flag != "" && !e.Caller.IsAgent() && errors.As(err, &pe) && pe.Code == "unknown-project" && config.ValidProgramSlug(flag) {
		if g, gerr := openProgramFolder(flag); gerr == nil {
			return g, nil
		}
	}
	return nil, err
}

// openProgramFolder is program slug's folder, refused programs-off
// while programs are off.
func openProgramFolder(slug string) (*project.Folder, error) {
	cfg, _ := config.Load() // a safety error still gives the tables
	if !features.Programs(cfg) {
		return nil, programsOff()
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return nil, err
	}
	return &g.Folder, nil
}

// programAt is the program whose folder cwd is in, "" for none.
func programAt(cwd string) string {
	if cwd == "" {
		return ""
	}
	root, err := home.ProgramsDir()
	if err != nil {
		return ""
	}
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	rel, ok := fsx.Within(real(root), real(cwd))
	if !ok || rel == "." {
		return ""
	}
	slug, _, _ := strings.Cut(rel, string(filepath.Separator))
	if !config.ValidProgramSlug(slug) {
		return ""
	}
	return slug
}

// folderWriter refuses what changes folder f to a caller other than
// the human and the folder's own coordinator or director.
func (e *Env) folderWriter(f *project.Folder, what string) error {
	if e.Caller.Kind == caller.Thread {
		return &project.Error{Code: "coordinator-only", Msg: what + " is the coordinator's"}
	}
	if !e.Caller.IsAgent() {
		return nil
	}
	if e.Caller.Project != "" && (e.Caller.Project != f.Slug || (e.Caller.Kind == caller.Director) != f.IsProgram()) {
		return &project.Error{Code: "other-project", Msg: "an agent changes only its own project's or program's folder"}
	}
	return nil
}
