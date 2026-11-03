package link

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

var (
	appsDirector = caller.Caller{Kind: caller.Director, Project: "apps"}
	userHere     = caller.Caller{Kind: caller.Human}
)

// joined is programServers with mini's api an accepted member of
// studio's apps, answered with o; delegate makes apps' authority
// delegate first, so the invite asks for starts too.
func joined(t *testing.T, o AnswerOpts, delegate bool) (sv *servers, apps *project.Folder, api *project.Project, toMini, toStudio *programWire) {
	t.Helper()
	sv, apps, toMini, toStudio = programServers(t)
	clearHolds := func() { holdMu.Lock(); holdUntil = map[string]time.Time{}; holdMu.Unlock() }
	clearHolds()
	t.Cleanup(clearHolds)
	if delegate {
		f := filepath.Join(sv.studio, "config.toml")
		b, _ := os.ReadFile(f)
		b = []byte(strings.Replace(string(b), "[programs.apps]\n", "[programs.apps]\nauthority = \"delegate\"\n", 1))
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	api = sv.open(sv.mini, "api")
	var id string
	sv.in(sv.studio, func() {
		r, err := InviteMember(userHere, apps, "mini/api")
		if err != nil {
			t.Fatal(err)
		}
		id = r.ID
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		o.Accept = true
		if _, _, err := AnswerInvite(userHere, api, id, o); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		if r := inviteIn(t, "apps"); r.State != ReqAccepted {
			t.Fatalf("not accepted: %+v", r)
		}
	})
	return sv, apps, api, toMini, toStudio
}

func appsBox(apps *project.Folder) *project.Project { return &project.Project{Folder: *apps} }

func resetPush() {
	pushMu.Lock()
	lastPush = time.Time{}
	pushMu.Unlock()
}

// TestProgramTaskFiled: studio's director files a task in mini's api
// (program_tasks = file): mini files it as the paired director, owned
// director:studio/apps, after refs read from mini's side; studio hears
// it filed as T1. A change goes the same way; one the store refuses
// comes back refused. Then api pushes the task's state.
func TestProgramTaskFiled(t *testing.T) {
	sv, apps, api, toMini, toStudio := joined(t, AnswerOpts{Tasks: config.ProgramTasksFile, Status: true}, false)
	var sent *ProgramSent
	sv.in(sv.mini, func() {
		if _, err := api.Tasks().Add(userHere, []tasks.NewTask{{Title: "base"}}); err != nil {
			t.Fatal(err)
		}
	})
	board := &tasks.Store{Dir: apps.Dir, Slug: apps.Slug, Program: true}
	sv.in(sv.studio, func() {
		if _, err := board.Add(userHere, []tasks.NewTask{{Title: "Other"}, {Title: "SSO"}}); err != nil {
			t.Fatal(err)
		}
	})
	sv.in(sv.studio, func() {
		var err error
		sent, err = SendProgramTask(appsDirector, apps, "mini/api", proto.ProgramTask{Key: "ios", Title: "OIDC \x1b[31mlogin\u202e", Notes: "why", Priority: "must", After: []string{"T1", "web/T4"}, Umbrella: "apps/T2"})
		if err != nil || sent.State != SentPending {
			t.Fatalf("send: %+v %v", sent, err)
		}
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		task, err := api.Tasks().Get(2)
		if err != nil {
			t.Fatal(err)
		}
		if task.Title != "OIDC [31mlogin" || task.Owner != "director:studio/apps" || task.Status != tasks.Ready || string(task.Priority) != "must" {
			t.Fatalf("filed: %+v", task)
		}
		if !slices.Equal(task.After, []int{1}) || !slices.Equal(task.AfterX, []string{"studio/web/T4"}) {
			t.Fatalf("after %v %v", task.After, task.AfterX)
		}
		if !strings.Contains(task.Notes, "filed by the director of studio/apps (apps/T2)") {
			t.Fatalf("notes %q", task.Notes)
		}
		its := items(t, api, KindProgramTask)
		if len(its) != 1 || !strings.Contains(its[0].Summary, "the director of studio/apps filed T2") {
			t.Fatalf("items %+v", its)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		got := Sent("apps")
		if len(got) != 1 || got[0].State != proto.ProgramFiled || got[0].Ref != "T2" {
			t.Fatalf("sent: %+v", got)
		}
		its := items(t, appsBox(apps), KindProgramTaskFiled)
		if len(its) != 1 || !strings.Contains(its[0].Summary, "filed mini/api/T2") {
			t.Fatalf("program items %+v", its)
		}
		// The filed task is its umbrella's part now (§5.3).
		if u, err := board.Get(2); err != nil || !slices.Equal(u.Parts, []string{"mini/api/T2"}) {
			t.Fatalf("umbrella %+v %v", u, err)
		}
		prio := "should"
		if _, err := SendProgramEdit(appsDirector, apps, "mini/api", "T2", proto.ProgramTaskEdit{Priority: &prio, Note: "sooner"}); err != nil {
			t.Fatal(err)
		}
		st := "review"
		if _, err := SendProgramEdit(appsDirector, apps, "mini/api", "T2", proto.ProgramTaskEdit{Status: &st}); err != nil {
			t.Fatal(err)
		}
		title := "x"
		if _, err := SendProgramEdit(appsDirector, apps, "mini/api", "T1", proto.ProgramTaskEdit{Title: &title}); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		task, _ := api.Tasks().Get(2)
		if string(task.Priority) != "should" || !strings.Contains(task.Notes, "director:studio/apps") || task.Status != tasks.Ready {
			t.Fatalf("after the edits: %+v", task)
		}
		Flush(context.Background(), toStudio)
	})
	// Over api's rate the last waits in studio's outbox, in order, and
	// goes once a token is back.
	sv.in(sv.studio, func() {
		if got := Sent("apps"); got[len(got)-1].State != SentPending {
			t.Fatalf("over the rate: %+v", got[len(got)-1])
		}
		*sv.clock = sv.clock.Add(time.Hour)
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		if base, _ := api.Tasks().Get(1); base.Title != "base" {
			t.Fatal("the director retitled a task it didn't file")
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		got := Sent("apps")
		want := []string{proto.ProgramFiled, proto.ProgramEdited, proto.ProgramRefused, proto.ProgramRefused}
		var states, codes []string
		for _, x := range got {
			states, codes = append(states, x.State), append(codes, x.Code)
		}
		if !slices.Equal(states, want) || codes[2] != "coordinator-only" || codes[3] != "not-its-filing" {
			t.Fatalf("answers %v %v", states, codes)
		}
	})

	// The status push: the task the director filed, and only it.
	ThreadPR = func(slug, id string) (int, string, bool) { return 7, "https://github.com/o/r/pull/7", true }
	t.Cleanup(func() { ThreadPR = nil })
	sv.in(sv.mini, func() {
		resetPush()
		if _, err := api.Tasks().SetThread(userHere, 2, "t-0003"); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		rt, ok := RemoteTaskOf("mini/api/T2")
		if !ok || rt.Status != "ready" || rt.PR != 7 || !rt.Merged || rt.URL != "https://github.com/o/r/pull/7" {
			t.Fatalf("pushed: %+v %v", rt, ok)
		}
		if _, ok := RemoteTaskOf("mini/api/T1"); ok {
			t.Fatal("T1, whose change was refused, was pushed")
		}
	})
	sv.in(sv.mini, func() {
		resetPush()
		if _, err := api.Tasks().SetStatus(userHere, 2, tasks.Done, ""); err != nil {
			t.Fatal(err)
		}
		n := len(toStudio.got)
		Flush(context.Background(), toStudio)
		resetPush()
		Flush(context.Background(), toStudio)
		if pushes := len(toStudio.got) - n; pushes != 1 {
			t.Fatalf("%d pushes for one change", pushes)
		}
	})
	sv.in(sv.studio, func() {
		if rt, _ := RemoteTaskOf("mini/api/T2"); rt.Status != "done" {
			t.Fatalf("after done: %+v", rt)
		}
	})
}

// TestProgramTaskProposed: program_tasks = propose: the task waits as a
// message the coordinator proposes; nothing is filed.
func TestProgramTaskProposed(t *testing.T) {
	sv, apps, api, toMini, toStudio := joined(t, AnswerOpts{Tasks: config.ProgramTasksPropose}, false)
	sv.in(sv.studio, func() {
		if _, err := SendProgramTask(appsDirector, apps, "mini/api", proto.ProgramTask{Title: "SSO", Notes: "use Okta"}); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		if b, _ := api.Tasks().Load(); len(b.Tasks) != 0 {
			t.Fatalf("filed under propose: %+v", b.Tasks)
		}
		its := items(t, api, KindProgramTask)
		if len(its) != 1 || !strings.Contains(its[0].Summary, "proposes a task for api (SSO)") {
			t.Fatalf("items %+v", its)
		}
		m, err := Read(api, its[0].Message)
		if err != nil || !strings.Contains(m.Text, "Proposed task: SSO") || !strings.Contains(m.Text, "use Okta") {
			t.Fatalf("proposal %+v %v", m, err)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		if got := Sent("apps"); got[0].State != proto.ProgramProposed {
			t.Fatalf("answer %+v", got)
		}
	})
}

// TestProgramWorkRefusals: what the director's side refuses before
// anything goes: no accepted membership, a feature not granted, a thread,
// programs off, attachments, and the link's limits.
func TestProgramWorkRefusals(t *testing.T) {
	sv, apps, _, _ := programServers(t)
	sv.in(sv.studio, func() {
		if _, err := SendProgramTask(appsDirector, apps, "mini/api", proto.ProgramTask{Title: "x"}); code(err) != "not-accepted" {
			t.Fatalf("before the invite: %v", err)
		}
	})
	sv, apps, _, _, _ = joined(t, AnswerOpts{Tasks: config.ProgramTasksFile}, false)
	sv.in(sv.studio, func() {
		cases := map[string]func() error{
			"not-granted": func() error {
				_, err := SendProgramStart(appsDirector, apps, "mini/api", "T1", "", "", proto.ProgramStartDelegate)
				return err
			},
			"director-only": func() error {
				_, err := SendProgramTask(caller.Caller{Kind: caller.Thread, Thread: "t-1"}, apps, "mini/api", proto.ProgramTask{Title: "x"})
				return err
			},
			"too-long": func() error {
				_, err := SendProgramTask(appsDirector, apps, "mini/api", proto.ProgramTask{Title: strings.Repeat("a", 201)})
				return err
			},
			"empty-title": func() error {
				_, err := SendProgramTask(appsDirector, apps, "mini/api", proto.ProgramTask{Title: " \x1b "})
				return err
			},
			"bad-attachment": func() error {
				_, err := SendProgramRemote(appsDirector, apps, "mini/api", Opts{Text: "hi", Attach: []string{"/etc/hosts"}})
				return err
			},
			"invalid-ref": func() error {
				_, err := SendProgramEdit(appsDirector, apps, "mini/api", "T0", proto.ProgramTaskEdit{Note: "x"})
				return err
			},
			"not-member": func() error {
				_, err := SendProgramRemote(appsDirector, apps, "mini/other", Opts{Text: "hi"})
				return err
			},
		}
		for want, fn := range cases {
			if err := fn(); code(err) != want {
				t.Errorf("%s: got %v", want, err)
			}
		}
		setPrograms(t, false)
		f := filepath.Join(sv.studio, "config.toml")
		b, _ := os.ReadFile(f)
		os.WriteFile(f, []byte(strings.Replace(string(b), "programs = true", "programs = false", 1)), 0o600)
		if _, err := SendProgramTask(appsDirector, apps, "mini/api", proto.ProgramTask{Title: "x"}); code(err) != config.ProgramsOff {
			t.Errorf("programs off: %v", err)
		}
	})
}

// TestProgramMessageRemote: the program channel both ways across the
// link, with the sender's copy queued, then sent once delivered.
func TestProgramMessageRemote(t *testing.T) {
	sv, apps, api, toMini, toStudio := joined(t, AnswerOpts{Tasks: config.ProgramTasksFile}, false)
	var id string
	sv.in(sv.studio, func() {
		m, err := SendProgramRemote(appsDirector, apps, "mini/api", Opts{Text: "please look at \x1b[2Jthis"})
		if err != nil || m.State != StateQueued {
			t.Fatalf("send %+v %v", m, err)
		}
		id = m.ID
		Flush(context.Background(), toMini)
		if mine, _ := Read(apps, id); mine.State != StateSent {
			t.Fatalf("sender's copy %+v", mine)
		}
	})
	sv.in(sv.mini, func() {
		its := items(t, api, KindProgramMessage)
		if len(its) != 1 || !strings.Contains(its[0].Summary, "the director of studio/apps wrote "+id) || !strings.Contains(its[0].Summary, "tm link read") {
			t.Fatalf("items %+v", its)
		}
		in, err := Read(api, id)
		if err != nil || in.Text != "please look at [2Jthis" || in.By != "director" {
			t.Fatalf("received %+v %v", in, err)
		}
		coord := caller.Caller{Kind: caller.Coordinator, Project: "api"}
		if _, err := SendProgramRemote(coord, &api.Folder, "studio/other", Opts{Text: "x"}); code(err) != "not-member" {
			t.Fatalf("to another program: %v", err)
		}
		if _, err := SendProgramRemote(coord, &api.Folder, "studio/apps", Opts{Text: "done", Re: id}); err != nil {
			t.Fatal(err)
		}
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		its := items(t, appsBox(apps), KindProgramMessage)
		if len(its) != 1 || !strings.Contains(its[0].Summary, "mini/api's coordinator wrote") || !strings.Contains(its[0].Summary, "re "+id) || !strings.Contains(its[0].Summary, "tm program read") {
			t.Fatalf("program items %+v", its)
		}
	})
}

// TestProgramStartRemote: a start goes only with program_starts; it
// runs ProgramStarter as the paired director, and the result comes back.
func TestProgramStartRemote(t *testing.T) {
	sv, apps, api, toMini, toStudio := joined(t, AnswerOpts{Tasks: config.ProgramTasksFile, Starts: true}, true)
	old := runAfter
	runAfter = func(fn func()) { fn() }
	t.Cleanup(func() { runAfter = old })
	var who caller.Caller
	ProgramStarter = func(c caller.Caller, p *project.Project, id int, agent, model string) (string, error) {
		who = c
		if id == 9 {
			return "", &tasks.Error{Code: "cap-reached", Msg: "full"}
		}
		return "t-0042", nil
	}
	t.Cleanup(func() { ProgramStarter = nil })
	sv.in(sv.studio, func() {
		for _, ref := range []string{"T1", "T9"} {
			if _, err := SendProgramStart(appsDirector, apps, "mini/api", ref, "claude", "opus", proto.ProgramStartDelegate); err != nil {
				t.Fatal(err)
			}
		}
		Flush(context.Background(), toMini)
	})
	if who.String() != "peer:studio (director apps)" || who.DirectorOf() != "studio/apps" {
		t.Fatalf("started as %v", who)
	}
	sv.in(sv.mini, func() { Flush(context.Background(), toStudio) })
	sv.in(sv.studio, func() {
		got := Sent("apps")
		if got[0].State != proto.ProgramStarted || got[0].Thread != "t-0042" || got[1].State != proto.ProgramRefused || got[1].Code != "cap-reached" {
			t.Fatalf("results %+v", got)
		}
		if its := items(t, appsBox(apps), KindProgramStartResult); len(its) != 2 || !strings.Contains(its[1].Summary, "thread caps are full") {
			t.Fatalf("items %+v", its)
		}
	})
	// mini's user turns starts off: refused there, whatever studio says.
	sv.in(sv.mini, func() {
		if err := config.SetProgramLinkKey("api", "program_starts", false); err != nil {
			t.Fatal(err)
		}
	})
	sv.in(sv.studio, func() {
		SendProgramStart(appsDirector, apps, "mini/api", "T1", "", "", proto.ProgramStartApproved)
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		Flush(context.Background(), toStudio)
		_ = api
	})
	sv.in(sv.studio, func() {
		got := Sent("apps")
		if last := got[len(got)-1]; last.State != proto.ProgramRefused || last.Code != "starts-off" {
			t.Fatalf("starts off: %+v", last)
		}
	})
}

// TestProgramRename: the director's program is renamed; mini's api
// follows without a prompt, and later work comes from the new name.
func TestProgramRename(t *testing.T) {
	sv, _, api, toMini, _ := joined(t, AnswerOpts{Tasks: config.ProgramTasksFile}, false)
	sv.in(sv.studio, func() {
		root := filepath.Dir(programFolder("apps").Dir)
		if err := os.Rename(filepath.Join(root, "apps"), filepath.Join(root, "mobile")); err != nil {
			t.Fatal(err)
		}
		if notes := TellRenamed("apps", "mobile", []string{"mini/api", "mini/never"}); len(notes) != 1 || !strings.Contains(notes[0], "mini/never not told") {
			t.Fatalf("notes %q", notes)
		}
		if r := inviteIn(t, "mobile"); r.Peer != "mini/api" {
			t.Fatalf("invite after rename %+v", r)
		}
		Flush(context.Background(), toMini)
	})
	sv.in(sv.mini, func() {
		l, in := mustCfg(t).ProgramLink("api")
		if !in || l.Program != "studio/mobile" || l.Tasks != config.ProgramTasksFile {
			t.Fatalf("api's keys %+v", l)
		}
		if r := inviteIn(t, "api"); r.Peer != "studio/mobile" {
			t.Fatalf("api's invite %+v", r)
		}
		_ = api
	})
}

// TestProgramWorkReceive: what a member's server takes from the wire,
// whatever the other side sent: ids, refs and slugs that don't match
// are bad-message, strings are cleaned and capped, a non-member is
// refused, and a status push is kept only from an accepted member that
// granted status, with a URL only when https on a known host.
func TestProgramWorkReceive(t *testing.T) {
	sv, apps, _, _, _ := joined(t, AnswerOpts{Tasks: config.ProgramTasksFile, Status: true}, false)
	sfp, mfp := fingerprint(sv.sid), fingerprint(sv.mid)
	n := 100
	msg := func(fp, kind, from, to string, task *proto.ProgramTask) proto.PeerMessage {
		n++
		return proto.PeerMessage{Epoch: "e", Seq: n, Kind: kind, From: from, To: to, ID: "M-" + fp + "-" + strconv.Itoa(n), By: "director", Task: task}
	}
	sv.in(sv.mini, func() {
		cases := []struct {
			name string
			m    proto.PeerMessage
			want string
		}{
			{"wrong fingerprint", msg(mfp, proto.PeerProgramTask, "apps", "api", &proto.ProgramTask{Title: "x"}), "bad-message"},
			{"no task", msg(sfp, proto.PeerProgramTask, "apps", "api", nil), "bad-message"},
			{"bad ref", msg(sfp, proto.PeerProgramTask, "apps", "api", &proto.ProgramTask{Ref: "T1; rm", Edit: &proto.ProgramTaskEdit{Note: "x"}}), "bad-message"},
			{"too many steps", msg(sfp, proto.PeerProgramTask, "apps", "api", &proto.ProgramTask{Title: "x", Steps: make([]string, 51)}), "bad-message"},
			{"other program", msg(sfp, proto.PeerProgramTask, "beta", "api", &proto.ProgramTask{Title: "x"}), "not-member"},
			{"no project", msg(sfp, proto.PeerProgramTask, "apps", "nope", &proto.ProgramTask{Title: "x"}), "not-member"},
			{"bad start allow", msg(sfp, proto.PeerProgramStart, "apps", "api", &proto.ProgramTask{Ref: "T1"}), "bad-message"},
			{"bad rename", func() proto.PeerMessage {
				m := msg(sfp, proto.PeerProgramRename, "apps", "api", nil)
				m.Rename = "../x"
				return m
			}(), "bad-message"},
		}
		for _, tc := range cases {
			if res := Receive("studio", sv.sid, tc.m); res.Code != tc.want {
				t.Errorf("%s: %+v", tc.name, res)
			}
		}
		// A long title is cut, never refused once it is here.
		m := msg(sfp, proto.PeerProgramTask, "apps", "api", &proto.ProgramTask{Title: strings.Repeat("é", 300), Notes: strings.Repeat("n", 9000)})
		if res := Receive("studio", sv.sid, m); res.Status != proto.PeerDelivered {
			t.Fatalf("long: %+v", res)
		}
		task, err := sv.open(sv.mini, "api").Tasks().Get(1)
		if err != nil || len([]rune(task.Title)) != maxProgramTitle || len(task.Notes) > maxProgramNotes+200 {
			t.Fatalf("capped: %d runes, %d bytes of notes, %v", len([]rune(task.Title)), len(task.Notes), err)
		}
	})
	sv.in(sv.studio, func() {
		push := func(from string, task proto.ProgramTask) proto.PeerMessageResult {
			m := msg(mfp, proto.PeerProgramStatus, from, "apps", &task)
			m.By = "human"
			return Receive("mini", sv.mid, m)
		}
		if res := push("api", proto.ProgramTask{Ref: "T1", Status: "shipped"}); res.Code != "bad-message" {
			t.Errorf("unknown status: %+v", res)
		}
		if res := push("other", proto.ProgramTask{Ref: "T1", Status: "ready"}); res.Code != "not-member" {
			t.Errorf("non-member: %+v", res)
		}
		for u, want := range map[string]string{
			"http://github.com/o/r/pull/1":                   "",
			"https://evil.example/pull/1":                    "",
			"https://github.com/o/r/pull/1\x1b":              "",
			"https://user@github.com/o/r/pull/1":             "",
			"https://github.com/o/r/pull/1":                  "https://github.com/o/r/pull/1",
			"https://dev.azure.com/o/p/_git/r/pullrequest/3": "https://dev.azure.com/o/p/_git/r/pullrequest/3",
		} {
			if res := push("api", proto.ProgramTask{Ref: "T1", Status: "review", Title: "t\u202e" + strings.Repeat("x", 200), URL: u}); res.Status != proto.PeerDelivered {
				t.Fatalf("push %s: %+v", u, res)
			}
			rt, _ := RemoteTaskOf("mini/api/T1")
			if rt.URL != want || len([]rune(rt.Title)) > maxStatusTitle || strings.ContainsRune(rt.Title, '\u202e') {
				t.Errorf("%s: kept %+v", u, rt)
			}
		}
		_ = apps
	})
}

// TestProgramWatch: studio's program asks mini's api for the state of
// a task its director didn't file (an umbrella's part): asked once, it
// joins what api pushes, and studio has its state. A member that
// didn't grant status pushes isn't asked.
func TestProgramWatch(t *testing.T) {
	sv, _, api, toMini, toStudio := joined(t, AnswerOpts{Tasks: config.ProgramTasksFile, Status: true}, false)
	sv.in(sv.mini, func() {
		if _, err := api.Tasks().Add(userHere, []tasks.NewTask{{Title: "Keychain login", Status: "ready"}}); err != nil {
			t.Fatal(err)
		}
	})
	sv.in(sv.studio, func() {
		for range 2 {
			if err := AskWatch("apps", "mini/api/T1"); err != nil {
				t.Fatal(err)
			}
		}
		n := len(toMini.got)
		Flush(context.Background(), toMini)
		if sent := len(toMini.got) - n; sent != 1 {
			t.Fatalf("%d program-watch messages sent", sent)
		}
		if err := AskWatch("apps", "acme/T1"); code(err) != "invalid-ref" {
			t.Errorf("a local ref: %v", err)
		}
	})
	sv.in(sv.mini, func() {
		resetPush()
		Flush(context.Background(), toStudio)
	})
	sv.in(sv.studio, func() {
		rt, ok := RemoteTaskOf("mini/api/T1")
		if !ok || rt.Status != "ready" || rt.Title != "Keychain login" {
			t.Fatalf("pushed: %+v %v", rt, ok)
		}
	})
}

// TestProgramWatchNoStatus: without status granted, nothing is asked.
func TestProgramWatchNoStatus(t *testing.T) {
	sv, _, _, _, _ := joined(t, AnswerOpts{Tasks: config.ProgramTasksFile}, false)
	sv.in(sv.studio, func() {
		if err := AskWatch("apps", "mini/api/T1"); code(err) != "not-granted" {
			t.Errorf("no status: %v", err)
		}
	})
}
