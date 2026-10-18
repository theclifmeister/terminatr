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
	"github.com/theclifmeister/terminatr/internal/guard"
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

// TestReadyThreadTemp: a link the thread left in its temp folder (a
// direct child, or in its claude-<uid> folder) makes the folder anew at
// launch, without touching where the link led; a clean folder is kept.
func TestReadyThreadTemp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges")
	}
	root := filepath.Join(t.TempDir(), "tm")
	dir := filepath.Join(root, "abc")
	target := t.TempDir()
	os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600)
	if renewed, err := readyThreadTemp(dir); err != nil || renewed {
		t.Fatalf("new folder: renewed %v, %v", renewed, err)
	}
	os.MkdirAll(filepath.Join(dir, "claude-1", "scratch"), 0o700)
	os.WriteFile(filepath.Join(dir, "claude-1", "scratch", "n.md"), []byte("x"), 0o600)
	if renewed, err := readyThreadTemp(dir); err != nil || renewed {
		t.Fatalf("clean folder: renewed %v, %v", renewed, err)
	}
	for _, link := range []string{filepath.Join(dir, "l"), filepath.Join(dir, "claude-1", "l")} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if renewed, err := readyThreadTemp(dir); err != nil || !renewed {
			t.Fatalf("link %s: renewed %v, %v", link, renewed, err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("renewed folder holds %v", entries)
		}
		if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
			t.Errorf("the link's target lost its file: %v", err)
		}
		os.MkdirAll(filepath.Join(dir, "claude-1"), 0o700)
	}
	// Someone else's folder, or a link, in its place: refused.
	os.RemoveAll(dir)
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := readyThreadTemp(dir); err == nil {
		t.Error("a link in the folder's place was taken")
	}
}

// TestTempWarnings: a shared temp folder left readable is a launch
// warning, said plainly; a deny skipped for another reason is not.
func TestTempWarnings(t *testing.T) {
	set := proto.AccessSet{Rules: []proto.AccessRule{
		{Kind: proto.AccessNoRead, Value: "/tmp/claude-12345", Skipped: notDenied + "too long"},
		{Kind: proto.AccessNoRead, Value: "/tmp/claude-1", Skipped: "it holds the thread's own folders"},
		{Kind: proto.AccessNoRead, Value: "/tmp/tm-1"},
	}}
	got := tempWarnings(set)
	if len(got) != 1 || !strings.Contains(got[0], "/tmp/claude-12345 is not denied: too long") {
		t.Errorf("tempWarnings = %q", got)
	}
}

// TestGuardThreadTemps: a thread's guard keeps file tools out of the
// other threads' temp folders, its own open (T591); a coordinator's
// has none.
func TestGuardThreadTemps(t *testing.T) {
	p := testPaths(t)
	t.Setenv("HOME", t.TempDir())
	newWatchProject(t)
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p}, agents: reg}
	own, _ := home.ThreadTempDir("demo", "t-0001")
	sibling := filepath.Join(home.ThreadTempRoot(), "0000000000", "claude-1", "x", "tasks", "a.output")
	for _, yolo := range []bool{false, true} {
		g := s.guardRulesFor(SessionRecord{Role: proto.RoleThread, Project: "demo", Thread: "t-0001", Cwd: t.TempDir(), Agent: "claude", Yolo: yolo})
		if g.OwnTemp != own || !slices.Contains(g.OtherTemps, home.ThreadTempRoot()) {
			t.Errorf("yolo %v: thread guard: own %q others %q", yolo, g.OwnTemp, g.OtherTemps)
		}
		if d := g.Judge("Read", map[string]any{"file_path": sibling}); d == nil || d.Rule != guard.RuleTempFolders || !d.Floor {
			t.Errorf("yolo %v: Read of a sibling's temp folder: %+v", yolo, d)
		}
		if d := g.Judge("Write", map[string]any{"file_path": filepath.Join(own, "claude-1", "n.md")}); d != nil {
			t.Errorf("yolo %v: Write in its own temp folder refused: %+v", yolo, d)
		}
	}
	if g := s.guardRulesFor(SessionRecord{Role: proto.RoleCoordinator, Project: "demo", Cwd: t.TempDir(), Agent: "claude"}); len(g.OtherTemps) != 0 {
		t.Errorf("coordinator guard: others %q", g.OtherTemps)
	}
}
