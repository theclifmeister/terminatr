package e2e

// tm thread answer (docs/SPEC.md §11.2): the coordinator relays the
// user's answer to a thread's question menu, recognised by the agent's
// screen rule; journaled. The fake agent draws Claude's AskUserQuestion
// menu, with "Type something." as a text field.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const askScript = `
[[step]]
do = "question"
header = "Color"
question = "Which color should the button be?"
options = ["Red", "Blue"]

[[step]]
do = "question"
header = "Name"
question = "What should the button say?"
options = ["OK", "Go"]

[[step]]
do = "stream"
ms = 50
text = "ASKED"
`

// askMultiScript is askScript with a lone multi-select before its end.
var askMultiScript = strings.Replace(askScript, `[[step]]
do = "stream"`, `[[step]]
do = "question"
header = "Fruits"
question = "Which fruits go on it?"
options = ["Apple", "Pear", "Plum"]
multi = true

[[step]]
do = "stream"`, 1)

func TestThreadAnswer(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	scripts := ""
	for _, kv := range env.Vars {
		if v, ok := strings.CutPrefix(kv, "FAKEAGENT_SCRIPTS="); ok {
			scripts = v
		}
	}
	os.WriteFile(filepath.Join(scripts, "thread-ask.toml"), []byte(askMultiScript), 0o644)
	th := startThread(t, env, projDir)
	answer := func(args ...string) Result {
		return env.CLI(append([]string{"thread", "answer", "t-0001", "--project", "demo"}, args...)...)
	}

	// No menu: refused.
	if r := answer("--choice", "1"); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, "not-question") {
		t.Fatalf("answer while idle: %+v", r)
	}
	if r := answer(); r.Code != 2 {
		t.Fatalf("answer without --choice: %+v", r)
	}

	env.MustCLI("thread", "prompt", "t-0001", "run thread-ask", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	env.WaitFor(th, "Which color should the button be?", agentWait)
	waitReason(t, env, th, "question")

	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"--choice", "7"}, "no-option"},
		{[]string{"--choice", "2", "--text", "teal"}, "not-text-option"},
		{[]string{"--choice", "3"}, "needs-text"},
	} {
		if r := answer(c.args...); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, c.code) {
			t.Errorf("answer %v: want %s, got %+v", c.args, c.code, r)
		}
	}
	if r := answer("--choice", "2"); r.Code != 0 || !strings.Contains(r.Stdout, "answered t-0001: Blue") {
		t.Fatalf("answer 2: %+v", r)
	}
	env.WaitFor(th, "→ Blue", agentWait)

	// The free-text option takes the user's words.
	env.WaitFor(th, "What should the button say?", agentWait)
	waitReason(t, env, th, "question")
	if r := answer("--choice", "3", "--text", "Start now\n"); r.Code != 0 || !strings.Contains(r.Stdout, `answered t-0001: "Start now"`) {
		t.Fatalf("answer with text: %+v", r)
	}
	env.WaitFor(th, "→ Start now", agentWait)

	// A multi-select, the menu's only question (its row is Submit, then
	// the review step): several numbers go as picks (T900); words are
	// the pane's.
	env.WaitFor(th, "Which fruits go on it?", agentWait)
	waitReason(t, env, th, "question")
	if r := answer("--choice", "1", "--text", "kiwi"); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, "screen-only") {
		t.Errorf("a multi-select's words: %+v", r)
	}
	if r := answer("--choice", "1,3"); r.Code != 0 || !strings.Contains(r.Stdout, "answered t-0001: Apple, Plum") {
		t.Fatalf("answer 1,3: %+v", r)
	}
	env.WaitFor(th, "→ Apple, Plum", agentWait)
	env.WaitFor(th, "ASKED", agentWait)

	journal, _ := os.ReadFile(filepath.Join(projDir, "JOURNAL.md"))
	for _, w := range []string{"human thread.answer t-0001 Which color should the button be? → Blue",
		`human thread.answer t-0001 What should the button say? → "Start now"`,
		"human thread.answer t-0001 Which fruits go on it? → Apple, Plum"} {
		if !strings.Contains(string(journal), w) {
			t.Errorf("journal lacks %q:\n%s", w, journal)
		}
	}

	// The conversation shows each answered question and what was picked
	// (T903): the option's label, the user's own words.
	b := runBridge(t, env)
	var tr appapi.TranscriptResult
	ts, err := b.Stream(proto.MethodTranscriptWatch, appapi.TranscriptParams{Session: th.ID}, &tr)
	if err != nil {
		t.Fatalf("transcript.watch: %v", err)
	}
	defer ts.Cancel()
	var got []appapi.TranscriptAnswer
	for len(got) < 2 {
		var ev appapi.TranscriptEvent
		if _, err := ts.Next(&ev); err != nil {
			t.Fatalf("transcript: %v (answers so far %+v)", err, got)
		}
		for _, it := range ev.Items {
			if it.Kind == proto.ItemResult {
				got = append(got, it.Answers...)
			}
		}
	}
	if got[0].Question != "Which color should the button be?" || !slices.Equal(got[0].Picked, []string{"Blue"}) ||
		got[1].Question != "What should the button say?" || got[1].Typed != "Start now" || len(got[1].Picked) != 0 {
		t.Errorf("answers %+v", got)
	}
}

// waitReason waits until the session's state has reason r, from the
// settled screen too.
func waitReason(t *testing.T, env *Env, s *Session, r string) {
	t.Helper()
	if !Poll(agentWait, func() bool {
		i, _ := env.Info(s)
		x := env.Explain(s)
		return i.Reason == r && x.Screen != nil && x.Screen.Reason == r
	}) {
		i, _ := env.Info(s)
		t.Fatalf("reason %q, want %q:\n%s", i.Reason, r, env.Screen(s))
	}
}

const codexAskScript = `
[[step]]
do = "question"
style = "codex"
at = 1
of = 2
question = "Which colour should the setting use?"
options = ["Red", "Green", "Blue (Recommended)"]

