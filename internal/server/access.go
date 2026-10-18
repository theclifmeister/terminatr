package server

// A session's access as one list of rules (T574 §3.1, phase 1): the
// file access of its role (accessFor), a thread's sandbox lists and
// presets (config sandbox), yolo and escapes, the guard and the secret
// environment, each rule with its source. A launch takes its settings
// from these rules (launchAccess, launchSandbox), so what the effective
// access view lists is what the agent got; each thread's launch keeps
// them in threads/<id>/access.json.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/gitx"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// accessInput is what a session's rules are worked out for: its role,
// project and folder, its agent, the platform, and the yolo and escapes
// of its record (tm thread start reads them from config.toml).
type accessInput struct {
	role, slug, cwd, agent, goos string
	// thread is a thread's id, which names its own temp folder; "" for
	// a thread to come.
	thread string
	// tree is a thread's checkout by its record (threadTree), whose git
	// dirs it is granted; nil for none.
	tree            *gitx.Tree
	yolo, noEscapes bool
}

// resolveAccess is the rules of a session of in, with config.toml as lc.
// It fails only where accessFor does.
func (s *Server) resolveAccess(in accessInput, lc launchConfig) (proto.AccessSet, error) {
	set := proto.AccessSet{Project: in.slug, Role: in.role, Agent: in.agent, OS: in.goos, Rules: []proto.AccessRule{}}
	a, err := s.accessFor(in.role, in.slug, in.tree, lc)
	if err != nil {
		return set, err
	}
	add := func(r ...proto.AccessRule) { set.Rules = append(set.Rules, r...) }
	add(s.fileRules(in, a, lc)...)
	if in.slug == "" || in.role == proto.RoleShell {
		// A session's own yolo, outside any project's settings.
		add(onOff(proto.AccessPrompts, proto.AccessYolo, in.yolo, proto.AccessFromDefault, false),
			onOff(proto.AccessOutside, proto.AccessEscapes, !in.noEscapes, proto.AccessFromDefault, false))
		return set, nil
	}
	from := len(set.Rules)
	if in.role == proto.RoleThread {
		rules, why := sandboxRules(lc, in.slug, in.goos)
		add(rules...)
		set.SandboxError = why
		if in.agent == "codex" && in.tree != nil {
			// Its pushes reach the repo's remotes (remotehosts.go).
			add(remoteHostRules(in.tree.CommonDir)...)
		}
		if in.agent == "codex" && (in.goos == config.OSWindows || in.goos == config.OSLinux) {
			// Its network keeps the open form it had before
			// (codex.ThreadNetwork): on Windows Codex's proxy needs its
			// elevated sandbox, on Linux its sandbox can't allow tm's
			// socket with the proxy on. Its host rules aren't used; one
			// rule says so.
			value, why := "open", codexOpenNote(in.goos)
			for i := from; i < len(set.Rules); i++ {
				if r := &set.Rules[i]; r.Kind == proto.AccessHost && r.Skipped == "" {
					r.Skipped = why
				}
			}
			add(proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessNetwork, Value: value, Source: proto.AccessFromTM, Note: why, Agents: []string{"codex"}})
		}
	}
	add(onOff(proto.AccessPrompts, proto.AccessYolo, in.yolo, settingSource(lc, in.slug, "yolo"), false),
		onOff(proto.AccessOutside, proto.AccessEscapes, !in.noEscapes, settingSource(lc, in.slug, "sandbox_escapes"), false))
	if in.yolo && in.role == proto.RoleThread {
		// Yolo is no sandbox (T574 case 4): what opens or leaves it isn't
		// used, and shows so.
		for i := from; i < len(set.Rules); i++ {
			if r := &set.Rules[i]; r.Kind != proto.AccessYolo && r.Kind != proto.AccessAllow && r.Skipped == "" {
				r.Skipped = proto.AccessYoloNoSandbox
			}
		}
	}
	set.Rules = append(set.Rules, guardRules(lc, in.slug, in.role, in.yolo)...)
	if in.role == proto.RoleThread {
		add(secretRules(lc, in.slug)...)
	}
	return set, nil
}

// onOff is an on/off rule.
func onOff(where, kind string, on bool, source string, live bool) proto.AccessRule {
	v := "off"
	if on {
		v = "on"
	}
	return proto.AccessRule{Where: where, Kind: kind, Value: v, Source: source, Live: live}
}

// settingSource is where project slug's setting key comes from: its own
// table, all projects', or tm's default.
func settingSource(lc launchConfig, slug, key string) string {
	if lc.err != nil {
		return proto.AccessFromDefault
	}
	switch {
	case slices.Contains(lc.cfg.Own(slug), key):
		return slug
	case slices.Contains(lc.cfg.OwnDefaults(), key):
		return proto.AccessFromAll
	}
	return proto.AccessFromDefault
}

