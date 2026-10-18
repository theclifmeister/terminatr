package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/plat/ipc/ipctest"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestGuardRulesFor: a thread gets every rule, a coordinator all but
// the thread's own two, and the human's settings turn rules off.
func TestGuardRulesFor(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	s := &Server{log: log.New(os.Stderr, "", 0)}
	wt := t.TempDir()
	thread := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt}
	g := s.guardRulesFor(thread)
	if !g.On || !slices.Equal(g.Rules, []string{"force-push", "push-default", "worktree-only", "delete-branch", "merge", "credentials"}) {
		t.Fatalf("thread %+v", g)
	}
	if g.Writable[0] != wt || g.Cwd != wt || !slices.Contains(g.Protected, "main") || len(g.Secrets) == 0 {
		t.Errorf("thread %+v", g)
	}
	// The session agent's tools, from its manifest's [guard.tools].
	if g.Tools["Bash"].Kind != guard.KindShell || g.Tools["Edit"].Kind != guard.KindWrite {
		t.Errorf("claude tools %+v", g.Tools)
	}
	if g := s.guardRulesFor(SessionRecord{ID: "s-4", Role: proto.RoleThread, Agent: "codex", Project: p.Slug, Cwd: wt}); g.Tools["apply_patch"].Kind != guard.KindPatch || g.Tools["Edit"].Kind != "" {
		t.Errorf("codex tools %+v", g.Tools)
	}
	coord := SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Project: p.Slug, Cwd: p.Dir}
	if g := s.guardRulesFor(coord); !slices.Equal(g.Rules, []string{"force-push", "push-default", "delete-branch", "credentials"}) || g.Writable != nil {
		t.Errorf("coordinator %+v", g)
	}
	if g := s.guardRulesFor(SessionRecord{ID: "s-3", Role: proto.RoleShell}); g.On {
		t.Errorf("shell %+v", g)
	}

	writeConfig(t, "[defaults]\nguard_off = [\"credentials\"]\n[projects."+p.Slug+"]\nmerge = \"thread\"\n")
	if g := s.guardRulesFor(thread); !slices.Equal(g.Rules, []string{"force-push", "push-default", "worktree-only", "delete-branch"}) || g.Secrets == nil {
		t.Errorf("merge = thread, credentials off: %+v", g)
	}
	writeConfig(t, "[projects."+p.Slug+"]\nguard = false\n")
	if g := s.guardRulesFor(thread); g.On || g.Rules != nil {
		t.Errorf("guard = false: %+v", g)
	}
	// A broken file leaves the guard on.
	writeConfig(t, "[projects."+p.Slug+"]\nguard = \"maybe\"\n")
	if g := s.guardRulesFor(thread); !g.On {
		t.Errorf("broken config: %+v", g)
	}
}

// TestGuardRulesFloor: tm's settings and the project folder are on the
// floor (noWrite): a thread's whole project folder, the coordinator's
// only the parts tm writes. A thread with escapes asks about them;
// under yolo the guard keeps its floor only (T574 cases 4, 6).
func TestGuardRulesFloor(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	home := os.Getenv("TERMINATR_HOME")
	s := &Server{log: log.New(os.Stderr, "", 0), opts: Options{Paths: client.Paths{Home: home}}}
	wt := t.TempDir()
	cfg := filepath.Join(home, "config.toml")
	writeConfig(t, "[projects."+p.Slug+"]\nguard_allow = [{ rule = \"push-default\", command = \"git push origin main\" }]\nguard_mode = \"learn\"\n")
	thread := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt}
	g := s.guardRulesFor(thread)
	if !slices.Contains(g.NoWrite, cfg) || !slices.Contains(g.NoWrite, p.Dir) || !g.AskEscapes || len(g.Rules) == 0 || len(g.Allow) == 0 || !g.Learn {
		t.Fatalf("thread %+v", g)
	}
	coord := SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Project: p.Slug, Cwd: p.Dir}
	if g := s.guardRulesFor(coord); !slices.Contains(g.NoWrite, cfg) || slices.Contains(g.NoWrite, p.Dir) ||
		!slices.Contains(g.NoWrite, filepath.Join(p.Dir, "threads")) || !slices.Contains(g.NoWrite, filepath.Join(p.Dir, "TASKS.md")) || g.AskEscapes {
		t.Fatalf("coordinator %+v", g)
	}
	closed := thread
	closed.NoSandboxEscapes = true
	if g := s.guardRulesFor(closed); g.AskEscapes {
		t.Errorf("escapes off still asks: %+v", g)
	}
	yolo := thread
	yolo.Yolo = true
	g = s.guardRulesFor(yolo)
	if !g.On || g.Rules != nil || g.Allow != nil || g.Learn || g.Mods != nil || g.AskEscapes ||
		!slices.Equal(g.Floor, []string{"force-push", "merge"}) || !slices.Contains(g.NoWrite, p.Dir) || len(g.Own) == 0 {
		t.Fatalf("yolo %+v", g)
	}
	bash := func(c string) *guard.Denial { return g.Judge("Bash", map[string]any{"command": c}) }
	if d := bash("git push origin main"); d != nil {
		t.Errorf("yolo push to main (not the floor): %+v", d)
	}
	if d := bash("git push -f origin main"); d == nil || !d.Floor {
		t.Errorf("yolo force-push to main: %+v", d)
	}
	if d := bash("echo x >> " + cfg); d == nil || d.Rule != guard.RuleTMFiles || !d.Floor {
		t.Errorf("yolo write to config.toml: %+v", d)
	}
	if d := g.Judge("Write", map[string]any{"file_path": filepath.Join(p.Dir, "CONTEXT.md")}); d == nil || d.Rule != guard.RuleTMFiles {
		t.Errorf("yolo write to the project folder: %+v", d)
	}
	if d := g.Judge("Write", map[string]any{"file_path": filepath.Join(wt, "x.go")}); d != nil {
		t.Errorf("yolo write in the worktree: %+v", d)
	}
}

func writeConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(os.Getenv("TERMINATR_HOME"), "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestGuardRoutes: the mod fetches its rules over its socket, and each
// refusal it reports is journaled, and a session refused three times in
// ten minutes files one inbox item.
func TestGuardRoutes(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	rt := ipctest.ShortTempDir(t, "tmguard")
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	s.mu.Lock()
	s.inSession = testInSession
	sock, err := s.listenMod("s-2", rt)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.closeMods)
	c := modClient(sock)

	resp, err := c.Get("http://terminatr/v1/rules")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("no record: %d", resp.StatusCode)
	}
	s.mu.Lock()
	s.records["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: rt}
	s.mu.Unlock()
	resp, err = c.Get("http://terminatr/v1/rules")
	if err != nil {
		t.Fatal(err)
	}
	var g GuardRules
	err = json.NewDecoder(resp.Body).Decode(&g)
	resp.Body.Close()
	if err != nil || !g.On || !slices.Contains(g.Rules, "merge") || g.Tools["Bash"].Kind != guard.KindShell {
		t.Fatalf("rules %+v, %v", g, err)
	}

	if code := postMod(t, c, "/v1/denied", `{"rule":"nope","tool":"Bash"}`); code != http.StatusBadRequest {
		t.Errorf("unknown rule: %d", code)
	}
	clock := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	guardNow = func() time.Time { return clock }
	t.Cleanup(func() { guardNow = time.Now })
	deny := func(rule string) {
		t.Helper()
		body := `{"rule":"` + rule + `","tool":"Bash","summary":"gh pr merge\n42"}`
		if code := postMod(t, c, "/v1/denied", body); code != http.StatusNoContent {
			t.Fatalf("denied: %d", code)
		}
	}
	guards := func() int {
		items, err := p.Inbox()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, it := range items {
			if it.Kind == "guard" {
				n++
			}
		}
		return n
	}
	// Two refusals are journaled only; the third within ten minutes
	// raises one item, and the count starts again.
	deny("merge")
	clock = clock.Add(4 * time.Minute)
	deny("force-push")
	if n := guards(); n != 0 {
		t.Fatalf("after 2 refusals: %d inbox items", n)
	}
	clock = clock.Add(4 * time.Minute)
	deny("merge")
	if n := guards(); n != 1 {
		t.Fatalf("after 3 refusals: %d inbox items", n)
	}
	// Three spread over more than ten minutes raise nothing.
	for range 3 {
		clock = clock.Add(6 * time.Minute)
		deny("merge")
	}
	if n := guards(); n != 1 {
		t.Fatalf("slow refusals: %d inbox items", n)
	}
	lines, _, _ := p.JournalTail(20)
	n := 0
	for _, l := range lines {
		if strings.Contains(l, "t-0001 guard.deny t-0001 merge Bash: gh pr merge 42") {
			n++
		}
	}
	if n != 5 {
		t.Errorf("journal %q", lines)
	}
}

