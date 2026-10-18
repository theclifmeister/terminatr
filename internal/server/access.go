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
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// accessInput is what a session's rules are worked out for: its role,
// project and folder, its agent, the platform, and the yolo and escapes
// of its record (tm thread start reads them from config.toml).
type accessInput struct {
	role, slug, cwd, agent, goos string
	yolo, noEscapes              bool
}

// resolveAccess is the rules of a session of in, with config.toml as lc.
// It fails only where accessFor does.
func (s *Server) resolveAccess(in accessInput, lc launchConfig) (proto.AccessSet, error) {
	set := proto.AccessSet{Project: in.slug, Role: in.role, Agent: in.agent, OS: in.goos, Rules: []proto.AccessRule{}}
	a, err := s.accessFor(in.role, in.slug, in.cwd, lc)
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
	if in.role == proto.RoleThread {
		rules, why := sandboxRules(lc, in.slug, in.goos)
		add(rules...)
		set.SandboxError = why
	}
	add(onOff(proto.AccessPrompts, proto.AccessYolo, in.yolo, settingSource(lc, in.slug, "yolo"), false),
		onOff(proto.AccessOutside, proto.AccessEscapes, !in.noEscapes, settingSource(lc, in.slug, "sandbox_escapes"), false))
	set.Rules = append(set.Rules, guardRules(lc, in.slug, in.role)...)
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
	note := s.pathNotes(in)
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
	case in.role == proto.RoleThread:
		// A thread to come: its worktree and git dirs aren't known yet.
		out = append(out, proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessWorktree, Value: "<worktree>", Source: proto.AccessFromTM, Note: "the thread's worktree"},
			proto.AccessRule{Where: proto.AccessInside, Kind: proto.AccessWrite, Value: "<repo>/.git: objects, refs, logs, packed-refs, its own git dir", Source: proto.AccessFromTM, Note: "the repo's git dir; never hooks, info, config or other worktrees'"})
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

// pathNotes says what a path of in's access is: the project folder,
// config.toml, a part of the project folder only tm writes, or the
// repo's git dir.
func (s *Server) pathNotes(in accessInput) func(string) string {
	var dir, common string
	if d, err := project.Dir(in.slug); err == nil {
		dir = realPath(d)
	}
	cfg := filepath.Join(realPath(s.opts.Paths.Home), "config.toml")
	if d, err := worktree.GitDirs(in.cwd); in.cwd != "" && err == nil {
		common = realPath(d.CommonDir)
	}
	return func(p string) string {
		switch {
		case p == dir:
			return "project folder"
		case p == cfg:
			return "config.toml: your settings"
		case dir != "" && strings.HasPrefix(p, dir+string(filepath.Separator)):
			return "project folder: only tm writes it"
		case common != "" && (p == common || strings.HasPrefix(p, common+string(filepath.Separator))):
			return "the repo's git dir"
		}
		return ""
	}
}

// sandboxRules are project slug's sandbox lists and presets for its
// threads on platform goos (config.ThreadSandbox): each entry a rule,
// a preset with the rules it opens, a rule that doesn't apply there
// marked skipped. A config.toml that doesn't load, or an entry that
// fails its check, gives no rules and why.
func sandboxRules(lc launchConfig, slug, goos string) ([]proto.AccessRule, string) {
	err := lc.err
	var t config.ThreadSandbox
	if err == nil {
		t, err = lc.cfg.ThreadSandbox(slug)
	}
	if err != nil {
		return nil, fmt.Sprintf("project %s: sandbox lists and presets dropped, the thread starts without them (fix or remove the entry: tm settings, Sandbox tab): %v", slug, err)
	}
	own := lc.cfg.SandboxOf(slug)
	ownPresets := lc.cfg.PresetsOf(slug)
	from := func(set bool) string {
		if set {
			return slug
		}
		return proto.AccessFromAll
	}
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
	var out []proto.AccessRule
	list := func(key, where, kind string, vals []string) {
		_, set := own[key]
		for _, v := range vals {
			out = append(out, proto.AccessRule{Where: where, Kind: kind, Value: v, Source: from(set),
				OS: config.SandboxSettings[key].OS, Agents: agents(key), Skipped: skip(key)})
		}
	}
	list(config.SandboxExcluded, proto.AccessOutside, proto.AccessExcluded, t.ExcludedCommands)
	list(config.SandboxAllow, proto.AccessPrompts, proto.AccessAllow, t.Allow)
	for _, k := range t.Presets {
		_, set := ownPresets[k]
		base := proto.AccessRule{Source: from(set), OS: config.SandboxSettings[k].OS, Skipped: skip(k), Note: presetName(k) + " preset"}
		r := base
		r.Where, r.Kind, r.Value, r.Note = proto.AccessInside, proto.AccessPreset, k, presetName(k)
		out = append(out, r)
		opens := func(where, kind, value string) {
			r := base
			r.Where, r.Kind, r.Value = where, kind, value
			out = append(out, r)
		}
		switch k {
		case config.SandboxBrowser:
			opens(proto.AccessBrief, proto.AccessGuide, "browser-testing")
		case config.SandboxDownloads:
			for _, h := range config.BrowserDomains {
				opens(proto.AccessInside, proto.AccessHost, h)
			}
			opens(proto.AccessBrief, proto.AccessGuide, "browser-downloads")
		case config.SandboxDevServers:
			opens(proto.AccessInside, proto.AccessLocalPorts, "on")
		}
	}
	return out, ""
}

