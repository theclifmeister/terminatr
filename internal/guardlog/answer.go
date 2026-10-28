package guardlog

import (
	"errors"
	"fmt"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// How the human answers a refusal: the console's Guard refusals and the
// Mac app's need card (docs/SPEC.md §8.6, Guard; §3.8).
const (
	AllowHere = "here" // allowed for its project
	AllowAll  = "all"  // allowed for all projects
	Keep      = "keep" // kept refusing: no longer listed
)

// Answer records the human's answer how on refusal e in the log under
// home: kept refusing, or allowed (an allowance in the project's
// settings or all projects'), journaled as who, and then the session
// told it may retry (tell). It returns what to say to the human. A
// settings error comes back as config's.
func Answer(who caller.Caller, home string, e Entry, how string, tell func(string)) (string, error) {
	journal := func(kind, detail string) {
		if p, err := project.Open(e.Project); err == nil {
			p.Journal(who, kind, e.Label(), detail)
		}
	}
	what := fmt.Sprintf("%s %s: %s", e.Rule, e.Tool, e.Summary)
	switch how {
	case Keep:
		if _, err := Decide(home, e.ID, Kept); err != nil {
			return "", err
		}
		journal("guard.keep", what)
		return "the guard keeps refusing it; it is no longer listed", nil
	case AllowHere, AllowAll:
	default:
		return "", fmt.Errorf("unknown answer %q: %s, %s or %s", how, AllowHere, AllowAll, Keep)
	}
	a, err := e.allowance()
	if err != nil {
		return "", err
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	scope, write := "this project", func(list []config.GuardAllow) error { return config.SetProject(e.Project, "guard_allow", list) }
	var list []config.GuardAllow
	if how == AllowAll {
		scope, write = config.AllProjectsName, func(list []config.GuardAllow) error { return config.SetDefaults("guard_allow", list) }
		all, _ := cfg.AllProjects()
		list = all.GuardAllow
	} else {
		own, _ := cfg.Safety(e.Project)
		list = config.OwnAllows(own.GuardAllow)
	}
	if !slices.ContainsFunc(list, a.Same) {
		if err := write(append(slices.Clip(list), a)); err != nil {
			return "", err
		}
	}
	if _, err := Decide(home, e.ID, Allowed); err != nil && !errors.Is(err, ErrGone) {
		return "", err
	}
	journal("guard.allow", what+" ("+scope+")")
	tell(fmt.Sprintf("[tm] The user allowed the %s call the guard refused (%s: %s), for %s: you may retry it.", e.Tool, e.Rule, e.Summary, scope))
	return "allowed for " + scope + "; " + e.Label() + " is told it may retry", nil
}

// Actionable: an allowance can lift refusal e, so the user has a real
// choice; otherwise (the floor, tm's files, a rule no allowance names)
// keeping it refusing is all there is. What Answer allows.
func (e Entry) Actionable() bool {
	_, err := e.allowance()
	return err == nil
}

// allowance is the allowance that lifts refusal e: the call the user
// saw, exactly (a * in it is the call's own, not a wildcard), or why
// none can.
func (e Entry) allowance() (config.GuardAllow, error) {
	if e.Floor {
		return config.GuardAllow{}, errors.New("the guard never allows this one: keep refusing it")
	}
	if e.Rule == "worktree-only" && e.Path {
		// One rule for a folder besides the worktree (T574 case 2).
		return config.GuardAllow{}, errors.New("a write outside the worktree is allowed by a write folder now (Access, Write folders), for the guard and the sandbox alike: add its folder there")
	}
	a := config.GuardAllow{Rule: e.Rule, Exact: true}
	if e.Path {
		a.Path = e.Subject
	} else {
		a.Command = e.Subject
	}
	if err := config.CheckGuardAllow(a); err != nil {
		return config.GuardAllow{}, err
	}
	return a, nil
}
