package server

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
)

// TestSocketRules: a socket rule launches by its path (and where it
// leads), never one in tm's folders, never a folder; macOS only.
func TestSocketRules(t *testing.T) {
	p := testPaths(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "pg.sock")
	os.WriteFile(sock, nil, 0o600)
	writeConfig(t, `[projects.demo.sandbox]
sockets = [`+tomlQuote(sock)+`, `+tomlQuote(p.HumanSocket())+`, `+tomlQuote(dir)+`]
`)
	s := &Server{opts: Options{Paths: p}}
	rules, why := sandboxRules(loadLaunchConfig(), "demo", config.OSMac, s.tmDirs())
	if why != "" {
		t.Fatal(why)
	}
	got := map[string]proto.AccessRule{}
	for _, r := range rules {
		if r.Kind == proto.AccessUnixSocket {
			got[r.Note] = r
		}
	}
	if r := got[sock]; !r.Given() || r.Where != proto.AccessInside {
		t.Errorf("socket: %+v", r)
	}
	if r := got[p.HumanSocket()]; !strings.Contains(r.Refused, "human socket") {
		t.Errorf("human socket: %+v", r)
	}
	if r := got[dir]; !strings.Contains(r.Refused, "folder") {
		t.Errorf("folder: %+v", r)
	}
	lists, _, _ := launchSandbox(proto.AccessSet{Rules: rules})
	if !slices.Contains(lists.Sockets, sock) || slices.Contains(lists.Sockets, p.HumanSocket()) || slices.Contains(lists.Sockets, dir) {
		t.Errorf("launched sockets %q", lists.Sockets)
	}
	rules, _ = sandboxRules(loadLaunchConfig(), "demo", config.OSLinux, s.tmDirs())
	if lists, _, _ := launchSandbox(proto.AccessSet{Rules: rules}); len(lists.Sockets) != 0 {
		t.Errorf("linux sockets %q", lists.Sockets)
	}
}

// tomlQuote is s as a TOML basic string.
func tomlQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestSocketsLaunchClaude: a Claude thread's allowUnixSockets has tm's
// socket and the project's; a coordinator's only tm's.
func TestSocketsLaunchClaude(t *testing.T) {
	if caps.OS != config.OSMac {
		t.Skip("socket rules are macOS's")
	}
	p := testPaths(t)
	cleanThreadTemp(t, "demo", "t-0001")
	t.Setenv("HOME", t.TempDir())
	newWatchProject(t)
	sock := filepath.Join(t.TempDir(), "pg.sock")
	writeConfig(t, "[projects.demo.sandbox]\nsockets = ["+tomlQuote(sock)+"]\n")
	reg, err := agent.Load("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin"}},
		log: log.New(os.Stderr, "", 0), noMods: true, agents: reg, records: map[string]SessionRecord{}}
	socketsOf := func(rec SessionRecord) []string {
		t.Helper()
		pl, perr := s.prepareLaunch(agentLaunch{rec: rec, kick: "Start."}, launchView{agents: reg})
		if perr != nil {
			t.Fatal(perr)
		}
		t.Cleanup(pl.abandon)
		b, err := os.ReadFile(filepath.Join(pl.rt, "claude-settings.json"))
		if err != nil {
			t.Fatal(err)
		}
		var st struct {
			Sandbox struct {
				Network struct{ AllowUnixSockets []string }
			}
		}
		if err := json.Unmarshal(b, &st); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		return st.Sandbox.Network.AllowUnixSockets
	}
	got := socketsOf(SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: "demo", Thread: "t-0001", Cwd: t.TempDir()})
	if len(got) < 2 || got[0] != p.Socket || !slices.Contains(got, sock) {
		t.Errorf("thread: %q", got)
	}
	if got := socketsOf(SessionRecord{ID: "s-2", Role: proto.RoleCoordinator, Agent: "claude", Project: "demo", Cwd: t.TempDir()}); !slices.Equal(got, []string{p.Socket}) {
		t.Errorf("coordinator: %q", got)
	}
}

