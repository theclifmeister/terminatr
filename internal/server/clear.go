package server

import (
	"fmt"
	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// clearSession is session.clear: the human clears session id's
// conversation, from a mod's session.clear binding they pressed and
// confirmed (docs/SPEC.md §5.3, Action binding). The agent's clear
// prompt ([inject] clear) is pasted as a plain prompt, never through the
// channel (which runs no slash command), once the agent is idle; it is
// dropped at delivery if the session ended meanwhile. The project's
// files hold its state, so a thread or coordinator gets its context
// back as after any clear (§7.8). Journaled in the session's project.
func (s *Server) clearSession(id string, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "only the human clears a session")
	}
	sess, perr := s.session(id)
	if perr != nil {
		return nil, perr
	}
	text := agent.ClearTextOf(sess.Agent())
	if text == "" {
		return nil, proto.Errorf(proto.ErrRefused, "session %s's agent has no clear prompt ([inject] clear)", id)
	}
	res, perr := s.promptWith(proto.SessionPromptParams{ID: id, Text: text}, session.PromptOptions{
		Refresh: func() (string, bool) {
			select {
			case <-sess.Done():
				return "", false
			default:
				return text, true
			}
		},
	})
	if perr != nil {
		return nil, perr
	}
	if slug := sess.Info().Project; slug != "" {
		if p, err := project.Open(slug); err == nil {
			if err := p.Journal(c, "session.clear", id, ""); err != nil {
				s.log.Printf("journal: %v", err)
			}
		}
	}
	return res, nil
}

// saveClear is coordinator.clear: the user's save and clear of a
// project's coordinator (tm clear, the context hint's key, /saveclear;
// docs/SPEC.md §7.5, Auto-clear). The ticker takes auto-clear's steps
// for it (ticker.RequestClear), journaled as auto-clear's: the save ask,
// then the clear. A coordinator whose agent has no clear prompt is
// refused here, so the user hears so at once.
func (s *Server) saveClear(p proto.CoordinatorClearParams) (any, *proto.Error) {
	if _, err := project.Open(p.Project); err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	var coord *proto.SessionInfo
	for _, info := range s.list().Sessions {
		if info.Project == p.Project && info.Role == proto.RoleCoordinator && info.State != string(agent.StateExited) {
			coord = &info
			break
		}
	}
	if coord == nil {
		return nil, proto.Errorf(proto.ErrRefused, "project %s has no coordinator running", p.Project)
	}
	sess, perr := s.session(coord.ID)
	if perr != nil {
		return nil, perr
	}
	if agent.ClearTextOf(sess.Agent()) == "" {
		return nil, proto.Errorf(proto.ErrRefused, "the coordinator's agent (%s) has no clear prompt ([inject] clear): tm can't clear it; ask it to save what it learned, then clear it by hand", coord.Agent)
	}
	s.mu.Lock()
	t := s.tick
	s.mu.Unlock()
	if t == nil {
		return nil, proto.Errorf(proto.ErrRefused, "this server runs no ticker")
	}
	t.RequestClear(p.Project, *coord)
	return proto.CoordinatorClearResult{Session: coord.ID}, nil
}

// journalAnswer journals a console's answer to a question menu, from a
// mod's question.answer binding the human confirmed or the Mac app, in
// the session's project, as tm thread answer journals the
// coordinator's.
func (s *Server) journalAnswer(p proto.SessionAnswerParams, c caller.Caller) {
	sess, perr := s.session(p.ID)
	if perr != nil {
		return
	}
	slug := sess.Info().Project
	if slug == "" {
		return
	}
	if pr, err := project.Open(slug); err == nil {
		q := p.Question
		if q == "" {
			q = fmt.Sprintf("question %d", p.Index+1)
		}
		if err := pr.Journal(c, "session.answer", p.ID, q+" → "+p.Answer); err != nil {
			s.log.Printf("journal: %v", err)
		}
	}
}
