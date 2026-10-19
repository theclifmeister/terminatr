package server

// The agent layer of the server (docs/SPEC.md §8, M3): the manifest
// registry, launching and resuming agent sessions with their generated
// files, hook events, prompts, waits and explain. Nothing here names an
// agent; everything agent-specific comes through internal/agent.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/session"
	"github.com/theclifmeister/terminatr/internal/skill"
	"github.com/theclifmeister/terminatr/internal/theme"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/ticker"
	"github.com/theclifmeister/terminatr/internal/version"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// loadAgents (re)reads the registry. Broken user manifests are logged
// and skipped; the built-ins always load.
func (s *Server) loadAgents() {
	reg, err := agent.Load(s.opts.Paths.AgentsDir())
	if reg == nil {
		// A broken built-in is a bug in this binary; run without agents.
		s.log.Printf("agents: %v", err)
		reg, _ = agent.Load("")
		if reg == nil {
			return
		}
	}
	var errs []string
	if err != nil {
		s.log.Printf("agents: %v", err)
		errs = strings.Split(err.Error(), "\n")
	}
	s.mu.Lock()
	s.agents, s.agentErrs = reg, errs
	s.mu.Unlock()
}

// writeLaunchFiles writes an agent's generated files (hooks, settings,
// brief) under its runtime dir. A session must not start without them.
func writeLaunchFiles(rt string, files map[string][]byte) error {
	for name, data := range files {
		p := filepath.Join(rt, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) registry() *agent.Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agents
}

func (s *Server) agentList() proto.AgentListResult {
	reg := s.registry()
	res := proto.AgentListResult{Agents: []proto.AgentInfo{}}
	s.mu.Lock()
	res.Errors = append(res.Errors, s.agentErrs...)
	s.mu.Unlock()
	if reg == nil {
		return res
	}
	res.Agents = agent.Infos(reg)
	return res
}

// newUUID returns a random version 4 UUID, the agent session id the
// server pre-assigns (docs/SPEC.md §8.5).
func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// runtimeDir is a session's scratch dir for generated files. It sits in
// the short run dir, which is private (0700).
func (s *Server) runtimeDir(id string) string {
	return filepath.Join(s.opts.Paths.RunDir, "s", id)
}

// readyThreadTemp makes a thread's own temp folder ready for a launch:
// it and its folder in the shared /tmp private folders of this user
// (made, not links, no one else's). The folder is the thread's to write,
// and kept across restarts: a link it left there (a direct child, or one
// of its claude-<uid> folder's) could lead the agent, or tm reading
// files in it, elsewhere, so then the folder is made anew (renewed).
func readyThreadTemp(dir string) (renewed bool, err error) {
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := fsx.EnsurePrivateDir(d); err != nil {
			return false, err
		}
	}
	if !holdsLink(dir, 2) {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, fsx.EnsurePrivateDir(dir)
}

// holdsLink reports whether folder dir holds a link (a junction or other
// reparse point on Windows) depth levels down at most, or can't be read.
func holdsLink(dir string, depth int) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return true
		}
		if e.IsDir() && depth > 1 && holdsLink(filepath.Join(dir, e.Name()), depth-1) {
			return true
		}
	}
	return false
}

// realPath resolves symlinks, because agents check permission rules
// against real paths (docs/SPEC.md §5.2).
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// ownWorktree reports whether dir is a worktree tm created: one level
// below a project's folder in ~/.terminatr/worktrees.
func (s *Server) ownWorktree(dir string) bool {
	root := realPath(filepath.Join(s.opts.Paths.Home, "worktrees"))
	rel, err := filepath.Rel(root, realPath(dir))
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) == 2 && parts[0] != ".." && parts[0] != "." && parts[1] != ".."
}

// accessFor is the role's file access policy (docs/SPEC.md §5.2), with
// the settings in lc; tree is a thread's checkout (threadTree), nil for
// none. A session outside a project gets no grants and no restrictions.
func (s *Server) accessFor(role, slug string, tree *gitx.Tree, lc launchConfig) (agent.Access, error) {
	if slug == "" || role == proto.RoleShell {
		return agent.Access{}, nil
	}
	dir, err := project.Dir(slug)
	if err != nil {
		return agent.Access{}, err
	}
	dir = realPath(dir)
	// The human's safety settings (docs/SPEC.md §11.2).
	cfg := filepath.Join(realPath(s.opts.Paths.Home), "config.toml")
	switch role {
	case proto.RoleCoordinator:
		wt := realPath(filepath.Join(s.opts.Paths.Home, "worktrees", slug))
		a := agent.Access{Read: []string{dir, wt}, NoWriteFiles: []string{cfg}}
		// Its folder, but for what tm alone writes there (§5.2).
		// The folders are made first, so that each sandbox has a real
		// path to close.
		owned, ownedFiles := (&project.Project{Slug: slug, Dir: dir}).Owned()
		for _, d := range owned {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return agent.Access{}, err
			}
		}
		a.NoWrite = owned
		a.NoWriteFiles = append(a.NoWriteFiles, ownedFiles...)
		a.Commands = coordinatorCommands(lc.safety(slug))
		return a, nil
	case proto.RoleThread:
		// The folder of tm's pinned git runs' git dirs (gitx.RunsDir),
		// made first for the sandbox to have a real path to close.
		runs := gitx.RunsDir(realPath(s.opts.Paths.Home))
		if err := os.MkdirAll(runs, 0o700); err != nil {
			return agent.Access{}, err
		}
		a := agent.Access{Read: []string{dir}, NoWrite: []string{dir, runs}, NoWriteFiles: []string{cfg}}
		// No tree: a thread to come (access.show), whose git dirs aren't
		// known yet, or one whose folder isn't its recorded checkout.
		if tree != nil {
			g := threadGitAccess(tree.CommonDir, tree.GitDir)
			a.Write, a.WriteFiles = g.Write, g.WriteFiles
			a.NoWrite = append(a.NoWrite, g.NoWrite...)
			a.NoWriteFiles = append(a.NoWriteFiles, g.NoWriteFiles...)
		}
		return a, nil
	}
	return agent.Access{}, fmt.Errorf("unknown role %q", role)
}

