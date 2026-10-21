package server

import (
	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// Session commands from hooks (docs/SPEC.md §8.6, Mods): without the
// Claude mod, a coordinator's /saveclear and /answer are plain plugin
// commands, and the UserPromptSubmit hook that carries them has the
// server run them (HookMap.Command), as the mod's command.run runs tm
// clear and tm ask open, and blocks the prompt with what came of it.

// hookCommandOf is session id's runner of the commands its hooks stand
// for: none with the mod, which registers and runs them itself, and
// only for a coordinator.
func (s *Server) hookCommandOf(id string, mod bool) func(*session.Session, string) string {
	if mod {
		return nil
	}
	return func(_ *session.Session, name string) string {
		s.mu.Lock()
		r := s.records[id]
		s.mu.Unlock()
		if r.Role != proto.RoleCoordinator || r.Project == "" {
			return ""
		}
		switch name {
		case agent.CommandSaveClear:
			_, perr := s.saveClear(proto.CoordinatorClearParams{Project: r.Project})
			return clearedText(perr)
		case agent.CommandAnswer:
			_, perr := s.openQuestions(proto.QuestionsOpenParams{Project: r.Project})
			return answeredText(perr)
		}
		return ""
	}
}

// clearedText is what /saveclear says, in the mod's words (view.ts).
func clearedText(perr *proto.Error) string {
	if perr != nil {
		return "Not cleared: " + perr.Message
	}
	return "Asked: the coordinator saves what it learned, then its conversation is cleared once it is idle"
}

// answeredText is what /answer says, in the mod's words (view.ts).
func answeredText(perr *proto.Error) string {
	if perr != nil {
		return "Not asked: " + perr.Message
	}
	return "Asked: your questions open in a dialog once the coordinator is free"
}
