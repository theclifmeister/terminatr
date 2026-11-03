package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/tasks"
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
	for _, want := range []string{"acme · needs you (1 of 3)", "[api+web] Q2 Session", "covers api/Q3, web/Q7", "( ) 1 hour", "submit the answers picked"} {
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

// TestNewProgram: G on the PROJECTS header, while programs are on, asks
// for a new program; its name and goal asked, it is made (NewProgram),
// its dashboard shows and its settings open for its members. + there
// is a new project only, P is pin only, and with programs off G does nothing new.
func TestNewProgram(t *testing.T) {
	data := Data{Projects: []ProjectData{{Slug: "api"}}, ProgramsOn: true}
	src := &fakeSource{data: data}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40})
	m.setData(data)
	m.focus, m.sideSel = areaSide, headKey
	drain(t, m, press(m, "+"))
	if in, ok := m.top().(*inputView); !ok || in.title != "New project" {
		t.Fatalf("+ on the header: %T", m.top())
	}
	m.pop()
	if bar := ansi.Strip(sideKeyBar(&treeRow{kind: treeHead, newProg: true}, true, false, 200)); !strings.Contains(bar, "G new program") {
		t.Errorf("key bar %q", bar)
	}
	drain(t, m, press(m, "P"))
	if in, ok := m.top().(*inputView); ok && in.title == "New program" {
		t.Fatalf("P on the header asked for a program")
	}
	drain(t, m, press(m, "G"))
	if in, ok := m.top().(*inputView); !ok || in.title != "New program" {
		t.Fatalf("G on the header: %T", m.top())
	}
	drain(t, m, typeLine(m, "acme"))
	if in, ok := m.top().(*inputView); !ok || !strings.Contains(in.title, "acme") {
		t.Fatalf("goal prompt %T", m.top())
	}
	drain(t, m, typeLine(m, "one login for every app"))
	if !slices.Equal(src.newPrograms, []string{"acme:one login for every app"}) {
		t.Fatalf("made %v", src.newPrograms)
	}
	if _, ok := m.top().(*programView); !ok || m.current != "g:acme" {
		t.Errorf("after: top %T, current %q", m.top(), m.current)
	}
	// Programs off: the new project's prompt.
	m.stack, m.data.ProgramsOn = nil, false
	m.focus, m.sideSel = areaSide, headKey
	drain(t, m, press(m, "G"))
	if in, ok := m.top().(*inputView); ok && in.title == "New program" {
		t.Errorf("programs off: G asked for a program")
	}
}

