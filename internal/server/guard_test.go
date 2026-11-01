package server

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gitx"
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
	// Off: no rule and no floor; a thread's escapes are still asked.
	if g := s.guardRulesFor(thread); g.Rules != nil || g.Floor != nil || !g.AskEscapes {
		t.Errorf("guard = false: %+v", g)
	}
	if g := s.guardRulesFor(coord); g.On {
		t.Errorf("guard = false, coordinator: %+v", g)
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
	// The guard off: no rule and no floor, but escapes are still asked.
	writeConfig(t, "[projects."+p.Slug+"]\nguard = false\n")
	if off := s.guardRulesFor(thread); !off.On || !off.AskEscapes || off.Rules != nil || off.Floor != nil || off.NoWrite != nil ||
		off.Judge("Bash", map[string]any{"command": "ls", "dangerouslyDisableSandbox": true}) == nil ||
		off.Judge("Bash", map[string]any{"command": "git push -f origin main"}) != nil {
		t.Fatalf("guard off, escapes on: %+v", off)
	} else if v, _ := guardVerdict(off, "git push -f origin main"); v != "off" {
		t.Errorf("guard off verdict %q", v)
	}
	if off := s.guardRulesFor(closed); off.On {
		t.Errorf("guard off, escapes off: %+v", off)
	}
	writeConfig(t, "")
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

// TestGuardDenied: a session's rules are its role's, and each refusal
// is journaled, and a session refused three times in ten minutes files
// one inbox item.
func TestGuardDenied(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	rec := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: t.TempDir()}
	s.records["s-2"] = rec
	if g := s.hookRules(rec); !g.On || !slices.Contains(g.Rules, "merge") || g.Tools["Bash"].Kind != guard.KindShell {
		t.Fatalf("rules %+v", g)
	}
	clock := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	guardNow = func() time.Time { return clock }
	t.Cleanup(func() { guardNow = time.Now })
	deny := func(rule string) {
		t.Helper()
		s.guardDenied(rec, GuardDenial{Rule: rule, Tool: "Bash", Summary: "gh pr merge\n42"})
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

// TestHookGuard: the server judges the tool calls a session's hooks
// report against its rules and records each refusal, but not an ask; a
// session without its record gets the unchecked refusal.
func TestHookGuard(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	wt := t.TempDir()
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	judge := s.hookGuardOf("s-2")
	if d := judge("Bash", map[string]any{"command": "gh pr merge 1"}); d == nil || d.Rule != guard.RuleUnchecked || d.Ask {
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
	if d := s.hookGuardOf("s-3")("PowerShell", map[string]any{"command": "iex $line"}); d == nil || !d.Ask || d.Rule != "unknown" {
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

// TestHookGuardLimit: a call the server doesn't judge within judgeLimit
// is refused (unchecked) and journaled, and the judging goes on alone.
func TestHookGuardLimit(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	s.records["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: t.TempDir()}
	release := make(chan struct{})
	var running, most atomic.Int32
	var deadline atomic.Value
	called := make(chan struct{}, 1)
	judgeLimit, judgeCall = 50*time.Millisecond, func(r guard.Rules, _ string, _ map[string]any) *guard.Denial {
		deadline.Store(r.Deadline)
		select {
		case called <- struct{}{}:
		default:
		}
		n := running.Add(1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		<-release
		running.Add(-1)
		return nil
	}
	t.Cleanup(func() { close(release); judgeLimit, judgeCall = 1500*time.Millisecond, guard.Rules.Judge })
	start := time.Now()
	judge := s.hookGuardOf("s-2")
	d := judge("Bash", map[string]any{"command": "ls"})
	if d == nil || d.Rule != guard.RuleUnchecked || d.Ask || !strings.Contains(d.Message, "took too long") {
		t.Fatalf("overrun: %+v", d)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("waited %v for a judge past its limit", took)
	}
	lines, _, _ := p.JournalTail(5)
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "guard.deny t-0001 unchecked Bash") }) {
		t.Errorf("journal %q", lines)
	}
	// The judge got the deadline the server waits to, so it stops there.
	// Its rules (config and git reads) can outlast the limit under -race:
	// wait for it to start.
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("the judge never ran")
	}
	if dl, _ := deadline.Load().(time.Time); dl.IsZero() || dl.After(start.Add(judgeLimit+100*time.Millisecond)) {
		t.Errorf("judge deadline %v, started %v", dl, start)
	}
	// While that judge runs on, the session's next call starts none: it
	// waits its own limit for it, and is refused.
	if d := judge("Bash", map[string]any{"command": "pwd"}); d == nil || d.Rule != guard.RuleUnchecked {
		t.Fatalf("second call: %+v", d)
	}
	if n := most.Load(); n != 1 {
		t.Errorf("%d judges at once in one session", n)
	}
}

// TestHookSettingsFloor: a session's .claude/settings.json and
// settings.local.json, which could turn its hooks off, are the floor's:
// no file tool or shell redirect writes them, whatever the rules, named
// through the session folder's link or where it leads (macOS's /var is
// /private/var).
func TestHookSettingsFloor(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	real := t.TempDir()
	wt := filepath.Join(t.TempDir(), "wt")
	if err := os.Symlink(real, wt); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	s.records["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt}
	writeConfig(t, "[projects."+p.Slug+"]\nguard_off = [\"worktree-only\"]\n")
	judge := s.hookGuardOf("s-2")
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Write", map[string]any{"file_path": filepath.Join(wt, ".claude", "settings.json"), "content": `{"disableAllHooks": true}`}},
		{"Edit", map[string]any{"file_path": filepath.Join(wt, ".claude", "settings.local.json")}},
		{"Bash", map[string]any{"command": `echo '{"disableAllHooks":true}' > .claude/settings.local.json`}},
		{"Write", map[string]any{"file_path": filepath.Join(real, ".claude", "settings.json")}},
		{"Edit", map[string]any{"file_path": filepath.Join(realPath(real), ".claude", "settings.local.json")}},
	} {
		if d := judge(c.tool, c.input); d == nil || d.Rule != guard.RuleTMFiles || !d.Floor {
			t.Errorf("%s %v: %+v", c.tool, c.input, d)
		}
	}
	if d := judge("Write", map[string]any{"file_path": filepath.Join(wt, ".claude", "agents", "x.md")}); d != nil {
		t.Errorf("another .claude file: %+v", d)
	}
}

// TestShellStateFloor: what a shell runs before each command, the user's
// shell startup files and the agents' shell snapshots, is the floor's
// for every role: no file tool or shell command writes it, named as the
// call gives it or where its link leads, so no session redefines cd or
// exit for the calls after.
func TestShellStateFloor(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"ZDOTDIR", "XDG_CONFIG_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME"} {
		t.Setenv(v, "")
	}
	dots := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(dots, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dots, "zshrc"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dots, "zshrc"), filepath.Join(home, ".zshrc")); err != nil {
		t.Skip(err)
	}
	// $ZDOTDIR a link to a folder whose files don't exist yet, and a
	// completion dump with the host in its name.
	zdot := filepath.Join(home, "zdot")
	if err := os.Symlink(dots, zdot); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZDOTDIR", zdot)
	if err := os.WriteFile(filepath.Join(home, ".zcompdump-host-5.9"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	s.records["s-1"] = SessionRecord{ID: "s-1", Role: proto.RoleCoordinator, Agent: "claude", Project: p.Slug, Cwd: p.Dir}
	s.records["s-2"] = SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: t.TempDir()}
	writeConfig(t, "[projects."+p.Slug+"]\nguard_off = [\"worktree-only\"]\n")
	for _, id := range []string{"s-1", "s-2"} {
		judge := s.hookGuardOf(id)
		for _, c := range []struct {
			tool  string
			input map[string]any
		}{
			{"Bash", map[string]any{"command": `echo 'cd() { :; }' >> ~/.zshrc`}},
			{"Bash", map[string]any{"command": "echo 'exit() { :; }' > ~/.claude/shell-snapshots/snapshot-zsh-1.sh"}},
			{"Bash", map[string]any{"command": "rm -rf ~/.claude/shell-snapshots"}},
			{"Write", map[string]any{"file_path": filepath.Join(home, ".bashrc")}},
			{"Write", map[string]any{"file_path": filepath.Join(dots, "zshrc")}},
			{"Write", map[string]any{"file_path": filepath.Join(home, ".codex", "shell_snapshots", "x.sh")}},
			{"Write", map[string]any{"file_path": filepath.Join(home, ".zshrc.zwc")}},
			{"Write", map[string]any{"file_path": filepath.Join(home, ".bash_aliases")}},
			{"Write", map[string]any{"file_path": filepath.Join(home, ".config", "zsh", ".zshrc")}},
			{"Bash", map[string]any{"command": "echo x > ~/.zshrc.d/50-cd.zsh"}},
			{"Bash", map[string]any{"command": "touch ~/.zcompdump-host-5.9"}},
			{"Bash", map[string]any{"command": "echo 'cd() { :; }' > ~/.zcompdump-other-5.9.zwc"}},
			{"Write", map[string]any{"file_path": filepath.Join(zdot, ".zcompdump")}},
			{"Write", map[string]any{"file_path": filepath.Join(dots, ".zshenv.zwc")}},
			{"Write", map[string]any{"file_path": filepath.Join(zdot, ".zlogin")}},
		} {
			if d := judge(c.tool, c.input); d == nil || d.Rule != guard.RuleTMFiles || !d.Floor {
				t.Errorf("%s %s %v: %+v", id, c.tool, c.input, d)
			}
		}
		if d := judge("Bash", map[string]any{"command": "cat ~/.zshrc > /dev/null"}); d != nil {
			t.Errorf("%s reading .zshrc: %+v", id, d)
		}
	}
}

// TestSessionCdPath: a session's shell may start with CDPATH or zsh's
// cdpath set, or cdable_vars on (guard.Rules.CdPath), when the server's
// environment has it, tm gives it, or Claude's env file, a startup file
// or a shell snapshot names it; then a relative cd leaves the guard's
// folder unknown.
func TestSessionCdPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"CDPATH", "BASHOPTS", "CLAUDE_ENV_FILE", "ZDOTDIR", "XDG_CONFIG_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME"} {
		t.Setenv(v, "")
	}
	s := &Server{log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}}
	write := func(p, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".zshrc"), "export PATH=$HOME/bin:$PATH\n")
	write(filepath.Join(home, ".claude", "shell-snapshots", "snapshot-zsh-1.sh"), "alias ll='ls -l'\n")
	if s.sessionCdPath([]string{"GH_TOKEN"}, home) {
		t.Fatal("cdpath with none set")
	}
	if !s.sessionCdPath([]string{"CDPATH"}, home) {
		t.Error("tm's env")
	}
	t.Setenv("CDPATH", "/src")
	if !s.sessionCdPath(nil, home) {
		t.Error("the server's environment")
	}
	t.Setenv("CDPATH", "")
	if !s.sessionCdPath([]string{"BASHOPTS"}, home) {
		t.Error("tm's BASHOPTS")
	}
	t.Setenv("BASHOPTS", "checkwinsize:cdable_vars")
	if !s.sessionCdPath(nil, home) {
		t.Error("the server's BASHOPTS")
	}
	t.Setenv("BASHOPTS", "checkwinsize")
	if s.sessionCdPath(nil, home) {
		t.Error("BASHOPTS without cdable_vars")
	}
	for _, line := range []string{"cdpath=(~/src)", "setopt CDABLE_VARS", "shopt -s cdable_vars"} {
		for _, f := range []string{
			filepath.Join(home, ".zshrc"),
			filepath.Join(home, ".claude", "shell-snapshots", "snapshot-zsh-2.sh"),
			filepath.Join(home, ".config", "fish", "conf.d", "cd.fish"),
			filepath.Join(home, "envfile"),
		} {
			old, _ := os.ReadFile(f)
			write(f, string(old)+line+"\n")
			if f == filepath.Join(home, "envfile") {
				t.Setenv("CLAUDE_ENV_FILE", f)
			}
			if !s.sessionCdPath(nil, home) {
				t.Errorf("%s: %s", f, line)
			}
			if err := os.Remove(f); err != nil {
				t.Fatal(err)
			}
		}
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
	judge := s.hookGuardOf("s-2")
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

// TestWithLanding: a floor path under folders that don't exist yet, with
// a link above them (macOS's /var), is held where a write to it lands
// as well as as written (T660).
func TestWithLanding(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	in := filepath.Join(dir, "link", ".codex", "shell_snapshots")
	got := withLanding([]string{in, in})
	want := []string{in, filepath.Join(dir, "real", ".codex", "shell_snapshots")}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestAccessCoordinatorCommands: the coordinator's allow rules follow
// each setting that authorizes its commands, per project; the read-only
// ones are always there; threads get none.
func TestAccessCoordinatorCommands(t *testing.T) {
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
	readOnly := []string{"tm context", "tm inbox list", "tm task list", "tm task show", "tm thread list", "tm thread show", "tm thread read", "tm library list", "tm memory find", "tm memory show", "tm memory list", "tm memory check", "tm access show", "tm access explain"}
	access := []string{"tm access add", "tm access rm", "tm access edit", "tm access drop", "tm access undrop", "tm access set local-ports"}
	with := func(c ...string) []string { return append(slices.Clone(readOnly), c...) }
	own := "[projects." + p.Slug + "]\n"
	for _, c := range []struct {
		body string
		want []string
	}{
		// coordinator_approves is on by default.
		{"", with("tm thread approve")},
		{own + "coordinator_approves = false\n", readOnly},
		{"[defaults]\ncoordinator_approves = false\n", readOnly},
		{"[defaults]\ncoordinator_approves = false\n" + own + "coordinator_approves = true\n", with("tm thread approve")},
		{own + "start_threads = \"auto\"\ncoordinator_approves = false\n", with("tm thread start", "tm task delegate")},
		{"[defaults]\nstart_threads = \"auto\"\n" + own + "start_threads = \"propose\"\n", with("tm thread approve")},
		{own + "coordinator_merges = true\n", with("tm thread approve", "tm thread merge")},
		{"[defaults]\ncoordinator_merges = true\n", with("tm thread approve", "tm thread merge")},
		{own + "coordinator_merges = true\nmerge = \"thread\"\n", with("tm thread approve")},
		{"[defaults]\ncoordinator_merges = true\n" + own + "coordinator_merges = false\n", with("tm thread approve")},
		{own + "start_threads = \"auto\"\ncoordinator_merges = true\n", with("tm thread start", "tm task delegate", "tm thread approve", "tm thread merge")},
		{own + "coordinator_access = true\ncoordinator_approves = false\n", with(access...)},
		{"[defaults]\ncoordinator_access = true\n", with(append(slices.Clone(access), "tm thread approve")...)},
		{"[defaults]\ncoordinator_access = true\n" + own + "coordinator_access = false\n", with("tm thread approve")},
		// Nothing for complete_tasks: done is the user's word whatever it says.
		{own + "complete_tasks = \"merged\"\ncoordinator_approves = false\n", readOnly},
	} {
		writeConfig(t, c.body)
		if got := cmds(proto.RoleCoordinator); !slices.Equal(got, c.want) {
			t.Errorf("%q: coordinator commands %q, want %q", c.body, got, c.want)
		}
		if got := cmds(proto.RoleThread); got != nil {
			t.Errorf("%q: thread commands %q", c.body, got)
		}
	}
	for _, r := range coordinatorRules {
		for _, c := range r.commands {
			for _, human := range []string{"tm access set", "tm access reset", "tm mod", "tm thread resolve", "tm library rm", "tm task status", "tm thread answer", "tm memory rm"} {
				if strings.HasPrefix(c, human) && c != "tm access set local-ports" {
					t.Errorf("%q allowed: it needs the user's word or is the human's", c)
				}
			}
		}
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
	if runs := gitx.RunsDir(realPath(s.opts.Paths.Home)); !slices.Contains(a.NoWrite, runs) {
		t.Errorf("worktree: tm's git runs folder %s not in no-write %q", runs, a.NoWrite)
	}
	for _, n := range []string{"hooks", "info", "modules", "remotes", "branches", "worktrees/other", "rebase-merge", "rebase-apply", "sequencer"} {
		if !slices.Contains(a.NoWrite, filepath.Join(common, n)) {
			t.Errorf("worktree: %s not in no-write %q", n, a.NoWrite)
		}
	}
	// Worktrees added later: the siblings of its own git dir.
	if want := in("worktrees/wt"); !slices.Equal(a.NoWriteSiblings, want) {
		t.Errorf("worktree: no-write siblings %q, want %q", a.NoWriteSiblings, want)
	}
	for _, n := range []string{"config", "config.lock", "config.worktree", "commondir"} {
		if !slices.Contains(a.NoWriteFiles, filepath.Join(common, n)) {
			t.Errorf("worktree: %s not in no-write files %q", n, a.NoWriteFiles)
		}
	}
	for _, n := range []string{"config.worktree", "commondir", "gitdir", "locked"} {
		if !slices.Contains(a.NoWriteFiles, filepath.Join(common, "worktrees", "wt", n)) {
			t.Errorf("worktree: its own %s not in no-write files %q", n, a.NoWriteFiles)
		}
	}
	for _, n := range []string{"HEAD", "HEAD.lock", "index", "index.lock", "ORIG_HEAD", "MERGE_HEAD", "AUTO_MERGE", "CHERRY_PICK_HEAD", "REVERT_HEAD", "MERGE_MSG", "SQUASH_MSG"} {
		if !slices.Contains(a.NoWriteFiles, filepath.Join(common, n)) {
			t.Errorf("worktree: the main checkout's %s not in no-write files %q", n, a.NoWriteFiles)
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
	for _, n := range []string{"modules", "worktrees"} {
		if !slices.Contains(m.NoWrite, filepath.Join(common, n)) {
			t.Errorf("main checkout: %s not in no-write %q", n, m.NoWrite)
		}
	}
	for _, n := range []string{"rebase-merge", "sequencer"} {
		if slices.Contains(m.NoWrite, filepath.Join(common, n)) {
			t.Errorf("main checkout: its own %s in no-write %q", n, m.NoWrite)
		}
	}
	if len(m.NoWriteSiblings) != 0 {
		t.Errorf("main checkout: no-write siblings %q", m.NoWriteSiblings)
	}
	for _, n := range []string{"HEAD", "index"} {
		if slices.Contains(m.NoWriteFiles, filepath.Join(common, n)) {
			t.Errorf("main checkout: its own %s in no-write files %q", n, m.NoWriteFiles)
		}
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
	for _, n := range []string{"hooks", "info", "modules", "config", "commondir", "worktrees/wt/config.worktree", "worktrees/wt/commondir", "worktrees/other/config.worktree", "rebase-merge", "sequencer", "HEAD"} {
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
	judge := s.hookGuardOf("s-2")
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
// journaled.
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
	if g := s.guardRulesFor(thread); g.Rules != nil || g.Mods != nil || g.Floor != nil {
		t.Fatalf("guard off %+v", g)
	}
	config("[mods.infra]\nenabled = true\nprojects = [\"other\"]\n")
	if g := s.guardRulesFor(thread); names(g) != "" {
		t.Fatalf("another project's mod %+v", g.Mods)
	}

	config("[mods.infra]\nenabled = true\n")
	s.records["s-2"] = thread
	if d := s.hookGuardOf("s-2")("Bash", map[string]any{"command": "terraform destroy"}); d == nil || d.Rule != "mod:infra.no-destroy" {
		t.Fatalf("terraform destroy: %+v", d)
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
// and its rules, without a restart (T301 B4).
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
	judge := s.hookGuardOf("s-2")
	refused := func(cmd string) bool {
		t.Helper()
		hook := judge("Bash", map[string]any{"command": cmd}) != nil
		if rules := s.hookRules(rec).Judge("Bash", map[string]any{"command": cmd}) != nil; rules != hook {
			t.Fatalf("%s: the hooks say refused %v, the rules %v", cmd, hook, rules)
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
	// tm's own excluded command is no escape: it asks first, as tm's
	// rule (T574 case 1), not as one of the user's.
	if d := bash(g, "npx playwright test"); d == nil || !d.Ask || d.Rule != guard.RuleExcluded {
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

// TestGuardOwnExcludedLive: a thread writing an excluded command into
// its worktree's Claude settings (or the user changing theirs) reaches
// its guard's rules at once, not after hookRulesFor: with escapes off
// the command is refused from the next call (security review, PR #654).
func TestGuardOwnExcludedLive(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	clock := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	guardNow = func() time.Time { return clock } // never a minute later
	t.Cleanup(func() { guardNow = time.Now })
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	s := &Server{log: log.New(os.Stderr, "", 0), opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}}, agents: reg, records: map[string]SessionRecord{}}
	rec := SessionRecord{ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt, NoSandboxEscapes: true}
	s.records["s-2"] = rec
	curl := func() *guard.Denial {
		return s.hookRules(rec).Judge("Bash", map[string]any{"command": "curl https://example.com"})
	}
	if d := curl(); d != nil {
		t.Fatalf("before: %+v", d)
	}
	write := func(path, body string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(path), 0o700)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(wt, ".claude", "settings.local.json"), `{"sandbox":{"excludedCommands":["curl *"]}}`)
	if d := curl(); d == nil || d.Rule != guard.RuleEscape || d.Ask {
		t.Errorf("after the worktree's settings: %+v", d)
	}
	os.Remove(filepath.Join(wt, ".claude", "settings.local.json"))
	if d := curl(); d != nil {
		t.Errorf("after removing it: %+v", d)
	}
	write(filepath.Join(home, ".claude", "settings.json"), `{"sandbox":{"excludedCommands":["curl"]}}`)
	if d := curl(); d == nil || d.Rule != guard.RuleEscape {
		t.Errorf("after the user's settings: %+v", d)
	}
}

// TestThreadRefused: a thread session isn't started where its git dirs
// would get no rules: a folder with a .git whose record can't be read or
// names no repo, git dirs Claude's rules can't name. One with no .git
// and no repo or record, or with a checkout tm can name, starts.
func TestThreadRefused(t *testing.T) {
	testPaths(t)
	p := newWatchProject(t)
	plain, gitted := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(gitted, ".git"), []byte("gitdir: ./fake\n"), 0o644)
	session := func(r thread.Record, cwd string) SessionRecord {
		t.Helper()
		rec, err := thread.Create(p, r)
		if err != nil {
			t.Fatal(err)
		}
		return SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: rec.ID, Cwd: cwd}
	}
	ok := session(thread.Record{Title: "plain", Worktree: plain}, plain)
	if why := threadRefused(ok, nil, "claude", "darwin"); why != "" {
		t.Errorf("no repo, no .git: %s", why)
	}
	if why := threadRefused(session(thread.Record{Title: "g", Worktree: gitted}, gitted), nil, "claude", "darwin"); why == "" {
		t.Error("no repo, a .git: started")
	}
	gone := ok
	gone.Thread = "t-0999"
	if why := threadRefused(gone, nil, "claude", "darwin"); why != "" {
		t.Errorf("no record, no .git: %s", why)
	}
	gone.Cwd = gitted
	if why := threadRefused(gone, nil, "claude", "darwin"); why == "" {
		t.Error("no record, a .git: started")
	}
	if why := threadRefused(session(thread.Record{Title: "r", Repo: plain, Worktree: plain}, plain), nil, "claude", "darwin"); why == "" {
		t.Error("a repo, no checkout: started")
	}
	common := filepath.Join(t.TempDir(), "[x]", ".git")
	tree := &gitx.Tree{Dir: plain, GitDir: filepath.Join(common, "worktrees", "a"), CommonDir: common}
	r := session(thread.Record{Title: "b", Repo: plain, Worktree: plain}, plain)
	if why := threadRefused(r, tree, "claude", "darwin"); why == "" {
		t.Error("glob path, Claude: started")
	}
	if why := threadRefused(r, tree, "codex", "darwin"); why != "" {
		t.Errorf("glob path, Codex: %s", why)
	}
	tree = &gitx.Tree{Dir: plain, GitDir: `/r/a\b/.git`, CommonDir: `/r/a\b/.git`}
	if why := threadRefused(r, tree, "claude", "darwin"); why == "" {
		t.Error("backslash path, Claude: started")
	}
}

// TestLaunchRefusesUnpinned: a thread whose recorded worktree isn't a
// checkout of its recorded repo by the repo's records isn't started,
// rather than started with no rules on its git dirs.
func TestLaunchRefusesUnpinned(t *testing.T) {
	testPaths(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	p := newWatchProject(t)
	root := realPath(t.TempDir())
	repo, wt := filepath.Join(root, "repo"), filepath.Join(root, "wt")
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c"},
		{"-C", repo, "worktree", "add", "-q", "-b", "x", wt},
	} {
		if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	rec, err := thread.Create(p, thread.Record{Title: "x", Agent: "claude", Repo: repo, Worktree: wt, State: "running"})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: client.Paths{Home: os.Getenv("TERMINATR_HOME")}, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin"}},
		log: log.New(os.Stderr, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	sr := SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: rec.ID, Cwd: wt}
	pl, perr := s.prepareLaunch(agentLaunch{rec: sr, kick: "Start."}, launchView{agents: reg})
	if perr != nil {
		t.Fatalf("a checkout: %v", perr)
	}
	pl.abandon()
	// Its git dir no longer names it back.
	os.WriteFile(filepath.Join(repo, ".git", "worktrees", "wt", "gitdir"), []byte("/elsewhere/.git\n"), 0o644)
	if _, perr := s.prepareLaunch(agentLaunch{rec: sr, kick: "Start."}, launchView{agents: reg}); perr == nil || perr.Code != proto.ErrRefused {
		t.Fatalf("unpinned: %v", perr)
	}
}

// TestDroppedGuardEvent: a PreToolUse the server drops (no such
// session, another agent's) is refused as unchecked, never answered with
// nothing; another dropped event is.
func TestDroppedGuardEvent(t *testing.T) {
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log.New(io.Discard, "", 0), agents: reg, sessions: map[string]*session.Session{}, records: map[string]SessionRecord{}}
	pre := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}
	for _, name := range []string{"claude", "codex"} {
		res := s.hookEvent(proto.HookEventParams{Session: "s-9", Agent: name, Event: "PreToolUse", Payload: pre})
		if !strings.Contains(res.Stdout, `"permissionDecision":"deny"`) || !strings.Contains(res.Stdout, "tm couldn't check this call") {
			t.Errorf("%s: %q", name, res.Stdout)
		}
	}
	if res := s.hookEvent(proto.HookEventParams{Session: "s-9", Agent: "claude", Event: "Stop", Payload: map[string]any{"hook_event_name": "Stop"}}); res.Stdout != "" {
		t.Errorf("Stop: %q", res.Stdout)
	}
}

// TestHomeFloor: tm's home is the floor's for every role, but for the
// worktrees (a thread's own) and the projects (the coordinator's folder,
// which its own rules hold): a session doesn't write tm's agents, mods,
// state or run folder, whether they exist yet or not, named through the
// home's link or where it leads (macOS's /var is /private/var).
func TestHomeFloor(t *testing.T) {
	paths := testPaths(t)
	real := paths.Home
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMINATR_HOME", home)
	paths.Home = home
	p := newWatchProject(t)
	wt := filepath.Join(home, "worktrees", p.Slug, "t-0001-x")
	os.MkdirAll(wt, 0o700)
	s := &Server{log: log.New(io.Discard, "", 0), opts: Options{Paths: paths}, records: map[string]SessionRecord{
		"s-1": {ID: "s-1", Role: proto.RoleCoordinator, Agent: "claude", Project: p.Slug, Cwd: p.Dir},
		"s-2": {ID: "s-2", Role: proto.RoleThread, Agent: "claude", Project: p.Slug, Thread: "t-0001", Cwd: wt},
	}}
	writeConfig(t, "[defaults]\nguard_off = [\"worktree-only\"]\n")
	for _, id := range []string{"s-1", "s-2"} {
		judge := s.hookGuardOf(id)
		for _, f := range []string{
			filepath.Join(home, "agents", "claude.toml"),
			filepath.Join(home, "mods", "x", "mod.toml"),
			filepath.Join(home, "state", "sessions.json"),
			filepath.Join(real, "agents", "codex.toml"),
			filepath.Join(realPath(real), "mods", "y", "mod.toml"),
			filepath.Join(paths.RunDir, "x"),
		} {
			if d := judge("Write", map[string]any{"file_path": f}); d == nil || d.Rule != guard.RuleTMFiles || !d.Floor {
				t.Errorf("%s writes %s: %+v", id, f, d)
			}
		}
		if d := judge("Bash", map[string]any{"command": "echo x > " + filepath.Join(home, "agents", "codex.toml")}); d == nil || d.Rule != guard.RuleTMFiles {
			t.Errorf("%s redirect into agents/: %+v", id, d)
		}
	}
	if d := s.hookGuardOf("s-2")("Write", map[string]any{"file_path": filepath.Join(wt, "main.go")}); d != nil {
		t.Errorf("a thread's own worktree: %+v", d)
	}
	if d := s.hookGuardOf("s-1")("Write", map[string]any{"file_path": filepath.Join(p.Dir, "CONTEXT.md")}); d != nil {
		t.Errorf("the coordinator's CONTEXT.md: %+v", d)
	}
}
