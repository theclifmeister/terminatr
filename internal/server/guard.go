package server

// The guard (docs/SPEC.md §8.6, Guard): the standing rules as checks on
// each tool call. The server decides which rules a session's guard
// enforces, from the human's settings (config.toml: guard, guard_off,
// merge) and the session's role, adds the rules the user's enabled mods
// add (they only refuse: mods design §7, Tighten only), and judges each
// call the session's PreToolUse hook reports (hookGuardOf). A refusal is
// journaled (and put in the project's inbox only when a session is
// refused repeatedly).

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/guardlog"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// GuardRules is what the server judges a hook's tool call by
// (guard.Rules.Judge). Off, nothing is refused and the agent's own
// permission rules decide alone.
type GuardRules = guard.Rules

// GuardDenial is a refusal: its rule, the tool, the short summary the journal keeps, and for the guard's log
// (guardlog, the human's) the whole command line or file an allowance
// would name and whether one can (Floor: never).
type GuardDenial struct {
	Rule    string
	Tool    string
	Summary string
	Subject string
	Floor   bool
}

// A refusal is journaled and shown on the thread's info panel, not put
// in the inbox: only a session refused guardInboxCount times within
// guardInboxWindow files one item, and the count starts again after it.
const (
	guardInboxCount  = 3
	guardInboxWindow = 10 * time.Minute
)

// guardNow is the clock of the inbox window (a variable for tests).
var guardNow = time.Now

// guardSettings is a project's resolved settings; a config.toml that
// doesn't load leaves the defaults, so the guard stays on.
func guardSettings(slug string) config.Safety {
	return loadLaunchConfig().safety(slug)
}

