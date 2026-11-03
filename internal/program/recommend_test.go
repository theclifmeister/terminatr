package program

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestRecommendDirectorOnly: only the program's own director
// recommends: never the human, another program's director, a member's
// coordinator, a thread or a paired server's director.
func TestRecommendDirectorOnly(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api := openChild(t, "acme-api")
	q := ask(t, api, "Which region?", false, "EU", "US")
	req := RecommendRequest{Question: "acme-api/" + q.ID, Choices: []string{"EU"}, Why: "acme-web runs in the EU (acme-web/T4)"}
	for _, c := range []struct {
		who  caller.Caller
		code string
	}{
		{human, "director-only"},
		{caller.Caller{Kind: caller.Director, Project: "beta"}, "other-program"},
		{caller.Caller{Kind: caller.Coordinator, Project: "acme-api"}, "director-only"},
		{caller.Caller{Kind: caller.Thread, Project: "acme-api", Thread: "t-0001"}, "director-only"},
		{caller.Caller{Kind: caller.Peer, Alias: "mini", Program: "acme"}, "director-only"},
	} {
		if _, _, err := g.Recommend(c.who, cfg, req); code(err) != c.code {
			t.Errorf("%s: %v, want %s", c.who, err, c.code)
		}
	}
	if _, err := g.Unrecommend(human, "acme-api/"+q.ID); code(err) != "director-only" {
		t.Errorf("human clears: %v", err)
	}
	if recs, _ := g.Recommendations(); len(recs) != 0 {
		t.Errorf("stored: %+v", recs)
	}
}

