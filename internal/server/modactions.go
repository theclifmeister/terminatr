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
// needs a grant to call, but available reads a project's board: a task
// or project target needs project_read and a project the mod runs for,
// so a mod can't learn a task's status by probing.
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
		if spec.On != modaction.OnSession {
			if !c.Mod.Granted(mod.PermProjectRead) {
				return nil, modbroker.Denied("actions.available on a project needs the %s permission, which %s doesn't have", mod.PermProjectRead, c.Mod.ID)
			}
			if err := c.Project(a.Target.Project); err != nil {
				return nil, err
			}
		}
		return modaction.Why(a.ID, s.actionState(), a.Target) == "", nil
	})
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

// modActionDone is mod.action.done from a console: the human ran (ok)
// or was refused an action mod p.Mod's row bound. Human-only: an agent
// can't make a mod believe the human acted. The mod must be enabled, and
// the action one a mod may bind, on a target of its shape; the event
// goes to that mod alone, and only for a project it runs for.
func (s *Server) modActionDone(p proto.ModActionDoneParams, peerPID int) (any, *proto.Error) {
	if s.callerOf(peerPID).IsAgent() {
		return nil, proto.Errorf("human-only", "only a human's console reports a bound action")
	}
	m, ok := s.modNamed(p.Mod)
	if !ok || m.State != mod.Enabled {
		return nil, proto.Errorf(proto.ErrBadParams, "no enabled mod %q", p.Mod)
	}
	spec, ok := modaction.Lookup(p.Action)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is not an action a mod may bind", p.Action)
	}
	t := modaction.Target{Project: p.Target["project"], Task: p.Target["task"], Thread: p.Target["thread"], Session: p.Target["session"]}
	if why := modaction.TargetWhy(spec, t); why != "" {
		return nil, proto.Errorf(proto.ErrBadParams, "%s", why)
	}
	if t.Project != "" && !m.Runs(t.Project) {
		return nil, proto.Errorf(proto.ErrBadParams, "%s doesn't run for project %s", p.Mod, t.Project)
	}
	if s.modEv != nil {
		s.modEv.bus.PublishTo(p.Mod, modbus.ActionDone, t.Project, modbus.Done{Action: p.Action, Target: t, OK: p.OK})
	}
	return struct{}{}, nil
}