// TestHookGuard: without a mod, the server judges the tool calls a
// session's hooks report against its rules and records each refusal as
// POST /v1/denied does, but not an ask; with a mod there is no hook
// guard.
func TestHookGuard(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	wt := t.TempDir()
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	if s.hookGuardOf("s-2", true) != nil {
		t.Fatal("a mod session got a hook guard")
	}
	judge := s.hookGuardOf("s-2", false)
	if d := judge("Bash", map[string]any{"command": "gh pr merge 1"}); d != nil {
		t.Fatalf("no record: %+v", d)
	}
	s.records["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "codex", Project: p.Slug, Thread: "t-0001", Cwd: wt}
	clock := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	guardNow = func() time.Time { return clock }
	t.Cleanup(func() { guardNow = time.Now })

	d := judge("Bash", map[string]any{"command": "cd x && gh pr merge 1 --squash"})
	if d == nil || d.Rule != "merge" || !strings.HasPrefix(d.Message, "terminatr guard (merge): ") {
		t.Fatalf("merge: %+v", d)
	}
	if d := judge("apply_patch", map[string]any{"command": "*** Begin Patch\n*** Add File: x.go\n+package x\n*** End Patch"}); d != nil {
		t.Errorf("patch in the worktree: %+v", d)
	}
	if d := judge("apply_patch", map[string]any{"command": "*** Begin Patch\n*** Update File: /etc/hosts\n*** End Patch"}); d == nil || d.Rule != "worktree-only" {
		t.Errorf("patch outside the worktree: %+v", d)
	}
	if d := judge("Bash", map[string]any{"command": "git push -u origin tm/x"}); d != nil {
		t.Errorf("own branch: %+v", d)
	}
	// Claude's PowerShell tool: a command the guard can't read is asked,
	// and an ask is not journaled.
	s.records["s-3"] = SessionRecord{ID: "s-3", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt}
	if d := s.hookGuardOf("s-3", false)("PowerShell", map[string]any{"command": "iex $line"}); d == nil || !d.Ask || d.Rule != "unknown" {
		t.Errorf("PowerShell iex: %+v", d)
	}
	lines, _, _ := p.JournalTail(20)
	var got []string
	for _, l := range lines {
		if strings.Contains(l, "guard.deny") {
			got = append(got, l[strings.Index(l, "guard.deny"):])
		}
	}
	want := []string{"guard.deny t-0001 merge Bash: gh pr merge", "guard.deny t-0001 worktree-only apply_patch: apply_patch of /etc/hosts"}
	if !slices.Equal(got, want) {
		t.Errorf("journal %q, want %q", got, want)
	}

	if judge("Bash", map[string]any{"command": "gh pr merge 1"}) == nil {
		t.Error("merge again: not refused")
	}
	// The human's settings reach the session's hooks at once (T301 B4).
	// The merge rule off, a thread merging is still the floor's; only
	// merge = "thread" lets it.
	writeConfig(t, "[projects."+p.Slug+"]\nguard_off = [\"merge\"]\n")
	if d := judge("Bash", map[string]any{"command": "gh pr merge 1"}); d == nil || !d.Floor {
		t.Errorf("merge rule off: %+v", d)
	}
	writeConfig(t, "[projects."+p.Slug+"]\nmerge = \"thread\"\n")
	if d := judge("Bash", map[string]any{"command": "gh pr merge 1"}); d != nil {
		t.Errorf("merge = thread: %+v", d)
	}
	// The third refusal within ten minutes filed one inbox item.
	items, _ := p.Inbox()
	n := 0
	for _, it := range items {
		if it.Kind == "guard" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d guard inbox items", n)
	}
}

// TestHookGuardFollowsLinks: a write through a link in the worktree is
// judged where it lands; one that stays in the worktree is not refused.
func TestHookGuardFollowsLinks(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	wt := t.TempDir()
	out := filepath.Join(os.Getenv("HOME"), "outside")
	if err := os.Symlink(filepath.Dir(out), filepath.Join(wt, "up")); err != nil {
		t.Skip(err)
	}
	if err := os.Mkdir(filepath.Join(wt, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join(wt, "in")); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	s.records["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "codex", Project: p.Slug, Thread: "t-0001", Cwd: wt}
	judge := s.hookGuardOf("s-2", false)
	patch := func(path string) map[string]any {
		return map[string]any{"command": "*** Begin Patch\n*** Add File: " + path + "\n+x\n*** End Patch"}
	}
	for _, path := range []string{"up/outside/x", filepath.Join(wt, "up", "outside"), "in/../up/x"} {
		if d := judge("apply_patch", patch(path)); d == nil || d.Rule != "worktree-only" {
			t.Errorf("%s: %+v", path, d)
		}
	}
	for _, path := range []string{"in/x.go", "sub/new/x.go", "x.go"} {
		if d := judge("apply_patch", patch(path)); d != nil {
			t.Errorf("%s: %+v", path, d)
		}
	}
}

// TestGuardAllowsExact: an exact allowance's path is taken whole, a *
// in it a folder's own name, so a link of that name is followed; a
// pattern's link before its first * is, and its * kept.
func TestGuardAllowsExact(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "l*nk")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "ln")); err != nil {
		t.Fatal(err)
	}
	got := guardAllows([]config.GuardAllow{
		{Rule: "worktree-only", Path: filepath.Join(dir, "l*nk", "a.go"), Exact: true},
		{Rule: "worktree-only", Path: filepath.Join(dir, "ln", "a*.go")},
	}, "")
	want := []guard.Allow{
		{Rule: "worktree-only", Path: filepath.Join(dir, "l*nk", "a.go"), Exact: true},
		{Rule: "worktree-only", Path: filepath.Join(dir, "real", "a.go"), Exact: true},
		{Rule: "worktree-only", Path: filepath.Join(dir, "ln", "a*.go")},
		{Rule: "worktree-only", Path: filepath.Join(dir, "real", "a*.go")},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// TestRealTarget: links are followed, a .. after one leaves its target,
// and what doesn't exist is joined on.
func TestRealTarget(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, d := range []string{"a/b", "c"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "a", "b"), filepath.Join(dir, "c", "abs")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink("../a", filepath.Join(dir, "c", "rel")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "c", "dangling")); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"c/abs/x":       "a/b/x",
		"c/abs/../y":    "a/y",
		"c/rel/b/new/z": "a/b/new/z",
		"c/dangling/f":  "nowhere/f",
		"c/./new/../q":  "c/q",
		"a/b":           "a/b",
	} {
		if got := realTarget(dir + "/" + in); got != filepath.Join(dir, want) {
			t.Errorf("%s: got %s, want %s", in, got, filepath.Join(dir, want))
		}
	}
}

