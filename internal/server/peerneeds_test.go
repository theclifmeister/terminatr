package server

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// TestPeerNeeds: a shared project's needs in full: a session's menu
// with its text, a permission dialog as the screen shows it, an
// escape's dialog bare (its screen never read), the coordinator's open
// questions, a task in review, and a thread's question in words only
// where its session's menu doesn't ask already.
func TestPeerNeeds(t *testing.T) {
	since := time.Unix(1700000000, 0)
	menu := &proto.Question{Since: since, Questions: []proto.QuestionItem{{Question: "Which cache?", Options: []proto.QuestionOption{{Label: "Redis"}}}}}
	sessions := []proto.SessionInfo{
		{ID: "s-1", Role: proto.RoleThread, Project: "web", Thread: "t-0001", Task: "T1", Question: menu},
		{ID: "s-2", Role: proto.RoleThread, Project: "web", Thread: "t-0002", Task: "T2", State: "blocked", Reason: "permission"},
		{ID: "s-3", Role: proto.RoleCoordinator, Project: "web", State: "blocked", Reason: "permission", Escape: true},
		{ID: "s-4", Role: proto.RoleThread, Project: "web", Thread: "t-0004", Question: &proto.Question{Escape: true,
			Questions: []proto.QuestionItem{{Question: "Run outside the sandbox?", Options: []proto.QuestionOption{{Label: "Yes"}}}}}},
		{ID: "s-5", Role: proto.RoleThread, Project: "api", State: "blocked", Reason: "permission"},
		{ID: "mini/s-6", Server: "mini", Project: "web", State: "blocked", Reason: "permission"},
	}
	w := proto.ProjectWatch{
		Threads: []proto.WatchThread{{ID: "t-0001", Title: "thread one", Task: &proto.WatchTask{ID: "T1", Title: "Cache layer"}}, {ID: "t-0003", Reports: 2}},
		NeedsYou: []proto.WatchNeed{
			{Why: proto.WhyQuestion, Thread: "t-0001", Task: "T1", Question: "Which cache?"},
			{Why: proto.WhyQuestion, Thread: "t-0003", Task: "T3", Question: "Ship on Friday?"},
			{Why: proto.WhyReview, Thread: "t-0003", Task: "T4", Title: "Docs", Status: "review", PR: "open", PRNumber: 9},
			{Why: proto.WhyBlocked, Task: "T5", Status: "blocked"},
		},
	}
	asks := []project.Question{{ID: "Q2", Header: "Scope", Question: "Both apps?", Options: []project.QuestionOption{{Label: "Yes"}, {Label: "No"}}, Asked: since}}
	var read []string
	approval := func(id string) *proto.PeerApproval {
		read = append(read, id)
		return &proto.PeerApproval{Question: "Allow curl?", Options: []string{"Yes", "Yes, and don't ask again", "No"}, Scopes: []string{"once", "persistent", "once"}}
	}
	ns := peerNeeds("web", w, sessions, asks, approval)
	byID := map[string]proto.PeerNeed{}
	var kinds []string
	for _, n := range ns {
		byID[n.ID] = n
		kinds = append(kinds, n.Kind)
	}
	want := []string{proto.NeedQuestion, proto.NeedApproval, proto.NeedApproval, proto.NeedQuestion, proto.NeedAsk, proto.NeedQuestion, proto.NeedReview, proto.NeedBlocked}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v, want %v: %+v", kinds, want, ns)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v, want %v", kinds, want)
		}
	}
	if q := ns[0]; q.Question == nil || q.Question.Questions[0].Question != "Which cache?" || q.Title != "Cache layer" || q.Task != "T1" || !q.Since.Equal(since) {
		t.Errorf("menu: %+v", q)
	}
	if a := ns[1]; a.Approval == nil || len(a.Approval.Options) != 3 || a.Session != "s-2" || a.ID == "approval:s-2" {
		t.Errorf("dialog: %+v", a)
	}
	if e := ns[2]; !e.Escape || e.Approval != nil || e.Role != proto.RoleCoordinator {
		t.Errorf("escape dialog: %+v", e)
	}
	if e := ns[3]; !e.Escape || e.Question != nil {
		t.Errorf("escape menu: %+v", e)
	}
	if len(read) != 1 || read[0] != "s-2" {
		t.Errorf("screens read: %v, want s-2's alone", read)
	}
	if a := byID["ask:Q2"]; a.Question == nil || a.Question.Questions[0].Header != "Scope" || len(a.Question.Questions[0].Options) != 2 {
		t.Errorf("ask: %+v", a)
	}
	if q := ns[5]; q.Text != "Ship on Friday?" || q.Thread != "t-0003" {
		t.Errorf("thread question: %+v", q)
	}
	if r := ns[6]; r.ID != "review:T4:2" || r.PRNumber != 9 || r.Reports != 2 {
		t.Errorf("review: %+v", r)
	}
	// A new dialog on the same session is a new need.
	ns2 := peerNeeds("web", w, sessions, asks, func(string) *proto.PeerApproval {
		return &proto.PeerApproval{Question: "Allow rm?", Options: []string{"Yes", "No"}}
	})
	if ns2[1].ID == ns[1].ID {
		t.Errorf("another dialog kept the id %s", ns[1].ID)
	}
}

// TestPeerActChecks: an act on a need runs only for a paired server's
// user, on a need the shared project still has, never an escape's, of
// the kind the need takes, with its guard holding as the paired server
// showed it (one line, cut), and then with this server's own words.
func TestPeerActChecks(t *testing.T) {
	testPaths(t)
	for _, slug := range []string{"web", "api"} {
		if _, err := project.New(project.Options{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.SetProject("web", "share", true); err != nil {
		t.Fatal(err)
	}
	label := "Redis, with a long label " + strings.Repeat("x", 300)
	menu := &proto.Question{Since: time.Unix(1700000000, 0), Questions: []proto.QuestionItem{
		{Question: "Which\tcache?", Options: []proto.QuestionOption{{Label: label}, {Label: "None"}}}}}
	sessions := []proto.SessionInfo{
		{ID: "s-1", Role: proto.RoleThread, Project: "web", Thread: "t-0001", Question: menu},
		{ID: "s-2", Role: proto.RoleThread, Project: "web", Thread: "t-0002", State: "blocked", Reason: "permission"},
		{ID: "s-3", Role: proto.RoleThread, Project: "web", Thread: "t-0003", State: "blocked", Reason: "permission", Escape: true},
		{ID: "s-4", Role: proto.RoleThread, Project: "api", State: "blocked", Reason: "permission"},
	}
	var ran []proto.PeerAct
	ex := peerExport{
		sessions: func() []proto.SessionInfo { return sessions },
		state: func(string, []proto.SessionInfo) (proto.ProjectWatch, error) {
			return proto.ProjectWatch{NeedsYou: []proto.WatchNeed{{Why: proto.WhyReview, Task: "T4", Status: "review"}}}, nil
		},
		approval: func(string) *proto.PeerApproval {
			return &proto.PeerApproval{Question: "Allow curl?", Options: []string{"Yes", "No"}, Scopes: []string{"once", "once"}}
		},
		act: func(c caller.Caller, n proto.PeerNeed, p proto.PeerAct) (proto.PeerActResult, *proto.Error) {
			ran = append(ran, p)
			return proto.PeerActResult{Asked: true}, nil
		},
	}
	ns, err := ex.needsOf("web")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range ns {
		ids[n.Kind+map[bool]string{true: ":escape"}[n.Escape]] = n.ID
	}
	user := caller.ForPeerHuman("studio", "")
	shown := func(s string) string { return textsafe.Line(s, 200) } // as the paired server's mirror has it
	cases := []struct {
		name string
		c    caller.Caller
		p    proto.PeerAct
		code string
	}{
		{"the paired server itself", caller.ForPeer("studio"), proto.PeerAct{Project: "web", Need: ids[proto.NeedReview], Act: proto.ActAccept, Status: "review"}, proto.ErrRefused},
		{"a project not shared", user, proto.PeerAct{Project: "api", Need: "approval:s-4", Act: proto.ActApprove}, proto.ErrNotShared},
		{"a need gone", user, proto.PeerAct{Project: "web", Need: "review:T4:9", Act: proto.ActAccept, Status: "review"}, proto.ErrNeedChanged},
		{"an escape", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedApproval+":escape"], Act: proto.ActApprove, Choice: 1, Option: "Yes"}, proto.ErrEscapeHere},
		{"another kind's act", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedReview], Act: proto.ActApprove, Choice: 1}, proto.ErrBadParams},
		{"a stale status", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedReview], Act: proto.ActAccept, Status: "in-progress"}, proto.ErrNeedChanged},
		{"no status", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedReview], Act: proto.ActSendBack, Note: "why"}, proto.ErrNeedChanged},
		{"another option", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedApproval], Act: proto.ActApprove, Choice: 1, Option: "No"}, proto.ErrNeedChanged},
		{"an option it hasn't", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedApproval], Act: proto.ActApprove, Choice: 3, Option: "No"}, proto.ErrBadParams},
		{"another question", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedQuestion], Act: proto.ActAnswer, Question: "Which db?", Answer: "None"}, proto.ErrNeedChanged},
		{"an answer that isn't an option", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedQuestion], Act: proto.ActAnswer, Question: "Which cache?", Answer: "Memcached"}, proto.ErrBadParams},
		{"answer", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedQuestion], Act: proto.ActAnswer, Question: shown("Which\tcache?"), Answer: shown(label)}, ""},
		{"approve", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedApproval], Act: proto.ActApprove, Choice: 2, Option: "No"}, ""},
		{"accept", user, proto.PeerAct{Project: "web", Need: ids[proto.NeedReview], Act: proto.ActAccept, Status: "review"}, ""},
	}
	for _, tc := range cases {
		res, err := ex.Act(tc.c, tc.p)
		var pe *proto.Error
		errors.As(err, &pe)
		switch {
		case tc.code == "" && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case tc.code == "" && (res.Act != tc.p.Act || !res.Asked):
			t.Errorf("%s: %+v", tc.name, res)
		case tc.code != "" && (pe == nil || pe.Code != tc.code):
			t.Errorf("%s: %v, want %s", tc.name, err, tc.code)
		}
	}
	if len(ran) != 3 {
		t.Fatalf("ran %+v", ran)
	}
	if ran[0].Question != "Which\tcache?" || ran[0].Answer != label {
		t.Errorf("the answer went in the paired server's words: %+v", ran[0])
	}
}
