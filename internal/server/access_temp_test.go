package server

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestThreadTempRules: a thread gets its own temp folder and a read
// deny of the other threads' and of Claude's shared one (T587), as tm
// rules the launch takes; a coordinator gets none, a thread to come a
// placeholder, and a shared folder that holds the thread's worktree is
// skipped.
func TestThreadTempRules(t *testing.T) {
	testPaths(t)
	t.Setenv("HOME", t.TempDir())
	newWatchProject(t)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	lc := loadLaunchConfig()
	cwd := realPath(t.TempDir())
	own, err := home.ThreadTempDir("demo", "t-0001")
	if err != nil {
		t.Fatal(err)
	}
	shared := ""
	if runtime.GOOS != "windows" {
		shared = realPath("/tmp/claude-" + strconv.Itoa(os.Getuid()))
	}
	rules := func(in accessInput) proto.AccessSet {
		t.Helper()
		set, err := s.resolveAccess(in, lc)
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	byKind := func(set proto.AccessSet, kind string) []proto.AccessRule {
		var out []proto.AccessRule
		for _, r := range set.Rules {
			if r.Kind == kind {
				out = append(out, r)
			}
		}
		return out
	}

	set := rules(accessInput{role: proto.RoleThread, slug: "demo", cwd: cwd, agent: "claude", goos: runtime.GOOS, thread: "t-0001"})
	if tr := byKind(set, proto.AccessTemp); len(tr) != 1 || tr[0].Value != own || tr[0].Source != proto.AccessFromTM {
		t.Errorf("temp rules %+v, want one tm rule for %s", tr, own)
	}
	if got := launchTemp(set); got != own {
		t.Errorf("launchTemp = %q, want %q", got, own)
	}
	a := launchAccess(set)
	if a.OwnTemp != own || !slices.Contains(a.NoRead, home.ThreadTempRoot()) {
		t.Errorf("launch access OwnTemp %q NoRead %q, want %s in %s", a.OwnTemp, a.NoRead, own, home.ThreadTempRoot())
	}
	if shared != "" {
		if !slices.Contains(a.NoRead, shared) {
			t.Errorf("NoRead %q lacks Claude's shared temp folder %s", a.NoRead, shared)
		}
		if c := a.Closed(); slices.Contains(c, home.ThreadTempRoot()) || !slices.Contains(c, shared) {
			t.Errorf("Closed %q: want %s and not %s, which holds the thread's own", c, shared, home.ThreadTempRoot())
		}
	}

	// A thread to come: shown, never launched.
	set = rules(accessInput{role: proto.RoleThread, slug: "demo", agent: "claude", goos: runtime.GOOS})
	if tr := byKind(set, proto.AccessTemp); len(tr) != 1 || tr[0].Value != "<thread temp folder>" {
		t.Errorf("a thread to come: temp rules %+v", tr)
	}
	if got := launchTemp(set); got != "" {
		t.Errorf("a thread to come launches with temp %q", got)
	}

	set = rules(accessInput{role: proto.RoleCoordinator, slug: "demo", cwd: cwd, agent: "claude", goos: runtime.GOOS})
	if len(byKind(set, proto.AccessTemp)) != 0 || len(byKind(set, proto.AccessNoRead)) != 0 {
		t.Errorf("a coordinator got temp rules: %+v", set.Rules)
	}

	if shared == "" {
		return
	}
	// A worktree in the shared folder (a scratch home there) would lose
	// itself to the deny: skipped.
	inShared := filepath.Join(shared, "x", "wt")
	set = rules(accessInput{role: proto.RoleThread, slug: "demo", cwd: inShared, agent: "claude", goos: runtime.GOOS, thread: "t-0001"})
	for _, r := range byKind(set, proto.AccessNoRead) {
		if r.Value == shared && r.Skipped == "" {
			t.Errorf("deny of %s kept for a worktree in it", shared)
		}
	}
	if slices.Contains(launchAccess(set).NoRead, shared) {
		t.Errorf("a skipped deny launched")
	}
}

// cleanThreadTemp removes thread id of project slug's own temp folder
// when t ends: a launch makes it in the shared /tmp, outside t's home.
func cleanThreadTemp(t *testing.T, slug, id string) {
	t.Helper()
	if d, err := home.ThreadTempDir(slug, id); err == nil {
		t.Cleanup(func() { os.RemoveAll(d) })
	}
}

// TestThreadTempLaunchEnv: a thread's launch makes its own temp folder,
// private, and gives it to Claude as CLAUDE_CODE_TMPDIR and TMPDIR; a
// coordinator's launch sets neither.
func TestThreadTempLaunchEnv(t *testing.T) {
	p := testPaths(t)
	t.Setenv("HOME", t.TempDir())
	newWatchProject(t)
	cleanThreadTemp(t, "demo", "t-0001")
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin", "TMPDIR=/srv/tmp"}},
		log: log.New(io.Discard, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	own, _ := home.ThreadTempDir("demo", "t-0001")
	rec := SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: "demo", Thread: "t-0001", Cwd: t.TempDir()}
	pl, perr := s.prepareLaunch(agentLaunch{rec: rec, kick: "Start."}, launchView{agents: reg})
	if perr != nil {
		t.Fatal(perr)
	}
	t.Cleanup(pl.abandon)
	for _, kv := range []string{"CLAUDE_CODE_TMPDIR=" + own, "TMPDIR=" + own} {
		if !slices.Contains(pl.env, kv) {
			t.Errorf("env lacks %s: %q", kv, pl.env)
		}
	}
	if err := fsx.CheckPrivate(own); err != nil {
		t.Errorf("own temp folder: %v", err)
	}
	coord := SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Agent: "claude", Project: "demo", Cwd: t.TempDir()}
	pc, perr := s.prepareLaunch(agentLaunch{rec: coord, kick: "Start."}, launchView{agents: reg})
	if perr != nil {
		t.Fatal(perr)
	}
	t.Cleanup(pc.abandon)
	if slices.ContainsFunc(pc.env, func(kv string) bool { return strings.HasPrefix(kv, "CLAUDE_CODE_TMPDIR=") }) || !slices.Contains(pc.env, "TMPDIR=/srv/tmp") {
		t.Errorf("a coordinator's temp variables changed: %q", pc.env)
	}
}