// threadGitAccess is what a thread's git may write in its checkout's
// git dirs (docs/SPEC.md §5.2), common the main repo's and own the
// checkout's own (the same for the main checkout). A worktree's commits
// and pushes go to the common dir, outside the cwd the sandbox allows:
// its objects, refs and reflogs, and packed-refs when a ref goes. HEAD
// and index are in the worktree's own git dir, granted by name for
// sandboxes that carve git dirs out of any broader grant (Codex's).
// What git runs or reads as configuration (hooks, config, info, the
// own git dir's config.worktree and commondir) and the other worktrees'
// git dirs are never written: the human's git and the server use them
// outside any sandbox. Nor, from a linked worktree, the main checkout's
// HEAD and index. They are named NoWrite so
// that a broader grant (the main checkout as cwd, a user's own rule)
// doesn't open them either. A worktree added later is in no grant.
func threadGitAccess(common, own string) agent.Access {
	dirs, files := gitConfig(common)
	a := agent.Access{
		Write:        []string{filepath.Join(common, "objects"), filepath.Join(common, "refs"), filepath.Join(common, "logs")},
		WriteFiles:   []string{filepath.Join(common, "packed-refs"), filepath.Join(common, "packed-refs.lock")},
		NoWrite:      dirs,
		NoWriteFiles: append(files, filepath.Join(common, "config.lock")),
	}
	wts := filepath.Join(common, "worktrees")
	if own != common {
		// Its own git dir is written, but for the files that would make
		// git read configuration or another common dir from it.
		a.Write = append(a.Write, own)
		a.NoWriteFiles = append(a.NoWriteFiles, ownConfig(own)...)
		// The main checkout's HEAD and index are the human's, not a
		// linked worktree's git.
		a.NoWriteFiles = append(a.NoWriteFiles, filepath.Join(common, "HEAD"), filepath.Join(common, "index"))
	}
	if es, err := os.ReadDir(wts); err == nil {
		for _, e := range es {
			if p := filepath.Join(wts, e.Name()); e.IsDir() && p != own {
				a.NoWrite = append(a.NoWrite, p)
			}
		}
	}
	return a
}

// ownConfig are what git reads as configuration in a linked worktree's
// own git dir own: config.worktree, and commondir, which says where the
// rest of it is.
func ownConfig(own string) []string {
	return []string{filepath.Join(own, "config.worktree"), filepath.Join(own, "commondir")}
}

// gitConfig are what git runs or reads as configuration in git dir
// common: its hooks and info folders, its submodules' git dirs (modules,
// with their own config and hooks), its config file, and the main
// checkout's own config.worktree and commondir (ownConfig), which a
// main checkout's cwd covers.
func gitConfig(common string) (dirs, files []string) {
	return []string{filepath.Join(common, "hooks"), filepath.Join(common, "info"), filepath.Join(common, "modules")},
		append([]string{filepath.Join(common, "config")}, ownConfig(common)...)
}

// mergeCommands are the commands the opt-in coordinator_merges setting
// allows the coordinator without a prompt: tm's own merge, which checks
// CI, mergeability and the project's merge setting itself. The raw
// host commands (gh pr merge, az repos pr update) stay out: Claude
// Code's auto mode blocks a merge it can't match to a rule exactly.
var mergeCommands = []string{"tm thread merge"}

// coordinatorCommands is the commands a coordinator may run unprompted:
// the merge commands when the project opted in and the coordinator is
// the one who merges. Threads never get them.
func coordinatorCommands(set config.Safety) []string {
	if set.CoordinatorMerges && set.Merge == config.MergeCoordinator {
		return mergeCommands
	}
	return nil
}

// contextFor renders the context a role gets back after a clear or a
// compaction (docs/SPEC.md §7.8): the role's rules plus `tm context`
// for a coordinator, the rules plus the brief, task, PR
// and latest follow-up for a thread, nothing for a session outside a
// project. Each stays within its size budget, the rules aside.
func contextFor(role, slug, threadID, brief, tickerState string) func() ([]byte, error) {
	return func() ([]byte, error) {
		switch role {
		case proto.RoleCoordinator:
			rules, _ := skill.ForProject("coordinator", version.Version, slug)
			p, err := project.Open(slug)
			if err != nil {
				return []byte(rules), nil
			}
			secs, err := thread.Context(p, ticker.Seen(tickerState, slug))
			if err != nil {
				return []byte(rules), nil
			}
			return []byte(rules + "\n\n" + coordinatorEssentials(secs)), nil
		case proto.RoleThread:
			rules, _ := skill.ForProject("thread", version.Version, slug)
			if p, err := project.Open(slug); err == nil && threadID != "" {
				pr := ticker.PRs(tickerState, slug)[threadID]
				line := pr.Summary()
				if line != "" && pr.URL != "" {
					line += " " + pr.URL
				}
				if ctx := thread.ResetContext(p, threadID, line); ctx != "" {
					return []byte(rules + "\n\n" + ctx), nil
				}
			}
			if brief != "" {
				rules += "\n\nYour brief: " + brief + "\nRead it again, then continue with your task's unchecked steps.\n"
			}
			return []byte(rules), nil
		}
		return nil, nil
	}
}

