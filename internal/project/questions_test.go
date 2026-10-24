package project

import (
	"strings"
	"testing"
)

func TestQuestions(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "demo-app"})
	if err != nil {
		t.Fatal(err)
	}
	if qs, err := p.Questions(); err != nil || qs != nil {
		t.Fatalf("none: %v %v", qs, err)
	}
	q, err := p.AddQuestion(Question{Task: "T3", Question: " Pick one? ", Recommended: "B",
		Options: []QuestionOption{{Label: "A"}, {Label: " B ", Description: "the safe one"}}})
	if err != nil || q.ID != "Q1" || q.Header != "T3" || q.Question != "Pick one?" || q.Asked.IsZero() {
		t.Fatalf("add %+v %v", q, err)
	}
	if got := QuestionLine(q); got != "Q1 T3 [T3] Pick one? Options: A; B (recommended) — the safe one" {
		t.Fatalf("line %q", got)
	}
	// The context shows them, so a cleared coordinator still knows.
	secs, err := p.Context(Ticked{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(RenderContext(secs), "Open questions (tm ask)") {
		t.Fatal("context lacks the open questions")
	}
	if _, err := p.DoneQuestion("Q1"); err != nil {
		t.Fatal(err)
	}
	secs, _ = p.Context(Ticked{}, nil)
	if strings.Contains(RenderContext(secs), "Open questions (tm ask)") {
		t.Fatal("context shows answered questions")
	}
	for _, c := range []struct {
		name string
		q    Question
	}{
		{"no options", Question{Question: "x?"}},
		{"five options", Question{Question: "x?", Options: []QuestionOption{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}}}},
		{"same label twice", Question{Question: "x?", Options: []QuestionOption{{Label: "a"}, {Label: "a"}}}},
		{"empty", Question{Options: []QuestionOption{{Label: "a"}, {Label: "b"}}}},
	} {
		if _, err := p.AddQuestion(c.q); err == nil || !strings.Contains(err.Error(), "invalid-question") && !strings.Contains(err.(*Error).Code, "invalid-question") {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if QuestionsWaiting(0) != "" || QuestionsWaiting(1) != "1 question waiting" || QuestionsWaiting(3) != "3 questions waiting" {
		t.Fatal("QuestionsWaiting")
	}
}

// TestQuestionPick: an answer names options by number or label, one for
// a single-select question, the user's own words besides.
func TestQuestionPick(t *testing.T) {
	q := Question{ID: "Q1", Options: []QuestionOption{{Label: "Main"}, {Label: "2"}, {Label: "Site"}}}
	for _, c := range []struct {
		choices []string
		other   string
		want    string
	}{
		{[]string{"1"}, "", `"Main"`},
		{[]string{"Site"}, "", `"Site"`},
		{[]string{"2"}, "", `"2"`}, // a label first, then a number
		{nil, " my\nown ", `Other: "my own"`},
	} {
		a, err := q.Pick(c.choices, c.other)
		if err != nil || a.Words() != c.want {
			t.Errorf("Pick(%q, %q) = %q, %v; want %q", c.choices, c.other, a.Words(), err, c.want)
		}
	}
	for _, c := range [][]string{nil, {"4"}, {"0"}, {"main"}, {"1", "3"}} {
		if _, err := q.Pick(c, ""); err == nil {
			t.Errorf("Pick(%q) answers", c)
		}
	}
	q.MultiSelect = true
	if a, err := q.Pick([]string{"3", "1"}, "more"); err != nil || a.Words() != `"Main", "Site", Other: "more"` {
		t.Errorf("multi %q %v", a.Words(), err)
	}
	if _, err := q.Pick(nil, strings.Repeat("x", MaxAnswerText+1)); err == nil {
		t.Error("a long answer is taken")
	}
}
