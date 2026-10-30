package server

import (
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// designList is design.list's (proto.CapDesigns): the project's designs
// as the mod API's designs.list gives them (modDesignOf), each with its
// build in after order, from its board and archive. This server's
// projects only: a paired server's mirror carries no designs.
func (s *Server) designList(p proto.DesignListParams) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	builds, err := pr.BoardDesigns()
	if err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	out := proto.DesignListResult{Designs: []proto.Design{}}
	for _, b := range builds {
		m := modDesignOf(b)
		d := proto.Design{Name: m.Name, Title: m.Title, Status: string(m.Status), Task: m.Task, TaskStatus: m.TaskStatus,
			Artifact: m.Artifact, Tasks: m.Tasks, TasksDone: m.TasksDone, Answers: m.Answers,
			Questions: []proto.DesignQuestion{}, Build: []proto.DesignTask{}, Archived: m.Archived}
		for _, q := range m.Questions {
			d.Questions = append(d.Questions, proto.DesignQuestion{ID: q.ID, Header: q.Header, Question: q.Question})
		}
		for _, t := range b.Ordered() {
			d.Build = append(d.Build, proto.DesignTask{ID: t.Ref(), Title: t.Title, Status: string(t.Status),
				After: taskRefs(t.After), Archived: t.Archived})
		}
		out.Designs = append(out.Designs, d)
	}
	return out, nil
}