// coordinatorBudget bounds the `tm context` a coordinator gets back
// after a clear or a compaction, in bytes.
const coordinatorBudget = 16 << 10

// coordinatorEssentials is `tm context` within coordinatorBudget, with a
// last line saying when to run it again. It is all a fresh coordinator
// needs (T109): its rules say not to run `tm context` again at once.
func coordinatorEssentials(secs []project.Section) string {
	tail := "\nThat is `tm context` as of this conversation's start; run it (or tm inbox list, tm thread list) again when you need the current state.\n"
	return thread.Clip(project.RenderContext(secs), coordinatorBudget-len(tail)) + tail
}

// contextOf is contextFor the session's current record, so that a
// session adopted as a thread gets a thread's context (§9, Adopt).
func (s *Server) contextOf(id string) func() ([]byte, error) {
	return func() ([]byte, error) {
		s.mu.Lock()
		r := s.records[id]
		s.mu.Unlock()
		return contextFor(r.Role, r.Project, r.Thread, r.Brief, ticker.StatePath(s.opts.Paths.Sessions))()
	}
}

// modContextLine is what the SessionStart hook brings instead of the
// context when the session has the mod: the mod gives it as a context
// block of the conversation (T85), so a copy here would only be printed
// twice.
const modContextLine = "terminatr: your role's rules and your task's state are in this conversation's `terminatr` context block."

// hookContextOf is what the session's SessionStart hook prints, given
// the hook's payload: the full context, or with the mod only a line
// saying where it is (none for a session outside a project).
//
// A resume keeps the conversation's context block as the transcript
// recorded it: Claude computes it once per conversation, and only /clear
// and compaction read it again. So a mod session resumed under another
// build than the one that rendered its block (an upgrade and a server
// restart, T226) gets the full context here, saying it replaces the
// block's.
func (s *Server) hookContextOf(id string, mod bool) func(payload map[string]any) ([]byte, error) {
	full := s.contextOf(id)
	return func(payload map[string]any) ([]byte, error) {
		b, err := full()
		if err != nil || len(b) == 0 || !mod {
			return b, err
		}
		if src, _ := payload["source"].(string); src == "resume" {
			if old, stale := s.staleContext(id); stale {
				return append([]byte(replacedContext(old)), b...), nil
			}
		}
		return []byte(modContextLine), nil
	}
}

// staleContext reports whether session id's conversation carries a
// context block another build rendered, and that build ("" when not
// known: a record from before context_build).
func (s *Server) staleContext(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.records[id].ContextBuild
	return b, b != version.BuildID()
}

// servedContext records that session id's conversation now carries the
// context this build renders (GET /v1/context).
func (s *Server) servedContext(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok || r.ContextBuild == version.BuildID() {
		return
	}
	r.ContextBuild = version.BuildID()
	s.records[id] = r
	if err := s.saveLocked(""); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
}

// replacedContext heads the full context a resumed session gets when its
// context block is another build's.
func replacedContext(old string) string {
	was := "an older tm"
	if v, _, _ := strings.Cut(old, "+"); v != "" {
		was = "tm " + v
	}
	return "terminatr: this conversation's `terminatr` context block was written by " + was +
		"; tm is now " + version.Version + ". These are your current rules and state, in its place:\n\n"
}

// paneScheme is the scheme whose pane palette a console last drew the
// panes with (emu.PaneDark, emu.PaneLight), "" for none or in safe mode:
// a theme mod's palette doesn't reach a new agent then; s.mu held.
func (s *Server) paneScheme() string {
	switch {
	case s.noMods:
		return ""
	case s.colors.Known&emu.PaneDark != 0:
		return "dark"
	case s.colors.Known&emu.PaneLight != 0:
		return "light"
	}
	return ""
}

// launchTheme is [ui] theme's colours for the scheme a console draws the
// panes' palette in (paneScheme), for an agent to follow; nil for none
// (no palette, safe mode, the built-in theme) or a theme that doesn't
// load, which leaves the agent's ANSI theme (§8.6).
func (s *Server) launchTheme(scheme string) *theme.Agent {
	if scheme == "" || s.noMods {
		return nil
	}
	cfg, _ := config.Load()
	return s.launchThemeOf(scheme, cfg)
}

// launchThemeOf is launchTheme with config.toml as cfg (nil: it didn't
// load).
func (s *Server) launchThemeOf(scheme string, cfg *config.Config) *theme.Agent {
	if scheme == "" || s.noMods {
		return nil
	}
	if cfg == nil || followsTerminal(cfg) {
		return nil
	}
	reg := s.modRegistry()
	if reg == nil {
		return nil
	}
	tt, err := theme.Load(reg, cfg.Theme)
	if err != nil {
		s.log.Printf("theme for agents: %v", err)
		return nil
	}
	return theme.AgentColors(tt, scheme == "dark")
}

// followsTerminal says [ui] theme is the built-in one (cfg not nil),
// which draws with the terminal's own colours: an agent that keeps a
// live theme then follows them too, Claude with its ANSI theme (§8.6).
func followsTerminal(cfg *config.Config) bool {
	return cfg.Theme == "" || cfg.Theme == mod.BundledTheme || cfg.Theme == "default"
}

