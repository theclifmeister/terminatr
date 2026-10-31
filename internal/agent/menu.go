package agent

// An agent's own question menu (docs/SPEC.md §11.2): read from the
// input of its [answer] tool's call (Claude's AskUserQuestion), and
// answered by keys on its screen, by tm thread answer and the server's
// session.answer alike.

import (
	"slices"
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

// MenuBox is a numbered option of a multi-select question on screen:
// its number, label, whether its box is ticked and whether the cursor
// is on it.
type MenuBox struct {
	N       int
	Label   string
	Checked bool
	Cursor  bool
}

// MultiMenu reads the multi-select question the agent's menu shows:
// its numbered options, each with a box ("[ ] Apple", "[✔] Pear"), down
// to the unnumbered row that ends them (one of done, e.g. "Next"), and
// whether the cursor (❯) is on that row. ok is false when the screen
// shows no such question: no row of done below the options, or an
// option without a box (a single-select), or options scrolled off (a
// pane too short: Claude marks the row next to them ↓ or ↑).
func MultiMenu(screen string, done []string) (boxes []MenuBox, onDone, ok bool) {
	lines, first := dialogLines(screen)
	if first < 0 {
		return nil, false, false
	}
	for _, l := range lines[first:] {
		t := strings.TrimSpace(l)
		if m := optionRE.FindStringSubmatch(t); m != nil {
			n, _ := strconv.Atoi(m[1])
			label, checked, boxed := unbox(strings.TrimSpace(m[2]))
			if !boxed || n != len(boxes)+1 {
				return nil, false, false
			}
			lead, _, _ := strings.Cut(t, m[1]+".")
			if strings.ContainsAny(lead, "↓↑") {
				return nil, false, false // options scrolled off
			}
			boxes = append(boxes, MenuBox{N: n, Label: label, Checked: checked, Cursor: cursorMark(lead)})
			continue
		}
		if len(boxes) == 0 {
			continue
		}
		row := strings.TrimSpace(strings.TrimLeft(t, "❯>›↓↑ "))
		if slices.Contains(done, row) {
			return boxes, cursorMark(strings.TrimSuffix(t, row)), true
		}
	}
	return nil, false, false
}

// unbox takes a multi-select option's box off its label.
func unbox(o string) (label string, checked, boxed bool) {
	for box, on := range map[string]bool{"[ ] ": false, "[✔] ": true, "[✓] ": true, "[x] ": true, "[X] ": true, "☐ ": false, "☒ ": true} {
		if rest, cut := strings.CutPrefix(o, box); cut {
			return strings.TrimSpace(rest), on, true
		}
	}
	return o, false, false
}

// cursorMark says whether the text before a menu row's number or label
// holds the cursor ("❯", or ">" where Claude draws no figures).
func cursorMark(lead string) bool { return strings.ContainsAny(lead, "❯>›") }
