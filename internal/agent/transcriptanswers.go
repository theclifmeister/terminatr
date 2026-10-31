package agent

import (
	"errors"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// TranscriptAnswers is a [transcript]'s answers: how an answered
// question menu (the result of its [answer] tool's call, Claude's
// AskUserQuestion) reads as each question and what the user picked
// (docs/SPEC.md §3.3, Transcript), so a conversation shows that rather
// than the result's first line. A result whose text starts with one of
// TextPrefix is one. Its line's object at Result gives the call's
// Questions (the call's input questions: question, header,
// multiSelect, options[] of label), its Answers (an answer by question
// text: an option's label, labels joined with ", " for a multi-select,
// or the user's own words) and Notes (by question text, an object with
// a "notes"). A line without it is read from the text: "Q"="A" pairs,
// each answer whole.
type TranscriptAnswers struct {
	TextPrefix []string `toml:"text_prefix"`
	Result     string   `toml:"result"`
	Questions  string   `toml:"questions"`
	Answers    string   `toml:"answers"`
	Notes      string   `toml:"notes"`
}

func (a *TranscriptAnswers) validate() error {
	var errs []error
	if len(a.TextPrefix) == 0 {
		errs = append(errs, errors.New("answers: needs text_prefix"))
	}
	if a.Result != "" && (a.Questions == "" || a.Answers == "") {
		errs = append(errs, errors.New("answers: result needs questions and answers"))
	}
	return errors.Join(errs...)
}

// answersOf are the answers a result says: line is its line, text its
// text. Nil for a result that isn't an answered menu's.
func (a *TranscriptAnswers) answersOf(line map[string]any, text string) []proto.TranscriptAnswer {
	if a == nil {
		return nil
	}
	text = strings.TrimSpace(text)
	var rest string
	for _, p := range a.TextPrefix {
		if after, ok := strings.CutPrefix(text, p); ok {
			rest = after
			break
		}
	}
	if rest == "" {
		return nil
	}
	if out := a.fromLine(line); len(out) > 0 {
		return out
	}
	return answersOfText(rest)
}

// fromLine reads the answers from the line's object at Result: each of
// its questions in order, with the answer its text has.
func (a *TranscriptAnswers) fromLine(line map[string]any) []proto.TranscriptAnswer {
	if a.Result == "" || line == nil {
		return nil
	}
	res, _ := lookup(line, a.Result)
	obj, _ := res.(map[string]any)
	if obj == nil {
		return nil
	}
	qs, _ := lookup(obj, a.Questions)
	list, _ := qs.([]any)
	answers, _ := lookup(obj, a.Answers)
	byText, _ := answers.(map[string]any)
	var notes map[string]any
	if a.Notes != "" {
		n, _ := lookup(obj, a.Notes)
		notes, _ = n.(map[string]any)
	}
	var out []proto.TranscriptAnswer
	for _, x := range list {
		if len(out) == proto.TranscriptAnswers {
			break
		}
		q, _ := x.(map[string]any)
		text, _ := q["question"].(string)
		if text == "" {
			continue
		}
		ans, ok := byText[text].(string)
		if !ok {
			continue // not answered
		}
		header, _ := q["header"].(string)
		multi, _ := q["multiSelect"].(bool)
		var labels []string
		opts, _ := q["options"].([]any)
		for _, y := range opts {
			if o, _ := y.(map[string]any); o != nil {
				if l, _ := o["label"].(string); l != "" {
					labels = append(labels, l)
				}
			}
		}
		it := proto.TranscriptAnswer{Header: answerLine(header), Question: answerLine(text)}
		picked, typed := splitAnswer(ans, labels, multi)
		for _, p := range picked[:min(len(picked), proto.TranscriptPicked)] {
			it.Picked = append(it.Picked, answerLine(p))
		}
		it.Typed = answerLine(typed)
		if n, _ := notes[text].(map[string]any); n != nil {
			s, _ := n["notes"].(string)
			it.Notes = answerLine(s)
		}
		out = append(out, it)
	}
	return out
}

// splitAnswer is an answer as the options' labels it picks and the
// user's own words: a label whole, or for a multi-select labels joined
// with ", " (the longest first), the words after them typed. An answer
// that is no label is typed. With no labels known, it is picked whole.
func splitAnswer(ans string, labels []string, multi bool) ([]string, string) {
	ans = strings.TrimSpace(ans)
	switch {
	case ans == "":
		return nil, ""
	case len(labels) == 0:
		return []string{ans}, ""
	case slices.Contains(labels, ans):
		return []string{ans}, ""
	case !multi:
		return nil, ans
	}
	byLen := slices.Clone(labels)
	slices.SortStableFunc(byLen, func(a, b string) int { return len(b) - len(a) })
	var picked []string
	rest := ans
	for rest != "" {
		i := slices.IndexFunc(byLen, func(l string) bool {
			return rest == l || strings.HasPrefix(rest, l+", ")
		})
		if i < 0 {
			break
		}
		picked = append(picked, byLen[i])
		rest = strings.TrimPrefix(rest[len(byLen[i]):], ", ")
	}
	return picked, strings.TrimSpace(rest)
}

// answersOfText reads "Q"="A" pairs, separated by ", " and ended by
// "." (Claude's result text after its prefix), each answer whole.
func answersOfText(s string) []proto.TranscriptAnswer {
	var out []proto.TranscriptAnswer
	for len(out) < proto.TranscriptAnswers {
		s = strings.TrimLeft(s, " ,")
		if !strings.HasPrefix(s, `"`) {
			break
		}
		q, after, ok := strings.Cut(s[1:], `"="`)
		if !ok {
			break
		}
		end := answerEnd(after)
		if end < 0 {
			break
		}
		out = append(out, proto.TranscriptAnswer{Question: answerLine(q), Picked: []string{answerLine(after[:end])}})
		s = after[end+1:]
	}
	return out
}

// answerEnd is where the quote that closes an answer is in s: the first
// one followed by the next pair (`, "…"="`), the end, or a "." with no
// quote after it; -1 for none.
func answerEnd(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		rest := s[i+1:]
		if rest == "" || strings.HasPrefix(rest, ".") && !strings.Contains(rest, `"`) {
			return i
		}
		if next, ok := strings.CutPrefix(rest, `, "`); ok && strings.Contains(next, `"="`) {
			return i
		}
	}
	return -1
}

func answerLine(s string) string {
	return textsafe.Line(s, proto.TranscriptAnswerText)
}
