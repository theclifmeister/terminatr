package server

// The action binding (the mods design §11; docs/SPEC.md §5.3, Action
// binding): $.actions.list and $.actions.available for a mod, and
// action.done once a human ran an action a mod's row bound. The core's
// table and its precondition check are internal/modaction's; the
// console runs the action itself, as the human, from the human's key.

import (
	"context"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// modActionMethods adds actions.list and actions.available to b. Neither
// needs a grant to call, but available reads live state: it needs
// project_read and a target in a project the mod runs for
// (modActionScope), so a mod can't learn a task's status, or which
// sessions run, by probing.
func (s *Server) modActionMethods(b *modbroker.Broker) {
	b.Handle("actions.list", "", func(context.Context, *modbroker.Call) (any, error) {
		out := make([]modapi.Action, len(modaction.Actions))
		for i, a := range modaction.Actions {
			out[i] = modapi.Action{ID: a.ID, Label: a.Label, Key: a.Key, On: a.On, Confirm: a.Confirm, Precondition: a.Precondition}
		}
		return out, nil
	})
	b.Handle("actions.available", "", func(_ context.Context, c *modbroker.Call) (any, error) {
		var a struct {
			ID     string           `json:"id"`
			Target modaction.Target `json:"target"`
		}
		if err := c.Decode(&a); err != nil {
			return nil, err
		}
		spec, ok := modaction.Lookup(a.ID)
		if !ok {
			return false, nil
		}
		if why := modaction.TargetWhy(spec, a.Target); why != "" {
			return nil, modbroker.BadParams("actions.available: %s", why)
		}
		if err := s.modActionScope(c.Mod, spec, a.Target); err != nil {
			if spec.On == modaction.OnSession && !modbroker.IsDenied(err) {
				// A session that isn't running, or not in a project the
				// mod runs for, answers alike: no probing for sessions.
				return false, nil
			}
			return nil, err
		}
		return modaction.Why(a.ID, s.actionState(), a.Target) == "", nil
	})
}

// modActionScope refuses a binding of action a on target t outside mod
// m's reach: it needs project_read, and the target's project (a
// session's own project) must be one m runs for. A session outside the
// projects, or not running, is not found. The console asks it before it
// runs a pressed binding (mod.action.check), and action.done and
// $.actions.available check it, so a mod enabled for one project can't
// have the human act on, or attach to, another.
func (s *Server) modActionScope(m mod.Mod, a modaction.Action, t modaction.Target) error {
	if !m.Granted(mod.PermProjectRead) {
		return modbroker.Denied("binding %s needs the %s permission, which %s doesn't have", a.ID, mod.PermProjectRead, m.ID)
	}
	slug := t.Project
	if a.On == modaction.OnSession {
		sess, perr := s.session(t.Session)
		if perr != nil {
			return modbroker.NotFound("no session %s", t.Session)
		}
		if slug = sess.Info().Project; slug == "" {
			return modbroker.NotFound("session %s is not in a project", t.Session)
		}
	}
	if !m.Runs(slug) {
		return modbroker.Denied("%s doesn't run for project %s", m.ID, slug)
	}
	return nil
}

// actionState is the server's live state for a precondition: the
// project's files as they are now, read once per check.
func (s *Server) actionState() modaction.State {
	return &liveActions{s: s, boards: map[string]*tasks.Board{}}
}

type liveActions struct {
	s      *Server
	boards map[string]*tasks.Board
}

func (l *liveActions) Project(slug string) bool {
	_, err := project.Open(slug)
	return err == nil
}

func (l *liveActions) Task(slug, ref string) *tasks.Task {
	b, ok := l.boards[slug]
	if !ok {
		if p, err := project.Open(slug); err == nil {
			b, _ = p.Tasks().Load()
		}
		l.boards[slug] = b
	}
	if b == nil {
		return nil
	}
	for _, t := range b.Tasks {
		if t.Ref() == ref {
			return t
		}
	}
	return nil
}

func (l *liveActions) Asked(slug, ref string) string {
	p, err := project.Open(slug)
	if err != nil {
		return ""
	}
	items, _ := p.Inbox()
	return project.TaskAsked(items, ref)
}

func (l *liveActions) Session(id string) bool {
	sess, perr := l.s.session(id)
	if perr != nil {
		return false
	}
	select {
	case <-sess.Done():
		return false
	default:
		return true
	}
}

// modBinding checks, for a console, that enabled mod id may bind action
// on target: the action is bindable, the target of its shape and in the
// mod's reach (modActionScope). Human-only: only a human's console asks.
func (s *Server) modBinding(id, action string, target map[string]string, peerPID int) (modaction.Target, *proto.Error) {
	t := modaction.Target{Project: target["project"], Task: target["task"], Thread: target["thread"], Session: target["session"]}
	if s.callerOf(peerPID).IsAgent() {
		return t, proto.Errorf("human-only", "only a human's console presses a mod's binding")
	}
	m, ok := s.modNamed(id)
	if !ok || m.State != mod.Enabled {
		return t, proto.Errorf(proto.ErrBadParams, "no enabled mod %q", id)
	}
	spec, ok := modaction.Lookup(action)
	if !ok {
		return t, proto.Errorf(proto.ErrBadParams, "%q is not an action a mod may bind", action)
	}
	if why := modaction.TargetWhy(spec, t); why != "" {
		return t, proto.Errorf(proto.ErrBadParams, "%s", why)
	}
	if err := s.modActionScope(m, spec, t); err != nil {
		return t, proto.Errorf("refused", "%s", err.Error())
	}
	return t, nil
}

// modActionCheck is mod.action.check: whether a pressed binding is in
// its mod's reach, asked before the console runs it.
func (s *Server) modActionCheck(p proto.ModActionParams, peerPID int) (any, *proto.Error) {
	if _, perr := s.modBinding(p.Mod, p.Action, p.Target, peerPID); perr != nil {
		return nil, perr
	}
	return struct{}{}, nil
}

// modActionDone is mod.action.done from a console: the human ran (ok)
// or was refused an action mod p.Mod's row bound. Human-only: an agent
// can't make a mod believe the human acted. The binding must be in the
// mod's reach (modBinding); the event goes to that mod alone.
func (s *Server) modActionDone(p proto.ModActionDoneParams, peerPID int) (any, *proto.Error) {
	t, perr := s.modBinding(p.Mod, p.Action, p.Target, peerPID)
	if perr != nil {
		return nil, perr
	}
	if s.modEv != nil {
		s.modEv.bus.PublishTo(p.Mod, modbus.ActionDone, t.Project, modbus.Done{Action: p.Action, Target: t, OK: p.OK})
	}
	return struct{}{}, nil
}