// TestApprovedRules: a project's approved commands are every agent's;
// each excluded command whose words fit is one for Codex threads, asking
// first as it does; one that doesn't fit isn't, and says why.
func TestApprovedRules(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, `[projects.demo.sandbox]
approved_commands = ["make test", "make e2e *"]
excluded_commands = ["npx playwright *", { command = "docker compose up *", ask = false }, "npm run test:*"]
`)
	s := &Server{opts: Options{Paths: p}}
	claude := s.approvedFor(SessionRecord{Project: "demo", Agent: "claude"})
	if want := []proto.ApprovedCommand{{Command: "make test", Source: "demo"}, {Command: "make e2e *", Source: "demo"}}; !slices.Equal(claude, want) {
		t.Errorf("claude: %+v", claude)
	}
	codex := s.approvedFor(SessionRecord{Project: "demo", Agent: "codex"})
	want := []proto.ApprovedCommand{{Command: "npx playwright *", Ask: true, Source: "demo"}, {Command: "docker compose up *", Source: "demo"},
		{Command: "make test", Source: "demo"}, {Command: "make e2e *", Source: "demo"}}
	if !slices.Equal(codex, want) {
		t.Errorf("codex: %+v", codex)
	}
	rules, _ := sandboxRules(loadLaunchConfig(), "demo", caps.OS, s.tmDirs())
	i := slices.IndexFunc(rules, func(r proto.AccessRule) bool { return r.Kind == proto.AccessApproved && r.Value == "npm run test:*" })
	if i < 0 || !strings.Contains(rules[i].Skipped, "Codex") {
		t.Errorf("npm run test:* for Codex: %+v", rules)
	}
	// The brief lists a thread's own.
	lists, _, notes := launchSandbox(proto.AccessSet{Agent: "claude", Rules: rules})
	if !slices.Equal(lists.Approved, []string{"make test", "make e2e *"}) || len(notes) != 0 {
		t.Errorf("claude lists %q, notes %q", lists.Approved, notes)
	}
	lists, _, _ = launchSandbox(proto.AccessSet{Agent: "codex", Rules: rules})
	if len(lists.Approved) != 4 || !strings.Contains(lists.Approved[0], "asked first") {
		t.Errorf("codex lists %q", lists.Approved)
	}
	rt := t.TempDir()
	brief, err := guideBrief(rt, "", nil, lists)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(brief)
	if !strings.Contains(string(b), "tm run -- <command>") || !strings.Contains(string(b), "- make e2e *") {
		t.Errorf("brief:\n%s", b)
	}
}

// runStream runs command.run for caller who on s, and returns its
// answer's error, the output it streamed and the exit event.
func runStream(t *testing.T, s *Server, who caller.Caller, params proto.CommandRunParams) (*proto.Error, string, proto.CommandRunEvent) {
	t.Helper()
	a, b := net.Pipe()
	raw, _ := json.Marshal(params)
	go func() {
		s.commandRun(a, bufio.NewReader(a), proto.Request{ID: 1, Method: proto.MethodCommandRun, Params: raw}, who)
		a.Close()
	}()
	defer b.Close()
	b.SetDeadline(time.Now().Add(20 * time.Second))
	br := bufio.NewReader(b)
	var resp proto.Response
	if err := proto.ReadLine(br, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil || params.List {
		return resp.Error, string(resp.Result), proto.CommandRunEvent{}
	}
	var out strings.Builder
	for {
		var ev proto.CommandRunEvent
		if err := proto.ReadLine(br, &ev); err != nil {
			t.Fatalf("stream: %v", err)
		}
		if ev.Event == proto.EventRunExit {
			return nil, out.String(), ev
		}
		out.Write(ev.Data)
	}
}

// TestCommandRun: a thread runs only an approved command, word for
// word, in its worktree, without a shell or its own environment; the
// guard judges it; each run is journaled; the human, and a session
// that isn't a running thread, can't.
func TestCommandRun(t *testing.T) {
	if caps.Windows {
		t.Skip("uses sh")
	}
	p := testPaths(t)
	proj := newWatchProject(t)
	wt := t.TempDir()
	os.WriteFile(filepath.Join(wt, "check.sh"), []byte("#!/bin/sh\necho \"in $(pwd) $# $1 ${SECRET_FROM_THREAD:-clean} [$2]\"\necho err >&2\nexit 3\n"), 0o755)
	writeConfig(t, `[projects.demo.sandbox]
approved_commands = ["./check.sh go *", "git push --force *"]
`)
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}},
		log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}, sessions: map[string]*session.Session{}}
	rec := SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "claude", Project: "demo", Thread: "t-0001", Cwd: wt}
	s.records["s-1"], s.sessions["s-1"] = rec, nil
	thr := caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001", Session: "s-1"}

	perr, out, ev := runStream(t, s, thr, proto.CommandRunParams{Argv: []string{"./check.sh", "go", "a b; rm -rf x"}})
	if perr != nil {
		t.Fatal(perr)
	}
	real, _ := filepath.EvalSymlinks(wt)
	if ev.Code != 3 || !strings.Contains(out, "in "+real+" 2 go clean [a b; rm -rf x]") || !strings.Contains(out, "err") {
		t.Errorf("run: code %d, out %q", ev.Code, out)
	}
	if perr, _, _ := runStream(t, s, thr, proto.CommandRunParams{Argv: []string{"./check2.sh"}}); perr == nil || !strings.Contains(perr.Message, "isn't an approved command") {
		t.Errorf("unlisted: %v", perr)
	}
	// The guard's floor holds for approved commands too.
	if perr, _, _ := runStream(t, s, thr, proto.CommandRunParams{Argv: []string{"git", "push", "--force", "origin", "main"}}); perr == nil || !strings.Contains(perr.Message, "terminatr guard") {
		t.Errorf("force-push: %v", perr)
	}
	if perr, _, _ := runStream(t, s, caller.Caller{Kind: caller.Human}, proto.CommandRunParams{Argv: []string{"./check.sh"}}); perr == nil || !strings.Contains(perr.Message, "only a thread") {
		t.Errorf("human: %v", perr)
	}
	gone := thr
	gone.Session = "s-9"
	if perr, _, _ := runStream(t, s, gone, proto.CommandRunParams{Argv: []string{"./check.sh"}}); perr == nil {
		t.Error("a session that doesn't run ran it")
	}
	perr, list, _ := runStream(t, s, thr, proto.CommandRunParams{List: true})
	if perr != nil || !strings.Contains(list, `"./check.sh go *"`) {
		t.Fatalf("list: %v %s", perr, list)
	}
	lines, _, _ := proj.JournalTail(20)
	j := strings.Join(lines, "\n")
	for _, want := range []string{"run.start t-0001 ./check.sh go 'a b; rm -rf x'", "run.end t-0001 ./check.sh go 'a b; rm -rf x': exit 3", "run.refused t-0001 git push --force origin main: the guard"} {
		if !strings.Contains(j, want) {
			t.Errorf("journal lacks %q:\n%s", want, j)
		}
	}
	if len(s.runs) != 0 {
		t.Errorf("runs left: %v", s.runs)
	}
}

