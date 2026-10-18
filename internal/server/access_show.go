package server

// A thread's launch record and the effective access view (T574 §3.4,
// §4.4, §5): what a thread launched with, kept by tm alone in
// threads/<id>/access.json, and what it would get now, marked where a
// restart would change it.

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/guard"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
	"github.com/theclifmeister/terminatr/internal/plat/fsx"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// accessRecord is a thread's launch record's file name in its folder.
const accessRecord = "access.json"

// maxAccessRecord bounds reading one.
const maxAccessRecord = 4 << 20

// accessNow is the clock of launch records; a variable for tests.
var accessNow = time.Now

// recordLaunch keeps what thread session r launched with (set) in its
// thread's folder; nothing for another role. A failure is logged: the
// launch stands.
func (s *Server) recordLaunch(r SessionRecord, set proto.AccessSet) {
	if r.Role != proto.RoleThread || r.Project == "" || !thread.ValidID(r.Thread) {
		return
	}
	p, err := project.Open(r.Project)
	if err != nil {
		s.log.Printf("session %s: access record: %v", r.ID, err)
		return
	}
	set.Launched = accessNow().UTC().Truncate(time.Second)
	b, err := json.MarshalIndent(set, "", "  ")
	if err == nil {
		err = fsx.WriteAtomic(thread.Path(p, r.Thread, accessRecord), append(b, '\n'), 0o644)
	}
	if err != nil {
		s.log.Printf("session %s: access record: %v", r.ID, err)
	}
}

// readLaunchRecord is thread id's launch record, ok false when it has
// none (it launched before tm kept them, or never) or it doesn't read.
func readLaunchRecord(p *project.Project, id string) (proto.AccessSet, bool) {
	var set proto.AccessSet
	f, err := os.Open(thread.Path(p, id, accessRecord))
	if err != nil {
		return set, false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.Size() > maxAccessRecord {
		return set, false
	}
	if err := json.NewDecoder(f).Decode(&set); err != nil {
		return set, false
	}
	return set, true
}

// mayShowAccess refuses an agent asking for any access but its own
// thread's, and says whether the caller is the human, who may see every
// project's and thread's: on the human socket, and not placed by the pid
// walk in an agent's session (an unsandboxed command reaches that
// socket too), as gateHuman (mayCall).
func mayShowAccess(c callCtx, slug, id string) (human bool, perr *proto.Error) {
	who := c.who()
	if c.human && !who.IsAgent() {
		return true, nil
	}
	if who.Kind == caller.Thread && id != "" && who.Project == slug && who.Thread == id {
		return false, nil
	}
	return false, proto.Errorf(proto.ErrRefused, "an agent may see only its own thread's access (tm access show --thread <your thread>)")
}

// accessShow is access.show: a project's thread to come, or a thread.
func (s *Server) accessShow(p proto.AccessShowParams, c callCtx) (any, *proto.Error) {
	human, perr := mayShowAccess(c, p.Project, p.Thread)
	if perr != nil {
		return nil, perr
	}
	proj, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	lc := loadLaunchConfig()
	safety := lc.safety(p.Project)
	// The agent's own files are the human's to see: the user's settings
	// aren't a thread's business, and its worktree's it reads itself.
	home := ""
	if human {
		home, _ = os.UserHomeDir()
	}
	if p.Thread == "" {
		name := s.threadAgentOf(safety)
		set, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: p.Project, agent: name, goos: caps.OS,
			yolo: safety.Yolo, noEscapes: !safety.SandboxEscapes}, lc)
		if err != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
		}
		set.Rules = append(set.Rules, s.agentFileRules(name, home, "")...)
		return proto.AccessShowResult{AccessSet: set}, nil
	}
	rec, err := thread.Load(proj, p.Thread)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	// What a restart would launch it with: tm thread restart reads yolo
	// and escapes from config.toml again.
	now, err := s.resolveAccess(accessInput{role: proto.RoleThread, slug: p.Project, cwd: rec.Worktree, tree: recordTree(p.Project, rec), agent: rec.Agent, goos: caps.OS,
		yolo: safety.Yolo, noEscapes: !safety.SandboxEscapes}, lc)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	now.Thread = rec.ID
	files := s.agentFileRules(rec.Agent, home, rec.Worktree)
	launched, ok := readLaunchRecord(proj, rec.ID)
	if !ok {
		now.Rules = append(now.Rules, files...)
		return proto.AccessShowResult{AccessSet: now}, nil
	}
	res := proto.AccessShowResult{AccessSet: launched, Recorded: true}
	res.Rules, res.Changes = markChanges(launched.Rules, now.Rules)
	if launched.SandboxError != now.SandboxError {
		res.Changes++
	}
	res.Rules = append(res.Rules, files...)
	return res, nil
}

