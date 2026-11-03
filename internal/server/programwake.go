package server

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// WakeKickoff is the first prompt of a coordinator a program's director
// woke (program.wake): the user didn't open the project, so it works
// from its inbox instead of greeting them.
const WakeKickoff = "You are this project's coordinator. Program %s's director started you so what it filed or sent isn't left waiting; the user may not be watching. Handle your inbox as your rules say, then wait: start nothing your settings don't allow you on your own."

// programWake is program.wake's: member p.Project's coordinator,
// started if none runs, as enter on its coordinator row does, for the
// program's own director or the human. Refused while programs are off,
// for a program that isn't there, is left out or is paused (the user's
// pause holds its director's wakes too), for a project that
// isn't a local member (a paired member's coordinator is its user's),
// and for an inactive or paused one, which stays the user's. A start is
// journaled in the member and in the program (coordinator.wake).
func (s *Server) programWake(p proto.ProgramWakeParams, cc callCtx) (any, *proto.Error) {
	c := cc.who()
	slug, perr := wakeCaller(p.Program, c)
	if perr != nil {
		return nil, perr
	}
	member := strings.TrimSpace(p.Project)
	if member == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "name the member project: tm program wake <project>")
	}
	cfg, _ := config.Load()
	if _, perr := directorReady(cfg, slug); perr != nil {
		return nil, perr
	}
	if cfg.DirectorSafety(slug).Paused {
		return nil, proto.Errorf("program-paused", "program %s is paused by the user: its members' coordinators stay theirs to start until they resume it", slug)
	}
	if strings.Contains(member, "/") {
		if cfg.ProgramOf(member) == slug {
			return nil, proto.Errorf("paired-member", "%s is on a paired server: its coordinator is its user's to start", member)
		}
		return nil, proto.Errorf("not-member", "%s is not a member of program %s (tm program show %s)", member, slug, slug)
	}
	if cfg.ProgramOf(member) != slug {
		return nil, proto.Errorf("not-member", "%s is not a member of program %s (tm program show %s)", member, slug, slug)
	}
	safety, err := cfg.Safety(member)
	if err != nil {
		safety = config.Defaults
	}
	switch {
	case !safety.Active:
		return nil, proto.Errorf("project-inactive", "%s is inactive: only the user activates it, and its coordinator stays theirs to start", member)
	case safety.Paused:
		return nil, proto.Errorf("project-paused", "%s is paused by the user: its coordinator stays theirs to start until they resume it", member)
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	kickoff := fmt.Sprintf(WakeKickoff, slug)
	// A sleeping coordinator resumes its conversation (coordsleep.go),
	// journaled in the member there.
	id, started := s.wakeCoordinator(member, c, "program "+slug)
	if !started {
		var err error
		id, started, err = openCoordinator(s.listSessions, s.startForOpen, member, "", p.Cols, p.Rows, kickoff)
		if err != nil {
			return nil, openError(err)
		}
		if started {
			if pr, err := project.Open(member); err == nil {
				if err := pr.Journal(c, "coordinator.wake", id, "program "+slug); err != nil {
					s.log.Printf("program.wake %s/%s: member journal: %v", slug, member, err)
				}
			}
		}
	}
	if started {
		if g, err := program.Open(slug, cfg); err == nil {
			if err := g.Journal(c, "coordinator.wake", member, id); err != nil {
				s.log.Printf("program.wake %s/%s: program journal: %v", slug, member, err)
			}
		}
		s.watch.wake()
	}
	return proto.ProgramOpenResult{Session: id, Started: started}, nil
}

// wakeCaller is the program a program.wake of caller c acts for: a
// director's own (named or not), any for the human here. Every other
// caller is refused, a paired server's director (a member's coordinator
// on this server isn't woken over the link) and the human acting from
// elsewhere among them.
func wakeCaller(slug string, c caller.Caller) (string, *proto.Error) {
	switch {
	case c.Kind == caller.Director:
		if slug != "" && slug != c.Project {
			return "", proto.Errorf("other-program", "a director wakes only its own program's members")
		}
		return c.Project, nil
	case c.Kind == caller.Human && !c.Remote():
		if slug == "" {
			return "", proto.Errorf(proto.ErrBadParams, "name the program: tm program wake <project> --program <slug>")
		}
		return slug, nil
	}
	return "", proto.Errorf("director-only", "program.wake is a program's director's (and the human's at this computer)")
}
