package server

// session.approve (docs/SPEC.md §11.2): answering an agent's permission
// prompt from its screen, after the checks tm thread approve made in
// the CLI. Here they serve the CLI and the Mac app's bridge alike.

import (
	"strconv"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// approve is session.approve's: the agent's state says it is blocked on
// a permission prompt, its screen shows the dialog with the option,
// which allows once only (for the session too, from the user in the Mac
// app, confirmed: scopeAllowed), and an escape's prompt is the user's (Relay
// coordinator is refused there, anyone else needs Escape). Then it
// sends the option's number and journals it as the caller's, or the
// coordinator's for Relay.
func (s *Server) approve(p proto.SessionApproveParams, cc callCtx) (any, *proto.Error) {
	sess, perr := s.session(p.ID)
	if perr != nil {
		return nil, perr
	}
	choice := p.Choice
	if choice == 0 {
		choice = 1
	}
	if choice < 1 || choice > 9 {
		return nil, proto.Errorf(proto.ErrBadParams, "choice %d: a dialog's options are 1-9", p.Choice)
	}
	info := sess.Info()
	byCoord := p.Relay == proto.RoleCoordinator
	if byCoord {
		if perr := coordinatorApproves(info.Project); perr != nil {
			return nil, perr
		}
	}
	if info.State != string(agent.StateBlocked) || info.Reason != "permission" {
		return nil, proto.Errorf(proto.ErrNotPermission, "session %s is %s/%s, not blocked on a permission prompt", p.ID, info.State, info.Reason)
	}
	// A command that asks to run outside the sandbox is the user's to
	// allow, whoever else may answer prompts (T574 case 6), and the
	// user's only once they confirmed it, naming the command.
	if info.Escape && cc.who().Remote() {
		return nil, proto.Errorf(proto.ErrDeviceEscapeOff, "session %s asks to run a command outside its sandbox: a paired device or server doesn't answer that prompt; answer it on this computer", p.ID)
	}
	if info.Escape && byCoord {
		return nil, proto.Errorf("human-only", "session %s asks to run a command outside its sandbox: only the user answers that prompt", p.ID)
	}
	if info.Escape && !p.Escape && !p.Check {
		return nil, proto.Errorf(proto.ErrEscapeUnconfirmed, "session %s asks to run a command outside its sandbox: allow it only once the user confirmed that command", p.ID)
	}
	// The screen must show the dialog too: approving on hook state alone
	// could answer something else.
	x, ok := sess.Explain()
	if !ok {
		return nil, proto.Errorf(proto.ErrRefused, "session %s runs no agent", p.ID)
	}
	if x.Screen == nil || x.Screen.State != agent.StateBlocked || x.Screen.Reason != "permission" {
		return nil, proto.Errorf(proto.ErrNoDialog, "no permission dialog on session %s's screen", p.ID)
	}
	screen, err := sess.Read(false)
	if err != nil {
		return nil, sessionError(p.ID, err)
	}
	option, question := agent.DialogOption(screen.Text, choice)
	if option == "" {
		return nil, proto.Errorf(proto.ErrNoDialog, "the dialog on session %s's screen has no option %d", p.ID, choice)
	}
	m := agent.ManifestOf(sess.Agent())
	scope := agent.OptionScope(m, option)
	if perr := scopeAllowed(scope, choice, option, p, cc.app && !byCoord); perr != nil {
		return nil, perr
	}
	res := proto.SessionApproveResult{Question: question, Option: option, Options: agent.DialogOptions(screen.Text), Escape: info.Escape}
	for _, o := range res.Options {
		res.Scopes = append(res.Scopes, string(agent.OptionScope(m, o)))
	}
	if p.Check {
		return res, nil
	}
	if _, perr := s.keys(proto.SessionKeysParams{ID: p.ID, Data: strconv.Itoa(choice), Relay: p.Relay}); perr != nil {
		return nil, perr
	}
	res.Sent = true
	who := cc.who()
	if byCoord {
		who = caller.Caller{Kind: caller.Coordinator, Project: info.Project}
	}
	ref := info.Thread
	if ref == "" {
		ref = info.ID
	}
	detail := question + " → " + option
	if scope == agent.ScopeSession {
		detail += " (for this session)"
	}
	s.journalIn(info.Project, who, "thread.approve", ref, textsafe.Line(detail, 160))
	s.watch.wake()
	return res, nil
}

// scopeAllowed refuses an option by how long it allows: one beyond the
// session always (the user's to pick, in the agent's pane), one for the
// session unless the user picks it in the Mac app (app) and confirmed
// it, naming the tool or command (Session). Check needs no
// confirmation: it sends nothing.
func scopeAllowed(scope agent.Scope, choice int, option string, p proto.SessionApproveParams, app bool) *proto.Error {
	switch scope {
	case agent.ScopePersistent:
		return proto.Errorf(proto.ErrAlwaysAllow, "option %d (%s) allows more than once; only the user may choose it, in the agent's pane", choice, textsafe.Line(option, 80))
	case agent.ScopeSession:
		if !app {
			return proto.Errorf(proto.ErrAlwaysAllow, "option %d (%s) allows for the rest of the session; only the user may choose it, in the agent's pane or the Mac app", choice, textsafe.Line(option, 80))
		}
		if !p.Session && !p.Check {
			return proto.Errorf(proto.ErrSessionUnconfirmed, "option %d (%s) allows for the rest of the session: choose it only once the user confirmed that, naming the tool or command", choice, textsafe.Line(option, 80))
		}
	}
	return nil
}

// coordinatorApproves refuses an answer sent for project slug's
// coordinator unless the project lets it answer prompts
// (coordinator_approves).
func coordinatorApproves(slug string) *proto.Error {
	if slug == "" {
		return proto.Errorf("human-only", "only the user answers the prompts of a session outside every project")
	}
	cfg, err := config.Load()
	if err != nil {
		return proto.Errorf(proto.ErrInternal, "%v", err)
	}
	safety, err := cfg.Safety(slug)
	if err != nil {
		return proto.Errorf(proto.ErrInternal, "%v", err)
	}
	if !safety.CoordinatorApproves {
		return proto.Errorf("human-only", "coordinator_approves = false: the user answers project %s's prompts", slug)
	}
	return nil
}

// journalIn journals action in project slug's journal, when the session
// has a project.
func (s *Server) journalIn(slug string, who caller.Caller, action, ref, detail string) {
	if slug == "" {
		return
	}
	p, err := project.Open(slug)
	if err == nil {
		err = p.Journal(who, action, ref, detail)
	}
	if err != nil {
		s.log.Printf("project %s: journal: %v", slug, err)
	}
}
