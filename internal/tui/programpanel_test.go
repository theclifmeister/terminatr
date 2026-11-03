package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestDirectorPanel: beside a director the panel shows its program: the
// band with what waits in it (a click opens the needs dialog), the
// rolled-up NEEDS YOU, then CHILDREN with each coordinator's state.
// tm's server not answering says so.
func TestDirectorPanel(t *testing.T) {
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"api\", \"mini/ios\"]\nparallel_threads = 4\n")
	res := proto.ProgramNeedsResult{Program: "acme", Needs: []proto.ProgramNeed{
		{PeerNeed: proto.PeerNeed{ID: "rv:T12", Kind: proto.NeedReview, Task: "T12", Title: "OIDC login"}, Project: "api", Acts: []string{proto.ActAccept}},
	}, Members: []proto.NeedMember{{Project: "api", Needs: 1}, {Project: "ios", Server: "mini", Count: 2}}}
	was := readProgramNeeds
	t.Cleanup(func() { readProgramNeeds = was })
	readProgramNeeds = func(tmclient.Paths, string) (proto.ProgramNeedsResult, error) { return res, nil }
	dir := proto.SessionInfo{ID: "s-d", Role: proto.RoleDirector, Project: "acme"}
	coord := proto.SessionInfo{ID: "s-c", Role: proto.RoleCoordinator, Project: "api", State: "working"}
	d := loadInfo(tmclient.Paths{}, dir, []proto.SessionInfo{dir, coord})
	if d == nil || d.prog == nil {
		t.Fatalf("no program info: %+v", d)
	}
	lines, hits := infoLines(d, 60, time.Now())
	out := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"acme · program", "propose · cap 4 · 2 members", "3 waiting in acme · A or click answers",
		"api T12 in review: OIDC login", "mini/ios: 2 waiting on mini", "CHILDREN", "api · coordinator", "mini/ios · on mini"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	band := -1
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "waiting in acme") {
			band = i
		}
	}
	if band < 0 || hits[band].kind != hitProgramNeeds {
		t.Errorf("band %d hits %+v", band, hits)
	}
	readProgramNeeds = func(tmclient.Paths, string) (proto.ProgramNeedsResult, error) {
		return proto.ProgramNeedsResult{}, &proto.Error{Message: "programs are off"}
	}
	d = loadInfo(tmclient.Paths{}, dir, nil)
	lines, _ = infoLines(d, 60, time.Now())
	if out := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(out, "isn't known now: programs are off") {
		t.Errorf("no answer:\n%s", out)
	}
}

// TestProgramNeedsOver: the band's A, over a director's pane, opens the
// dashboard with the program needs dialog for its program.
func TestProgramNeedsOver(t *testing.T) {
	m, _ := progDash(t)
	m.over, m.then, m.loaded = &Over{Key: thenProgramNeeds, Project: "acme", Session: "s-d", Screen: []string{"", "row"}}, thenProgramNeeds, false
	feed(m, m.setData(m.data))
	v, ok := m.top().(*programNeedsView)
	if !ok || v.slug != "acme" || !v.loaded || v.page != 0 {
		t.Fatalf("top %T %+v", m.top(), m.top())
	}
}

// TestOfferNeeds: a director's pane asked to open its program's needs
// dialog (tm program needs --open) opens it once; an old request, a
// coordinator's pane or an open menu doesn't.
func TestOfferNeeds(t *testing.T) {
	now := time.Now()
	dir := proto.SessionInfo{ID: "s-d", Role: proto.RoleDirector, Project: "acme", NeedsOpen: now}
	c := &client{focus: &pane{info: dir}}
	if got := c.offerNeeds(); got != "acme" {
		t.Fatalf("offer %q", got)
	}
	if got := c.offerNeeds(); got != "" {
		t.Errorf("again: %q", got)
	}
	c.focus.info.NeedsOpen = now.Add(time.Second)
	c.menu = &amenu{}
	if got := c.offerNeeds(); got != "" {
		t.Errorf("over a menu: %q", got)
	}
	c.menu = nil
	if got := c.offerNeeds(); got != "acme" {
		t.Errorf("a new request: %q", got)
	}
	old := &client{focus: &pane{info: proto.SessionInfo{ID: "s-d", Role: proto.RoleDirector, Project: "acme", NeedsOpen: now.Add(-time.Hour)}}}
	coord := &client{focus: &pane{info: proto.SessionInfo{ID: "s-c", Role: proto.RoleCoordinator, Project: "api", NeedsOpen: now}}}
	if old.offerNeeds() != "" || coord.offerNeeds() != "" {
		t.Error("an old request, or a coordinator's pane, opened it")
	}
}
