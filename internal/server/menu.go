package server

// session.answer on the agent's own question menu (docs/SPEC.md §3.3,
// Ask): Claude's AskUserQuestion, which the session keeps from the
// call's input (proto.Question.Screen). The answer is typed on the
// agent's screen as tm thread answer types it, once the screen shows
// that question with that option: one question at a time, in order, a
// single option or the user's words.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

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
// options' labels, as for tm's own questions. A multi-select takes
// p.Picks (answerMulti); p.Answer is then set to the labels picked.
// Once the last question has its answer, the menu's review step, if
// it shows one, gets its Submit (submitReview).
func (s *Server) answerMenu(sess *session.Session, q *proto.Question, call string, p *proto.SessionAnswerParams) (proto.SessionAnswerResult, *proto.Error) {
	var none proto.SessionAnswerResult
	if p.Index < 0 || p.Index >= len(q.Questions) {
		return none, proto.Errorf(proto.ErrBadParams, "the open question has %d question(s), not %d", len(q.Questions), p.Index+1)
	}
	it := q.Questions[p.Index]
	ans := strings.TrimSpace(textsafe.Clean(p.Answer, false))
	multi := len(p.Picks) > 0
	if multi {
		var perr *proto.Error
		if ans, perr = pickedAnswer(it, p.Picks); perr != nil {
			return none, perr
		}
		p.Answer = ans
	}
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
	case p.Question != "" && !multi && (it.MultiSelect || !label):
		return none, proto.Errorf(proto.ErrBadParams, "%q is not one of that question's options", ans)
	case it.MultiSelect && !multi:
		return none, proto.Errorf(errScreenOnly, "%q takes several options: send them as picks, or pick them in the agent's pane", it.Question)
	}
	how := agent.AnswerOf(sess.Agent())
	if how == nil {
		return none, proto.Errorf(errScreenOnly, "the agent's menu is answered in its pane")
	}
	if multi && !how.Multi() {
		return none, proto.Errorf(errScreenOnly, "%s's menu takes several options only in its pane", sess.Agent().Name())
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
	if multi {
		if perr := answerMulti(paneIO{s, sess, p}, p.ID, it, how, p.Picks, screen.Text); perr != nil {
			return none, perr
		}
	} else if perr := s.answerOne(it, how, p, screen.Text, ans, label); perr != nil {
		return none, perr
	}
	sess.MenuAnswered(call, p.Index, ans)
	left := 0
	for i, it := range q.Questions {
		if !it.Answered && i != p.Index {
			left++
		}
	}
	if left == 0 {
		s.submitReview(sess, call, how, p)
	}
	return proto.SessionAnswerResult{Remaining: left}, nil
}

// answerOne types a single option's number, or the free-text option's
// keys and the user's words (label false), on the menu screen shows.
func (s *Server) answerOne(it proto.QuestionItem, how *agent.Answer, p *proto.SessionAnswerParams, screen, ans string, label bool) *proto.Error {
	choice, text := 0, ""
	for n, o := range agent.DialogOptions(screen) {
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
		return proto.Errorf(proto.ErrNoQuestion, "session %s's screen shows no option for %q", p.ID, textsafe.Line(ans, 80))
	}
	if why := menuShows(screen, it.Question); why != "" {
		return proto.Errorf(proto.ErrNoQuestion, "session %s's %s", p.ID, why)
	}
	return s.menuKeys(p, agent.MenuKeys(how, choice, text)...)
}

// menuKeys sends keys to p's session one at a time, menuPause apart.
func (s *Server) menuKeys(p *proto.SessionAnswerParams, keys ...string) *proto.Error {
	for i, k := range keys {
		if i > 0 {
			time.Sleep(menuPause)
		}
		if _, perr := s.keys(proto.SessionKeysParams{ID: p.ID, Data: k, Relay: p.Relay}); perr != nil {
			return perr
		}
	}
	return nil
}

