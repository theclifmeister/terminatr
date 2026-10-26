package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// CoordinatorKickoff is the coordinator's first prompt. Its context (the
// role rules and tm context) arrives through the agent's session-start
// hook (docs/SPEC.md §7.8); this only asks it to speak first.
const CoordinatorKickoff = "You are this project's coordinator. Greet the user: say in a few lines where the project stands, from your context, then ask what to do next."

// InactiveError is opening an inactive project's coordinator: opening
// never activates (docs/SPEC.md §5.1).
func InactiveError(slug string) error {
	return &project.Error{Code: "project-inactive", Msg: slug + " is inactive: activate it first (S on it in the sidebar, or tm project activate " + slug + ")"}
}

// OpenCoordinator returns the id of the project's coordinator session,
// starting one (agent, role coordinator, cwd the project folder) if none
// runs, and whether it started one: tm project open, the dashboard's
// project rows and project.open. list lists the sessions and start
// starts one. An empty agentName is the project's coordinator_agent
// setting (§11.2).
func OpenCoordinator(list func() ([]proto.SessionInfo, error), start func(proto.SessionStartParams) (string, error),
	slug, agentName string, cols, rows uint16) (string, bool, error) {
	p, err := project.Open(slug)
	if err != nil {
		return "", false, err
	}
	sessions, err := list()
	if err != nil {
		return "", false, err
	}
	for _, s := range sessions {
		if s.Role == proto.RoleCoordinator && s.Project == slug {
			return s.ID, false, nil
		}
	}
	// The project's agent, model and remote control settings; a broken
	// config.toml shows in the settings popup, it doesn't keep the
	// coordinator from starting.
	safety := config.Defaults
	cfg, err := config.Load()
	if err == nil {
		safety, _ = cfg.Safety(slug)
	}
	if !safety.Active {
		return "", false, InactiveError(slug)
	}
	name, model, err := coordinatorAgent(cfg, agentName, safety)
	if err != nil {
		return "", false, err
	}
	id, err := start(proto.SessionStartParams{
		Agent: name, Model: model, Role: proto.RoleCoordinator, Project: slug, Cwd: p.Dir,
		Cols: cols, Rows: rows, Kickoff: CoordinatorKickoff,
		RemoteControl: safety.CoordinatorRemoteControl})
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// coordinatorAgent is the agent and model a new coordinator runs: the
// agent named (tm project open --agent), else the project's
// coordinator_agent, else the one installed agent (docs/SPEC.md §8.2,
// §11.2); and the project's coordinator_model while that agent offers
// it in the project's scope, else none (the agent's own default, or the
// user's default_model, which the server passes). An agent that isn't
// installed, none, or several with none chosen are refused
// (models.Error: the dashboard asks for CodeNotChosen).
func coordinatorAgent(cfg *config.Config, flag string, safety config.Safety) (name, model string, err error) {
	reg := models.Registry()
	if reg == nil {
		return "", "", errors.New("no home directory for the agent manifests")
	}
	installed := models.Installed(reg, nil)
	setting := cmp.Or(flag, safety.CoordinatorAgent)
	name, _, err = models.Resolve(reg, installed, setting, "coordinator agent")
	if err != nil {
		return "", "", err
	}
	if safety.CoordinatorModel != "" {
		// An agent never asked (the server just started) is asked first.
		models.Ensure(context.Background(), reg, name, nil)
		if c, ok := models.CatalogOf(reg, cfg, name, installed); ok {
			model = c.CoordinatorModel(safety)
		}
	}
	return name, model, nil
}

// projectOpen is project.open's: OpenCoordinator for the app, which
// can't start a session itself. One open at a time, so two calls don't
// start two coordinators; a start is journaled as the caller.
func (s *Server) projectOpen(p proto.ProjectOpenParams, cc callCtx) (any, *proto.Error) {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	list := func() ([]proto.SessionInfo, error) { return s.list().Sessions, nil }
	start := func(sp proto.SessionStartParams) (string, error) {
		res, perr := s.startSession(sp)
		if perr != nil {
			return "", perr
		}
		started, ok := res.(proto.SessionStartResult)
		if !ok {
			return "", fmt.Errorf("unexpected result %T", res)
		}
		return started.Session.ID, nil
	}
	id, started, err := OpenCoordinator(list, start, p.Project, "", p.Cols, p.Rows)
	if err != nil {
		return nil, openError(err)
	}
	if started {
		if pr, err := project.Open(p.Project); err == nil {
			if err := pr.Journal(cc.who(), "coordinator.start", id, ""); err != nil {
				s.log.Printf("project.open %s: journal: %v", p.Project, err)
			}
		}
		s.watch.wake()
	}
	return proto.ProjectOpenResult{Session: id, Started: started}, nil
}

// openError is err as project.open answers it: a refusal keeps its code
// (project-inactive, an unknown project, the models' codes, the
// server's own), anything else is internal.
func openError(err error) *proto.Error {
	var perr *proto.Error
	var merr *models.Error
	var terr *tasks.Error // project.Error
	switch {
	case errors.As(err, &perr):
		return perr
	case errors.As(err, &merr):
		return proto.Errorf(merr.Code, "%s", merr.Msg)
	case errors.As(err, &terr):
		return proto.Errorf(terr.Code, "%s", terr.Msg)
	}
	return proto.Errorf(proto.ErrInternal, "%v", err)
}
