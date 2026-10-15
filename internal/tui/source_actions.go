package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/models"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// ServerSource's ProjectSource and SessionSource: opening projects,
// asking their coordinators, acting on sessions.

func (s *ServerSource) NewProject(slug string) (string, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return "", errors.New("human-only: only the human creates projects")
	}
	p, err := project.New(project.Options{Slug: slug})
	if err != nil {
		return "", err
	}
	return p.Slug, nil
}

// coordinatorKickoff is the coordinator's first prompt. Its context (the
// role rules and tm context) arrives through the agent's session-start
// hook (docs/SPEC.md §7.8); this only asks it to speak first.
const coordinatorKickoff = "You are this project's coordinator. Greet the user: say in a few lines where the project stands, from your context, then ask what to do next."

func (s *ServerSource) OpenProject(slug string, cols, rows int) (string, error) {
	return OpenCoordinator(s.call, slug, "", cols, rows)
}

// InactiveError is opening an inactive project's coordinator: opening
// never activates (docs/SPEC.md §5.1).
func InactiveError(slug string) error {
	return &project.Error{Code: "project-inactive", Msg: slug + " is inactive: activate it first (space on it in the sidebar, or tm project activate " + slug + ")"}
}

// OpenCoordinator returns the id of the project's coordinator session,
// starting one (agent, role coordinator, cwd the project folder) if none
// runs: `tm project open` and the dashboard's project rows. An empty
// agentName is the project's coordinator_agent setting (§11.2).
func OpenCoordinator(call func(method string, params, result any) error, slug, agentName string, cols, rows int) (string, error) {
	p, err := project.Open(slug)
	if err != nil {
		return "", err
	}
	var res proto.SessionListResult
	if err := call(proto.MethodSessionList, nil, &res); err != nil {
		return "", err
	}
	for _, s := range res.Sessions {
		if s.Role == proto.RoleCoordinator && s.Project == slug {
			return s.ID, nil
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
		return "", InactiveError(slug)
	}
	name, model, err := coordinatorAgent(cfg, agentName, safety)
	if err != nil {
		return "", err
	}
	var started proto.SessionStartResult
	err = call(proto.MethodSessionStart, proto.SessionStartParams{
		Agent: name, Model: model, Role: proto.RoleCoordinator, Project: slug, Cwd: p.Dir,
		Cols: uint16(cols), Rows: uint16(rows), Kickoff: coordinatorKickoff,
		RemoteControl: safety.CoordinatorRemoteControl}, &started)
	if err != nil {
		return "", err
	}
	return started.Session.ID, nil
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

func (s *ServerSource) Lifecycle(slug, verb string) (string, error) {
	defer s.touch()
	switch verb {
	case "activate", "deactivate":
		return ActivateProject(s.call, s.Caller, slug, verb == "activate")
	case "pause", "resume":
		return PauseProject(s.Caller, slug, verb == "pause")
	case "pin", "unpin":
		return PinProject(s.Caller, slug, verb == "pin")
	case "archive":
		return ArchiveProject(s.call, s.Caller, slug, true)
	case "delete":
		return DeleteProject(s.call, s.Caller, slug)
	}
	return "", fmt.Errorf("unknown project action %q", verb)
}

func (s *ServerSource) Templates(slug string) []mod.Template {
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	reg, err := mod.Load(cfg)
	if err != nil {
		return nil
	}
	ts, _ := reg.Templates(slug) // tm doctor and the Mods tab say which don't load
	return ts
}

func (s *ServerSource) AddTask(slug, title string, tpl *mod.Template) (string, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return "", err
	}
	it := tasks.NewTask{Title: title}
	if tpl != nil {
		it.Title, it.Notes, it.Steps = tpl.Apply(title)
	}
	res, err := p.Tasks().Add(s.Caller, []tasks.NewTask{it})
	switch {
	case err != nil:
		return "", err
	case len(res) != 1:
		return "", errors.New("the task wasn't added")
	case res[0].Result == "failed":
		return "", errors.New(res[0].Error)
	case res[0].Result == "existing":
		return res[0].ID, fmt.Errorf("%s %s is there already", res[0].ID, oneLine(res[0].Title))
	}
	return res[0].ID, nil
}

func (s *ServerSource) Ask(slug string, id int, kind, note string) (bool, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return false, errors.New("human-only: only the human asks the coordinator about a task")
	}
	p, err := project.Open(slug)
	if err != nil {
		return false, err
	}
	t, err := p.Tasks().Get(id)
	if err != nil {
		return false, err
	}
	if why := notAskable(t, kind); why != "" {
		return false, errors.New(why)
	}
	switch kind {
	case project.KindAccept:
		return p.AskAccept(s.Caller, t.Ref())
	case project.KindSendBack:
		return p.AskSendBack(s.Caller, t.Ref(), note)
	}
	return p.AskDelegate(s.Caller, t.Ref())
}