// TestServerNewProgram: the source makes the program folder as tm
// program new does, and lists it in config.toml with no members, so
// the sidebar shows it; a name taken refuses it.
func TestServerNewProgram(t *testing.T) {
	writeConfig(t, "[features]\nprograms = true\n")
	s := &ServerSource{}
	if slug, err := s.NewProgram("acme", "one login"); err != nil || slug != "acme" {
		t.Fatalf("new: %q %v", slug, err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Program("acme"); !ok {
		t.Error("not listed in config.toml")
	}
	if len(server.SidePrograms(cfg)) != 1 {
		t.Error("the sidebar doesn't list it")
	}
	if _, err := s.NewProgram("acme", ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("again: %v", err)
	}
	writeConfig(t, "")
	if _, err := s.NewProgram("beta", ""); err == nil {
		t.Error("made one while programs are off")
	}
}

// TestProgramPages: the dialog's pages are program.needs' rows: the
// program's questions (program, with the answer act) four to a page
// first, then each other need; a program row without the act is a
// page of its own, never answered here.
func TestProgramPages(t *testing.T) {
	q := &proto.Question{Questions: []proto.QuestionItem{{Question: "?", Options: []proto.QuestionOption{{Label: "a"}}}}}
	var needs []proto.ProgramNeed
	for i := 1; i <= 5; i++ {
		needs = append(needs, proto.ProgramNeed{PeerNeed: proto.PeerNeed{ID: fmt.Sprintf("ask:Q%d", i), Kind: proto.NeedAsk, Question: q},
			Project: "acme", Program: true, Acts: []string{proto.ActAnswer}})
	}
	needs = append(needs,
		proto.ProgramNeed{PeerNeed: proto.PeerNeed{ID: "ask:Q9", Kind: proto.NeedAsk, Question: q}, Project: "acme", Program: true},
		proto.ProgramNeed{PeerNeed: proto.PeerNeed{ID: "rv:T1", Kind: proto.NeedReview, Task: "T1"}, Project: "api"})
	pages := programPages(&proto.ProgramNeedsResult{Needs: needs})
	var got []string
	for _, p := range pages {
		if p.need != nil {
			got = append(got, p.need.ID)
			continue
		}
		var ids []string
		for _, n := range p.questions {
			ids = append(ids, questionID(n))
		}
		got = append(got, strings.Join(ids, "+"))
	}
	if want := []string{"Q1+Q2+Q3+Q4", "Q5", "ask:Q9", "rv:T1"}; !slices.Equal(got, want) {
		t.Errorf("pages %v, want %v", got, want)
	}
}

// TestProgramUmbrellas: the program dashboard's UMBRELLAS show each open
// umbrella with how far its parts are and its parts as a checklist
// (programs §5.3), and DEPENDENCIES each member task that waits on
// another's; enter on a part shows that task in its project, on an
// umbrella in review accepts it once the user says yes.
func TestProgramUmbrellas(t *testing.T) {
	m, src := progDash(t)
	g := &m.data.Programs[0]
	g.Umbrellas = []*tasks.Task{
		{ID: 4, Title: "SSO with Okta", Status: tasks.Started, Priority: tasks.Must, Parts: []string{"api/T12", "web/T40", "mini/ios/T7"}},
		{ID: 5, Title: "Bump axios", Status: tasks.Review, Parts: []string{"api/T14", "web/T41"}},
	}
	g.Deps = map[string]program.Dep{
		"api/T12":     {State: program.DepMet, Status: "review", Merged: true, Title: "OIDC login"},
		"web/T40":     {State: program.DepOpen, Status: "started", Title: "Login button"},
		"mini/ios/T7": {State: program.DepUnknown, Why: "on mini"},
		"api/T14":     {State: program.DepMet, Status: "done", Title: "axios api"},
		"web/T41":     {State: program.DepMet, Status: "done", Title: "axios web"},
	}
	g.Waits = []program.Wait{{Task: "web/T40", Title: "Login button", Status: "started", After: "api/T12"}}
	g.Tasks = map[string]*tasks.Task{"web/T40": {ID: 40, Title: "Login button", Status: tasks.Started}, "api/T12": {ID: 12, Title: "OIDC login", Status: tasks.Review}}
	src.data = m.data
	m.current = "g:acme"
	m.rebuild()
	out := screen(m)
	for _, want := range []string{"UMBRELLAS", "T4", "SSO with Okta", "1/3 parts · must", "[x] api/T12 OIDC login", "merged",
		"[ ] web/T40 Login button", "[ ] mini/ios/T7", "on mini", "T5", "2/2 parts · every part is in: enter accepts",
		"DEPENDENCIES", "web/T40 Login button", "← api/T12 (merged) · met"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "CHILDREN") > strings.Index(out, "UMBRELLAS") || strings.Index(out, "UMBRELLAS") > strings.Index(out, "DEPENDENCIES") {
		t.Error("panes out of order")
	}
	// A local part shows its task in its project; a paired one its
	// server's dashboard.
	m.focus, m.sel = areaMain, "pu:T4:api/T12"
	if r, _ := m.selected(); r.project != "api" || r.task == nil || r.task.ID != 12 {
		t.Fatalf("part row %+v", r)
	}
	m.sel = "pu:T4:mini/ios/T7"
	if r, _ := m.selected(); r.remote == "" || r.task != nil {
		t.Fatalf("paired part row %+v", r)
	}
	// Started: enter says what its status follows.
	m.sel = "pu:T4"
	drain(t, m, press(m, "enter"))
	if !strings.Contains(m.msg, "follows its parts") || len(src.programActs) != 0 {
		t.Fatalf("msg %q acts %v", m.msg, src.programActs)
	}
	m.sel = "pu:T5"
	drain(t, m, press(m, "enter"))
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatal("no confirm")
	}
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.programActs, []string{"acme accept T5"}) {
		t.Fatalf("acts %v", src.programActs)
	}
}

// TestProgramNeedsPaired: a paired member's need in the program's queue
// with acts (its server takes this one's at needs answer) is acted on
// from the dialog through the paired need dialog (peer.need-act, as the
// user's); one without acts (needs view) only shows its dashboard
// there.
func TestProgramNeedsPaired(t *testing.T) {
	m, src := progDash(t)
	res := src.pneeds["acme"]
	rv := proto.ProgramNeed{PeerNeed: proto.PeerNeed{ID: "review:T7:1", Kind: proto.NeedReview, Task: "T7", Title: "Login", Status: "review"},
		Project: "ios", Server: "mini", Acts: []string{proto.ActAccept, proto.ActSendBack}}
	res.Needs = append(res.Needs, rv)
	src.pneeds["acme"] = res
	drain(t, m, m.showProject("g:acme"))
	drain(t, m, m.openProgramNeeds("acme", rv.ID))
	if !strings.Contains(screen(m), "act on it here: it goes to mini as your answer") {
		t.Fatalf("paired page:\n%s", screen(m))
	}
	drain(t, m, press(m, "1"))
	if _, ok := m.top().(*peerNeedView); !ok {
		t.Fatalf("top %T", m.top())
	}
	drain(t, m, press(m, "1"))
	if _, ok := m.top().(*confirmView); ok {
		drain(t, m, press(m, "Y"))
	}
	if len(src.acts) != 1 || src.acts[0].Peer != "mini" || src.acts[0].Project != "ios" || src.acts[0].Act != proto.ActAccept || src.acts[0].Status != "review" {
		t.Errorf("acts %+v", src.acts)
	}

	m.pop()
	rv.Acts = nil
	res.Needs[len(res.Needs)-1] = rv
	src.pneeds["acme"] = res
	drain(t, m, m.openProgramNeeds("acme", rv.ID))
	if out := screen(m); strings.Contains(out, "act on it here") || !strings.Contains(out, "(answered there)") {
		t.Errorf("view page:\n%s", out)
	}
}

