package agent

import (
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestMenuOf(t *testing.T) {
	opt := func(l, d string) any { return map[string]any{"label": l, "description": d} }
	in := map[string]any{"questions": []any{
		map[string]any{"question": "Which color?", "header": "Color", "options": []any{opt("Red", "Loud"), opt("Blue\x1b[31m", "")}},
		map[string]any{"question": "Which fruits?", "multiSelect": true, "options": []any{opt("Apple", ""), opt("Pear", "")}},
	}}
	q := MenuOf("AskUserQuestion", in)
	want := []proto.QuestionItem{
		{Question: "Which color?", Header: "Color", Options: []proto.QuestionOption{{Label: "Red", Description: "Loud"}, {Label: "Blue[31m"}}},
		{Question: "Which fruits?", MultiSelect: true, Options: []proto.QuestionOption{{Label: "Apple"}, {Label: "Pear"}}},
	}
	if q == nil || q.Tool != "AskUserQuestion" || !slices.EqualFunc(q.Questions, want, func(a, b proto.QuestionItem) bool {
		return a.Question == b.Question && a.Header == b.Header && a.MultiSelect == b.MultiSelect && slices.Equal(a.Options, b.Options)
	}) {
		t.Fatalf("MenuOf: %+v", q)
	}
	many := make([]any, maxMenuOptions+1)
	for i := range many {
		many[i] = opt("o", "")
	}
	for name, in := range map[string]map[string]any{
		"none":          {},
		"no questions":  {"questions": []any{}},
		"no text":       {"questions": []any{map[string]any{"options": []any{opt("a", "")}}}},
		"no options":    {"questions": []any{map[string]any{"question": "Q?"}}},
		"no label":      {"questions": []any{map[string]any{"question": "Q?", "options": []any{opt(" ", "d")}}}},
		"too many opts": {"questions": []any{map[string]any{"question": "Q?", "options": many}}},
	} {
		if q := MenuOf("AskUserQuestion", in); q != nil {
			t.Errorf("%s: %+v", name, q)
		}
	}
}

func TestMenuKeys(t *testing.T) {
	claude := &Answer{TextOption: "Type something", Submit: "\r"}
	codex := &Answer{TextOption: "None of the above", TextFocus: "\x1b[B", TextKey: "\t", Submit: "\r"}
	for _, c := range []struct {
		ans    *Answer
		choice int
		text   string
		want   []string
	}{
		{claude, 2, "", []string{"2"}},
		{claude, 3, "teal", []string{"3", "teal", "\r"}},
		{codex, 3, "teal", []string{"\x1b[B\x1b[B", "\t", "teal", "\r"}},
	} {
		if got := MenuKeys(c.ans, c.choice, c.text); !slices.Equal(got, c.want) {
			t.Errorf("MenuKeys(%d, %q) = %q, want %q", c.choice, c.text, got, c.want)
		}
	}
}
