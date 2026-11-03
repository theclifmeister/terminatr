package program

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
)

func ask(t *testing.T, p *project.Project, text string, multi bool, labels ...string) project.Question {
	t.Helper()
	q := project.Question{Question: text, MultiSelect: multi}
	for _, l := range labels {
		q.Options = append(q.Options, project.QuestionOption{Label: l})
	}
	got, err := p.AddQuestion(q)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestSimilar: alike words and options that match by label (any order,
// case and punctuation aside) are similar; other options, a
// multi-select against a single one, or other words are not.
func TestSimilar(t *testing.T) {
	q := func(text string, multi bool, labels ...string) project.Question {
		out := project.Question{Question: text, MultiSelect: multi}
		for _, l := range labels {
			out.Options = append(out.Options, project.QuestionOption{Label: l})
		}
		return out
	}
	a := q("Should we deploy the API to staging on Fridays?", false, "Yes", "No")
	for _, c := range []struct {
		name string
		b    project.Question
		want bool
	}{
		{"same words, options reordered", q("Should we deploy the API to staging on Fridays?", false, "no", "Yes."), true},
		{"stemmed words", q("Deploying API to staging Friday?", false, "Yes", "No"), true},
		{"other options", q("Should we deploy the API to staging on Fridays?", false, "Yes", "Later"), false},
		{"multi-select", q("Should we deploy the API to staging on Fridays?", true, "Yes", "No"), false},
		{"other words", q("Which database should the web app use?", false, "Yes", "No"), false},
	} {
		if got := Similar(a, c.b); got != c.want {
			t.Errorf("%s: Similar = %v, want %v (jaccard %.2f)", c.name, got, c.want, Jaccard(Words(a.Question), Words(c.b.Question)))
		}
	}
}

// TestMergeAndAnswer: a merge makes program question Q1 covering the
// two member questions (marked covered by acme/Q1, journaled in each);
// the user's answer goes to each, mapped to its own labels, as the
// user's answer there ("human (program acme)"); the program question
// closes, the members' marks clear, and the director gets a
// program-answer item.
func TestMergeAndAnswer(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api, web := openChild(t, "acme-api"), openChild(t, "acme-web")
	qa := ask(t, api, "Which region do we deploy to?", false, "EU", "US")
	qw := ask(t, web, "Which region should we deploy to?", false, "us", "eu")

	pq, err := g.Merge(director, cfg, MergeRequest{Questions: []string{qa.ID + "@acme-api", "acme-web/" + qw.ID}, Header: "Region"})
	if err != nil {
		t.Fatal(err)
	}
	if pq.ID != "Q1" || pq.Header != "Region" || len(pq.Covers) != 2 || !slices.Equal(pq.Covers[1].Map, []string{"eu", "us"}) {
		t.Fatalf("merged: %+v", pq)
	}
	for _, c := range []struct {
		p  *project.Project
		id string
	}{{api, qa.ID}, {web, qw.ID}} {
		q, err := c.p.Question(c.id)
		if err != nil || q.CoveredBy != "acme/Q1" {
			t.Fatalf("%s/%s: %+v %v", c.p.Slug, c.id, q, err)
		}
		if !strings.Contains(journal(t, &c.p.Folder), "director:acme program.ask.merge "+c.id+" covered by acme/Q1") {
			t.Errorf("%s journal: %s", c.p.Slug, journal(t, &c.p.Folder))
		}
	}
	// Merging one again refuses covered.
	if _, err := g.Merge(director, cfg, MergeRequest{Questions: []string{"acme-api/" + qa.ID, "acme-web/" + qw.ID}, Text: "again"}); code(err) != "covered" {
		t.Errorf("second merge: %v", err)
	}
	// The director never answers.
	if _, err := g.Answer(director, cfg, "Q1", []string{"EU"}, ""); code(err) != "human-only" {
		t.Errorf("director's answer: %v", err)
	}

	res, err := g.Answer(human, cfg, "q1", []string{"EU"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fanned) != 2 || res.Fanned[0].Result != "answered" || res.Fanned[1].Answer != `"eu"` {
		t.Fatalf("fanned: %+v", res.Fanned)
	}
	for _, p := range []*project.Project{api, web} {
		items, _ := p.Inbox()
		if !slices.ContainsFunc(items, func(it project.Item) bool { return it.Kind == project.KindAnswer }) {
			t.Errorf("%s: no answer item: %+v", p.Slug, items)
		}
		if j := journal(t, &p.Folder); !strings.Contains(j, "human (program acme) ask.answer") {
			t.Errorf("%s journal: %s", p.Slug, j)
		}
		qs, _ := p.Questions()
		if len(qs) != 1 || qs[0].CoveredBy != "" {
			t.Errorf("%s: questions after: %+v", p.Slug, qs)
		}
	}
	if qs, _ := g.Questions(); len(qs) != 0 {
		t.Errorf("program questions after: %+v", qs)
	}
	items, _ := g.Inbox()
	if len(items) != 1 || items[0].Kind != KindAnswer || items[0].Subject != "Q1" || !strings.Contains(items[0].Summary, "acme-web/"+qw.ID+" answered") {
		t.Errorf("program items: %+v", items)
	}
	if _, err := g.Answer(human, cfg, "Q1", []string{"EU"}, ""); code(err) != "unknown-question" {
		t.Errorf("answered twice: %v", err)
	}
}

// TestMergeRefusals: options that don't match need a map (which then
// maps them), a remote or non-member question, one question, an
// answered one and a coordinator all refuse.
func TestMergeRefusals(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api, web, other := openChild(t, "acme-api"), openChild(t, "acme-web"), openChild(t, "other")
	qa := ask(t, api, "Ship the beta?", false, "Ship it", "Wait")
	qw := ask(t, web, "Ship the beta now?", false, "Hold", "Go")
	qo := ask(t, other, "Ship the beta?", false, "Ship it", "Wait")
	refs := []string{"acme-api/" + qa.ID, "acme-web/" + qw.ID}

	for _, c := range []struct {
		name string
		c    caller.Caller
		req  MergeRequest
		want string
	}{
		{"options differ", director, MergeRequest{Questions: refs}, "incompatible"},
		{"bad map", director, MergeRequest{Questions: refs, Map: map[string]string{"acme-web/" + qw.ID: "1,1"}}, "invalid-map"},
		{"one question", director, MergeRequest{Questions: refs[:1]}, "invalid-merge"},
		{"not a member", director, MergeRequest{Questions: []string{refs[0], "other/" + qo.ID}}, "not-member"},
		{"paired member", director, MergeRequest{Questions: []string{refs[0], "Q1@mini/acme-ios"}}, "remote-member"},
		{"no such question", director, MergeRequest{Questions: []string{refs[0], "acme-web/Q9"}}, "unknown-question"},
		{"a coordinator", caller.Caller{Kind: caller.Coordinator, Project: "acme-api"}, MergeRequest{Questions: refs}, "director-only"},
		{"another director", caller.Caller{Kind: caller.Director, Project: "beta"}, MergeRequest{Questions: refs}, "other-program"},
	} {
		if _, err := g.Merge(c.c, cfg, c.req); code(err) != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
	pq, err := g.Merge(director, cfg, MergeRequest{Questions: refs, Map: map[string]string{qw.ID + "@acme-web": "2,1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pq.Covers[1].Map, []string{"Go", "Hold"}) {
		t.Errorf("mapped: %+v", pq.Covers)
	}
	// Withdrawn, the members' questions are their own again.
	if _, err := g.Withdraw(director, cfg, pq.ID); err != nil {
		t.Fatal(err)
	}
	if q, _ := web.Question(qw.ID); q.CoveredBy != "" {
		t.Errorf("after withdraw: %+v", q)
	}
	// A question the user answered already, waiting on its coordinator,
	// isn't merged.
	if _, _, _, err := api.AnswerQuestion(human, qa.ID, []string{"Wait"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Merge(director, cfg, MergeRequest{Questions: refs, Map: map[string]string{"acme-web/" + qw.ID: "2,1"}}); code(err) != "answered" {
		t.Errorf("answered: %v", err)
	}
}

// TestAnswerClosedCover: a covered question closed meanwhile is left
// (closed); the others still get the answer, the user's own words
// too.
func TestAnswerClosedCover(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api, web := openChild(t, "acme-api"), openChild(t, "acme-web")
	qa := ask(t, api, "Which logo?", false, "Round", "Square")
	qw := ask(t, web, "Which logo?", false, "Round", "Square")
	pq, err := g.Merge(director, cfg, MergeRequest{Questions: []string{"acme-api/" + qa.ID, "acme-web/" + qw.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := web.DoneQuestion(qw.ID); err != nil {
		t.Fatal(err)
	}
	res, err := g.Answer(human, cfg, pq.ID, nil, "a hexagon")
	if err != nil {
		t.Fatal(err)
	}
	if res.Fanned[0].Result != "answered" || res.Fanned[1].Result != "closed" {
		t.Errorf("fanned: %+v", res.Fanned)
	}
}

// TestMemberQuestionRef reads both forms.
func TestMemberQuestionRef(t *testing.T) {
	for in, want := range map[string]string{"Q3@acme-api": "acme-api Q3", "acme-api/q3": "acme-api Q3", " q12@web ": "web Q12"} {
		slug, id, err := MemberQuestionRef(in)
		if err != nil || slug+" "+id != want {
			t.Errorf("%q: %s %s %v", in, slug, id, err)
		}
	}
	for _, in := range []string{"Q3", "acme-api/T3", "@acme", "Q0@x"} {
		if _, _, err := MemberQuestionRef(in); err == nil {
			t.Errorf("%q: no error", in)
		}
	}
}
