package server

import (
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

// journalAnswer journals a console's answer to a question menu, from a
// mod's question.answer binding the human confirmed, in the session's
// project, as tm thread answer journals the coordinator's.
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
		if err := pr.Journal(c, "session.answer", p.ID, p.Question+" → "+p.Answer); err != nil {
			s.log.Printf("journal: %v", err)
		}
	}
}
