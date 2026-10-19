package server

// The mod's guard (docs/SPEC.md §8.6, Guard): the standing rules as
// checks on each tool call. The server decides which rules a session's
// mod enforces, from the human's settings (config.toml: guard,
// guard_off, merge) and the session's role, adds the rules the user's
// enabled mods add (they only refuse: mods design §7, Tighten only), and
// serves them on the mod socket as GET /v1/rules; the mod matches each call itself
// (hooks/guard.ts), asking POST /v1/resolve where a file tool's writes
// land (realTarget: the mod has no file system), and reports a refusal
// with POST /v1/denied, which the server journals (and puts in the project's inbox only when a session is refused repeatedly).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// GuardRules is the body of GET /v1/rules: what the session's mod
// refuses, and what the server judges a hook's tool call by
// (guard.Rules.Judge). Off, nothing is refused and the agent's own
// permission rules decide alone, as without the mod.
type GuardRules = guard.Rules

// GuardDenial is the body of POST /v1/denied: the refusal's rule, the
// tool, the short summary the journal keeps, and for the guard's log
// (guardlog, the human's) the whole command line or file an allowance
// would name and whether one can (Floor: never).
type GuardDenial struct {
	Rule    string `json:"rule"`
	Tool    string `json:"tool"`
	Summary string `json:"summary,omitempty"`
	Subject string `json:"subject,omitempty"`
	Floor   bool   `json:"floor,omitempty"`
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
// no rule, allowance or mod rule, nothing asked.
func (s *Server) guardRulesFor(r SessionRecord) GuardRules {
	if r.Project == "" || (r.Role != proto.RoleThread && r.Role != proto.RoleCoordinator) {
		return GuardRules{}
	}
	lc := loadLaunchConfig()
	set := lc.safety(r.Project)
	// A thread's escapes, which its sandbox lets it ask for, are the
	// user's to answer, the guard on or off (T574 case 6).
	askEscapes := r.Role == proto.RoleThread && !r.Yolo && !r.NoSandboxEscapes
	// So are its excluded commands that ask first (T574 case 1).
	var askFirst []string
	if r.Role == proto.RoleThread && !r.Yolo {
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
	g.AskEscapes, g.AskFirst = askEscapes, askFirst
	if s.opts.Paths.Home != "" {
		g.Worktrees = realPath(filepath.Join(s.opts.Paths.Home, "worktrees"))
	}
	if r.Yolo {
		g.Learn, g.Allow = false, nil
	}
	for _, id := range config.GuardRules {
		if r.Yolo {
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
	if r.Role == proto.RoleThread && !r.Yolo {
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
	if !r.Yolo {
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
// whatever the rules (the guard's floor, T574 §3.1): tm's settings
// (config.toml), and a thread's project folder, or the parts of it only
// tm writes for the coordinator, whose folder it is. The sandbox and the
// agent's deny rules hold them too, where there are those (not under
// yolo).
func (s *Server) guardNoWrite(r SessionRecord) []string {
	var out []string
	if h := s.opts.Paths.Home; h != "" {
		out = append(out, filepath.Join(h, "config.toml"))
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

// GuardEscape is POST /v1/escape's body: the call that asks to leave
// the sandbox, by the mod's id for it, and whether it has ended.
type GuardEscape struct {
	ID   string `json:"id"`
	Done bool   `json:"done,omitempty"`
}

// GuardResolve is POST /v1/resolve's body and answer: the paths a file
// tool writes, absolute but not cleaned, and where each lands
// (realTarget), in the same order.
type GuardResolve struct {
	Paths []string `json:"paths"`
}

// maxResolve bounds how many paths one POST /v1/resolve may ask about.
const maxResolve = 256

// guardRoutes adds the guard's routes to session id's mod handler.
func (s *Server) guardRoutes(mux *http.ServeMux, id string) {
	mux.HandleFunc("POST /v1/resolve", func(w http.ResponseWriter, r *http.Request) {
		var q GuardResolve
		b, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err == nil {
			err = json.Unmarshal(b, &q)
		}
		if err == nil && len(q.Paths) > maxResolve {
			err = fmt.Errorf("more than %d paths", maxResolve)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := GuardResolve{Paths: make([]string, len(q.Paths))}
		for i, p := range q.Paths {
			// Only an absolute path is followed; the mod sends no other.
			out.Paths[i] = p
			if filepath.IsAbs(filepath.FromSlash(p)) {
				out.Paths[i] = realTarget(p)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /v1/rules", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		rec, ok := s.records[id]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "session "+id+" is gone", http.StatusGone)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.hookRules(rec))
	})
	// The guard asked about a call that leaves the sandbox: the prompt it
	// brings is the user's (T574 case 6), which tm thread approve and the
	// ticker read from the session's state (AgentState.Escape).
	// Its body names the call (the mod's own id) and, once the call has
	// ended, says done.
	mux.HandleFunc("POST /v1/escape", func(w http.ResponseWriter, r *http.Request) {
		var e GuardEscape
		b, err := io.ReadAll(io.LimitReader(r.Body, maxModReport))
		if err == nil {
			err = json.Unmarshal(b, &e)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		sess := s.sessions[id]
		s.mu.Unlock()
		if sess == nil {
			http.Error(w, "session "+id+" is gone", http.StatusGone)
			return
		}
		if e.Done {
			sess.EndEscape(e.ID)
		} else {
			sess.MarkEscape(e.ID)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /v1/denied", func(w http.ResponseWriter, r *http.Request) {
		var d GuardDenial
		b, err := io.ReadAll(io.LimitReader(r.Body, maxModReport))
		if err == nil {
			err = json.Unmarshal(b, &d)
		}
		mods := strings.HasPrefix(d.Rule, "mod:")
		if err == nil && !mods && !slices.Contains(config.GuardRules, d.Rule) && d.Rule != guard.RuleTMFiles {
			err = fmt.Errorf("unknown rule %q", d.Rule)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		rec, ok := s.records[id]
		s.mu.Unlock()
		if !ok {
			http.Error(w, "session "+id+" is gone", http.StatusGone)
			return
		}
		// A mod's rule is one the session has.
		if mods && !slices.ContainsFunc(s.guardMods(rec, ""), func(m guard.ModRule) bool { return m.Name() == d.Rule }) {
			http.Error(w, fmt.Sprintf("unknown rule %q", d.Rule), http.StatusBadRequest)
			return
		}
		s.guardDenied(rec, d)
		w.WriteHeader(http.StatusNoContent)
	})
}

// hookGuardOf judges the tool calls session id's hooks report against
// its rules (agent.HookEnv.Guard: agents without a mod, which answer
// PreToolUse with the refusal, or an ask) and records a refusal as POST
// /v1/denied does; an ask is not recorded. A file tool's write is judged
// where it lands, through the links on its path (realTarget). With a
// mod, the mod judges: nil.
func (s *Server) hookGuardOf(id string, mod bool) func(string, map[string]any) *guard.Denial {
	if mod {
		return nil
	}
	return func(tool string, input map[string]any) *guard.Denial {
		s.mu.Lock()
		rec, ok := s.records[id]
		s.mu.Unlock()
		if !ok {
			return nil
		}
		rules := s.hookRules(rec)
		rules.Resolve = realTarget
		d := rules.Judge(tool, input)
		if d != nil && !d.Ask {
			s.guardDenied(rec, GuardDenial{Rule: d.Rule, Tool: tool, Summary: d.Summary, Subject: d.Subject, Floor: d.Floor})
		}
		return d
	}
}

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

// hookRules is session r's rules, for its hooks and its mod (GET
// /v1/rules): worked out again when what they are made of changed (the
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
		} else if listed && !e.Floor && guardSettings(r.Project).GuardMode == config.GuardLearn {
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