// markChanges is a thread's launched rules with the rules it would get
// now: one launched and gone marked removed, a new one added after
// them, and how many are marked. The guard's rules, read live, are
// today's, never marked.
func markChanges(launched, now []proto.AccessRule) ([]proto.AccessRule, int) {
	out := make([]proto.AccessRule, 0, len(launched)+len(now))
	n := 0
	for _, r := range launched {
		if r.Live {
			continue
		}
		if !slices.ContainsFunc(now, r.Same) {
			r.Change = proto.AccessRemoved
			n++
		}
		out = append(out, r)
	}
	for _, r := range now {
		switch {
		case r.Live:
			out = append(out, r)
		case !slices.ContainsFunc(launched, r.Same):
			r.Change = proto.AccessAdded
			n++
			out = append(out, r)
		}
	}
	return out, n
}

// threadAgentOf is the agent a project's new thread runs: its
// thread_agent, else the only agent installed, else "" (tm thread start
// asks then).
func (s *Server) threadAgentOf(safety config.Safety) string {
	if safety.ThreadAgent != "" {
		return safety.ThreadAgent
	}
	reg := s.registry()
	if reg == nil {
		return ""
	}
	if names := reg.Names(); len(names) == 1 {
		return names[0]
	}
	return ""
}

// agentFileRules are the rules agent name reads from its own files.
func (s *Server) agentFileRules(name, home, cwd string) []proto.AccessRule {
	reg := s.registry()
	if reg == nil || name == "" {
		return nil
	}
	a, ok := reg.Get(name)
	if !ok {
		return nil
	}
	return agentRules(a, home, cwd)
}

// accessExplain is access.explain: what a thread of the project (the
// thread named, else one to come) does with a command line.
func (s *Server) accessExplain(p proto.AccessExplainParams) (any, *proto.Error) {
	cmd := strings.TrimSpace(p.Command)
	if cmd == "" {
		return nil, proto.Errorf(proto.ErrBadParams, "no command to explain")
	}
	proj, err := project.Open(p.Project)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	lc := loadLaunchConfig()
	safety := lc.safety(p.Project)
	in := accessInput{role: proto.RoleThread, slug: p.Project, agent: s.threadAgentOf(safety), goos: caps.OS,
		yolo: safety.Yolo, noEscapes: !safety.SandboxEscapes}
	// The guard judges a command by the folder it runs in: the thread's
	// worktree, else the project's first repo.
	cwd := proj.Dir
	if len(proj.Repos) > 0 {
		cwd = proj.Repos[0]
	}
	if p.Thread != "" {
		rec, err := thread.Load(proj, p.Thread)
		if err != nil {
			return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
		}
		in.cwd, in.tree, in.agent, cwd = rec.Worktree, recordTree(p.Project, rec), rec.Agent, rec.Worktree
	}
	if reg := s.registry(); in.agent == "" && reg != nil && len(reg.Names()) > 0 {
		// No agent chosen yet: the guard judges by the first one's tools.
		in.agent = reg.Names()[0]
	}
	set, err := s.resolveAccess(in, lc)
	if err != nil {
		return nil, proto.Errorf(proto.ErrBadParams, "%v", err)
	}
	res := explainCommand(set, cmd)
	g := s.guardRulesFor(SessionRecord{Role: proto.RoleThread, Project: p.Project, Thread: p.Thread, Cwd: cwd, Agent: in.agent,
		Yolo: in.yolo, NoSandboxEscapes: in.noEscapes})
	res.Guard, res.GuardWhy = guardVerdict(g, cmd)
	return res, nil
}

