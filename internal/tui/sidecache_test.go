package tui

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/tasks"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/ipc/ipctest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// cacheProject is a project demo with one thread, under a fresh home.
func cacheProject(t *testing.T) (*project.Project, *thread.Record) {
	t.Helper()
	t.Setenv("TERMINATR_HOME", t.TempDir())
	p, err := project.New(project.Options{Slug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := thread.Create(p, thread.Record{Title: "work"})
	if err != nil {
		t.Fatal(err)
	}
	return p, r
}

// startServer runs a server for the test, under a fresh home and
// socket, and stops it at the test's end.
func startServer(t *testing.T) tmclient.Paths {
	t.Helper()
	if os.Getenv("TERMINATR_HOME") == "" {
		t.Setenv("TERMINATR_HOME", t.TempDir())
	}
	sock := ipctest.ShortTempDir(t, "tmt")
	t.Setenv("TERMINATR_SOCKET", filepath.Join(sock, "tm.sock"))
	t.Setenv("TERMINATR_LAUNCHD", "off")
	t.Setenv(caller.EnvSession, "")
	paths, err := tmclient.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.Options{Paths: paths, Log: log.New(io.Discard, "", 0), Bin: "/nonexistent/tm",
			Env: []string{"PATH=/usr/bin:/bin"}})
	}()
	t.Cleanup(func() { stop(); <-done })
	for deadline := time.Now().Add(5 * time.Second); ; {
		c, err := tmclient.Dial(paths, proto.KindControl)
		if err == nil {
			c.Close()
			return paths
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSideFeed: the sidebar's projects come from the server's
// projects.watch: the projects now at first, then a thread's new status,
// a new thread, a new project and an archive as they happen, each taken
// once.
func TestSideFeed(t *testing.T) {
	p, r := cacheProject(t)
	t.Setenv("TERMINATR_WATCH_POLL", "50ms")
	paths := startServer(t)
	feed, list := watchSide(paths, false)
	defer feed.close()
	if len(list) != 1 || list[0].Slug != "demo" || len(list[0].Threads) != 1 || list[0].Threads[0].ID != r.ID {
		t.Fatalf("first projects: %+v", list)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{})
	go func() {
		feed.run(ctx, paths)
		close(ran)
	}()
	defer func() {
		cancel()
		<-ran
	}()
	await := func(what string, ok func([]ProjectData) bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if list, fresh := feed.take(); fresh && ok(list) {
				return
			}
		}
		t.Fatalf("%s doesn't show", what)
	}

	if _, err := thread.UpdateStatus(p, r.ID, func(st *thread.Status) error { st.Activity = "testing the feed"; return nil }); err != nil {
		t.Fatal(err)
	}
	await("the new status", func(l []ProjectData) bool {
		st := l[0].Threads[0].Status
		return st != nil && st.Activity == "testing the feed"
	})
	if _, fresh := feed.take(); fresh {
		t.Error("the same projects taken twice")
	}

	if _, err := thread.Create(p, thread.Record{Title: "more work"}); err != nil {
		t.Fatal(err)
	}
	await("the new thread", func(l []ProjectData) bool { return len(l[0].Threads) == 2 })

	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	await("the new project", func(l []ProjectData) bool { return len(l) == 2 && l[1].Slug == "other" })

	if err := config.SetProject("other", "archived", true); err != nil {
		t.Fatal(err)
	}
	await("the archive", func(l []ProjectData) bool { return len(l) == 1 && l[0].Slug == "demo" })
}

// TestSideProjects: the server's projects as the sidebar keeps them:
// the tasks that need you as tasks, the threads with their PRs; none is
// an empty list, not nil (no change).
func TestSideProjects(t *testing.T) {
	if got := sideProjects(nil); got == nil || len(got) != 0 {
		t.Errorf("no projects: %#v", got)
	}
	rec := &thread.Record{ID: "t-0001", Task: "T3"}
	got := sideProjects([]server.SideProject{{Slug: "demo", Questions: 2, PRHostFails: 4,
		NeedsYou: []server.SideTask{{ID: 3, Title: "Check", Status: tasks.Status("review"), Steps: []tasks.Step{{N: 1, Text: "a", Done: true}}}},
		Threads:  []server.SideThread{{Record: rec, Checks: "fail", PR: 9}, {}}}})
	want := []ProjectData{{Slug: "demo", Questions: 2, PRHostFails: 4,
		NeedsYou: []*tasks.Task{{ID: 3, Title: "Check", Status: "review", Steps: []tasks.Step{{N: 1, Text: "a", Done: true}}}},
		Threads:  []ThreadRow{{Record: rec, Checks: "fail", PR: 9}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sideProjects: %+v, want %+v", got, want)
	}
}

// TestStatusCache: a thread session's status is read again when its
// STATUS.md changes, and the same one kept while it doesn't.
func TestStatusCache(t *testing.T) {
	p, r := cacheProject(t)
	sessions := []proto.SessionInfo{{ID: "s-1", Role: proto.RoleThread, Project: p.Slug, Thread: r.ID}}
	sc := statusCache{}
	first := sc.statuses(sessions)["s-1"]
	if first == nil {
		t.Fatal("no status")
	}
	if again := sc.statuses(sessions)["s-1"]; again != first {
		t.Error("an unchanged status was read again")
	}
	if _, err := thread.UpdateStatus(p, r.ID, func(st *thread.Status) error { st.Activity = "now this"; return nil }); err != nil {
		t.Fatal(err)
	}
	if st := sc.statuses(sessions)["s-1"]; st == nil || st.Activity != "now this" {
		t.Errorf("the new status doesn't show: %+v", st)
	}
	sc.statuses(nil)
	if len(sc) != 0 {
		t.Errorf("the gone session's status is kept: %v", sc)
	}
}

// pollClient is a console with a sidebar and a pane of session s-1 for
// applyPoll.
func pollClient(t *testing.T) *client {
	t.Helper()
	c, err := newClient(tmclient.Paths{}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clear(c.panes) // no streams to close
		c.close()
	})
	c.side = &sidebar{}
	c.panes["s-1"] = &pane{info: proto.SessionInfo{ID: "s-1"}}
	return c
}

// woke says whether c was poked to draw, and takes the poke.
func woke(c *client) bool {
	select {
	case <-c.wake:
		return true
	default:
		return false
	}
}

// TestApplyPollIdle: a tick that reads what the last one did doesn't
// draw, until idleRedraw passed, for the ages; one whose sessions,
// projects or pane status changed draws at once.
func TestApplyPollIdle(t *testing.T) {
	c := pollClient(t)
	now := time.Now()
	sessions := []proto.SessionInfo{{ID: "s-1", Role: proto.RoleThread, State: "working"}}
	st := &thread.Status{Activity: "x"}
	tick := func(projects []ProjectData, sessions []proto.SessionInfo, st *thread.Status) *polled {
		return &polled{res: proto.SessionListResult{Sessions: sessions}, connected: true, projects: projects,
			ctxHint: config.DefaultContextHint, statuses: map[string]*thread.Status{"s-1": st}}
	}
	if !c.applyPoll(tick([]ProjectData{{Slug: "demo"}}, sessions, st), now) || !woke(c) {
		t.Fatal("the first tick didn't draw")
	}
	now = now.Add(statePoll)
	if c.applyPoll(tick(nil, append([]proto.SessionInfo(nil), sessions...), st), now) || woke(c) {
		t.Fatal("an idle tick drew")
	}
	now = now.Add(statePoll)
	if !c.applyPoll(tick(nil, sessions, st), now) || !woke(c) {
		t.Error("no idle redraw after idleRedraw")
	}

	now = now.Add(statePoll)
	if !c.applyPoll(tick([]ProjectData{{Slug: "demo"}, {Slug: "new"}}, sessions, st), now) || !woke(c) || len(c.side.projects) != 2 {
		t.Error("new projects didn't draw")
	}
	now = now.Add(statePoll)
	blocked := []proto.SessionInfo{{ID: "s-1", Role: proto.RoleThread, State: "blocked"}}
	if !c.applyPoll(tick(nil, blocked, st), now) || !woke(c) || c.panes["s-1"].info.State != "blocked" {
		t.Error("a session's new state didn't draw")
	}
	now = now.Add(statePoll)
	if !c.applyPoll(tick(nil, blocked, &thread.Status{Activity: "y"}), now) || !woke(c) || c.panes["s-1"].status.Activity != "y" {
		t.Error("a pane's new status didn't draw")
	}
	now = now.Add(statePoll)
	if c.applyPoll(tick(nil, blocked, c.panes["s-1"].status), now) || woke(c) {
		t.Error("an idle tick after the changes drew")
	}
}

// TestInfoCache: the thread panel's data is read again when the
// thread's files change, with the session as the poll gives it, and
// comes out the same while they don't; a coordinator's is its project's
// watch.
func TestInfoCache(t *testing.T) {
	p, r := cacheProject(t)
	s := proto.SessionInfo{ID: "s-1", Role: proto.RoleThread, Project: p.Slug, Thread: r.ID, State: "working"}
	var ic infoCache
	d := ic.load(tmclient.Paths{}, s, []proto.SessionInfo{s})
	if d == nil || d.rec == nil || d.rec.ID != r.ID {
		t.Fatalf("no thread data: %+v", d)
	}
	s.State = "idle"
	again := ic.load(tmclient.Paths{}, s, []proto.SessionInfo{s})
	if again.status != d.status || again.session.State != "idle" {
		t.Errorf("an unchanged thread was read again, or the session not taken: %+v", again.session)
	}
	if _, err := thread.UpdateStatus(p, r.ID, func(st *thread.Status) error { st.Activity = "panel"; return nil }); err != nil {
		t.Fatal(err)
	}
	if d := ic.load(tmclient.Paths{}, s, []proto.SessionInfo{s}); d.status == nil || d.status.Activity != "panel" {
		t.Errorf("the new status doesn't show: %+v", d.status)
	}
	co := proto.SessionInfo{ID: "s-2", Role: proto.RoleCoordinator, Project: p.Slug}
	if d := ic.load(tmclient.Paths{}, co, []proto.SessionInfo{s, co}); d == nil || d.watch == nil || len(d.watch.Threads) != 1 {
		t.Errorf("no coordinator watch: %+v", d)
	}
}

// TestSlowCache: the dashboard's Load reads the mods and agents again
// only when their files change: an unchanged look keeps the registry it
// read, a mod installed in the mods folder or a manifest edited shows
// at once.
func TestSlowCache(t *testing.T) {
	t.Setenv("TERMINATR_HOME", t.TempDir())
	modsDir, err := home.ModsDir()
	if err != nil {
		t.Fatal(err)
	}
	manifest := func(id, name string) {
		t.Helper()
		dir := filepath.Join(modsDir, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, mod.ManifestFile), []byte("id = \""+id+"\"\nname = \""+name+"\"\nversion = \"1.0.0\"\napi = \"1.0\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	userMods := func(d slowData) map[string]string {
		out := map[string]string{}
		for _, m := range d.reg.Mods {
			if !m.Builtin && m.Manifest != nil {
				out[m.ID] = m.Manifest.Name
			}
		}
		return out
	}
	var sc slowCache
	agents := filepath.Join(t.TempDir(), "agents")
	first := sc.load(agents)
	if first.regErr != nil || first.reg == nil {
		t.Fatalf("first load: %v", first.regErr)
	}
	if again := sc.load(agents); again.reg != first.reg {
		t.Error("an unchanged load read the mods again")
	}
	manifest("alpha", "Alpha")
	if got := userMods(sc.load(agents)); got["alpha"] != "Alpha" {
		t.Fatalf("the new mod doesn't show: %v", got)
	}
	manifest("alpha", "Alpha Two")
	if got := userMods(sc.load(agents)); got["alpha"] != "Alpha Two" {
		t.Errorf("the edited manifest doesn't show: %v", got)
	}
}

// fakeWatch is a projects watch: Next gives ups, then fails at once
// when ends, else once closed.
type fakeWatch struct {
	ups    [][]server.SideProject
	ends   bool
	closed chan struct{}
	closes atomic.Int32
}

func (w *fakeWatch) Next() ([]server.SideProject, error) {
	if len(w.ups) > 0 {
		ps := w.ups[0]
		w.ups = w.ups[1:]
		return ps, nil
	}
	if !w.ends {
		<-w.closed
	}
	return nil, io.EOF
}

func (w *fakeWatch) Close() error {
	if w.closes.Add(1) == 1 {
		close(w.closed)
	}
	return nil
}

// TestFollowClosesOnce: the watch is closed once, whether it fails on
// its own or the context ends while it waits, and every update before
// it is passed on.
func TestFollowClosesOnce(t *testing.T) {
	for _, ends := range []bool{true, false} {
		w := &fakeWatch{ups: [][]server.SideProject{{{Slug: "a"}}, {{Slug: "b"}}}, ends: ends, closed: make(chan struct{})}
		ctx, cancel := context.WithCancel(context.Background())
		var got []string
		follow(ctx, w, func(ps []server.SideProject) {
			got = append(got, ps[0].Slug)
			if len(got) == 2 && !ends {
				cancel() // the context ends while the watch waits
			}
		})
		cancel()
		if strings.Join(got, ",") != "a,b" || w.closes.Load() != 1 {
			t.Errorf("ends %v: updates %v, closes %d", ends, got, w.closes.Load())
		}
	}
}
