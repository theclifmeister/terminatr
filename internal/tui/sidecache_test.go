package tui

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
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

// TestSideCacheChanges: a load with nothing changed reads nothing and
// says so, keeping the same list; a thread's new status, a new thread,
// a new project and an archive all show on the next load.
func TestSideCacheChanges(t *testing.T) {
	p, r := cacheProject(t)
	var sc sideCache
	paths := server.Paths{Sessions: t.TempDir() + "/sessions"}
	list, changed := sc.load(paths)
	if !changed || len(list) != 1 || len(list[0].Threads) != 1 {
		t.Fatalf("first load: %v %+v", changed, list)
	}
	again, changed := sc.load(paths)
	if changed || &again[0] != &list[0] {
		t.Fatalf("an idle load changed: %v", changed)
	}

	if _, err := thread.UpdateStatus(p, r.ID, func(st *thread.Status) error { st.Activity = "testing the cache"; return nil }); err != nil {
		t.Fatal(err)
	}
	list, changed = sc.load(paths)
	if !changed || list[0].Threads[0].Status == nil || list[0].Threads[0].Status.Activity != "testing the cache" {
		t.Fatalf("the new status doesn't show: %v %+v", changed, list[0].Threads[0].Status)
	}

	if _, err := thread.Create(p, thread.Record{Title: "more work"}); err != nil {
		t.Fatal(err)
	}
	if list, changed = sc.load(paths); !changed || len(list[0].Threads) != 2 {
		t.Fatalf("the new thread doesn't show: %v %d", changed, len(list[0].Threads))
	}

	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	if list, changed = sc.load(paths); !changed || len(list) != 2 || list[1].Slug != "other" {
		t.Fatalf("the new project doesn't show: %v %+v", changed, list)
	}

	if err := config.SetProject("other", "archived", true); err != nil {
		t.Fatal(err)
	}
	if list, changed = sc.load(paths); !changed || len(list) != 1 || list[0].Slug != "demo" {
		t.Fatalf("the archived project still shows: %v %+v", changed, list)
	}
	if _, changed = sc.load(paths); changed {
		t.Error("an idle load after the changes changed")
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
	c, err := newClient(server.Paths{}, log.New(io.Discard, "", 0))
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
	d := ic.load(server.Paths{}, s, []proto.SessionInfo{s})
	if d == nil || d.rec == nil || d.rec.ID != r.ID {
		t.Fatalf("no thread data: %+v", d)
	}
	s.State = "idle"
	again := ic.load(server.Paths{}, s, []proto.SessionInfo{s})
	if again.status != d.status || again.session.State != "idle" {
		t.Errorf("an unchanged thread was read again, or the session not taken: %+v", again.session)
	}
	if _, err := thread.UpdateStatus(p, r.ID, func(st *thread.Status) error { st.Activity = "panel"; return nil }); err != nil {
		t.Fatal(err)
	}
	if d := ic.load(server.Paths{}, s, []proto.SessionInfo{s}); d.status == nil || d.status.Activity != "panel" {
		t.Errorf("the new status doesn't show: %+v", d.status)
	}
	co := proto.SessionInfo{ID: "s-2", Role: proto.RoleCoordinator, Project: p.Slug}
	if d := ic.load(server.Paths{}, co, []proto.SessionInfo{s, co}); d == nil || d.watch == nil || len(d.watch.Threads) != 1 {
		t.Errorf("no coordinator watch: %+v", d)
	}
}