// schemeName is the terminal's scheme for an agent's theme: its
// background's, light or dark, when a console reported one, as the
// console's own theme follows it, else the scheme a console last
// reported (mode 2031); "" for neither yet. A terminal's 2031 scheme may
// be the system's appearance, not its background's: a dark terminal
// that reports light would give Claude a light theme over it (T520).
func (s *Server) schemeName() string {
	if s.colors.Known&emu.KnownBg != 0 {
		if s.colors.Bg.Dark() {
			return "dark"
		}
		return "light"
	}
	switch s.scheme {
	case emu.SchemeDark:
		return "dark"
	case emu.SchemeLight:
		return "light"
	}
	return ""
}

// writeLiveTheme writes a's own theme file for the tm theme and scheme
// now (§8.6), when a keeps one, outside safe mode; false when it isn't
// in place, so the launch doesn't select it; s.mu held.
func (s *Server) writeLiveTheme(a agent.Agent) bool {
	if _, ok := a.(agent.LiveThemer); !ok || s.noMods {
		return false
	}
	cfg, _ := config.Load()
	if !s.writeThemeFile(a, s.launchThemeOf(s.paneScheme(), cfg), s.schemeName(), cfg != nil && followsTerminal(cfg)) {
		return false
	}
	s.markLiveThemed(a)
	return true
}

