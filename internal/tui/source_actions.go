package tui

import (
	"errors"
	"fmt"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
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

func (s *ServerSource) OpenProject(slug string, cols, rows int) (string, error) {
	return OpenCoordinator(s.call, slug, "", cols, rows)
}

// InactiveError is opening an inactive project's coordinator: opening
// never activates (docs/SPEC.md §5.1).
func InactiveError(slug string) error { return server.InactiveError(slug) }

// OpenCoordinator returns the id of the project's coordinator session,
// starting one if none runs (server.OpenCoordinator, which project.open
// shares): `tm project open` and the dashboard's project rows. An empty
// agentName is the project's coordinator_agent setting (§11.2).
func OpenCoordinator(call func(method string, params, result any) error, slug, agentName string, cols, rows int) (string, error) {
	list := func() ([]proto.SessionInfo, error) {
		var res proto.SessionListResult
		err := call(proto.MethodSessionList, nil, &res)
		return res.Sessions, err
	}
	start := func(p proto.SessionStartParams) (string, error) {
		var started proto.SessionStartResult
		if err := call(proto.MethodSessionStart, p, &started); err != nil {
			return "", err
		}
		return started.Session.ID, nil
	}
	id, _, err := server.OpenCoordinator(list, start, slug, agentName, uint16(cols), uint16(rows))
	return id, err
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

func (s *ServerSource) SetPriority(slug string, id int, prio tasks.Priority) (bool, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return false, errHumanOnly
	}
	p, err := project.Open(slug)
	if err != nil {
		return false, err
	}
	res, err := p.Tasks().SetPriority(s.Caller, id, string(prio))
	return res.Changed, err
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
	return server.AdoptWhere(s, shortHome(s.Cwd, home))
}

func (s *ServerSource) OpenQuestions(slug string) (proto.QuestionsOpenResult, error) {
	var res proto.QuestionsOpenResult
	err := s.call(proto.MethodQuestionsOpen, proto.QuestionsOpenParams{Project: slug}, &res)
	return res, err
}

func (s *ServerSource) SaveClear(slug string) (proto.CoordinatorClearResult, error) {
	var res proto.CoordinatorClearResult
	err := s.call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: slug}, &res)
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
