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
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
)

func (e *Env) threadApprove(p *project.Project, id string, choice int) error {
	if err := e.coordinatorOnly(p, "approving prompts"); err != nil {
		return err
	}
	if e.Caller.Kind == caller.Coordinator {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if safety, err := cfg.Safety(p.Slug); err != nil {
			return err
		} else if !safety.CoordinatorApproves {
			return &tasks.Error{Code: "human-only", Msg: "coordinator_approves = false: the user answers this thread's prompts"}
		}
	}
	r, info, err := e.liveThread(p, id)
	if err != nil {
		return err
	}
	if info.State != "blocked" || info.Reason != "permission" {
		return &tasks.Error{Code: "not-permission", Msg: fmt.Sprintf("thread %s is %s/%s, not blocked on a permission prompt", id, info.State, info.Reason)}
	}
	// The screen must show the dialog too: approving on hook state alone
	// could answer something else.
	var x struct {
		Screen *struct {
			State, Reason string
		} `json:"screen"`
	}
	if err := e.call(proto.MethodAgentExplain, proto.SessionIDParams{ID: r.Session}, &x); err != nil {
		return err
	}
	if x.Screen == nil || x.Screen.State != "blocked" || x.Screen.Reason != "permission" {
		return &tasks.Error{Code: "no-dialog", Msg: fmt.Sprintf("no permission dialog on thread %s's screen; look with tm thread read %s", id, id)}
	}
	var screen proto.SessionReadResult
	if err := e.call(proto.MethodSessionRead, proto.SessionReadParams{ID: r.Session}, &screen); err != nil {
		return err
	}
	option, question := dialogLines(screen.Text, choice)
	if option == "" {
		return &tasks.Error{Code: "no-dialog", Msg: fmt.Sprintf("the dialog on thread %s's screen has no option %d", id, choice)}
	}
	if l := strings.ToLower(option); strings.Contains(l, "don't ask again") || strings.Contains(l, "always") || strings.Contains(l, "don’t ask again") {
		return &tasks.Error{Code: "always-allow", Msg: fmt.Sprintf("option %d (%s) allows more than once; only the user may choose it", choice, option)}
	}
	if err := e.call(proto.MethodSessionKeys, proto.SessionKeysParams{ID: r.Session, Data: strconv.Itoa(choice)}, nil); err != nil {
		return err
	}
	if err := p.Journal(e.Caller, "thread.approve", id, textsafe.Line(question+" → "+option, 160)); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "approved %s: %s\n", id, shown(option))
	return nil
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
		if it.MultiSelect {
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

// answerArgs are tm thread answer's thread id and flags: options by number (choices)
// or label, free text, and which question of the menu (from 1; 0 is the
// first one not yet answered).
type answerArgs struct {
	id       string
	choices  []int
	options  []string
	text     string
	withText bool
	question int
}

// threadAnswer relays the user's answer to a question menu on a thread
// (docs/SPEC.md §11.2). With the agent's mod the menu is open on the
// server (proto.SessionInfo.Question) and is answered through it, by
// option number or label, or with free text; otherwise by keys on the
// screen, recognised by the agent's own screen rule.
func (e *Env) threadAnswer(p *project.Project, id string, a answerArgs) error {
	if err := e.coordinatorOnly(p, "answering questions"); err != nil {
		return err
	}
	r, info, err := e.liveThread(p, id)
	if err != nil {
		return err
	}
	a.text = strings.TrimSpace(textsafe.Clean(a.text, false))
	if info.Question != nil {
		return e.answerByMod(p, id, r.Session, info.Question, a)
	}
	if len(a.options) > 0 || len(a.choices) != 1 {
		return &tasks.Error{Code: "no-mod-question", Msg: fmt.Sprintf("thread %s's menu isn't open through the mod (mods off, or an older agent): answer one menu at a time with --choice N [--text T], as on its screen", id)}
	}
	return e.answerByKeys(p, id, r, info, a.choices[0], a.text, a.withText, a)
}

// answerByMod answers question a.question of the open menu q through the
// session's mod: the server keeps the answers until the menu's last
// question has one, then the mod answers the menu with them.
func (e *Env) answerByMod(p *project.Project, id, session string, q *proto.Question, a answerArgs) error {
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
	err = e.call(proto.MethodSessionAnswer, proto.SessionAnswerParams{ID: session, Index: k - 1, Answer: answer}, &res)
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
// (the one after the last is the menu's free-text option, as on screen)
// or by label, joined with ", " as the agent joins a multi-select, and
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
	for _, l := range a.options {
		found := ""
		for _, o := range it.Options {
			if strings.EqualFold(strings.TrimSpace(l), o.Label) {
				found = o.Label
				break
			}
		}
		if found == "" {
			labels := make([]string, len(it.Options))
			for i, o := range it.Options {
				labels[i] = strconv.Quote(o.Label)
			}
			return "", &tasks.Error{Code: "no-option", Msg: fmt.Sprintf("%q has no option %q; its options: %s (or --text)", it.Question, l, strings.Join(labels, ", "))}
		}
		picked = append(picked, found)
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
		return &tasks.Error{Code: "no-mod-question", Msg: fmt.Sprintf("thread %s's menu is on question %d; by keys the questions are answered in order, one call each", id, max(cur, 1))}
	}
	option, question := dialogLines(screen.Text, choice)
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
	keys := func(data string) error {
		return e.call(proto.MethodSessionKeys, proto.SessionKeysParams{ID: r.Session, Data: data}, nil)
	}
	if isText && ans.TextFocus != "" {
		// The number would submit the option without the words.
		if err := keys(strings.Repeat(ans.TextFocus, choice-1)); err != nil {
			return err
		}
	} else if err := keys(strconv.Itoa(choice)); err != nil {
		return err
	}
	// Codex draws each option's description in a column after it.
	answer, _, _ := strings.Cut(option, "  ")
	if isText {
		if ans.TextKey != "" {
			time.Sleep(answerPause)
			if err := keys(ans.TextKey); err != nil {
				return err
			}
		}
		time.Sleep(answerPause)
		if err := keys(text); err != nil {
			return err
		}
		time.Sleep(answerPause)
		if err := keys(ans.Submit); err != nil {
			return err
		}
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

var optionRE = regexp.MustCompile(`^[^0-9A-Za-z]*([0-9])\.\s+(.+)$`)

// dialogLines finds option n of a numbered dialog on screen, and the
// question above the options.
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

func dialogLines(screen string, n int) (option, question string) {
	lines := strings.Split(screen, "\n")
	first := -1
	for i, l := range lines {
		m := optionRE.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		if first < 0 {
			first = i
		}
		if m[1] == strconv.Itoa(n) && option == "" {
			option = strings.TrimSpace(m[2])
		}
	}
	for i := first - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); strings.HasSuffix(t, "?") {
			question = t
			break
		}
	}
	return option, question
}