// TestModResolve: the mod's POST /v1/resolve answers where each write
// lands, through a link out of the worktree, in order; a relative path
// comes back as sent, and too many paths are refused.
func TestModResolve(t *testing.T) {
	rt := ipctest.ShortTempDir(t, "tmresolve")
	rt, _ = filepath.EvalSymlinks(rt)
	wt, out := filepath.Join(rt, "wt"), filepath.Join(rt, "out")
	for _, d := range []string{wt, out} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(out, filepath.Join(wt, "link")); err != nil {
		t.Skip(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	s.mu.Lock()
	s.inSession = testInSession
	sock, err := s.listenMod("s-3", rt)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.closeMods)
	c := modClient(sock)

	q, _ := json.Marshal(GuardResolve{Paths: []string{wt + "/link/x", wt + "/new/y", "rel/z"}})
	resp, err := c.Post("http://terminatr/v1/resolve", "application/json", bytes.NewReader(q))
	if err != nil {
		t.Fatal(err)
	}
	var got GuardResolve
	err = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	want := []string{filepath.Join(out, "x"), filepath.Join(wt, "new", "y"), "rel/z"}
	if err != nil || !slices.Equal(got.Paths, want) {
		t.Errorf("got %q, %v; want %q", got.Paths, err, want)
	}
	q, _ = json.Marshal(GuardResolve{Paths: make([]string, maxResolve+1)})
	if code := postMod(t, c, "/v1/resolve", string(q)); code != http.StatusBadRequest {
		t.Errorf("too many: %d", code)
	}
}

// TestAccessMergeCommands: coordinator_merges (off by default) // coordinator tm thread merge, only while merge = coordinator, and a
// thread never gets them.
func TestAccessMergeCommands(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	cmds := func(role string) []string {
		a, err := s.accessFor(role, p.Slug, nil, loadLaunchConfig())
		if err != nil {
			t.Fatal(err)
		}
		return a.Commands
	}
	if got := cmds(proto.RoleCoordinator); got != nil {
		t.Errorf("default: coordinator commands %q", got)
	}
	want := []string{"tm thread merge"}
	for _, body := range []string{
		"[projects." + p.Slug + "]\ncoordinator_merges = true\n",
		"[defaults]\ncoordinator_merges = true\n",
	} {
		writeConfig(t, body)
		if got := cmds(proto.RoleCoordinator); !slices.Equal(got, want) {
			t.Errorf("%q: coordinator commands %q, want %q", body, got, want)
		}
		if got := cmds(proto.RoleThread); got != nil {
			t.Errorf("%q: thread commands %q", body, got)
		}
	}
	writeConfig(t, "[projects."+p.Slug+"]\ncoordinator_merges = true\nmerge = \"thread\"\n")
	if got := cmds(proto.RoleCoordinator); got != nil {
		t.Errorf("merge = thread: coordinator commands %q", got)
	}
	writeConfig(t, "[defaults]\ncoordinator_merges = true\n[projects."+p.Slug+"]\ncoordinator_merges = false\n")
	if got := cmds(proto.RoleCoordinator); got != nil {
		t.Errorf("project off: coordinator commands %q", got)
	}
}

// TestAccessCoordinatorOwned: the coordinator may write its project
// folder but not what tm alone writes there, whose folders exist before
// it starts; CONTEXT.md, MEMORY.md, memory/ and PROJECT.md stay open.
func TestAccessCoordinatorOwned(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	a, err := s.accessFor(proto.RoleCoordinator, p.Slug, nil, loadLaunchConfig())
	if err != nil {
		t.Fatal(err)
	}
	dir := realPath(p.Dir)
	for _, n := range []string{"threads", "inbox", "tasks", "journal", "messages", "uploads"} {
		d := filepath.Join(dir, n)
		if !slices.Contains(a.NoWrite, d) {
			t.Errorf("%s not in no-write %q", n, a.NoWrite)
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Errorf("%s not made: %v", n, err)
		}
	}
	for _, n := range []string{"JOURNAL.md", "TASKS.md", "questions.json", "AGENTS.md", ".JOURNAL.md.lock"} {
		if !slices.Contains(a.NoWriteFiles, filepath.Join(dir, n)) {
			t.Errorf("%s not in no-write files %q", n, a.NoWriteFiles)
		}
	}
	for _, n := range []string{"", "CONTEXT.md", "MEMORY.md", "memory", "PROJECT.md"} {
		w := filepath.Join(dir, n)
		if slices.Contains(a.NoWrite, w) || slices.Contains(a.NoWriteFiles, w) {
			t.Errorf("%q closed", n)
		}
	}
}

// TestAccessThreadGitDirs: a thread in a linked worktree may write what
// its commits and pushes need in the main repo's git dir and, by name,
// the worktree's own git dir below it; never the git dir's hooks,
// config or info, nor another worktree's git dir, which its deny rules
// name, and the guard keeps its commands off them too. The git dirs are
// the thread record's repo's (threadTree).
func TestAccessThreadGitDirs(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	root := realPath(t.TempDir())
	repo, wt, other := filepath.Join(root, "repo"), filepath.Join(root, "wt"), filepath.Join(root, "other")
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c"},
		{"-C", repo, "worktree", "add", "-q", "-b", "x", wt},
		{"-C", repo, "worktree", "add", "-q", "-b", "y", other},
	} {
		if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	common := filepath.Join(repo, ".git")
	in := func(names ...string) []string {
		var out []string
		for _, n := range names {
			out = append(out, filepath.Join(common, n))
		}
		return out
	}
	session := func(r thread.Record) SessionRecord {
		t.Helper()
		rec, err := thread.Create(p, r)
		if err != nil {
			t.Fatal(err)
		}
		return SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: rec.ID, Cwd: rec.Worktree}
	}
	wtRec := session(thread.Record{Title: "wt", Repo: repo, Worktree: wt})
	tree := threadTree(wtRec)
	if tree == nil {
		t.Fatal("worktree: no tree")
	}
	a, err := s.accessFor(proto.RoleThread, p.Slug, tree, loadLaunchConfig())
	if err != nil {
		t.Fatal(err)
	}
	if want := in("objects", "refs", "logs", "worktrees/wt"); !slices.Equal(a.Write, want) {
		t.Errorf("worktree: write %q, want %q", a.Write, want)
	}
	if want := in("packed-refs", "packed-refs.lock"); !slices.Equal(a.WriteFiles, want) {
		t.Errorf("worktree: write files %q, want %q", a.WriteFiles, want)
	}
	for _, n := range []string{"hooks", "info", "modules", "worktrees/other"} {
		if !slices.Contains(a.NoWrite, filepath.Join(common, n)) {
			t.Errorf("worktree: %s not in no-write %q", n, a.NoWrite)
		}
	}
	for _, n := range []string{"config", "config.lock", "config.worktree", "commondir"} {
		if !slices.Contains(a.NoWriteFiles, filepath.Join(common, n)) {
			t.Errorf("worktree: %s not in no-write files %q", n, a.NoWriteFiles)
		}
	}
	for _, n := range []string{"config.worktree", "commondir"} {
		if !slices.Contains(a.NoWriteFiles, filepath.Join(common, "worktrees", "wt", n)) {
			t.Errorf("worktree: its own %s not in no-write files %q", n, a.NoWriteFiles)
		}
	}
	if slices.Contains(a.NoWrite, filepath.Join(common, "worktrees", "wt")) {
		t.Errorf("worktree: its own git dir in no-write %q", a.NoWrite)
	}
	// The main checkout: the same, without a git dir of its own to add.
	mt := threadTree(session(thread.Record{Title: "main", Repo: repo, Worktree: repo, Checkout: true}))
	if mt == nil {
		t.Fatal("main checkout: no tree")
	}
	m, _ := s.accessFor(proto.RoleThread, p.Slug, mt, loadLaunchConfig())
	if want := in("objects", "refs", "logs"); !slices.Equal(m.Write, want) {
		t.Errorf("main checkout: write %q, want %q", m.Write, want)
	}
	for _, n := range []string{"config", "config.lock", "config.worktree", "commondir"} {
		if !slices.Contains(m.NoWriteFiles, filepath.Join(common, n)) {
			t.Errorf("main checkout: %s not in no-write files %q", n, m.NoWriteFiles)
		}
	}
	if !slices.Contains(m.NoWrite, filepath.Join(common, "modules")) {
		t.Errorf("main checkout: modules not in no-write %q", m.NoWrite)
	}
	// Nothing written is the git dir or holds what git runs or reads
	// as configuration.
	for _, a := range []agent.Access{a, m} {
		for _, w := range append(slices.Clone(a.Write), a.WriteFiles...) {
			for _, n := range []string{"", "hooks", "config", "info", "hooks/pre-commit"} {
				if pathUnder(filepath.Join(common, n), w) {
					t.Errorf("write %s covers %s", w, filepath.Join(common, n))
				}
			}
		}
	}
	// The guard keeps a thread's commands off them too.
	g := s.guardRulesFor(wtRec)
	for _, n := range []string{"hooks", "info", "modules", "config", "commondir", "worktrees/wt/config.worktree", "worktrees/wt/commondir", "worktrees/other/config.worktree"} {
		if !slices.Contains(g.GitConfig, filepath.Join(common, n)) {
			t.Errorf("guard: %s not in %q", n, g.GitConfig)
		}
	}
}

