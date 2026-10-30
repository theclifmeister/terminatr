package server

import (
	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// designList is design.list's (proto.CapDesigns): the project's designs
// as the mod API's designs.list gives them (modDesignOf), each with its
// build in after order, from its board and archive, and Working where
// a thread of a build task is running a turn (designWorking). This
// server's projects only: a paired server's mirror carries no designs.
func (s *Server) designList(p proto.DesignListParams) (any, *proto.Error) {
	pr, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	return designsOf(pr, s.list().Sessions)
}

// designsOf is design.list's result for project pr, with sessions the
// server's.
func designsOf(pr *project.Project, sessions []proto.SessionInfo) (proto.DesignListResult, *proto.Error) {
	builds, err := pr.BoardDesigns()
	if err != nil {
		return proto.DesignListResult{}, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	working := designWorking(pr.Slug, sessions)
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
				After: taskRefs(t.After), Archived: t.Archived, Working: !t.Archived && working[t.Ref()]})
			d.Working = d.Working || d.Build[len(d.Build)-1].Working
		}
		out.Designs = append(out.Designs, d)
	}
	return out, nil
}

// designWorking is the tasks of project slug that a thread of this
// server is running a turn on now: its session's agent state working
// (a kickoff too), not idle, blocked on the user or exited.
func designWorking(slug string, sessions []proto.SessionInfo) map[string]bool {
	out := map[string]bool{}
	for _, info := range sessions {
		if info.Project == slug && info.Server == "" && info.Role == proto.RoleThread && info.Task != "" &&
			info.State == string(agent.StateWorking) {
			out[info.Task] = true
		}
	}
	return out
}
