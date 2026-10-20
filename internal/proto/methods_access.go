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
	AccessGitDir       = "git-dir"       // the git-dir grant (T551): value the repo whose git dir it opens
	AccessNetwork      = "network"       // a Codex thread's network where it isn't limited (Linux, Windows): "open"
	AccessRead         = "read"          // a folder readable without a prompt
	AccessWrite        = "write"         // a folder writable besides the cwd
	AccessWriteFile    = "write-file"    // a file writable besides the cwd
	AccessNoWrite      = "no-write"      // a folder never written
	AccessNoWriteFile  = "no-write-file" // a file never written
	AccessNoSiblings   = "no-siblings"   // a folder whose sibling folders, later ones too, are never written (a linked worktree's own git dir)
	AccessNoRead       = "no-read"       // a folder never read nor written (other sessions' temp folders)
	AccessTemp         = "temp"          // the thread's own temp folder, its TMPDIR
	AccessSocket       = "socket"        // tm's socket, reachable from the sandbox
	AccessCommand      = "command"       // a command prefix run without a prompt (the coordinator's merge)
	AccessPreset       = "preset"        // a sandbox preset's switch (launch records of tm 0.37's previews only)
	AccessLocalPorts   = "local-ports"   // sandboxed commands may listen on and reach localhost
	AccessHost         = "host"          // a host sandboxed commands may reach
	AccessGuide        = "guide"         // a guide in the brief: value its title, body its text ("" for a name of tm 0.38)
	AccessExcluded     = "excluded"      // a command run outside the sandbox; note AccessAsksFirst or AccessWithoutAsking
	AccessWriteFolder  = "write-folder"  // a write folder (config write): value its absolute path, note as written
	AccessUnixSocket   = "unix-socket"   // a project's socket (config sockets): value its absolute path, note as written
	AccessApproved     = "approved"      // a command tm runs outside the sandbox when the thread asks (tm run); live
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

// Why a rule isn't used under yolo (AccessRule.Skipped): the sandbox
// is off, and the guard keeps its floor only (T574 case 4).
const (
	AccessYoloNoSandbox = "yolo is on: the sandbox is off"
	AccessYoloFloor     = "floor only: yolo is on"
)

// An excluded command's note: whether it asks first (the guard asks)
// or runs without asking (tm writes its allow rule).
const (
	AccessAsksFirst     = "asks first"
	AccessWithoutAsking = "without asking"
)

// AccessSettingsNote is the note of the rule that names the human's
// settings file (config.toml), which the TUI shows by that name only.
const AccessSettingsNote = "your settings"

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
	// Body is a guide's text (AccessGuide).
	Body string `json:"body,omitempty"`
	// OS are the platforms it applies on (runtime.GOOS names), Agents the
	// agents that get it; none: all.
	OS     []string `json:"os,omitempty"`
	Agents []string `json:"agents,omitempty"`
	// Skipped is why the launch leaves it out (another platform, yolo),
	// "" for a rule in force.
	Skipped string `json:"skipped,omitempty"`
	// Dropped: all projects' rule, which the project drops (Note names
	// it); the launch leaves it out.
	Dropped bool `json:"dropped,omitempty"`
	// Refused is why the rule fails its check; the launch leaves it out,
	// and only it.
	Refused string `json:"refused,omitempty"`
	// Live: read when it is used (the guard), so a change needs no
	// restart.
	Live bool `json:"live,omitempty"`
	// Change marks, in a thread's view, a rule added or removed since its
	// launch: a restart applies it.
	Change string `json:"change,omitempty"`
}

// Same says whether r and o give a launch the same, wherever each was
// read and whichever table set it.
func (r AccessRule) Same(o AccessRule) bool {
	// An excluded command's note is its choice to ask first.
	return r.Where == o.Where && r.Kind == o.Kind && r.Value == o.Value && r.Body == o.Body && r.Skipped == o.Skipped &&
		r.Dropped == o.Dropped && r.Refused == o.Refused && (r.Kind != AccessExcluded && r.Kind != AccessApproved || r.Note == o.Note)
}

// Given reports whether a launch gives the rule: not one left out
// where it runs, dropped, or refused.
func (r AccessRule) Given() bool { return r.Skipped == "" && !r.Dropped && r.Refused == "" }

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
	// SandboxError is why the sandbox rules are left out: a config.toml
	// that doesn't load (an entry that fails its check is refused on its
	// own, AccessRule.Refused).
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
	// Approved says which approved command tm run would run it as,
	// outside the sandbox, and for which threads; "" for none.
	Approved string `json:"approved,omitempty"`
}

// The notes on an excluded command of the agent's own settings files
// (T574 case 7): the guard treats a command it runs outside the
// sandbox, and none of tm's does, as an escape.
const (
	AccessOwnEscape    = "tm treats it as an escape: asks you"
	AccessOwnEscapeOff = "tm treats it as an escape: refused, escapes are off"
)

// ExcludedOnlyItself is what excluded command e, which has no *, leaves
// in the sandbox, or "": Claude matches it as a Bash rule (T632), so
// "docker" takes only docker run bare, never docker ps; "docker *"
// takes its arguments too. tm doesn't widen it: that is the human's.
func ExcludedOnlyItself(e string) string {
	if e == "" || strings.Contains(e, "*") {
		return ""
	}
	return "only " + e + " itself, with no arguments: " + e + " * takes it with them"
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
	if r.Refused != "" {
		out = append(out, "REFUSED: "+r.Refused)
	}
	if r.Dropped {
		out = append(out, "dropped: not given")
	}
	if r.Skipped != "" {
		out = append(out, "not used: "+r.Skipped)
	}
	if r.Where == AccessAgent && (r.Note == AccessOwnEscape || r.Note == AccessOwnEscapeOff) {
		out = append(out, "the agent's own file: tm doesn't change it, its guard judges the commands")
	} else if r.Where == AccessAgent {
		out = append(out, "the agent's own file, shown only")
	} else if len(r.Agents) > 0 {
		out = append(out, strings.Join(r.Agents, ", ")+" threads only")
	}
	if r.Kind == AccessExcluded {
		if n := ExcludedOnlyItself(r.Value); n != "" {
			out = append(out, n)
		}
	}
	return out
}
