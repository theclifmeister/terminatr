package server

import (
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestHumanOnlyAsk: a program's questions on a director's session
// (Question.HumanOnly) take only the user's answer: not an agent's, a
// device's or a paired server's (byHuman false).
func TestHumanOnlyAsk(t *testing.T) {
	var a asks
	q := proto.Question{Tool: ProgramQuestionsTool, HumanOnly: true, Questions: []proto.QuestionItem{{Question: "Region?", Options: []proto.QuestionOption{{Label: "EU"}, {Label: "US"}}}}}
	o := a.put("s-1", q)
	if _, perr := a.answer("s-1", 0, "EU", "Region?", false); perr == nil || perr.Code != "human-only" {
		t.Fatalf("not the human: %v", perr)
	}
	if res, perr := a.answer("s-1", 0, "EU", "Region?", true); perr != nil || res.Remaining != 0 {
		t.Fatalf("the human: %+v %v", res, perr)
	}
	if got := <-o.answered; got["Region?"] != "EU" {
		t.Errorf("answers: %v", got)
	}
}

// TestChoicesOf reads a dialog's answer back into a program question's
// choices: one label, a multi-select's labels in order (a label with a
// comma in it too), else the user's own words.
func TestChoicesOf(t *testing.T) {
	opts := func(labels ...string) []project.QuestionOption {
		var out []project.QuestionOption
		for _, l := range labels {
			out = append(out, project.QuestionOption{Label: l})
		}
		return out
	}
	single := project.Question{Options: opts("EU", "US")}
	multi := project.Question{MultiSelect: true, Options: opts("Red, dark", "Blue", "Green")}
	for _, c := range []struct {
		q       project.Question
		in      string
		choices []string
		other   string
	}{
		{single, "US", []string{"US"}, ""},
		{single, "Asia", nil, "Asia"},
		{single, "EU, US", nil, "EU, US"},
		{multi, "Red, dark, Green", []string{"Red, dark", "Green"}, ""},
		{multi, "Blue", []string{"Blue"}, ""},
		{multi, "Blue, Purple", nil, "Blue, Purple"},
	} {
		choices, other := choicesOf(c.q, c.in)
		if !slices.Equal(choices, c.choices) || other != c.other {
			t.Errorf("%q: %q %q, want %q %q", c.in, choices, other, c.choices, c.other)
		}
	}
}

// TestRollUpProgramQuestions: an open program question is a row of its
// own (Program, acts answer, Merged its covers), and the member
// questions it covers have none; alike uncovered questions in other
// members are flagged Similar on both, never in the same member.
func TestRollUpProgramQuestions(t *testing.T) {
	opts := []project.QuestionOption{{Label: "Yes"}, {Label: "No"}}
	q := func(id, text, by string) project.Question {
		return project.Question{ID: id, Question: text, Options: opts, CoveredBy: by}
	}
	member := func(slug string, qs ...project.Question) memberNeeds {
		m := memberNeeds{project: slug, questions: map[string]project.Question{}}
		for _, x := range qs {
			m.questions[x.ID] = x
			m.needs = append(m.needs, proto.PeerNeed{ID: "ask:" + x.ID, Kind: proto.NeedAsk, Role: proto.RoleCoordinator})
		}
		return m
	}
	api := member("api", q("Q1", "Ship the beta on Friday?", ""), q("Q2", "Ship the beta Friday?", ""), q("Q3", "Rename the app?", "acme/Q1"))
	web := member("web", q("Q4", "Should we ship the beta on Friday?", ""), q("Q5", "Rename the app?", "acme/Q1"))
	asks := []project.Question{{ID: "Q1", Question: "Rename the app?", Options: opts, Covers: []project.Cover{{Project: "api", Question: "Q3"}, {Project: "web", Question: "Q5"}}}}
	res := rollUp("acme", []memberNeeds{api, web}, asks)
	rows := map[string]proto.ProgramNeed{}
	for _, n := range res.Needs {
		rows[n.Project+" "+n.ID] = n
	}
	if len(rows) != 4 {
		t.Fatalf("rows: %v", rows)
	}
	p := rows["acme ask:Q1"]
	if !p.Program || !slices.Equal(p.Merged, []string{"api/Q3", "web/Q5"}) || !slices.Equal(p.Acts, []string{proto.ActAnswer}) || p.Question == nil {
		t.Errorf("program row: %+v", p)
	}
	if got := rows["api ask:Q1"].Similar; !slices.Equal(got, []string{"Q4@web"}) {
		t.Errorf("api Q1 similar: %v", got)
	}
	if got := rows["web ask:Q4"].Similar; !slices.Equal(got, []string{"Q1@api", "Q2@api"}) {
		t.Errorf("web Q4 similar: %v", got)
	}
	if res.Members[0].Needs != 2 || res.Members[1].Needs != 1 {
		t.Errorf("members: %+v", res.Members)
	}
}
