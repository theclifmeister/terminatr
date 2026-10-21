package server

import (
	"maps"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

var menu = proto.Question{Tool: "AskUserQuestion", Questions: []proto.QuestionItem{
	{Question: "Which colour?", Header: "Colour", Options: []proto.QuestionOption{{Label: "Red"}, {Label: "Blue"}}},
	{Question: "Which fruits?", MultiSelect: true, Options: []proto.QuestionOption{{Label: "Apple"}, {Label: "Pear"}}},
}}

// TestAsk: an open question shows on the session until it ends; answers
// fill it in question by question, and the last one goes back to the
// asker. A new question replaces it, and dropping it takes it down.
func TestAsk(t *testing.T) {
	// Answers fill in the question in place: a copy of menu's.
	menu := menu
	menu.Questions = slices.Clone(menu.Questions)
	var a asks
	if _, perr := a.answer("s-1", 0, "Red", "", true); perr == nil || perr.Code != proto.ErrNoQuestion {
		t.Fatalf("answer without a question: %v", perr)
	}
	o := a.put("s-1", menu)
	if q := a.of("s-1"); q == nil || len(q.Questions) != 2 || q.Questions[1].Options[1].Label != "Pear" {
		t.Fatalf("open question %+v", q)
	}
	if _, perr := a.answer("s-1", 2, "Red", "", true); perr == nil || perr.Code != proto.ErrBadParams {
		t.Fatalf("answer to question 3 of 2: %v", perr)
	}
	if res, perr := a.answer("s-1", 0, "Blue", "", true); perr != nil || res.Remaining != 1 {
		t.Fatalf("answer 1: %+v %v", res, perr)
	}
	if q := a.of("s-1"); q == nil || !q.Questions[0].Answered || q.Questions[0].Answer != "Blue" || q.Questions[1].Answered {
		t.Fatalf("after answer 1: %+v", q)
	}
	if res, perr := a.answer("s-1", 1, "Apple, kiwi", "", true); perr != nil || res.Remaining != 0 {
		t.Fatalf("answer 2: %+v %v", res, perr)
	}
	select {
	case ans := <-o.answered:
		if want := map[string]string{"Which colour?": "Blue", "Which fruits?": "Apple, kiwi"}; !maps.Equal(ans, want) {
			t.Fatalf("the asker got %v", ans)
		}
	default:
		t.Fatal("the asker got no answers")
	}
	if a.of("s-1") != nil {
		t.Fatal("an answered question stays open")
	}

	// A new question closes the old one unanswered; dropping it takes
	// it down, and a stale drop leaves the newer one.
	first := a.put("s-1", menu)
	second := a.put("s-1", menu)
	select {
	case <-first.replaced:
	default:
		t.Fatal("the replaced question didn't end")
	}
	a.drop("s-1", first)
	if a.of("s-1") == nil {
		t.Fatal("a stale drop took the newer question")
	}
	a.drop("s-1", second)
	if a.of("s-1") != nil {
		t.Fatal("dropped, still open")
	}
}
