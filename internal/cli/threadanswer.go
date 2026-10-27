package cli

// tm thread approve and answer: relaying the user's answer to a permission
// prompt or a question menu.

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// threadApprove answers thread id's permission prompt with option
// choice through the server's session.approve, which checks the
// agent's state and screen, the escape rule and coordinator_approves
// (for the coordinator, by Relay), and journals the answer.
func (e *Env) threadApprove(p *project.Project, id string, choice int) error {
	if err := e.coordinatorOnly(p, "approving prompts"); err != nil {
		return err
	}
	r, _, err := e.liveThread(p, id)
	if err != nil {
		return err
	}
	// The user's own tm thread approve names the thread and the
	// option: their confirmation of an escape's command too.
	var res proto.SessionApproveResult
	err = e.call(proto.MethodSessionApprove, proto.SessionApproveParams{ID: r.Session, Choice: choice,
		Escape: e.Caller.Kind == caller.Human, Relay: e.relay()}, &res)
	var perr *proto.Error
	if errors.As(err, &perr) {
		switch perr.Code {
		case proto.ErrNotPermission, proto.ErrAlwaysAllow, "human-only":
			return &tasks.Error{Code: perr.Code, Msg: fmt.Sprintf("thread %s: %s", id, perr.Message)}
		case proto.ErrNoDialog:
			return &tasks.Error{Code: perr.Code, Msg: fmt.Sprintf("thread %s: %s; look with tm thread read %s", id, perr.Message, id)}
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "approved %s: %s\n", id, shown(res.Option))
	return nil
}

// relay is the role the keys the command sends answer for: the
// coordinator's, which the server refuses for an escape's prompt.
func (e *Env) relay() string {
	if e.Caller.Kind == caller.Coordinator {
		return proto.RoleCoordinator
	}
	return ""
}

// answerPause lets the agent redraw between the keys of an answer: a
// TUI reads one burst of input as one key.
var answerPause = 300 * time.Millisecond

// questionLines are an open question menu as tm thread show and tm
// context print it: each question with its header, then its options by
// the numbers --choice takes, the last one the menu's free text; each
// line shown as plain text.
func questionLines(q *proto.Question) []string {
	var out []string
	for i, it := range q.Questions {
		l := fmt.Sprintf("%d. %s", i+1, it.Question)
		if it.Header != "" {
			l = fmt.Sprintf("%d. [%s] %s", i+1, it.Header, it.Question)
		}
		switch {
		case it.MultiSelect && q.Screen:
			l += " (several: the user picks them in its pane)"
		case it.MultiSelect:
			l += " (several: --choice 1,3)"
		}
		if it.Answered {
			out = append(out, l+" → answered: "+strconv.Quote(it.Answer))
			continue
		}
		out = append(out, l)
		for j, o := range it.Options {
			opt := fmt.Sprintf("   %d. %s", j+1, o.Label)
			if o.Description != "" {
				opt += " — " + textsafe.Line(o.Description, 100)
			}
			out = append(out, opt)
		}
		out = append(out, fmt.Sprintf("   %d. (the user's own words: --text)", len(it.Options)+1))
	}
	for i, l := range out {
		out[i] = shown(l)
	}
	return out
}

// answerArgs are tm thread answer's thread id and flags: options by
// number (choices), free text, and which question of the menu (from 1; 0
// is the first one not yet answered).
type answerArgs struct {
	id       string
	choices  []int
	text     string
	withText bool
	question int
}

// threadAnswer relays the user's answer to a question menu on a thread
// (docs/SPEC.md §11.2). A question tm itself asks (an approved command
// that asks first) is open on the server (proto.SessionInfo.Question)
// and is answered through it, by option number or with free text; an agent's own menu by keys on the screen, recognised by the
// agent's own screen rule.
func (e *Env) threadAnswer(p *project.Project, id string, a answerArgs) error {
	if err := e.coordinatorOnly(p, "answering questions"); err != nil {
		return err
	}
	r, info, err := e.liveThread(p, id)
	if err != nil {
		return err
	}
	a.text = strings.TrimSpace(textsafe.Clean(a.text, false))
	// The agent's own menu (Screen) is typed on its screen from here,
	// by number, as before the server kept it.
	if info.Question != nil && !info.Question.Screen {
		return e.answerOpen(p, id, r.Session, info.Question, a)
	}
	if len(a.choices) != 1 {
		return &tasks.Error{Code: "no-open-question", Msg: fmt.Sprintf("thread %s's menu is answered on its screen: one menu at a time with --choice N [--text T], as on its screen", id)}
	}
	return e.answerByKeys(p, id, r, info, a.choices[0], a.text, a.withText, a)
}

// answerOpen answers question a.question of the question q open on the
// server: it keeps the answers until q's last question has one, then
// hands them to its asker.
func (e *Env) answerOpen(p *project.Project, id, session string, q *proto.Question, a answerArgs) error {
	k := a.question
	if k == 0 {
		for i, it := range q.Questions {
			if !it.Answered {
				k = i + 1
				break
			}
		}
	}
	if k < 1 || k > len(q.Questions) {
		return &tasks.Error{Code: "no-question", Msg: fmt.Sprintf("thread %s's menu has %d question(s), not %d", id, len(q.Questions), k)}
	}
	it := q.Questions[k-1]
	answer, err := menuAnswer(it, a)
	if err != nil {
		return err
	}
	var res proto.SessionAnswerResult
	err = e.call(proto.MethodSessionAnswer, proto.SessionAnswerParams{ID: session, Index: k - 1, Answer: answer, Relay: e.relay()}, &res)
	var perr *proto.Error
	if errors.As(err, &perr) && perr.Code == proto.ErrNoQuestion {
		return &tasks.Error{Code: "no-menu", Msg: fmt.Sprintf("thread %s's menu has closed (answered in its pane?); look with tm thread read %s", id, id)}
	}
	if err != nil {
		return err
	}
	if err := p.Journal(e.Caller, "thread.answer", id, textsafe.Line(it.Question+" → "+answer, 160)); err != nil {
		return err
	}
	if res.Remaining > 0 {
		fmt.Fprintf(e.Stdout, "answered %s question %d of %d: %s; %d more before the menu closes (tm thread show %s)\n", id, k, len(q.Questions), answer, res.Remaining, id)
		return nil
	}
	fmt.Fprintf(e.Stdout, "answered %s: %s\n", id, answer)
	return nil
}

// menuAnswer is the answer to question it: the options picked by number
// (the one after the last is the menu's free-text option, as on screen),
// joined with ", " as the agent joins a multi-select, and
// the free text last.
func menuAnswer(it proto.QuestionItem, a answerArgs) (string, error) {
	var picked []string
	other := false
	for _, n := range a.choices {
		switch {
		case n >= 1 && n <= len(it.Options):
			picked = append(picked, it.Options[n-1].Label)
		case n == len(it.Options)+1:
			other = true
		default:
			return "", &tasks.Error{Code: "no-option", Msg: fmt.Sprintf("%q has options 1-%d (%d: your own words, with --text)", it.Question, len(it.Options), len(it.Options)+1)}
		}
	}
	seen := map[string]bool{}
	picked = slices.DeleteFunc(picked, func(l string) bool {
		dup := seen[l]
		seen[l] = true
		return dup
	})
	switch {
	case other && a.text == "":
		return "", &tasks.Error{Code: "needs-text", Msg: fmt.Sprintf("option %d of %q takes the user's words: add --text", len(it.Options)+1, it.Question)}
	case a.withText && a.text == "":
		return "", usagef("empty --text")
	case !it.MultiSelect && len(picked) > 1:
		return "", &tasks.Error{Code: "single-select", Msg: fmt.Sprintf("%q takes one option, not %d", it.Question, len(picked))}
	case !it.MultiSelect && len(picked) == 1 && a.text != "":
		return "", &tasks.Error{Code: "not-text-option", Msg: fmt.Sprintf("%q takes one option or the user's words, not both", it.Question)}
	}
	if a.text != "" {
		picked = append(picked, a.text)
	}
	return strings.Join(picked, ", "), nil
}

// answerByKeys answers the menu on the thread's screen with keys: option
// n, or the free-text option with text.
func (e *Env) answerByKeys(p *project.Project, id string, r *thread.Record, info proto.SessionInfo, choice int, text string, withText bool, a answerArgs) error {
	paths, err := client.ResolvePaths()
	if err != nil {
		return err
	}
	var ans *agent.Answer
	reg, _ := agent.Load(paths.AgentsDir())
	if reg != nil {
		if a, ok := reg.Get(info.Agent); ok {
			ans = agent.AnswerOf(a)
		}
	}
	if ans == nil {
		return &tasks.Error{Code: "no-answer", Msg: fmt.Sprintf("agent %q has no question menus tm can answer; the user answers in thread %s's pane", info.Agent, id)}
	}
	if info.State != "blocked" || info.Reason != "question" {
		return &tasks.Error{Code: "not-question", Msg: fmt.Sprintf("thread %s is %s/%s, not blocked on a question", id, info.State, info.Reason)}
	}
	var x struct {
		Screen *struct{ Rule string } `json:"screen"`
	}
	if err := e.call(proto.MethodAgentExplain, proto.SessionIDParams{ID: r.Session}, &x); err != nil {
		return err
	}
	if x.Screen == nil || !menuRule(reg, info.Agent, ans, x.Screen.Rule) {
		return &tasks.Error{Code: "no-menu", Msg: fmt.Sprintf("no question menu on thread %s's screen; look with tm thread read %s", id, id)}
	}
	var screen proto.SessionReadResult
	if err := e.call(proto.MethodSessionRead, proto.SessionReadParams{ID: r.Session}, &screen); err != nil {
		return err
	}
	cur, total := menuPosition(screen.Text)
	if a.question > 0 && a.question != max(cur, 1) {
		return &tasks.Error{Code: "question-order", Msg: fmt.Sprintf("thread %s's menu is on question %d; by keys the questions are answered in order, one call each", id, max(cur, 1))}
	}
	option, question := agent.DialogOption(screen.Text, choice)
	if option == "" {
		return &tasks.Error{Code: "no-option", Msg: fmt.Sprintf("the menu on thread %s's screen has no option %d", id, choice)}
	}
	isText := ans.TextOption != "" && strings.Contains(option, ans.TextOption)
	switch {
	case withText && !isText:
		return &tasks.Error{Code: "not-text-option", Msg: fmt.Sprintf("option %d (%s) takes no text", choice, option)}
	case isText && text == "":
		return &tasks.Error{Code: "needs-text", Msg: fmt.Sprintf("option %d (%s) takes the user's words: add --text", choice, option)}
	}
	words := ""
	if isText {
		words = text
	}
	for i, k := range agent.MenuKeys(ans, choice, words) {
		if i > 0 {
			time.Sleep(answerPause)
		}
		if err := e.call(proto.MethodSessionKeys, proto.SessionKeysParams{ID: r.Session, Data: k, Relay: e.relay()}, nil); err != nil {
			return err
		}
	}
	// Codex draws each option's description in a column after it.
	answer, _, _ := strings.Cut(option, "  ")
	if isText {
		answer = strconv.Quote(text)
	}
	if err := p.Journal(e.Caller, "thread.answer", id, textsafe.Line(question+" → "+answer, 160)); err != nil {
		return err
	}
	if total > 1 && cur < total {
		fmt.Fprintf(e.Stdout, "answered %s question %d of %d: %s; %d more before the menu closes (tm thread read %s)\n", id, cur, total, answer, total-cur, id)
		return nil
	}
	fmt.Fprintf(e.Stdout, "answered %s: %s\n", id, answer)
	return nil
}

var questionPosRE = regexp.MustCompile(`(?m)^\s*Question (\d+)/(\d+)`)

// menuPosition is the "Question k/N" a multi-question menu shows on
// screen: 0, 0 when it shows none.
func menuPosition(screen string) (k, n int) {
	m := questionPosRE.FindStringSubmatch(screen)
	if m == nil {
		return 0, 0
	}
	k, _ = strconv.Atoi(m[1])
	n, _ = strconv.Atoi(m[2])
	return k, n
}

// menuRule says whether the screen rule id matched a menu tm answers by
// keys: the [answer] rule's question menu, or another blocked rule whose
// reason is question (Codex's "Implement this plan?" and /new's dialog),
// where a number picks an option too.
func menuRule(reg *agent.Registry, name string, ans *agent.Answer, id string) bool {
	if id == ans.Rule {
		return true
	}
	if reg == nil {
		return false
	}
	a, ok := reg.Get(name)
	m := agent.ManifestOf(a)
	if !ok || m == nil {
		return false
	}
	for _, r := range m.Rules {
		if r.ID == id {
			return r.State == agent.StateBlocked && r.Reason == "question"
		}
	}
	return false
}
