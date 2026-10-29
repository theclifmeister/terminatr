package e2e

// The agent's own question menu in the Mac app (docs/SPEC.md §3.3,
// Ask): Claude's AskUserQuestion reaches the app as the session's open
// question (screen), read from the call's input, and session.answer
// types the app's answer on the agent's screen, one question at a time.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const appAskScript = `
[[step]]
do = "question"
header = "Color"
question = "Which color should the button be?"
options = ["Red", "Blue"]
descriptions = ["Loud", "Calm"]

[[step]]
do = "question"
header = "Name"
question = "What should the button say?"
options = ["OK", "Go"]

[[step.more]]
header = "Fruits"
question = "Which fruits go on it?"
options = ["Apple", "Pear"]
multi = true

[[step]]
do = "stream"
ms = 50
text = "ASKED"
`

// TestAppAgentQuestion: a thread's and a coordinator's AskUserQuestion
// menu, answered from the app: a guarded option, the user's words,
// questions in order, and a multi-select left to the pane.
func TestAppAgentQuestion(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	for _, kv := range env.Vars {
		if v, ok := strings.CutPrefix(kv, "FAKEAGENT_SCRIPTS="); ok {
			os.WriteFile(filepath.Join(v, "app-ask.toml"), []byte(appAskScript), 0o644)
		}
	}
	th := startThread(t, env, projDir)
	b := runBridge(t, env)
	t.Run("thread", func(t *testing.T) { appAsk(t, env, b, th) })
	t.Run("coordinator", func(t *testing.T) { appAsk(t, env, b, coordinator(t, env)) })
}

func appAsk(t *testing.T, env *Env, b *apptest.Bridge, s *Session) {
	question := func() *appapi.Question {
		t.Helper()
		var list appapi.SessionList
		if err := b.Call(proto.MethodSessionList, nil, &list); err != nil {
			t.Fatal(err)
		}
		for _, x := range list.Sessions {
			if x.ID == s.ID {
				return x.Question
			}
		}
		t.Fatalf("no session %s", s.ID)
		return nil
	}
	answer := func(i int, ans, guard string) (appapi.AnswerResult, error) {
		var r appapi.AnswerResult
		err := b.Call(proto.MethodSessionAnswer, appapi.AnswerParams{ID: s.ID, Index: i, Answer: ans, Question: guard}, &r)
		return r, err
	}
	// sessions.watch pushes each question as it opens and as it goes
	// (T870), without a session.list.
	var first appapi.SessionList
	st, err := b.Stream(proto.MethodSessionsWatch, nil, &first)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Cancel()
	lists := make(chan appapi.SessionList, 64)
	go func() {
		defer close(lists)
		for {
			var l appapi.SessionList
			if _, err := st.Next(&l); err != nil {
				return
			}
			lists <- l
		}
	}()
	pushed := func(what string, ok func(*appapi.Question) bool) {
		t.Helper()
		deadline := time.After(agentWait)
		for {
			select {
			case l, open := <-lists:
				if !open {
					t.Fatalf("sessions.watch ended waiting for %s", what)
				}
				for _, x := range l.Sessions {
					if x.ID == s.ID && ok(x.Question) {
						return
					}
				}
			case <-deadline:
				t.Fatalf("sessions.watch sent no list with %s", what)
			}
		}
	}
	asks := func(text string) func(*appapi.Question) bool {
		return func(q *appapi.Question) bool { return q != nil && q.Screen && q.Questions[0].Question == text }
	}
	if q := question(); q != nil {
		t.Fatalf("question while idle: %+v", q)
	}
	if err := b.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: s.ID, Text: "run app-ask"}, nil); err != nil {
		t.Fatal(err)
	}

	// One single-select question, with its options' descriptions.
	pushed("the color question", asks("Which color should the button be?"))
	env.WaitFor(s, "Which color should the button be?", agentWait)
	waitReason(t, env, s, "question")
	q := question()
	if q == nil || !q.Screen || q.Tool != "AskUserQuestion" || len(q.Questions) != 1 || q.Since.IsZero() {
		t.Fatalf("question: %+v", q)
	}
	if it := q.Questions[0]; it.Header != "Color" || it.MultiSelect || len(it.Options) != 2 || it.Options[1] != (appapi.QuestionOption{Label: "Blue", Description: "Calm"}) {
		t.Fatalf("question item: %+v", it)
	}
	if _, err := answer(0, "Green", "Which color should the button be?"); appCode(err) != proto.ErrBadParams {
		t.Fatalf("a label it hasn't: %v", err)
	}
	if _, err := answer(0, "Blue", "Another question?"); appCode(err) != proto.ErrNoQuestion {
		t.Fatalf("another question's guard: %v", err)
	}
	if r, err := answer(0, "Blue", "Which color should the button be?"); err != nil || r.Remaining != 0 {
		t.Fatalf("answer Blue: %+v %v", r, err)
	}
	env.WaitFor(s, "→ Blue", agentWait)
	pushed("the color question gone", func(q *appapi.Question) bool { return !asks("Which color should the button be?")(q) })

	// Two questions in one call: in order; the user's words; a
	// multi-select is the pane's.
	env.WaitFor(s, "What should the button say?", agentWait)
	waitReason(t, env, s, "question")
	if q = question(); q == nil || len(q.Questions) != 2 || !q.Questions[1].MultiSelect || q.Questions[1].Header != "Fruits" {
		t.Fatalf("two questions: %+v", q)
	}
	if _, err := answer(1, "Apple", ""); appCode(err) != proto.ErrRefused {
		t.Fatalf("the second first: %v", err)
	}
	if r, err := answer(0, "Start now", ""); err != nil || r.Remaining != 1 {
		t.Fatalf("answer with words: %+v %v", r, err)
	}
	env.WaitFor(s, "Which fruits go on it?", agentWait)
	if q = question(); q == nil || !q.Questions[0].Answered || q.Questions[0].Answer != "Start now" || q.Questions[1].Answered {
		t.Fatalf("after the first: %+v", q)
	}
	if _, err := answer(1, "Apple", ""); appCode(err) != "screen-only" {
		t.Fatalf("a multi-select: %v", err)
	}
	// The user picks in the pane.
	env.Keys(s, "1")
	env.WaitFor(s, "[✔] Apple", agentWait)
	env.Keys(s, "\r")
	env.WaitFor(s, "→ Apple", agentWait)
	env.WaitFor(s, "ASKED", agentWait)
	// Answered in the pane: gone from the push at once.
	pushed("no question", func(q *appapi.Question) bool { return q == nil })
	if !Poll(agentWait, func() bool { return question() == nil }) {
		t.Fatalf("question after the call: %+v", question())
	}
}