// TestRecommendNeverAnswers: the director's recommendation is stored
// and journaled, and the member question stays open and unanswered: no
// answer item, nothing in its journal. A choice the question lacks, or
// no reason, refuses.
func TestRecommendNeverAnswers(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api := openChild(t, "acme-api")
	q := ask(t, api, "Which region?", false, "EU", "US")
	if _, _, err := g.Recommend(director, cfg, RecommendRequest{Question: "acme-api/" + q.ID, Choices: []string{"Asia"}, Why: "x"}); code(err) != "invalid-answer" {
		t.Errorf("unknown option: %v", err)
	}
	if _, _, err := g.Recommend(director, cfg, RecommendRequest{Question: "acme-api/" + q.ID, Choices: []string{"1"}}); code(err) != "invalid-recommendation" {
		t.Errorf("no why: %v", err)
	}
	key, r, err := g.Recommend(director, cfg, RecommendRequest{Question: q.ID + "@acme-api", Choices: []string{"1"}, Why: "acme-web runs in the EU\n(acme-web/T4)"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "acme-api/"+q.ID || !slices.Equal(r.Labels, []string{"EU"}) || r.Why != "acme-web runs in the EU (acme-web/T4)" || r.Sum != Fingerprint(q) {
		t.Fatalf("recommended %s: %+v", key, r)
	}
	items, _ := api.Inbox()
	if project.QuestionAnswered(items, q.ID) {
		t.Error("recommending answered the question")
	}
	if j := journal(t, &api.Folder); strings.Contains(j, "ask.answer") {
		t.Errorf("member journal: %s", j)
	}
	if j := journal(t, &g.Folder); !strings.Contains(j, `director:acme program.recommend acme-api/`+q.ID+` "EU" — acme-web runs in the EU`) {
		t.Errorf("program journal: %s", j)
	}
	// In words, on a question of the program's own.
	pq, err := g.AddQuestion(project.Question{Question: "Ship on Friday?", Options: []project.QuestionOption{{Label: "Yes"}, {Label: "No"}}})
	if err != nil {
		t.Fatal(err)
	}
	if key, r, err = g.Recommend(director, cfg, RecommendRequest{Question: pq.ID, Text: "Monday instead", Why: "the freeze starts Friday (PROGRAM.md)"}); err != nil || key != "acme/"+pq.ID || r.Text != "Monday instead" {
		t.Fatalf("own question: %s %+v %v", key, r, err)
	}
	if qs, _ := g.Questions(); len(qs) != 1 {
		t.Errorf("program question closed: %+v", qs)
	}
}

// TestAcceptAnswersAsUser: accepting answers the member question with
// the recommended option as the user's answer ("human (program acme,
// the director's recommendation)"), journaled in the program, and the
// recommendation is gone. The director can't accept it; a second
// accept finds none.
func TestAcceptAnswersAsUser(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api := openChild(t, "acme-api")
	q := ask(t, api, "Which region?", false, "EU", "US")
	ref := "acme-api/" + q.ID
	if _, _, err := g.Recommend(director, cfg, RecommendRequest{Question: ref, Choices: []string{"US"}, Why: "the customers are in the US (acme-web/T9)"}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Accept(director, cfg, ref); code(err) != "human-only" {
		t.Errorf("director accepts: %v", err)
	}
	res, err := g.Accept(human, cfg, ref)
	if err != nil {
		t.Fatal(err)
	}
	if res.Key != ref || res.Result != "answered" || res.Answer != `"US"` {
		t.Fatalf("accepted: %+v", res)
	}
	items, _ := api.Inbox()
	i := slices.IndexFunc(items, func(it project.Item) bool { return it.Kind == project.KindAnswer && it.Subject == q.ID })
	if i < 0 || !strings.Contains(items[i].Summary, `"US"`) {
		t.Fatalf("answer item: %+v", items)
	}
	if j := journal(t, &api.Folder); !strings.Contains(j, "human (program acme, the director's recommendation) ask.answer "+q.ID) {
		t.Errorf("member journal: %s", j)
	}
	if j := journal(t, &g.Folder); !strings.Contains(j, "program.recommend.accept "+ref) {
		t.Errorf("program journal: %s", j)
	}
	if recs, _ := g.Recommendations(); len(recs) != 0 {
		t.Errorf("left: %+v", recs)
	}
	if _, err := g.Accept(human, cfg, ref); code(err) != "no-recommendation" {
		t.Errorf("accept again: %v", err)
	}
}

// TestAcceptProgramQuestion: accepting on a program question answers it
// as the user's answer does: every covered member question gets it.
func TestAcceptProgramQuestion(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api, web := openChild(t, "acme-api"), openChild(t, "acme-web")
	qa := ask(t, api, "Which region do we deploy to?", false, "EU", "US")
	qw := ask(t, web, "Which region should we deploy to?", false, "us", "eu")
	pq, err := g.Merge(director, cfg, MergeRequest{Questions: []string{"acme-api/" + qa.ID, "acme-web/" + qw.ID}})
	if err != nil {
		t.Fatal(err)
	}
	// A covered member question takes no recommendation of its own.
	if _, _, err := g.Recommend(director, cfg, RecommendRequest{Question: "acme-api/" + qa.ID, Choices: []string{"EU"}, Why: "x"}); code(err) != "covered" {
		t.Errorf("covered: %v", err)
	}
	if _, _, err := g.Recommend(director, cfg, RecommendRequest{Question: "acme/" + pq.ID, Choices: []string{"EU"}, Why: "GDPR (PROGRAM.md)"}); err != nil {
		t.Fatal(err)
	}
	res, err := g.Accept(human, cfg, pq.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fanned) != 2 || res.Fanned[1].Answer != `"eu"` {
		t.Fatalf("fanned: %+v", res.Fanned)
	}
	if qs, _ := g.Questions(); len(qs) != 0 {
		t.Errorf("program question open: %+v", qs)
	}
}

// TestRecommendStale: a recommendation is dropped once its question
// changes (asked again with other options), is answered by the user
// another way, or closes; accepting a changed one refuses
// recommendation-stale and answers nothing.
func TestRecommendStale(t *testing.T) {
	g, cfg := routeFixture(t, routeConfig)
	api, web := openChild(t, "acme-api"), openChild(t, "acme-web")
	q := ask(t, api, "Which region?", false, "EU", "US")
	w := ask(t, web, "Which database?", false, "Postgres", "SQLite")
	for _, ref := range []string{"acme-api/" + q.ID, "acme-web/" + w.ID} {
		if _, _, err := g.Recommend(director, cfg, RecommendRequest{Question: ref, Choices: []string{"1"}, Why: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	// The coordinator asks Q1 again with another option: same id,
	// another question.
	ask(t, api, "Which region?", false, "EU", "US", "Asia")
	if _, err := g.Accept(human, cfg, "acme-api/"+q.ID); code(err) != "recommendation-stale" {
		t.Errorf("changed: %v", err)
	}
	if items, _ := api.Inbox(); project.QuestionAnswered(items, q.ID) {
		t.Error("a stale recommendation answered")
	}
	// The user answers acme-web's another way: pruned.
	if _, _, _, err := web.AnswerQuestion(human, w.ID, []string{"SQLite"}, ""); err != nil {
		t.Fatal(err)
	}
	dropped, err := g.Prune(cfg)
	if err != nil || !slices.Equal(dropped, []string{"acme-web/" + w.ID}) {
		t.Errorf("pruned %v %v", dropped, err)
	}
	if recs, _ := g.Recommendations(); len(recs) != 0 {
		t.Errorf("left: %+v", recs)
	}
	if j := journal(t, &g.Folder); !strings.Contains(j, "program.recommend.drop acme-web/"+w.ID) {
		t.Errorf("journal: %s", j)
	}
}