// presetName is a preset's key as the UI and the log say it.
func presetName(key string) string { return strings.ReplaceAll(key, "_", " ") }

// guardRules are the guard's settings for a session of role in project
// slug, read live: on or off, its mode, the rules off, the allowances
// and the floor. A config.toml that doesn't load leaves the defaults
// (guard on), as the guard does.
func guardRules(lc launchConfig, slug, role string) []proto.AccessRule {
	set := lc.safety(slug)
	src := func(key string) string { return settingSource(lc, slug, key) }
	out := []proto.AccessRule{onOff(proto.AccessGuard, proto.AccessGuardOn, set.Guard, src("guard"), true)}
	if !set.Guard {
		return out
	}
	out = append(out, proto.AccessRule{Where: proto.AccessGuard, Kind: proto.AccessGuardMode, Value: set.GuardMode, Source: src("guard_mode"), Live: true})
	for _, r := range set.GuardOff {
		out = append(out, proto.AccessRule{Where: proto.AccessGuard, Kind: proto.AccessGuardRuleOff, Value: r, Source: src("guard_off"), Live: true})
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
		out = append(out, proto.AccessRule{Where: proto.AccessGuard, Kind: proto.AccessGuardAllow, Value: a.Text(), Source: from, Note: note, Live: true})
	}
	floor := "reading tm's own files, printing a secret variable tm gives threads, force-pushing to a protected branch"
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
// runs.
func launched(r proto.AccessRule) bool {
	return r.Where != proto.AccessAgent && r.Skipped == ""
}

// launchAccess is the file access policy the launch gives the agent:
// set's file rules, in their order.
func launchAccess(set proto.AccessSet) agent.Access {
	var a agent.Access
	for _, r := range set.Rules {
		if !launched(r) {
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
		case proto.AccessCommand:
			a.Commands = append(a.Commands, r.Value)
		}
	}
	return a
}

// launchSandbox is a thread's sandbox lists from set, the presets on
// where it runs (for their guidance in the brief), and a note for each
// setting that doesn't apply there, or the one why its lists were
// dropped, for the log and the launch's warnings.
func launchSandbox(set proto.AccessSet) (lists agent.SandboxLists, presets, skipped []string) {
	if set.SandboxError != "" {
		return lists, nil, []string{set.SandboxError}
	}
	said := map[string]bool{}
	for _, r := range set.Rules {
		if r.Where == proto.AccessAgent {
			continue
		}
		if r.Skipped != "" {
			what := ""
			switch r.Kind {
			case proto.AccessExcluded:
				what = "excluded commands"
			case proto.AccessAllow:
				what = "allow rules"
			case proto.AccessPreset:
				what = "preset " + presetName(r.Value)
			}
			if what != "" && !said[what] {
				said[what] = true
				skipped = append(skipped, fmt.Sprintf("sandbox %s is set but %s: skipped", what, r.Skipped))
			}
			continue
		}
		switch r.Kind {
		case proto.AccessExcluded:
			lists.Excluded = append(lists.Excluded, r.Value)
		case proto.AccessAllow:
			lists.Allow = append(lists.Allow, r.Value)
		case proto.AccessPreset:
			presets = append(presets, r.Value)
		case proto.AccessHost:
			lists.Domains = append(lists.Domains, r.Value)
		case proto.AccessLocalPorts:
			lists.LocalBinding = true
		}
	}
	return lists, presets, skipped
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