[[step]]
do = "question"
style = "codex"
at = 2
of = 2
question = "What should it be called?"
options = ["Alpha", "Beta"]

[[step]]
do = "stream"
ms = 50
text = "ASKED"
`

// TestThreadAnswerCodex: Codex's plan-mode menus (T102): "Question k/N"
// menus answered one call each, a number submitting at once; the last
// option, "None of the above", takes the user's words as notes: its
// number, Tab, the words, Enter ([answer] text_key). The fake agent
// draws Codex's screen, matched by Codex's own rule and [answer].
func TestThreadAnswerCodex(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	cx, ok := agent.Builtin("codex")
	if !ok {
		t.Fatal("no built-in codex manifest")
	}
	rule := regexp.MustCompile(`(?s)\[\[rules\]\]\nid = "blocked-question".*?\n\n`)
	codexRule := rule.FindString(string(cx))
	ans := regexp.MustCompile(`(?s)\[answer\]\n.*?\n\n`).FindString(string(cx))
	if codexRule == "" || ans == "" {
		t.Fatal("codex.toml: no blocked-question rule or [answer]")
	}
	env.FakeClaude(func(m string) string {
		m = rule.ReplaceAllLiteralString(m, codexRule)
		return regexp.MustCompile(`(?s)\[answer\]\n.*?\n\n`).ReplaceAllLiteralString(m, ans)
	})
	scripts := ""
	for _, kv := range env.Vars {
		if v, ok := strings.CutPrefix(kv, "FAKEAGENT_SCRIPTS="); ok {
			scripts = v
		}
	}
	os.WriteFile(filepath.Join(scripts, "thread-ask.toml"), []byte(codexAskScript), 0o644)
	th := startThread(t, env, projDir)
	answer := func(args ...string) Result {
		return env.CLI(append([]string{"thread", "answer", "t-0001", "--project", "demo"}, args...)...)
	}
	env.MustCLI("thread", "prompt", "t-0001", "run thread-ask", "--project", "demo")
	env.WaitState(th, "blocked", agentWait)
	env.WaitFor(th, "Question 1/2", agentWait)
	waitReason(t, env, th, "question")

	if r := answer("--choice", "1", "--question", "2"); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, "question-order") {
		t.Errorf("answer question 2 first: %+v", r)
	}
	if r := answer("--choice", "4"); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, "needs-text") {
		t.Errorf("answer 3 without text: %+v", r)
	}
	if r := answer("--choice", "4", "--text", "teal"); r.Code != 0 ||
		!strings.Contains(r.Stdout, "question 1 of 2: ") || !strings.Contains(r.Stdout, `"teal"`) {
		t.Fatalf("answer with notes: %+v", r)
	}
	env.WaitFor(th, "→ teal", agentWait)
	env.WaitFor(th, "Question 2/2", agentWait)
	waitReason(t, env, th, "question")
	if r := answer("--choice", "2"); r.Code != 0 || !strings.Contains(r.Stdout, "answered t-0001: Beta") {
		t.Fatalf("answer 2: %+v", r)
	}
	env.WaitFor(th, "→ Beta", agentWait)
	env.WaitFor(th, "ASKED", agentWait)
}

// TestThreadAnswerCodexMenu: a Codex menu that isn't [answer]'s question
// but another blocked/question rule (/new's dialog here, "Implement this
// plan?" live) is answered by number too, its label without the
// description column (T155).
func TestThreadAnswerCodexMenu(t *testing.T) {
	env, projDir, _ := threadEnv(t)
	env.FakeCodex()
	env.MustCLI("thread", "start", "Small fix", "--agent", "codex", "--project", "demo")
	var rec struct{ Session string }
	readTOML(t, filepath.Join(projDir, "threads", "t-0001", "thread.toml"), &rec)
	th := &Session{ID: rec.Session}
	env.WaitState(th, "idle", agentWait)
	answer := func(args ...string) Result {
		return env.CLI(append([]string{"thread", "answer", "t-0001", "--project", "demo"}, args...)...)
	}

	env.Keys(th, "/new")
	time.Sleep(300 * time.Millisecond)
	env.Keys(th, "\r")
	env.WaitFor(th, "Where should the new conversation run?", agentWait)
	waitReason(t, env, th, "question")
	if r := answer("--choice", "2", "--text", "here"); r.Code != 1 || !strings.Contains(r.Stderr+r.Stdout, "not-text-option") {
		t.Errorf("text on a menu without a text option: %+v", r)
	}
	if r := answer("--choice", "1"); r.Code != 0 || strings.TrimSpace(r.Stdout) != "answered t-0001: Current checkout" {
		t.Fatalf("answer /new's dialog: %+v\n%s", r, env.Screen(th))
	}
	if !Poll(agentWait, func() bool { return !strings.Contains(env.Screen(th), "Where should the new conversation run?") }) {
		t.Fatalf("the dialog stayed:\n%s", env.Screen(th))
	}
}
