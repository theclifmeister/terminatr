package appapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/home/hometest"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/plat/ipc/ipctest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/service/servicetest"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

func TestMain(m *testing.M) {
	os.Exit(hometest.Run(func() int { return servicetest.Run(m) }))
}

// testPaths gives a test its own home and a short socket path.
func testPaths(t *testing.T) client.Paths {
	t.Helper()
	t.Setenv("TERMINATR_HOME", t.TempDir())
	t.Setenv("TERMINATR_SOCKET", filepath.Join(ipctest.ShortTempDir(t, "tma"), "tm.sock"))
	t.Setenv("TERMINATR_LAUNCHD", "off")
	t.Setenv(caller.EnvSession, "")
	p, err := client.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// startServer runs a server for p until the test ends.
func startServer(t *testing.T, p client.Paths) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.Options{Paths: p, Log: log.New(io.Discard, "", 0), Bin: "/nonexistent/tm",
			Env: []string{"PATH=/usr/bin:/bin"}})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("server did not stop")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := client.Dial(p, proto.KindControl); err == nil {
			c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not come up")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// bridge runs Serve in-process over pipes and returns the app's side.
func bridge(t *testing.T, p client.Paths, autostart bool) (*apptest.Client, error) {
	t.Helper()
	return bridgeWith(t, appapi.Options{Paths: p, Autostart: autostart})
}

// bridgeWith runs Serve with options o.
func bridgeWith(t *testing.T, o appapi.Options) (*apptest.Client, error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	served := make(chan error, 1)
	go func() {
		err := appapi.Serve(inR, outW, o)
		outW.Close()
		served <- err
	}()
	t.Cleanup(func() {
		inW.Close()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Error("the bridge didn't end with its stdin")
		}
	})
	return apptest.New(outR, inW)
}

