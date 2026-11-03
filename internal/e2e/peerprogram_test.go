package e2e

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// invitesIn reads env's invites of folder slug, in this process.
func invitesIn(t *testing.T, env *Env, slug string) []link.Invite {
	t.Helper()
	t.Setenv(home.Env, env.Home)
	return link.Invites(slug)
}

// TestSmokePeerProgramInvite (B14): studio and mini, paired over a relay
// on 127.0.0.1, with programs on. Each lists the hello cap program; a
// program on studio invites mini's api (as its settings popup does on
// saving the members), mini's user accepts it with tm program accept,
// which writes api's program keys, and studio hears the features
// granted; api leaves with tm program leave, and studio sees it left.
// With mini's programs off, its server refuses the next invite.
func TestSmokePeerProgramInvite(t *testing.T) {
	studio, mini := New(t), New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))

	studio.MustCLI("project", "new", "acme")
	mini.MustCLI("project", "new", "api")
	writeConfig(t, studio, "[features]\nprograms = true\n[programs.apps]\nprojects = [\"acme\", \"mini/api\"]\n")
	writeConfig(t, mini, "[features]\nprograms = true\n")
	studio.MustCLI("program", "new", "apps")
	studio.MustCLI("server", "start")
	mini.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	waitPeerState(t, mini, "studio", "up", "")
	if !Poll(30*time.Second, func() bool { pm, _ := mirrored(t, studio, "mini"); return pm.Program }) {
		t.Fatal("mini's hello never listed program")
	}

	t.Setenv(home.Env, studio.Home)
	g, err := program.Open("apps", nil)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := link.InviteMember(caller.Caller{Kind: caller.Human}, &g.Folder, "mini/api")
	if err != nil {
		t.Fatal(err)
	}
	if !Poll(30*time.Second, func() bool { rs := invitesIn(t, mini, "api"); return len(rs) == 1 && rs[0].ID == inv.ID }) {
		t.Fatalf("the invite never reached mini: %+v", invitesIn(t, studio, "apps"))
	}
	if r := mini.CLI("program", "accept", inv.ID, "--project", "api", "--tasks", "sideways"); r.Code == 0 {
		t.Fatalf("a bad --tasks: %+v", r)
	}
	if out := mini.MustCLI("program", "accept", inv.ID, "--project", "api"); !strings.Contains(out, "accepted "+inv.ID) {
		t.Fatalf("accept: %s", out)
	}
	t.Setenv(home.Env, mini.Home)
	if cfg, _ := config.Load(); func() bool { l, in := cfg.ProgramLink("api"); return !in || l.Program != "studio/apps" }() {
		t.Fatal("api's program keys aren't written")
	}
	if !Poll(30*time.Second, func() bool {
		rs := invitesIn(t, studio, "apps")
		return len(rs) == 1 && rs[0].State == link.ReqAccepted && strings.Join(rs[0].Granted, ",") == "tasks,messages,status"
	}) {
		t.Fatalf("studio never heard the answer: %+v", invitesIn(t, studio, "apps"))
	}

	mini.MustCLI("program", "leave", "--project", "api")
	if !Poll(30*time.Second, func() bool { rs := invitesIn(t, studio, "apps"); return len(rs) == 1 && rs[0].State == link.InviteLeft }) {
		t.Fatalf("studio never heard api left: %+v", invitesIn(t, studio, "apps"))
	}

	// mini's programs off: its hello drops program, and an invite waits.
	mini.MustCLI("server", "stop")
	writeConfig(t, mini, "[features]\nprograms = false\n")
	mini.MustCLI("server", "start")
	if !Poll(30*time.Second, func() bool { pm, _ := mirrored(t, studio, "mini"); return pm.State == "up" && !pm.Program }) {
		t.Fatal("mini's hello still lists program")
	}
	t.Setenv(home.Env, studio.Home)
	again, err := link.InviteMember(caller.Caller{Kind: caller.Human}, &g.Folder, "mini/api")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * time.Second) // a flush or more
	if rs := invitesIn(t, studio, "apps"); len(rs) != 1 || rs[0].ID != again.ID || rs[0].State != link.ReqQueued {
		t.Fatalf("an invite to a server with programs off: %+v", rs)
	}
}