// guardAllows are a project's allowances as the matcher takes them: a
// path from ~ made absolute, and once more with the symlinks of the
// folder before its first * (an exact one's, its folder) resolved, as
// writes are judged where they land.
func guardAllows(list []config.GuardAllow, home string) []guard.Allow {
	var out []guard.Allow
	for _, a := range list {
		g := guard.Allow{Rule: a.Rule, Command: a.Command, Path: a.Path, Exact: a.Exact}
		if rest, ok := strings.CutPrefix(a.Path, "~/"); ok && home != "" {
			g.Path = filepath.Join(home, filepath.FromSlash(rest))
		}
		out = append(out, g)
		if g.Path == "" {
			continue
		}
		fixed, wild, pattern := g.Path, "", false
		if !g.Exact {
			fixed, wild, pattern = strings.Cut(g.Path, "*")
		}
		dir, tail := fixed, ""
		if !strings.HasSuffix(fixed, string(filepath.Separator)) && !strings.HasSuffix(fixed, "/") {
			dir, tail = filepath.Dir(fixed), fixed[len(filepath.Dir(fixed)):]
		}
		if real := realPath(filepath.Clean(dir)); real != filepath.Clean(dir) {
			r := g
			r.Path = strings.TrimSuffix(real, string(filepath.Separator)) + string(filepath.Separator) + strings.TrimLeft(tail, `/\`)
			if pattern {
				r.Path += "*" + wild
			}
			out = append(out, r)
		}
	}
	return out
}

// guardOwn are tm's own files no agent reads, whatever the rules (the
// guard's floor): the run folder (the sockets, the sessions' runtime
// folders), the server's keys and the guard's log.
func (s *Server) guardOwn() []string {
	var out []string
	if d := s.opts.Paths.RunDir; d != "" {
		out = append(out, d)
	}
	if h := s.opts.Paths.Home; h != "" {
		out = append(out, peer.DirIn(h), guardlog.Dir(h))
	}
	return withReal(out)
}

// guardRulesFor is session record r's rules. Only a project's agents are
// guarded; a coordinator neither merges under a thread's rule nor keeps
// to a worktree. Under yolo the guard keeps its floor only (T574 case 4):
// no rule, allowance or mod rule, nothing asked. So it does below
// protection guarded, read live; a thread without its sandbox has no
// escapes or excluded commands to ask about.
func (s *Server) guardRulesFor(r SessionRecord) GuardRules {
	if r.Project == "" || (r.Role != proto.RoleThread && r.Role != proto.RoleCoordinator) {
		return GuardRules{}
	}
	lc := loadLaunchConfig()
	set := lc.safety(r.Project)
	// A thread's escapes, which its sandbox lets it ask for, are the
	// user's to answer, the guard on or off (T574 case 6).
	sandboxed := r.Role == proto.RoleThread && !r.Yolo && !r.NoSandbox
	askEscapes := sandboxed && !r.NoSandboxEscapes
	// So are its excluded commands that ask first (T574 case 1).
	var askFirst []string
	if sandboxed {
		askFirst = s.askFirst(r, lc)
	}
	if !set.Guard {
		if !askEscapes && len(askFirst) == 0 {
			return GuardRules{}
		}
		// Off: no rule and no floor (Floor nil says so), only the
		// questions about escapes and excluded commands.
		s.mu.Lock()
		reg := s.agents
		s.mu.Unlock()
		return GuardRules{On: true, Role: r.Role, Cwd: r.Cwd, CaseFold: caps.CaseFold, Tools: agent.GuardTools(reg, r.Agent), AskEscapes: askEscapes, AskFirst: askFirst}
	}
	home, _ := os.UserHomeDir()
	g := GuardRules{On: true, Role: r.Role, Home: home, Cwd: r.Cwd, CaseFold: caps.CaseFold,
		Learn: set.GuardMode == config.GuardLearn, Allow: guardAllows(set.GuardAllow, home), Own: s.guardOwn()}
	if r.Role == proto.RoleThread {
		g.SecretEnv = lc.cfg.SecretEnvNames(r.Project)
		// The other threads' temp folders (T587): the sandbox keeps Bash
		// out of them, the floor its file tools.
		if own, err := threadTemp(r.Project, r.Thread); err == nil {
			g.OtherTemps, g.OwnTemp = withReal([]string{threadTempRoot()}), own
		}
	}
	// The floor holds with its rule off (guard_off): only the guard off
	// lifts it.
	g.Floor = []string{"force-push"}
	if r.Role == proto.RoleThread && set.Merge == config.MergeCoordinator {
		g.Floor = append(g.Floor, "merge")
	}
	g.NoWrite = s.guardNoWrite(r)
	g.CdPath = s.sessionCdPath(lc.cfg.SecretEnvNames(r.Project), home)
	if home != "" {
		g.NoWriteNames = withLanding(agent.ShellPatterns(home, os.Getenv))
	}
	// The project lets its coordinator change its access: the floor lets
	// its tm access writes through, which tm checks in the server.
	g.AccessWrites = r.Role == proto.RoleCoordinator && set.CoordinatorAccess
	g.AskEscapes, g.AskFirst = askEscapes, askFirst
	if s.opts.Paths.Home != "" {
		g.Worktrees = realPath(filepath.Join(s.opts.Paths.Home, "worktrees"))
	}
	floorOnly := r.Yolo || !set.GuardRulesOn()
	if floorOnly {
		g.Learn, g.Allow = false, nil
	}
	for _, id := range config.GuardRules {
		if floorOnly {
			break
		}
		if slices.Contains(set.GuardOff, id) {
			continue
		}
		switch id {
		case "worktree-only":
			if r.Role != proto.RoleThread || r.Cwd == "" {
				continue
			}
		case "merge":
			if r.Role != proto.RoleThread || set.Merge != config.MergeCoordinator {
				continue
			}
		}
		g.Rules = append(g.Rules, id)
	}
	s.mu.Lock()
	reg := s.agents
	s.mu.Unlock()
	if slices.Contains(g.Rules, "worktree-only") {
		dirs := append([]string{r.Cwd}, fsx.TempRoots()...)
		if d, err := threadTemp(r.Project, r.Thread); err == nil {
			dirs = append(dirs, d) // its own temp folder (T587)
		}
		dirs = append(dirs, agent.GuardWritable(reg, r.Agent, home, os.Getenv)...)
		dirs = append(dirs, writeFolders(lc, r.Project, s.tmDirs())...)
		g.Writable = withReal(dirs)
		g.GitConfig = withReal(threadGitConfig(r))
	}
	g.Tools = agent.GuardTools(reg, r.Agent)
	if sandboxed {
		g.Unsandboxed, g.TMExcluded = ownExcluded(reg, r, home, lc)
		g.NoEscapes = r.NoSandboxEscapes
	}
	g.Protected = []string{"main", "master"}
	repos := []string{r.Cwd}
	if r.Role == proto.RoleCoordinator {
		if p, err := project.Open(r.Project); err == nil {
			repos = p.Repos
		}
	}
	for _, repo := range repos {
		// origin's default branch only: a worktree's own HEAD is the
		// thread's branch.
		if b, ok := strings.CutPrefix(worktree.DefaultRef(repo), "origin/"); ok && !slices.Contains(g.Protected, b) {
			g.Protected = append(g.Protected, b)
		}
	}
	g.Secrets = withReal(agent.Secrets(reg, home, os.Getenv))
	if !floorOnly {
		g.Mods = s.guardMods(r, home)
	}
	return g
}

// askFirst are the excluded commands thread session r asks first about
// (T574 case 1): those of the project's that ask first now, and those
// that asked first when it launched (its launch record), which its
// launch settings still run outside the sandbox though config.toml no
// longer lists them. Only for an agent that gets excluded commands
// (Claude's); none when its lists are dropped.
func (s *Server) askFirst(r SessionRecord, lc launchConfig) []string {
	var out []string
	add := func(rules []proto.AccessRule) {
		for _, x := range rules {
			if x.Kind == proto.AccessExcluded && x.Note == proto.AccessAsksFirst && x.Given() && !slices.Contains(out, x.Value) &&
				(len(x.Agents) == 0 || slices.Contains(x.Agents, r.Agent)) {
				out = append(out, x.Value)
			}
		}
	}
	rules, _ := sandboxRules(lc, r.Project, caps.OS, s.tmDirs())
	add(rules)
	if p, err := project.Open(r.Project); err == nil && thread.ValidID(r.Thread) {
		if set, ok := readLaunchRecord(p, r.Thread); ok {
			add(set.Rules)
		}
	}
	return out
}

// writeFolders are project slug's write folders the guard lets file
// tools write in (config write, live): absolute, without those the
// launch leaves out.
func writeFolders(lc launchConfig, slug string, tm tmDirs) []string {
	rules, _ := sandboxRules(lc, slug, caps.OS, tm)
	var out []string
	for _, r := range rules {
		if r.Kind == proto.AccessWriteFolder && r.Given() {
			out = append(out, r.Value)
		}
	}
	return out
}

// agentOf is reg's agent name, nil for none.
func agentOf(reg *agent.Registry, name string) agent.Agent {
	a, _ := reg.Get(name)
	return a
}

// ownExcluded are the excluded commands of thread r's agent's own
// settings files (agent.OwnRuler: Claude's ~/.claude/settings.json and
// its worktree's .claude), read live, and the ones tm gives it (config
// sandbox): the guard treats a command the first run outside the
// sandbox, and none of tm's, as an escape (T574 case 7). tm doesn't
// change the user's files.
func ownExcluded(reg *agent.Registry, r SessionRecord, home string, lc launchConfig) (own, tm []string) {
	if reg == nil {
		return nil, nil
	}
	a, ok := reg.Get(r.Agent)
	if !ok {
		return nil, nil
	}
	or, ok := a.(agent.OwnRuler)
	if !ok || home == "" {
		return nil, nil
	}
	for _, o := range or.OwnRules(home, r.Cwd) {
		if o.Kind == proto.AccessExcluded && !slices.Contains(own, o.Value) {
			own = append(own, o.Value)
		}
	}
	if len(own) == 0 {
		return nil, nil
	}
	if lc.err == nil {
		tm = lc.cfg.ThreadSandbox(r.Project).Given(config.SandboxExcluded)
	}
	return own, tm
}

// guardNoWrite are the files and folders no tool of session r writes,
// whatever the rules (the guard's floor, T574 §3.1): tm's home but for
// the projects and worktrees (homeFloor: its settings, agents, mods,
// state, logs) and its run folder, the settings in the session's folder that could turn
// its hooks off (hookSettings), what a shell runs before each command
// (the user's shell startup files and the agents' shell snapshots,
// agent.ShellState: a cd or exit defined there is one the guard can't
// see), and a thread's project folder, or the
// parts of it only tm writes for the coordinator, whose folder it is. The sandbox and the
// agent's deny rules hold them too, where there are those (not under
// yolo).
func (s *Server) guardNoWrite(r SessionRecord) []string {
	var out []string
	if h := s.opts.Paths.Home; h != "" {
		out = append(out, homeFloor(h)...)
	}
	if run := s.opts.Paths.RunDir; run != "" {
		out = append(out, realPath(run), run)
	}
	if r.Cwd != "" && (r.Role == proto.RoleThread || r.Role == proto.RoleCoordinator) {
		out = append(out, withLanding(hookSettings(r.Cwd))...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		s.mu.Lock()
		reg := s.agents
		s.mu.Unlock()
		out = append(out, withLanding(agent.ShellState(reg, home, os.Getenv))...)
	}
	dir, err := project.Dir(r.Project)
	if err != nil {
		return withReal(out)
	}
	switch r.Role {
	case proto.RoleThread:
		out = append(out, dir)
	case proto.RoleCoordinator:
		dirs, files := (&project.Project{Slug: r.Project, Dir: dir}).Owned()
		out = append(append(out, dirs...), files...)
	}
	return withReal(out)
}

// sessionCdPath: session r's shell may start with CDPATH or zsh's
// cdpath set, or cdable_vars on (guard.Rules.CdPath), so the guard
// can't place a relative cd: the server's environment, which sessions
// inherit, has it (bash's BASHOPTS may turn cdable_vars on), tm gives
// it (env, its names), or a file the shell runs before each command
// names it in any case: Claude's env file ($CLAUDE_ENV_FILE), the
// user's startup files and the agents' shell snapshots
// (agent.ShellState), each folder's newest cdPathFiles.
func (s *Server) sessionCdPath(env []string, home string) bool {
	if os.Getenv("CDPATH") != "" || cdPathName.MatchString(os.Getenv("BASHOPTS")) ||
		slices.ContainsFunc(env, func(n string) bool { return cdPathName.MatchString(n) || n == "BASHOPTS" }) {
		return true
	}
	files := []string{os.Getenv("CLAUDE_ENV_FILE")}
	if home != "" {
		s.mu.Lock()
		reg := s.agents
		s.mu.Unlock()
		files = append(files, agent.ShellState(reg, home, os.Getenv)...)
	}
	for _, f := range files {
		if f != "" && namesCdPath(f) {
			return true
		}
	}
	return false
}

// cdPathName names CDPATH or zsh's cdpath, or bash's cdable_vars or
// zsh's cdablevars, any case.
var cdPathName = regexp.MustCompile(`(?i)cd_?path|cd_?able_?vars`)

// cdPathFiles and cdPathRead bound what namesCdPath reads of a folder:
// its newest files, and of each its start.
const (
	cdPathFiles = 32
	cdPathRead  = 1 << 20
)

// namesCdPath: file p, or one of the newest files in folder p (and
// their folders' one level down), names CDPATH, cdpath or cdable_vars
// (cdPathName).
func namesCdPath(p string) bool {
	fi, err := os.Stat(p)
	switch {
	case err != nil:
		return false
	case !fi.IsDir():
		f, err := os.Open(p)
		if err != nil {
			return false
		}
		defer f.Close()
		b, _ := io.ReadAll(io.LimitReader(f, cdPathRead))
		return cdPathName.Match(b)
	}
	es, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	type entry struct {
		path string
		mod  time.Time
	}
	var all []entry
	for _, e := range es {
		if i, err := e.Info(); err == nil && (i.Mode().IsRegular() || i.IsDir()) {
			all = append(all, entry{filepath.Join(p, e.Name()), i.ModTime()})
		}
	}
	slices.SortFunc(all, func(a, b entry) int { return b.mod.Compare(a.mod) })
	for _, e := range all[:min(len(all), cdPathFiles)] {
		if fi, err := os.Stat(e.path); err == nil && fi.IsDir() {
			// One level down only (.config/fish/conf.d).
			sub, _ := os.ReadDir(e.path)
			for _, x := range sub[:min(len(sub), cdPathFiles)] {
				if x.Type().IsRegular() && namesCdPath(filepath.Join(e.path, x.Name())) {
					return true
				}
			}
			continue
		}
		if namesCdPath(e.path) {
			return true
		}
	}
	return false
}

// homeFloor are what no session writes in tm's home h: everything but
// projects/ and worktrees/, whose own rules hold (a thread writes its
// worktree; the coordinator its project folder but for tm's parts).
// The names tm keeps there, whether they exist yet or not, and anything
// else found there.
func homeFloor(h string) []string {
	names := []string{"config.toml", "ui.json", "agents", "mods", "cache", "run", "state", "logs", "bin", ".trash"}
	if es, err := os.ReadDir(h); err == nil {
		for _, e := range es {
			names = append(names, e.Name())
		}
	}
	var paths []string
	for _, n := range names {
		if n != "projects" && n != "worktrees" {
			paths = append(paths, filepath.Join(h, n))
		}
	}
	return withLanding(paths)
}

// hookSettings are the agent settings files in a session's folder dir
// that could turn its hooks off (Claude's .claude/settings.json and
// settings.local.json, disableAllHooks): refused whole, whatever they
// would say, as a shell's write can't be read for what it writes.
func hookSettings(dir string) []string {
	return []string{filepath.Join(dir, ".claude", "settings.json"), filepath.Join(dir, ".claude", "settings.local.json")}
}

// guardMods are the guard rules the user's mods that run for session
// r's project add for its role, labelled with each mod: commands as
// words, paths from ~ made absolute and with their symlinks resolved.
// They only add refusals, so mods that can't be read add none.
func (s *Server) guardMods(r SessionRecord, home string) []guard.ModRule {
	reg := s.modRegistry()
	if reg == nil {
		return nil
	}
	abs := func(paths []string) []string {
		var out []string
		for _, p := range paths {
			if rest, ok := strings.CutPrefix(p, "~/"); ok {
				p = filepath.Join(home, filepath.FromSlash(rest))
			}
			out = append(out, p)
		}
		return withReal(out)
	}
	var out []guard.ModRule
	for _, g := range reg.GuardRules(r.Role, r.Project) {
		m := guard.ModRule{Mod: g.Mod, ID: g.ID, Writes: abs(g.Writes), Reads: abs(g.Reads), Message: strings.TrimSpace(mod.Clean(g.Message, false))}
		for _, c := range g.Commands {
			m.Commands = append(m.Commands, strings.Fields(c))
		}
		out = append(out, m)
	}
	return out
}

// withLanding is paths, each as written and where a write to it lands
// (realTarget: the real path of its nearest existing folder, the rest
// joined on as written), without repeats. A path whose folders don't
// exist yet has no real path of its own, but a link above them still
// carries the write elsewhere (macOS's /var to /private/var).
func withLanding(paths []string) []string {
	var out []string
	for _, p := range paths {
		for _, q := range []string{filepath.Clean(p), realTarget(p)} {
			if !slices.Contains(out, q) {
				out = append(out, q)
			}
		}
	}
	return out
}

// withReal is paths, each followed by its symlinks resolved where that
// differs, without repeats.
func withReal(paths []string) []string {
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		for _, q := range []string{p, realPath(p)} {
			if !slices.Contains(out, q) {
				out = append(out, q)
			}
		}
	}
	return out
}

// regularFile: p (absolute, not cleaned) lands on a regular file, its
// links followed (realTarget).
func regularFile(p string) bool {
	fi, err := os.Stat(realTarget(p))
	return err == nil && fi.Mode().IsRegular()
}

// realTarget is where a write to p (absolute, not cleaned) lands: p with
// the symlinks and junctions of its longest existing prefix followed, a
// .. after one leaving the link's target as the file system does; what
// doesn't exist yet is joined on as written. A link a thread makes inside
// its worktree doesn't carry a write out of it.
func realTarget(p string) string {
	p = filepath.FromSlash(p)
	split := func(p string) []string {
		return strings.FieldsFunc(p, func(c rune) bool { return c == '/' || c == filepath.Separator })
	}
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	parts := split(p[len(vol):])
	for hops := 0; len(parts) > 0; {
		c := parts[0]
		parts = parts[1:]
		switch c {
		case ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, c)
		fi, err := os.Lstat(next)
		if err != nil {
			return filepath.Join(append([]string{next}, parts...)...)
		}
		// A junction is no symlink to Lstat, but Readlink reads it.
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 && hops < 40 {
			if t, err := os.Readlink(next); err == nil {
				hops++
				switch {
				case filepath.IsAbs(t):
				case t != "" && filepath.VolumeName(t) == "" && strings.ContainsRune(`/`+string(filepath.Separator), rune(t[0])):
					// Rooted on the link's own drive (Windows' \x).
					t = filepath.VolumeName(cur) + t
				default:
					t = cur + string(filepath.Separator) + t
				}
				vol := filepath.VolumeName(t)
				cur = vol + string(filepath.Separator)
				parts = append(split(t[len(vol):]), parts...)
				continue
			}
		}
		cur = next
	}
	return cur
}

// hookGuardOf judges the tool calls session id's hooks report against
// its rules (agent.HookEnv.Guard: the agent answers PreToolUse with the
// refusal, or an ask) and records a refusal; an ask is not recorded. A
// file tool's write is judged where it lands, through the links on its
// path (realTarget).
//
// It fails closed: a session without its record (exiting) gets
// guard.Unchecked, and so does a call not judged within judgeLimit
// (the rules' config and git reads included), well inside what tm hook
// waits for the verdict (agent.HookGuardWait). The judge gets the same
// deadline (guard.Rules.Deadline) and stops there, and a session has
// one judge at a time: a call waits for the one before it.
func (s *Server) hookGuardOf(id string) func(string, map[string]any) *guard.Denial {
	// busy holds the session's one judge: a call waits for the one
	// before it, which its deadline ends, within its own limit.
	busy := make(chan struct{}, 1)
	return func(tool string, input map[string]any) *guard.Denial {
		s.mu.Lock()
		rec, ok := s.records[id]
		s.mu.Unlock()
		if !ok {
			return guard.Unchecked()
		}
		judge, limit := judgeCall, judgeLimit
		deadline := time.Now().Add(limit)
		var d *guard.Denial
		select {
		case busy <- struct{}{}:
			done := make(chan *guard.Denial, 1)
			go func() {
				defer func() { <-busy }()
				rules := s.hookRules(rec)
				rules.Resolve, rules.File = realTarget, regularFile
				rules.Deadline = deadline
				done <- judge(rules, tool, input)
			}()
			select {
			case d = <-done:
			case <-time.After(time.Until(deadline)):
				d = tooLong(s, id, tool, limit)
			}
		case <-time.After(limit):
			d = tooLong(s, id, tool, limit)
		}
		if d != nil && !d.Ask {
			s.guardDenied(rec, GuardDenial{Rule: d.Rule, Tool: tool, Summary: d.Summary, Subject: d.Subject, Floor: d.Floor})
		}
		return d
	}
}

// tooLong is the refusal of a call not judged within limit.
func tooLong(s *Server, id, tool string, limit time.Duration) *guard.Denial {
	d := guard.Unchecked()
	d.Message = "terminatr guard: checking this call took too long, so it is refused; try a shorter or simpler command."
	s.log.Printf("session %s: the guard took over %v to judge a %s call: refused", id, limit, tool)
	return d
}

// judgeLimit bounds how long the server judges one tool call; a
// variable for tests.
var judgeLimit = 1500 * time.Millisecond

// judgeCall is how hookGuardOf judges a call: Rules.Judge (tests hold
// it).
var judgeCall = guard.Rules.Judge

// hookRulesFor is how long a session's rules are kept for its hooks: a
// tool call must not wait on config.toml and git each time, and a
// change of the human's settings still reaches running sessions.
const hookRulesFor = time.Minute

// guardState is the guard's memory of the sessions, by session id: the
// rules cached for their hooks and when they were last refused. A
// session's entries go when it exits (forget). The zero value is ready.
type guardState struct {
	mu     sync.Mutex
	rules  map[string]hookRulesEntry
	recent map[string][]time.Time // the refusals within guardInboxWindow
}

// forget drops session id's entries.
func (g *guardState) forget(id string) {
	g.mu.Lock()
	delete(g.rules, id)
	delete(g.recent, id)
	g.mu.Unlock()
}

type hookRulesEntry struct {
	rules GuardRules
	at    time.Time
	stamp string
}

// hookRules is session r's rules, for its hooks: worked out again when what they are made of changed (the
// human's settings, the project, a mod enabled, disabled, updated or
// removed: guardStamp), so a guard mod tightens or loosens running
// sessions at once (T301 B4), and at least once per hookRulesFor for
// what only git says (the protected branches).
func (s *Server) hookRules(r SessionRecord) GuardRules {
	now := guardNow()
	stamp := s.guardStamp(r)
	s.guard.mu.Lock()
	e, ok := s.guard.rules[r.ID]
	s.guard.mu.Unlock()
	if ok && e.stamp == stamp && now.Sub(e.at) < hookRulesFor {
		return e.rules
	}
	g := s.guardRulesFor(r)
	s.guard.mu.Lock()
	for id, old := range s.guard.rules {
		if now.Sub(old.at) >= hookRulesFor {
			delete(s.guard.rules, id)
		}
	}
	if s.guard.rules == nil {
		s.guard.rules = map[string]hookRulesEntry{}
	}
	s.guard.rules[r.ID] = hookRulesEntry{rules: g, at: now, stamp: stamp}
	s.guard.mu.Unlock()
	return g
}

// guardStamp says, cheaply (stats only), whether what session r's rules
// are made of changed: the installed mods and config.toml
// (s.installedMods, which reads them anew when they did, so the rules
// worked out next see the change), the project's PROJECT.md (its repos
// until they move to config.toml, project.MoveRepos) and a thread's
// agent's own settings files.
func (s *Server) guardStamp(r SessionRecord) string {
	_, _, stamp := s.installedMods(0)
	var b strings.Builder
	b.WriteString(stamp)
	if r.Project != "" {
		if dir, err := project.Dir(r.Project); err == nil {
			stampFile(&b, filepath.Join(dir, "PROJECT.md"))
		}
	}
	// A thread's agent's own settings files, whose excluded commands are
	// its escapes (ownExcluded): the thread writes its worktree's, so a
	// change must not wait for hookRulesFor.
	if r.Role == proto.RoleThread {
		s.mu.Lock()
		reg := s.agents
		s.mu.Unlock()
		if reg != nil {
			if or, ok := agentOf(reg, r.Agent).(agent.OwnRuler); ok {
				home, _ := os.UserHomeDir()
				for _, f := range or.OwnFiles(home, r.Cwd) {
					stampFile(&b, f)
				}
			}
		}
	}
	return b.String()
}

// repeated records a refusal of session id and says whether it is the
// guardInboxCount-th within guardInboxWindow, which starts the count
// again.
func (g *guardState) repeated(id string) bool {
	now := guardNow()
	g.mu.Lock()
	defer g.mu.Unlock()
	keep := g.recent[id][:0]
	for _, t := range g.recent[id] {
		if now.Sub(t) < guardInboxWindow {
			keep = append(keep, t)
		}
	}
	keep = append(keep, now)
	if len(keep) >= guardInboxCount {
		delete(g.recent, id)
		return true
	}
	if g.recent == nil {
		g.recent = map[string][]time.Time{}
	}
	g.recent[id] = keep
	return false
}

// guardDenied journals denial d of session r's mod; the thread's info
// panel shows it from there. Only when the session keeps being refused
// (guardInboxCount times in guardInboxWindow) does it tell the
// coordinator, with one inbox item. The tool's own words never reach
// either: the summary is the mod's, cut short and folded to one line.
// The whole call goes to the guard's log only (guardlog), the human's,
// for the dashboard's Guard refusals; in learn mode a call newly listed
// there is put to the user with an inbox item.
func (s *Server) guardDenied(r SessionRecord, d GuardDenial) {
	s.log.Printf("session %s: guard denied %s (%s)", r.ID, d.Rule, d.Tool)
	if r.Project == "" {
		return
	}
	p, err := project.Open(r.Project)
	if err != nil {
		return
	}
	who := caller.Caller{Kind: caller.Coordinator, Project: r.Project}
	ref := r.ID
	if r.Role == proto.RoleThread && r.Thread != "" {
		who = caller.Caller{Kind: caller.Thread, Project: r.Project, Thread: r.Thread}
		ref = r.Thread
	}
	summary := oneLine(d.Summary, 120)
	tool := oneLine(d.Tool, 40)
	if err := p.Journal(who, "guard.deny", ref, fmt.Sprintf("%s %s: %s", d.Rule, tool, summary)); err != nil {
		s.log.Printf("session %s: journal: %v", r.ID, err)
	}
	if s.opts.Paths.Home != "" && d.Subject != "" {
		e := guardlog.Entry{Project: r.Project, Session: r.ID, Role: r.Role, Rule: d.Rule, Tool: tool, Summary: summary,
			Subject: d.Subject, Floor: d.Floor, Path: s.guardFileTool(r, d.Tool)}
		if r.Role == proto.RoleThread {
			e.Thread = r.Thread
		}
		e, listed, err := guardlog.Record(s.opts.Paths.Home, e, guardNow())
		if err != nil {
			s.log.Printf("session %s: guard log: %v", r.ID, err)
		} else if listed && e.Actionable() && guardSettings(r.Project).GuardMode == config.GuardLearn {
			msg := fmt.Sprintf("%s: the guard refused a %s call (%s: %s) and learns from your answer: allow it or keep refusing it in the project's Settings, Guard refusals",
				ref, tool, d.Rule, summary)
			if _, err := p.AddItem("guard", ref, msg, true); err != nil {
				s.log.Printf("session %s: inbox: %v", r.ID, err)
			}
		}
	}
	if !s.guard.repeated(r.ID) {
		return
	}
	msg := fmt.Sprintf("%s: the guard refused %d calls in %d minutes, the last a %s call (%s): %s",
		ref, guardInboxCount, int(guardInboxWindow/time.Minute), tool, d.Rule, summary)
	if _, err := p.AddItem("guard", ref, msg, false); err != nil {
		s.log.Printf("session %s: inbox: %v", r.ID, err)
	}
}

// guardFileTool: tool is one of session r's agent's file tools, whose
// refusals name a file rather than a command line.
func (s *Server) guardFileTool(r SessionRecord, tool string) bool {
	s.mu.Lock()
	reg := s.agents
	s.mu.Unlock()
	t, ok := agent.GuardTools(reg, r.Agent)[tool]
	return ok && t.Kind != guard.KindShell
}

// oneLine is s folded to one line of at most n runes, cleaned
// (textsafe.Line): what it folds comes from agents and mods.
func oneLine(s string, n int) string { return textsafe.Line(s, n) }