func code(err error) string {
	var e *appapi.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// TestBridgeNoServer: without a server, and not to start one, the hello
// says so and the bridge ends.
func TestBridgeNoServer(t *testing.T) {
	p := testPaths(t)
	c, err := bridge(t, p, false)
	if code(err) != appapi.ErrServer || c == nil || c.Hello.AppAPI != appapi.Version || c.Hello.Server != nil {
		t.Fatalf("hello %+v, %v", c, err)
	}
}

// TestBridge: the hello names the app API and the server; requests are
// answered by id, several at once, streams among them; what isn't in
// the app API never reaches the server; writes are journaled as
// "human (app)".
func TestBridge(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Review me"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pr.Tasks().SetStatus(human, 1, tasks.Review, ""); err != nil {
		t.Fatal(err)
	}

	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	h := c.Hello
	if h.AppAPI != appapi.Version || h.Server == nil || h.Server.Protocol != proto.Protocol || h.Server.PID != os.Getpid() ||
		len(h.Methods) != len(appapi.MethodNames())-1 || slices.Contains(h.Methods, appapi.MethodDeviceNonce) || h.Sign || len(h.Capabilities) == 0 {
		t.Fatalf("hello %+v %+v", h, h.Server)
	}

	// A stream and calls beside it.
	var ps appapi.Projects
	st, err := c.Stream(proto.MethodProjectsWatch, appapi.ProjectsWatchParams{Detail: true}, &ps)
	if err != nil || len(ps.Projects) != 1 || ps.Projects[0].Slug != "web" || len(ps.Projects[0].NeedsYou) != 1 ||
		ps.Projects[0].NeedsYou[0].Ref != "T1" || ps.Projects[0].Counts == nil {
		t.Fatalf("projects.watch: %+v %v", ps, err)
	}
	var list appapi.SessionList
	if err := c.Call(proto.MethodSessionList, nil, &list); err != nil || list.Sessions == nil || list.Notices == nil || list.Version == "" {
		t.Fatalf("session.list: %+v %v", list, err)
	}
	var peers appapi.PeerList
	if err := c.Call(proto.MethodPeerList, nil, &peers); err != nil || peers.Peers == nil {
		t.Fatalf("peer.list: %+v %v", peers, err)
	}
	var asked appapi.TaskAskResult
	if err := c.Call(proto.MethodTaskAsk, appapi.TaskAskParams{Project: "web", Task: "T1", Kind: "accept"}, &asked); err != nil || !asked.Asked {
		t.Fatalf("task.ask: %+v %v", asked, err)
	}
	// The ask changes the project: its item comes as an event.
	for {
		var next appapi.Projects
		ev, err := st.Next(&next)
		if err != nil || ev != proto.EventProjectsChanged {
			t.Fatalf("event %q: %v", ev, err)
		}
		if len(next.Projects) == 1 && len(next.Projects[0].Items) == 1 {
			if it := next.Projects[0].Items[0]; it.Kind != project.KindAccept || it.Subject != "T1" {
				t.Fatalf("item %+v", it)
			}
			break
		}
	}
	if err := st.Cancel(); err != nil {
		t.Fatal(err)
	}
	for {
		var next appapi.Projects
		if _, err := st.Next(&next); err == apptest.ErrEnd {
			break
		} else if err != nil {
			t.Fatalf("after cancel: %v", err)
		}
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	if !strings.Contains(string(journal), " human (app) task.accept.ask T1") {
		t.Errorf("journal:\n%s", journal)
	}

	// Not in the app API, whatever the server has; strict params.
	for _, m := range []string{proto.MethodServerStop, proto.MethodSessionStart, proto.MethodCLIRun, "view.attach", proto.MethodModCommand} {
		if err := c.Call(m, map[string]any{"yes": true}, nil); code(err) != appapi.ErrUnknownMethod {
			t.Errorf("%s: %v", m, err)
		}
	}
	if err := c.Call(proto.MethodSessionKeys, map[string]string{"id": "s-1", "data": "1", "relay": "coordinator"}, nil); code(err) != appapi.ErrBadParams {
		t.Errorf("keys with a relay: %v", err)
	}
	// The server's own errors keep their codes.
	if err := c.Call(proto.MethodSessionApprove, appapi.ApproveParams{ID: "s-99"}, nil); code(err) != proto.ErrUnknownSession {
		t.Errorf("approve of no session: %v", err)
	}
	if err := c.Call(proto.MethodTaskAsk, appapi.TaskAskParams{Project: "web", Task: "T1", Kind: "delegate"}, nil); code(err) != proto.ErrRefused {
		t.Errorf("delegate a task in review: %v", err)
	}
	// A line that isn't a request is answered, and the next one too.
	if err := c.WriteLine(map[string]any{"id": 0, "method": "session.list"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(proto.MethodSessionList, nil, nil); err != nil {
		t.Errorf("after a bad line: %v", err)
	}
}

// TestBridgeTaskActions: the task actions by name (app API 5) ask the
// coordinator with tm task accept's and send-back's checks, their codes
// reaching the app; project.open starts the project's coordinator once;
// task.add (app API 7) adds a task once; coordinator.clear's refusals
// reach the app, and project.active (app API 13) stops the coordinator
// and resumes it. Each is journaled as "human (app)".
// task.get (app API 14) reads a task with its notes.
func TestBridgeTaskActions(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(p.AgentsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "manifest_version = 1\nname = \"fake\"\n[launch]\ncommand = \"/bin/sh\"\nargs = [\"-c\", \"sleep 30\"]\n"
	if err := os.WriteFile(filepath.Join(p.AgentsDir(), "fake.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SetProject("web", "coordinator_agent", "fake"); err != nil {
		t.Fatal(err)
	}
	human := caller.Caller{Kind: caller.Human}
	if _, err := pr.Tasks().Add(human, []tasks.NewTask{{Title: "Ship it"}, {Title: "Review me"}, {Title: "Review me too"}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{2, 3} {
		if _, err := pr.Tasks().SetStatus(human, id, tasks.Review, ""); err != nil {
			t.Fatal(err)
		}
	}
	startServer(t, p)
	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	var asked appapi.TaskAskResult
	if err := c.Call(proto.MethodTaskAccept, appapi.TaskParams{Project: "web", Task: "T2"}, &asked); err != nil || !asked.Asked {
		t.Fatalf("task.accept: %+v %v", asked, err)
	}
	if err := c.Call(proto.MethodTaskSendBack, appapi.SendBackParams{Project: "web", Task: "T3", Note: "the tests fail"}, &asked); err != nil || !asked.Asked {
		t.Fatalf("task.send-back: %+v %v", asked, err)
	}
	if err := c.Call(proto.MethodTaskDelegate, appapi.TaskParams{Project: "web", Task: "T1"}, &asked); err != nil || !asked.Asked {
		t.Fatalf("task.delegate: %+v %v", asked, err)
	}
	for _, bad := range []struct {
		method string
		params any
		code   string
	}{
		{proto.MethodTaskAccept, appapi.TaskParams{Project: "web", Task: "T1"}, "not-accept"},
		{proto.MethodTaskSendBack, appapi.SendBackParams{Project: "web", Task: "T2", Note: "no"}, "already-asked"},
		{proto.MethodTaskSendBack, appapi.SendBackParams{Project: "web", Task: "T3"}, "invalid-note"},
		{proto.MethodTaskAccept, map[string]string{"project": "web", "task": "T2", "note": "x"}, appapi.ErrBadParams},
		{proto.MethodTaskAdd, appapi.TaskAddParams{Project: "web", Title: "two\nlines"}, "invalid-title"},
	} {
		if err := c.Call(bad.method, bad.params, nil); code(err) != bad.code {
			t.Errorf("%s %+v: %v, want %s", bad.method, bad.params, err, bad.code)
		}
	}
	var added appapi.TaskAdded
	if err := c.Call(proto.MethodTaskAdd, appapi.TaskAddParams{Project: "web", Title: "Add docs", Notes: "From the app."}, &added); err != nil || added.Task != "T4" || added.Existing {
		t.Fatalf("task.add: %+v %v", added, err)
	}
	if err := c.Call(proto.MethodTaskAdd, appapi.TaskAddParams{Project: "web", Title: "Add docs"}, &added); err != nil || added.Task != "T4" || !added.Existing {
		t.Fatalf("task.add again: %+v %v", added, err)
	}
	var got appapi.Task
	if err := c.Call(proto.MethodTaskGet, appapi.TaskGetParams{Project: "web", Task: "T4"}, &got); err != nil ||
		got.Ref != "T4" || got.Title != "Add docs" || got.Notes != "From the app." {
		t.Fatalf("task.get: %+v %v", got, err)
	}
	if err := c.Call(proto.MethodTaskGet, appapi.TaskGetParams{Project: "web", Task: "T9"}, nil); code(err) != "unknown-task" {
		t.Errorf("task.get of no task: %v", err)
	}
	// task.done and task.templates (app API 20): none done yet; the
	// bundled templates, one of which task.add lays on a title.
	var done appapi.DoneTasks
	if err := c.Call(proto.MethodTaskDone, appapi.ProjectParams{Project: "web"}, &done); err != nil || done.Tasks == nil || len(done.Tasks) != 0 {
		t.Fatalf("task.done: %+v %v", done, err)
	}
	var tpls appapi.Templates
	if err := c.Call(proto.MethodTaskTemplates, appapi.ProjectParams{Project: "web"}, &tpls); err != nil || len(tpls.Templates) == 0 {
		t.Fatalf("task.templates: %+v %v", tpls, err)
	}
	if err := c.Call(proto.MethodTaskAdd, appapi.TaskAddParams{Project: "web", Title: "Login", Template: tpls.Templates[0].Ref}, &added); err != nil || added.Task != "T5" {
		t.Fatalf("task.add with %s: %+v %v", tpls.Templates[0].Ref, added, err)
	}
	if _, err := pr.AddQuestion(project.Question{Question: "Which?", Options: []project.QuestionOption{{Label: "A"}, {Label: "B"}}}); err != nil {
		t.Fatal(err)
	}
	// No coordinator runs to put the question to the user.
	if err := c.Call(proto.MethodQuestionsOpen, appapi.ProjectParams{Project: "web"}, nil); code(err) != proto.ErrRefused {
		t.Errorf("questions.open without a coordinator: %v", err)
	}
	var opened appapi.ProjectOpen
	if err := c.Call(proto.MethodProjectOpen, appapi.ProjectOpenParams{Project: "web", Cols: 120, Rows: 40}, &opened); err != nil || !opened.Started || opened.Session == "" {
		t.Fatalf("project.open: %+v %v", opened, err)
	}
	var again appapi.ProjectOpen
	if err := c.Call(proto.MethodProjectOpen, appapi.ProjectOpenParams{Project: "web"}, &again); err != nil || again.Started || again.Session != opened.Session {
		t.Fatalf("project.open again: %+v %v", again, err)
	}
	var qs appapi.QuestionsOpen
	if err := c.Call(proto.MethodQuestionsOpen, appapi.ProjectParams{Project: "web"}, &qs); err != nil || qs.Session != opened.Session || qs.Open != 1 {
		t.Fatalf("questions.open: %+v %v", qs, err)
	}
	// A file for the coordinator's prompt lands in the project's uploads/.
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	var up appapi.Upload
	if err := c.Call(proto.MethodSessionUpload, appapi.UploadParams{Session: opened.Session, Name: "shot.png", Size: int64(len(png)), Data: png}, &up); err != nil ||
		!strings.HasSuffix(up.Ref, "-shot.png") || up.Type != "image/png" {
		t.Fatalf("session.upload: %+v %v", up, err)
	}
	if got, err := os.ReadFile(pr.Path("uploads", up.Ref)); err != nil || !bytes.Equal(got, png) {
		t.Errorf("uploaded: %v", err)
	}
	if err := c.Call(proto.MethodSessionPrompt, appapi.PromptParams{ID: opened.Session, Files: []string{"../PROJECT.md"}}, nil); code(err) != proto.ErrBadParams {
		t.Errorf("session.prompt with a bad ref: %v", err)
	}
	// The fake agent has no clear prompt: the save and clear is refused
	// at once, its code reaching the app.
	if err := c.Call(proto.MethodCoordinatorClear, appapi.ProjectParams{Project: "web"}, nil); code(err) != proto.ErrRefused {
		t.Errorf("coordinator.clear without a clear prompt: %v", err)
	}
	var off appapi.ProjectActive
	if err := c.Call(proto.MethodProjectActive, appapi.ProjectActiveParams{Project: "web"}, &off); err != nil || !off.Changed ||
		!slices.Equal(off.Stopped, []string{"coordinator"}) {
		t.Fatalf("project.active off: %+v %v", off, err)
	}
	if err := c.Call(proto.MethodProjectActive, appapi.ProjectActiveParams{Project: "web"}, &off); err != nil || off.Changed {
		t.Fatalf("project.active off again: %+v %v", off, err)
	}
	if err := c.Call(proto.MethodCoordinatorClear, appapi.ProjectParams{Project: "web"}, nil); code(err) != proto.ErrRefused {
		t.Errorf("coordinator.clear with none running: %v", err)
	}
	var on appapi.ProjectActive
	if err := c.Call(proto.MethodProjectActive, appapi.ProjectActiveParams{Project: "web", Active: true}, &on); err != nil || !on.Changed ||
		!slices.Equal(on.Resumed, []string{"coordinator"}) {
		t.Fatalf("project.active on: %+v %v", on, err)
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	for _, want := range []string{" human (app) session.upload ", " human (app) task.accept.ask T2", " human (app) task.sendback.ask T3 the tests fail",
		" human (app) task.delegate.ask T1", " human (app) task.add T4 Add docs", " human (app) coordinator.start " + opened.Session,
		" human (app) questions.open " + opened.Session + " 1 open", " human (app) project.deactivate web",
		" human (app) project.activate web"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
}

// TestBridgeLinkActs: what a project's links wait on the user for
// reaches the app by the ids link.act takes (app API 16); acting on
// them is journaled as "human (app)", with tm link's checks; a message
// is never written through it.
func TestBridgeLinkActs(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"),
		[]byte("[projects.a.links]\nb = { allow = \"both\", ask = true }\n\n[projects.b.links]\na = \"both\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := project.New(project.Options{Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := project.New(project.Options{Slug: "b"})
	if err != nil {
		t.Fatal(err)
	}
	lib := thread.Path(b, "t-0001", "library")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "x.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := link.Send(caller.Caller{Kind: caller.Coordinator, Project: "b"}, b, "a", link.Opts{Text: "a file", File: "t-0001/x.txt"})
	if err != nil {
		t.Fatal(err)
	}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "a"}
	h1, err := link.Send(coord, a, "b", link.Opts{Text: "Hello b\nmore"})
	if err != nil || h1.State != link.StateHeld {
		t.Fatalf("send: %+v %v", h1, err)
	}
	h2, _ := link.Send(coord, a, "b", link.Opts{Text: "Second"})
	startServer(t, p)
	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.Hello.Server.Caps, proto.CapLinkAct) {
		t.Errorf("caps %v", c.Hello.Server.Caps)
	}
	var ps appapi.Projects
	st, err := c.Stream(proto.MethodProjectsWatch, appapi.ProjectsWatchParams{Detail: true}, &ps)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Cancel()
	var needs []appapi.LinkNeed
	for _, pr := range ps.Projects {
		if pr.Slug == "a" {
			needs = pr.LinkNeeds
		}
	}
	if len(needs) != 3 || needs[0].Kind != "held" || needs[0].ID != h1.ID || needs[0].Peer != "b" || needs[0].Title != "Hello b" ||
		needs[1].ID != h2.ID || needs[2].Kind != "file" || needs[2].ID != f.Transfer || needs[2].File != "x.txt" || needs[2].Size != 5 {
		t.Fatalf("link needs %+v", needs)
	}
	var res appapi.LinkActResult
	if err := c.Call(proto.MethodLinkAct, appapi.LinkActParams{Project: "a", Action: "approve", ID: h1.ID}, &res); err != nil ||
		!strings.Contains(res.Words, h1.ID) {
		t.Fatalf("approve: %+v %v", res, err)
	}
	if err := c.Call(proto.MethodLinkAct, appapi.LinkActParams{Project: "a", Action: "drop", ID: h2.ID}, &res); err != nil ||
		res.Words != "dropped "+h2.ID+"; it is never sent" {
		t.Fatalf("drop: %+v %v", res, err)
	}
	if err := c.Call(proto.MethodLinkAct, appapi.LinkActParams{Project: "a", Action: "accept-file", ID: f.Transfer}, &res); err != nil ||
		res.Words != "accepted x.txt: it is in place" {
		t.Fatalf("accept-file: %+v %v", res, err)
	}
	for _, bad := range []struct {
		params any
		code   string
	}{
		{appapi.LinkActParams{Project: "a", Action: "approve", ID: h1.ID}, "not-held"},
		{appapi.LinkActParams{Project: "a", Action: "decline-file", ID: f.Transfer}, "not-offered"},
		{appapi.LinkActParams{Project: "a", Action: "send", ID: "b"}, proto.ErrBadParams},
		{appapi.LinkActParams{Project: "a", Action: "resume", ID: "b"}, proto.ErrBadParams},
		{appapi.LinkActParams{Project: "a", Action: "drop", ID: h1.ID, Allow: "both"}, proto.ErrBadParams},
		{appapi.LinkActParams{Project: "a", Action: "decline", ID: "R-0123456789ABCDEF-1"}, "unknown-request"},
		{map[string]string{"project": "a", "action": "send", "id": "b", "text": "hi"}, appapi.ErrBadParams},
	} {
		if err := c.Call(proto.MethodLinkAct, bad.params, nil); code(err) != bad.code {
			t.Errorf("link.act %+v: %v, want %s", bad.params, err, bad.code)
		}
	}
	journal, _ := os.ReadFile(a.Path("JOURNAL.md"))
	for _, want := range []string{" human (app) link.approve b " + h1.ID, " human (app) link.drop b " + h2.ID,
		" human (app) link.file-accept b " + f.Transfer} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	// A share request that isn't there: the server's refusal, its code
	// kept.
	if err := c.Call(proto.MethodPeerShareAnswer, appapi.ShareAnswerParams{ID: "S-0123456789ABCDEF-1"}, nil); code(err) != "unknown-request" {
		t.Errorf("peer.share-answer: %v", err)
	}
}

// TestBridgeGuard: the app lists the guard's refusals with the whole
// command and answers one at a time (app API 18), as the console's
// Guard refusals do: the allowance in the project's settings or all
// projects', journaled as "human (app)" without the command; the floor's
// is only kept; an answer given elsewhere is gone. An agent's bridge
// gets neither.
func TestBridgeGuard(t *testing.T) {
	p := testPaths(t)
	pr, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	const cmd = "curl -H 'Authorization: Bearer sekrit' https://registry/x"
	record := func(e guardlog.Entry) guardlog.Entry {
		t.Helper()
		e, _, err := guardlog.Record(p.Home, e, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	here := record(guardlog.Entry{Project: "web", Thread: "t-0001", Rule: "credentials", Tool: "Bash", Summary: "an HTTP call with credentials", Subject: cmd})
	all := record(guardlog.Entry{Project: "web", Thread: "t-0002", Rule: "worktree-only", Tool: "Bash", Summary: "cp outside", Subject: "cp a /opt/b"})
	floor := record(guardlog.Entry{Project: "web", Thread: "t-0003", Rule: "merge", Tool: "Bash", Summary: "gh pr merge", Subject: "gh pr merge 1", Floor: true})
	startServer(t, p)

	agent, err := bridgeWith(t, appapi.Options{Paths: p, Caller: caller.Caller{Kind: caller.Thread, Project: "web", Thread: "t-0001"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Call(appapi.MethodGuardRefusals, nil, nil); code(err) != appapi.ErrHumanOnly {
		t.Errorf("an agent's guard.refusals: %v", err)
	}
	if err := agent.Call(appapi.MethodGuardDecide, appapi.GuardDecideParams{ID: here.ID, How: "here"}, nil); code(err) != appapi.ErrHumanOnly {
		t.Errorf("an agent's guard.decide: %v", err)
	}

	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.Hello.Methods, appapi.MethodGuardDecide) {
		t.Errorf("hello methods %v", c.Hello.Methods)
	}
	var list appapi.GuardRefusals
	if err := c.Call(appapi.MethodGuardRefusals, nil, &list); err != nil || len(list.Refusals) != 3 {
		t.Fatalf("guard.refusals: %+v %v", list, err)
	}
	if r := list.Refusals[2]; r.ID != here.ID || r.Subject != cmd || r.Thread != "t-0001" || r.Rule != "credentials" || r.Count != 1 {
		t.Errorf("refusal %+v", r)
	}
	for _, bad := range []struct {
		params any
		code   string
	}{
		{appapi.GuardDecideParams{ID: here.ID, How: "always"}, appapi.ErrBadParams},
		{map[string]string{"id": here.ID, "how": "here", "subject": "rm -rf /"}, appapi.ErrBadParams},
		{appapi.GuardDecideParams{ID: "nope", How: "keep"}, appapi.ErrGone},
		{appapi.GuardDecideParams{ID: floor.ID, How: "all"}, appapi.ErrGuardRefused},
	} {
		if err := c.Call(appapi.MethodGuardDecide, bad.params, nil); code(err) != bad.code {
			t.Errorf("guard.decide %+v: %v, want %s", bad.params, err, bad.code)
		}
	}
	var done appapi.GuardDecided
	if err := c.Call(appapi.MethodGuardDecide, appapi.GuardDecideParams{ID: here.ID, How: "here"}, &done); err != nil || !strings.Contains(done.Message, "allowed for this project") {
		t.Fatalf("guard.decide here: %+v %v", done, err)
	}
	if err := c.Call(appapi.MethodGuardDecide, appapi.GuardDecideParams{ID: all.ID, How: "all"}, &done); err != nil || !strings.Contains(done.Message, "allowed for "+config.AllProjectsName) {
		t.Fatalf("guard.decide all: %+v %v", done, err)
	}
	if err := c.Call(appapi.MethodGuardDecide, appapi.GuardDecideParams{ID: floor.ID, How: "keep"}, &done); err != nil || !strings.Contains(done.Message, "keeps refusing") {
		t.Fatalf("guard.decide keep: %+v %v", done, err)
	}
	if err := c.Call(appapi.MethodGuardDecide, appapi.GuardDecideParams{ID: here.ID, How: "keep"}, nil); code(err) != appapi.ErrGone {
		t.Errorf("answered twice: %v", err)
	}
	if err := c.Call(appapi.MethodGuardRefusals, nil, &list); err != nil || len(list.Refusals) != 0 {
		t.Errorf("guard.refusals after: %+v %v", list, err)
	}
	cfg, _ := config.Load()
	if s, _ := cfg.Safety("web"); !slices.Contains(config.OwnAllows(s.GuardAllow), config.GuardAllow{Rule: "credentials", Command: cmd, Exact: true}) {
		t.Errorf("project's allowances %+v", s.GuardAllow)
	}
	if a, _ := cfg.AllProjects(); !slices.Contains(a.GuardAllow, config.GuardAllow{Rule: "worktree-only", Command: "cp a /opt/b", Exact: true}) {
		t.Errorf("all projects' allowances %+v", a.GuardAllow)
	}
	journal, _ := os.ReadFile(pr.Path("JOURNAL.md"))
	for _, want := range []string{" human (app) guard.allow t-0001 credentials Bash: an HTTP call with credentials (this project)",
		" human (app) guard.allow t-0002 worktree-only Bash: cp outside (" + config.AllProjectsName + ")",
		" human (app) guard.keep t-0003 merge Bash: gh pr merge"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	if strings.Contains(string(journal), "sekrit") {
		t.Errorf("the command reached the journal:\n%s", journal)
	}
}

// TestBridgeProjectSettings: each project's active, paused and pinned
// reach the app (app API 4), and no other setting does.
func TestBridgeProjectSettings(t *testing.T) {
	p := testPaths(t)
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		if _, err := project.New(project.Options{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	for _, set := range []struct {
		slug, key string
		value     bool
	}{{"alpha", "pinned", true}, {"beta", "paused", true}, {"gamma", "active", false}, {"gamma", "yolo", true}} {
		if err := config.SetProject(set.slug, set.key, set.value); err != nil {
			t.Fatal(err)
		}
	}
	startServer(t, p)
	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	var raw json.RawMessage
	if _, err := c.Stream(proto.MethodProjectsWatch, appapi.ProjectsWatchParams{}, &raw); err != nil {
		t.Fatal(err)
	}
	var ps appapi.Projects
	if err := json.Unmarshal(raw, &ps); err != nil {
		t.Fatal(err)
	}
	type flags struct{ active, paused, pinned bool }
	got := map[string]flags{}
	for _, pr := range ps.Projects {
		got[pr.Slug] = flags{pr.Active, pr.Paused, pr.Pinned}
	}
	want := map[string]flags{"alpha": {true, false, true}, "beta": {true, true, false}, "gamma": {false, false, false}}
	if !maps.Equal(got, want) {
		t.Errorf("projects %+v, want %+v", got, want)
	}
	if strings.Contains(string(raw), "yolo") || strings.Contains(string(raw), "safety") {
		t.Errorf("a setting crossed: %s", raw)
	}
}

// TestBridgeKeys: the app's keys reach a session as the human's, and a
// shell session started beside it echoes them.
func TestBridgeKeys(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	h, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var started proto.SessionStartResult
	if err := h.Call(proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{"/bin/cat"}}, &started); err != nil {
		t.Fatal(err)
	}
	id := started.Session.ID
	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	var w appapi.Watch
	st, err := c.Stream(proto.MethodSessionWatch, appapi.SessionIDParams{ID: id}, &w)
	if err != nil || w.Session.ID != id {
		t.Fatalf("session.watch: %+v %v", w, err)
	}
	if err := c.Call(proto.MethodSessionKeys, appapi.KeysParams{ID: id, Data: "hello app\r"}, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var r proto.SessionReadResult
		if err := h.Call(proto.MethodSessionRead, proto.SessionReadParams{ID: id}, &r); err == nil && strings.Count(r.Text, "hello app") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the keys didn't reach the session")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The session ends: so does its watch, with its last state.
	if err := h.Call(proto.MethodSessionStop, proto.SessionIDParams{ID: id}, nil); err != nil {
		t.Fatal(err)
	}
	for {
		var next appapi.Watch
		_, err := st.Next(&next)
		if err == apptest.ErrEnd {
			break
		}
		if err != nil {
			t.Fatalf("watch after stop: %v", err)
		}
	}
}

// TestBridgePane: the hello names the server's caps; pane.vt streams a
// pane (a repaint, then output) and the app's keys go in through
// pane.input, into that stream only; transcript.watch answers a
// session's conversation status (unmapped: no agent here). The bridge
// acks for the app.
func TestBridgePane(t *testing.T) {
	p := testPaths(t)
	startServer(t, p)
	h, err := client.Dial(p, proto.KindControl)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var started proto.SessionStartResult
	if err := h.Call(proto.MethodSessionStart, proto.SessionStartParams{Argv: []string{"/bin/cat"}}, &started); err != nil {
		t.Fatal(err)
	}
	id := started.Session.ID
	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if caps := c.Hello.Server.Caps; !slices.Contains(caps, proto.CapPaneVT) || !slices.Contains(caps, proto.CapTranscript) {
		t.Fatalf("hello caps %v", caps)
	}

	var pr appapi.PaneResult
	pane, err := c.Stream(proto.MethodPaneVT, appapi.PaneParams{Session: id}, &pr)
	if err != nil || pr.Session.ID != id {
		t.Fatalf("pane.vt: %+v %v", pr, err)
	}
	var ev appapi.PaneEvent
	if name, err := pane.Next(&ev); err != nil || name != proto.EventPaneRepaint || ev.Cols == 0 {
		t.Fatalf("first event %q %+v %v", name, ev, err)
	}
	if err := c.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte("hello pane\r")}, nil); err != nil {
		t.Fatal(err)
	}
	var out []byte
	for !strings.Contains(string(out), "hello pane") {
		var ev appapi.PaneEvent
		name, err := pane.Next(&ev)
		if err != nil {
			t.Fatalf("pane after input: %v (%q)", err, out)
		}
		if name == proto.EventPaneOutput || name == proto.EventPaneRepaint {
			out = append(out, ev.Data...)
		}
	}
	if err := c.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID() + 100, Repaint: true}, nil); code(err) != appapi.ErrBadParams {
		t.Errorf("input into no stream: %v", err)
	}

	// More than the server's window: only the bridge's acks let it all
	// through (cat and the terminal's echo double it).
	line := strings.Repeat("x", 99) + "\r"
	for range 20 {
		if err := c.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte(strings.Repeat(line, 100))}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: pane.ID(), Input: []byte("END-MARK\r")}, nil); err != nil {
		t.Fatal(err)
	}
	total, tail := 0, ""
	for !strings.Contains(tail, "END-MARK") {
		var ev appapi.PaneEvent
		name, err := pane.Next(&ev)
		if err != nil {
			t.Fatalf("pane after %d bytes: %v", total, err)
		}
		if name == proto.EventPaneRepaint {
			tail = ""
		}
		total += len(ev.Data)
		tail = string(append([]byte(tail), ev.Data...))
		tail = tail[max(0, len(tail)-64):]
	}
	if total < proto.PaneVTWindow {
		t.Errorf("%d bytes came, under the window %d: the test proves nothing", total, proto.PaneVTWindow)
	}

	var tr appapi.TranscriptResult
	ts, err := c.Stream(proto.MethodTranscriptWatch, appapi.TranscriptParams{Session: id}, &tr)
	if err != nil || tr.Session.ID != id || tr.Status.State == "" {
		t.Fatalf("transcript.watch: %+v %v", tr, err)
	}
	// Input into a stream that isn't a pane's.
	if err := c.Call(appapi.MethodPaneInput, appapi.PaneInputParams{ID: ts.ID(), Repaint: true}, nil); code(err) != appapi.ErrBadParams {
		t.Errorf("pane input into a transcript: %v", err)
	}
	ts.Cancel()

	// The session ends: the pane says so and ends.
	if err := h.Call(proto.MethodSessionStop, proto.SessionIDParams{ID: id}, nil); err != nil {
		t.Fatal(err)
	}
	closed := false
	for {
		var ev appapi.PaneEvent
		name, err := pane.Next(&ev)
		if err == apptest.ErrEnd {
			break
		}
		if err != nil {
			t.Fatalf("pane after stop: %v", err)
		}
		closed = closed || name == proto.EventPaneClosed
	}
	if !closed {
		t.Error("no pane.closed before the end")
	}
}

// TestBridgeSideActs: the sidebar's and the sessions' acts (app API
// 22) with the TUI's checks, journaled as "human (app)": a project
// pinned and unpinned, one made (and refused when its slug is taken or
// its repo isn't a folder), a message written on a link (text only),
// and an adopt's and remote control's refusals for a session that
// isn't there.
func TestBridgeSideActs(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"),
		[]byte("[projects.a.links]\nb = \"both\"\n\n[projects.b.links]\na = \"both\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := project.New(project.Options{Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.New(project.Options{Slug: "b"}); err != nil {
		t.Fatal(err)
	}
	startServer(t, p)
	c, err := bridge(t, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.Hello.Server.Caps, proto.CapSideActs) {
		t.Errorf("caps %v", c.Hello.Server.Caps)
	}
	var pinned appapi.ProjectPinned
	for _, step := range []struct{ on, changed bool }{{true, true}, {true, false}, {false, true}} {
		if err := c.Call(proto.MethodProjectPin, appapi.ProjectPinParams{Project: "a", Pinned: step.on}, &pinned); err != nil || pinned.Changed != step.changed {
			t.Fatalf("project.pin %v: %+v %v", step.on, pinned, err)
		}
	}
	var made appapi.ProjectNew
	repo := t.TempDir()
	if err := c.Call(proto.MethodProjectNew, appapi.ProjectNewParams{Project: "Demo App", Repos: []string{repo}}, &made); err != nil || made.Project != "demo-app" {
		t.Fatalf("project.new: %+v %v", made, err)
	}
	if pr, err := project.Open("demo-app"); err != nil || len(pr.Repos) != 1 {
		t.Fatalf("demo-app: %+v %v", pr, err)
	}
	var sent appapi.LinkActResult
	if err := c.Call(proto.MethodLinkSend, appapi.LinkSendParams{Project: "a", Peer: "b", Text: "Hello from the app"}, &sent); err != nil ||
		!strings.HasPrefix(sent.Words, "sent M-") || !strings.HasSuffix(sent.Words, " to b") {
		t.Fatalf("link.send: %+v %v", sent, err)
	}
	for _, bad := range []struct {
		method string
		params any
		code   string
	}{
		{proto.MethodProjectNew, appapi.ProjectNewParams{Project: "demo-app"}, "project-exists"},
		{proto.MethodProjectNew, appapi.ProjectNewParams{Project: "x", Repos: []string{filepath.Join(repo, "nope")}}, "invalid-repo"},
		{proto.MethodProjectPin, appapi.ProjectPinParams{Project: "nope", Pinned: true}, "unknown-project"},
		{proto.MethodLinkSend, appapi.LinkSendParams{Project: "a", Peer: "b", Text: " "}, proto.ErrBadParams},
		{proto.MethodLinkSend, appapi.LinkSendParams{Project: "a", Peer: "demo-app", Text: "hi"}, "no-link"},
		{proto.MethodLinkSend, map[string]string{"project": "a", "peer": "b", "text": "hi", "file": "/etc/passwd"}, appapi.ErrBadParams},
		{proto.MethodSessionAdoptRequest, appapi.AdoptRequestParams{ID: "s-404", Project: "a"}, proto.ErrUnknownSession},
		{proto.MethodSessionRemote, appapi.RemoteParams{ID: "s-404", On: true}, proto.ErrUnknownSession},
	} {
		if err := c.Call(bad.method, bad.params, nil); code(err) != bad.code {
			t.Errorf("%s %+v: %v, want %s", bad.method, bad.params, err, bad.code)
		}
	}
	var adopted appapi.AdoptRequested
	if err := c.Call(proto.MethodSessionAdoptRequest, appapi.AdoptRequestParams{ID: "s-404", Project: "a", Withdraw: true}, &adopted); err != nil || adopted.Asked {
		t.Fatalf("withdraw: %+v %v", adopted, err)
	}
	journal, _ := os.ReadFile(a.Path("JOURNAL.md"))
	for _, want := range []string{" human (app) project.pin a", " human (app) project.unpin a", " human (app) link.send b",
		" human (app) thread.adopt.withdraw s-404"} {
		if !strings.Contains(string(journal), want) {
			t.Errorf("journal lacks %q:\n%s", want, journal)
		}
	}
	if pr, _ := project.Open("demo-app"); pr != nil {
		if j, _ := os.ReadFile(pr.Path("JOURNAL.md")); !strings.Contains(string(j), " human (app) project.new demo-app") {
			t.Errorf("demo-app's journal:\n%s", j)
		}
	}
}
