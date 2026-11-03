package server

import (
	"errors"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestRecommendRows: a question row carries the director's
// recommendation while its question is the one it read; a changed
// question's (other options) and other rows carry none.
func TestRecommendRows(t *testing.T) {
	opts := []project.QuestionOption{{Label: "Yes"}, {Label: "No"}}
	q1 := project.Question{ID: "Q1", Question: "Ship Friday?", Options: opts}
	q2 := project.Question{ID: "Q2", Question: "Rename?", Options: opts}
	own := project.Question{ID: "Q1", Question: "Freeze?", Options: opts}
	m := memberNeeds{project: "api", questions: map[string]project.Question{"Q1": q1, "Q2": q2}}
	m.needs = []proto.PeerNeed{{ID: "ask:Q1", Kind: proto.NeedAsk}, {ID: "ask:Q2", Kind: proto.NeedAsk}}
	res := rollUp("acme", []memberNeeds{m}, []project.Question{own})
	changed := q2
	changed.Options = append(slices.Clone(opts), project.QuestionOption{Label: "Later"})
	recs := map[string]program.Recommendation{
		"api/Q1":  {Labels: []string{"No"}, Why: "the freeze (PROGRAM.md)", Sum: program.Fingerprint(q1)},
		"api/Q2":  {Labels: []string{"Yes"}, Why: "x", Sum: program.Fingerprint(changed)},
		"acme/Q1": {Text: "after the release", Why: "y", Sum: program.Fingerprint(own)},
	}
	recommend("acme", res.Needs, []memberNeeds{m}, []project.Question{own}, recs)
	got := map[string]*proto.Recommendation{}
	for _, n := range res.Needs {
		got[proto.RecommendKey("acme", n)] = n.Recommend
	}
	if r := got["api/Q1"]; r == nil || !slices.Equal(r.Labels, []string{"No"}) || r.Why != "the freeze (PROGRAM.md)" {
		t.Errorf("api/Q1: %+v", r)
	}
	if r := got["api/Q2"]; r != nil {
		t.Errorf("stale api/Q2 shown: %+v", r)
	}
	if r := got["acme/Q1"]; r == nil || r.Text != "after the release" {
		t.Errorf("program question: %+v", r)
	}
}

// TestRecommendAccept: program.recommend.accept answers as the user
// (the member's answer item), refused to every agent, the director
// included, and to a paired server's user; program.needs shows the
// recommendation until then, and none after.
func TestRecommendAccept(t *testing.T) {
	p := testPaths(t)
	demo := newWatchProject(t)
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\"]\n")
	q, err := demo.AddQuestion(project.Question{Question: "Which region?", Options: []project.QuestionOption{{Label: "EU"}, {Label: "US"}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	g, err := program.Open("acme", cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := caller.Caller{Kind: caller.Director, Project: "acme", Session: "s-9"}
	if _, _, err := g.Recommend(dir, cfg, program.RecommendRequest{Question: "demo/" + q.ID, Choices: []string{"EU"}, Why: "web is in the EU (web/T2)"}); err != nil {
		t.Fatal(err)
	}
	got := make(chan *Server, 1)
	onRun = func(s *Server) { got <- s }
	t.Cleanup(func() { onRun = nil })
	startServer(t, p)
	s := <-got

	res, perr := s.rolledUpNeeds("acme")
	if perr != nil {
		t.Fatal(perr)
	}
	i := slices.IndexFunc(res.Needs, func(n proto.ProgramNeed) bool { return n.ID == "ask:"+q.ID })
	if i < 0 || res.Needs[i].Recommend == nil || res.Needs[i].Recommend.Labels[0] != "EU" {
		t.Fatalf("needs: %+v", res.Needs)
	}
	params := proto.ProgramRecommendAcceptParams{Program: "acme", Question: "demo/" + q.ID}
	for _, c := range []caller.Caller{
		dir,
		{Kind: caller.Coordinator, Project: "demo"},
		caller.ForPeerDirector("mini", "acme"),
		caller.ForPeerHuman("mini", ""),
	} {
		if _, perr := s.programRecommendAccept(params, callCtx{who: func() caller.Caller { return c }}); perr == nil || perr.Code != "human-only" {
			t.Errorf("%s accepted: %v", c, perr)
		}
	}
	if items, _ := demo.Inbox(); project.QuestionAnswered(items, q.ID) {
		t.Fatal("a refused accept answered")
	}
	out, perr := s.programRecommendAccept(params, callCtx{who: func() caller.Caller { return human }})
	if perr != nil {
		t.Fatal(perr)
	}
	if r := out.(program.AcceptResult); r.Result != "answered" || r.Answer != `"EU"` {
		t.Errorf("accepted: %+v", r)
	}
	if items, _ := demo.Inbox(); !project.QuestionAnswered(items, q.ID) {
		t.Error("not answered")
	}
	res, _ = s.rolledUpNeeds("acme")
	for _, n := range res.Needs {
		if n.Recommend != nil {
			t.Errorf("still recommended: %+v", n)
		}
	}
	_, perr = s.programRecommendAccept(params, callCtx{who: func() caller.Caller { return human }})
	var pe *proto.Error
	if !errors.As(error(perr), &pe) || pe.Code != "no-recommendation" {
		t.Errorf("accept again: %v", perr)
	}
}

// TestRecommendedFirst: the /answer dialog lists the director's
// recommended options first, the rest in their order.
func TestRecommendedFirst(t *testing.T) {
	q := project.Question{Options: []project.QuestionOption{{Label: "A"}, {Label: "B"}, {Label: "C"}}}
	var got []string
	for _, o := range recommendedFirst(q, []string{"C"}).Options {
		got = append(got, o.Label)
	}
	if !slices.Equal(got, []string{"C", "A", "B"}) {
		t.Errorf("order: %v", got)
	}
	if len(recommendedFirst(q, nil).Options) != 3 {
		t.Error("none recommended")
	}
}