func (s *ServerSource) AskAdopt(slug string, sess proto.SessionInfo) (bool, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return false, errors.New("human-only: only the human asks the coordinator to adopt a session")
	}
	p, err := project.Open(slug)
	if err != nil {
		return false, err
	}
	// The go-ahead the coordinator's tm thread adopt needs is the
	// server's, given only on the human socket; the inbox item, which an
	// agent could write too, only tells the coordinator.
	if err := s.call(proto.MethodSessionAdoptAsk, proto.SessionAdoptAskParams{ID: sess.ID, Project: slug}, nil); err != nil {
		return false, err
	}
	return p.AskAdopt(s.Caller, sess.ID, adoptWhere(sess, ""))
}

// WithdrawAdopt takes back the user's go-ahead to adopt session id into
// project slug: the server's, which the coordinator's tm thread adopt
// needs, and the inbox item that told it.
func (s *ServerSource) WithdrawAdopt(slug, id string) error {
	defer s.touch()
	if s.Caller.IsAgent() {
		return errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return err
	}
	if err := s.call(proto.MethodSessionAdoptAsk, proto.SessionAdoptAskParams{ID: id, Project: slug, Withdraw: true}, nil); err != nil {
		return err
	}
	if err := p.DoneItems(project.KindAdopt, id); err != nil {
		return err
	}
	return p.Journal(s.Caller, "thread.adopt.withdraw", id, "")
}

// adoptWhere says what runs in session s and where, for an adopt ask:
// "claude in /src/app on fix-login"; home, when set, is shortened to ~.
func adoptWhere(s proto.SessionInfo, home string) string {
	out := s.Agent + " in " + shortHome(s.Cwd, home)
	if pl, ok := worktree.Locate(s.Cwd); ok && pl.Branch != "" {
		out += " on " + pl.Branch
	}
	return out
}

func (s *ServerSource) OpenQuestions(slug string) (proto.QuestionsOpenResult, error) {
	var res proto.QuestionsOpenResult
	err := s.call(proto.MethodQuestionsOpen, proto.QuestionsOpenParams{Project: slug}, &res)
	return res, err
}

func (s *ServerSource) Review(slug string, t *tasks.Task) Review {
	var rv Review
	p, err := project.Open(slug)
	if err != nil {
		return rv
	}
	var rec *thread.Record
	if thread.ValidID(t.Thread) {
		rec, _ = thread.Load(p, t.Thread)
	}
	reportPR, repo := "", ""
	if rec != nil {
		repo = rec.Repo
		if rep, _ := thread.ReadReport(p, rec.ID); rep != nil {
			rv.Check, reportPR = rep.Check, rep.PR
		}
	}
	if repo == "" && len(p.Repos) > 0 {
		repo = p.Repos[0]
	}
	pr := ticker.PRs(ticker.StatePath(s.Paths.Sessions), slug)[t.Thread]
	rv.PR = pr.Number
	if rv.PR == 0 {
		rv.PR = ticker.PRNumber(reportPR)
	}
	rv.Ship = shipped(repo, rv.PR, pr)
	return rv
}

// shipped is where pull request n stands (Review.Ship), from repo's
// history as last fetched, else the ticker's last look at it (pr).
func shipped(repo string, n int, pr ticker.PR) string {
	switch {
	case n == 0:
		return ""
	case pr.State == "MERGED" || pr.Merge != "" || repo != "" && worktree.MergeCommit(repo, n) != "":
		return ShipMerged
	case pr.State == "OPEN":
		return ShipOpen
	case pr.State == "CLOSED":
		return ShipClosed
	}
	return ""
}

func (s *ServerSource) SetRemote(slug string, on bool) (string, error) {
	res, err := SetRemote(s.call, slug, on)
	if err != nil {
		return "", err
	}
	return RemoteMessage(slug, res), nil
}

func (s *ServerSource) ClearSession(id string) error {
	return s.call(proto.MethodSessionClear, proto.SessionIDParams{ID: id}, nil)
}

func (s *ServerSource) AnswerQuestion(id string, index int, question, option string) error {
	return s.call(proto.MethodSessionAnswer, proto.SessionAnswerParams{ID: id, Index: index, Answer: option, Question: question}, nil)
}
