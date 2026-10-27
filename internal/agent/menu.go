package agent

// An agent's own question menu (docs/SPEC.md §11.2): read from the
// input of its [answer] tool's call (Claude's AskUserQuestion), and
// answered by keys on its screen, by tm thread answer and the server's
// session.answer alike.

import (
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// Bounds of a menu MenuOf takes: an option is picked by its digit.
const (
	maxMenuQuestions = 8
	maxMenuOptions   = 8
	maxMenuText      = 2000
)

// MenuOf is the question menu a call's input holds, as AskUserQuestion
// takes it: questions[], each with question, header, multiSelect and
// options[] of label and description. Nil when it holds none, or one
// tm can't show whole (no question text, an option without a label,
// more options than digits).
func MenuOf(tool string, input map[string]any) *proto.Question {
	qs, _ := input["questions"].([]any)
	if len(qs) == 0 || len(qs) > maxMenuQuestions {
		return nil
	}
	q := &proto.Question{Tool: tool, Questions: make([]proto.QuestionItem, 0, len(qs))}
	for _, x := range qs {
		m, _ := x.(map[string]any)
		it := proto.QuestionItem{Question: menuText(m["question"]), Header: menuText(m["header"])}
		it.MultiSelect, _ = m["multiSelect"].(bool)
		opts, _ := m["options"].([]any)
		if it.Question == "" || len(opts) == 0 || len(opts) > maxMenuOptions {
			return nil
		}
		for _, y := range opts {
			o, _ := y.(map[string]any)
			op := proto.QuestionOption{Label: menuText(o["label"]), Description: menuText(o["description"])}
			if op.Label == "" {
				return nil
			}
			it.Options = append(it.Options, op)
		}
		q.Questions = append(q.Questions, it)
	}
	return q
}

func menuText(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(textsafe.Clean(textsafe.Clip(s, maxMenuText), false))
}

// MenuKeys are the keys that answer option choice (from 1) of the menu
// on screen, each sent on its own with a pause between (a TUI reads one
// burst of input as one key): its number, or, for the free-text option
// (text set), the keys that focus it, then text and Submit.
func MenuKeys(ans *Answer, choice int, text string) []string {
	if text == "" {
		return []string{strconv.Itoa(choice)}
	}
	var out []string
	if ans.TextFocus != "" {
		// The number would submit the option without the words.
		out = append(out, strings.Repeat(ans.TextFocus, choice-1))
	} else {
		out = append(out, strconv.Itoa(choice))
	}
	if ans.TextKey != "" {
		out = append(out, ans.TextKey)
	}
	return append(out, text, ans.Submit)
}