// fileRules are access a's rules (accessFor's, all tm's), in the order
// launchAccess gives them back, with the session's folder and tm's
// socket before and after them, which the agent's launch takes from
// elsewhere. Notes say what a path is.
func (s *Server) fileRules(in accessInput, a agent.Access, lc launchConfig) []proto.AccessRule {
	if in.slug == "" || in.role == proto.RoleShell {
		return nil
	}
	// The repo whose git dir a thread's commits write in: accessFor
	// grants it on the same condition (threadGitAccess).
	var repo, common string
	if in.tree != nil {
		repo, common = repoRoot(in.tree.CommonDir), in.tree.CommonDir
	}
	note := s.pathNotes(in.slug, repo, common)
	var out []proto.AccessRule
	rule := func(where, kind string) func(string) {
		return func(v string) {
			out = append(out, proto.AccessRule{Where: where, Kind: kind, Value: v, Source: proto.AccessFromTM, Note: note(v)})
		}
	}
	switch {
	case in.cwd != "":
		what := "the session's folder"
		if in.role == proto.RoleThread {
			what = "the thread's worktree"
		}
		out = append(out, proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessWorktree, Value: in.cwd, Source: proto.AccessFromTM, Note: what})
		if in.role == proto.RoleThread && repo != "" {
			out = append(out, gitDirRule(repo))
		}
	case in.role == proto.RoleThread:
		// A thread to come: its worktree and repo aren't known yet.
		out = append(out, proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessWorktree, Value: "<worktree>", Source: proto.AccessFromTM, Note: "the thread's worktree"},
			gitDirRule("<its repo>"))
	}
	if in.role == proto.RoleThread {
		out = append(out, s.tempRules(in)...)
	}
	for _, l := range []struct {
		kind  string
		paths []string
	}{
		{proto.AccessRead, a.Read}, {proto.AccessWrite, a.Write}, {proto.AccessWriteFile, a.WriteFiles},
		{proto.AccessNoWrite, a.NoWrite}, {proto.AccessNoWriteFile, a.NoWriteFiles},
	} {
		for _, p := range l.paths {
			rule(proto.AccessInside, l.kind)(p)
		}
	}
	for _, c := range a.Commands {
		out = append(out, proto.AccessRule{Where: proto.AccessPrompts, Kind: proto.AccessCommand, Value: c, Source: settingSource(lc, in.slug, "coordinator_merges"), Note: "coordinator_merges"})
	}
	if s.opts.Paths.Socket != "" {
		out = append(out, proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessSocket, Value: s.opts.Paths.Socket, Source: proto.AccessFromTM, Note: "tm's socket; never the human socket"})
	}
	return out
}

// tempRules are a thread's own temp folder (home.ThreadTempDir), which
// its launch makes the agent's TMPDIR, and the folders it never reads
// nor writes (T587): the other threads' (home.ThreadTempRoot, its own
// open within) and the temp folders the user's other agent sessions
// share (agent.SharedTemps: Claude's /tmp/claude-<uid>, with their
// scratchpads and task output). The shared ones are skipped where the
// agent would keep its TMPDIR there all the same (agent.OwnTempFits),
// or where one holds the thread's own folders (a tm home in a temp
// folder: tests, scratch servers): the deny would take them too.
func (s *Server) tempRules(in accessInput) []proto.AccessRule {
	id := in.thread
	if id == "" {
		id = "t-0000" // a thread to come: as long a path as its own
	}
	own, err := threadTemp(in.slug, id)
	if err != nil {
		return nil
	}
	shown := own
	if in.thread == "" {
		shown = "<thread temp folder>"
	}
	out := []proto.AccessRule{
		{Where: proto.AccessInside, Kind: proto.AccessTemp, Value: shown, Source: proto.AccessFromTM,
			Note: "the thread's own temp folder: its TMPDIR, its scratchpads"},
		{Where: proto.AccessInside, Kind: proto.AccessNoRead, Value: home.ThreadTempRoot(), Source: proto.AccessFromTM,
			Note: "the other threads' temp folders; its own is open"},
	}
	reg := s.registry()
	fits := agent.OwnTempFits(reg, in.agent, own)
	h, _ := os.UserHomeDir()
	for _, d := range agent.SharedTemps(reg, h, os.Getenv) {
		// By the kernel's spelling (/private/tmp), which sandboxes match,
		// even before the folder exists: a later session of the user
		// makes it while the thread runs.
		d = filepath.Join(realPath(filepath.Dir(d)), filepath.Base(d))
		r := proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessNoRead, Value: d, Source: proto.AccessFromTM,
			Note: "other sessions' temp folders: the user's Claude sessions' scratchpads and task output"}
		switch {
		case !fits:
			r.Skipped = notDenied + "the thread's temp folder (" + own + ") is too long for " + in.agent + "'s sandbox, which keeps its TMPDIR in this shared folder instead, so the thread can read the user's other sessions here"
		case within(d, own) || filepath.IsAbs(in.cwd) && within(d, in.cwd):
			r.Skipped = "it holds the thread's own folders"
		}
		out = append(out, r)
	}
	return out
}

