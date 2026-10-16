package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/changes"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modbus"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

func writeIn(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// changesRepo is a clone of a bare origin with main and develop pushed.
func changesRepo(t *testing.T, root, name string) string {
	t.Helper()
	origin, repo := filepath.Join(root, name+".git"), filepath.Join(root, name)
	gittest.Run(t, root, "init", "--bare", "-q", origin)
	gittest.Run(t, root, "clone", "-q", origin, repo)
	writeIn(t, repo, "a.txt", "a\n")
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "c")
	gittest.Run(t, repo, "push", "-q", "origin", "HEAD:main")
	gittest.Run(t, repo, "remote", "set-head", "origin", "main")
	gittest.Run(t, repo, "checkout", "-q", "-b", "develop")
	writeIn(t, repo, "dev.txt", "d\n")
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-q", "-m", "dev")
	gittest.Run(t, repo, "push", "-q", "origin", "develop")
	gittest.Run(t, repo, "checkout", "-q", "main")
	return repo
}

// changesThread records a thread as tm thread start (or adopt) would.
func changesThread(t *testing.T, p *project.Project, rec thread.Record) *thread.Record {
	t.Helper()
	r, err := thread.Create(p, thread.Record{Title: "x", State: thread.Running})
	if err != nil {
		t.Fatal(err)
	}
	r, err = thread.Update(p, r.ID, func(x *thread.Record) error {
		x.Repo, x.Base, x.Branch, x.Worktree, x.Adopted, x.Checkout = rec.Repo, rec.Base, rec.Branch, rec.Worktree, rec.Adopted, rec.Checkout
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// changesFixture is the lab mod enabled for project demo with
// project_read, and demo's threads: one started from origin/main, one
// with --base origin/develop, one in a second repo of the project, one
// adopted in a repo's main checkout, and one without a repo.
type changesFixture struct {
	s                                       *Server
	p                                       *project.Project
	root                                    string
	started, based, second, adopted, norepo *thread.Record
}

func newChangesFixture(t *testing.T) changesFixture {
	t.Helper()
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	dir := filepath.Join(tmHome, "mods", "lab")
	os.MkdirAll(dir, 0o700)
	for name, text := range map[string]string{"mod.toml": labManifest, "main.js": ""} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[mods.lab]\nenabled = true\ngrants = [\"project_read\"]\nprojects = [\"demo\"]\n"
	if err := os.WriteFile(filepath.Join(tmHome, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	p := newWatchProject(t)
	if _, err := project.New(project.Options{Slug: "other"}); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	one, two := changesRepo(t, root, "one"), changesRepo(t, root, "two")
	wts := filepath.Join(tmHome, "worktrees", "demo")
	f := changesFixture{s: &Server{log: log.New(os.Stderr, "", 0), modEv: newModEvents()}, p: p, root: root}

	wt := filepath.Join(wts, "t-0001-x")
	gittest.Run(t, one, "worktree", "add", "-q", "-b", "tm/demo/t-0001-x", wt, "origin/main")
	writeIn(t, wt, "a.txt", "a\nb\n")
	writeIn(t, wt, ".env", "SECRET=1\n")
	f.started = changesThread(t, p, thread.Record{Repo: one, Base: "origin/main", Branch: "tm/demo/t-0001-x", Worktree: wt})

	wt = filepath.Join(wts, "t-0002-x")
	gittest.Run(t, one, "worktree", "add", "-q", "-b", "tm/demo/t-0002-x", wt, "origin/develop")
	writeIn(t, wt, "b.txt", "b\n")
	gittest.Run(t, wt, "add", ".")
	gittest.Run(t, wt, "commit", "-q", "-m", "b")
	f.based = changesThread(t, p, thread.Record{Repo: one, Base: "origin/develop", Branch: "tm/demo/t-0002-x", Worktree: wt})

	wt = filepath.Join(wts, "t-0003-x")
	gittest.Run(t, two, "worktree", "add", "-q", "-b", "tm/demo/t-0003-x", wt, "origin/main")
	writeIn(t, wt, "two.txt", "1\n2\n")
	f.second = changesThread(t, p, thread.Record{Repo: two, Base: "origin/main", Branch: "tm/demo/t-0003-x", Worktree: wt})

	writeIn(t, two, "a.txt", "a\nadopted\n")
	f.adopted = changesThread(t, p, thread.Record{Repo: two, Branch: "main", Worktree: two, Adopted: true, Checkout: true})

	wt = filepath.Join(wts, "t-0005-x")
	os.MkdirAll(wt, 0o755)
	f.norepo = changesThread(t, p, thread.Record{Worktree: wt})
	return f
}

func (f changesFixture) call(params any) (*changes.Changeset, error) {
	raw, _ := json.Marshal(params)
	got, err := f.s.CallMod(context.Background(), "lab", "thread.changes", raw)
	if err != nil {
		return nil, err
	}
	return got.(*changes.Changeset), nil
}

func files(cs *changes.Changeset) string {
	var out []string
	for _, f := range cs.Files {
		out = append(out, f.Path+":"+f.Status+":"+f.Bucket)
	}
	return strings.Join(out, " ")
}

// TestModThreadChanges: thread.changes reads each kind of thread's
// worktree (started, --base, a second repo, adopted, no repo), answers
// one file's patch, refuses paths outside the worktree or secret, and
// threads or projects the mod can't read; no absolute path is in it.
func TestModThreadChanges(t *testing.T) {
	f := newChangesFixture(t)
	type args = map[string]any
	cs, err := f.call(args{"project": "demo", "thread": f.started.ID})
	if err != nil {
		t.Fatal(err)
	}
	if cs.State != changes.StateDirty || cs.Base != "origin/main" || files(cs) != ".env:?:uncommitted a.txt:M:uncommitted" || cs.Totals.Add != 2 {
		t.Fatalf("started: %+v", cs)
	}
	js, _ := json.Marshal(cs)
	if strings.Contains(string(js), f.root) || strings.Contains(string(js), os.Getenv("TERMINATR_HOME")) {
		t.Fatalf("an absolute path in %s", js)
	}
	cs, err = f.call(args{"project": "demo", "thread": f.based.ID})
	if err != nil || cs.State != changes.StateClean || cs.Base != "origin/develop" || cs.Ahead != 1 || cs.Unpushed != 1 || files(cs) != "b.txt:A:unpushed" {
		t.Fatalf("--base: %+v %v", cs, err)
	}
	cs, err = f.call(args{"project": "demo", "thread": f.second.ID, "options": args{"mode": "uncommitted"}})
	if err != nil || cs.Mode != changes.ModeUncommitted || files(cs) != "two.txt:?:uncommitted" || cs.Totals.Add != 2 {
		t.Fatalf("second repo: %+v %v", cs, err)
	}
	cs, err = f.call(args{"project": "demo", "thread": f.adopted.ID})
	if err != nil || cs.Branch != "main" || cs.Base != "origin/main" || files(cs) != "a.txt:M:uncommitted" {
		t.Fatalf("adopted: %+v %v", cs, err)
	}
	cs, err = f.call(args{"project": "demo", "thread": f.norepo.ID})
	if err != nil || cs.State != changes.StateNone || len(cs.Files) != 0 {
		t.Fatalf("no repo: %+v %v", cs, err)
	}

	// One file, flat options too, with its patch.
	cs, err = f.call(args{"project": "demo", "thread": f.started.ID, "path": "a.txt", "patch": true})
	if err != nil || files(cs) != "a.txt:M:uncommitted" || cs.Totals.Files != 2 || !strings.Contains(cs.Patch, "+b\n") {
		t.Fatalf("patch: %+v %v", cs, err)
	}
	cs, err = f.call(args{"project": "demo", "thread": f.started.ID, "options": args{"path": "a.txt"}})
	if err != nil || files(cs) != "a.txt:M:uncommitted" || cs.Patch != "" {
		t.Fatalf("path without patch: %+v %v", cs, err)
	}

	code := func(err error) int {
		if e, ok := err.(*modbroker.Error); ok {
			return e.Code
		}
		return 0
	}
	for _, c := range []struct {
		name   string
		params args
		code   int
	}{
		{".env", args{"project": "demo", "thread": f.started.ID, "path": ".env", "patch": true}, modbroker.CodeDenied},
		{"outside", args{"project": "demo", "thread": f.started.ID, "path": "../t-0002-x/b.txt", "patch": true}, modbroker.CodeDenied},
		{"absolute", args{"project": "demo", "thread": f.started.ID, "path": filepath.Join(f.root, "one", "a.txt"), "patch": true}, modbroker.CodeDenied},
		{".git", args{"project": "demo", "thread": f.started.ID, "path": ".git", "patch": true}, modbroker.CodeDenied},
		{"no repo", args{"project": "demo", "thread": f.norepo.ID, "path": "a.txt"}, modbroker.CodeNotFound},
		{"no thread", args{"project": "demo", "thread": "t-0099"}, modbroker.CodeNotFound},
		{"mode", args{"project": "demo", "thread": f.started.ID, "mode": "all"}, modbroker.CodeBadParams},
		{"other project", args{"project": "other", "thread": f.started.ID}, modbroker.CodeDenied},
	} {
		if _, err := f.call(c.params); code(err) != c.code {
			t.Errorf("%s: %v, want code %d", c.name, err, c.code)
		}
	}
}

// TestModThreadChangesEvent: the look publishes thread.changes for a
// thread whose session is live, only while a mod listens to it; it asks
// git at most every modChangesEvery, reads in full only when the
// fingerprint moved, and publishes only what differs.
func TestModThreadChangesEvent(t *testing.T) {
	f := newChangesFixture(t)
	e := f.s.modEv
	reads := 0
	e.changes.read = func(ctx context.Context, dir string, o changes.Options) (*changes.Changeset, error) {
		reads++
		return changes.Read(ctx, dir, o)
	}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	e.changes.now = func() time.Time { return now }
	live := []proto.SessionInfo{
		{ID: "s-1", Project: "demo", Thread: f.started.ID, Role: proto.RoleThread, State: "working"},
		{ID: "s-2", Project: "demo", Thread: f.norepo.ID, Role: proto.RoleThread, State: "working"},
	}
	tr := modbus.NewTracker(e.bus, nil)
	look := func(sessions []proto.SessionInfo) {
		tr.Project(f.p.Slug, projectState(f.p, sessions, "", f.s.liveChanges(f.p)))
		e.changes.sweep()
	}

	// No mod listens: no git.
	other := e.bus.Subscribe("other", modbus.Options{Grants: []string{modbus.GrantProjectRead}, Events: []string{modbus.TaskChanged}})
	look(live)
	if reads != 0 {
		t.Fatalf("%d reads with no mod listening", reads)
	}
	other.Close()
	tr.Reset()

	sub := e.bus.Subscribe("lab", modbus.Options{Grants: []string{modbus.GrantProjectRead}, Events: []string{modbus.ThreadChanges}})
	look(live) // the baseline
	if reads != 1 {
		t.Fatalf("baseline: %d reads, want 1 (the no-repo thread has none)", reads)
	}
	now = now.Add(time.Second)
	writeIn(t, f.started.Worktree, "a.txt", "a\nb\nc\n")
	look(live) // within modChangesEvery: not asked
	now = now.Add(modChangesEvery)
	look(live) // moved: read, published
	now = now.Add(modChangesEvery)
	look(live) // same: not read
	if reads != 2 {
		t.Fatalf("%d reads, want 2", reads)
	}
	gittest.Run(t, f.started.Worktree, "add", ".")
	gittest.Run(t, f.started.Worktree, "commit", "-q", "-m", "c")
	now = now.Add(modChangesEvery)
	look(live)
	// Its session ended: nothing, and no git.
	now = now.Add(modChangesEvery)
	writeIn(t, f.started.Worktree, "a.txt", "gone\n")
	look(nil)
	if reads != 3 {
		t.Fatalf("%d reads, want 3", reads)
	}
	var got []modbus.Changes
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		ev, err := sub.Next(ctx)
		cancel()
		if err != nil {
			break
		}
		if ev.Name == modbus.ThreadChanges {
			got = append(got, ev.Data.(modbus.Changes))
		}
	}
	if len(got) != 2 || got[0].Thread != f.started.ID || !got[0].Dirty || got[0].Add != 3 || got[0].Files != 2 ||
		got[1].Dirty || got[1].Ahead != 1 || got[1].Unpushed != 1 || got[1].Files != 2 {
		t.Fatalf("events %+v", got)
	}
}
