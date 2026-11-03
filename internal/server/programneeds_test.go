package server

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestRollUpRanks: the program's queue is ranked by kind (a stopped
// thread first, blocked last), then the task's priority, then age (an
// unknown one last), then the members' order; a member with nothing
// waiting is counted quiet.
func TestRollUpRanks(t *testing.T) {
	at := func(m int) time.Time { return time.Unix(1700000000, 0).Add(time.Duration(m) * time.Minute) }
	menu := &proto.Question{Questions: []proto.QuestionItem{{Question: "Which db?", Options: []proto.QuestionOption{{Label: "pg"}, {Label: "sqlite"}}}}}
	api := memberNeeds{project: "api", priority: map[string]tasks.Priority{"T1": tasks.Could, "T2": tasks.Must, "T5": tasks.Must}, needs: []proto.PeerNeed{
		{ID: "blocked:T5", Kind: proto.NeedBlocked, Task: "T5"},
		{ID: "review:T1:1", Kind: proto.NeedReview, Task: "T1", Status: "review"},
		{ID: "review:T2:1", Kind: proto.NeedReview, Task: "T2", Status: "review"},
		{ID: "question:s-1:x", Kind: proto.NeedQuestion, Session: "s-1", Thread: "t-0001", Question: menu, Since: at(5)},
	}}
	web := memberNeeds{project: "web", needs: []proto.PeerNeed{
		{ID: "approval:s-2", Kind: proto.NeedApproval, Session: "s-2", Since: at(9)},
		{ID: "question:s-3:y", Kind: proto.NeedQuestion, Session: "s-3", Escape: true, Since: at(20)},
		{ID: "question:s-4:z", Kind: proto.NeedQuestion, Session: "s-4", Question: menu, Since: at(1)},
		{ID: "ask:Q1", Kind: proto.NeedAsk, Role: proto.RoleCoordinator, Since: at(2)},
		{ID: "ci:T3", Kind: proto.NeedCI, Task: "T3"},
	}, links: []link.Need{{Kind: link.NeedHeld, Peer: "ops", ID: "M-4", Created: at(3), Title: "deploy?"}}, hostFails: 2}
	docs := memberNeeds{project: "docs"}
	ios := memberNeeds{project: "ios", server: "mini", count: 3}
	res := rollUp("acme", []memberNeeds{api, web, docs, ios}, nil)
	var got []string
	for _, n := range res.Needs {
		got = append(got, n.Project+" "+n.ID)
	}
	want := []string{
		"web approval:s-2",      // a stopped thread, the older
		"web question:s-3:y",    // an escape is a stopped thread too
		"web question:s-4:z",    // a thread's menu, the oldest
		"api question:s-1:x",    // …
		"api review:T2:1",       // review: must
		"api review:T1:1",       // could
		"web ask:Q1",            // the coordinator's question
		"web ci:T3",             // a red PR: no age
		"web host:web",          // the PR host
		"web link:held:ops:M-4", // a link's held message
		"api blocked:T5",        // blocked last, whatever its priority
	}
	if !slices.Equal(got, want) {
		t.Errorf("queue:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if res.Quiet != 1 {
		t.Errorf("quiet = %d, want 1 (docs; ios has 3 waiting on mini)", res.Quiet)
	}
	if len(res.Members) != 4 || res.Members[0].Needs != 4 || res.Members[1].Needs != 7 || res.Members[3].Count != 3 || res.Members[3].Server != "mini" {
		t.Errorf("members: %+v", res.Members)
	}
	for _, n := range res.Needs {
		switch n.ID {
		case "review:T2:1":
			if n.Priority != "must" || !slices.Equal(n.Acts, []string{proto.ActAccept, proto.ActSendBack}) {
				t.Errorf("review row: %+v", n)
			}
		case "question:s-3:y":
			if len(n.Acts) != 0 {
				t.Errorf("an escape offers acts: %v", n.Acts)
			}
		case "link:held:ops:M-4":
			if n.Peer != "ops" || n.Link != "M-4" || !slices.Equal(n.Acts, []string{proto.LinkApprove, proto.LinkDrop}) {
				t.Errorf("link row: %+v", n)
			}
		}
	}
}

// TestFoldNeeds: the same need seen twice folds into one row with both
// acts: a task in review with the coordinator's question about it, led
// by the better-ranked need and as old as the oldest; an escape, a
// permission dialog and a question about a task with no row of its own
// stay rows of their own.
func TestFoldNeeds(t *testing.T) {
	at := time.Unix(1700000000, 0)
	m := memberNeeds{project: "api", priority: map[string]tasks.Priority{"T4": tasks.Should}, needs: []proto.PeerNeed{
		{ID: "review:T4:2", Kind: proto.NeedReview, Task: "T4", Status: "review"},
		{ID: "ask:Q3", Kind: proto.NeedAsk, Task: "T4", Since: at},
		{ID: "approval:s-1", Kind: proto.NeedApproval, Task: "T4", Session: "s-1"},
		{ID: "question:s-2:k", Kind: proto.NeedQuestion, Task: "T4", Session: "s-2", Escape: true},
		{ID: "ask:Q4", Kind: proto.NeedAsk, Task: "T9"},
	}}
	rows := foldNeeds(m)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []string{"review:T4:2", "approval:s-1", "question:s-2:k", "ask:Q4"}) {
		t.Fatalf("rows: %v", ids)
	}
	r := rows[0]
	if len(r.Also) != 1 || r.Also[0].ID != "ask:Q3" || !r.Since.Equal(at) || r.Priority != "should" {
		t.Errorf("folded row: %+v", r)
	}
	if !slices.Equal(r.Acts, []string{proto.ActAccept, proto.ActSendBack, proto.ActQuestions}) {
		t.Errorf("folded acts: %v", r.Acts)
	}
	// A thread's question on the task leads the row: it ranks above
	// review.
	m.needs = []proto.PeerNeed{
		{ID: "ci:T4", Kind: proto.NeedCI, Task: "T4"},
		{ID: "question:T4:k", Kind: proto.NeedQuestion, Task: "T4", Text: "which port?"},
	}
	rows = foldNeeds(m)
	if len(rows) != 1 || rows[0].ID != "question:T4:k" || len(rows[0].Also) != 1 || rows[0].Also[0].ID != "ci:T4" {
		t.Errorf("led by the question: %+v", rows)
	}
	line := program.NeedLine(rows[0])
	if line != "should api T4 thread asks: which port? (also PR red)" {
		t.Errorf("line: %q", line)
	}
}

