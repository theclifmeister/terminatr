package server

// session.answer on the agent's own question menu (docs/SPEC.md §3.3,
// Ask): Claude's AskUserQuestion, which the session keeps from the
// call's input (proto.Question.Screen). The answer is typed on the
// agent's screen as tm thread answer types it, once the screen shows
// that question with that option: one question at a time, in order, a
// single option or the user's words.

import (
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// errScreenOnly: the agent's menu takes that answer only in its pane
// (several options of a multi-select).
const errScreenOnly = "screen-only"

// menuPause lets the agent redraw between the keys of an answer: a TUI
// reads one burst of input as one key.
var menuPause = 300 * time.Millisecond

// questionOf is session id's open question: tm's own (asks), else the
// agent's menu while it is blocked on it; nil for none.
func (s *Server) questionOf(id string) *proto.Question {
	if q := s.asks.of(id); q != nil {
		return q
	}
	if sess, perr := s.session(id); perr == nil {
		q, _ := sess.Menu()
		return q
	}
	return nil
}

// answerMenu answers question p.Index of sess's menu q (call's) by keys,
// as the human or the coordinator mayAnswer let through. With
// p.Question set it must still ask that, and p.Answer be one of its
// options' labels, as for tm's own questions.
func (s *Server) answerMenu(sess *session.Session, q *proto.Question, call string, p proto.SessionAnswerParams) (proto.SessionAnswerResult, *proto.Error) {
	var none proto.SessionAnswerResult
	if p.Index < 0 || p.Index >= len(q.Questions) {
		return none, proto.Errorf(proto.ErrBadParams, "the open question has %d question(s), not %d", len(q.Questions), p.Index+1)
	}
	it := q.Questions[p.Index]
	ans := strings.TrimSpace(textsafe.Clean(p.Answer, false))
	labels := make([]string, len(it.Options))
	for j, o := range it.Options {
		labels[j] = o.Label
	}
	label := slices.Contains(labels, ans)
	first := slices.IndexFunc(q.Questions, func(it proto.QuestionItem) bool { return !it.Answered })
	switch {
	case ans == "":
		return none, proto.Errorf(proto.ErrBadParams, "empty answer")
	case p.Question != "" && it.Question != p.Question:
		return none, proto.Errorf(proto.ErrNoQuestion, "session %s's open menu no longer asks that question", p.ID)
	case it.Answered:
		return none, proto.Errorf(proto.ErrRefused, "that question is answered already")
	case first != p.Index:
		return none, proto.Errorf(proto.ErrRefused, "the agent's menu asks its questions in order: answer question %d first", first+1)
	case p.Question != "" && (it.MultiSelect || !label):
		return none, proto.Errorf(proto.ErrBadParams, "%q is not one of that question's options", ans)
	case it.MultiSelect:
		return none, proto.Errorf(errScreenOnly, "%q takes several options: pick them in the agent's pane", it.Question)
	}
	how := agent.AnswerOf(sess.Agent())
	if how == nil {
		return none, proto.Errorf(errScreenOnly, "the agent's menu is answered in its pane")
	}
	// The screen must show the menu, on this question, with the option:
	// keys on anything else would answer something else.
	x, ok := sess.Explain()
	if !ok || x.Screen == nil || x.Screen.Rule != how.Rule {
		return none, proto.Errorf(proto.ErrNoQuestion, "no question menu on session %s's screen", p.ID)
	}
	screen, err := sess.Read(false)
	if err != nil {
		return none, sessionError(p.ID, err)
	}
	choice, text := 0, ""
	for n, o := range agent.DialogOptions(screen.Text) {
		switch {
		case label && menuLabel(o) == ans:
			choice = n + 1
		case !label && how.TextOption != "" && strings.Contains(o, how.TextOption):
			choice, text = n+1, ans
		}
		if choice > 0 {
			break
		}
	}
	if choice == 0 || choice > 9 {
		return none, proto.Errorf(proto.ErrNoQuestion, "session %s's screen shows no option for %q", p.ID, textsafe.Line(ans, 80))
	}
	if !strings.Contains(spaced(screen.Text), spaced(it.Question)) {
		return none, proto.Errorf(proto.ErrNoQuestion, "session %s's screen doesn't show %q", p.ID, textsafe.Line(it.Question, 80))
	}
	for i, k := range agent.MenuKeys(how, choice, text) {
		if i > 0 {
			time.Sleep(menuPause)
		}
		if _, perr := s.keys(proto.SessionKeysParams{ID: p.ID, Data: k, Relay: p.Relay}); perr != nil {
			return none, perr
		}
	}
	sess.MenuAnswered(call, p.Index, ans)
	left := 0
	for i, it := range q.Questions {
		if !it.Answered && i != p.Index {
			left++
		}
	}
	return proto.SessionAnswerResult{Remaining: left}, nil
}

// menuLabel is an option's label as a menu line shows it: Codex draws
// its description in a column after it, a multi-select a box before.
func menuLabel(o string) string {
	o, _, _ = strings.Cut(o, "  ")
	for _, box := range []string{"[ ] ", "[✔] ", "[x] ", "☐ ", "☒ "} {
		o = strings.TrimPrefix(o, box)
	}
	return strings.TrimSpace(o)
}

func spaced(s string) string { return strings.Join(strings.Fields(s), " ") }
