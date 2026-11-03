package server

import (
	"os"
	"path/filepath"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Program folders and the roles (the programs design §3.4, "Guard and
// launch rules"): the programs live inside the projects folder, at
// projects/_programs/<slug>/, so the guard needs no new root. What each
// role may do there:
//
//   - a thread: nothing; no grant, and the whole programs folder is in
//     its floor;
//   - a coordinator: read its own program's folder (PROGRAM.md, the
//     program's memory), when its project is a member; write none;
//   - a director: write its own program's folder (its cwd), but for
//     what tm alone writes there (program.Program.Owned); write no
//     project's folder and no other program's.
//
// All of it only while [features] programs is on: off, every role's
// grants are today's exactly (§3.5).

// programAccess is what role's session of slug (a project's, or a
// program's for the director) gets for the program folders under cfg:
// folders it reads, and folders and files it never writes. Each path
// has its symlinks resolved.
func programAccess(role, slug string, cfg *config.Config) (read, noWrite, noWriteFiles []string, err error) {
	if !features.Programs(cfg) || slug == "" {
		return nil, nil, nil, nil
	}
	root, err := home.ProgramsDir()
	if err != nil {
		return nil, nil, nil, err
	}
	root = realPath(root)
	switch role {
	case proto.RoleThread:
		return nil, []string{root}, nil, nil
	case proto.RoleCoordinator:
		if g := cfg.ProgramOf(slug); g != "" && config.ValidProgramSlug(g) && !project.ProgramLeftOut(g) {
			read = []string{filepath.Join(root, g)}
		}
		return read, []string{root}, nil, nil
	case proto.RoleDirector:
		noWrite, noWriteFiles, err = programOwned(slug)
		if err != nil {
			return nil, nil, nil, err
		}
		noWrite = append(noWrite, otherFolders(root, slug)...)
		if projects, err := home.ProjectsDir(); err == nil {
			noWrite = append(noWrite, otherFolders(realPath(projects), "")...)
		}
		return []string{filepath.Join(root, slug)}, noWrite, noWriteFiles, nil
	}
	return nil, nil, nil, nil
}

// programOwned is what in program slug's folder only tm writes
// (program.Program.Owned), symlinks resolved.
func programOwned(slug string) (dirs, files []string, err error) {
	dir, err := program.Dir(slug)
	if err != nil {
		return nil, nil, err
	}
	p := &program.Program{Folder: project.Folder{Slug: slug, Dir: realPath(dir), Kind: project.KindProgram}}
	dirs, files = p.Owned()
	return dirs, files, nil
}

// otherFolders are the folders in root named by a slug, but for own:
// the projects in the projects folder, or the programs but one in the
// programs folder (_programs, not a slug, is never listed).
func otherFolders(root, own string) []string {
	ents, _ := os.ReadDir(root)
	var out []string
	for _, e := range ents {
		if e.IsDir() && project.ValidSlug(e.Name()) && e.Name() != own {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out
}
