package proto

import (
	"strings"
	"time"
)

// A thread's access as one list of rules (T574 §3.1): what its sandbox,
// its prompts, the guard and its environment give it, each rule with
// where it comes from. Read-only: the rules are set in config.toml and
// the settings popups.

// MethodAccessShow is what a project's new thread would get, or what a
// thread launched with (its launch record, threads/<id>/access.json),
// marking what a restart would change. The human's, and a thread's for
// itself.
const MethodAccessShow = "access.show"

// MethodAccessExplain says what a project's thread would do with one
// command: inside or outside the sandbox, with a prompt or not, and the
// guard's verdict. The human's.
const MethodAccessExplain = "access.explain"

// Where a rule acts (AccessRule.Where).
const (
	AccessInside  = "inside"  // what sandboxed commands and file tools may do
	AccessOutside = "outside" // what runs outside the sandbox
	AccessBrief   = "brief"   // guidance added to the thread's brief
	AccessPrompts = "prompts" // what the agent may do without a prompt
	AccessGuard   = "guard"   // the guard, read live
	AccessEnv     = "env"     // the thread's environment
	AccessAgent   = "agent"   // the agent's own settings files, shown only
)

// Kinds of rule (AccessRule.Kind).
const (
	AccessWorktree     = "worktree"      // the cwd, writable
	AccessRead         = "read"          // a folder readable without a prompt
	AccessWrite        = "write"         // a folder writable besides the cwd
	AccessWriteFile    = "write-file"    // a file writable besides the cwd
	AccessNoWrite      = "no-write"      // a folder never written
	AccessNoWriteFile  = "no-write-file" // a file never written
	AccessSocket       = "socket"        // tm's socket, reachable from the sandbox
	AccessCommand      = "command"       // a command prefix run without a prompt (the coordinator's merge)
	AccessPreset       = "preset"        // a sandbox preset's switch (config key)
	AccessLocalPorts   = "local-ports"   // sandboxed commands may listen on and reach localhost
	AccessHost         = "host"          // a host sandboxed commands may reach
	AccessGuide        = "guide"         // guidance in the brief
	AccessExcluded     = "excluded"      // a command run outside the sandbox
	AccessAllow        = "allow"         // a permission rule on top of tm's
	AccessDeny         = "deny"          // a permission rule refusing (agent files)
	AccessEscapes      = "escapes"       // sandbox_escapes: on or off
	AccessYolo         = "yolo"          // on or off
	AccessApproves     = "coordinator-approves"
	AccessGuardOn      = "guard"       // on or off
	AccessGuardMode    = "guard-mode"  // enforce or learn
	AccessGuardRuleOff = "guard-off"   // a guard rule turned off
	AccessGuardAllow   = "guard-allow" // an allowance: value the command or path, note its rule
	AccessGuardFloor   = "guard-floor" // what the guard always refuses
	AccessSecret       = "secret"      // a secret variable's name; note its source, never a value
)

// Sources of a rule (AccessRule.Source) besides a project's slug and an
// agent file's path.
const (
	AccessFromTM      = "tm"           // built in
	AccessFromAll     = "all projects" // [defaults], [defaults.sandbox]
	AccessFromDefault = "tm default"   // an on/off setting nobody set
)

// Changes since launch (AccessRule.Change).
const (
	AccessAdded   = "added"
	AccessRemoved = "removed"
)