// pickedAnswer is the answer picks (indexes into its options) make to
// multi-select question it: their labels in the options' order, joined
// with ", " as the agent joins them.
func pickedAnswer(it proto.QuestionItem, picks []int) (string, *proto.Error) {
	if !it.MultiSelect {
		return "", proto.Errorf(proto.ErrBadParams, "%q takes one option, not picks", it.Question)
	}
	in := make([]bool, len(it.Options))
	for _, j := range picks {
		if j < 0 || j >= len(it.Options) {
			return "", proto.Errorf(proto.ErrBadParams, "%q has %d option(s), not %d", it.Question, len(it.Options), j+1)
		}
		in[j] = true
	}
	var out []string
	for j, o := range it.Options {
		if in[j] {
			out = append(out, o.Label)
		}
	}
	return strings.Join(out, ", "), nil
}

// menuIO is an agent's pane as an answer drives it: its screen, read
// after the agent redrew, and keys, each sent on its own.
type menuIO interface {
	read() (string, *proto.Error)
	send(keys ...string) *proto.Error
}

// paneIO is session p.ID's pane.
type paneIO struct {
	s    *Server
	sess *session.Session
	p    *proto.SessionAnswerParams
}

func (io paneIO) read() (string, *proto.Error) {
	time.Sleep(menuPause)
	screen, err := io.sess.Read(false)
	if err != nil {
		return "", sessionError(io.p.ID, err)
	}
	return screen.Text, nil
}

func (io paneIO) send(keys ...string) *proto.Error { return io.s.menuKeys(io.p, keys...) }

// answerMulti ticks the boxes of multi-select question it that picks
// pick, and only those, on the menu screen shows, then confirms the
// question (its Next): each key checked on the screen before the next
// goes. Every option must show, with its box: tm types an option's
// number only where it sees it tick. A screen that stops matching stops
// the keys, with what they did said in the error.
func answerMulti(io menuIO, id string, it proto.QuestionItem, how *agent.Answer, picks []int, screen string) *proto.Error {
	boxes, onDone, ok := agent.MultiMenu(screen, how.MultiDone)
	if !ok {
		return proto.Errorf(proto.ErrNoQuestion, "session %s's screen shows no multi-select menu with all its options (a pane too short?)", id)
	}
	if why := menuShows(screen, it.Question); why != "" {
		return proto.Errorf(proto.ErrNoQuestion, "session %s's %s", id, why)
	}
	want := map[int]bool{}
	for _, j := range picks {
		want[j] = true
	}
	// Each option's box on the screen, by the option's index.
	at := make([]int, len(it.Options))
	for j, o := range it.Options {
		at[j] = slices.IndexFunc(boxes, func(b agent.MenuBox) bool { return b.Label == o.Label })
		if at[j] < 0 || boxes[at[j]].N > 9 {
			return proto.Errorf(proto.ErrNoQuestion, "session %s's screen shows no option %q", id, textsafe.Line(o.Label, 80))
		}
	}
	var did []string
	for j, o := range it.Options {
		if boxes[at[j]].Checked == want[j] {
			continue
		}
		if perr := io.send(strconv.Itoa(boxes[at[j]].N)); perr != nil {
			return perr
		}
		text, perr := io.read()
		if perr != nil {
			return perr
		}
		now, nowDone, ok := agent.MultiMenu(text, how.MultiDone)
		if !ok || len(now) != len(boxes) || now[at[j]].Label != o.Label || now[at[j]].Checked != want[j] {
			return proto.Errorf(proto.ErrNoQuestion, "session %s's menu didn't %s %q%s: finish it in the agent's pane", id, tick(want[j]), textsafe.Line(o.Label, 80), didSoFar(did))
		}
		did = append(did, tick(want[j])+"ed "+strconv.Quote(o.Label))
		boxes, onDone = now, nowDone
	}
	// Down to the row that confirms the question, from the cursor.
	if !onDone {
		cur := slices.IndexFunc(boxes, func(b agent.MenuBox) bool { return b.Cursor })
		if cur < 0 {
			return proto.Errorf(proto.ErrNoQuestion, "session %s's menu shows no cursor%s: finish it in the agent's pane", id, didSoFar(did))
		}
		downs := make([]string, len(boxes)-cur)
		for i := range downs {
			downs[i] = how.Down
		}
		if perr := io.send(downs...); perr != nil {
			return perr
		}
		text, perr := io.read()
		if perr != nil {
			return perr
		}
		if now, onDone, _ := agent.MultiMenu(text, how.MultiDone); !onDone || !sameBoxes(now, boxes) {
			return proto.Errorf(proto.ErrNoQuestion, "session %s's menu cursor didn't reach %q%s: finish it in the agent's pane", id, how.MultiDone[0], didSoFar(did))
		}
	}
	return io.send(how.Submit)
}

