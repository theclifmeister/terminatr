// Package seat is a folder's one standing agent session: a project's
// coordinator or a program's director (the programs design §4.1). The
// two run the same way in different folders, so what only tells them
// apart to find the folder, its settings or its session goes here.
//
// A seat says where and which, never what may be done: rights stay
// explicit at each check (caller.IsAgent, Caller.DirectorOf, the task
// store's refusals, the guard's floors, the server's scopes).
package seat

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

// Seat is a folder and the role of its agent.
type Seat struct {
	*project.Folder
	Role string // proto.RoleCoordinator or proto.RoleDirector
	// Project is the project, nil for a program (no threads, no repos).
	Project *project.Project
	program *program.Program // nil for a project
}

// Coordinator is project p's seat.
func Coordinator(p *project.Project) Seat {
	return Seat{Folder: &p.Folder, Role: proto.RoleCoordinator, Project: p}
}

// Director is program g's seat.
func Director(g *program.Program) Seat {
	return Seat{Folder: &g.Folder, Role: proto.RoleDirector, program: g}
}

// Open is the seat of the folder a session of role in slug runs in: a
// director's program, any other session's project, so a thread gets its
// coordinator's seat. cfg is for a program, nil reads config.toml.
func Open(role, slug string, cfg *config.Config) (Seat, error) {
	if role == proto.RoleDirector {
		g, err := program.Open(slug, cfg)
		if err != nil {
			return Seat{}, err
		}
		return Director(g), nil
	}
	p, err := project.Open(slug)
	if err != nil {
		return Seat{}, err
	}
	return Coordinator(p), nil
}

// Is reports whether role is a seat's.
func Is(role string) bool {
	return role == proto.RoleCoordinator || role == proto.RoleDirector
}

// Find is the session of role in slug among sessions, nil for none.
func Find(sessions []proto.SessionInfo, role, slug string) *proto.SessionInfo {
	for i, s := range sessions {
		if s.Role == role && s.Project == slug {
			return &sessions[i]
		}
	}
	return nil
}

// Dir is the folder of a session of role in slug: a director's program's
// (its Project is the program's slug), any other session's project's.
func Dir(role, slug string) (string, error) {
	if role == proto.RoleDirector {
		return program.Dir(slug)
	}
	return project.Dir(slug)
}

// Settings are the settings a seat of role in slug runs with: a
// director's are read from its program (config.DirectorSafety), a
// coordinator's from its project.
func Settings(cfg *config.Config, role, slug string) (config.Safety, error) {
	if role == proto.RoleDirector {
		return cfg.DirectorSafety(slug), nil
	}
	return cfg.Safety(slug)
}

// Key is the ticker's memo and lock key of a seat of role in folder
// slug: the project's slug, or a program's _programs/<slug>, which no
// project slug can be.
func Key(role, slug string) string {
	if role == proto.RoleDirector {
		return vaultlink.ProgramFolder(slug)
	}
	return slug
}

// FolderWord is what a seat of role sits on, for messages: "program" or
// "project".
func FolderWord(role string) string {
	if role == proto.RoleDirector {
		return "program"
	}
	return "project"
}

// Tasks is the seat's board: its project's, or its program's.
func (s Seat) Tasks() *tasks.Store {
	if s.program != nil {
		return s.program.Tasks()
	}
	return s.Project.Tasks()
}

// Key is the seat's memo and lock key.
func (s Seat) Key() string { return Key(s.Role, s.Slug) }

// Of is the seat's session among sessions, nil for none.
func (s Seat) Of(sessions []proto.SessionInfo) *proto.SessionInfo {
	return Find(sessions, s.Role, s.Slug)
}

// Caller is the seat's agent as a caller, for what it journals.
func (s Seat) Caller() caller.Caller {
	if s.Role == proto.RoleDirector {
		return caller.Caller{Kind: caller.Director, Project: s.Slug}
	}
	return caller.Caller{Kind: caller.Coordinator, Project: s.Slug}
}