// explainCommand is what set's sandbox and prompt rules do with cmd:
// outside the sandbox when an excluded command takes it (and the agent
// gets the list), asking first or without asking as that command says.
func explainCommand(set proto.AccessSet, cmd string) proto.AccessExplainResult {
	res := proto.AccessExplainResult{Command: cmd, Sandbox: proto.AccessInside, Prompt: proto.PromptByAgent}
	yolo, noEscapes := launchFlags(set)
	var allow []string
	for _, r := range set.Rules {
		if r.Kind == proto.AccessAllow && launched(r) {
			allow = append(allow, r.Value)
		}
	}
	var excluded *proto.AccessRule
	for i, r := range set.Rules {
		if r.Kind == proto.AccessExcluded && launched(r) && config.ExcludedMatch(r.Value, cmd) {
			excluded = &set.Rules[i]
			break
		}
	}
	if excluded != nil {
		res.Sandbox, res.SandboxFrom = proto.AccessOutside, excluded.Source
		res.SandboxWhy = fmt.Sprintf("excluded command %q", excluded.Value)
		if len(excluded.Agents) > 0 {
			res.Agents = "only " + strings.Join(excluded.Agents, ", ") + " threads get excluded commands; the others run it inside"
		}
	} else {
		res.SandboxWhy = "no excluded command takes it"
		if noEscapes {
			res.SandboxWhy += "; escapes are off, so it can't leave the sandbox"
		}
	}
	switch a := config.SandboxCovered(cmd, allow); {
	case excluded != nil && excluded.Note == proto.AccessWithoutAsking:
		res.Prompt, res.PromptFrom = proto.PromptNone, excluded.Source
		res.PromptWhy = fmt.Sprintf("excluded command %q runs without asking", excluded.Value)
	case excluded != nil:
		res.Prompt, res.PromptFrom = proto.PromptAsks, excluded.Source
		res.PromptWhy = fmt.Sprintf("excluded command %q asks first: the guard asks, whatever an allow rule or auto mode says", excluded.Value)
	case a != "":
		res.Prompt, res.PromptWhy = proto.PromptNone, fmt.Sprintf("allow rule %q", a)
		for _, r := range set.Rules {
			if r.Kind == proto.AccessAllow && r.Value == a {
				res.PromptFrom = r.Source
				break
			}
		}
	default:
		res.PromptWhy = "no tm rule covers it: the agent's mode and own rules decide"
	}
	if yolo {
		// No sandbox and no prompts (T574 case 4).
		res.Prompt, res.PromptWhy, res.PromptFrom = proto.PromptNone, "yolo is on: no permission prompts", ""
		if set.Role == proto.RoleThread {
			res.Sandbox, res.SandboxWhy, res.Agents = proto.AccessOutside, proto.AccessYoloNoSandbox, ""
		}
		for _, r := range set.Rules {
			if r.Kind == proto.AccessYolo {
				res.PromptFrom = r.Source
				if set.Role == proto.RoleThread {
					res.SandboxFrom = r.Source
				}
			}
		}
	}
	return res
}

// guardVerdict is the guard's verdict on command line cmd under rules
// g, by the agent's first shell tool, and why.
func guardVerdict(g guard.Rules, cmd string) (string, string) {
	// The guard off still asks about escapes, with no floor.
	if !g.On || g.Floor == nil {
		return "off", "the guard is off"
	}
	tool, field := "", ""
	for _, name := range slices.Sorted(maps.Keys(g.Tools)) {
		t := g.Tools[name]
		if t.Kind == guard.KindShell && t.Syntax == "" && len(t.Fields) > 0 {
			tool, field = name, t.Fields[0]
			break
		}
	}
	if tool == "" {
		return "allowed", "the agent has no shell tool the guard judges"
	}
	g.Resolve = realTarget
	d := g.Judge(tool, map[string]any{field: cmd})
	switch {
	case d == nil:
		return "allowed", "no rule refuses it"
	case d.Ask:
		return "asks", d.Message
	case d.Floor:
		return "refused", d.Rule + " (floor: no allowance lifts it): " + d.Summary
	}
	return "refused", d.Rule + ": " + d.Summary
}