// TestRunEscapeQuestion: only the human answers tm's question before an
// approved command that asks first.
func TestRunEscapeQuestion(t *testing.T) {
	var a asks
	a.put("s-1", proto.Question{Escape: true, Questions: []proto.QuestionItem{{Question: "Run?", Options: []proto.QuestionOption{{Label: runYes}}}}})
	if _, perr := a.answer("s-1", 0, runYes, "Run?", false); perr == nil || perr.Code != proto.ErrRefused {
		t.Errorf("not the human: %v", perr)
	}
	if res, perr := a.answer("s-1", 0, runYes, "Run?", true); perr != nil || res.Remaining != 0 {
		t.Errorf("the human: %v", perr)
	}
}

// TestShellLine: what the guard and the journal see.
func TestShellLine(t *testing.T) {
	if got := shellLine([]string{"make", "test", "X=1", "a b", "it's", ""}); got != `make test X=1 'a b' 'it'\''s' ''` {
		t.Errorf("%s", got)
	}
}

// TestCommandRunHangUp: while a run lasts, the pid walk places it in
// its thread; when the thread's call ends, its process group is killed.
func TestCommandRunHangUp(t *testing.T) {
	if caps.Windows {
		t.Skip("uses sleep")
	}
	p := testPaths(t)
	newWatchProject(t)
	writeConfig(t, "[projects.demo.sandbox]\napproved_commands = [\"sleep 30\"]\n")
	s := &Server{opts: Options{Paths: p, Bin: "/nonexistent/tm", Env: []string{"PATH=/usr/bin:/bin"}},
		log: log.New(os.Stderr, "", 0), records: map[string]SessionRecord{}, sessions: map[string]*session.Session{}}
	s.records["s-1"], s.sessions["s-1"] = SessionRecord{ID: "s-1", Role: proto.RoleThread, Agent: "codex", Project: "demo", Thread: "t-0001", Cwd: t.TempDir()}, nil
	thr := caller.Caller{Kind: caller.Thread, Project: "demo", Thread: "t-0001", Session: "s-1"}
	a, b := net.Pipe()
	done := make(chan struct{})
	go func() {
		s.commandRun(a, bufio.NewReader(a), proto.Request{ID: 1, Method: proto.MethodCommandRun, Params: json.RawMessage(`{"argv":["sleep","30"]}`)}, thr)
		close(done)
	}()
	var resp proto.Response
	if err := proto.ReadLine(bufio.NewReader(b), &resp); err != nil || resp.Error != nil {
		t.Fatal(err, resp.Error)
	}
	var pid int
	for deadline := time.Now().Add(5 * time.Second); pid == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		s.mu.Lock()
		for k := range s.runs {
			pid = k
		}
		s.mu.Unlock()
	}
	if pid == 0 {
		t.Fatal("no run")
	}
	s.mu.Lock()
	delete(s.sessions, "s-1") // its nil stand-in has no pid
	s.mu.Unlock()
	if c := s.callerOf(pid); c.Kind != caller.Thread || c.Thread != "t-0001" || c.Session != "s-1" {
		t.Errorf("callerOf the run: %+v", c)
	}
	b.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the run outlived its call")
	}
	if len(s.runs) != 0 {
		t.Errorf("runs left: %v", s.runs)
	}
}