// notDenied starts the reason a shared temp folder's deny is left out
// where the thread can then read it: said plainly, in the effective
// view and as a launch warning (tempWarnings).
const notDenied = "NOT DENIED: "

// tempWarnings are the launch warnings of set's shared temp folders the
// thread can read after all.
func tempWarnings(set proto.AccessSet) []string {
	var out []string
	for _, r := range set.Rules {
		if r.Kind == proto.AccessNoRead && strings.HasPrefix(r.Skipped, notDenied) {
			out = append(out, "access: "+r.Value+" is "+strings.ToLower(strings.TrimSuffix(notDenied, ": "))+": "+strings.TrimPrefix(r.Skipped, notDenied))
		}
	}
	return out
}

// threadTemp is thread id of project slug's own temp folder.
func threadTemp(slug, id string) (string, error) { return home.ThreadTempDir(slug, id) }

// threadTempRoot holds every thread's own temp folder.
func threadTempRoot() string { return home.ThreadTempRoot() }

// within reports whether path is dir or below it, by either's real path.
func within(dir, path string) bool {
	for _, p := range []string{path, realPath(path)} {
		if _, ok := fsx.Within(dir, p); ok {
			return true
		}
	}
	return false
}

// gitDirRule is the git-dir grant (T551) as one rule naming repo, the
// repo whose git dir a thread's commits and pushes write in; shown only:
// the launch takes the paths it opens and closes from their own rules.
func gitDirRule(repo string) proto.AccessRule {
	return proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessGitDir, Value: repo, Source: proto.AccessFromTM,
		Note: "git-dir grant: objects, refs, logs, packed-refs and its own git dir; never hooks, info, modules, config files or other worktrees'"}
}

// pathNotes says what a path of project slug's access is: the project
// folder, config.toml, a part of the project folder only tm writes, or
// a path of the git-dir grant of repo, whose git dir is common.
func (s *Server) pathNotes(slug, repo, common string) func(string) string {
	var dir string
	if d, err := project.Dir(slug); err == nil {
		dir = realPath(d)
	}
	cfg := filepath.Join(realPath(s.opts.Paths.Home), "config.toml")
	return func(p string) string {
		switch {
		case p == dir:
			return "project folder"
		case p == cfg:
			return proto.AccessSettingsNote
		case dir != "" && strings.HasPrefix(p, dir+string(filepath.Separator)):
			return "project folder: only tm writes it"
		case common != "" && (p == common || strings.HasPrefix(p, common+string(filepath.Separator))):
			return "git-dir grant: " + repo
		}
		return ""
	}
}

// sandboxRules are project slug's sandbox rules for its threads on
// platform goos (config.ThreadSandbox): each entry a rule, all
// projects' first, those the project drops or that fail their check
// marked and left out, a rule that doesn't apply there marked skipped;
// local ports when a table sets them. A config.toml that doesn't load
// gives no rules and why.
func sandboxRules(lc launchConfig, slug, goos string) ([]proto.AccessRule, string) {
	if lc.err != nil {
		return nil, fmt.Sprintf("project %s: sandbox rules left out, the thread starts without them (fix config.toml: tm settings, Access tab): %v", slug, lc.err)
	}
	t := lc.cfg.ThreadSandbox(slug)
	skip := func(key string) string {
		if config.SandboxApplies(key, goos) {
			return ""
		}
		return "doesn't apply on " + config.OSName(goos)
	}
	// The agents of a key Codex threads don't get (config.SandboxSettings).
	agents := func(key string) []string {
		if config.SandboxSettings[key].Codex {
			return nil
		}
		return []string{"claude"}
	}
	kinds := map[string][2]string{
		config.SandboxHosts:    {proto.AccessInside, proto.AccessHost},
		config.SandboxGuides:   {proto.AccessBrief, proto.AccessGuide},
		config.SandboxExcluded: {proto.AccessOutside, proto.AccessExcluded},
		config.SandboxAllow:    {proto.AccessPrompts, proto.AccessAllow},
	}
	var out []proto.AccessRule
	for _, r := range t.Rules {
		k := kinds[r.Key]
		rule := proto.AccessRule{Where: k[0], Kind: k[1], Value: r.Value, Source: proto.AccessFromAll,
			OS: config.SandboxSettings[r.Key].OS, Agents: agents(r.Key), Skipped: skip(r.Key), Dropped: r.Dropped || r.Replaced, Refused: r.Refused}
		switch {
		case r.Own:
			rule.Source = slug
		case r.Dropped:
			rule.Note = "dropped by " + slug
		case r.Replaced:
			rule.Note = slug + "'s own list replaces all projects'"
		}
		out = append(out, rule)
	}
	if t.LocalPortsSet {
		src := proto.AccessFromAll
		if t.LocalPortsOwn {
			src = slug
		}
		r := onOff(proto.AccessInside, proto.AccessLocalPorts, t.LocalPorts, src, false)
		r.OS, r.Skipped = config.SandboxSettings[config.SandboxLocalPorts].OS, skip(config.SandboxLocalPorts)
		out = append(out, r)
	}
	return out, ""
}