// AccessRule is one rule of a thread's access.
type AccessRule struct {
	Where string `json:"where"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
	// Source is AccessFromTM, AccessFromAll, AccessFromDefault, the
	// project's slug, or the agent file's path (~ for home).
	Source string `json:"source"`
	// Note says more: what a path is, where a host comes from, an
	// allowance's rule.
	Note string `json:"note,omitempty"`
	// OS are the platforms it applies on (runtime.GOOS names), Agents the
	// agents that get it; none: all.
	OS     []string `json:"os,omitempty"`
	Agents []string `json:"agents,omitempty"`
	// Skipped is why the launch leaves it out (another platform), "" for
	// a rule in force.
	Skipped string `json:"skipped,omitempty"`
	// Live: read when it is used (the guard), so a change needs no
	// restart.
	Live bool `json:"live,omitempty"`
	// Change marks, in a thread's view, a rule added or removed since its
	// launch: a restart applies it.
	Change string `json:"change,omitempty"`
}

// Same says whether r and o are the same rule, wherever each was read.
func (r AccessRule) Same(o AccessRule) bool {
	return r.Where == o.Where && r.Kind == o.Kind && r.Value == o.Value && r.Source == o.Source && r.Skipped == o.Skipped
}

// AccessSet is a thread's access: its rules and what they are for.
type AccessSet struct {
	Project string `json:"project"`
	Thread  string `json:"thread,omitempty"`
	Session string `json:"session,omitempty"`
	Role    string `json:"role"`
	Agent   string `json:"agent,omitempty"`
	OS      string `json:"os"`
	// Launched is when the thread launched with it; zero for what a new
	// thread would get.
	Launched time.Time    `json:"launched,omitzero"`
	Rules    []AccessRule `json:"rules"`
	// SandboxError is why the sandbox lists and presets are left out (a
	// config.toml that doesn't load, an entry that fails its check).
	SandboxError string `json:"sandbox_error,omitempty"`
}

// AccessShowParams name a project, and a thread of it for what that
// thread launched with.
type AccessShowParams struct {
	Project string `json:"project"`
	Thread  string `json:"thread,omitempty"`
}

// AccessShowResult is the set. For a thread, Recorded says it is its
// launch record (else what it would get now: it launched before tm kept
// one, or hasn't launched), and Changes counts the rules marked added or
// removed.
type AccessShowResult struct {
	AccessSet
	Recorded bool `json:"recorded,omitempty"`
	Changes  int  `json:"changes,omitempty"`
}

// AccessExplainParams name a project, optionally a thread of it, and a
// command line.
type AccessExplainParams struct {
	Project string `json:"project"`
	Thread  string `json:"thread,omitempty"`
	Command string `json:"command"`
}

// Whether a command asks first (AccessExplainResult.Prompt).
const (
	PromptAsks    = "asks"           // a prompt comes first
	PromptNone    = "without asking" // a rule lets it run
	PromptByAgent = "agent decides"  // no tm rule says: the agent's mode and own rules do
)

// AccessExplainResult is what a thread does with the command: Sandbox
// AccessInside or AccessOutside, Prompt one of PromptAsks, PromptNone,
// PromptByAgent, Guard the guard's verdict ("allowed", "refused",
// "asks", "off"), each with why and the rule's source.
type AccessExplainResult struct {
	Command     string `json:"command"`
	Sandbox     string `json:"sandbox"`
	SandboxWhy  string `json:"sandbox_why"`
	SandboxFrom string `json:"sandbox_from,omitempty"`
	Prompt      string `json:"prompt"`
	PromptWhy   string `json:"prompt_why"`
	PromptFrom  string `json:"prompt_from,omitempty"`
	Guard       string `json:"guard"`
	GuardWhy    string `json:"guard_why"`
	// Agents says which agents' threads the answer is not for, e.g. an
	// excluded command Codex threads aren't given.
	Agents string `json:"agents,omitempty"`
}

// Remarks are what the views say beside a rule: a change since launch,
// why it is left out, the only agents that get it, a guard rule's
// liveness.
func (r AccessRule) Remarks() []string {
	var out []string
	switch r.Change {
	case AccessAdded:
		out = append(out, "RESTART TO APPLY: added since launch")
	case AccessRemoved:
		out = append(out, "RESTART TO APPLY: removed since launch")
	}
	if r.Skipped != "" {
		out = append(out, "not used: "+r.Skipped)
	}
	if r.Where == AccessAgent {
		out = append(out, "the agent's own file, shown only")
	} else if len(r.Agents) > 0 {
		out = append(out, strings.Join(r.Agents, ", ")+" threads only")
	}
	return out
}
