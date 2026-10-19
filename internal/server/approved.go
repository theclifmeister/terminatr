package server

// Approved commands and sockets (T306, T574 phase 4, docs/SPEC.md
// §5.2). A socket rule opens one unix socket to a thread's sandboxed
// commands; the launch refuses one in or holding tm's folder or its run
// folder (the human socket, every session's runtime folder), one in a
// folder the thread writes (its worktree, its temp folder, its write
// folders: it could put a link there later), or one that is a folder.
// An approved command is one tm runs outside the thread's sandbox when
// it asks (tm run -- make test, command.run): only one the project's
// list names (or, for a Codex thread, one of its excluded commands,
// T574 case 3), its words as they are with no shell, in the thread's
// worktree, with tm's environment and the thread's TERMINATR_*
// variables (nothing the thread sets), judged by the thread's guard as
// its own Bash call would be, journaled when it starts and ends. Its
// output streams back; when the thread's call ends, its session ends or
// the server stops, its process group is killed, and so is what is left
// of it when it exits.
//
// It runs as the user, and the thread writes the files it reads (a
// Makefile target), so it runs what the thread wants. While it runs, the
// pid walk places what it starts in the thread (Server.runs), but only
// what stays in its process tree: a process it detaches (setsid, a
// double fork) is reparented, outlives the kill, and the pid walk then
// takes it for the human, human-only methods included. So on macOS the
// run is confined (runProfile): everything allowed but tm's run folder,
// its sockets' folder (the human socket among them), its keys and
// guard log, and config.toml, which it neither reads, writes nor
// connects to; what it starts, detached or not, inherits that. On Linux
// and Windows it isn't confined: there a detached process reaches the
// human socket as the human, as an excluded command's can.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/proc"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// tmDirs are tm's folders no thread's rule opens: its home and its run
// folder, with the sockets' folder where TERMINATR_SOCKET puts them.
type tmDirs struct{ home, run, sockets string }

// sandboxExec applies a seatbelt profile on macOS.
const sandboxExec = "/usr/bin/sandbox-exec"

// runClosed are what an approved command's run never reaches (macOS,
// runProfile): the folders of tm's own files (the guard's floor: its
// run folder, keys and guard log) and the sockets' folder, and the
// file config.toml.
func (s *Server) runClosed() (dirs, files []string) {
	dirs = s.guardOwn()
	if s.opts.Paths.Socket != "" {
		dirs = append(dirs, withReal([]string{filepath.Dir(s.opts.Paths.Socket)})...)
	}
	if s.opts.Paths.Home != "" {
		files = withReal([]string{filepath.Join(s.opts.Paths.Home, "config.toml")})
	}
	return dirs, files
}

// runUnconfined are the setuid programs an approved command's run may
// start outside its profile (T618). A process in a seatbelt sandbox
// can't exec a setuid program at all (EPERM, nothing in the sandbox
// log), and some tools need one: prlctl's wrapper looks for Parallels'
// dispatcher with ps. These only read the process table and print, so
// they reach none of tm's files while unconfined. No other setuid
// program is here: crontab, at and batch schedule jobs that later run
// outside the profile, sudo, su and login change user.
var runUnconfined = []string{"/bin/ps", "/usr/bin/top"}

// runProfile is the seatbelt profile of an approved command's run:
// everything allowed, but no read, write or connect in dirs (network*
// on a folder refuses connecting to a unix socket below it, through a
// link too) and no read or write of files; runUnconfined start outside
// it.
func runProfile(dirs, files []string) string {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	b.WriteString("(allow process-exec (with no-sandbox)")
	for _, p := range runUnconfined {
		fmt.Fprintf(&b, " (literal \"%s\")", p)
	}
	b.WriteString(")\n")
	for _, d := range dirs {
		fmt.Fprintf(&b, "(deny network* (subpath \"%s\"))\n(deny file-read* file-write* (subpath \"%s\"))\n", q.Replace(d), q.Replace(d))
	}
	for _, f := range files {
		fmt.Fprintf(&b, "(deny file-read* file-write* (literal \"%s\"))\n", q.Replace(f))
	}
	return b.String()
}

// tmDirs is this server's tm folders.
func (s *Server) tmDirs() tmDirs {
	t := tmDirs{home: s.opts.Paths.Home, run: s.opts.Paths.RunDir}
	if s.opts.Paths.Socket != "" {
		t.sockets = filepath.Dir(s.opts.Paths.Socket)
	}
	return t
}