// writeThemeFile writes a's own theme file with the colours th (the
// pane scheme's: launchTheme), the terminal's scheme and whether the tm
// theme follows the terminal (followsTerminal), when a keeps one,
// outside safe mode; false when it isn't in place. It reads no server
// state: s.mu needn't be held.
func (s *Server) writeThemeFile(a agent.Agent, th *theme.Agent, scheme string, terminal bool) bool {
	lt, ok := a.(agent.LiveThemer)
	if !ok || s.noMods {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	if err := lt.WriteTheme(home, th, scheme, terminal); err != nil {
		s.log.Printf("agent %s: theme: %v", a.Name(), err)
		return false
	}
	return true
}

// markLiveThemed has themeChangedLocked keep a's live theme; s.mu held.
func (s *Server) markLiveThemed(a agent.Agent) {
	if s.liveThemed == nil {
		s.liveThemed = map[string]bool{}
	}
	s.liveThemed[a.Name()] = true
}

// themeChangedLocked rewrites the live theme of every agent this server
// started with one, after a console reported new colours (another
// theme's pane palette, or none) or another scheme: its running
// sessions redraw with it, without a restart (§8.6); s.mu held.
func (s *Server) themeChangedLocked() {
	if s.agents == nil {
		return
	}
	for name := range s.liveThemed {
		if a, ok := s.agents.Get(name); ok {
			s.writeLiveTheme(a)
		}
	}
}

// agentLaunch is everything needed to (re)start one agent session.
type agentLaunch struct {
	rec    SessionRecord
	resume bool
	kick   string
	cols   uint16
	rows   uint16
}

// launchView is the server state a launch reads, taken under s.mu so
// that the slow part of the launch runs without it.
type launchView struct {
	agents     *agent.Registry
	paneScheme string
	schemeName string
}

// launchViewLocked takes the server state a launch reads; s.mu held.
func (s *Server) launchViewLocked() launchView {
	return launchView{agents: s.agents, paneScheme: s.paneScheme(), schemeName: s.schemeName()}
}

// preparedLaunch is an agent session ready to start: its files are
// written and its mod listens; commitLaunchLocked starts it.
type preparedLaunch struct {
	l         agentLaunch
	a         agent.Agent
	rt        string
	mod       *http.Server // the mod's server, nil for none
	modSock   string
	model     string
	liveTheme bool
	argv      []string
	env       []string
	kickoff   bool
	agentSID  string
	home      string
	warnings  []string // what the launch tells the human who started it
	access    proto.AccessSet
}

// launchAgent prepares an agent session without s.mu and starts it
// under s.mu, when ok (under s.mu) still holds; ok nil always does.
// s.mu not held: the git, config and version reads of a launch, its
// files and the agent's own launch steps take long enough (seconds for
// the first --version) to stall every hook and request behind them.
func (s *Server) launchAgent(l agentLaunch, ok func() *proto.Error) (*session.Session, []string, *proto.Error) {
	s.mu.Lock()
	v := s.launchViewLocked()
	s.mu.Unlock()
	p, perr := s.prepareLaunch(l, v)
	if perr != nil {
		return nil, nil, perr
	}
	s.mu.Lock()
	if ok != nil {
		if perr := ok(); perr != nil {
			s.mu.Unlock()
			p.abandon()
			return nil, nil, perr
		}
	}
	sess, perr := s.commitLaunchLocked(p)
	s.mu.Unlock()
	if perr == nil {
		s.recordLaunch(p.l.rec, p.access)
	}
	return sess, p.warnings, perr
}

// abandon undoes a prepared launch that doesn't start.
func (p *preparedLaunch) abandon() {
	if p.mod != nil {
		p.mod.Close()
	}
	os.RemoveAll(p.rt)
}

// prepareLaunch builds the argv, environment and files for an agent
// session from v, the server state when the launch began; s.mu not held.
func (s *Server) prepareLaunch(l agentLaunch, v launchView) (*preparedLaunch, *proto.Error) {
	r := l.rec
	reg := v.agents
	if reg == nil {
		return nil, proto.Errorf(proto.ErrRefused, "no agents loaded")
	}
	a, ok := reg.Get(r.Agent)
	if !ok {
		return nil, proto.Errorf(proto.ErrBadParams, "unknown agent %q (tm agent list)", r.Agent)
	}
	if l.resume && r.AgentSessionID == "" {
		return nil, proto.Errorf(proto.ErrRefused, "session %s: no agent session id to resume", r.ID)
	}
	// config.toml once for the launch: its access, mod, model and theme.
	lc := loadLaunchConfig()
	// Its access as rules (access.go), which the launch's settings come
	// from and the thread's launch record keeps.
	tree := threadTree(r)
	acc, err := s.resolveAccess(accessInput{role: r.Role, slug: r.Project, cwd: r.Cwd, tree: tree, agent: r.Agent, goos: caps.OS,
		thread: r.Thread, yolo: r.Yolo, noEscapes: r.NoSandboxEscapes}, lc)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	acc.Thread, acc.Session = r.Thread, r.ID
	yolo, noEscapes := launchFlags(acc)
	if r.Role == proto.RoleCoordinator {
		if p, err := project.Open(r.Project); err == nil {
			if err := p.RefreshRoleFile(); err != nil {
				s.log.Printf("session %s: role file: %v", r.ID, err)
			}
		}
	}
	var repo worktree.Dirs // empty outside a git repo
	if tree != nil {
		repo = worktree.Dirs{GitDir: tree.GitDir, CommonDir: tree.CommonDir, RepoRoot: repoRoot(tree.CommonDir)}
	} else if d, err := worktree.GitDirs(r.Cwd); err == nil && r.Role != proto.RoleThread {
		repo = worktree.Dirs{GitDir: realPath(d.GitDir), CommonDir: realPath(d.CommonDir), RepoRoot: realPath(d.RepoRoot)}
	}
	// No session of this id runs (a new id, a resume, a relaunch after
	// the old one exited and its mod closed): its runtime dir is free.
	p := &preparedLaunch{l: l, a: a, rt: s.runtimeDir(r.ID)}
	os.RemoveAll(p.rt)
	if err := os.MkdirAll(p.rt, 0o700); err != nil {
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	if s.modsFor(a, r.ID, lc) {
		// Without its channel the mod can't report state: no mod then.
		if p.mod, p.modSock, err = s.openMod(r.ID, p.rt); err != nil {
			s.log.Printf("session %s: no mod: %v", r.ID, err)
		}
	}
	// A thread's secret environment (secretenv.go): got now, put only in
	// the session's environment below.
	var secrets map[string]string
	if r.Role == proto.RoleThread && r.Project != "" {
		var fails []string
		secrets, fails = secretEnv(lc.cfg, r.Project, s.baseEnv())
		for _, f := range fails {
			s.log.Printf("session %s: %s", r.ID, f)
		}
	}
	if r.ModelPending {
		// The coordinator's setting changed while its agent couldn't
		// switch live: this start applies it (session.model).
		r.Model, r.ModelNext, r.ModelPending = r.ModelNext, "", false
	}
	spec := agent.LaunchSpec{
		Role: agent.Role(r.Role), SessionID: r.ID, AgentSID: r.AgentSessionID,
		Cwd: r.Cwd, RepoRoot: repo.RepoRoot, GitDir: repo.GitDir, RuntimeDir: p.rt, BriefPath: r.Brief, Kickoff: l.kick, Resume: l.resume,
		Yolo: yolo, NoSandboxEscapes: noEscapes, Model: r.Model, TMBin: s.opts.Bin, Socket: s.opts.Paths.Socket, Access: launchAccess(acc),
		RemoteControl: r.RemoteControl, RemoteName: remoteName(r),
		Mods: p.modSock != "", PaneScheme: v.paneScheme,
	}
	if spec.TempDir = launchTemp(acc); spec.TempDir != "" {
		// The thread's own temp folder (T587), kept across its restarts
		// like its worktree: its scratchpads stay where it left them. Its
		// folder in the shared /tmp must be this user's own first: no
		// one else may have made it or put a link there.
		renewed, err := readyThreadTemp(spec.TempDir)
		if err != nil {
			p.abandon()
			return nil, proto.Errorf(proto.ErrInternal, "thread temp folder: %v", err)
		}
		if renewed {
			s.log.Printf("session %s: thread temp folder %s held a link: made anew", r.ID, spec.TempDir)
		}
	}
	if r.Role == proto.RoleThread && r.Project != "" {
		// The human's sandbox rules, read here at every launch: never
		// from the session's parameters, which a coordinator writes.
		// What doesn't apply on this platform stays in config.toml and is
		// skipped.
		var guides []config.Guide
		var skipped []string
		spec.Sandbox, guides, skipped = launchSandbox(acc)
		for _, n := range skipped {
			s.log.Printf("session %s: %s", r.ID, n)
		}
		p.warnings = append(skipped, tempWarnings(acc)...)
		for _, n := range p.warnings[len(skipped):] {
			s.log.Printf("session %s: %s", r.ID, n)
		}
		if spec.BriefPath, err = guideBrief(p.rt, r.Brief, guides, spec.Sandbox); err != nil {
			s.log.Printf("session %s: sandbox guidance: %v", r.ID, err)
		}
	}
	if len(secrets) > 0 {
		spec.SecretEnv = slices.Sorted(maps.Keys(secrets))
		spec.EnvHide = envHide(s.baseEnv(), spec.SecretEnv)
	}
	if spec.Model == "" {
		spec.Model = launchModel(a, r.Project, r.Role == proto.RoleCoordinator, lc.cfg)
	}
	spec.Theme = s.launchThemeOf(spec.PaneScheme, lc.cfg)
	spec.LiveTheme = s.writeThemeFile(a, spec.Theme, v.schemeName, lc.cfg != nil && followsTerminal(lc.cfg))
	launch, err := a.Launch(spec)
	if err != nil {
		p.abandon()
		return nil, proto.Errorf(proto.ErrRefused, "%v", err)
	}
	for _, w := range launch.Warnings {
		s.log.Printf("session %s: %s", r.ID, w)
	}
	if err := writeLaunchFiles(p.rt, launch.Files); err != nil {
		p.abandon()
		return nil, proto.Errorf(proto.ErrInternal, "%v", err)
	}
	set := s.terminatrEnv(r)
	for k, v := range secrets {
		if _, taken := set[k]; !taken {
			set[k] = v
		}
	}
	if spec.Mods {
		set[envModSocket] = p.modSock
		if !lc.band() {
			set[envBand] = "off"
		}
		if !lc.pane() {
			set[envPane] = "off"
		}
	}
	for _, kv := range launch.Env {
		k, v, _ := strings.Cut(kv, "=")
		set[k] = v
	}
	base := agent.FilterEnv(withBinFirst(s.baseEnv(), s.opts.Bin), launch.Unset)
	p.env = sessionEnv(base, set)
	r.Argv = launch.Argv
	// The agent's own id is known ahead only when the launch gives it
	// (Claude's --session-id, a resume). Codex picks its thread id itself
	// and reports it with the first hook: until then there is none, so a
	// prompt isn't queued for a thread that doesn't exist (T105).
	p.agentSID = r.AgentSessionID
	if !slices.Contains(launch.Argv, p.agentSID) {
		p.agentSID = ""
	}
	p.home, _ = os.UserHomeDir()
	// A thread never waits on a folder-trust screen for the worktree tm
	// made for it (docs/SPEC.md §8.6): an agent that can, trusts it first.
	if t, ok := a.(agent.Truster); ok && r.Role == proto.RoleThread && p.home != "" && s.ownWorktree(r.Cwd) {
		if err := t.TrustDir(p.home, r.Cwd); err != nil {
			s.log.Printf("session %s: trust %s: %v", r.ID, r.Cwd, err)
		}
	}
	p.l.rec = r
	p.access = acc
	p.model, p.liveTheme = spec.Model, spec.LiveTheme
	p.argv, p.kickoff = launch.Argv, launch.Kickoff
	return p, nil
}

// commitLaunchLocked starts a prepared agent session and records it;
// s.mu held. The pty's spawn stays under s.mu: a hook the agent fires
// at once then waits for the session to be known instead of finding
// none.
func (s *Server) commitLaunchLocked(p *preparedLaunch) (*session.Session, *proto.Error) {
	r, a := p.l.rec, p.a
	mod := p.modSock != ""
	sess, err := session.Start(session.Config{
		ID: r.ID, Role: r.Role, Project: r.Project, Thread: r.Thread, Argv: p.argv, Cwd: r.Cwd, Env: p.env,
		Cols: p.l.cols, Rows: p.l.rows, Created: r.Created,
		Xtversion: "terminatr " + version.Version,
		Scheme:    s.scheme,
		Colors:    s.colors,
		Logf:      s.log.Printf,
		OnExit:    s.sessionExited,
		// Set before Start: the first session.list must show it.
		RemoteControl: r.RemoteControl, RemoteHeld: r.RemoteHeld,
		Agent: &session.AgentConfig{
			Agent: a, AgentSID: p.agentSID, Kickoff: p.kickoff, Home: p.home,
			Context:    s.hookContextOf(r.ID, mod),
			Guard:      s.hookGuardOf(r.ID, mod),
			OnChange:   s.agentChanged,
			OnUsage:    s.tailUsage,
			OnRefused:  s.modelRefused,
			PromptHold: envDuration(envPromptHold), OnPromptResolved: s.promptResolved,
			ModSocket:     p.modSock,
			PromptsEscape: r.Role == proto.RoleThread && agent.ManifestOf(a) != nil && agent.ManifestOf(a).Guard.PromptsEscape,
		},
	})
	if err != nil {
		p.abandon()
		return nil, proto.Errorf(proto.ErrRefused, "%v", err)
	}
	if p.mod != nil {
		s.setModLocked(r.ID, p.mod)
	} else {
		s.closeModLocked(r.ID)
	}
	if p.liveTheme {
		s.markLiveThemed(a)
	}
	s.sessions[r.ID] = sess
	s.records[r.ID] = r
	if s.launched == nil {
		s.launched = map[string]string{}
	}
	s.launched[r.ID] = p.model
	sess.SetModel(session.ModelState{Model: p.model})
	if err := s.saveLocked(""); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
	how := "started"
	if p.l.resume {
		how = "resumed " + r.AgentSessionID
	}
	s.log.Printf("session %s: %s agent %s pid %d %q in %s", r.ID, how, r.Agent, sess.PID(), p.argv, r.Cwd)
	// The version is information for the drift notes and the log, never
	// a gate (docs/SPEC.md §8.8). Cached per binary; off the launch path.
	go func(env []string) {
		v, err := s.versionOf(a, env)
		if err != nil {
			return
		}
		sess.SetAgentVersion(v)
		s.modelsVersion(a, v, env)
		if m := agent.ManifestOf(a); m != nil && m.Identify.LastTested != "" && !agent.VersionAtLeast(m.Identify.LastTested, v) {
			s.log.Printf("session %s: %s %s is newer than the last tested %s: supported; drift is logged as \"agent drift\"", r.ID, r.Agent, v, m.Identify.LastTested)
		}
	}(s.baseEnv())
	return sess, nil
}

// terminatrEnv is the session environment of docs/SPEC.md §3.4.
func (s *Server) terminatrEnv(r SessionRecord) map[string]string {
	set := map[string]string{
		"TERM":                 "xterm-256color",
		"COLORTERM":            "truecolor",
		"TERM_PROGRAM":         "terminatr",
		"TERM_PROGRAM_VERSION": version.Version,
		"TERMINATR":            "1",
		"TERMINATR_SESSION":    r.ID,
		"TERMINATR_SOCKET":     s.opts.Paths.Socket,
		"TERMINATR_HOME":       s.opts.Paths.Home,
		"TERMINATR_BIN":        s.opts.Bin,
		"TERMINATR_ROLE":       r.Role,
	}
	if r.Project != "" {
		set["TERMINATR_PROJECT"] = r.Project
	}
	if r.Thread != "" {
		set["TERMINATR_THREAD"] = r.Thread
	}
	// The server's own TERMINATR_* don't reach sessions (terminalEnv),
	// but where tm hook logs what it failed to deliver (tests) does.
	for _, kv := range s.baseEnv() {
		if v, ok := strings.CutPrefix(kv, hookLogEnv+"="); ok && v != "" {
			set[hookLogEnv] = v
		}
	}
	return set
}

// hookLogEnv names the file tm hook logs undelivered events to
// (internal/cli).
const hookLogEnv = "TERMINATR_HOOK_LOG"

func (s *Server) baseEnv() []string {
	if s.opts.Env != nil {
		return s.opts.Env
	}
	return os.Environ()
}

// promptResolved logs and journals a queued prompt the session resolved
// after it was held for PromptHold while the agent was idle (§8.6), and
// rings the bell for one dropped, or waiting for the channel's token.
func (s *Server) promptResolved(sess *session.Session, res session.PromptResolution) {
	held := res.Held.Round(time.Second)
	if res.Via == "stale" {
		// Nothing was lost: what it said was handled meanwhile.
		s.log.Printf("session %s: queued prompt (queued %s) went stale and was not delivered", sess.ID(), res.Queued.Format(time.DateTime))
		return
	}
	msg := fmt.Sprintf("session %s: queued prompt (queued %s) held %s while idle, %s: ", sess.ID(), res.Queued.Format(time.DateTime), held, res.Why)
	switch res.Via {
	case "sent":
		msg += "sent through the agent's channel (delivery not confirmed)"
	case "waiting":
		// Not lost: it stays queued (session.HeldToken).
		msg += "not sent: the agent's channel needs its messaging token, which no hook has reported yet " +
			"(was its SessionStart hook lost?); it waits for the agent's next hook, or an empty prompt box"
	default:
		msg += "dropped"
		if res.Err != nil {
			msg += fmt.Sprintf(" (channel: %v)", res.Err)
		}
	}
	s.mu.Lock()
	r, ok := s.records[sess.ID()]
	s.mu.Unlock()
	if ok && r.Project != "" {
		if p, err := project.Open(r.Project); err == nil {
			if err := p.Journal(caller.Caller{Kind: caller.Ticker}, "prompt."+res.Via, sess.ID(), fmt.Sprintf("held %s: %s", held, res.Why)); err != nil {
				s.log.Printf("session %s: journal: %v", sess.ID(), err)
			}
		}
	}
	if res.Via == "sent" {
		s.log.Print(msg)
		return
	}
	s.alert(msg)
}

// agentChanged records the agent's latest session id (Claude rotates it
// on /clear) for resume, and logs state changes.
func (s *Server) agentChanged(sess *session.Session) {
	st, ok := sess.AgentState()
	if !ok {
		return
	}
	s.mu.Lock()
	r, ok := s.records[sess.ID()]
	s.mu.Unlock()
	if ok && r.Role == proto.RoleThread {
		s.syncThread(r, st)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tick != nil && ok && r.Project != "" {
		s.tick.Kick()
	}
	s.watch.wake()
	s.modEv.wake()
	s.log.Printf("session %s: %s %s/%s (%s)", sess.ID(), st.Agent, st.State, st.Reason, st.Sources)
	if blocked := st.State == agent.StateBlocked; blocked != s.blocked[sess.ID()] {
		if blocked {
			s.blocked[sess.ID()] = true
			s.alerts.Add(1)
		} else {
			delete(s.blocked, sess.ID())
		}
	}
	r, ok = s.records[sess.ID()]
	// An agent found in a shell is the shell's business, unless the
	// shell was adopted as a thread: then resume needs its ids.
	if !ok || (st.Observed && r.Role != proto.RoleThread) {
		return
	}
	prompted := r.Prompted || worked(st)
	// The agent's own word on remote control wins: a resume reconnects.
	remote := r.RemoteControl
	if _, restarting := s.relaunch[sess.ID()]; st.RemoteKnown && !restarting {
		remote = st.RemoteControl
	}
	if (st.AgentSID == "" || r.AgentSessionID == st.AgentSID) && prompted == r.Prompted && remote == r.RemoteControl {
		return
	}
	r.RemoteControl = remote
	if st.AgentSID != "" {
		r.AgentSessionID = st.AgentSID
	}
	r.Prompted = prompted
	s.records[sess.ID()] = r
	if err := s.saveLocked(""); err != nil {
		s.log.Printf("sessions.json: %v", err)
	}
}

// worked reports whether the agent's state shows it worked on a prompt;
// a kickoff it hasn't started on doesn't count.
func worked(st session.AgentState) bool {
	return (st.State == agent.StateWorking && st.Reason != agent.ReasonKickoff) || st.State == agent.StateBlocked
}

// resume relaunches the previous server's agent sessions with their
// latest agent session ids (docs/SPEC.md §3.6), and says what became of
// each.
// Each launch runs without s.mu, taking it only to start the session:
// hooks of those already resumed, and requests, are answered meanwhile.
func (s *Server) resume(recs []SessionRecord) (outs []restartOutcome) {
	for _, r := range recs {
		o := s.resumeOne(r)
		s.mu.Lock()
		if o.how == "lost" {
			s.lost = append(s.lost, r.ID)
		} else {
			s.resumed = append(s.resumed, r.ID)
		}
		s.mu.Unlock()
		outs = append(outs, o)
	}
	return outs
}

// resumeOne relaunches one recorded agent session under its id:
// resumed, or fresh when it was never prompted (nothing to resume), or
// lost when its cwd is gone or the launch fails. s.mu not held.
func (s *Server) resumeOne(r SessionRecord) restartOutcome {
	kept := r
	r.CleanExit = false
	if _, err := os.Stat(r.Cwd); err != nil {
		s.log.Printf("session %s: not resumed: %v", r.ID, err)
		return restartOutcome{rec: r, how: "lost"}
	}
	cols, rows := r.Cols, r.Rows
	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	l := agentLaunch{rec: r, resume: true, cols: cols, rows: rows}
	how := "resumed"
	if !r.Prompted {
		how = "fresh"
		// Nothing to resume: the agent saved no conversation yet.
		l.resume, l.kick, l.rec.AgentSessionID = false, r.Kickoff, newUUID()
	}
	if _, _, perr := s.launchAgent(l, func() *proto.Error {
		if _, taken := s.records[r.ID]; taken {
			return proto.Errorf(proto.ErrRefused, "session %s: id taken during the resume", r.ID)
		}
		if perr := s.notStoppingLocked(); perr != nil {
			// Not lost: dormant, the next server resumes it.
			s.dormant[r.ID] = kept
			return perr
		}
		return nil
	}); perr != nil {
		s.log.Printf("session %s: not resumed: %v", r.ID, perr)
		return restartOutcome{rec: r, how: "lost"}
	}
	return restartOutcome{rec: r, how: how}
}

func (s *Server) hookEvent(p proto.HookEventParams) proto.HookEventResult {
	// tm hook says nothing of a dropped event, and the agent carries on
	// without it: its state, its context and its prompt token (T491: a
	// coordinator's only SessionStart lost, a nudge then refused).
	sess, perr := s.session(p.Session)
	if perr != nil {
		s.log.Printf("hook %s for %s dropped: %s", p.Event, p.Session, perr.Message)
		return proto.HookEventResult{}
	}
	if a := sess.Agent(); a == nil || a.Name() != p.Agent {
		runs := "no agent"
		if a != nil {
			runs = a.Name()
		}
		s.log.Printf("hook %s for %s dropped: from agent %q, the session runs %s", p.Event, p.Session, p.Agent, runs)
		return proto.HookEventResult{}
	}
	if p.Token != "" {
		sess.SetPromptToken(p.Token)
	}
	res, err := sess.Hook(p.Event, p.Payload)
	if err != nil && !errors.Is(err, session.ErrNoAgent) {
		s.log.Printf("session %s: hook %s: %v", p.Session, p.Event, err)
	}
	return proto.HookEventResult{Stdout: string(res.Stdout)}
}

func (s *Server) prompt(p proto.SessionPromptParams) (any, *proto.Error) {
	return s.promptWith(p, session.PromptOptions{})
}

func (s *Server) promptWith(p proto.SessionPromptParams, o session.PromptOptions) (any, *proto.Error) {
	sess, perr := s.session(p.ID)
	if perr != nil {
		return nil, perr
	}
	if strings.TrimSpace(p.Text) == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "empty prompt")
	}
	var via, why string
	var err error
	switch p.Mode {
	case "":
		via, err = sess.PromptWith(p.Text, o)
	case proto.PromptNow, proto.PromptInterrupt:
		via, why, err = sess.PromptNow(p.Text, p.Mode == proto.PromptInterrupt)
	default:
		return nil, proto.Errorf(proto.ErrBadParams, "mode %q is not %s or %s", p.Mode, proto.PromptNow, proto.PromptInterrupt)
	}
	if errors.Is(err, session.ErrNoAgent) {
		return nil, proto.Errorf(proto.ErrRefused, "session %s runs no agent; use tm session keys", p.ID)
	}
	if err != nil {
		return nil, sessionError(p.ID, err)
	}
	s.watch.wake()
	return proto.SessionPromptResult{Via: via, Why: why}, nil
}

// maxWait bounds session.wait.
const maxWait = 10 * time.Minute

func (s *Server) wait(p proto.SessionWaitParams) (any, *proto.Error) {
	sess, perr := s.session(p.ID)
	if perr != nil {
		return nil, perr
	}
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > maxWait {
		timeout = maxWait
	}
	deadline := time.After(timeout)
	first := true
	for {
		ch := sess.StateChanged()
		st, ok := sess.AgentState()
		if !ok {
			return nil, proto.Errorf(proto.ErrRefused, "session %s runs no agent", p.ID)
		}
		res := proto.SessionWaitResult{State: string(st.State), Reason: st.Reason}
		if len(p.States) == 0 && !first {
			return res, nil
		}
		for _, want := range p.States {
			if want == string(st.State) {
				return res, nil
			}
		}
		if st.State == agent.StateExited {
			return res, nil
		}
		first = false
		select {
		case <-ch:
		case <-deadline:
			res.TimedOut = true
			return res, nil
		}
	}
}

// ExplainResult is the result of agent.explain.
type ExplainResult struct {
	Session proto.SessionInfo `json:"session"`
	agent.Explanation
}

func (s *Server) explain(id string) (any, *proto.Error) {
	sess, perr := s.session(id)
	if perr != nil {
		return nil, perr
	}
	e, ok := sess.Explain()
	if !ok {
		return nil, proto.Errorf(proto.ErrRefused, "session %s runs no agent", id)
	}
	return ExplainResult{Session: s.info(sess), Explanation: e}, nil
}
