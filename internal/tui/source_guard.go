package tui

import (
	"errors"
	"fmt"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// ServerSource's guard: the refusals in the guard's log, and the user's
// decisions on them (docs/SPEC.md §8.6, Guard). The log is the human's:
// read here, in the console's own process, never through the server.

// How the user decides on a refusal.
const (
	guardAllowHere = "here" // allowed for its project
	guardAllowAll  = "all"  // allowed for all projects
	guardKeep      = "keep" // kept refusing: no longer listed
)

func (s *ServerSource) GuardRefusals(slug string) ([]guardlog.Entry, error) {
	if s.Caller.IsAgent() {
		return nil, errHumanOnly
	}
	return guardlog.List(s.Paths.Home, slug)
}

func (s *ServerSource) GuardDecide(e guardlog.Entry, how, session string) (string, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	return guardDecide(s.Caller, s.Paths.Home, e, how, func(text string) {
		if session != "" {
			var res proto.SessionPromptResult
			s.call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: session, Text: text}, &res)
		}
	})
}

// guardDecide records the user's decision how on refusal e in the
// guard's log under home: kept refusing, or allowed (an allowance in the
// project's settings or all projects'), journaled, and then the session
// told it may retry (tell). It returns what to say in the footer.
func guardDecide(who caller.Caller, home string, e guardlog.Entry, how string, tell func(string)) (string, error) {
	journal := func(kind, detail string) {
		if p, err := project.Open(e.Project); err == nil {
			p.Journal(who, kind, e.Label(), detail)
		}
	}
	what := fmt.Sprintf("%s %s: %s", e.Rule, e.Tool, e.Summary)
	if how == guardKeep {
		if _, err := guardlog.Decide(home, e.ID, guardlog.Kept); err != nil {
			return "", err
		}
		journal("guard.keep", what)
		return "the guard keeps refusing it; it is no longer listed", nil
	}
	if e.Floor {
		return "", errors.New("the guard never allows this one: keep refusing it")
	}
	// The call the user saw, exactly: a * in it is the call's own, not a
	// wildcard.
	a := config.GuardAllow{Rule: e.Rule, Exact: true}
	if e.Path {
		a.Path = e.Subject
	} else {
		a.Command = e.Subject
	}
	if err := config.CheckGuardAllow(a); err != nil {
		return "", err
	}
	cfg, err := config.Load()
	if err != nil {
		return "", settingsErr(err)
	}
	scope, write := "this project", func(list []config.GuardAllow) error { return config.SetProject(e.Project, "guard_allow", list) }
	var list []config.GuardAllow
	if how == guardAllowAll {
		scope, write = config.AllProjectsName, func(list []config.GuardAllow) error { return config.SetDefaults("guard_allow", list) }
		all, _ := cfg.AllProjects()
		list = all.GuardAllow
	} else {
		own, _ := cfg.Safety(e.Project)
		list = config.OwnAllows(own.GuardAllow)
	}
	if !slices.ContainsFunc(list, a.Same) {
		if err := write(append(slices.Clip(list), a)); err != nil {
			return "", settingsErr(err)
		}
	}
	if _, err := guardlog.Decide(home, e.ID, guardlog.Allowed); err != nil && !errors.Is(err, guardlog.ErrGone) {
		return "", err
	}
	journal("guard.allow", what+" ("+scope+")")
	tell(fmt.Sprintf("[tm] The user allowed the %s call the guard refused (%s: %s), for %s: you may retry it.", e.Tool, e.Rule, e.Summary, scope))
	return "allowed for " + scope + "; " + e.Label() + " is told it may retry", nil
}