// TestThreadTreeRecorded: a thread's git dirs are its record's repo's,
// whatever its folder's .git says: a worktree whose .git names another
// repository, a folder other than the recorded worktree, a thread with
// no record or none of its repo, and a main checkout whose .git is no
// folder get none. The guard's rule on the repo's git configuration
// holds without the worktree.
func TestThreadTreeRecorded(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	root := realPath(t.TempDir())
	repo, wt, other, wt2 := filepath.Join(root, "repo"), filepath.Join(root, "wt"), filepath.Join(root, "other"), filepath.Join(root, "wt2")
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c"},
		{"-C", repo, "worktree", "add", "-q", "-b", "x", wt},
		{"-C", repo, "worktree", "add", "-q", "-b", "y", wt2},
		{"init", "-q", other},
		{"-C", other, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c"},
	} {
		if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	session := func(r thread.Record, cwd string) SessionRecord {
		t.Helper()
		rec, err := thread.Create(p, r)
		if err != nil {
			t.Fatal(err)
		}
		return SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: rec.ID, Cwd: cwd}
	}
	if tr := threadTree(session(thread.Record{Title: "ok", Repo: repo, Worktree: wt}, wt)); tr == nil || tr.GitDir != filepath.Join(repo, ".git", "worktrees", "wt") {
		t.Fatalf("worktree: %+v", tr)
	}
	for name, r := range map[string]SessionRecord{
		"other repo":     session(thread.Record{Title: "o", Repo: other, Worktree: wt}, wt),
		"other folder":   session(thread.Record{Title: "f", Repo: repo, Worktree: wt}, wt2),
		"no repo":        session(thread.Record{Title: "n", Worktree: wt}, wt),
		"no record":      {ID: "s-1", Role: proto.RoleThread, Project: p.Slug, Thread: "t-0999", Cwd: wt},
		"no thread":      {ID: "s-1", Role: proto.RoleThread, Project: p.Slug, Cwd: wt},
		"main as linked": session(thread.Record{Title: "m", Repo: repo, Worktree: wt, Checkout: true}, wt),
	} {
		if tr := threadTree(r); tr != nil {
			t.Errorf("%s: %+v", name, tr)
		}
	}
	// The agent points its worktree's .git at another repository's git
	// dir: no grant, nor guard rules from it.
	r := session(thread.Record{Title: "forged", Repo: repo, Worktree: wt}, wt)
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if tr := threadTree(r); tr != nil {
		t.Errorf("forged .git: %+v", tr)
	}
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}}
	g := s.guardRulesFor(r)
	for _, n := range []string{"hooks", "config", "worktrees/wt/config.worktree"} {
		if !slices.Contains(g.GitConfig, filepath.Join(repo, ".git", n)) {
			t.Errorf("forged .git: guard %s not in %q", n, g.GitConfig)
		}
	}
	// A main checkout's git dir is its recorded folder's .git, a folder.
	main := session(thread.Record{Title: "main", Repo: other, Worktree: other, Checkout: true}, other)
	if tr := threadTree(main); tr == nil || tr.GitDir != filepath.Join(other, ".git") || tr.CommonDir != tr.GitDir {
		t.Errorf("main checkout: %+v", tr)
	}
	if err := os.Rename(filepath.Join(other, ".git"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, ".git"), []byte("gitdir: "+filepath.Join(root, "moved")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if tr := threadTree(main); tr != nil {
		t.Errorf("main checkout, .git a file: %+v", tr)
	}
}

// pathUnder: p is root or below it.
func pathUnder(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// TestGuardSecrets: the agents' paths come from their manifests. Codex's
// login is a credential store, in ~/.codex or $CODEX_HOME, and a Codex
// thread's hooks refuse reading it; every session keeps every agent's
// secrets.
func TestGuardSecrets(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	home, codexHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows' home
	t.Setenv("CODEX_HOME", codexHome)
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	rec := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "codex", Project: p.Slug, Thread: "t-0001", Cwd: t.TempDir()}
	g := s.guardRulesFor(rec)
	for _, want := range []string{filepath.Join(home, ".codex", "auth.json"), filepath.Join(codexHome, "auth.json"), filepath.Join(home, ".claude", ".credentials.json")} {
		if !slices.Contains(g.Secrets, want) {
			t.Errorf("secrets %q lack %s", g.Secrets, want)
		}
	}
	s.records["s-2"] = rec
	judge := s.hookGuardOf("s-2", false)
	// Bash takes forward slashes on Windows too (Git Bash).
	if d := judge("Bash", map[string]any{"command": "cat " + filepath.ToSlash(filepath.Join(codexHome, "auth.json"))}); d == nil || d.Rule != "credentials" {
		t.Errorf("cat of $CODEX_HOME/auth.json: %+v", d)
	}
	if d := judge("Bash", map[string]any{"command": "cat ~/.codex/auth.json"}); d == nil || d.Rule != "credentials" {
		t.Errorf("cat of ~/.codex/auth.json: %+v", d)
	}
	if d := judge("Bash", map[string]any{"command": "cat ~/.codex/config.toml"}); d != nil {
		t.Errorf("cat of ~/.codex/config.toml: %+v", d)
	}
	// The writable folders are the session agent's: Claude's plans and
	// memory for a Claude thread, none of them for a Codex thread.
	plans := filepath.Join(home, ".claude", "plans")
	if slices.Contains(g.Writable, plans) {
		t.Errorf("codex thread writable %q", g.Writable)
	}
	rec.Agent = "claude"
	if g := s.guardRulesFor(rec); !slices.Contains(g.Writable, plans) || !slices.Contains(g.Writable, filepath.Join(home, ".claude", "projects")) || !slices.Contains(g.Secrets, filepath.Join(codexHome, "auth.json")) {
		t.Errorf("claude thread %+v", g)
	}
}

// TestGuardModRules: an enabled mod's guard rules reach the sessions of
// its projects and roles, labelled with the mod, even with every core
// rule off; the guard off takes them too. A refusal under one is
// journaled; a mod rule the session doesn't have is refused.
func TestGuardModRules(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	dir := filepath.Join(os.Getenv("TERMINATR_HOME"), "mods", "infra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `id = "infra"
name = "Infra"
version = "1.0.0"
api = "1.0"

[[provides.guard]]
id = "no-destroy"
commands = ["terraform destroy"]
writes = ["~/infra/prod"]
message = "infrastructure is the human's to change."

[[provides.guard]]
id = "no-prod"
reads = ["/etc/prod"]
roles = ["coordinator"]
message = "prod secrets\nstay with the human."
`
	if err := os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := ipctest.ShortTempDir(t, "tmguard")
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	config := func(body string) {
		t.Helper()
		writeConfig(t, body)
		s.modBr.mu.Lock()
		s.modBr.reg = nil
		s.modBr.mu.Unlock()
	}
	thread := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: rt}
	coord := SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Agent: "claude", Project: p.Slug, Cwd: p.Dir}
	names := func(g GuardRules) string {
		var out []string
		for _, m := range g.Mods {
			out = append(out, m.Name())
		}
		return strings.Join(out, " ")
	}

	if g := s.guardRulesFor(thread); names(g) != "" {
		t.Fatalf("not enabled: %+v", g.Mods)
	}
	config("[mods.infra]\nenabled = true\n")
	g := s.guardRulesFor(thread)
	home, _ := os.UserHomeDir()
	if names(g) != "mod:infra.no-destroy" || g.Mods[0].Writes[0] != filepath.Join(home, "infra", "prod") ||
		!slices.Equal(g.Mods[0].Commands[0], []string{"terraform", "destroy"}) || g.Mods[0].Message != "infrastructure is the human's to change." {
		t.Fatalf("thread %+v", g.Mods)
	}
	if g := s.guardRulesFor(coord); names(g) != "mod:infra.no-destroy mod:infra.no-prod" || g.Mods[1].Message != "prod secrets stay with the human." {
		t.Fatalf("coordinator %+v", g.Mods)
	}
	config("[defaults]\nguard_off = [\"force-push\", \"push-default\", \"worktree-only\", \"delete-branch\", \"merge\", \"credentials\"]\n[mods.infra]\nenabled = true\n")
	if g := s.guardRulesFor(thread); !g.On || g.Rules != nil || names(g) != "mod:infra.no-destroy" ||
		g.Judge("Bash", map[string]any{"command": "terraform destroy"}) == nil {
		t.Fatalf("core rules off %+v", g)
	}
	config("[projects." + p.Slug + "]\nguard = false\n[mods.infra]\nenabled = true\n")
	if g := s.guardRulesFor(thread); g.On || g.Mods != nil {
		t.Fatalf("guard off %+v", g)
	}
	config("[mods.infra]\nenabled = true\nprojects = [\"other\"]\n")
	if g := s.guardRulesFor(thread); names(g) != "" {
		t.Fatalf("another project's mod %+v", g.Mods)
	}

	config("[mods.infra]\nenabled = true\n")
	s.mu.Lock()
	s.inSession = testInSession
	sock, err := s.listenMod("s-2", rt)
	s.records["s-2"] = thread
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.closeMods)
	c := modClient(sock)
	resp, err := c.Get("http://terminatr/v1/rules")
	if err != nil {
		t.Fatal(err)
	}
	var got GuardRules
	err = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if err != nil || names(got) != "mod:infra.no-destroy" {
		t.Fatalf("GET /v1/rules %+v, %v", got.Mods, err)
	}
	for _, rule := range []string{"mod:infra.no-prod", "mod:infra.nope", "mod:other.no-destroy"} {
		if code := postMod(t, c, "/v1/denied", `{"rule":"`+rule+`","tool":"Bash"}`); code != http.StatusBadRequest {
			t.Errorf("%s: %d", rule, code)
		}
	}
	if code := postMod(t, c, "/v1/denied", `{"rule":"mod:infra.no-destroy","tool":"Bash","summary":"terraform destroy"}`); code != http.StatusNoContent {
		t.Fatalf("denied: %d", code)
	}
	lines, _, _ := p.JournalTail(5)
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "guard.deny t-0001 mod:infra.no-destroy Bash: terraform destroy")
	}) {
		t.Errorf("journal %q", lines)
	}
}

// TestGuardModLive: a guard mod enabled, disabled, updated or removed
// changes a running session's guard decisions at once, for its hooks
// and for its mod's GET /v1/rules, without a restart (T301 B4).
func TestGuardModLive(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	clock := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	guardNow = func() time.Time { return clock } // never a minute later
	t.Cleanup(func() { guardNow = time.Now })
	dir := filepath.Join(os.Getenv("TERMINATR_HOME"), "mods", "infra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := func(cmd string) {
		t.Helper()
		m := "id = \"infra\"\nname = \"Infra\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[[provides.guard]]\nid = \"no-destroy\"\ncommands = [\"" + cmd + "\"]\nmessage = \"the human's.\"\n"
		if err := os.WriteFile(filepath.Join(dir, "mod.toml"), []byte(m), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest("terraform destroy")
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	rec := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "codex", Project: p.Slug, Thread: "t-0001", Cwd: t.TempDir()}
	s.records["s-2"] = rec
	judge := s.hookGuardOf("s-2", false)
	refused := func(cmd string) bool {
		t.Helper()
		hook := judge("Bash", map[string]any{"command": cmd}) != nil
		if mod := s.hookRules(rec).Judge("Bash", map[string]any{"command": cmd}) != nil; mod != hook {
			t.Fatalf("%s: the hooks say refused %v, the mod's rules %v", cmd, hook, mod)
		}
		return hook
	}
	if refused("terraform destroy") {
		t.Fatal("refused before the mod was enabled")
	}
	writeConfig(t, "[mods.infra]\nenabled = true\n")
	if !refused("terraform destroy") {
		t.Fatal("enabled: not refused")
	}
	writeConfig(t, "[mods.infra]\nenabled = false\n")
	if refused("terraform destroy") {
		t.Fatal("disabled: still refused")
	}
	writeConfig(t, "[mods.infra]\nenabled = true\n")
	manifest("pulumi destroy --yes")
	if refused("terraform destroy") || !refused("pulumi destroy --yes") {
		t.Fatal("updated: the old rule holds")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if refused("pulumi destroy --yes") {
		t.Fatal("removed: still refused")
	}
}

// TestGuardStateOfServer: the guard's memory of a session (its hook
// rules, its refusals) is its server's, and goes when the session exits:
// a new session under the same id, on this server or another in the
// process, starts afresh.
func TestGuardStateOfServer(t *testing.T) {
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	guardNow = func() time.Time { return clock }
	t.Cleanup(func() { guardNow = time.Now })
	a := &Server{log: log.New(io.Discard, "", 0), stopping: true}
	b := &Server{log: log.New(io.Discard, "", 0)}
	for range guardInboxCount - 1 {
		if a.guard.repeated("s-1") {
			t.Fatal("repeated too soon")
		}
	}
	if b.guard.repeated("s-1") {
		t.Fatal("another server's refusals counted")
	}
	a.guard.rules = map[string]hookRulesEntry{"s-1": {at: clock}, "s-2": {at: clock}}

	done := make(chan struct{})
	sess, err := session.Start(session.Config{ID: "s-1", Argv: []string{testSh(t), "-c", "exit 0"}, Cwd: t.TempDir(),
		Logf: t.Logf, OnExit: func(sess *session.Session) { a.sessionExited(sess); close(done) }})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Stop(StopGrace)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not exit")
	}
	if _, ok := a.guard.rules["s-1"]; ok {
		t.Error("the exited session's rules are kept")
	}
	if _, ok := a.guard.rules["s-2"]; !ok {
		t.Error("another session's rules went")
	}
	if a.guard.repeated("s-1") {
		t.Error("the exited session's refusals are kept")
	}
}

// TestGuardOwnExcluded: a thread's guard gets the excluded commands of
// the user's own Claude settings and its worktree's, with tm's beside
// them (T574 case 7): a command one takes and tm's don't asks the user,
// or with escapes off is refused; none under yolo, nor for Codex,
// which has no such files.
func TestGuardOwnExcluded(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"sandbox":{"excludedCommands":["docker","npx *"]}}`), 0o600)
	wt := t.TempDir()
	os.MkdirAll(filepath.Join(wt, ".claude"), 0o700)
	os.WriteFile(filepath.Join(wt, ".claude", "settings.local.json"), []byte(`{"sandbox":{"excludedCommands":["make deploy *","docker"]}}`), 0o600)
	writeConfig(t, "[projects."+p.Slug+".sandbox]\nexcluded_commands = [\"npx playwright *\"]\n")
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}, agents: reg}
	thread := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt}
	g := s.guardRulesFor(thread)
	if !slices.Equal(g.Unsandboxed, []string{"docker", "npx *", "make deploy *"}) || !slices.Equal(g.TMExcluded, []string{"npx playwright *"}) || g.NoEscapes {
		t.Fatalf("thread %+v", g)
	}
	bash := func(g guard.Rules, c string) *guard.Denial { return g.Judge("Bash", map[string]any{"command": c}) }
	if d := bash(g, "docker ps"); d == nil || !d.Ask || d.Rule != guard.RuleEscape {
		t.Errorf("docker ps: %+v", d)
	}
	if d := bash(g, "make deploy prod"); d == nil || !d.Ask {
		t.Errorf("the worktree's own: %+v", d)
	}
	if d := bash(g, "npx playwright test"); d != nil {
		t.Errorf("tm's excluded command: %+v", d)
	}
	closed := thread
	closed.NoSandboxEscapes = true
	if d := bash(s.guardRulesFor(closed), "docker ps"); d == nil || d.Ask || d.Rule != guard.RuleEscape {
		t.Errorf("escapes off: %+v", d)
	}
	yolo := thread
	yolo.Yolo = true
	if g := s.guardRulesFor(yolo); g.Unsandboxed != nil {
		t.Errorf("yolo %+v", g.Unsandboxed)
	}
	codex := thread
	codex.Agent = "codex"
	if g := s.guardRulesFor(codex); g.Unsandboxed != nil {
		t.Errorf("codex %+v", g.Unsandboxed)
	}
}
