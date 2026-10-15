package cli

// tm thread adopt: a running agent session outside the projects becomes a
// thread.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

type adoptOpts struct {
	task, title *string
}

// threadAdopt makes a running agent session outside the projects a
// thread (docs/SPEC.md §9, Adopt): a thread record for where it works,
// the session given the thread's role, and a prompt that tells it so.
func (e *Env) threadAdopt(p *project.Project, sid string, o adoptOpts, asJSON bool) error {
	if err := e.coordinatorOnly(p, "adopting sessions"); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	safety, err := cfg.Safety(p.Slug)
	if err != nil {
		return err
	}
	if err := parkedErr(p, safety); err != nil {
		return err
	}
	// An agent adopts only the session the user asked it to, with T on
	// the dashboard: the user's own session is theirs, so neither
	// --approved-by-user (the agent's word) nor start_threads = auto
	// will do. Asked before anything else, so the refusal is the same
	// whether or not the session exists and wherever it belongs.
	if e.Caller.IsAgent() {
		var ask proto.SessionAdoptAskResult
		err := e.call(proto.MethodSessionAdoptAsk, proto.SessionAdoptAskParams{ID: sid, Project: p.Slug, Check: true}, &ask)
		var perr *proto.Error
		if errors.As(err, &perr) && perr.Code == proto.ErrUnknownMethod {
			return &tasks.Error{Code: "old-server", Msg: "the running server can't check the user's go-ahead to adopt; restart it (tm server restart)"}
		}
		if err != nil {
			return err
		}
		if !ask.Asked {
			return &tasks.Error{Code: "needs-approval", Msg: fmt.Sprintf("the user hasn't asked to adopt %s into %s, or took it back, or asked over an hour ago: only the user adopts their own session, with T on its row in tm's dashboard (or tm thread adopt in their shell); ask them to", sid, p.Slug)}
		}
	}
	var res proto.SessionListResult
	if err := e.call(proto.MethodSessionList, nil, &res); err != nil {
		return err
	}
	var info proto.SessionInfo
	for _, s := range res.Sessions {
		if s.ID == sid {
			info = s
		}
	}
	switch {
	case info.ID == "":
		return &tasks.Error{Code: proto.ErrUnknownSession, Msg: fmt.Sprintf("no session %s (tm session list)", sid)}
	case info.Role != proto.RoleShell || info.Project != "":
		what := "the coordinator"
		if info.Thread != "" {
			what = "thread " + info.Thread
		}
		return &tasks.Error{Code: "in-project", Msg: fmt.Sprintf("session %s is %s of project %s already", sid, what, info.Project)}
	case info.Agent == "" || info.State == "exited":
		return &tasks.Error{Code: "no-agent", Msg: fmt.Sprintf("no agent runs in session %s; only an agent session becomes a thread", sid)}
	}
	task, err := threadTask(p, *o.task)
	if err != nil {
		return err
	}
	rec := thread.Record{Agent: info.Agent, Worktree: info.Cwd, State: thread.Running, Adopted: true, Session: info.ID, AgentSID: info.AgentSID}
	if pl, ok := worktree.Locate(info.Cwd); ok {
		rec.Repo, rec.Branch, rec.Worktree, rec.Checkout = pl.Repo, pl.Branch, pl.Top, !pl.Linked
	}
	rec.Title = strings.Join(strings.Fields(*o.title), " ")
	switch {
	case rec.Title != "":
	case task != nil:
		rec.Title = task.Title
	default:
		rec.Title = filepath.Base(rec.Worktree)
	}
	if task != nil {
		rec.Task = task.Ref()
	}
	now := time.Now().UTC()
	rec.Created, rec.LastPrompt = now, now
	r, err := thread.Create(p, rec)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		thread.Update(p, r.ID, func(x *thread.Record) error { x.State, x.Session = thread.Stopped, ""; return nil })
		return fmt.Errorf("thread %s: %w", r.ID, err)
	}
	brief, err := e.threadFiles(p, r, task)
	if err != nil {
		return fail(err)
	}
	var adopted proto.SessionStartResult
	if err := e.call(proto.MethodSessionAdopt, proto.SessionAdoptParams{ID: sid, Project: p.Slug, Thread: r.ID, Brief: brief, Asked: e.Caller.IsAgent()}, &adopted); err != nil {
		var perr *proto.Error
		if errors.As(err, &perr) && perr.Code == proto.ErrUnknownMethod {
			err = &tasks.Error{Code: "old-server", Msg: "the running server can't adopt sessions; restart it (tm server restart)"}
		}
		return fail(err)
	}
	if adopted.Session.AgentSID != "" && adopted.Session.AgentSID != r.AgentSID {
		r, _ = thread.Update(p, r.ID, func(x *thread.Record) error { x.AgentSID = adopted.Session.AgentSID; return nil })
	}
	detail := strings.TrimSpace(r.Task+" "+r.Title) + " (session " + sid + ")"
	if e.Caller.IsAgent() {
		detail += " (asked by the user)"
	}
	if err := p.Journal(e.Caller, "thread.adopt", r.ID, detail); err != nil {
		return err
	}
	var pr proto.SessionPromptResult
	if err := e.call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: sid, Text: thread.AdoptKickoff(p.Slug, r.ID, brief)}, &pr); err != nil {
		return fmt.Errorf("thread %s adopted, but its prompt failed (tm thread prompt %s): %w", r.ID, r.ID, err)
	}
	if asJSON {
		return e.printJSON(map[string]any{"id": r.ID, "worktree": r.Worktree, "branch": r.Branch, "repo": r.Repo, "checkout": r.Checkout, "session": sid, "task": r.Task})
	}
	fmt.Fprintf(e.Stdout, "adopted session %s as %s in %s", sid, r.ID, r.Worktree)
	if r.Branch != "" {
		fmt.Fprintf(e.Stdout, " on %s", r.Branch)
	}
	fmt.Fprintf(e.Stdout, " (prompt %s)\n", pr.Via)
	return nil
}
