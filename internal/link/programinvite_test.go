package link

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// programServers are setupServers with programs on on both, and on
// studio the program apps listing mini/api.
func programServers(t *testing.T) (sv *servers, apps *project.Folder, toMini, toStudio *programWire) {
	t.Helper()
	sv = setupServers(t)
	setPrograms(t, true)
	add := func(dir, extra string) {
		f := filepath.Join(dir, "config.toml")
		b, _ := os.ReadFile(f)
		if err := os.WriteFile(f, append([]byte("[features]\nprograms = true\n\n"+extra), b...), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	add(sv.studio, "[programs.apps]\nprojects = [\"mini/api\"]\n\n")
	add(sv.mini, "")
	sv.in(sv.studio, func() {
		root, _ := home.ProgramsDir()
		if err := os.MkdirAll(filepath.Join(root, "apps"), 0o700); err != nil {
			t.Fatal(err)
		}
		apps = programFolder("apps")
	})
	toMini = &programWire{wire: &wire{sv: sv, to: sv.mini, alias: "studio", id: sv.sid, up: true}, cap: true}
	toStudio = &programWire{wire: &wire{sv: sv, to: sv.studio, alias: "mini", id: sv.mid, up: true}, cap: true}
	return sv, apps, toMini, toStudio
}

func inviteIn(t *testing.T, slug string) Invite {
	t.Helper()
	rs := Invites(slug)
	if len(rs) != 1 {
		t.Fatalf("%s's invites: %+v", slug, rs)
	}
	return rs[0]
}

// TestProgramInvite: studio's program apps invites mini's api; mini's
// user, and no agent, accepts it, which writes api's program keys and
// the ticked switches; studio hears the granted features. Then api
// leaves, and studio sees it left; invited again and dropped by studio,
// api's keys go.
func TestProgramInvite(t *testing.T) {
	sv, apps, toMini, toStudio := programServers(t)
	human := caller.Caller{Kind: caller.Human}
	api := sv.open(sv.mini, "api")
	var id string
	sv.in(sv.studio, func() {
		r, err := InviteMember(human, apps, "mini/api")
		if err != nil {
			t.Fatal(err)
		}
		if id = r.ID; r.State != ReqQueued || !slices.Equal(r.Features, []string{"tasks", "messages", "status"}) {
			t.Fatalf("invite: %+v", r)
		}
		if again, err := InviteMember(human, apps, "mini/api"); err != nil || again.ID != id {
			t.Fatalf("invited twice: %+v %v", again, err)
		}
		Flush(context.Background(), toMini)
		if r := inviteIn(t, "apps"); r.State != ReqWaiting {
			t.Fatalf("after delivery: %+v", r)
		}
	})
	sv.in(sv.mini, func() {
		r := inviteIn(t, "api")
		if r.ID != id || r.Peer != "studio/apps" || r.State != ReqWaiting {
			t.Fatalf("mini's invite: %+v", r)
		}
		its := items(t, api, KindProgramRequest)
		if len(its) != 1 || !its[0].NeedsUser || !strings.Contains(its[0].Summary, "invites api into its program") {
			t.Fatalf("item: %+v", its)
		}
		opts := AnswerOpts{Accept: true, Tasks: config.ProgramTasksPropose, Status: true, Share: true, NeedsView: true}
		if _, _, err := AnswerInvite(caller.Caller{Kind: caller.Coordinator, Project: "api"}, api, id, opts); err == nil {
			t.Fatal("a coordinator answered an invite")
		}
		got, changed, err := AnswerInvite(human, api, id, opts)
		if err != nil || got.State != ReqAccepted || !changed {
			t.Fatalf("accept: %+v %v %v", got, changed, err)
		}
		cfg := mustCfg(t)
		l, in := cfg.ProgramLink("api")
		if !in || l != (config.ProgramLink{Program: "studio/apps", Tasks: config.ProgramTasksPropose, Status: true}) {
			t.Fatalf("api's program keys: %+v %v", l, in)
		}
		if sf, _ := cfg.Safety("api"); !sf.Share {
			t.Fatal("api isn't shared")
		}
		peers, _ := peer.LoadPeers(peer.DirIn(sv.mini))
		if peers[0].NeedsLevel() != proto.NeedsView {
			t.Fatalf("needs %q", peers[0].NeedsLevel())
		}
		if its := items(t, api, KindProgramRequest); len(its) != 0 {
			t.Fatalf("item not done: %+v", its)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		r := inviteIn(t, "apps")
		if r.State != ReqAccepted || !slices.Equal(r.Granted, []string{"tasks", "messages", "status"}) || r.Words() != "member: tasks, messages, status" {
			t.Fatalf("studio's invite after the answer: %+v", r)
		}
		if its := items(t, &project.Project{Folder: *apps}, KindProgramAnswer); len(its) != 1 {
			t.Fatalf("program's item: %+v", its)
		}
	})

	// api leaves.
	sv.in(sv.mini, func() {
		if _, err := Leave(human, &api.Folder, "studio/apps"); err != nil {
			t.Fatal(err)
		}
		if _, in := mustCfg(t).ProgramLink("api"); in {
			t.Fatal("api's program keys stay")
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		if r := inviteIn(t, "apps"); r.State != InviteLeft || r.By != "there" {
			t.Fatalf("studio after api left: %+v", r)
		}
		r, err := InviteMember(human, apps, "mini/api")
		if err != nil || r.ID == id {
			t.Fatalf("invited again: %+v %v", r, err)
		}
		id = r.ID
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		if _, _, err := AnswerInvite(human, api, id, AnswerOpts{Accept: true, Tasks: config.ProgramTasksFile}); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toStudio)
	})
	// studio drops it: api's keys go there.
	sv.in(sv.studio, func() {
		if r := inviteIn(t, "apps"); !slices.Equal(r.Granted, []string{"tasks", "messages"}) {
			t.Fatalf("granted without status: %+v", r)
		}
		if _, err := Leave(human, apps, "mini/api"); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		if _, in := mustCfg(t).ProgramLink("api"); in {
			t.Fatal("api's keys stay after studio dropped it")
		}
		if r := inviteIn(t, "api"); r.State != InviteLeft || r.By != "there" {
			t.Fatalf("mini after studio dropped it: %+v", r)
		}
		if its := items(t, api, KindProgramLeft); len(its) != 1 {
			t.Fatalf("left item: %+v", its)
		}
	})
}

// TestProgramInviteRefused: an invite to a project in a program already
// is refused not-taken, and one to a server with programs off
// programs-off; the program's item says so. Invites aren't shown while
// programs are off.
func TestProgramInviteRefused(t *testing.T) {
	sv, apps, toMini, _ := programServers(t)
	human := caller.Caller{Kind: caller.Human}
	sv.in(sv.mini, func() {
		if err := config.SetProgramLink("api", config.ProgramLink{Program: "nas/other", Tasks: config.ProgramTasksFile}); err != nil {
			t.Fatal(err)
		}
	})
	sv.in(sv.studio, func() {
		if _, err := InviteMember(human, apps, "mini/api"); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
		r := inviteIn(t, "apps")
		if r.State != ReqUndelivered || r.Code != "not-taken" {
			t.Fatalf("to a project in a program: %+v", r)
		}
		its := items(t, &project.Project{Folder: *apps}, KindProgramUndelivered)
		if len(its) != 1 || !strings.Contains(its[0].Summary, "in a program already") {
			t.Fatalf("item: %+v", its)
		}
	})
	sv.in(sv.mini, func() { config.UnsetProgramLink("api") })
	sv.in(sv.studio, func() {
		if _, err := InviteMember(human, apps, "mini/api"); err != nil {
			t.Fatal(err)
		}
		SetPrograms(false) // mini's switch, as Receive reads it
	})
	// The sender's outbox is paused too in one process: deliver by hand.
	var res proto.PeerMessageResult
	sv.in(sv.mini, func() {
		res = Receive("studio", sv.sid, proto.PeerMessage{Kind: proto.PeerProgramInvite, Epoch: "e", Seq: 9, From: "apps", To: "api", ID: "P-" + fingerprint(sv.sid) + "-99", By: "human"})
	})
	if res.Code != config.ProgramsOff {
		t.Fatalf("to a server with programs off: %+v", res)
	}
	sv.in(sv.studio, func() {
		f := filepath.Join(sv.studio, "config.toml")
		b, _ := os.ReadFile(f)
		os.WriteFile(f, []byte(strings.Replace(string(b), "programs = true", "programs = false", 1)), 0o600)
		if rs := Invites("apps"); rs != nil {
			t.Fatalf("invites shown while off: %+v", rs)
		}
		if _, err := InviteMember(human, apps, "mini/api"); err == nil || !strings.Contains(err.Error(), "programs are off") {
			t.Fatalf("invite while off: %v", err)
		}
	})
}