// list is the folders, those set.
func (t tmDirs) list() []string {
	var out []string
	for _, d := range []string{t.home, t.run, t.sockets} {
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// socketRefused is why the launch leaves socket p out, or "": one it
// can't place (no home folder), one in or holding tm's folders (the
// human socket and the sessions' sockets are there), by its path or
// where its links lead, one that leads where no socket rule may be, and
// a folder, which would open every socket below it, and one in writable
// (the threads' temp folders, the project's write folders), where a
// thread could swap a link in after the launch.
func socketRefused(p string, tm tmDirs, writable []string) string {
	if !filepath.IsAbs(p) {
		return "refused: no home folder to place it in"
	}
	real := realPath(p)
	if real != p {
		if err := config.CheckSocket(filepath.ToSlash(real)); err != nil {
			return "refused: it leads to " + real + ", which isn't a socket rule (" + err.Error() + ")"
		}
	}
	for _, c := range withReal(tm.list()) {
		for _, q := range []string{p, real} {
			if inside(q, c) || inside(c, q) {
				return "refused: it is in or holds tm's folder or its run folder, whose sockets (the human socket among them) no thread reaches"
			}
		}
	}
	for _, w := range withReal(writable) {
		for _, q := range []string{p, real} {
			if inside(q, w) {
				return "refused: it is in " + w + ", which threads write: a thread could put a link to another socket there"
			}
		}
	}
	if fi, err := os.Stat(real); err == nil && fi.IsDir() {
		return "refused: it is a folder, which would open every socket below it: name the socket itself"
	}
	return ""
}

// threadWritable are the folders besides tm's a project's threads
// write in, whatever its worktree (tm's folder holds those): the
// threads' temp folders and the write folders of t.
func threadWritable(t config.ThreadSandbox) []string {
	out := []string{threadTempRoot()}
	for _, w := range t.Given(config.SandboxWrite) {
		out = append(out, writeFolder(w))
	}
	return out
}

// codexApproved is excluded command rule x as an approved command for
// Codex threads (T574 case 3): asking first as x does, given as x is;
// one whose words don't fit an approved command isn't used, and says
// why.
func codexApproved(x proto.AccessRule) proto.AccessRule {
	r := x
	r.Kind, r.Agents, r.Live, r.OS = proto.AccessApproved, []string{"codex"}, true, config.SandboxSettings[config.SandboxApproved].OS
	if x.Note != proto.AccessAsksFirst && x.Note != proto.AccessWithoutAsking {
		return r // dropped or replaced: its note says so
	}
	r.Note = "excluded command, " + x.Note
	if r.Refused != "" {
		// The excluded command's own rule says why.
		r.Refused, r.Skipped = "", "its excluded command is refused"
	}
	if _, why := config.ApprovedOf(x.Value); why != "" && r.Skipped == "" {
		r.Skipped = why
	}
	return r
}

// forAgent reports whether a launch of agent gives rule r: an approved
// command only to the agents it names (a Codex thread's excluded
// commands); the other rules as the launch decides.
func forAgent(r proto.AccessRule, agent string) bool {
	return r.Kind != proto.AccessApproved || agent == "" || len(r.Agents) == 0 || slices.Contains(r.Agents, agent)
}

// approvedAsks reports whether approved command rule r asks the user
// before each run: a Codex thread's excluded command that asks first.
func approvedAsks(r proto.AccessRule) bool {
	return r.Note == "excluded command, "+proto.AccessAsksFirst
}

// approvedLine is approved command rule r as the thread's brief lists
// it.
func approvedLine(r proto.AccessRule) string {
	if approvedAsks(r) {
		return r.Value + "   (the user is asked first, each run)"
	}
	return r.Value
}

// approvedFor are the approved commands thread session r may run now:
// read from config.toml at each run (a rule removed holds at once), for
// its agent.
func (s *Server) approvedFor(r SessionRecord) []proto.ApprovedCommand {
	rules, _ := sandboxRules(loadLaunchConfig(), r.Project, caps.OS, s.tmDirs())
	var out []proto.ApprovedCommand
	for _, x := range rules {
		if x.Kind != proto.AccessApproved || !x.Given() || x.Skipped != "" || !forAgent(x, r.Agent) || r.Agent == "" && len(x.Agents) > 0 {
			continue
		}
		if slices.ContainsFunc(out, func(c proto.ApprovedCommand) bool { return c.Command == x.Value }) {
			continue
		}
		out = append(out, proto.ApprovedCommand{Command: x.Value, Ask: approvedAsks(x), Source: x.Source})
	}
	return out
}

// runWaitDelay is how long a run's output may stay open after it
// exits (a child it left holding it), before tm closes it.
const runWaitDelay = 5 * time.Second

// maxRuns bounds a thread's runs at once; maxAskLine the line a
// question shows whole; runWriteWait how long a client may take to
// read one piece of output before tm stops sending it.
const (
	maxRuns      = 4
	maxAskLine   = 1000
	runWriteWait = time.Minute
)

// batchMeta are what cmd.exe reads in a batch file's arguments.
const batchMeta = "&|<>^%\"\r\n()!"

// batchArgs is why args can't go to program path, or "": Windows runs
// a .cmd or .bat through cmd.exe, which reads its own syntax in them.
func batchArgs(path string, args []string) string {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".cmd" && ext != ".bat" {
		return ""
	}
	for _, a := range args {
		if strings.ContainsAny(a, batchMeta) {
			return filepath.Base(path) + " is a batch file, which cmd.exe runs: its arguments can't hold " + batchMeta
		}
	}
	return ""
}

// maxRunArgs and maxRunArg bound what a thread asks to run.
const (
	maxRunArgs = 256
	maxRunArg  = 4096
)

// checkArgv is why argv can't be run, or "": words of plain text.
func checkArgv(argv []string) string {
	if len(argv) == 0 {
		return "no command: tm run -- <command> [args…] (tm run --list lists them)"
	}
	if len(argv) > maxRunArgs {
		return fmt.Sprintf("more than %d words", maxRunArgs)
	}
	for _, a := range argv {
		if len(a) > maxRunArg || strings.ContainsRune(a, 0) || !textsafe.Safe(a) {
			return "a word is too long or has control or invisible characters"
		}
	}
	return ""
}

// shellLine is argv as one Bash line, each word quoted where it needs
// it: what the guard judges and the journal shows.
func shellLine(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@%") == "" {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}

// runRecord is the session record of thread caller c, if it runs.
func (s *Server) runRecord(c caller.Caller) (SessionRecord, *proto.Error) {
	if c.Kind != caller.Thread || c.Session == "" {
		return SessionRecord{}, proto.Errorf(proto.ErrRefused, "only a thread asks tm to run an approved command, from its own session")
	}
	s.mu.Lock()
	r, ok := s.records[c.Session]
	_, running := s.sessions[c.Session]
	s.mu.Unlock()
	if !ok || !running || r.Role != proto.RoleThread || r.Cwd == "" {
		return SessionRecord{}, proto.Errorf(proto.ErrRefused, "session %s isn't a running thread with a worktree", c.Session)
	}
	r.ID = c.Session
	return r, nil
}

// runGuard is the guard's verdict on running line for thread r: its
// rules as for its own Bash call, without the questions about leaving
// the sandbox (an approved command leaves it by the human's list). An
// "ask" is refused too: nobody is there to be asked.
func (s *Server) runGuard(r SessionRecord, line string) *proto.Error {
	g := s.guardRulesFor(r)
	g.AskEscapes, g.AskFirst, g.Unsandboxed, g.TMExcluded = false, nil, nil, nil
	if d := g.JudgeCommand(line); d != nil {
		return proto.Errorf(proto.ErrRefused, "%s", strings.TrimSpace(d.Message))
	}
	return nil
}

// runYes and runNo are the options of the question an approved command
// that asks first puts to the user.
const (
	runYes = "Run it"
	runNo  = "Don't run it"
)

// askRun puts the question whether to run line to the user, on thread
// session id, and waits for the answer: only the human answers it
// (Question.Escape). False for no, or when the thread's call ends, the
// question is replaced or the session ends first.
func (s *Server) askRun(id, line string, gone <-chan struct{}) bool {
	sess, perr := s.session(id)
	if perr != nil {
		return false
	}
	q := proto.Question{Tool: "tm run", Escape: true, Since: time.Now().UTC(), Questions: []proto.QuestionItem{{
		// The whole line: the user approves what they see.
		Question: "Run " + line + " outside the sandbox, as you, in the thread's worktree?",
		Header:   "tm run",
		Options:  []proto.QuestionOption{{Label: runYes, Description: "an excluded command that asks first"}, {Label: runNo}},
	}}}
	o := s.asks.put(id, q)
	defer s.asks.drop(id, o)
	s.watch.wake()
	s.modEv.wake()
	defer s.watch.wake()
	defer s.modEv.wake()
	select {
	case ans := <-o.answered:
		return ans[q.Questions[0].Question] == runYes
	case <-o.replaced:
	case <-sess.Done():
	case <-gone:
	}
	return false
}

// lookPathIn is name's program by the folders of path (a PATH value),
// or name from cwd when it names a folder.
func lookPathIn(name, path, cwd string) (string, error) {
	if strings.ContainsAny(name, `/\`) {
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			return "", fmt.Errorf("%s: no such program", name)
		}
		return p, nil
	}
	for _, d := range filepath.SplitList(path) {
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		cands := []string{filepath.Join(d, name)}
		if caps.Windows {
			cands = []string{filepath.Join(d, name+".exe"), filepath.Join(d, name+".cmd"), filepath.Join(d, name+".bat"), filepath.Join(d, name)}
		}
		for _, p := range cands {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && (caps.Windows || fi.Mode()&0o111 != 0) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s: not found on tm's PATH", name)
}

// runEnv is the environment an approved command of thread r gets: the
// server's own, as a session's starts (tm's bin first, the launching
// terminal's variables out), with r's TERMINATR_* variables and its own
// temp folder as TMPDIR. Nothing of the thread's own environment, and
// none of its secret variables.
func (s *Server) runEnv(r SessionRecord) []string {
	set := s.terminatrEnv(r)
	delete(set, "COLORTERM")
	set["TERM"] = "dumb"
	if d, err := threadTemp(r.Project, r.Thread); err == nil {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			set["TMPDIR"] = d
		}
	}
	return sessionEnv(withBinFirst(s.baseEnv(), s.opts.Bin), set)
}

// envValue is key's value in env.
func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && (k == key || caps.Windows && strings.EqualFold(k, key)) {
			return v
		}
	}
	return ""
}

// serveCommandRun answers command.run (approved commands, above) for
// the caller the pid walk finds.
func (s *Server) serveCommandRun(c net.Conn, br *bufio.Reader, req proto.Request, peerPID int) {
	s.commandRun(c, br, req, s.callerOf(peerPID))
}

// commandRun answers command.run for caller who.
func (s *Server) commandRun(c net.Conn, br *bufio.Reader, req proto.Request, who caller.Caller) {
	var p proto.CommandRunParams
	if perr := decodeParams(req.Params, &p); perr != nil {
		refuse(c, req, perr)
		return
	}
	r, perr := s.runRecord(who)
	if perr != nil {
		refuse(c, req, perr)
		return
	}
	cmds := s.approvedFor(r)
	if p.List {
		answer(c, req, proto.CommandRunResult{Commands: cmds})
		return
	}
	if why := checkArgv(p.Argv); why != "" {
		refuse(c, req, proto.Errorf(proto.ErrBadParams, "%s", why))
		return
	}
	i := slices.IndexFunc(cmds, func(x proto.ApprovedCommand) bool { return config.ApprovedMatch(x.Command, p.Argv) })
	line := shellLine(p.Argv)
	if i < 0 {
		refuse(c, req, proto.Errorf(proto.ErrRefused, "%s isn't an approved command of project %s (tm run --list lists them; only the user adds one)", textsafe.Clip(line, 200), r.Project))
		return
	}
	cmd := cmds[i]
	if perr := s.runGuard(r, line); perr != nil {
		s.journalRun(r, "run.refused", line+": the guard")
		refuse(c, req, perr)
		return
	}
	gone := clientGone(br)
	if cmd.Ask && len(line) > maxAskLine {
		refuse(c, req, proto.Errorf(proto.ErrRefused, "%s asks the user first, and is longer than the %d characters a question shows whole", textsafe.Clip(line, 80), maxAskLine))
		return
	}
	if cmd.Ask && !s.askRun(r.ID, line, gone) {
		s.journalRun(r, "run.refused", line+": the user didn't let it run")
		refuse(c, req, proto.Errorf(proto.ErrRefused, "the user didn't let %s run", textsafe.Clip(line, 200)))
		return
	}
	env := s.runEnv(r)
	path, err := lookPathIn(p.Argv[0], envValue(env, "PATH"), r.Cwd)
	if err != nil {
		refuse(c, req, proto.Errorf(proto.ErrBadParams, "%v", err))
		return
	}
	if why := batchArgs(path, p.Argv[1:]); why != "" {
		refuse(c, req, proto.Errorf(proto.ErrRefused, "%s", why))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x := exec.CommandContext(ctx, path, p.Argv[1:]...)
	x.Args[0] = p.Argv[0]
	if caps.OS == config.OSMac {
		// Confined away from tm's own files (runProfile), the human
		// socket above all; a sandbox-exec that can't apply it (a server
		// itself in a sandbox) fails the run rather than run it open.
		x = exec.CommandContext(ctx, sandboxExec, append([]string{"-p", runProfile(s.runClosed()), path}, p.Argv[1:]...)...)
	}
	x.Dir, x.Env = r.Cwd, env
	proc.Group(x)
	// Once it exits, what it left running doesn't hold its output open.
	x.WaitDelay = runWaitDelay
	out := &runWriter{c: c}
	x.Stdout, x.Stderr = out.stream("stdout"), out.stream("stderr")
	if !answer(c, req, proto.CommandRunResult{Command: cmd.Command, Cwd: r.Cwd}) {
		return
	}
	s.journalRun(r, "run.start", fmt.Sprintf("%s (approved %q)", line, cmd.Command))
	var ended <-chan struct{}
	if sess, _ := s.session(r.ID); sess != nil {
		ended = sess.Done()
	}
	if err := s.startRun(x, r); err != nil {
		out.end(proto.CommandRunEvent{Event: proto.EventRunExit, Code: -1, Error: err.Error()})
		s.journalRun(r, "run.end", line+": didn't start: "+err.Error())
		return
	}
	done := make(chan error, 1)
	go func() { done <- x.Wait() }()
	pid := x.Process.Pid
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var why string
	for why == "" {
		select {
		case err := <-done:
			s.endRun(pid)
			code, msg := exitOf(err)
			out.end(proto.CommandRunEvent{Event: proto.EventRunExit, Code: code, Error: msg})
			s.journalRun(r, "run.end", fmt.Sprintf("%s: exit %d", line, code))
			return
		case <-gone:
			why = "the thread's call ended"
		case <-ended:
			why = "the thread's session ended"
		case <-tick.C:
			if s.isStopping() {
				why = "the server stopped"
			}
		}
	}
	s.endRun(pid)
	<-done
	out.end(proto.CommandRunEvent{Event: proto.EventRunExit, Code: -1, Error: "killed: " + why})
	s.journalRun(r, "run.end", line+": killed, "+why)
}

// startRun starts x for thread r, its pid among the runs before any of
// its children can call tm.
func (s *Server) startRun(x *exec.Cmd, r SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, o := range s.runs {
		if o.ID == r.ID {
			n++
		}
	}
	if n >= maxRuns {
		return fmt.Errorf("%d approved commands of this thread run already: wait for one to end", n)
	}
	if err := x.Start(); err != nil {
		return err
	}
	if s.runs == nil {
		s.runs = map[int]SessionRecord{}
	}
	s.runs[x.Process.Pid] = r
	return nil
}

// endRun kills what is left of run pid's process group and forgets it.
func (s *Server) endRun(pid int) {
	proc.KillGroup(pid)
	s.mu.Lock()
	delete(s.runs, pid)
	s.mu.Unlock()
}

// exitOf is a run's exit code and, for one that didn't exit by itself,
// why.
func exitOf(err error) (int, string) {
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, ""
	case errors.As(err, &ee):
		if code := ee.ExitCode(); code >= 0 {
			return code, ""
		}
		return -1, ee.String()
	}
	return -1, err.Error()
}

// journalRun journals an approved command's run of thread r.
func (s *Server) journalRun(r SessionRecord, action, detail string) {
	p, err := project.Open(r.Project)
	if err != nil {
		return
	}
	who := caller.Caller{Kind: caller.Thread, Project: r.Project, Thread: r.Thread, Session: r.ID}
	if err := p.Journal(who, action, r.Thread, detail); err != nil {
		s.log.Printf("project %s: journal: %v", r.Project, err)
	}
	s.log.Printf("thread %s/%s: %s %s", r.Project, r.Thread, action, textsafe.Line(detail, 300))
}

// runWriter writes a run's output to its client as run.output lines,
// one writer at a time; after the client is gone, it drops them.
type runWriter struct {
	mu   sync.Mutex
	c    net.Conn
	dead bool
}

// stream is the writer of one of the run's streams.
func (w *runWriter) stream(name string) io.Writer {
	return writerFunc(func(b []byte) (int, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.c.SetWriteDeadline(time.Now().Add(runWriteWait))
		if !w.dead && proto.WriteLine(w.c, proto.CommandRunEvent{Event: proto.EventRunOutput, Stream: name, Data: b}) != nil {
			w.dead = true
		}
		return len(b), nil
	})
}

// end writes the last line.
func (w *runWriter) end(ev proto.CommandRunEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.dead {
		w.c.SetWriteDeadline(time.Now().Add(runWriteWait))
		proto.WriteLine(w.c, ev)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }
