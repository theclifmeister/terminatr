package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// progDash is a dashboard over program acme (members api, web, and
// mini/ios and mini/mac on a paired server), its needs: a program
// question covering api's and web's, a task in review in api, a
// thread's question menu in web.
func progDash(t *testing.T) (*dash, *fakeSource) {
	t.Helper()
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"api\", \"web\", \"mini/ios\", \"mini/mac\"]\n")
	q := &proto.Question{Questions: []proto.QuestionItem{{Question: "How long should a session last?", Header: "Session",
		Options: []proto.QuestionOption{{Label: "1 hour"}, {Label: "8 hours"}}}}}
	menu := &proto.Question{Questions: []proto.QuestionItem{{Question: "Use the Keychain?", Options: []proto.QuestionOption{{Label: "yes"}}}}}
	res := proto.ProgramNeedsResult{Program: "acme", Needs: []proto.ProgramNeed{
		{PeerNeed: proto.PeerNeed{ID: "ask:Q2", Kind: proto.NeedAsk, Question: q}, Project: "acme", Program: true, Merged: []string{"api/Q3", "web/Q7"}, Acts: []string{proto.ActAnswer}},
		{PeerNeed: proto.PeerNeed{ID: "rv:T12", Kind: proto.NeedReview, Task: "T12", Title: "OIDC login", Status: "review"}, Project: "api", Priority: "must", Acts: []string{proto.ActAccept, proto.ActSendBack}},
		{PeerNeed: proto.PeerNeed{ID: "q:s-7", Kind: proto.NeedQuestion, Session: "s-7", Thread: "t-0041", Question: menu}, Project: "web", Acts: []string{proto.ActAnswer}},
	}, Members: []proto.NeedMember{{Project: "api", Needs: 1}, {Project: "web", Needs: 1}, {Project: "ios", Server: "mini", Count: 2}}}
	working := proto.SessionInfo{ID: "s-7", Role: proto.RoleThread, Project: "web", Thread: "t-0041", State: "working"}
	data := Data{
		Projects: []ProjectData{
			{Slug: "api", Threads: []ThreadRow{{Record: &thread.Record{ID: "t-1"}, Checks: "fail"}}},
			{Slug: "web", Threads: []ThreadRow{{Record: &thread.Record{ID: "t-0041", Session: "s-7"}}}},
		},
		Sessions: []proto.SessionInfo{working, {ID: "s-c", Role: proto.RoleCoordinator, Project: "api", State: "idle"}},
		Programs: []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Authority: "propose", ParallelThreads: 6,
			Projects: []string{"api", "web", "mini/ios", "mini/mac"}},
			Invites: map[string]link.Invite{"mini/mac": {ID: "P-1", Out: true, Peer: "mini/mac", State: link.ReqWaiting}}}},
		Peers: []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Projects: []proto.PeerMirrorProject{{PeerProject: proto.PeerProject{Project: "ios"}}}}},
	}
	src := &fakeSource{data: data, pneeds: map[string]proto.ProgramNeedsResult{"acme": res}}
	m := newDash(DashOptions{Source: src, Width: 140 + sideDefault, Height: 40})
	m.layout.Details = false
	m.setData(data)
	return m, src
}

// TestProgramDashboard: enter on a program's row shows its dashboard:
// its header, NEEDS YOU as tm's server ranks it (each need with its
// member, a program question with the members it covers), then
// CHILDREN in the program's order, a paired member invited or dormant.
func TestProgramDashboard(t *testing.T) {
	m, _ := progDash(t)
	m.focus, m.sideSel = areaSide, "g:acme"
	drain(t, m, press(m, "enter"))
	if m.current != "g:acme" {
		t.Fatalf("current %q", m.current)
	}
	out := screen(m)
	for _, want := range []string{"acme · program · propose · cap 6", "NEEDS YOU", "api+web", "Q2 How long should a session last?",
		"T12 OIDC login", "must · in review", "t-0041 Use the Keychain?", "CHILDREN",
		"1 thread · 1 needs you · 1 PR red", "1 working", "mini/ios", "dormant on mini", "mini/mac", "invited"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "NEEDS YOU") > strings.Index(out, "CHILDREN") {
		t.Error("CHILDREN above NEEDS YOU")
	}
	var keys []string
	for _, r := range m.rows {
		if r.selectable() {
			keys = append(keys, r.key)
		}
	}
	if want := []string{"pn:/acme:ask:Q2", "pn:/api:rv:T12", "pn:/web:q:s-7", "pc:api", "pc:web", "pc:mini/ios", "pc:mini/mac"}; !slices.Equal(keys, want) {
		t.Errorf("rows %v", keys)
	}
	// The sidebar's cursor rests on the program's row: it is the one
	// you are on.
	if hereKey(m.tree()) != "g:acme" {
		t.Errorf("here %q", hereKey(m.tree()))
	}
	// Enter on a child shows its dashboard; on a paired one its
	// read-only one.
	m.focus, m.sel = areaMain, "pc:web"
	drain(t, m, press(m, "enter"))
	if m.current != "web" {
		t.Errorf("child: current %q", m.current)
	}
	drain(t, m, m.showProject("g:acme"))
	m.sel = "pc:mini/ios"
	drain(t, m, press(m, "enter"))
	if m.current != "@mini/ios" {
		t.Errorf("paired child: current %q", m.current)
	}
}

// TestProgramNeedsDialog: enter on a program question opens the needs
// dialog on its page; an option picked and submitted asks first, then
// goes as the user's answer (AnswerProgram). The next page is the task
// in review: accept asks, then asks api's coordinator.
func TestProgramNeedsDialog(t *testing.T) {
	m, src := progDash(t)
	drain(t, m, m.showProject("g:acme"))
	m.focus, m.sel = areaMain, "pn:/acme:ask:Q2"
	drain(t, m, press(m, "enter"))
	v, ok := m.top().(*programNeedsView)
	if !ok {
		t.Fatalf("top %T", m.top())
	}
	out := screen(m)
	for _, want := range []string{"acme · needs you (1 of 3)", "[api+web] Q2 Session", "( ) 1 hour", "submit the answers picked"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	drain(t, m, press(m, "2"))
	if !strings.Contains(screen(m), "(x) 8 hours") {
		t.Errorf("not picked:\n%s", screen(m))
	}
	v.sel = len(v.items(m)) - 1
	drain(t, m, press(m, "enter"))
	if _, ok := m.top().(*confirmView); !ok || len(src.panswers) != 0 {
		t.Fatalf("didn't ask first: %T %v", m.top(), src.panswers)
	}
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.panswers, []string{"acme/Q2=8 hours"}) {
		t.Fatalf("answers %v", src.panswers)
	}
	if !strings.Contains(m.msg, "Q2 → api/Q1 answered") {
		t.Errorf("footer %q", m.msg)
	}
	// The review row: its page offers accept and send back.
	m.sel = "pn:/api:rv:T12"
	drain(t, m, press(m, "enter"))
	v = m.top().(*programNeedsView)
	if v.page != 1 || !strings.Contains(screen(m), "[api] T12 in review · must") {
		t.Fatalf("page %d:\n%s", v.page, screen(m))
	}
	drain(t, m, press(m, "1"))
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("accept didn't ask: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.asked, []string{"accept api T12"}) {
		t.Errorf("asked %v", src.asked)
	}
	// → turns to the thread's menu: its act opens the pane.
	drain(t, m, m.openProgramNeeds("acme", "rv:T12"))
	drain(t, m, press(m, "right"))
	if !strings.Contains(screen(m), "open its pane to answer its question") {
		t.Errorf("menu page:\n%s", screen(m))
	}
}
