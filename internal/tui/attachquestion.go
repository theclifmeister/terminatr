package tui

import (
	"fmt"
	"strconv"
	"strings"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A program's questions in a director's pane (the programs design
// §7.3): the director's /answer opens them as tm's own question on its
// session, which only the user answers (proto.Question.HumanOnly). The
// attach client shows each question in a menu over the pane as it
// opens, its options to pick (a multi-select's to tick, then submit);
// the pick goes to the server as the user's answer (session.answer,
// with the question as its guard), which sends it on to every member
// question it covers once all have one.

// programQuestionsTool is the Tool of the question a director's /answer
// opens (server.ProgramQuestionsTool).
const programQuestionsTool = "tm program"

// answerSession sends the user's answer to question i of session id's
// open question; a variable for tests.
var answerSession = func(paths tmclient.Paths, p proto.SessionAnswerParams) (proto.SessionAnswerResult, error) {
	var res proto.SessionAnswerResult
	cl, err := tmclient.Connect(paths, false)
	if err != nil {
		return res, err
	}
	defer cl.Close()
	err = cl.Call(proto.MethodSessionAnswer, p, &res)
	return res, err
}

// openQuestionAt is the question of q the menu asks next: its first
// unanswered one, -1 for none.
func openQuestionAt(q *proto.Question) int {
	for i, it := range q.Questions {
		if !it.Answered {
			return i
		}
	}
	return -1
}

// offerQuestion opens the menu for the focused pane's program question
// when one waits that the menu hasn't shown yet, and no menu, dialog
// or confirm is open. It reports whether it opened one. c.mu held.
func (c *client) offerQuestion() bool {
	p := c.focus
	if p == nil || c.menu != nil || c.dialog != nil || c.confirm != nil {
		return false
	}
	q := p.info.Question
	if q == nil || !q.HumanOnly || q.Tool != programQuestionsTool || p.info.Server != "" {
		return false
	}
	i := openQuestionAt(q)
	if i < 0 {
		return false
	}
	key := fmt.Sprintf("%s %d %d", p.info.ID, q.Since.UnixNano(), i)
	if c.askedKey == key {
		return false
	}
	c.askedKey = key
	c.openQuestionMenu(p.info.ID, p.info.Project, q, i, nil)
	return true
}

// openQuestionMenu opens the menu for question i of q, open on session
// id of program slug: its text, then its options; ticked are a
// multi-select's options ticked so far, by index. c.mu held.
func (c *client) openQuestionMenu(id, slug string, q *proto.Question, i int, ticked map[int]bool) {
	it := q.Questions[i]
	w := max(min(dialogWidth, c.cols-2), 8)
	text := wrapLines(it.Question, max(w-4, 4))
	ref := slug + "/Q<n>"
	if f := strings.Fields(it.Header); len(f) > 0 && strings.HasPrefix(f[0], "Q") {
		ref = slug + "/" + f[0]
	}
	more := "or in your own words: tm ask answer " + ref + " --other \"…\""
	if it.MultiSelect {
		more = "space or enter ticks; submit sends · " + more
	}
	text = append(text, "", "Your answer goes to every member question it covers.")
	text = append(text, wrapLines(more, max(w-4, 4))...)
	var items []aitem
	for j, o := range it.Options {
		label := o.Label
		if o.Description != "" {
			label += " — " + o.Description
		}
		key := ""
		if j < 9 {
			key = strconv.Itoa(j + 1)
		}
		if it.MultiSelect {
			box := "[ ] "
			if ticked[j] {
				box = "[x] "
			}
			label = box + label
		}
		j := j
		items = append(items, aitem{label: fit(label, w-4-len(key)-2), key: key, run: func() {
			if !it.MultiSelect {
				c.sendAnswer(id, proto.SessionAnswerParams{ID: id, Index: i, Answer: o.Label, Question: it.Question}, o.Label)
				return
			}
			next := map[int]bool{}
			for k, v := range ticked {
				next[k] = v
			}
			next[j] = !next[j]
			if !c.lock() {
				return
			}
			c.openQuestionMenu(id, slug, q, i, next)
			c.mu.Unlock()
		}})
	}
	if it.MultiSelect {
		items = append(items, aitem{label: "submit the options ticked", run: func() {
			var picks []int
			var labels []string
			for j, o := range it.Options {
				if ticked[j] {
					picks, labels = append(picks, j), append(labels, o.Label)
				}
			}
			if len(picks) == 0 {
				if c.lock() {
					c.flash = "tick one option or more first"
					c.openQuestionMenu(id, slug, q, i, ticked)
					c.status()
					c.mu.Unlock()
				}
				return
			}
			c.sendAnswer(id, proto.SessionAnswerParams{ID: id, Index: i, Picks: picks}, strings.Join(labels, ", "))
		}})
	}
	title := it.Header
	if n := len(q.Questions); n > 1 {
		title = fmt.Sprintf("%s (%d of %d)", title, i+1, n)
	}
	mn := &amenu{title: slug + " · " + title, items: items, text: text, w: w}
	_, h := mn.size()
	mn.x, mn.y = max((c.cols-w)/2, 0), max((c.rows-h)/2, 0)
	c.menu = mn
}

// sendAnswer sends the user's answer (label, in words for the status
// bar) to session id; the status bar says what came of it. The next
// question, if any, opens on the next poll.
func (c *client) sendAnswer(id string, p proto.SessionAnswerParams, label string) {
	res, err := answerSession(c.paths, p)
	if !c.lock() {
		return
	}
	switch {
	case err != nil:
		c.flash = "not answered: " + err.Error()
	case res.Remaining > 0:
		c.flash = "answered: " + oneLine(label)
	default:
		c.flash = "answered: " + oneLine(label) + "; tm sends it to the member questions"
	}
	c.status()
	c.mu.Unlock()
	c.poke()
}
