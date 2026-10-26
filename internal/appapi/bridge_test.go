package appapi_test

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/appapi"
	"github.com/theclifmeister/terminatr/internal/appapi/apptest"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/home/hometest"
	"github.com/theclifmeister/terminatr/internal/plat/ipc/ipctest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/service/servicetest"
	"github.com/theclifmeister/terminatr/internal/tasks"
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
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	served := make(chan error, 1)
	go func() {
		err := appapi.Serve(inR, outW, appapi.Options{Paths: p, Autostart: autostart})
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
		len(h.Methods) != len(appapi.MethodNames()) || len(h.Capabilities) == 0 {
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
