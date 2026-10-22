package server

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// consoleTakeover is HumanInput's took for a console of this server
// attached to a thread's session (info): the first text it types into
// the agent's prompt box during its attach (console, the view client)
// tells the thread's coordinator, in by's words (docs/SPEC.md §4). nil
// once that console has told, and for any other session.
func (s *Server) consoleTakeover(info proto.SessionInfo, console string, by func() caller.Caller) func() {
	if info.Role != proto.RoleThread {
		return nil
	}
	s.mu.Lock()
	told := s.told[info.ID][console]
	s.mu.Unlock()
	if told {
		return nil
	}
	return func() {
		s.mu.Lock()
		if s.told == nil {
			s.told = map[string]map[string]bool{}
		}
		if s.told[info.ID] == nil {
			s.told[info.ID] = map[string]bool{}
		}
		told := s.told[info.ID][console]
		s.told[info.ID][console] = true
		s.mu.Unlock()
		if !told {
			go s.tookOver(info, by(), "")
		}
	}
}

// tookOver tells the coordinator of info's thread that c typed into its
// prompt box, and logs it, with the start of what was typed when given.
func (s *Server) tookOver(info proto.SessionInfo, c caller.Caller, typed string) {
	if typed != "" {
		s.log.Printf("session %s: takeover from %s on %q", info.ID, c, typed)
	} else {
		s.log.Printf("session %s: takeover from %s", info.ID, c)
	}
	pr, err := project.Open(info.Project)
	if err == nil {
		err = pr.TookOver(c, info.Thread, thread.Label(pr, info.Thread))
	}
	if err != nil {
		s.log.Printf("session %s: takeover from %s: %v", info.ID, c, err)
	}
}
