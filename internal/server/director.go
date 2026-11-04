package server

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/seat"
)

// A program's director (the programs design §4.1): a session of role
// director, Project its program's slug, cwd the program's folder. It
// launches with the agent's coordinator profile (directorProfile), so no
// agent manifest changes; only tm's role differs: its rules (tm skill
// director), its context (tm program context), its grants
// (programAccess) and its caller kind (caller.Director). None runs while
// programs are off.

// DirectorKickoff is the director's first prompt; its context arrives
// through the session-start hook, as a coordinator's does.
const DirectorKickoff = "You are this program's director. Greet the user: say in a few lines where the program and its projects stand, from your context, then ask what to do next."

// directorProfile is the agent role a session of tm role is launched
// with: a director runs as a coordinator does (the same hooks,
// permission shape and MCP tools).
func directorProfile(role string) string {
	if role == proto.RoleDirector {
		return proto.RoleCoordinator
	}
	return role
}

// directorReady refuses a director for program slug under cfg: programs
// off (programs-off), no such program (unknown-program), one whose name
// a project has too (program-left-out: the project wins, §3.4). It
// returns the program's folder.
func directorReady(cfg *config.Config, slug string) (string, *proto.Error) {
	if !features.Programs(cfg) {
		return "", proto.Errorf(features.OffCode, "%s", features.OffMsg)
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return "", openError(err)
	}
	if g.LeftOut {
		return "", proto.Errorf("program-left-out", "a project is called %s too: the program is left out until one is renamed (tm doctor)", slug)
	}
	return realPath(g.Dir), nil
}

// OpenDirector returns the id of program slug's director session,
// starting one (agent, role director, cwd the program's folder) if none
// runs, and whether it started one: tm program open. list lists the
// sessions and start starts one. An empty agentName is the program's
// director_agent, else the coordinator agent.
func OpenDirector(list func() ([]proto.SessionInfo, error), start func(proto.SessionStartParams) (string, error),
	slug, agentName string, cols, rows uint16) (string, bool, error) {
	cfg, err := config.Load()
	if err != nil && cfg == nil {
		return "", false, err
	}
	dir, perr := directorReady(cfg, slug)
	if perr != nil {
		return "", false, perr
	}
	sessions, err := list()
	if err != nil {
		return "", false, err
	}
	if s := seat.Find(sessions, proto.RoleDirector, slug); s != nil {
		return s.ID, false, nil
	}
	safety := cfg.DirectorSafety(slug)
	name, model, err := coordinatorAgent(cfg, agentName, safety)
	if err != nil {
		return "", false, err
	}
	id, err := start(proto.SessionStartParams{
		Agent: name, Model: model, Role: proto.RoleDirector, Project: slug, Cwd: dir,
		Cols: cols, Rows: rows, Kickoff: DirectorKickoff, RemoteControl: safety.CoordinatorRemoteControl})
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// directorStart checks session.start's params for a director: an agent,
// a program that may run one, and its folder as the cwd (the one given,
// if any, must be it). It returns the cwd.
func directorStart(p proto.SessionStartParams) (string, *proto.Error) {
	if p.Agent == "" || p.Project == "" {
		return "", proto.Errorf(proto.ErrBadParams, "role director needs an agent and a program")
	}
	cfg, _ := config.Load() // a safety error still gives the tables
	dir, perr := directorReady(cfg, p.Project)
	if perr != nil {
		return "", perr
	}
	if p.Cwd != "" && realPath(p.Cwd) != dir {
		return "", proto.Errorf(proto.ErrBadParams, "a director runs in its program's folder (%s), not %s", dir, p.Cwd)
	}
	return dir, nil
}

// directorLabel names a director session in lists and items:
// "director <program>".
func directorLabel(slug string) string {
	return strings.TrimSpace(fmt.Sprintf("director %s", slug))
}

// directorModel is the model a director of program slug launches with
// when its start named none (a resume, a relaunch): the program's
// director_model while the agent offers it, else the coordinator's.
func directorModel(a agent.Agent, slug string, cfg *config.Config) string {
	m := agent.ManifestOf(a)
	if m == nil {
		return ""
	}
	return models.Get(m, cfg, true).CoordinatorModel(cfg.DirectorSafety(slug))
}

// directorContext is a director's session-start block (contextFor): its
// rules, or with them in its system prompt the header saying so, then
// tm program context within the start-up budget. With programs off, or
// its program gone, only the rules and a line saying why. needs reads
// the program's rolled-up Needs you, placed after the Program part
// (nil leaves it out).
func directorContext(r SessionRecord, needs NeedsFunc) ([]byte, error) {
	rules, _ := roleRules("director", r.Project)
	head := rules
	if r.Prompt != nil {
		head = strings.TrimRight(promptHeader(r.Role, r.Prompt, rules, ""), "\n")
	}
	cfg, _ := config.Load()
	if !features.Programs(cfg) {
		return []byte(head + "\n\nPrograms are off on this server: the user turned them off, and this session is to stop."), nil
	}
	g, err := program.Open(r.Project, cfg)
	if err != nil {
		return []byte(head + "\n\nProgram " + r.Project + " can't be read: " + err.Error()), nil
	}
	secs, err := g.Context(cfg)
	if err != nil {
		return []byte(head + "\n\nProgram " + r.Project + "'s context can't be read: " + err.Error()), nil
	}
	if needs != nil {
		secs = program.WithNeeds(secs, program.NeedsSection(needs(r.Project)))
	}
	if r.Prompt != nil {
		return []byte(program.Startup(head, secs)), nil
	}
	return []byte(head + "\n\n" + program.Startup("", secs)), nil
}