// guardRules are the guard's settings for a session of role in project
// slug, read live: on or off, its mode, the rules off, the allowances
// and the floor. Under yolo the guard keeps its floor only, so the
// others show as not used. A config.toml that doesn't load leaves the
// defaults (guard on), as the guard does.
func guardRules(lc launchConfig, slug, role string, yolo bool) []proto.AccessRule {
	set := lc.safety(slug)
	src := func(key string) string { return settingSource(lc, slug, key) }
	on := onOff(proto.AccessGuard, proto.AccessGuardOn, set.Guard, src("guard"), true)
	skip := ""
	if yolo && set.Guard {
		on.Note, skip = proto.AccessYoloFloor, proto.AccessYoloFloor
	}
	out := []proto.AccessRule{on}
	if !set.Guard {
		return out
	}
	out = append(out, proto.AccessRule{Where: proto.AccessGuard, Kind: proto.AccessGuardMode, Value: set.GuardMode, Source: src("guard_mode"), Live: true, Skipped: skip})
	for _, r := range set.GuardOff {
		out = append(out, proto.AccessRule{Where: proto.AccessGuard, Kind: proto.AccessGuardRuleOff, Value: r, Source: src("guard_off"), Live: true, Skipped: skip})
	}
	// A project without guard_allow of its own takes all projects'
	// whole (config.Safety marks All only beside its own).
	own := lc.err == nil && slices.Contains(lc.cfg.Own(slug), "guard_allow")
	for _, a := range set.GuardAllow {
		from := slug
		if a.All || !own {
			from = proto.AccessFromAll
		}
		note := a.Rule
		if a.Exact {
			note += ", exact"
		}
		out = append(out, proto.AccessRule{Where: proto.AccessGuard, Kind: proto.AccessGuardAllow, Value: a.Text(), Source: from, Note: note, Live: true, Skipped: skip})
	}
	floor := "reading tm's own files, writing your settings"
	if role == proto.RoleThread {
		floor += ", writing the project folder"
	} else {
		floor += ", writing the parts of the project folder only tm writes"
	}
	floor += ", printing a secret variable tm gives threads, force-pushing to a protected branch"
	if role == proto.RoleThread && set.Merge == config.MergeCoordinator {
		floor += ", a thread merging"
	}
	return append(out, proto.AccessRule{Where: proto.AccessGuard, Kind: proto.AccessGuardFloor, Value: floor, Source: proto.AccessFromTM, Live: true})
}

// secretRules are the names of project slug's secret variables, each
// with its source (never a value).
func secretRules(lc launchConfig, slug string) []proto.AccessRule {
	if lc.err != nil {
		return nil
	}
	env, err := lc.cfg.SecretEnv(slug)
	if err != nil {
		return nil
	}
	own := lc.cfg.EnvOf(slug)
	var out []proto.AccessRule
	for _, name := range slices.Sorted(maps.Keys(env)) {
		from := proto.AccessFromAll
		if _, ok := own[name]; ok {
			from = slug
		}
		out = append(out, proto.AccessRule{Where: proto.AccessEnv, Kind: proto.AccessSecret, Value: name, Source: from, Note: env[name].From})
	}
	return out
}