// TestProgramNeedsMethod: program.needs over the human's socket rolls
// up the members' boards and questions; a director reads only its own
// program's, other agents none; programs off refuses programs-off.
func TestProgramNeedsMethod(t *testing.T) {
	programFixture(t)
	p, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.New(project.Options{Slug: "docs"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Tasks().Add(human, []tasks.NewTask{
		{Title: "OIDC login", Status: "review", Priority: "must"},
		{Title: "Rate limits", Status: "blocked"},
	}); err != nil {
		t.Fatal(err)
	}
	opts := []project.QuestionOption{{Label: "1h"}, {Label: "8h"}}
	if _, err := p.AddQuestion(project.Question{Task: "T1", Question: "Session length?", Options: opts}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AddQuestion(project.Question{Question: "Which region?", Options: opts}); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"demo\", \"docs\", \"mini/ios\"]\n[programs.beta]\nprojects = [\"web\"]\n")
	cfg, _ := config.Load()
	g, err := program.Open("acme", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Tasks().Add(human, []tasks.NewTask{{Title: "SSO everywhere", Status: "ready"}, {Title: "Not in review", Status: "ready"}}); err != nil {
		t.Fatal(err)
	}
	// The part is done (met), so the ticker's derive keeps the umbrella in review.
	if _, err := p.Tasks().Add(human, []tasks.NewTask{{Title: "Shipped", Status: "done"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Tasks().SetParts(human, 1, []string{"demo/T3"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Tasks().SetDerived(caller.Caller{Kind: caller.Ticker}, 1, tasks.Review); err != nil {
		t.Fatal(err)
	}
	paths, err := client.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	startServer(t, paths)
	c, err := client.Dial(paths, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var res proto.ProgramNeedsResult
	if err := c.Call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: "acme"}, &res); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range res.Needs {
		got = append(got, n.Project+" "+n.Kind+" "+needRefOf(n))
	}
	if !slices.Equal(got, []string{"demo review T1", "acme umbrella-review T1", "demo ask Q2", "demo blocked T2"}) {
		t.Errorf("needs: %v", got)
	}
	if r := res.Needs[0]; len(r.Also) != 1 || r.Also[0].ID != "ask:Q1" || r.Priority != "must" || !slices.Contains(r.Acts, proto.ActQuestions) {
		t.Errorf("review row: %+v", r)
	}
	if u := res.Needs[1]; !u.Program || u.Task != "T1" || !slices.Equal(u.Acts, []string{proto.ActAccept, proto.ActSendBack}) || !strings.Contains(u.Text, "demo/T3") {
		t.Errorf("umbrella row: %+v", u)
	}
	if res.Quiet != 1 || len(res.Members) != 3 || res.Members[2].Server != "mini" || res.Members[2].Error == "" {
		t.Errorf("members: %+v quiet %d", res.Members, res.Quiet)
	}
	if err := c.Call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: "nope"}, &res); err == nil {
		t.Errorf("an unknown program answered")
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	var perr *proto.Error
	if err := c.Call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: "acme"}, &res); !errors.As(err, &perr) || perr.Code != "programs-off" {
		t.Errorf("off: %v", err)
	}
}

// TestProgramNeedsCallers: a director reads its own program's needs (no
// other), a coordinator or thread none.
func TestProgramNeedsCallers(t *testing.T) {
	programFixture(t)
	writeConfig(t, programsOn)
	s := &Server{}
	call := func(c caller.Caller, slug string) *proto.Error {
		_, perr := s.programNeeds(proto.ProgramNeedsParams{Program: slug}, callCtx{who: func() caller.Caller { return c }})
		return perr
	}
	dir := caller.Caller{Kind: caller.Director, Project: "acme", Session: "s-9"}
	if perr := call(dir, "beta"); perr == nil || perr.Code != "other-program" {
		t.Errorf("another program's: %v", perr)
	}
	for _, c := range []caller.Caller{{Kind: caller.Coordinator, Project: "demo"}, {Kind: caller.Thread, Project: "demo", Thread: "t-0001"}} {
		if perr := call(c, "acme"); perr == nil || perr.Code != "director-only" {
			t.Errorf("%s: %v", c.Kind, perr)
		}
	}
	if perr := call(caller.Caller{Kind: caller.Human}, ""); perr == nil || perr.Code != proto.ErrBadParams {
		t.Errorf("no program: %v", perr)
	}
}

// needRefOf is what a row is on, for the test's words.
func needRefOf(n proto.ProgramNeed) string {
	if n.Task != "" {
		return n.Task
	}
	return strings.TrimPrefix(n.ID, "ask:")
}

// TestPairedMemberNeeds: a paired member whose server shows this one
// its needs (view or answer) has them as rows in the queue, with acts
// at answer only; its coordinator's questions are flagged alike with a
// local member's (as Q4@mini/ios) and, once a program question covers
// one, have no row. At off it is a count.
func TestPairedMemberNeeds(t *testing.T) {
	opts := []proto.QuestionOption{{Label: "EU"}, {Label: "US"}}
	ask := proto.PeerNeed{ID: "ask:Q4", Kind: proto.NeedAsk, Role: proto.RoleCoordinator,
		Question: &proto.Question{Questions: []proto.QuestionItem{{Question: "Which region do we deploy to?", Options: opts}}}}
	review := proto.PeerNeed{ID: "review:T7:1", Kind: proto.NeedReview, Task: "T7", Status: "review", Title: "Login"}
	mirror := func(level string) []proto.PeerMirror {
		return []proto.PeerMirror{{Alias: "mini", NeedsCap: true, Needs: level, Projects: []proto.PeerMirrorProject{
			{PeerProject: proto.PeerProject{Project: "ios", NeedsYou: 2}, Needs: []proto.PeerNeed{ask, review}}}}}
	}
	local := memberNeeds{project: "api", questions: map[string]project.Question{
		"Q1": {ID: "Q1", Question: "Which region should we deploy to?", Options: []project.QuestionOption{{Label: "us"}, {Label: "eu"}}}}}
	local.needs = []proto.PeerNeed{{ID: "ask:Q1", Kind: proto.NeedAsk, Role: proto.RoleCoordinator}}

	res := rollUp("acme", []memberNeeds{local, pairedMember("mini", "ios", mirror(proto.NeedsAnswer))}, nil)
	byID := map[string]proto.ProgramNeed{}
	for _, n := range res.Needs {
		byID[n.Server+"/"+n.Project+" "+n.ID] = n
	}
	r, a := byID["mini/ios review:T7:1"], byID["mini/ios ask:Q4"]
	if !slices.Equal(r.Acts, []string{proto.ActAccept, proto.ActSendBack}) || !slices.Equal(a.Similar, []string{"Q1@api"}) {
		t.Errorf("answer: review %+v ask %+v", r, a)
	}
	if l := byID["/api ask:Q1"]; !slices.Equal(l.Similar, []string{"Q4@mini/ios"}) {
		t.Errorf("local ask: %+v", l)
	}
	if m := res.Members[1]; m.Server != "mini" || m.Needs != 2 || m.Count != 0 {
		t.Errorf("member: %+v", m)
	}

	view := pairedMember("mini", "ios", mirror(proto.NeedsView))
	res = rollUp("acme", []memberNeeds{view}, nil)
	if len(res.Needs) != 2 || slices.ContainsFunc(res.Needs, func(n proto.ProgramNeed) bool { return len(n.Acts) > 0 }) {
		t.Errorf("view: %+v", res.Needs)
	}
	covers := []project.Question{{ID: "Q1", Question: "Region?", Covers: []project.Cover{{Project: "mini/ios", Question: "Q4"}}}}
	res = rollUp("acme", []memberNeeds{view}, covers)
	if len(res.Needs) != 2 || !res.Needs[0].Program && !res.Needs[1].Program || slices.ContainsFunc(res.Needs, func(n proto.ProgramNeed) bool { return n.ID == "ask:Q4" }) {
		t.Errorf("covered: %+v", res.Needs)
	}

	off := pairedMember("mini", "ios", mirror(proto.NeedsOff))
	if res = rollUp("acme", []memberNeeds{off}, nil); len(res.Needs) != 0 || res.Members[0].Count != 2 {
		t.Errorf("off: %+v", res)
	}
}
