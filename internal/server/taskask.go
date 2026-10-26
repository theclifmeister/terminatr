package server

import (
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// taskAsk is task.ask's: the dashboard's D, A and X (docs/SPEC.md §4,
// Asking the coordinator) for a console that doesn't write the project
// itself, the Mac app's. The task is checked against the live board
// (modaction.NotAskable), and the item and its journal line are the
// caller's.
func (s *Server) taskAsk(p proto.TaskAskParams, cc callCtx) (any, *proto.Error) {
	id, ok := tasks.ParseRef(p.Task)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "%q is no task", p.Task)
	}
	switch p.Kind {
	case project.KindDelegate, project.KindAccept, project.KindSendBack:
	default:
		return nil, proto.Errorf(proto.ErrBadParams, "%q is no task ask: delegate, accept or send-back", p.Kind)
	}
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	t, err := pr.Tasks().Get(id)
	if err != nil {
		return nil, proto.Errorf(proto.ErrRefused, "%v", err)
	}
	if why := modaction.NotAskable(t, p.Kind); why != "" {
		return nil, proto.Errorf(proto.ErrRefused, "%s", why)
	}
	var asked bool
	switch p.Kind {
	case project.KindAccept:
		asked, err = pr.AskAccept(cc.who(), t.Ref())
	case project.KindSendBack:
		if _, nerr := project.SendBackNote(p.Note); nerr != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "%v", nerr)
		}
		asked, err = pr.AskSendBack(cc.who(), t.Ref(), p.Note)
	default:
		asked, err = pr.AskDelegate(cc.who(), t.Ref())
	}
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	s.watch.wake()
	return proto.TaskAskResult{Asked: asked}, nil
}