// agentRules are the rules agent a reads from its own settings files
// (an agent.OwnRuler's), for the session's folder cwd: shown, never
// launched.
func agentRules(a agent.Agent, home, cwd string) []proto.AccessRule {
	or, ok := a.(agent.OwnRuler)
	if !ok || home == "" {
		return nil
	}
	var out []proto.AccessRule
	for _, r := range or.OwnRules(home, cwd) {
		out = append(out, proto.AccessRule{Where: proto.AccessAgent, Kind: r.Kind, Value: r.Value, Source: r.File, Agents: []string{a.Name()}})
	}
	return out
}

// launched reports whether rule r goes into the agent's launch: not
// one shown only (the agent's own files), nor one left out where it
// runs, dropped or refused.
func launched(r proto.AccessRule) bool {
	return r.Where != proto.AccessAgent && r.Given()
}

// launchAccess is the file access policy the launch gives the agent:
// set's file rules, in their order.
func launchAccess(set proto.AccessSet) agent.Access {
	var a agent.Access
	for _, r := range set.Rules {
		// A path rule launches only with an absolute path: never a
		// placeholder of a thread to come (<worktree>, <its repo>).
		if !launched(r) || r.Kind != proto.AccessCommand && !filepath.IsAbs(r.Value) {
			continue
		}
		switch r.Kind {
		case proto.AccessRead:
			a.Read = append(a.Read, r.Value)
		case proto.AccessWrite:
			a.Write = append(a.Write, r.Value)
		case proto.AccessWriteFile:
			a.WriteFiles = append(a.WriteFiles, r.Value)
		case proto.AccessNoWrite:
			a.NoWrite = append(a.NoWrite, r.Value)
		case proto.AccessNoWriteFile:
			a.NoWriteFiles = append(a.NoWriteFiles, r.Value)
		case proto.AccessNoRead:
			a.NoRead = append(a.NoRead, r.Value)
		case proto.AccessTemp:
			a.OwnTemp = r.Value
		case proto.AccessCommand:
			a.Commands = append(a.Commands, r.Value)
		}
	}
	return a
}

// launchTemp is the thread's own temp folder set's rules give it, ""
// for none.
func launchTemp(set proto.AccessSet) string {
	for _, r := range set.Rules {
		if r.Kind == proto.AccessTemp && launched(r) && filepath.IsAbs(r.Value) {
			return r.Value
		}
	}
	return ""
}

// launchSandbox is a thread's sandbox lists from set, the guidance
// texts its brief gets where it runs, and a note for each kind of rule
// that doesn't apply there and each rule refused, or the one why its
// rules were left out, for the log and the launch's warnings.
func launchSandbox(set proto.AccessSet) (lists agent.SandboxLists, guides, skipped []string) {
	if set.SandboxError != "" {
		return lists, nil, []string{set.SandboxError}
	}
	said := map[string]bool{}
	for _, r := range set.Rules {
		if r.Where == proto.AccessAgent || r.Dropped {
			continue
		}
		if r.Refused != "" {
			skipped = append(skipped, fmt.Sprintf("sandbox %s %q refused, the thread starts without it: %s", r.Kind, r.Value, r.Refused))
			continue
		}
		if r.Skipped == proto.AccessYoloNoSandbox {
			continue
		}
		if r.Skipped != "" {
			if what := sandboxKindWords[r.Kind]; what != "" && !said[what] {
				said[what] = true
				skipped = append(skipped, fmt.Sprintf("sandbox %s skipped: %s", what, r.Skipped))
			}
			continue
		}
		switch r.Kind {
		case proto.AccessExcluded:
			lists.Excluded = append(lists.Excluded, r.Value)
		case proto.AccessAllow:
			lists.Allow = append(lists.Allow, r.Value)
		case proto.AccessGuide:
			guides = append(guides, r.Value)
		case proto.AccessHost:
			// Once each: Codex refuses a key given twice.
			if !slices.Contains(lists.Domains, r.Value) {
				lists.Domains = append(lists.Domains, r.Value)
			}
		case proto.AccessLocalPorts:
			lists.LocalBinding = r.Value == "on"
		}
	}
	return lists, guides, skipped
}

// sandboxKindWords name the sandbox rules of a kind in the log.
var sandboxKindWords = map[string]string{
	proto.AccessExcluded: "excluded commands", proto.AccessAllow: "allow rules", proto.AccessHost: "hosts",
	proto.AccessGuide: "guidance texts", proto.AccessLocalPorts: "local ports",
}

// launchFlags are the yolo and escapes the launch gives the agent.
func launchFlags(set proto.AccessSet) (yolo, noEscapes bool) {
	for _, r := range set.Rules {
		switch r.Kind {
		case proto.AccessYolo:
			yolo = r.Value == "on"
		case proto.AccessEscapes:
			noEscapes = r.Value == "off"
		}
	}
	return yolo, noEscapes
}
