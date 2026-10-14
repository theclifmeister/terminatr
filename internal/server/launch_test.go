package server

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// slowLaunchServer is a server whose agent launches ask Claude's version
// (its mod is on) through a run that waits for release; entered gets a
// value each time a run starts. Claude is a script that sleeps.
func slowLaunchServer(t *testing.T) (s *Server, entered chan struct{}, release chan struct{}) {
	t.Helper()
	p := testPaths(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(p.Home, "config.toml"), []byte("[agent_mod]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"+testExe), []byte("#!"+testSh(t)+"\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	// The pty finds the command on the server's own PATH: the fake one,
	// never a real Claude.
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.MkdirAll(filepath.Dir(p.Sessions), 0o700); err != nil {
		t.Fatal(err)
	}
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s = &Server{
		opts:      Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=" + nowhere + bin + string(os.PathListSeparator) + "/usr/bin:/bin"}},
		log:       log.New(&testWriter{t: t}, "server: ", log.Lmicroseconds),
		noMods:    true,
		agents:    reg,
		stopReq:   make(chan struct{}),
		sessions:  map[string]*session.Session{},
		records:   map[string]SessionRecord{},
		relaunch:  map[string]bool{},
		sleeping:  map[string]bool{},
		dormant:   map[string]SessionRecord{},
		blocked:   map[string]bool{},
		adoptAsks: map[string]adoptAsk{},
		nextID:    1,
		conns:     map[net.Conn]struct{}{},

		prevProject: map[string]string{},
		protocol:    proto.Protocol,
		modEv:       newModEvents(),
		notices:     modnotify.New(nil),
	}
	s.views = newViews(s, filepath.Join(t.TempDir(), "views.json"), t.Logf)
	entered, release = make(chan struct{}, 16), make(chan struct{})
	s.versions = &agent.Versions{Run: func(ctx context.Context, _ string, _, _ []string) ([]byte, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []byte("9.9.9 (Claude Code)\n"), nil
	}}
	s.modelsCtx, s.modelsStop = context.WithCancel(context.Background())
	t.Cleanup(s.shutdown)
	return s, entered, release
}

// answers fails the test unless f returns within a second.
func answers(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s waited for the launch", what)
	}
}

func waitEntered(t *testing.T, entered chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the launch never asked the version")
	}
}

// TestListDuringSlowLaunch: the slow part of a launch (here the first
// claude --version) runs without s.mu, so session.list and a hook's
// session lookup answer meanwhile (PERF-01); the session shows once the
// launch commits.
func TestListDuringSlowLaunch(t *testing.T) {
	s, entered, release := slowLaunchServer(t)
	type started struct {
		res  any
		perr *proto.Error
	}
	done := make(chan started, 1)
	go func() {
		res, perr := s.startSession(proto.SessionStartParams{Agent: "claude", Cwd: t.TempDir()})
		done <- started{res, perr}
	}()
	waitEntered(t, entered)
	answers(t, "session.list", func() {
		if l := s.list(); len(l.Sessions) != 0 {
			t.Errorf("listed before the launch committed: %+v", l.Sessions)
		}
	})
	answers(t, "a hook's lookup", func() {
		if _, perr := s.session("s-1"); perr == nil {
			t.Error("s-1 known before the launch committed")
		}
	})
	close(release)
	r := <-done
	if r.perr != nil {
		t.Fatalf("session.start: %v", r.perr)
	}
	if id := r.res.(proto.SessionStartResult).Session.ID; id != "s-1" {
		t.Fatalf("started %s", id)
	}
	if l := s.list(); len(l.Sessions) != 1 || l.Sessions[0].ID != "s-1" {
		t.Fatalf("after the launch: %+v", l.Sessions)
	}
	s.mu.Lock()
	_, mod := s.mods["s-1"]
	s.mu.Unlock()
	if !mod {
		t.Error("the mod's listener isn't the session's")
	}
}

// TestStartRefusedWhenStoppedDuringLaunch: a stop while a launch is
// being prepared keeps it from starting, and its files go.
func TestStartRefusedWhenStoppedDuringLaunch(t *testing.T) {
	s, entered, release := slowLaunchServer(t)
	done := make(chan *proto.Error, 1)
	go func() {
		_, perr := s.startSession(proto.SessionStartParams{Agent: "claude", Cwd: t.TempDir()})
		done <- perr
	}()
	waitEntered(t, entered)
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
	close(release)
	if perr := <-done; perr == nil || perr.Code != proto.ErrRefused {
		t.Fatalf("session.start during a stop: %v", perr)
	}
	s.mu.Lock()
	n, mods := len(s.sessions), len(s.mods)
	s.mu.Unlock()
	if n != 0 || mods != 0 {
		t.Fatalf("%d sessions, %d mod listeners after a refused launch", n, mods)
	}
	if _, err := os.Stat(s.runtimeDir("s-1")); !os.IsNotExist(err) {
		t.Fatalf("runtime dir left: %v", err)
	}
}

// TestResumeAnswersBetweenLaunches: resume launches each session
// without s.mu: while one waits on its version, the sessions resumed
// before it answer.
func TestResumeAnswersBetweenLaunches(t *testing.T) {
	s, entered, release := slowLaunchServer(t)
	cwd := t.TempDir()
	recs := []SessionRecord{
		{ID: "s-1", Role: proto.RoleShell, Agent: "claude", AgentSessionID: newUUID(), Cwd: cwd, Created: time.Now()},
		{ID: "s-2", Role: proto.RoleShell, Agent: "claude", AgentSessionID: newUUID(), Cwd: cwd, Created: time.Now()},
	}
	done := make(chan []restartOutcome, 1)
	go func() { done <- s.resume(recs) }()
	waitEntered(t, entered)
	answers(t, "session.list", func() { s.list() })
	close(release)
	outs := <-done
	if len(outs) != 2 || outs[0].how != "fresh" || outs[1].how != "fresh" {
		t.Fatalf("outcomes %+v", outs)
	}
	if l := s.list(); len(l.Sessions) != 2 {
		t.Fatalf("listed %+v", l.Sessions)
	}
}