// sameBoxes says whether two reads of a multi-select show the same
// options, ticked the same.
func sameBoxes(a, b []agent.MenuBox) bool {
	return slices.EqualFunc(a, b, func(x, y agent.MenuBox) bool { return x.N == y.N && x.Label == y.Label && x.Checked == y.Checked })
}

func tick(on bool) string {
	if on {
		return "tick"
	}
	return "untick"
}

// didSoFar says what an answer's keys did before they stopped.
func didSoFar(done []string) string {
	if len(done) == 0 {
		return ""
	}
	return " (tm " + strings.Join(done, ", ") + ")"
}

// reviewWait is how long submitReview waits for the menu's review step
// to show after the last answer (a variable for tests).
var reviewWait = 3 * time.Second

// submitReview picks the review step's Submit (how.Review) once the
// agent's menu (call's) has all its answers: the step shows it under
// how.ReviewTitle. Nothing when the menu closes first (a single
// question takes its answer at once) or the step doesn't show in time:
// the user submits it in the agent's pane then.
func (s *Server) submitReview(sess *session.Session, call string, how *agent.Answer, p *proto.SessionAnswerParams) {
	if how.Review == "" {
		return
	}
	for end := time.Now().Add(reviewWait); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if _, c := sess.Menu(); c != call {
			return
		}
		screen, err := sess.Read(false)
		if err != nil {
			return
		}
		if n := reviewOption(screen.Text, how); n > 0 {
			if perr := s.menuKeys(p, strconv.Itoa(n)); perr != nil {
				s.log.Printf("session %s: submitting the menu's answers: %v", p.ID, perr.Message)
			}
			return
		}
	}
}

// reviewOption is the number of how.Review on the menu's review step on
// screen; 0 when the screen shows none.
func reviewOption(screen string, how *agent.Answer) int {
	if how.ReviewTitle != "" && !strings.Contains(screen, how.ReviewTitle) {
		return 0
	}
	opts := agent.DialogOptions(screen)
	if i := slices.IndexFunc(opts, func(o string) bool { return menuLabel(o) == how.Review }); i >= 0 && i < 9 {
		return i + 1
	}
	return 0
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

// minShown is how much of a question's end a pane must show when its
// start scrolled off: enough not to be another question's ending.
const minShown = 40

// menuShows says why screen doesn't show question, as the agent's menu
// draws it above its options; "" when it does. The menu wraps it
// behind a bar (Claude's "│ ") or in a box, which unbar takes off, and
// spaced folds the wrapping. A pane too short for it all shows its end
// from its top line on: at least minShown of it, starting a word (the
// menu's rule and the option are on the screen as well).
func menuShows(screen, question string) string {
	text, q := spaced(unbar(screen)), spaced(question)
	if strings.Contains(text, q) {
		return ""
	}
	for i := 0; i < len(q)-minShown; i++ {
		if (i == 0 || q[i-1] == ' ') && strings.HasPrefix(text, q[i:]) {
			return ""
		}
	}
	if opts := agent.DialogOptions(screen); len(opts) > 0 {
		above, _, _ := strings.Cut(screen, opts[0])
		if !strings.ContainsFunc(unbar(above), unicode.IsLetter) {
			return "pane is too short to show the question above its options"
		}
	}
	return fmt.Sprintf("screen doesn't show %q", textsafe.Line(question, 80))
}

// unbar is screen with each line's bars and box sides taken off.
func unbar(screen string) string {
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		lines[i] = strings.Trim(l, " \t│┃▌▎|")
	}
	return strings.Join(lines, "\n")
}
