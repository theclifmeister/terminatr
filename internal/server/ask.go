package server

// Open questions (session.answer, docs/SPEC.md §3.3, Ask): tm's own
// question on a session (askRun); `tm thread answer` fills in its
// questions one by one, and the last answer goes back to the asker.

import (
	"slices"
	"strings"
	"sync"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// asks holds each session's open question.
type asks struct {
	mu   sync.Mutex
	open map[string]*openAsk
}

type openAsk struct {
	q proto.Question
	// answered gets the answers once every question has one; closed
	// when another ask replaces this one.
	answered chan map[string]string
	replaced chan struct{}
}

// of is a copy of session id's open question, nil for none.
func (a *asks) of(id string) *proto.Question {
	a.mu.Lock()
	defer a.mu.Unlock()
	o := a.open[id]
	if o == nil {
		return nil
	}
	q := o.q
	q.Questions = append([]proto.QuestionItem(nil), o.q.Questions...)
	return &q
}

// put opens q on session id, replacing (and closing) any open one.
func (a *asks) put(id string, q proto.Question) *openAsk {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.open == nil {
		a.open = map[string]*openAsk{}
	}
	if old := a.open[id]; old != nil {
		close(old.replaced)
	}
	o := &openAsk{q: q, answered: make(chan map[string]string, 1), replaced: make(chan struct{})}
	a.open[id] = o
	return o
}

// drop removes o if it is still session id's open question.
func (a *asks) drop(id string, o *openAsk) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.open[id] == o {
		delete(a.open, id)
	}
}

// answer gives question i of session id's open question its answer.
// When none is left unanswered, the answers go to the asker and the
// question closes. With question set (a console's question.answer
// binding), question i must still ask it, unanswered, and ans must be
// one of its options' labels: a menu replaced since the human confirmed
// gets no answer meant for another.
// Only the human (byHuman) answers a question only the user answers
// (Question.Escape), checked under the lock: another question can't
// take its place between the check and the answer.
func (a *asks) answer(id string, i int, ans, question string, byHuman bool) (proto.SessionAnswerResult, *proto.Error) {
	return a.answerPicks(id, i, ans, nil, question, byHuman)
}

// answerPicks is answer with a multi-select's picks (indexes into its
// options) in place of ans, when there are any.
func (a *asks) answerPicks(id string, i int, ans string, picks []int, question string, byHuman bool) (proto.SessionAnswerResult, *proto.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	o := a.open[id]
	if o == nil {
		return proto.SessionAnswerResult{}, proto.Errorf(proto.ErrNoQuestion, "session %s has no open question", id)
	}
	if o.q.Escape && !byHuman {
		return proto.SessionAnswerResult{}, proto.Errorf(proto.ErrRefused, "session %s asks to run a command outside its sandbox: only the user answers that question", id)
	}
	if i < 0 || i >= len(o.q.Questions) {
		return proto.SessionAnswerResult{}, proto.Errorf(proto.ErrBadParams, "the open question has %d question(s), not %d", len(o.q.Questions), i+1)
	}
	if len(picks) > 0 {
		var perr *proto.Error
		if ans, perr = pickedAnswer(o.q.Questions[i], picks); perr != nil {
			return proto.SessionAnswerResult{}, perr
		}
	}
	if strings.TrimSpace(ans) == "" {
		return proto.SessionAnswerResult{}, proto.Errorf(proto.ErrBadParams, "empty answer")
	}
	if question != "" {
		it := o.q.Questions[i]
		labels := make([]string, len(it.Options))
		for j, op := range it.Options {
			labels[j] = op.Label
		}
		switch {
		case it.Question != question:
			return proto.SessionAnswerResult{}, proto.Errorf(proto.ErrNoQuestion, "session %s's open menu no longer asks that question", id)
		case it.Answered:
			return proto.SessionAnswerResult{}, proto.Errorf(proto.ErrRefused, "that question is answered already")
		case len(picks) == 0 && (it.MultiSelect || !slices.Contains(labels, ans)):
			return proto.SessionAnswerResult{}, proto.Errorf(proto.ErrBadParams, "%q is not one of that question's options", ans)
		}
	}
	o.q.Questions[i].Answered, o.q.Questions[i].Answer = true, ans
	left := 0
	for _, it := range o.q.Questions {
		if !it.Answered {
			left++
		}
	}
	if left == 0 {
		out := map[string]string{}
		for _, it := range o.q.Questions {
			out[it.Question] = it.Answer
		}
		o.answered <- out
		delete(a.open, id)
	}
	return proto.SessionAnswerResult{Remaining: left}, nil
}
