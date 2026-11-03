package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestProgramWakeRefusals: program.wake refuses every caller but the
// program's own director and the human here, and every member that
// isn't a local, active, unpaused one, before it starts anything.
func TestProgramWakeRefusals(t *testing.T) {
	s, _ := programFixture(t)
	for _, slug := range []string{"off", "held", "lone"} {
		if _, err := project.New(project.Options{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	const conf = "[programs.acme]\nprojects = [\"demo\", \"off\", \"held\", \"mini/ios\"]\n[programs.beta]\nprojects = [\"web\"]\n" +
		"[projects.off]\nactive = false\n[projects.held]\npaused = true\n"
	writeConfig(t, "[features]\nprograms = true\n"+conf)
	wake := func(c caller.Caller, prog, member string) *proto.Error {
		t.Helper()
		_, perr := s.programWake(proto.ProgramWakeParams{Program: prog, Project: member}, callCtx{who: func() caller.Caller { return c }})
		return perr
	}
	dir := caller.Caller{Kind: caller.Director, Project: "acme", Session: "s-9"}
	human := caller.Caller{Kind: caller.Human}
	for _, tc := range []struct {
		name         string
		c            caller.Caller
		prog, member string
		code         string
	}{
		{"another program", dir, "beta", "web", "other-program"},
		{"a coordinator", caller.Caller{Kind: caller.Coordinator, Project: "demo"}, "acme", "demo", "director-only"},
		{"a thread", caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001"}, "acme", "demo", "director-only"},
		{"a paired server's director", caller.ForPeerDirector("mini", "acme"), "acme", "demo", "director-only"},
		{"the human from a device", caller.Caller{Kind: caller.Human, Device: "phone"}, "acme", "demo", "director-only"},
		{"the human, no program", human, "", "demo", proto.ErrBadParams},
		{"no project", dir, "", "", proto.ErrBadParams},
		{"no such program", human, "nope", "demo", "unknown-program"},
		{"another program's member", dir, "", "web", "not-member"},
		{"in no program", dir, "", "lone", "not-member"},
		{"no such project", dir, "", "ghost", "not-member"},
		{"a paired member", dir, "", "mini/ios", "paired-member"},
		{"another server's project", dir, "", "mini/other", "not-member"},
		{"inactive", dir, "", "off", "project-inactive"},
		{"inactive, the human", human, "acme", "off", "project-inactive"},
		{"paused", dir, "", "held", "project-paused"},
	} {
		if perr := wake(tc.c, tc.prog, tc.member); perr == nil || perr.Code != tc.code {
			t.Errorf("%s: %v, want %s", tc.name, perr, tc.code)
		}
	}
	writeConfig(t, conf)
	if perr := wake(dir, "", "demo"); perr == nil || perr.Code != "programs-off" {
		t.Errorf("programs off: %v", perr)
	}
}

// TestProgramWake: program.wake starts a member's coordinator as the
// coordinator row does (role coordinator, the member's agent); once, the
// running one after; the start journaled in the member and the program
// as the caller. Over the socket the test is the human, who names the
// program (TestProgramWakeRefusals has the director's checks).
func TestProgramWake(t *testing.T) {
	p := testPaths(t)
	t.Setenv("PATH", nowhere+"/usr/bin:/bin")
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"fake\"\n[launch]\ncommand = " + strconv.Quote(testSh(t)) + "\nargs = [\"-c\", \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "fake.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := program.New(program.Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	writeConfig(t, "[features]\nprograms = true\n[programs.acme]\nprojects = [\"web\"]\n[projects.web]\ncoordinator_agent = \"fake\"\n")
	startServer(t, p)
	c, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var perr *proto.Error
	if err := c.Call(proto.MethodProgramWake, proto.ProgramWakeParams{Project: "web"}, nil); !errors.As(err, &perr) || perr.Code != proto.ErrBadParams {
		t.Errorf("the human without a program: %v", err)
	}
	var woke proto.ProgramOpenResult
	if err := c.Call(proto.MethodProgramWake, proto.ProgramWakeParams{Program: "acme", Project: "web", Cols: 100, Rows: 30}, &woke); err != nil || !woke.Started || woke.Session == "" {
		t.Fatalf("wake: %+v %v", woke, err)
	}
	var again proto.ProgramOpenResult
	if err := c.Call(proto.MethodProgramWake, proto.ProgramWakeParams{Program: "acme", Project: "web"}, &again); err != nil || again.Started || again.Session != woke.Session {
		t.Fatalf("wake again: %+v %v", again, err)
	}
	var list proto.SessionListResult
	call(t, c, proto.MethodSessionList, nil, &list)
	found := false
	for _, si := range list.Sessions {
		if si.ID == woke.Session {
			found = true
			if si.Role != proto.RoleCoordinator || si.Project != "web" || si.Agent != "fake" {
				t.Errorf("coordinator %+v", si)
			}
		}
	}
	if !found {
		t.Errorf("no session %s", woke.Session)
	}
	member, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	if want := " coordinator.wake " + woke.Session + " program acme"; strings.Count(string(member), want) != 1 {
		t.Errorf("member journal lacks one %q:\n%s", want, member)
	}
	prog, _ := os.ReadFile(g.Path("JOURNAL.md"))
	if want := " coordinator.wake web " + woke.Session; strings.Count(string(prog), want) != 1 {
		t.Errorf("program journal lacks one %q:\n%s", want, prog)
	}
}

// TestWakeKickoff: a woken coordinator's first prompt names the program
// and sends it to its inbox, not to greet a user who isn't there.
func TestWakeKickoff(t *testing.T) {
	k := fmt.Sprintf(WakeKickoff, "acme")
	if !strings.Contains(k, "Program acme's director") || !strings.Contains(k, "inbox") || strings.Contains(k, "%") {
		t.Errorf("kickoff: %s", k)
	}
}