// TestProgramNeedsUmbrella: an umbrella in review is a row of the
// program's needs; its dialog page offers accept (asks first) and
// send-back (asks for a note).
func TestProgramNeedsUmbrella(t *testing.T) {
	n := proto.ProgramNeed{PeerNeed: proto.PeerNeed{ID: "umbrella:T5", Kind: proto.NeedUmbrella, Task: "T5", Title: "Bump axios"}, Project: "acme", Program: true}
	n.Acts = []string{proto.ActAccept, proto.ActSendBack}
	src := &fakeSource{}
	m := newDash(DashOptions{Source: src, Width: 120 + sideDefault, Height: 40})
	items := needActs(m, "acme", n)
	if len(items) != 2 || !strings.HasPrefix(items[0].label, "accept") || !strings.HasPrefix(items[1].label, "send back") {
		t.Fatalf("items %+v", items)
	}
	drain(t, m, items[0].run(m))
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatal("no confirm")
	}
	drain(t, m, press(m, "Y"))
	drain(t, m, items[1].run(m))
	for _, r := range "add web" {
		keyPress(m, string(r))
	}
	drain(t, m, keyPress(m, "enter"))
	if !slices.Equal(src.programActs, []string{"acme accept T5", "acme send-back T5 add web"}) {
		t.Fatalf("acts %v", src.programActs)
	}
}

// TestRecommendAccept (T982): a question the director recommends an
// answer to says so on its row; K accepts it at once, the user's own
// answer (AcceptRecommendation), and K on a row without one does
// nothing. The needs dialog lists the recommended option marked, and
// its accept item answers with it.
func TestRecommendAccept(t *testing.T) {
	m, src := progDash(t)
	res := src.pneeds["acme"]
	ask := proto.ProgramNeed{PeerNeed: proto.PeerNeed{ID: "ask:Q5", Kind: proto.NeedAsk, Role: proto.RoleCoordinator,
		Question: &proto.Question{Questions: []proto.QuestionItem{{Question: "Which region?", Options: []proto.QuestionOption{{Label: "EU"}, {Label: "US"}}}}}},
		Project: "web", Acts: []string{proto.ActQuestions}, Recommend: &proto.Recommendation{Labels: []string{"EU"}, Why: "api runs in the EU (api/T4)"}}
	res.Needs = append(res.Needs, ask)
	res.Needs[0].Recommend = &proto.Recommendation{Labels: []string{"8 hours"}, Why: "a workday (PROGRAM.md)"}
	src.pneeds["acme"] = res
	drain(t, m, m.showProject("g:acme"))
	if out := screen(m); !strings.Contains(out, `director recommends: "EU" — api runs in the EU (api/T4)`) || !strings.Contains(out, `director recommends: "8 hours"`) {
		t.Fatalf("row:\n%s", out)
	}
	m.focus, m.sel = areaMain, "pn:/api:rv:T12"
	drain(t, m, press(m, "K"))
	if len(src.programActs) != 0 {
		t.Fatalf("K without a recommendation: %v", src.programActs)
	}
	m.sel = "pn:/web:ask:Q5"
	drain(t, m, press(m, "K"))
	if !slices.Equal(src.programActs, []string{"acme accept-recommendation web/Q5"}) || !strings.Contains(m.msg, `answered web/Q5: "EU" (answered)`) {
		t.Fatalf("accepted %v, footer %q", src.programActs, m.msg)
	}
	// The dialog: the program question's option marked, and its
	// accept item after submit.
	m.sel = "pn:/acme:ask:Q2"
	drain(t, m, press(m, "enter"))
	v := m.top().(*programNeedsView)
	v.sel = len(v.items(m)) - 1 // in view: the dialog scrolls to it
	out := screen(m)
	for _, want := range []string{"8 hours (Recommended)", `accept the director's recommendation on Q2: "8 hours"`} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	items := v.items(m)
	drain(t, m, press(m, strconv.Itoa(len(items))))
	if !slices.Contains(src.programActs, "acme accept-recommendation acme/Q2") {
		t.Errorf("dialog accept: %v", src.programActs)
	}
}
