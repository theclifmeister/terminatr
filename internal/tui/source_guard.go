package tui

import (
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// ServerSource's guard: the refusals in the guard's log, and the user's
// decisions on them (docs/SPEC.md §8.6, Guard). The log is the human's:
// read here, in the console's own process, never through the server.

// How the user decides on a refusal.
const (
	guardAllowHere = guardlog.AllowHere
	guardAllowAll  = guardlog.AllowAll
	guardKeep      = guardlog.Keep
)

func (s *ServerSource) GuardRefusals(slug string) ([]guardlog.Entry, error) {
	if s.Caller.IsAgent() {
		return nil, errHumanOnly
	}
	return guardlog.List(s.Paths.Home, slug)
}

func (s *ServerSource) GuardDecide(e guardlog.Entry, how, session string) (string, error) {
	defer s.touch()
	if s.Caller.IsAgent() {
		return "", errHumanOnly
	}
	return guardDecide(s.Caller, s.Paths.Home, e, how, func(text string) {
		if session != "" {
			var res proto.SessionPromptResult
			s.call(proto.MethodSessionPrompt, proto.SessionPromptParams{ID: session, Text: text}, &res)
		}
	})
}

// guardDecide records the user's decision how on refusal e
// (guardlog.Answer), its settings errors in the console's words.
func guardDecide(who caller.Caller, home string, e guardlog.Entry, how string, tell func(string)) (string, error) {
	msg, err := guardlog.Answer(who, home, e, how, tell)
	return msg, settingsErr(err)
}
