// Package guard is the guard's matcher (docs/SPEC.md §8.6, Guard): the
// standing rules as checks on each tool call: the server judges each
// call a PreToolUse hook reports and the agent's manifest answers with
// the refusal (.Guard in a respond template). Its test vectors are
// testdata/vectors.json.
//
// It refuses, or for a PowerShell command it can't read, asks: a call no
// rule matches goes on to the agent's own permission check. Bash
// commands are split into simple commands on ; & | && || newlines, ( )
// and { }, the inside of a $( ) or backticks read as commands too, and
// read shell-style (quotes, comments, env assignments, wrappers such as
// sudo and timeout, bash -c, heredocs, git -C). That catches what agents
// type; the sandbox and the permission rules stay the backstop for what
// is written to hide. PowerShell
// commands (powershell.go) have no sandbox behind them on Windows, so
// what names its command by an expression, or evaluates a string, is put
// to the human (or auto mode's classifier) instead.
package guard

import (
	"cmp"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Rules are what a session's guard refuses, as the server works them out
// from the human's settings and the session's role. Off, nothing is
// refused.
type Rules struct {
	On   bool   `json:"on"`
	Role string `json:"role,omitempty"`
	// Rules are the config.GuardRules ids in force.
	Rules []string `json:"rules,omitempty"`
	// Home is the user's home directory, for ~ in paths and commands;
	// Cwd the folder the session started in, for relative paths.
	Home string `json:"home,omitempty"`
	Cwd  string `json:"cwd,omitempty"`
	// Writable are the folders the file tools may write in under
	// worktree-only: the thread's worktree first, then the temporary
	// folders and Claude's own (plans, memory). Each as given and
	// with its symlinks resolved.
	Writable []string `json:"writable,omitempty"`
	// GitConfig are what git runs or reads as configuration in the main
	// repo's git dir (hooks, config, info): under worktree-only, no
	// shell command names them but to view them. The thread's sandbox
	// refuses writing them too; this says why sooner.
	GitConfig []string `json:"gitConfig,omitempty"`
	// Worktrees is tm's worktrees folder: under delete-branch, no rm -r
	// takes it, a project's folder in it or a worktree.
	Worktrees string `json:"worktrees,omitempty"`
	// Protected are the branches no push may target: the repos' default
	// branches, main and master.
	Protected []string `json:"protected,omitempty"`
	// Secrets are the files and folders no tool may read under
	// credentials.
	Secrets []string `json:"secrets,omitempty"`
	// Tools are the session's agent's tools the guard judges, by name
	// (its manifest's [guard.tools]); a tool not listed is not judged.
	Tools map[string]Tool `json:"tools,omitempty"`
	// CaseFold: the file system ignores case in names (macOS, Windows;
	// plat/caps), so paths are compared with case folded and
	// ~/.Terminatr/worktrees is the worktrees folder.
	CaseFold bool `json:"caseFold,omitempty"`
	// Mods are the rules the user's enabled mods add: they only refuse,
	// in force whichever core rules the human turned off.
	Mods []ModRule `json:"mods,omitempty"`
	// Allow are the human's allowances for the session's project
	// (config guard_allow): a refusal under an allowance's rule of a call
	// whose command line, or the file it names, the allowance matches is
	// lifted, and the call judged again without that rule. Never one of
	// the floor's (Denial.Floor). Paths absolute.
	Allow []Allow `json:"allow,omitempty"`
	// Own are tm's own files and folders (its run folder with the
	// sockets, its keys, the guard's log): no tool reads them, whichever
	// rules are on (the floor).
	Own []string `json:"own,omitempty"`
	// OtherTemps hold the other threads' own temp folders (/tmp/tm-<uid>,
	// T587): no file tool reads or writes in them but in OwnTemp, the
	// thread's own, whichever rules are on (the floor). Shell commands
	// are the sandbox's to keep out.
	OtherTemps []string `json:"otherTemps,omitempty"`
	OwnTemp    string   `json:"ownTemp,omitempty"`
	// SecretEnv are the names of the secret variables tm gives the
	// session (config env): no command prints them or the whole
	// environment, whichever rules are on (the floor).
	SecretEnv []string `json:"secretEnv,omitempty"`
	// NoWrite are the files and folders no tool writes, whichever rules
	// are on (the floor): tm's settings (config.toml), what a shell runs
	// before each command (the user's startup files, the agents' shell
	// snapshots), and a thread's project folder or the parts of it only
	// tm writes for the coordinator. File tools are judged by where they write, shell
	// commands by the paths they redirect into or give a command that
	// writes (cp, mv, tee, rm and the like: judgeNoWrite).
	NoWrite []string `json:"noWrite,omitempty"`
	// AskEscapes: a shell call that asks to run outside the sandbox (its
	// tool's Escape field true) is put to the user (rule "escape"), even
	// where the agent's own rules would let it run: tm answers that
	// prompt for the user only, never for the coordinator (T574 case 6).
	AskEscapes bool `json:"askEscapes,omitempty"`
	// AskFirst are the thread's excluded commands that ask first
	// (config excluded_commands without ask = false), in Claude's Bash
	// rule syntax ("npx playwright *"): a shell call with a command one
	// matches runs outside the sandbox, so it is put to the user (rule
	// "excluded"), even where an allow rule or auto mode would let it run
	// (T574 case 1).
	AskFirst []string `json:"askFirst,omitempty"`
	// Deadline, when set, is when Judge gives up: a judge past it stops
	// reading and refuses the call (unchecked), so one the server stopped
	// waiting for doesn't run on.
	Deadline time.Time `json:"-"`
	// ps is the reading of the call being judged, shared by every copy
	// of the rules Judge makes: one budget and deadline for every read
	// and re-read of its command lines (sh -c, eval, wrappers), and depth
	// how deeply the line being read is nested in them.
	ps    *parse
	depth int
	// Unsandboxed are the command patterns of the agent's own settings
	// files that run a command outside the sandbox (Claude's
	// sandbox.excludedCommands in ~/.claude and the folder's .claude),
	// with no flag in the call for Escape to see (T574 case 7). A call
	// of a shell tool with an Escape field that one takes, and that none
	// of TMExcluded (the excluded commands tm gives the thread) takes,
	// is an escape: put to the user (rule "escape"), or with NoEscapes
	// refused. They are matched more widely than the agent does (any of
	// the line's commands, a bare word as a prefix), so the guard asks
	// rather than misses.
	Unsandboxed []string `json:"unsandboxed,omitempty"`
	TMExcluded  []string `json:"tmExcluded,omitempty"`
	// NoEscapes: the thread's escapes are off (sandbox_escapes = false),
	// so a call Unsandboxed takes is refused.
	NoEscapes bool `json:"noEscapes,omitempty"`
	// AccessWrites: the project lets its coordinator change its access
	// rules (coordinator_access), so the floor lets the coordinator's tm
	// access writes of rules through (accessOpen): tm checks them in the
	// server. Never one for all projects (--all), a reset or a switch
	// but local ports (protection, escapes, yolo, coordinator), which
	// stay the human's.
	AccessWrites bool `json:"accessWrites,omitempty"`
	// Learn is guard_mode "learn": a refusal tells the model the user
	// reviews it.
	Learn bool `json:"learn,omitempty"`
	// Floor are the rules whose floor holds though the human turned the
	// rule off (guard_off): force-push (to a protected branch, --all,
	// --mirror) always, merge for a thread while the coordinator merges.
	// Only the guard off lifts them.
	Floor []string `json:"floor,omitempty"`
	// Resolve, when set, is where a write to a path lands: the path made
	// absolute but not yet cleaned, with the links of its longest existing
	// prefix followed. The matcher has no file system of its own; the
	// server, which judges the hooks' calls, gives it one. Not served.
	Resolve func(string) string `json:"-"`
	// File, when set, is whether a path, absolute but not cleaned as
	// Resolve is asked about, lands on a regular file: where a shell given
	// input runs a script of the worktree's (worktreeScript). Not served.
	File func(string) bool `json:"-"`
	// line is the command line being judged (with the lines that run it,
	// for bash -c), and lineCd whether one of them has a cd: then a path
	// from a folder the line can change (lineFolder) is not placed.
	line   string
	lineCd bool
	// physical: the line may make its cds physical (physicalCd).
	physical bool
	// olds are the folders the line was in before its folder now
	// ($OLDPWD, ~-; "" one it can't tell), as judgeLine follows its cds.
	olds []string
	// wt is the thread's worktree, as judgeBash takes it (worktreeOf).
	wt string
	// trapFrom is where the trap actions the line being read sets start
	// in ps.traps (judgeLine).
	trapFrom int
	// sub: the line is a group's or a substitution's (walkLine), which
	// the traps set before it may run in too.
	sub bool
}

// ModRule is a guard rule a mod adds (mods design §7, Tighten only): the
// commands a shell tool may not run and the files no file tool may write
// or read, with the mod's sentence. Its commands are judged on each
// simple command before the core's rules; it only ever refuses.
type ModRule struct {
	Mod string `json:"mod"`
	ID  string `json:"id"`
	// Commands are the commands refused, each its words: the command's
	// name, then words its arguments include in that order, others
	// between them allowed. A word ending in * matches any word it
	// starts. PowerShell's commands are compared with case folded.
	Commands [][]string `json:"commands,omitempty"`
	// Writes are the files and folders no write or patch tool writes in;
	// Reads those no read or glob tool reads (a glob by the part before
	// its first wildcard). Absolute.
	Writes  []string `json:"writes,omitempty"`
	Reads   []string `json:"reads,omitempty"`
	Message string   `json:"message"`
}

// Name is the rule's name in a refusal and the journal: mod:<mod>.<id>.
func (m ModRule) Name() string { return "mod:" + m.Mod + "." + m.ID }

// Allow is one of the human's allowances (config.GuardAllow): Rule's
// refusals of the command line Command, or of the file Path, are
// lifted. A * matches any run of characters, but in an Exact one,
// which matches only that command or path; a credentials allowance with
// one is ignored (it names one exact command or path).
type Allow struct {
	Rule    string `json:"rule"`
	Command string `json:"command,omitempty"`
	Path    string `json:"path,omitempty"`
	Exact   bool   `json:"exact,omitempty"`
}

// Tool is what the guard knows of one of an agent's tools: its kind and
// the fields of its input the kind reads. A shell tool's Syntax is how
// its command lines are read: "" for a POSIX shell, SyntaxPowerShell
// for PowerShell (Claude's PowerShell tool on Windows).
type Tool struct {
	Kind   Kind     `json:"kind" toml:"kind"`
	Syntax string   `json:"syntax,omitempty" toml:"syntax"`
	Fields []string `json:"fields" toml:"fields"`
	// Escape is a shell tool's input field that, true, asks to run the
	// call outside the sandbox (Claude's dangerouslyDisableSandbox).
	Escape string `json:"escape,omitempty" toml:"escape"`
	// Optional are the Fields the tool may leave out, each a location
	// that then means the session's folder (Grep's and Glob's path):
	// the call is judged with that. A call without one of the others,
	// which the tool always carries, is refused.
	Optional []string `json:"optional,omitempty" toml:"optional"`
}

// RuleTMFiles is the rule of the floor's refusal to write tm's settings
// or the project folder (Rules.NoWrite).
const RuleTMFiles = "tm-files"

// RuleTempFolders is the rule of the floor's refusal of a file tool in
// another thread's temp folder (Rules.OtherTemps).
const RuleTempFolders = "temp-folders"

// RuleEscape is the rule of the guard's question about a call that asks
// to leave the sandbox (Rules.AskEscapes).
const RuleEscape = "escape"

// RuleExcluded is the rule of the guard's question about a call that
// runs an excluded command that asks first (Rules.AskFirst).
const RuleExcluded = "excluded"

// RuleUnchecked is the rule of a refusal of a call nobody could judge:
// tm hook got no verdict from the server, or the server ran out of
// time judging it (docs/SPEC.md §8.6, Guard: the guard fails closed).
const RuleUnchecked = "unchecked"

// Unchecked is the refusal of a call tm couldn't check: the agent's
// hook answers with it when the server's verdict doesn't come.
func Unchecked() *Denial {
	return &Denial{Rule: RuleUnchecked, Floor: true, Summary: "a call tm couldn't check",
		Message: "terminatr guard: tm couldn't check this call (its server didn't answer in time), so it is refused; try again."}
}

// RuleUnreadable is the rule of the floor's refusal of a command line
// past the guard's limits (MaxCommand, maxNesting, maxParse).
const RuleUnreadable = "unreadable"

// unreadable is the refusal of a command line past the guard's limits.
func unreadable() *Denial {
	return &Denial{Rule: RuleUnreadable, Floor: true, Summary: "a command too long or nested too deep to check",
		Message: fmt.Sprintf("terminatr guard: this command is too long (over %d KiB) or nested too deep (substitutions, subshells or heredocs over %d levels) for the guard to check, so it is refused; split it into shorter commands, or put the script in a file in the worktree and run that.", MaxCommand>>10, maxNesting)}
}

// SyntaxPowerShell marks a shell tool whose command lines are PowerShell.
const SyntaxPowerShell = "powershell"

// Syntaxes are the syntaxes a manifest may give a shell tool.
var Syntaxes = []string{SyntaxPowerShell}

// Kind is what a tool does, as the guard judges it.
type Kind string

const (
	// KindShell runs a command line (each field one), judged command by
	// command.
	KindShell Kind = "shell"
	// KindWrite writes the file each field names (worktree-only).
	KindWrite Kind = "write"
	// KindPatch applies a patch (each field one, Codex's *** Begin Patch
	// format) and writes each file it adds, updates, deletes or moves to.
	KindPatch Kind = "patch"
	// KindRead reads the file or folder each field names (credentials).
	KindRead Kind = "read"
	// KindGlob is KindRead whose fields may be glob patterns, judged by
	// the part before the first wildcard.
	KindGlob Kind = "glob"
)

// Kinds are the kinds a manifest may give a tool.
var Kinds = []Kind{KindShell, KindWrite, KindPatch, KindRead, KindGlob}

// Denial is a refusal: the rule, what the model reads, and a short
// account for the journal that carries no text of the call beyond a
// path or branch. With Ask it is no refusal but a question: the call is
// one the guard can't read (rule "unknown"), so it goes to the agent's
// dialog or classifier however its rules would allow it; Message is
// the reason the dialog shows. An ask is not journaled.
//
// Floor marks a refusal no allowance lifts, in any mode: reading tm's
// own files, printing a secret variable tm gives the session,
// force-pushing to a protected branch, a thread merging. Subject is what
// an allowance of the refusal names: the whole command line, or the file
// (where a write lands); it is the human's to see (the guard's log),
// never journaled.
type Denial struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
	Summary string `json:"summary"`
	Ask     bool   `json:"ask,omitempty"`
	Floor   bool   `json:"floor,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// learnNote ends a refusal's sentence in learn mode.
const learnNote = " The user reviews refused calls: if they allow this one, you will be told and may retry it; until then don't work around it."

// Judge is the rule the call of tool with input breaks, or nil. The
// tool is judged by its kind in r.Tools, from the fields listed there.
//
// One judge reads the call's command lines with one budget: every read
// and re-read (sh -c, eval, a wrapper's command, an allowance judged
// again) counts against judgeParse and maxNesting, and Deadline ends it;
// past either it stops and refuses the call (unreadable, or unchecked
// for the deadline), never judging it in part.
func (r Rules) Judge(tool string, input map[string]any) *Denial {
	if r.ps == nil {
		r.ps = &parse{limit: judgeParse, deadline: r.Deadline}
	}
	d := r.judge(tool, input)
	return r.ps.verdict(d)
}

// judgeParse bounds the bytes one judge reads in all, its re-reads
// counted: a line inside maxParse is read several times over (its
// parts, its folders, an allowance judged again).
const judgeParse = 16 << 20

// verdict is d, unless the reading went past its limits: then the
// refusal of a call the guard couldn't read, or couldn't in time.
func (p *parse) verdict(d *Denial) *Denial {
	switch {
	case p.late:
		d = Unchecked()
		d.Message = "terminatr guard: checking this call took too long, so it is refused; try a shorter or simpler command."
	case p.over:
		d = unreadable()
	}
	return d
}

func (r Rules) judge(tool string, input map[string]any) *Denial {
	if !r.On || len(r.Rules) == 0 && len(r.Mods) == 0 && len(r.Own) == 0 && len(r.OtherTemps) == 0 && len(r.SecretEnv) == 0 && len(r.Floor) == 0 && len(r.NoWrite) == 0 && !r.AskEscapes && len(r.Unsandboxed) == 0 && len(r.AskFirst) == 0 {
		return nil
	}
	t, ok := r.Tools[tool]
	if !ok {
		return nil
	}
	var values, missing, malformed []string
	for _, f := range t.Fields {
		v, has := input[f]
		switch s, ok := v.(string); {
		case ok && s != "":
			values = append(values, s)
		case has && !ok:
			// There, but not the text a location or command is: the
			// guard can't tell what it names.
			malformed = append(malformed, f)
		case slices.Contains(t.Optional, f):
			// A location left out: the session's own folder (the agent's
			// own, when its hook gave it, is filled in before: agent).
			if r.Cwd != "" {
				values = append(values, r.Cwd)
			}
		default:
			missing = append(missing, f)
		}
	}
	if len(malformed) > 0 {
		d := Unchecked()
		d.Summary = "a " + tool + " call with a malformed " + strings.Join(malformed, " or ")
		d.Message = "terminatr guard: this call's " + strings.Join(malformed, ", ") + " is not text the guard can read, so it is refused."
		return d
	}
	if len(missing) > 0 {
		// A field the tool always carries is missing (its input trimmed
		// away, or not the tool's shape): nothing to judge it by, so
		// refused, never let through.
		d := Unchecked()
		d.Summary = "a " + tool + " call without " + strings.Join(missing, " or ")
		d.Message = "terminatr guard: this call's input lacks " + strings.Join(missing, ", ") + ", which the guard reads, so it is refused."
		return d
	}
	switch t.Kind {
	case KindShell:
		// A line past the guard's limits is refused whole, whatever else
		// is allowed: read in part, what it hides would run unjudged.
		for _, c := range values {
			if len(c) > MaxCommand || t.Syntax == "" && !readable(c) {
				return unreadable()
			}
		}
		var ask *Denial
		for _, c := range values {
			d := r.lifted(strings.TrimSpace(c), false, func(r Rules) *Denial {
				if t.Syntax == SyntaxPowerShell {
					return r.judgePowerShell(c)
				}
				return r.judgeBash(c)
			})
			if d != nil && !d.Ask {
				return d
			}
			if ask == nil {
				ask = d
			}
		}
		if t.Escape != "" {
			for _, c := range values {
				if d := r.judgeUnsandboxed(strings.TrimSpace(c)); d != nil && !d.Ask {
					return d
				} else if ask == nil {
					ask = d
				}
			}
		}
		if ask == nil && r.AskEscapes && t.Escape != "" && input[t.Escape] == true {
			ask = &Denial{Rule: RuleEscape, Ask: true, Summary: "a command outside the sandbox",
				Message: "terminatr guard: this command asks to run outside the sandbox, so the user answers it."}
		}
		if ask == nil && t.Syntax == "" {
			for _, c := range values {
				if p := r.askFirst(c); p != "" {
					// p passed config's checks: one line of plain text.
					ask = &Denial{Rule: RuleExcluded, Ask: true, Summary: "an excluded command that asks first",
						Message: "terminatr guard: this command runs outside the sandbox (excluded command " + p + "), and the user set it to ask first."}
					break
				}
			}
		}
		return ask
	case KindWrite:
		for _, p := range values {
			if d := r.lifted(r.target(p), true, func(r Rules) *Denial { return r.judgeWrite(tool, r.abs(p), r.target(p)) }); d != nil {
				return d
			}
		}
	case KindPatch:
		for _, patch := range values {
			for _, p := range PatchPaths(patch) {
				if d := r.lifted(r.target(p), true, func(r Rules) *Denial { return r.judgeWrite(tool, r.abs(p), r.target(p)) }); d != nil {
					return d
				}
			}
		}
	case KindRead, KindGlob:
		for _, p := range values {
			p = r.abs(p)
			if d := r.lifted(p, true, func(r Rules) *Denial { return r.judgeRead(tool, p) }); d != nil {
				return d
			}
		}
	}
	return nil
}

// judgeUnsandboxed judges a command line by the agent's own excluded
// commands (Rules.Unsandboxed): one that runs it outside the sandbox
// where none of tm's does is an escape, asked of the user or, with the
// thread's escapes off, refused.
func (r Rules) judgeUnsandboxed(line string) *Denial {
	if len(r.Unsandboxed) == 0 || slices.ContainsFunc(r.TMExcluded, func(e string) bool { return excludedTakes(e, line) }) {
		return nil
	}
	var cands []string
	for _, words := range r.cmds(line) {
		cands = append(cands, strings.Join(words, " "), strings.Join(strip(words), " "))
	}
	cands = append(cands, line)
	if !slices.ContainsFunc(r.Unsandboxed, func(p string) bool {
		return slices.ContainsFunc(cands, func(c string) bool { return unsandboxedTakes(p, c) })
	}) {
		return nil
	}
	const summary = "an excluded command of the agent's own settings"
	if r.NoEscapes {
		return &Denial{Rule: RuleEscape, Summary: summary,
			Message: "terminatr guard (escape): this command runs outside the sandbox through an excluded command in your agent's own settings (sandbox.excludedCommands, not tm's), and this thread's escapes are off. Run it another way inside the sandbox; if the task needs it, tell the user."}
	}
	return &Denial{Rule: RuleEscape, Ask: true, Summary: summary,
		Message: "terminatr guard: this command runs outside the sandbox through an excluded command of the user's own settings, so the user answers it."}
}

// unsandboxedTakes: the agent's excluded command p takes command c, read
// widely: * any text, a trailing " *" (or the older ":*") no arguments
// too, and a pattern without * also the command with arguments.
func unsandboxedTakes(p, c string) bool {
	p = strings.TrimSpace(p)
	if q, ok := strings.CutSuffix(p, ":*"); ok {
		p = q + " *"
	}
	if p == "" || c == "" {
		return false
	}
	if q, ok := strings.CutSuffix(p, " *"); ok && c == q {
		return true
	}
	return globMatch(p, c) || !strings.Contains(p, "*") && strings.HasPrefix(c, p+" ")
}

// excludedTakes: tm's excluded command e takes command line c as Claude
// matches it (config.ExcludedMatch): one command, without the shell's
// operators, that e's pattern takes.
func excludedTakes(e, c string) bool {
	for _, op := range []string{";", "&", "|", "<", ">", "`", "$(", "\n"} {
		if strings.Contains(c, op) {
			return false
		}
	}
	if q, ok := strings.CutSuffix(e, " *"); ok && c == q {
		return true
	}
	return globMatch(e, c)
}

// judgeRead judges a file tool reading p (absolute; a glob by the part
// before its first wildcard).
func (r Rules) judgeRead(tool, p string) *Denial {
	if r.otherTemp(p) {
		return floor(r.deny(RuleTempFolders, tool+" of "+safe(p), ""))
	}
	if r.covers(r.Own, p) {
		return floor(r.deny("credentials", tool+" of "+safe(p), ""))
	}
	if r.has("credentials") && r.isSecret(p) {
		return r.deny("credentials", tool+" of "+safe(p), "")
	}
	for _, m := range r.Mods {
		if r.covers(m.Reads, p) {
			return r.denyMod(m, tool+" of "+safe(p))
		}
	}
	return nil
}

// lifted is judge's refusal of a call, the ones the human allowed lifted:
// while it refuses under a rule an allowance of subject (the command
// line, or with path the file) names, it judges again without that rule.
// The refusal that stands carries subject, and in learn mode says the
// user reviews it.
func (r Rules) lifted(subject string, path bool, judge func(Rules) *Denial) *Denial {
	for {
		d := judge(r)
		if d == nil || d.Ask {
			return d
		}
		if !d.Floor && r.allows(d.Rule, subject, path) {
			r = r.without(d.Rule)
			continue
		}
		d.Subject = subject
		if r.Learn && !d.Floor {
			d.Message += learnNote
		}
		return d
	}
}

// allows: an allowance of rule matches subject, a command line or with
// path a file.
func (r Rules) allows(rule, subject string, path bool) bool {
	for _, a := range r.Allow {
		text := a.Command
		if path {
			text = a.Path
		}
		if a.Rule != rule || strings.TrimSpace(text) == "" || rule == "credentials" && strings.Contains(text, "*") {
			continue
		}
		match := globMatch
		if a.Exact {
			match = func(text, s string) bool { return text == s }
		}
		if path && match(r.fold(r.abs(text)), r.fold(subject)) || !path && match(strings.TrimSpace(text), subject) {
			return true
		}
	}
	return false
}

// without is r without rule: a core rule's id, or a mod's rule's name.
func (r Rules) without(rule string) Rules {
	if strings.HasPrefix(rule, "mod:") {
		r.Mods = slices.DeleteFunc(slices.Clone(r.Mods), func(m ModRule) bool { return m.Name() == rule })
	} else {
		r.Rules = slices.DeleteFunc(slices.Clone(r.Rules), func(id string) bool { return id == rule })
	}
	return r
}

// globMatch: s is pattern, a * in it matching any run of characters.
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return len(s) >= len(last) && strings.HasSuffix(s, last)
}

// askFirst is the excluded command of AskFirst that a command of line
// matches, or "": the line, or one of its simple commands as written or
// without its env assignments and wrappers. Claude runs a line outside
// the sandbox only when each of its commands matches an excluded one;
// one is enough here, so a prompt comes whenever one might run outside.
func (r Rules) askFirst(line string) string {
	if len(r.AskFirst) == 0 {
		return ""
	}
	texts := []string{strings.TrimSpace(line)}
	for _, words := range r.cmds(line) {
		texts = append(texts, strings.Join(words, " "))
		if argv := strip(words); len(argv) > 0 {
			texts = append(texts, strings.Join(argv, " "))
		}
	}
	for _, p := range r.AskFirst {
		for _, s := range texts {
			if excludedMatch(p, s) {
				return p
			}
		}
	}
	return ""
}

// excludedMatch: command s fits excluded command p as Claude matches
// it, a * matching any run of characters and a trailing " *" (or the
// prefix form ":*") no arguments too.
func excludedMatch(p, s string) bool {
	if q, ok := strings.CutSuffix(p, ":*"); ok {
		p = q + " *"
	}
	if q, ok := strings.CutSuffix(p, " *"); ok && s == q {
		return true
	}
	return globMatch(p, s)
}

// floor marks d as a refusal no allowance lifts.
func floor(d *Denial) *Denial {
	if d != nil {
		d.Floor = true
	}
	return d
}

// target is where a file tool writing p writes: p made absolute, through
// Resolve when there is one.
func (r Rules) target(p string) string {
	if r.Resolve == nil {
		return r.abs(p)
	}
	return r.abs(r.resolve(r.joined(p)))
}

// fsCheck is what one file-system check (Resolve, File) counts against
// a judge's budget, as bytes read: a line that needs more than
// judgeParse/fsCheck of them is refused as unreadable at once, rather
// than run out the deadline on a slow file system. A judge checks each
// path once.
const fsCheck = judgeParse / 64

// resolve is r.Resolve(p), counted against the judge's budget; past it,
// p as it is (Judge refuses the call).
func (r Rules) resolve(p string) string {
	if r.ps == nil {
		return r.Resolve(p)
	}
	if q, ok := r.ps.resolved[p]; ok {
		return q
	}
	if r.ps.stop(fsCheck, r.depth) {
		return p
	}
	q := r.Resolve(p)
	if r.ps.resolved == nil {
		r.ps.resolved = map[string]string{}
	}
	r.ps.resolved[p] = q
	return q
}

// file is r.File(p), counted against the judge's budget as resolve is;
// past it, false.
func (r Rules) file(p string) bool {
	if r.ps == nil {
		return r.File(p)
	}
	if f, ok := r.ps.files[p]; ok {
		return f
	}
	if r.ps.stop(fsCheck, r.depth) {
		return false
	}
	f := r.File(p)
	if r.ps.files == nil {
		r.ps.files = map[string]bool{}
	}
	r.ps.files[p] = f
	return f
}

// judgeWrite judges a file tool writing p (absolute, where the write
// lands), named in a refusal as shown (as the call gave it).
func (r Rules) judgeWrite(tool, shown, p string) *Denial {
	if r.otherTemp(p) {
		return floor(r.deny(RuleTempFolders, tool+" of "+safe(shown), ""))
	}
	if r.covers(r.NoWrite, p) {
		return floor(r.deny(RuleTMFiles, tool+" of "+safe(shown), ""))
	}
	if r.has("worktree-only") && !r.writable(p) {
		return r.deny("worktree-only", tool+" of "+safe(shown), "")
	}
	for _, m := range r.Mods {
		if slices.ContainsFunc(m.Writes, func(w string) bool { return r.under(p, w) }) {
			return r.denyMod(m, tool+" of "+safe(shown))
		}
	}
	return nil
}

// judgeMods judges a simple command, its name and arguments, by the
// mods' rules; fold compares them with case folded (PowerShell's), and
// under CaseFold the name is.
func (r Rules) judgeMods(name string, args []string, fold bool) *Denial {
	for _, m := range r.Mods {
		for _, c := range m.Commands {
			if len(c) == 0 || !modWord(c[0], name, fold || r.CaseFold) {
				continue
			}
			j := 1
			for _, a := range args {
				if j < len(c) && modWord(c[j], a, fold) {
					j++
				}
			}
			if j == len(c) {
				words := make([]string, len(c))
				for i, w := range c {
					words[i] = safe(w)
				}
				return r.denyMod(m, strings.Join(words, " "))
			}
		}
	}
	return nil
}

// modWord: word w is pattern p, or starts with p's text before a final *.
func modWord(p, w string, fold bool) bool {
	if fold {
		p, w = strings.ToLower(p), strings.ToLower(w)
	}
	if pre, ok := strings.CutSuffix(p, "*"); ok {
		return strings.HasPrefix(w, pre)
	}
	return p == w
}

// denyMod is the refusal under mod rule m: its own sentence, labelled
// with the mod.
func (r Rules) denyMod(m ModRule, summary string) *Denial {
	return &Denial{Rule: m.Name(), Message: "terminatr guard (" + m.Name() + "): " + m.Message, Summary: summary}
}

var patchFile = regexp.MustCompile(`(?m)^\*\*\* (?:(?:Add|Update|Delete) File|Move to): (.+?)\s*$`)

// PatchPaths are the files a Codex patch (*** Begin Patch …) writes.
func PatchPaths(patch string) []string {
	var out []string
	for _, m := range patchFile.FindAllStringSubmatch(patch, -1) {
		out = append(out, m[1])
	}
	return out
}

// has: rule id is on. Merge on the floor is: every merge refusal is
// the floor's.
func (r Rules) has(id string) bool {
	return slices.Contains(r.Rules, id) || id == "merge" && slices.Contains(r.Floor, id)
}

func (r Rules) writable(p string) bool {
	return slices.ContainsFunc(r.Writable, func(w string) bool { return r.under(p, w) })
}

// deny is the refusal under rule; the sentence's end says what to do,
// which differs for a thread (its report) and the coordinator (the user).
func (r Rules) deny(rule, summary, target string) *Denial {
	tell := "ask the user"
	if r.Role == "thread" {
		tell = "say so in your report"
	}
	first := ""
	if len(r.Writable) > 0 {
		first = r.Writable[0]
	}
	if target == "" {
		target = "the default branch"
	}
	var m string
	switch rule {
	case "force-push":
		m = "force-pushing is not allowed here. Push without force; if the branch has diverged, merge instead of rewriting it, or " + tell + "."
	case "push-default":
		m = "nothing is pushed straight to " + target + ": work lands through a pull request. Push your own branch and report; the coordinator opens the PR."
	case "worktree-only":
		m = "a thread writes only inside its worktree (" + first + "). Write the file there, or " + tell + " what needs changing elsewhere."
	case "delete-branch":
		m = "agents don't delete branches or worktrees; tm cleans them up once the work has merged. Leave them, and " + tell + " if one is in the way."
	case "merge":
		m = `merging is the coordinator's job in this project (merge = "coordinator"). Leave the PR open with CI green and say so in your report.`
	case RuleTMFiles:
		if r.Role == "thread" {
			m = "a thread doesn't write tm's own files (config.toml, agents/, mods/, state/ and the rest of tm's home but its worktrees), the project folder, the worktree's .claude/settings.json and settings.local.json (they could turn tm's hooks off) or the shell's startup files and snapshots (.bashrc, .zshrc and the like: they run before each command). Write elsewhere in your worktree, or say so in your report."
		} else {
			m = "tm's own files (config.toml, agents/, mods/, state/ and the rest of tm's home but the projects and worktrees), the parts of the project folder tm keeps (threads/, inbox/, tasks/, journal/, TASKS.md and the like) the folder's .claude/settings.json and settings.local.json (they could turn tm's hooks off) and the shell's startup files and snapshots (.bashrc, .zshrc and the like: they run before each command) are written by tm or the user only: use tm's commands, or ask the user."
		}
	case RuleTempFolders:
		m = "other threads' temp folders are not read or written here. Keep your files in your own temp folder ($TMPDIR) or your worktree, or " + tell + "."
	case "credentials":
		m = "credential files and tokens are not read here. A tool that needs a token gets it from the secret environment the user gives threads in tm's settings; if the task needs access you don't have, " + tell + " exactly what is missing."
	}
	// A thread never merges while the rule holds: no allowance lifts it.
	return &Denial{Rule: rule, Message: "terminatr guard (" + rule + "): " + m, Summary: summary, Floor: rule == "merge"}
}

// folderForms are the forms a path may start with that name a folder
// the line has been in, by kind: cur, the line's folder ($PWD, ${PWD}
// and the expansions that keep its value, ${PWD:-x}, pwd substituted,
// ~+, ~0, ~+0); stack, the folder stack's index 0, the line's folder in
// bash and empty in zsh ($DIRSTACK, ${DIRSTACK[0]}); bottom and
// bottomStack, the stack's bottom entry (~-0, ${DIRSTACK[-1]}); old, the
// folder before (~-, $OLDPWD and the like); unknown, any other index
// (~1, ~-2, ${DIRSTACK[1]}), a listing (${DIRSTACK[@]}, dirs
// substituted), or $PWD, $OLDPWD or $DIRSTACK changed by an operator
// (${PWD%/*}). zsh's $dirstack is read as $DIRSTACK, in every form. A
// tilde form ends at a / or the path's end.
var folderForms = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"cur", regexp.MustCompile("^(?:\\$PWD\\b|\\$\\{PWD(?::?[-=?][^}]*)?\\}|\\$\\(\\s*pwd(?:\\s+-[LP]+)*\\s*\\)|`\\s*pwd(?:\\s+-[LP]+)*\\s*`|~(?:\\+0?|0)(?:/|$))")},
	{"stack", regexp.MustCompile(`^(?:\$(?:DIRSTACK|dirstack)\b|\$\{(?:DIRSTACK|dirstack)(?:\[0\])?\})`)},
	{"bottom", regexp.MustCompile(`^~-0(?:/|$)`)},
	{"bottomStack", regexp.MustCompile(`^\$\{(?:DIRSTACK|dirstack)\[-1\]\}`)},
	{"old", regexp.MustCompile(`^(?:\$OLDPWD\b|\$\{OLDPWD(?::?[-=?][^}]*)?\}|~-(?:/|$))`)},
	{"unknown", regexp.MustCompile("^(?:~[+-]?[0-9]+(?:/|$)|\\$\\{[#!]?(?:PWD|OLDPWD|DIRSTACK|dirstack)\\b[^}]*\\}|\\$\\(\\s*dirs\\b[^)]*\\)|`\\s*dirs\\b[^`]*`)")},
}

// folderStart: path w starts with a form of a folder the line has been
// in (folderForms): which kind ("" none), and how long the form is.
func folderStart(w string) (string, int) {
	for _, f := range folderForms {
		if m := f.re.FindString(w); m != "" {
			return f.kind, len(strings.TrimSuffix(m, "/"))
		}
	}
	return "", 0
}

// zshStack is zsh's $dirstack starting a path.
var zshStack = regexp.MustCompile(`^\$\{?dirstack`)

// defaultName is the ${x:-, ${x-, ${x:=, ${x=, ${x:+ or ${x+ a path
// starts with.
var defaultName = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*:?[-=+]`)

// defaultWord is what path w gives when the ${x:-word}, ${x-word},
// ${x:=word}, ${x=word}, ${x:+word} or ${x+word} it starts with gives
// its word: the word, quotes removed, then the rest of w; false for
// none.
func defaultWord(w string) (string, bool) {
	m := defaultName.FindString(w)
	if m == "" {
		return "", false
	}
	depth := 1
	for i := len(m); i < len(w); i++ {
		switch {
		case w[i] == '\\':
			i++
		case w[i] == '$' && i+1 < len(w) && w[i+1] == '{':
			depth++
			i++
		case w[i] == '}':
			if depth--; depth == 0 {
				return strings.NewReplacer(`"`, "", "'", "").Replace(w[len(m):i]) + w[i+1:], true
			}
		}
	}
	return "", false
}

// pushes is a pushd or popd word on a line.
var pushes = regexp.MustCompile("(?m)(^|[\\s;&|(){}!`])(pushd|popd)([\\s;&|)`]|$)")

// parent is a path with a .. in it (../x, a/../b, ..).
var parent = regexp.MustCompile(`(^|/)\.\.(/|$)`)

// dotted is a path from . or .. (./x, ../x), which CDPATH doesn't look up.
var dotted = regexp.MustCompile(`^\.{0,2}(/|$)`)

// paramName is the name a path starts with as $NAME or ${NAME….
var paramName = regexp.MustCompile(`^\$(?:\{([A-Za-z_][A-Za-z0-9_]*)|([A-Za-z_][A-Za-z0-9_]*))`)

// lineFolder: path w starts with a folder that a cd on line can change
// and folderStart doesn't place: $_ (the last word of the command
// before: the cd's folder), a substitution, a positional or special
// parameter ($1, $@, ${!x}), or a variable the line may set (assigns).
func lineFolder(w, line string) bool {
	if strings.HasPrefix(w, "`") || strings.HasPrefix(w, "$(") {
		return true
	}
	if !strings.HasPrefix(w, "$") {
		return false
	}
	m := paramName.FindStringSubmatch(w)
	if m == nil {
		return true
	}
	name := m[1] + m[2]
	return name == "_" || assigns(line, name)
}

// assigns: line names variable name other than to expand it, as an
// assignment (d=…, d+=…, d[0]=…), an expansion that assigns (${d:=…},
// ${d=…}) or a word (read d, for d in, export d, printf -v d) does.
func assigns(line, name string) bool {
	return regexp.MustCompile("(?m)(^|[\\s;&|(){}!`\"'])" + name + "(\\[|:?=|\\+=|[\\s;&|)`\"']|$)").MatchString(line)
}

// bareParam is a bare $NAME or ${NAME} a path starts with.
var bareParam = regexp.MustCompile(`^\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)

// placedParam is a variable a path may start with that placedVar may
// let through: $NAME, ${NAME} or ${NAME:-/abs}, then a /.
var placedParam = regexp.MustCompile(`^\$(?:\{([A-Za-z_][A-Za-z0-9_]*)(:-/[^$` + "`" + `{}'"\\]*)?\}|([A-Za-z_][A-Za-z0-9_]*))/`)

// placedVar: path w starts with a variable whose value a cd doesn't
// move, then a /: $TMPDIR, ${TMPDIR} or ${TMPDIR:-/abs}, or another bare
// $NAME or ${NAME}, that the line doesn't set (assigns). Any other
// expansion (${D:-.}, ${D#/}, ${!D}) may be a relative folder, and a
// variable glued to text ($x.ext) another name.
func placedVar(w, line string) bool {
	m := placedParam.FindStringSubmatch(w)
	if m == nil {
		return false
	}
	name := m[1] + m[3]
	return (m[2] == "" || name == "TMPDIR") && name != "_" && !assigns(line, name)
}

// unplaced is the tm-files refusal of a path from a folder the guard
// can't place (folderStart, lineFolder): its sentence says how to write
// it.
func (r Rules) unplaced(summary string) *Denial {
	tell := "ask the user"
	if r.Role == "thread" {
		tell = "say so in your report"
	}
	m := "a path given to a command that writes that starts with a folder the guard can't place ($OLDPWD or ~- from before the line, $PWD changed by an operator, or after a cd $_, a substitution, a positional parameter or a variable the line sets) could be tm's settings or the project folder. Give the path from /, from a variable the line doesn't set ($TMPDIR/x), or relative after a cd to a folder written out; if none of that fits, " + tell + "."
	return &Denial{Rule: RuleTMFiles, Message: "terminatr guard (" + RuleTMFiles + "): " + m, Summary: summary}
}

// lostCd is the tm-files refusal of a relative path after a cd the
// guard can't follow: its sentence says how to write the line so it can.
func (r Rules) lostCd(summary string) *Denial {
	tell := "ask the user"
	if r.Role == "thread" {
		tell = "say so in your report"
	}
	m := "after a cd the guard can't follow (to a variable or a substitution, cd -, popd, or one inside an if or a loop), a relative path given to a command that writes could be tm's settings or the project folder. Give the path from / or a variable ($TMPDIR/x), cd to a folder written out and join the rest with &&, or run the cd in a call of its own; if none of that fits, " + tell + "."
	return &Denial{Rule: RuleTMFiles, Message: "terminatr guard (" + RuleTMFiles + "): " + m, Summary: summary}
}

// JudgeCommand is the rule a command line breaks as a Bash call would,
// the allowances lifting what they name, or nil: what tm runs for a
// thread outside its sandbox (an approved command, its words quoted)
// is judged as the thread's own call.
func (r Rules) JudgeCommand(line string) *Denial {
	if !r.On {
		return nil
	}
	line = strings.TrimSpace(line)
	if len(line) > MaxCommand || !readable(line) {
		return unreadable()
	}
	if r.ps == nil {
		r.ps = &parse{limit: judgeParse, deadline: r.Deadline}
	}
	return r.ps.verdict(r.lifted(line, false, func(r Rules) *Denial { return r.judgeBash(line) }))
}

// judgeBash checks each simple command of a Bash command line.
func (r Rules) judgeBash(command string) *Denial {
	r.wt = r.worktreeOf()
	if r.ps != nil {
		r.ps.traps = nil
	}
	return r.judgeLine(command, []string{r.Cwd})
}

// worktreeOf is the thread's worktree: its first writable folder, or
// the session's folder, where a thread starts; "" for a coordinator.
func (r Rules) worktreeOf() string {
	switch {
	case r.Role != "thread":
		return ""
	case len(r.Writable) > 0:
		return r.Writable[0]
	}
	return r.Cwd
}

// judgeLine judges each simple command of a Bash command line with its
// relative paths from each folder of cwds, as the floor sees them: ""
// is one it can't tell, which refuses a relative path a writer is
// given. A cd (or pushd, popd) moves them only where it is a plain
// command of the line's top level: joined by && it moves them for what
// follows in its list, joined by ; or || it adds a folder they may be
// in (it may fail, or not run); in a pipeline or the background it
// moves nothing, and anywhere else (if, a loop, a substitution) it
// leaves them unknown. A part's ( ) or { } (linePart's group) is a line
// of its own, judged from the part's folders: a cd in it moves them for
// the rest of the group, and a { } run in the line's own shell, with
// nothing before it, leaves the line in the folders the group may end
// in; a ( ) moves nothing after it. A part that runs shell code the
// line doesn't show (eval, source, ., a trap whose action may move them,
// also through command or builtin) may move them at any point after, so
// the rest of the line keeps unknown among them.
//
// A trap's action (judgeCommands keeps it in ps.traps, as set here or in
// a line this one reads: eval, bash -c) may run at any point from the
// trap on (a signal) and at the end (EXIT), so it is judged as eval's
// arguments are, from each folder the line may be in from the trap on
// and those it may end in.
func (r Rules) judgeLine(command string, cwds []string) *Denial {
	d, _, _ := r.walkLine(command, cwds)
	return d
}

// walkLine is judgeLine, and the folders the line may end in and those
// $OLDPWD may then hold.
func (r Rules) walkLine(command string, cwds []string) (d *Denial, end, endOlds []string) {
	if r.line != "" {
		r.line += "\n"
	}
	r.line += command
	r.lineCd = r.lineCd || r.hasCd(command)
	r.physical = r.physical || physicalCd.MatchString(command)
	parts, ok := r.topLevel(command)
	if !ok {
		// A line it can't split: one part, with any cd in it leaving its
		// relative paths unknown from the start.
		parts = []linePart{{text: command}}
		if r.hasCd(command) {
			cwds = withDirs(cwds, "")
		}
	}
	cur, list := cwds, cwds
	hidden := false
	// The folders $OLDPWD may hold: before the line, one it can't tell.
	olds := r.olds
	if olds == nil {
		olds = []string{""}
	}
	// The trap actions set from here, and the folders the line may be
	// in from the first on.
	from := 0
	if r.ps != nil && !r.sub {
		from = len(r.ps.traps)
	}
	r.sub = false
	trapped := func() bool { return r.ps != nil && len(r.ps.traps) > from }
	var trapDirs []string
	for i, pt := range parts {
		// The folders a part may run in count against the budget, and
		// a line that may be in more than maxFolders is past it.
		if r.ps != nil && (len(cur)+len(list)+len(olds) > maxFolders || r.ps.stop(len(cur)*len(pt.text), r.depth)) {
			r.ps.over = true
		}
		if r.spent() {
			return nil, nil, nil // Judge refuses the call: no part is let through unread
		}
		if listEnd(pt.before) {
			list = cur
		}
		// A heredoc's body read as commands (topLevel and Commands may
		// tell a << apart differently: $[1<<2]) may hold a cd anywhere.
		if pt.doc && r.hasCd(pt.text) {
			olds = withDirs(withDirs(olds, cur...), "")
			cur = withDirs(cur, "")
		}
		rp := r
		rp.olds = olds
		rp.trapFrom = from
		for _, c := range cur {
			if d := rp.judgeCommands(pt.text, c); d != nil || r.spent() {
				return d, nil, nil
			}
		}
		// Its group's own commands, from the part's folders on, and
		// those of each substitution it runs in a subshell (the group's
		// own walk reads those in it), which moves nothing after.
		var gEnd, gOlds []string
		subs := pt.text
		if g := pt.group; g != nil {
			subs = g.lead + " " + g.tail
			rg := rp.nested()
			rg.sub = true
			if d, gEnd, gOlds = rg.walkLine(g.inner, cur); d != nil || r.spent() {
				return d, nil, nil
			}
		}
		for _, in := range r.subsOf(subs) {
			rs := rp.nested()
			rs.sub = true
			if d, _, _ := rs.walkLine(in, cur); d != nil || r.spent() {
				return d, nil, nil
			}
		}
		if trapped() {
			trapDirs = withDirs(trapDirs, cur...)
		}
		inPipe := pt.before == "|" || pt.after == "|" || pt.after == "&"
		if g := pt.group; g != nil && g.brace && g.lead == "" && !inPipe && r.hasCd(pt.text) {
			// A { } in the line's own shell: where it may end, whether
			// it fails or not.
			olds, cur = gOlds, gEnd
			if r.hasCd(g.tail) {
				olds = withDirs(withDirs(olds, cur...), "")
				cur = withDirs(cur, "")
			}
		} else if cmd, args, plain := r.plainCd(pt.text); plain && !inPipe {
			var to []string
			for _, c := range cur {
				to = withDirs(to, nowhere{dir: c}.cd(r, cmd, args).dir)
			}
			// The cd leaves $OLDPWD the folder it left, if it runs.
			sure := pt.after == "&&" && pt.before != "||"
			// A cd .. that always runs (the first command of its list,
			// none after it on ||) fails only where the parent is gone:
			// the folders the line may be in merge into their parents, so
			// a script that walks its subfolders in turn (cd X || exit 1;
			// …; cd ..) doesn't gather a folder for each. Not in a list
			// run in the background, which moves no folder of the line's
			// own shell. A cd X || exit merges nothing: an earlier member
			// of its list may have ended it, and the shell may have made
			// exit something else where the guard can't see (Claude's
			// Bash sources a snapshot of the user's shell first).
			up := cmd == "cd" && len(args) == 1 && (args[0] == ".." || args[0] == "../") &&
				listEnd(pt.before) && pt.after != "||" && listSep(parts, i) != "&"
			if sure || up {
				olds = slices.Clone(cur)
			} else {
				olds = withDirs(olds, cur...)
			}
			switch {
			case up:
				cur, list = to, to
			case sure:
				cur = to
			case pt.after == "||":
				list = withDirs(list, to...)
			default:
				cur = withDirs(cur, to...)
			}
		} else if !inPipe && (pt.group == nil || pt.group.brace || pt.group.lead != "") && r.hasCd(pt.text) {
			olds = withDirs(withDirs(olds, cur...), "")
			cur = withDirs(cur, "")
		}
		hidden = hidden || r.runsHidden(pt.text)
		if hidden {
			olds = withDirs(olds, "")
			cur = withDirs(cur, "")
		}
		list = withDirs(list, cur...)
		if listEnd(pt.after) {
			cur = list
		}
	}
	if !trapped() {
		return nil, cur, olds
	}
	// The actions read here may set traps of their own, which their own
	// reading judges.
	rt := r.nested()
	rt.olds = withDirs(olds, "")
	for _, a := range slices.Clone(r.ps.traps[from:]) {
		if d := rt.judgeLine(a, withDirs(trapDirs, cur...)); d != nil || r.spent() {
			return d, nil, nil
		}
	}
	return nil, cur, olds
}

// listSep is the separator that ends the and-or list part i is in: ";",
// "&" (run in the background), or "" at the line's end.
func listSep(parts []linePart, i int) string {
	for ; i < len(parts); i++ {
		if listEnd(parts[i].after) {
			return parts[i].after
		}
	}
	return ""
}

// hiddenCode are the builtins that run shell code the line doesn't show
// in the shell itself, where a cd in it moves the line's folder.
var hiddenCode = setOf("eval source .")

// runsHidden: one of text's commands is one of those, maybe through
// command or builtin, or a trap whose action may move the line's folder
// (a cd in it, hidden code, or an action the guard can't read).
func (r Rules) runsHidden(text string) bool {
	for _, words := range r.cmds(text) {
		argv := strip(words)
		for len(argv) > 1 && (argv[0] == "command" || argv[0] == "builtin") {
			argv = argv[1:]
		}
		if len(argv) == 0 {
			continue
		}
		if hiddenCode[argv[0]] {
			return true
		}
		if argv[0] == "trap" {
			a, ok := r.trapAction(argv[1:])
			if !ok || a != "" && (r.hasCd(a) || r.nested().runsHidden(a)) {
				return true
			}
		}
	}
	return false
}

// trapAction is the action trap's args set ("" for none: a listing, a
// reset, a signal ignored); false for one the guard can't read, whose
// commands a variable or a substitution names ("$cleanup", $(cat f)).
// The first word after the options is the action, as bash reads it with
// a signal after it; with none (trap 2, bash's reset) it is read too,
// which judges no command.
func (r Rules) trapAction(args []string) (string, bool) {
	i := 0
	for ; i < len(args) && strings.HasPrefix(args[i], "-") && args[i] != "-"; i++ {
		if args[i] == "--" {
			i++
			break
		}
		if strings.ContainsAny(args[i], "lp") {
			return "", true // trap -l, trap -p: a listing
		}
	}
	if i >= len(args) || args[i] == "-" {
		return "", true
	}
	a := args[i]
	for _, words := range r.cmds(a) {
		if argv := strip(words); len(argv) > 0 && (strings.HasPrefix(argv[0], "$") || strings.Contains(argv[0], "`")) {
			return a, false
		}
	}
	return a, true
}

// unreadableTrap is the refusal of a trap whose action the guard can't
// read (trapAction).
func unreadableTrap() *Denial {
	return &Denial{Rule: RuleUnreadable, Floor: true, Summary: "a trap action the guard can't read",
		Message: "terminatr guard: this trap's action runs a command a variable or a substitution names, which the guard can't read, so it is refused; write the action's commands out in the trap."}
}

// judgeCommands judges each simple command of part, a part of a line
// (topLevel), with its relative paths from cwd ("" unknown: the floor
// refuses them, the other rules take the session's folder). After a
// command that ends in a redirect (>| f, tee >(…) f: the part splits
// there), a command's first word may be what it writes.
func (r Rules) judgeCommands(part, cwd string) *Denial {
	fl := r
	fl.Cwd = cwd
	if cwd != "" {
		r.Cwd = cwd
	}
	split := false
	var read [][]string
	if r.ps != nil {
		read = r.ps.read(part, false, func(p string) bool { return fl.worktreeScript(p) }, r.depth)
	} else {
		read = readCommands(part, false, func(p string) bool { return fl.worktreeScript(p) })
	}
	for _, words := range read {
		argv := strip(words)
		// Only a word that looks like a path: 2>&1 splits the same way.
		if split && len(words) > 0 && (strings.ContainsAny(words[0], `/\`) || strings.HasPrefix(words[0], "~")) {
			if d := fl.judgeNoWrite(">", []string{words[0]}, []string{">", words[0]}); d != nil {
				return d
			}
		}
		split = split || len(words) > 0 && words[len(words)-1] == ">"
		if len(argv) == 0 {
			// A redirect alone (date &> f: the & ends the command).
			if d := fl.judgeNoWrite(">", nil, words); d != nil {
				return d
			}
			continue
		}
		cmd := base(argv[0])
		args := argv[1:]
		if d := r.judgeMods(cmd, args, false); d != nil {
			return d
		}
		if d := r.judgeGitConfig(cmd, words); d != nil {
			return d
		}
		if d := fl.judgeNoWrite(cmd, args, words); d != nil {
			return d
		}
		var d *Denial
		switch {
		case cmd == "git":
			d = r.judgeGit(args)
		case cmd == "gh":
			d = r.judgeGh(args)
		case cmd == "az":
			d = r.judgeAz(args)
		case httpClients[cmd]:
			d = r.judgeHTTP(cmd, args)
		case shells[cmd]:
			// bash -c STRING runs the string: read one level deeper.
			if c, ok := shellCommand(args); ok {
				d = r.nested().judgeLine(c, []string{cwd})
			}
		case cmd == "eval":
			d = r.nested().judgeLine(strings.Join(args, " "), []string{cwd})
		case cmd == "trap":
			// Its action runs later: judgeLine judges it where it may.
			if a, ok := r.trapAction(args); !ok {
				d = unreadableTrap()
			} else if a != "" && r.ps != nil && !slices.Contains(r.ps.traps[r.trapFrom:], a) {
				r.ps.traps = append(r.ps.traps, a)
			}
		case cmd == "pwsh" || cmd == "powershell":
			d = r.judgePwsh(args)
		case cmd == "rm" && r.has("delete-branch") && slices.ContainsFunc(args, func(a string) bool { return rmRecursive.MatchString(a) || isLong(a, "--recursive") }):
			for _, a := range args {
				if strings.HasPrefix(a, "-") {
					continue
				}
				if r.isWorktreeRoot(r.abs(a)) {
					d = r.deny("delete-branch", "rm -r of "+safe(r.abs(a)), "")
					break
				}
			}
		}
		if d != nil {
			return d
		}
		if r.guardsSecrets() {
			if d := r.judgeSecret(cmd, args, words, readers[cmd], printers[cmd]); d != nil {
				return d
			}
		}
	}
	return nil
}

// guardsSecrets: credentials is on, or the floor keeps tm's files or
// the session's secret variables.
func (r Rules) guardsSecrets() bool {
	return r.has("credentials") || len(r.Own) > 0 || len(r.SecretEnv) > 0
}

var (
	rmRecursive = regexp.MustCompile(`^-[a-zA-Z]*[rR]`)
	// A bundle of short options may hold digits (git push -4f: -4 is
	// --ipv4).
	shortFlags  = regexp.MustCompile(`^-[a-zA-Z0-9]+$`)
	branchUpper = regexp.MustCompile(`^-[a-zA-Z0-9]*D`)
)

// shells run a command line given with -c; stdinShells (them and more)
// run the commands a heredoc gives them.
var (
	shells      = setOf("bash sh zsh dash ksh mksh ash")
	stdinShells = setOf("bash sh zsh dash ksh mksh ash fish csh tcsh pwsh powershell source . eval xargs ssh sudo doas su")
)

func (r Rules) judgeGit(args []string) *Denial {
	// Global options before the subcommand.
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace":
			i++
		}
		i++
	}
	sub := at(args, i)
	var rest []string
	if i+1 < len(args) {
		rest = args[i+1:]
	}
	// long: one of the long options, with or without a value, or its
	// abbreviation; flag: that or the short one, alone or among others
	// (-fu).
	long := func(names ...string) bool {
		return slices.ContainsFunc(rest, func(a string) bool {
			return slices.ContainsFunc(names, func(l string) bool { return isLong(a, l) })
		})
	}
	flag := func(short string, names ...string) bool {
		return long(names...) || slices.ContainsFunc(rest, func(a string) bool { return shortFlags.MatchString(a) && strings.Contains(a, short) })
	}
	switch sub {
	case "push":
		if flag("f", "--force", "--force-with-lease", "--force-if-includes", "--mirror") || slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "+") }) {
			// Every branch, or a protected one: never allowed, the rule
			// off too.
			onFloor := slices.Contains(r.Floor, "force-push") || r.has("force-push")
			if onFloor && (long("--all", "--branches", "--mirror") || slices.ContainsFunc(r.pushTargets(rest), func(b string) bool { return slices.Contains(r.Protected, b) })) {
				return floor(r.deny("force-push", "git push with force", ""))
			}
			if r.has("force-push") {
				return r.deny("force-push", "git push with force", "")
			}
		}
		if r.has("delete-branch") && (flag("d", "--delete", "--prune") || slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, ":") && len(a) > 1 })) {
			return r.deny("delete-branch", "git push deleting a branch", "")
		}
		if r.has("push-default") {
			if long("--all", "--branches", "--mirror") {
				return r.deny("push-default", "git push --all", "")
			}
			for _, target := range r.pushTargets(rest) {
				if slices.Contains(r.Protected, target) {
					return r.deny("push-default", "git push to "+safe(target), target)
				}
			}
		}
	case "branch":
		if r.has("delete-branch") && flag("d", "--delete") {
			return r.deny("delete-branch", "git branch --delete", "")
		}
		if r.has("delete-branch") && slices.ContainsFunc(rest, branchUpper.MatchString) {
			return r.deny("delete-branch", "git branch -D", "")
		}
	case "worktree":
		if w := at(rest, 0); r.has("delete-branch") && (w == "remove" || w == "prune") {
			return r.deny("delete-branch", "git worktree "+w, "")
		}
	case "update-ref":
		if r.has("delete-branch") && flag("d", "--delete") {
			return r.deny("delete-branch", "git update-ref -d", "")
		}
	case "credential":
		if r.has("credentials") && at(rest, 0) == "fill" {
			return r.deny("credentials", "git credential fill", "")
		}
	}
	return nil
}

// pushTargets are the branches git push's arguments (after push) name
// as where they push: each refspec's destination.
func (r Rules) pushTargets(rest []string) []string {
	var out []string
	pos := positionals(rest, []string{"-o", "--push-option", "--repo", "--receive-pack", "--exec"})
	for j := 1; j < len(pos); j++ {
		spec := strings.TrimPrefix(pos[j], "+")
		if k := strings.Index(spec, ":"); k >= 0 {
			spec = spec[k+1:]
		}
		if target := branchOf(spec); target != "" {
			out = append(out, target)
		}
	}
	return out
}

var (
	ghMerge     = regexp.MustCompile(`pulls/[^/]+/merge/?$`)
	ghRefs      = regexp.MustCompile(`git/refs/heads/`)
	ghMethodOpt = regexp.MustCompile(`^(-X|--method=)`)
	// The GraphQL mutations that merge a PR, and that delete a branch.
	ghMergeMutation  = regexp.MustCompile(`(?i)mergePullRequest|enablePullRequestAutoMerge|enqueuePullRequest`)
	ghDeleteMutation = regexp.MustCompile(`(?i)deleteRef`)
)

// ghValued are gh's options that take a value, which may come before or
// between the command words (gh pr -R o/r merge).
var ghValued = map[string]bool{"-R": true, "--repo": true, "--hostname": true}

func (r Rules) judgeGh(args []string) *Denial {
	// The command words, flags among them aside.
	var words []string
	api := -1
	for i := 0; i < len(args) && len(words) < 2; i++ {
		if strings.HasPrefix(args[i], "-") {
			if ghValued[args[i]] {
				i++
			}
			continue
		}
		if len(words) == 0 && args[i] == "api" {
			api = i
		}
		words = append(words, args[i])
	}
	a, b := at(words, 0), at(words, 1)
	if a == "pr" && b == "merge" && r.has("merge") {
		return r.deny("merge", "gh pr merge", "")
	}
	if a == "repo" && b == "delete" && r.has("delete-branch") {
		return r.deny("delete-branch", "gh repo delete", "")
	}
	if a == "auth" && r.has("credentials") && (b == "token" || slices.Contains(args, "--show-token") || slices.Contains(args, "-t")) {
		if b == "token" {
			return r.deny("credentials", "gh auth token", "")
		}
		return r.deny("credentials", "gh auth status --show-token", "")
	}
	if a == "api" {
		// Each path without its query or fragment, which GitHub ignores.
		var paths []string
		for _, x := range args[api+1:] {
			if !strings.HasPrefix(x, "-") {
				paths = append(paths, routeSplit.Split(x, 2)[0])
			}
		}
		if r.has("merge") && slices.ContainsFunc(paths, ghMerge.MatchString) {
			return r.deny("merge", "gh api merging a PR", "")
		}
		if slices.ContainsFunc(paths, func(p string) bool { return strings.Trim(p, "/") == "graphql" }) {
			if r.has("merge") && slices.ContainsFunc(args, ghMergeMutation.MatchString) {
				return r.deny("merge", "gh api graphql merging a PR", "")
			}
			if r.has("delete-branch") && slices.ContainsFunc(args, ghDeleteMutation.MatchString) {
				return r.deny("delete-branch", "gh api graphql deleting a branch", "")
			}
		}
		method, found := "", false
		for i := 1; i < len(args); i++ {
			if args[i-1] == "-X" || args[i-1] == "--method" {
				method, found = strings.ToUpper(args[i]), true
				break
			}
		}
		if !found {
			for _, x := range args {
				if ghMethodOpt.MatchString(x) && len(x) > 2 {
					method = strings.ToUpper(ghMethodOpt.ReplaceAllString(x, ""))
					break
				}
			}
		}
		if r.has("delete-branch") && method == "DELETE" && slices.ContainsFunc(paths, ghRefs.MatchString) {
			return r.deny("delete-branch", "gh api deleting a branch", "")
		}
	}
	return nil
}

var methodOverride = regexp.MustCompile(`(?i)^x-http-method-override[=:]\s*(\w+)`)

// judgeAz judges az (the Azure CLI with its azure-devops extension) like
// gh, with the same rules and sentences. The extension's verbs:
// completing a PR (`az repos pr update --status completed`,
// `--auto-complete`, `--bypass-policy`, also on create) is the merge;
// `--delete-source-branch`, `az repos delete` and `az repos ref delete`
// delete; the REST routes under az rest, az devops invoke and curl are
// held to the same, by method and route.
func (r Rules) judgeAz(args []string) *Denial {
	// The command words come first; flags (any order, --x=v or --x v) after.
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		if azValued[args[i]] {
			i += 2
		} else {
			i++
		}
	}
	var words []string
	for ; i < len(args) && !strings.HasPrefix(args[i], "-"); i++ {
		words = append(words, strings.ToLower(args[i]))
	}
	var rest []string
	if i < len(args) {
		rest = args[i:]
	}
	a, b, c := at(words, 0), at(words, 1), at(words, 2)
	if a == "repos" && b == "pr" && (c == "update" || c == "create") {
		completes := slices.ContainsFunc(opts(rest, "--status"), func(v string) bool { return strings.ToLower(v) == "completed" }) ||
			truthy(rest, "--auto-complete") || truthy(rest, "--bypass-policy")
		if r.has("merge") && completes {
			return r.deny("merge", "az repos pr "+c+" completing a PR", "")
		}
		if r.has("delete-branch") && truthy(rest, "--delete-source-branch") {
			return r.deny("delete-branch", "az repos pr "+c+" --delete-source-branch", "")
		}
	}
	if a == "repos" && b == "delete" && r.has("delete-branch") {
		return r.deny("delete-branch", "az repos delete", "")
	}
	if a == "repos" && b == "ref" && c == "delete" && r.has("delete-branch") {
		return r.deny("delete-branch", "az repos ref delete", "")
	}
	if r.has("credentials") {
		if a == "account" && b == "get-access-token" {
			return r.deny("credentials", "az account get-access-token", "")
		}
		if a == "devops" && b == "login" {
			return r.deny("credentials", "az devops login", "")
		}
	}
	if a == "rest" {
		// A method override header is the method the service acts on.
		method := ""
		for _, h := range optsMulti(rest, "--headers") {
			if m := methodOverride.FindStringSubmatch(h); m != nil {
				method = m[1]
				break
			}
		}
		if method == "" {
			method = first(opts(rest, "--method", "-m"), "GET")
		}
		return r.judgeRoute(method, first(opts(rest, "--url", "--uri", "-u"), ""))
	}
	if a == "devops" && b == "invoke" {
		resource := strings.ToLower(first(opts(rest, "--resource"), ""))
		method := first(opts(rest, "--http-method"), "GET")
		var params []string
		for _, p := range optsMulti(rest, "--route-parameters") {
			params = append(params, strings.ToLower(p))
		}
		area := strings.ToLower(first(opts(rest, "--area"), "git"))
		if area != "git" || !isWrite(method) {
			return nil
		}
		if r.has("merge") && (resource == "merges" || (resource == "pullrequests" && slices.ContainsFunc(params, func(p string) bool { return strings.HasPrefix(p, "pullrequestid=") }))) {
			return r.deny("merge", "az devops invoke "+safe(strings.ToUpper(method))+" on "+safe(resource), "")
		}
		if r.has("delete-branch") && (resource == "refs" || (resource == "repositories" && strings.ToUpper(method) == "DELETE")) {
			return r.deny("delete-branch", "az devops invoke "+safe(strings.ToUpper(method))+" on "+safe(resource), "")
		}
	}
	return nil
}

// httpClients speak HTTP; given an Azure DevOps address they are judged
// by route and method as az rest is, and by what they authenticate with.
var httpClients = map[string]bool{"curl": true, "wget": true, "http": true, "https": true, "xh": true}

var (
	azureHost = regexp.MustCompile(`(?i)(^|[/@.])(dev\.azure\.com|[a-z0-9-]+\.visualstudio\.com|vssps\.dev\.azure\.com)([/:?#]|$)`)
	httpUser  = regexp.MustCompile(`^(-u.*|--user(=.*)?|--oauth2-bearer(=.*)?)$`)
	httpAuth  = regexp.MustCompile(`(?i)authorization:|AZURE_DEVOPS_EXT_PAT`)
	shortX    = regexp.MustCompile(`^-X.`)
)

func (r Rules) judgeHTTP(cmd string, args []string) *Denial {
	i := slices.IndexFunc(args, azureHost.MatchString)
	if i < 0 {
		return nil
	}
	url := args[i]
	if r.has("credentials") && slices.ContainsFunc(args, func(a string) bool { return httpUser.MatchString(a) || httpAuth.MatchString(a) }) {
		return r.deny("credentials", "an HTTP call to Azure DevOps with credentials", "")
	}
	return r.judgeRoute(httpMethod(cmd, args, i), url)
}

var (
	// curlValued are curl's short options that take a value.
	curlValued = "AbcCdDeEFHKmoPQrtTuUwxXyYz"
	// httpMethods are the methods HTTPie and xh take as a word before
	// the URL.
	httpMethods = setOf("GET POST PUT PATCH DELETE HEAD OPTIONS")
	// httpieData is an HTTPie request item that sends data (a=b, a:=1,
	// a@file), not a query (a==b) or a header (A:b).
	httpieData = regexp.MustCompile(`^[^=:@]+(?:=[^=]|=$|:=|@)`)
)

// httpMethod is the method an HTTP client's arguments send, the URL at
// args[url]: the one they name, else what the data they send makes it
// (curl -d is a POST, -T a PUT), else GET.
func httpMethod(cmd string, args []string, url int) string {
	method, implied := "", ""
	imply := func(m string) {
		if implied != "PUT" {
			implied = m
		}
	}
	for i, a := range args {
		switch {
		case a == "-X" || a == "--request":
			method = at(args, i+1)
		case strings.HasPrefix(a, "--request="):
			method = a[len("--request="):]
		case shortX.MatchString(a):
			method = a[2:]
		}
	}
	switch cmd {
	case "curl":
		get := false
		for i := 0; i < len(args); i++ {
			a := args[i]
			if name, val, eq := strings.Cut(a, "="); strings.HasPrefix(a, "--") {
				// curl takes its long options whole.
				switch {
				case name == "--request":
					if !eq {
						i++
						val = at(args, i)
					}
					method = val
				case name == "--get":
					get = true
				case name == "--upload-file":
					imply("PUT")
				case strings.HasPrefix(name, "--data") || name == "--json" || name == "--form" || name == "--form-string":
					imply("POST")
				}
				continue
			}
			if !strings.HasPrefix(a, "-") {
				continue
			}
			// A bundle of short options (-sSd @b.json): the first that
			// takes a value takes the rest of the word, or the next one.
		bundle:
			for k, c := range a[1:] {
				rest := a[2+k:]
				switch {
				case c == 'G':
					get = true
					continue
				case c == 'd' || c == 'F':
					imply("POST")
				case c == 'T':
					imply("PUT")
				case !strings.ContainsRune(curlValued, c):
					continue
				}
				if rest == "" {
					i++
					rest = at(args, i)
				}
				if c == 'X' {
					method = rest
				}
				break bundle
			}
		}
		if get && implied == "POST" {
			implied = ""
		}
	case "wget":
		for i, a := range args {
			switch _, val, eq := strings.Cut(a, "="); {
			case isLong(a, "--method"):
				if !eq {
					val = at(args, i+1)
				}
				method = val
			case isLong(a, "--post-data"), isLong(a, "--post-file"):
				imply("POST")
			}
		}
	case "http", "https", "xh":
		if url > 0 && httpMethods[strings.ToUpper(args[url-1])] {
			method = args[url-1]
		}
		for _, a := range args {
			if a == "-f" || a == "--form" || a == "--multipart" || a == "--raw" || strings.HasPrefix(a, "--raw=") {
				imply("POST")
			}
		}
		for _, a := range args[url+1:] {
			if !strings.HasPrefix(a, "-") && httpieData.MatchString(a) {
				imply("POST")
			}
		}
	}
	switch {
	case method != "":
		return method
	case implied != "":
		return implied
	}
	return "GET"
}

var (
	routeSplit = regexp.MustCompile(`[?#]`)
	routePR    = regexp.MustCompile(`/pullrequests/[^/]+$`)
	routeMerge = regexp.MustCompile(`/merges$`)
	routeRefs  = regexp.MustCompile(`/refs$`)
	routeRepo  = regexp.MustCompile(`/repositories/[^/]+$`)
)

// judgeRoute: a write method on an Azure DevOps Git route that completes
// a PR or deletes a ref or a repository.
func (r Rules) judgeRoute(method, url string) *Denial {
	m := strings.ToUpper(method)
	if !isWrite(m) {
		return nil
	}
	path := strings.TrimRight(strings.ToLower(routeSplit.Split(url, 2)[0]), "/")
	if !strings.Contains(path, "/_apis/git/") {
		return nil
	}
	if r.has("merge") && (routePR.MatchString(path) || routeMerge.MatchString(path)) {
		return r.deny("merge", safe(m)+" on an Azure DevOps pull request", "")
	}
	if r.has("delete-branch") && (routeRefs.MatchString(path) || (m == "DELETE" && routeRepo.MatchString(path))) {
		return r.deny("delete-branch", safe(m)+" on an Azure DevOps ref or repository", "")
	}
	return nil
}

func isWrite(method string) bool {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS", "":
		return false
	}
	return true
}

// azValued are az's global options that take a value before the
// command words.
var azValued = map[string]bool{"--output": true, "-o": true, "--query": true, "--subscription": true,
	"--organization": true, "--org": true, "--project": true, "-p": true}

// opts are the values of the option under any of names, as --x v or
// --x=v.
func opts(args []string, names ...string) []string {
	var out []string
	for i, a := range args {
		for _, n := range names {
			if a == n {
				v := at(args, i+1)
				if i+1 >= len(args) || strings.HasPrefix(v, "-") {
					v = ""
				}
				out = append(out, v)
			} else if strings.HasPrefix(a, n+"=") {
				out = append(out, a[len(n)+1:])
			}
		}
	}
	return out
}

// optsMulti is every value after the option up to the next option, for
// those that take several (--route-parameters a=1 b=2).
func optsMulti(args []string, name string) []string {
	var out []string
	for i, a := range args {
		if a == name {
			for j := i + 1; j < len(args) && !strings.HasPrefix(args[j], "-"); j++ {
				out = append(out, args[j])
			}
		} else if strings.HasPrefix(a, name+"=") {
			out = append(out, a[len(name)+1:])
		}
	}
	return out
}

// truthy: a boolean option given true, or bare (az takes true/false,
// and a bare flag counts as given).
func truthy(args []string, name string) bool {
	return slices.ContainsFunc(opts(args, name), func(v string) bool {
		switch strings.ToLower(v) {
		case "false", "no", "n", "0", "off":
			return false
		}
		return true
	})
}

// readers print or copy what they are given; printers print their
// arguments.
var (
	readers  = map[string]bool{}
	printers = map[string]bool{"echo": true, "printf": true, "print": true}
	// envPrinters print the variables when given no names.
	envPrinters = setOf("export declare typeset")
)

func init() {
	for _, c := range strings.Fields(`cat head tail less more bat nl tac cp mv scp rsync base64 xxd od
		hexdump strings grep egrep fgrep rg ag awk sed cut sort uniq tar zip
		gzip openssl gpg jq yq diff cmp source . curl dd`) {
		readers[c] = true
	}
}

var (
	// secretName: environment variable names that look like they hold
	// a secret.
	secretName  = regexp.MustCompile(`(?i)token|secret|key|password|passwd|credential|(^|_)pat$`)
	securityCmd = regexp.MustCompile(`^(find-(generic|internet)-password|dump-keychain|export)$`)
	// shellVar: a variable, $NAME or ${NAME}, or PowerShell's $env:NAME.
	shellVar = regexp.MustCompile(`\$\{?(?:[Ee][Nn][Vv]:)?([A-Za-z_][A-Za-z0-9_]*)`)
	azurePAT = regexp.MustCompile(`(?i)^AZURE_DEVOPS_EXT_PAT$|^AZURE_DEVOPS_.*(PAT|TOKEN)$`)
)

// judgeSecret: cmd (a reader or a printer of what it is given) reading a
// secret or printing the environment. Under credentials, any secret;
// whatever the rules (the floor), tm's own files and the session's
// secret variables.
func (r Rules) judgeSecret(cmd string, args, words []string, reader, printer bool) *Denial {
	creds := r.has("credentials")
	// The whole environment printed: refused under credentials, and
	// always where it holds a secret variable of tm's.
	dump := func(what string) *Denial {
		if len(r.SecretEnv) > 0 {
			return floor(r.deny("credentials", what, ""))
		}
		if creds {
			return r.deny("credentials", what, "")
		}
		return nil
	}
	if cmd == "security" && creds && securityCmd.MatchString(at(args, 0)) {
		return r.deny("credentials", "security "+at(args, 0), "")
	}
	if cmd == "printenv" {
		// printenv NAME... prints only what it is asked for: fine unless a
		// name looks secret. With no name (or only substitutions) it prints
		// everything.
		var names []string
		for _, a := range unredirected(args) {
			if !strings.HasPrefix(a, "-") && !substituted(a) {
				names = append(names, a)
			}
		}
		if len(names) == 0 {
			if d := dump("printenv printing the environment"); d != nil {
				return d
			}
		}
		if i := slices.IndexFunc(names, r.isSecretEnv); i >= 0 {
			return floor(r.deny("credentials", "printenv "+safe(names[i]), ""))
		}
		if i := slices.IndexFunc(names, secretName.MatchString); i >= 0 && creds {
			return r.deny("credentials", "printenv "+safe(names[i]), "")
		}
	} else if cmd == "env" && !slices.ContainsFunc(unredirected(args), func(a string) bool { return !strings.HasPrefix(a, "-") && !substituted(a) }) {
		if d := dump("env printing the environment"); d != nil {
			return d
		}
	} else if envPrinters[cmd] && !slices.ContainsFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") || strings.ContainsAny(a, "fF") }) ||
		cmd == "set" && len(args) == 0 {
		// export, declare -x and the like with no names print the
		// variables (with -f, the functions); set alone prints them all.
		if d := dump(cmd + " printing the environment"); d != nil {
			return d
		}
	}
	// echo and the like given a secret variable ($AZURE_DEVOPS_EXT_PAT,
	// or one of tm's).
	if printer || reader {
		for _, w := range words {
			if d := r.printed(cmd+" of ", w); d != nil {
				return d
			}
		}
	}
	// A reader given a secret, or a secret redirected in.
	for i, w := range words {
		redirected := w == "<" || (i > 0 && words[i-1] == "<")
		path := w
		if strings.HasPrefix(w, "<") && len(w) > 1 {
			path = w[1:]
		}
		if !(reader || redirected) || strings.HasPrefix(path, "-") || path == "<" {
			continue
		}
		p := r.abs(path)
		switch {
		case r.covers(r.Own, p):
			return floor(r.deny("credentials", cmd+" of "+safe(p), ""))
		case procEnviron.MatchString(p):
			if d := dump(cmd + " of " + safe(p)); d != nil {
				return d
			}
		case creds && r.isSecret(p):
			return r.deny("credentials", cmd+" of "+safe(p), "")
		}
	}
	return nil
}

// procEnviron: a process's environment on Linux.
var procEnviron = regexp.MustCompile(`^/proc/[^/]+/environ$`)

// isSecretEnv: name is one of the session's secret variables (case
// aside: Windows ignores it).
func (r Rules) isSecretEnv(name string) bool {
	return slices.ContainsFunc(r.SecretEnv, func(s string) bool { return strings.EqualFold(s, name) })
}

// printed is the refusal of word w printing a secret variable, summary
// what then the variable: one of the session's secret variables (the
// floor), or under credentials Azure DevOps' token.
func (r Rules) printed(what, w string) *Denial {
	for _, m := range shellVar.FindAllStringSubmatch(w, -1) {
		if r.isSecretEnv(m[1]) {
			return floor(r.deny("credentials", what+safe(m[1]), ""))
		}
		if r.has("credentials") && azurePAT.MatchString(m[1]) {
			return r.deny("credentials", what+safe(m[1]), "")
		}
	}
	return nil
}

// unredirected is args without their redirections (> f, < f, << EOF).
func unredirected(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "<" || args[i] == ">" {
			for i+1 < len(args) && (args[i+1] == "<" || args[i+1] == ">") {
				i++
			}
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// substituted: word a is a substitution alone, $( ), backticks or a
// PowerShell expression, which may give no word at all.
func substituted(a string) bool {
	return strings.HasPrefix(a, "$(") || strings.HasPrefix(a, "`") || psExprArg(a)
}

// viewers are the commands that only show the files they are given.
var viewers = map[string]bool{}

func init() {
	for _, c := range strings.Fields(`cat head tail less more bat nl grep egrep fgrep rg ls stat file wc diff cmp`) {
		viewers[c] = true
	}
}

// judgeGitConfig: under worktree-only, cmd naming the main repo's git
// hooks, config or info other than to view them, or redirecting into
// them.
func (r Rules) judgeGitConfig(cmd string, words []string) *Denial {
	if !r.has("worktree-only") || len(r.GitConfig) == 0 {
		return nil
	}
	for i, w := range words {
		if w == "<" || w == ">" || strings.HasPrefix(w, "-") {
			continue
		}
		out := i > 0 && words[i-1] == ">"
		p := r.abs(w)
		if slices.ContainsFunc(r.GitConfig, func(g string) bool { return r.under(p, g) }) && (out || !viewers[cmd]) {
			return r.deny("worktree-only", cmd+" of "+safe(p), "")
		}
	}
	return nil
}

// writers are the commands that write the files they are given: all
// of them (true), or the last one, where the others are what is copied
// (false); inPlace those that write only with -i (sed -i, perl -pi).
// Of the paths given to removers (and to the others with -r: cp -r,
// chmod -R), a folder that holds a NoWrite path counts too: removing or
// moving ~/.terminatr takes the project folder with it.
var (
	writers = map[string]bool{"rm": true, "rmdir": true, "unlink": true, "shred": true, "touch": true, "mkdir": true,
		"truncate": true, "tee": true, "mv": true, "chmod": true, "chown": true, "chgrp": true,
		"cp": false, "install": false, "ln": false, "rsync": false}
	inPlace  = setOf("sed gsed perl ruby")
	removers = setOf("rm rmdir mv rsync")
)

// scriptCluster is a cluster of short options ending in the one that
// takes the script (perl -pe, sed -ne).
var scriptCluster = regexp.MustCompile(`^-[a-zA-Z]+$`)

// inPlaceFiles are the files cmd (inPlace) with args writes: its
// arguments but its options, the script -e gives (sed's -f too, with
// their long forms), and sed's first argument when no -e or -f gives
// the script (sed -i s/a/b/ f). An option it can't read keeps what
// follows among the files.
func inPlaceFiles(cmd string, args []string) []string {
	sed := cmd == "sed" || cmd == "gsed"
	var out []string
	script := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			out = append(out, args[i+1:]...)
			i = len(args)
		case a == "-e" || a == "-E" && !sed && cmd != "ruby" || sed && (a == "-f" || isLong(a, "--expression") || isLong(a, "--file")):
			script = true
			if !strings.Contains(a, "=") {
				i++
			}
		case scriptCluster.MatchString(a) && !strings.Contains(a, "i") && (strings.HasSuffix(a, "e") || !sed && strings.HasSuffix(a, "E")):
			// A cluster ending in the script's option.
			script = true
			i++
		case strings.HasPrefix(a, "-") && a != "-":
			if sed && (strings.HasPrefix(a, "-e") || strings.HasPrefix(a, "-f")) {
				script = true
			}
			// macOS's sed -i '' (no backup): '' is -i's.
			if sed && a == "-i" && i+1 < len(args) && args[i+1] == "" {
				i++
			}
		default:
			out = append(out, a)
		}
	}
	if sed && !script && len(out) > 0 {
		return out[1:]
	}
	return out
}

// tmAgentMod are the tm mod subcommands an agent may run; the others,
// tm project's tmProjectWrites (and repo add), tm access's
// tmAccessWrites and the dashboard write
// tm's settings in the tm process itself, the human's alone: an agent
// past its sandbox (yolo) that runs one with its session's variables
// unset and out of its session's process tree would pass for the human
// (internal/cli: Run, caller.Narrower), so the floor refuses them.
var (
	tmAgentMod      = setOf("list show validate new test logs examples host")
	tmProjectWrites = setOf("new remote activate deactivate pause resume archive unarchive delete rename mv link open")
	tmAccessWrites  = setOf("add rm edit drop undrop set reset")
)

// tmWritesSettings is which of those tm with args (after its name) is
// ("tm mod enable", "tm (the dashboard)"), or "".
func tmWritesSettings(args []string) string {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		if args[i] == "-h" || args[i] == "--help" {
			return ""
		}
		i++
	}
	if i == len(args) {
		return "tm (the dashboard)"
	}
	var pos []string
	for _, a := range args[i+1:] {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	switch {
	case len(pos) == 0:
	case args[i] == "mod" && !tmAgentMod[pos[0]], args[i] == "project" && tmProjectWrites[pos[0]]:
		return "tm " + args[i] + " " + safe(pos[0])
	case args[i] == "project" && (pos[0] == "repo" || pos[0] == "repos") && len(pos) > 1 && pos[1] == "add":
		return "tm project repo add"
	case args[i] == "access" && tmAccessWrites[pos[0]]:
		return "tm access " + pos[0]
	}
	return ""
}

// accessOpen reports whether tm with args (after its name) is a tm
// access write a coordinator with AccessWrites may run: a rule's (add,
// rm, edit, drop, undrop, set local-ports), never for all
// projects, never a reset or another switch (the human's).
func accessOpen(args []string) bool {
	var pos []string
	for _, a := range args {
		if a == "--all" || strings.HasPrefix(a, "--all=") {
			return false
		}
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	if len(pos) < 2 || pos[0] != "access" || pos[1] == "reset" {
		return false
	}
	return pos[1] != "set" || len(pos) > 2 && pos[2] == "local-ports"
}

// nowhere is the folder a command line's relative paths are from, as
// the floor follows its cd: "" when it can't tell.
type nowhere struct{ dir string }

// cd is where cmd (cd, pushd, popd) with args leaves the line: a
// folder it names plainly, home for none, else unknown: an entry of the
// folder stack (+N, -N) may be any folder, a pattern or braces (cd
// ~/.term*) may name another, and so may a relative one CDPATH looks
// up, when the line sets it. Its redirections (pushd x >/dev/null) are
// no folder. A physical cd (cd -P, or one the line may
// have made physical: physicalCd) leaves the folder with its links
// followed (Resolve), so its .. is the real parent; where the guard
// can't follow them, or can't tell a logical cd from a physical one
// that lands elsewhere, the folder is unknown.
func (n nowhere) cd(r Rules, cmd string, args []string) nowhere {
	var to []string
	mode := byte(0)
	for _, a := range unredirected(args) {
		if stackIndex.MatchString(a) {
			return nowhere{} // cd +1, pushd -2: an entry of the folder stack
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			to = append(to, a)
		} else if i := strings.LastIndexAny(a, "LP"); i > 0 && !strings.HasPrefix(a, "--") {
			mode = a[i]
		}
	}
	switch {
	case cmd == "popd" || len(to) > 1:
		return nowhere{}
	case len(to) == 0:
		if cmd == "pushd" {
			return nowhere{}
		}
		return nowhere{dir: r.Home}
	}
	a := to[0]
	if a == "-" || strings.ContainsAny(a, "$`*?[{") || strings.HasPrefix(a, "~") && !strings.HasPrefix(a, "~/") && a != "~" {
		return nowhere{}
	}
	if !dotted.MatchString(slash(a)) && !strings.HasPrefix(a, "~") && !drive.MatchString(slash(a)) && strings.Contains(r.line, "CDPATH") {
		return nowhere{}
	}
	if n.dir == "" && !strings.HasPrefix(a, "/") && !strings.HasPrefix(a, "~") && !drive.MatchString(slash(a)) {
		return nowhere{}
	}
	r.Cwd = n.dir
	dir := r.abs(a)
	if mode == 'P' || mode == 0 && r.physical {
		if r.Resolve == nil {
			return nowhere{}
		}
		real := r.abs(r.resolve(r.joined(a)))
		if mode == 0 && real != dir {
			return nowhere{}
		}
		dir = real
	}
	return nowhere{dir: dir}
}

// stackIndex is an argument of cd, pushd or popd that names an entry of
// the folder stack (+N, -N), not a folder or an option.
var stackIndex = regexp.MustCompile(`^[+-][0-9]+$`)

// physicalCd is a line that may make its cds physical: bash's set -P or
// set -o physical (or a shell started with them), zsh's chase_links or
// chase_dots.
var physicalCd = regexp.MustCompile(`\bphysical\b|(?i:chase_?(?:links|dots))|(?:^|[\s;&|(){}])(?:set|bash|sh|zsh|ksh|dash)(?:\s+[-+]\S*)*\s+-[A-Za-z]*P`)

// globReaches: glob p (a wildcard in it) could name a folder that holds
// root, or root itself: its part before the first wildcard, back to the
// last /, holds root, and the pattern's next part matches root's
// (rm -rf ~/.term*, ~/.terminatr/proj*).
func (r Rules) globReaches(p, root string) bool {
	i := strings.IndexAny(p, "*?[")
	if i < 0 {
		return false
	}
	j := strings.LastIndex(p[:i], "/")
	if j < 0 {
		return false
	}
	dir, pat := r.fold(p[:j]), r.fold(p[j+1:])
	if dir == "" {
		dir = "/"
	}
	root = r.fold(root)
	if !under(root, dir) || root == dir {
		return false
	}
	rest := strings.TrimPrefix(root[len(dir):], "/")
	part, _, _ := strings.Cut(rest, "/")
	pat, _, _ = strings.Cut(pat, "/")
	ok, err := path.Match(pat, part)
	return ok || err != nil
}

// linePart is a part of a command line between the separators of its
// top level (;, newline, &&, ||, |, &): its text and the separators
// before and after it ("" at the line's ends; a newline is ;, |& is |);
// doc when its text holds the body of a heredoc it opens; group the
// first ( ) or { } its top level opens, when only words that run it in
// the line's flow (groupLead) come before it.
type linePart struct {
	text, before, after string
	doc                 bool
	group               *lineGroup
}

// lineGroup is a part's ( ) or { } (linePart): the words before it
// (then, do, !), what is inside its brackets and what comes after them
// (its redirections), and brace for { }.
type lineGroup struct {
	lead, inner, tail string
	brace             bool
}

// groupLead are the words that may come before a part's ( ) or { } and
// run it in the line's flow (an if's or a loop's, a pipeline's !, time).
var groupLead = setOf("! time -p if then elif else while until do")

// listEnd: separator sep ends an and-or list.
func listEnd(sep string) bool { return sep == "" || sep == ";" || sep == "&" }

// withDirs is dirs with more added, without repeats.
func withDirs(dirs []string, more ...string) []string {
	out := slices.Clone(dirs)
	for _, d := range more {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// cdWords are the commands that change a shell's folder.
var cdWords = setOf("cd pushd popd")

// hasCd: some word of text's commands is cd, pushd or popd.
func (r Rules) hasCd(text string) bool {
	for _, words := range r.cmds(text) {
		if slices.ContainsFunc(words, func(w string) bool { return cdWords[w] }) {
			return true
		}
	}
	return false
}

// plainCd: part is one plain cd, pushd or popd, and its arguments.
func (r Rules) plainCd(part string) (cmd string, args []string, ok bool) {
	if strings.HasPrefix(part, "(") || strings.HasPrefix(part, "{") {
		return "", nil, false
	}
	cmds := r.cmds(part)
	if len(cmds) != 1 || len(cmds[0]) == 0 || !cdWords[cmds[0][0]] {
		return "", nil, false
	}
	return cmds[0][0], cmds[0][1:], true
}

// topLevel splits a command line at its top level's separators, outside
// quotes, ( ), { }, $( ) and backticks. A heredoc opened at the top
// level has its body added to the part that opens it, after the
// newline that ends the part's line. ok is false for a line whose
// quotes or brackets don't close, with a heredoc inside brackets, or
// with a shell that may run a heredoc's body (cat <<EOF | bash), which
// the line's own reading (Commands) judges.
func (r Rules) topLevel(line string) (parts []linePart, ok bool) {
	s := []rune(line)
	var stack []rune
	var b strings.Builder
	before := ""
	// The heredocs opened and not yet read: their word, <<- and the
	// part that opens each (-1: the one being read).
	type doc struct {
		delim string
		tabs  bool
		part  int
	}
	var open []doc
	docs := false
	// The first bracket the part's top level opens and the index after
	// its close, in b (-1: none yet).
	gOpen, gClose := -1, -1
	cut := func(sep string) {
		raw := b.String()
		if t := strings.TrimSpace(raw); t != "" {
			parts = append(parts, linePart{text: t, before: before})
			if gClose > 0 {
				g := &lineGroup{lead: strings.TrimSpace(raw[:gOpen]), inner: raw[gOpen+1 : gClose-1], tail: raw[gClose:], brace: raw[gOpen] == '{'}
				if !slices.ContainsFunc(strings.Fields(g.lead), func(w string) bool { return !groupLead[w] }) {
					parts[len(parts)-1].group = g
				}
			}
			before = sep
			for k := range open {
				if open[k].part < 0 {
					open[k].part = len(parts) - 1
				}
			}
		} else if before == "" || sep == ";" {
			before = sep
		}
		if n := len(parts); n > 0 && parts[n-1].after == "" {
			parts[n-1].after = sep
		}
		b.Reset()
		gOpen, gClose = -1, -1
	}
	// bodies reads the bodies of the open heredocs from s[i], the start
	// of the line after the one that opened them, into their parts; the
	// index after them.
	bodies := func(i int) int {
		for _, h := range open {
			var body strings.Builder
			for i < len(s) {
				k := i
				for k < len(s) && s[k] != '\n' {
					k++
				}
				l := string(s[i:k])
				body.WriteString("\n" + l)
				i = k + 1
				if h.tabs {
					l = strings.TrimLeft(l, "\t")
				}
				if l == h.delim {
					break
				}
			}
			if h.part < 0 {
				b.WriteString(body.String())
			} else {
				parts[h.part].text += body.String()
				parts[h.part].doc = true
			}
		}
		open = nil
		return i
	}
	wordStart := func(i int) bool { return i == 0 || strings.ContainsRune(" \t\n;&|(", s[i-1]) }
	top := func() rune {
		if len(stack) == 0 {
			return 0
		}
		return stack[len(stack)-1]
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch top() {
		case '"':
			b.WriteRune(c)
			switch {
			case c == '\\' && i+1 < len(s):
				i++
				b.WriteRune(s[i])
			case c == '"':
				stack = stack[:len(stack)-1]
			case c == '$' && i+1 < len(s) && s[i+1] == '(':
				i++
				b.WriteRune('(')
				stack = append(stack, '(')
			case c == '`':
				stack = append(stack, '`')
			}
			continue
		case '`':
			b.WriteRune(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteRune(s[i])
			} else if c == '`' {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		next := rune(0)
		if i+1 < len(s) {
			next = s[i+1]
		}
		prev := rune(0)
		if i > 0 {
			prev = s[i-1]
		}
		switch {
		case c == '\\' && i+1 < len(s):
			b.WriteRune(c)
			i++
			b.WriteRune(s[i])
			continue
		case c == '$' && next == '\'':
			j := ansiEnd(s, i+2)
			if j == len(s) {
				return nil, false
			}
			b.WriteString(string(s[i : j+1]))
			i = j
			continue
		case c == '\'':
			j := i + 1
			for j < len(s) && s[j] != '\'' {
				j++
			}
			if j == len(s) {
				return nil, false
			}
			b.WriteString(string(s[i : j+1]))
			i = j
			continue
		case c == '"' || c == '`':
			stack = append(stack, c)
		case c == '$' && next == '{':
			j := i + 2
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j == len(s) {
				return nil, false
			}
			b.WriteString(string(s[i : j+1]))
			i = j
			continue
		case c == '(':
			if len(stack) == 0 && gOpen < 0 {
				gOpen = b.Len()
			}
			stack = append(stack, '(')
		case c == ')':
			if top() != '(' {
				return nil, false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 && gClose < 0 {
				gClose = b.Len() + 1
			}
		case c == '{' && wordStart(i) && (next == ' ' || next == '\t' || next == '\n'):
			if len(stack) == 0 && gOpen < 0 {
				gOpen = b.Len()
			}
			stack = append(stack, '{')
		case c == '}' && top() == '{' && wordStart(i):
			stack = stack[:len(stack)-1]
			if len(stack) == 0 && gClose < 0 {
				gClose = b.Len() + 1
			}
		case c == '<' && next == '<' && (i+2 >= len(s) || s[i+2] != '<') && prev != '<':
			// A heredoc: <<WORD or <<-WORD, its word quoted or not.
			if len(stack) > 0 {
				return nil, false
			}
			j := i + 2
			h := doc{part: -1}
			if j < len(s) && s[j] == '-' {
				h.tabs = true
				j++
			}
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			dw := docWord(s, j)
			// A word the guard can't read for sure: Commands reads the
			// rest as commands.
			if !dw.sure || dw.delim == "" && !dw.quoted {
				return nil, false
			}
			h.delim = dw.delim
			j = dw.end
			b.WriteString(string(s[i:min(j, len(s))]))
			open = append(open, h)
			docs = true
			i = j - 1
			continue
		case len(stack) > 0:
			// Inside brackets: no separator, and no heredoc body read.
			if c == '\n' && len(open) > 0 {
				return nil, false
			}
		case c == '#' && wordStart(i):
			for i < len(s) && s[i] != '\n' {
				i++
			}
			cut(";")
			i = bodies(i+1) - 1
			continue
		case c == ';' || c == '\n':
			cut(";")
			if c == '\n' {
				i = bodies(i+1) - 1
			}
			continue
		case c == '&' && next == '&':
			i++
			cut("&&")
			continue
		case c == '|' && next == '|':
			i++
			cut("||")
			continue
		case c == '|' && prev != '>':
			if next == '&' {
				i++
			}
			cut("|")
			continue
		case c == '&' && prev != '>' && prev != '<' && next != '>':
			cut("&")
			continue
		}
		b.WriteRune(c)
	}
	if len(stack) > 0 {
		return nil, false
	}
	cut("")
	if docs && slices.ContainsFunc(parts, func(p linePart) bool {
		return slices.ContainsFunc(r.cmds(p.text), func(w []string) bool { return readsInput(w, p.before == "|", nil) })
	}) {
		return nil, false
	}
	return parts, true
}

// judgeNoWrite: the floor's NoWrite, of a simple command, its name and
// arguments and its words as written: a redirect into a path there, or
// a command that writes the paths it is given naming one (cp's and the
// like's target; a folder holding one, for a remover). With r.Cwd ""
// (a cd the line made that it can't follow) a relative path could be
// anywhere, so it is refused; one that starts with a variable or a
// substitution ($TMPDIR/x, "$F") is where its value says, which the
// guard can't place with a cd or without one.
func (r Rules) judgeNoWrite(cmd string, args, words []string) *Denial {
	if len(r.NoWrite) == 0 {
		return nil
	}
	var refuse func(w string, parents bool) *Denial
	refuse = func(w string, parents bool) *Denial {
		// A descriptor (2>&1) or a process substitution (> >(tee f)).
		if w == "" || strings.HasPrefix(w, "&") || strings.HasPrefix(w, "<(") || strings.HasPrefix(w, ">(") {
			return nil
		}
		// ${x:-word} and the like may give their word: judged as a path
		// too.
		if dw, ok := defaultWord(w); ok {
			if dd := refuse(dw, parents); dd != nil {
				return dd
			}
		}
		// A path from a folder the line has been in is judged from each
		// folder that may be: $PWD from the line's, $OLDPWD from those
		// before it (olds), the folder stack's bottom from the line's
		// while it pushes none, and from none another index or $PWD
		// changed by an operator. zsh has no $DIRSTACK: there it is empty,
		// and its $dirstack is empty too until the line pushes, then the
		// folders before.
		if kind, n := folderStart(w); kind != "" {
			switch {
			case kind == "bottom" && pushes.MatchString(r.line), kind == "bottomStack" && pushes.MatchString(r.line):
				kind = "unknown"
			case kind == "bottom":
				kind = "cur"
			case kind == "bottomStack":
				kind = "stack"
			}
			if kind == "stack" && pushes.MatchString(r.line) && zshStack.MatchString(w) {
				kind = "unknown"
			}
			if kind == "stack" {
				if dd := refuse(w[n:], parents); dd != nil {
					return dd
				}
			}
			dirs := []string{""}
			switch kind {
			case "cur", "stack":
				dirs = []string{r.Cwd}
			case "old":
				dirs = r.olds
				if dirs == nil {
					dirs = []string{""}
				}
			}
			for _, d := range dirs {
				if d == "" {
					if kind == "cur" || kind == "stack" {
						return floor(r.lostCd(cmd + " of " + safe(w) + " after a cd"))
					}
					return floor(r.unplaced(cmd + " of " + safe(w)))
				}
				if dd := refuse(d+w[n:], parents); dd != nil {
					return dd
				}
			}
			return nil
		}
		// A variable that isn't set is empty: $NOPE/x is /x.
		if m := bareParam.FindString(w); m != "" && strings.HasPrefix(w[len(m):], "/") {
			if dd := refuse(w[len(m):], parents); dd != nil {
				return dd
			}
		}
		if r.lineCd && lineFolder(w, r.line) {
			return floor(r.unplaced(cmd + " of " + safe(w) + " after a cd"))
		}
		if r.Cwd == "" && !strings.HasPrefix(slash(w), "/") && !strings.HasPrefix(w, "~") && !placedVar(w, r.line) && !drive.MatchString(slash(w)) {
			return floor(r.lostCd(cmd + " of " + safe(w) + " after a cd"))
		}
		// The kernel takes a .. from where the links before it lead,
		// where abs takes it from the path as written (after a cd into a
		// link, ../x is beside the link's target): such a path is judged
		// where Resolve says it lands too, and without Resolve, after a
		// cd, it is unknown.
		lands := []string{r.abs(w)}
		if parent.MatchString(slash(w)) {
			switch {
			case r.Resolve != nil:
				lands = withDirs(lands, r.abs(r.resolve(r.joined(w))))
			case r.lineCd:
				return floor(r.lostCd(cmd + " of " + safe(w) + " after a cd"))
			}
		}
		for _, p := range lands {
			fixed := strings.TrimSuffix(wildcard.ReplaceAllString(p, ""), "/")
			if fixed == "" {
				fixed = "/"
			}
			if r.covers(r.NoWrite, p) || parents && slices.ContainsFunc(r.NoWrite, func(n string) bool { return r.under(n, fixed) || r.globReaches(p, n) }) {
				return floor(r.deny(RuleTMFiles, cmd+" of "+safe(p), ""))
			}
		}
		return nil
	}
	for i := 1; i < len(words); i++ {
		if words[i-1] == ">" && words[i] != ">" {
			if d := refuse(words[i], false); d != nil {
				return d
			}
		}
	}
	if cmd == ">" {
		return nil
	}
	if cmd == "tm" || strings.Contains(cmd, "TERMINATR_BIN") {
		if what := tmWritesSettings(args); what != "" && !(r.AccessWrites && accessOpen(args)) {
			return floor(r.deny(RuleTMFiles, what, ""))
		}
	}
	all, ok := writers[cmd]
	if inPlace[cmd] {
		all, ok = true, slices.ContainsFunc(args, func(a string) bool {
			return isLong(a, "--in-place") || shortFlags.MatchString(a) && strings.Contains(a, "i") || strings.HasPrefix(a, "-i")
		})
	}
	switch cmd {
	case "dd":
		for _, a := range args {
			if p, ok := strings.CutPrefix(a, "of="); ok {
				if d := refuse(p, false); d != nil {
					return d
				}
			}
		}
	case "git":
		// git config --file F (-f F) writes F.
		if i := slices.Index(args, "config"); i >= 0 {
			for j := i + 1; j < len(args); j++ {
				f := ""
				switch {
				case (args[j] == "--file" || args[j] == "-f") && j+1 < len(args):
					f = args[j+1]
				case strings.HasPrefix(args[j], "--file="):
					f = strings.TrimPrefix(args[j], "--file=")
				}
				if d := refuse(f, false); d != nil {
					return d
				}
			}
		}
	}
	if !ok {
		return nil
	}
	if inPlace[cmd] {
		for _, p := range inPlaceFiles(cmd, unredirected(args)) {
			if d := refuse(p, false); d != nil {
				return d
			}
		}
		return nil
	}
	recursive := removers[cmd] || slices.ContainsFunc(args, func(a string) bool {
		return isLong(a, "--recursive") || isLong(a, "--archive") || shortFlags.MatchString(a) && strings.ContainsAny(a[1:], "rRa")
	})
	// The paths given, and the targets given with -t.
	var paths, targets []string
	args = unredirected(args)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-t" || a == "--target-directory") && i+1 < len(args):
			i++
			targets = append(targets, args[i])
		case strings.HasPrefix(a, "--target-directory="):
			targets = append(targets, strings.TrimPrefix(a, "--target-directory="))
		case !strings.HasPrefix(a, "-"):
			paths = append(paths, a)
		}
	}
	switch {
	case all:
		paths = append(paths, targets...)
	case len(targets) > 0:
		paths = targets
	case len(paths) > 0:
		paths = paths[len(paths)-1:]
	}
	for _, p := range paths {
		if d := refuse(p, recursive); d != nil {
			return d
		}
	}
	return nil
}

// heredoc is a here-document a command line opens (<<WORD, <<-WORD):
// the word that ends it, whether its lines lose their leading tabs, and
// whether the word was quoted (then nothing in it expands); then its
// body once read. odd when its word is one the guard may end elsewhere
// than bash does ($'EOF', E$(x)F, a quote left open): its body is read
// as commands.
type heredoc struct {
	delim             string
	tabs, quoted, odd bool
	body              string
}

// Commands splits a command line into simple commands, each a list of
// words with quotes removed; redirections stay words ("<", ">"). A $( )
// or backtick is a word of its command, its inside read as commands too.
// A heredoc's body is read as commands when the line has a shell to run
// it; otherwise only what it substitutes is.
func Commands(line string) [][]string { return readCommands(line, false, nil) }

// readCommands is Commands (in arithmetic, arith, << is a shift, no
// heredoc), where a shell given input that runs a
// script reads none of it if script says the file is the worktree's
// (worktreeScript; nil: none is).
func readCommands(line string, arith bool, script func(string) bool) [][]string {
	return (&parse{}).read(line, arith, script, 0)
}

// Limits on what the guard reads of one command line (docs/SPEC.md
// §8.6, Guard): MaxCommand bytes, substitutions, subshells, arithmetic
// and heredocs nested maxNesting deep, and maxParse bytes read by the
// parser in all, every nesting level counted (a heredoc's body is read
// twice: as text the line expands and as a shell's input, so without a
// bound nested heredocs double the work at each level). A line past
// them is refused (Judge), not read in part.
const (
	MaxCommand = 64 << 10
	maxNesting = 32
	maxParse   = 4 << 20
	// maxFolders bounds the folders a line's cd, pushd and popd may
	// leave it in (judgeLine), each of which its parts are judged from:
	// a script that walks its subfolders in turn levels off near 36 (the
	// parents its cd .. climb to repeat), more for a deeper folder; the
	// parse budget and deadline bound the work either way.
	maxFolders = 256
)

// parse is one line's reading: what its parser has read, every nesting
// level counted, and whether it went past the limits.
type parse struct {
	work     int
	limit    int
	deadline time.Time
	over     bool
	late     bool // over by the deadline
	// traps are the actions of the traps the lines read set, in order
	// (judgeLine).
	traps []string
	// resolved and files are what the file system answered (resolve,
	// file).
	resolved map[string]string
	files    map[string]bool
	// subs, while collect, are the insides of the substitutions read at
	// depth at (subsOf).
	collect bool
	at      int
	subs    []string
}

// sub reads body, the inside of a substitution ($( ), backticks, <( ),
// >( )) that runs in a subshell of a line read at depth, one level
// deeper; while collecting at depth, it is kept too.
func (p *parse) sub(body string, depth int) [][]string {
	if p.collect && depth == p.at {
		p.subs = append(p.subs, body)
	}
	return p.read(body, false, nil, depth+1)
}

// spent reports whether the judge's reading went past its limits (or
// its deadline): judging stops, and Judge refuses the call.
func (r Rules) spent() bool { return r.ps != nil && r.ps.stop(0, r.depth) }

// nested is r for a line read one level deeper (sh -c, eval): past
// maxNesting the reading stops.
func (r Rules) nested() Rules {
	r.depth++
	if r.ps != nil {
		r.ps.stop(0, r.depth)
	}
	return r
}

// subsOf are the insides of the substitutions text runs in subshells of
// its own as it is expanded, those in them not counted: read with the
// judge's budget.
func (r Rules) subsOf(text string) []string {
	if r.ps == nil {
		return nil
	}
	p := r.ps
	p.collect, p.at, p.subs = true, r.depth, nil
	p.read(text, false, nil, r.depth)
	subs := p.subs
	p.collect, p.subs = false, nil
	return subs
}

// cmds are the commands of line as Commands reads them, read with the
// judge's budget at its depth.
func (r Rules) cmds(line string) [][]string {
	if r.ps == nil {
		return Commands(line)
	}
	return r.ps.read(line, false, nil, r.depth)
}

// stop counts n more bytes read at depth and reports whether the
// reading is over its limits: its work (maxParse unless limit says),
// maxNesting, or its deadline.
func (p *parse) stop(n, depth int) bool {
	p.work += n
	limit := cmp.Or(p.limit, maxParse)
	switch {
	case p.over:
	case !p.deadline.IsZero() && time.Now().After(p.deadline):
		p.over, p.late = true, true
	case depth > maxNesting || p.work > limit:
		p.over = true
	}
	return p.over
}

// readable reports whether line is within the guard's limits: no longer
// than MaxCommand, nested no deeper than maxNesting, read within
// maxParse.
func readable(line string) bool {
	if len(line) > MaxCommand {
		return false
	}
	p := &parse{}
	p.read(line, false, nil, 0)
	return !p.over
}

// read is readCommands of line at nesting depth; the commands of what
// it substitutes or nests are read at depth+1, sharing p's budget. Past
// the limits it reads nothing more and marks p over.
func (p *parse) read(line string, arith bool, script func(string) bool, depth int) [][]string {
	if p.stop(len(line), depth) {
		return nil
	}
	var out, inner [][]string
	var words []string
	// word is the word as written; lit the same without its unquoted
	// substitutions, what the word is when they print nothing (printenv$(x)
	// is printenv): the word, unless it is substitutions alone. Those a
	// path starts with stay ($(pwd)/f is no /f).
	var word, lit strings.Builder
	inWord, hasLit := false, false
	// quoted: some of the word is quoted or escaped ("2">f writes 2's
	// output to f: no descriptor).
	quoted := false
	var open, docs []heredoc
	// redir is the index of the last < or > read as a redirect (>| is
	// one); fed: a pipe feeds the next command, and fedShell, it is a
	// shell.
	redir := -1
	fed, fedShell := false, false
	endWord := func() {
		if inWord && hasLit {
			words = append(words, lit.String())
		} else if inWord {
			words = append(words, word.String())
		}
		word.Reset()
		lit.Reset()
		inWord, hasLit, quoted = false, false, false
	}
	// text is literal text of the word.
	text := func(t string) {
		if kind, _ := folderStart(word.String()); !hasLit && word.Len() > 0 && (strings.HasPrefix(t, "/") || kind != "") {
			lit.WriteString(word.String())
		}
		word.WriteString(t)
		lit.WriteString(t)
		inWord, hasLit = true, true
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
			if fed && readsInput(words, true, script) {
				fedShell = true
			}
			fed = false
			out = append(out, words)
		}
		// What the command substitutes, after it.
		out = append(out, inner...)
		words, inner = nil, nil
	}
	s := []rune(line)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			if s[i+1] != '\n' {
				text(string(s[i+1]))
				quoted = true
			}
			i++
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			// ANSI-C quoting, $'…': the text its escapes give.
			end := ansiEnd(s, i+2)
			text(ansiC(string(s[i+2 : end])))
			quoted = true
			i = end
		case c == '$' && i+1 < len(s) && s[i+1] == '"':
			// A locale string, $"…": read as the double-quoted one it is.
		case c == '\'':
			end := len(s)
			if j := slices.Index(s[i+1:], '\''); j >= 0 {
				end = i + 1 + j
			}
			text(string(s[i+1 : end]))
			quoted = true
			i = end
		case c == '"':
			// Inside double quotes a $( ) or backtick still runs: its
			// inside is read as commands too.
			j := i + 1
			var q strings.Builder
			for j < len(s) && s[j] != '"' {
				switch {
				case s[j] == '\\' && j+1 < len(s):
					q.WriteRune(s[j+1])
					j += 2
				case s[j] == '$' && j+1 < len(s) && s[j+1] == '(':
					end := closing(s, j+2, '(', ')')
					out = append(out, p.sub(string(s[j+2:end]), depth)...)
					q.WriteString(string(s[j:min(end+1, len(s))]))
					j = end + 1
				case s[j] == '`':
					end := tickClose(s, j+1)
					out = append(out, p.sub(string(s[j+1:end]), depth)...)
					q.WriteString(string(s[j:min(end+1, len(s))]))
					j = end + 1
				default:
					q.WriteRune(s[j])
					j++
				}
			}
			text(q.String())
			quoted = true
			i = j
		case c == ' ' || c == '\t':
			endWord()
		case c == '#' && !inWord:
			// A comment, to the end of the line.
			for i+1 < len(s) && s[i+1] != '\n' {
				i++
			}
		case c == '(' && i+1 < len(s) && s[i+1] == '(':
			// Arithmetic, (( … )), whose << is a shift, not a heredoc: read
			// on its own, as commands (bash takes ((a; b)) for subshells).
			end := closing(s, i+1, '(', ')')
			endCommand()
			out = append(out, p.read(string(s[i+2:min(end, len(s))]), true, nil, depth+1)...)
			i = end
		case c == '$' && i+1 < len(s) && s[i+1] == '[' && closing(s, i+2, '[', ']') < len(s):
			// Arithmetic, $[ … ], a part of its word: its inside read on
			// its own, as commands.
			end := closing(s, i+2, '[', ']')
			inner = append(inner, p.read(string(s[i+2:end]), true, nil, depth+1)...)
			text(string(s[i : end+1]))
			i = end
		case c == '$' && i+2 < len(s) && s[i+1] == '{' && !strings.ContainsRune(" \t\n|", s[i+2]):
			// A parameter expansion, ${NAME} or ${NAME:-word}, is part of
			// the word (${ cmd; } runs commands: read below as { }). A $( )
			// or backtick in it still runs.
			end := closing(s, i+2, '{', '}')
			for _, in := range substitutions(string(s[i+2 : end])) {
				out = append(out, p.sub(in, depth)...)
			}
			text(string(s[i:min(end+1, len(s))]))
			i = end
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			// A command substitution is a word of the command it is in.
			end := closing(s, i+2, '(', ')')
			inner = append(inner, p.sub(string(s[i+2:end]), depth)...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord = true
			i = end
		case c == '`':
			end := tickClose(s, i+1)
			inner = append(inner, p.sub(string(s[i+1:end]), depth)...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord = true
			i = end
		case c == '\n' && len(open) > 0:
			// The bodies of the heredocs the line opened.
			endCommand()
			j := i + 1
			for _, h := range open {
				var body strings.Builder
				for j < len(s) {
					k := j
					for k < len(s) && s[k] != '\n' {
						k++
					}
					l := string(s[j:k])
					if h.tabs {
						l = strings.TrimLeft(l, "\t")
					}
					j = k + 1
					if l == h.delim {
						break
					}
					body.WriteString(l + "\n")
				}
				h.body = body.String()
				docs = append(docs, h)
			}
			open = nil
			i = j - 1
		case c == '&' && redir == i-1 && i+1 < len(s) && strings.ContainsRune("0123456789-", s[i+1]):
			// A redirect to or from a descriptor (2>&1, <&0, >&-): its word.
			text(string(c))
		case c == '|' && redir == i-1 && s[i-1] == '>':
			// >|, a redirect that overwrites under noclobber: its word
			// follows.
		case (c == '<' || c == '>') && i+1 < len(s) && s[i+1] == '(' && !arith:
			// A process substitution, <( ) or >( ), is a word of its
			// command (cp <(x) f copies to f), its inside read as commands
			// too.
			end := closing(s, i+2, '(', ')')
			inner = append(inner, p.sub(string(s[i+2:end]), depth)...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord = true
			i = end
		case strings.ContainsRune(";&|\n(){}", c):
			endCommand()
			if c == '|' && (i+1 >= len(s) || s[i+1] != '|') && s[i-1] != '|' {
				fed = true
			}
		case c == '<' && !arith && i+1 < len(s) && s[i+1] == '<' && (i+2 >= len(s) || s[i+2] != '<') && (i == 0 || s[i-1] != '<'):
			// A heredoc: <<WORD or <<-WORD.
			endWord()
			words = append(words, "<", "<")
			j := i + 2
			h := heredoc{}
			if j < len(s) && s[j] == '-' {
				h.tabs = true
				j++
			}
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			dw := docWord(s, j)
			h.delim, h.quoted, h.odd = dw.delim, dw.quoted, !dw.sure
			j = dw.end
			if h.delim != "" || h.quoted {
				words = append(words, h.delim)
				open = append(open, h)
			}
			i = j - 1
		case c == '<' || c == '>':
			// Digits right before it, unquoted, are the descriptor it
			// redirects (2>f), no word of the command.
			if inWord && hasLit && !quoted && word.String() == lit.String() && digits.MatchString(word.String()) {
				word.Reset()
				lit.Reset()
				inWord, hasLit = false, false
			}
			endWord()
			words = append(words, string(c))
			redir = i
		default:
			text(string(c))
		}
	}
	endCommand()
	if len(docs) > 0 {
		// A shell anywhere on the line (ssh host bash, docker exec -i c
		// sh) may run what a heredoc gives it.
		shell := fedShell || slices.ContainsFunc(out, func(w []string) bool { return readsInput(w, false, script) })
		for _, h := range docs {
			if !h.quoted {
				// The line's own shell expands it first.
				for _, in := range substitutions(h.body) {
					out = append(out, p.sub(in, depth)...)
				}
			}
			if shell || h.odd {
				out = append(out, p.read(h.body, false, nil, depth+1)...)
			}
		}
	}
	return out
}

// ansiEnd is the index of the quote that closes $'…' whose text starts
// at s[i], past its escapes (\'); len(s) when none does.
func ansiEnd(s []rune, i int) int {
	for ; i < len(s) && s[i] != '\''; i++ {
		if s[i] == '\\' {
			i++
		}
	}
	return min(i, len(s))
}

// ansiEscape is an escape of $'…'.
var ansiEscape = regexp.MustCompile(`\\(x[0-9a-fA-F]{1,2}|u[0-9a-fA-F]{1,4}|U[0-9a-fA-F]{1,8}|[0-7]{1,3}|c[\s\S]|[\s\S])`)

// ansiSimple are the escapes of $'…' that stand for one character.
var ansiSimple = map[string]string{"n": "\n", "t": "\t", "r": "\r", "a": "\x07", "b": "\b", "e": "\x1b", "E": "\x1b", "f": "\f", "v": "\v"}

// ansiC is the text of $'t': its escapes (\n, \x2e, \056, \u2e, \cA)
// as the characters they stand for, any other \c as c.
func ansiC(t string) string {
	return ansiEscape.ReplaceAllStringFunc(t, func(m string) string {
		e := m[1:]
		switch {
		case strings.ContainsRune("xuU", rune(e[0])) && len(e) > 1:
			n, _ := strconv.ParseUint(e[1:], 16, 32)
			if n > unicode.MaxRune {
				return "\uFFFD"
			}
			return string(rune(n))
		case e[0] >= '0' && e[0] <= '7':
			n, _ := strconv.ParseUint(e, 8, 32)
			return string(rune(n & 0xff))
		case e[0] == 'c' && len(e) > 1:
			return string([]rune(e)[1] & 0x1f)
		}
		if r, ok := ansiSimple[e]; ok {
			return r
		}
		return e
	})
}

// digits is a word of digits alone.
var digits = regexp.MustCompile(`^[0-9]+$`)

// readsInput: the simple command w (its words) may run what a heredoc
// gives it: a shell or the like reading commands from its input, a
// shell word anywhere in it (ssh host bash, docker exec -i c sh), or an
// exec (exec <<EOF gives the line's later commands the heredoc as their
// input). A shell that runs a script file reads none while nothing is
// given it, nor when a pipe feeds it (fed) or a redirect does, if the
// script is a regular file of the worktree (script); options or
// variables that make it read more (-i, --rcfile, BASH_ENV=) count as
// reading.
func readsInput(w []string, fed bool, script func(string) bool) bool {
	argv := strip(w)
	isShell := func(a string) bool { return shells[base(a)] }
	if len(argv) > 0 && shells[base(argv[0])] {
		file, extra := shellRun(unredirected(argv[1:]))
		pre := w[:max(0, len(w)-len(argv))]
		if file == "" || extra || slices.ContainsFunc(pre, extraEnv.MatchString) || !scriptFile(file) {
			return true
		}
		if (fed || slices.Contains(w, "<")) && (script == nil || !script(file)) {
			return true
		}
		return slices.ContainsFunc(argv[1:], isShell)
	}
	return len(argv) > 0 && (stdinShells[base(argv[0])] || argv[0] == "exec") || slices.ContainsFunc(w, isShell)
}

// extraEnv are the assignments that make a shell read a file first:
// bash's BASH_ENV, sh's ENV, zsh's ZDOTDIR (its .zshenv).
var extraEnv = regexp.MustCompile(`^(BASH_ENV|ENV|ZDOTDIR)=`)

// shellFlags is a cluster of a shell's short options (-ex, +o).
var shellFlags = regexp.MustCompile(`^[-+][a-zA-Z]+$`)

// shellRun is how a shell given args runs: the script file it runs, the
// first argument that is no option ("" when it runs commands from its
// input, -s, or a string, -c, or nothing is given), and extra: an
// option makes it read more (-i, --interactive, --rcfile, --init-file).
// -o and -O take a value.
func shellRun(args []string) (file string, extra bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if i+1 < len(args) {
				return args[i+1], extra
			}
			return "", extra
		case isLong(a, "--rcfile") || isLong(a, "--init-file"):
			extra = true
			if !strings.Contains(a, "=") {
				i++
			}
		case isLong(a, "--interactive"):
			extra = true
		case shellFlags.MatchString(a):
			if strings.ContainsAny(a[1:], "sc") {
				return "", extra
			}
			if strings.HasPrefix(a, "-") && strings.Contains(a, "i") {
				extra = true
			}
			if strings.ContainsAny(a, "oO") {
				i++
			}
		case !strings.HasPrefix(a, "-"):
			return a, extra
		}
	}
	return "", extra
}

// devOrProc is a clean absolute path under /dev or /proc; devPart a
// relative one with a part dev, proc or fd.
var (
	devOrProc = regexp.MustCompile(`^/(dev|proc)(/|$)`)
	devPart   = regexp.MustCompile(`(^|/)(dev|proc|fd)(/|$)`)
)

// scriptFile: a is a script's path written out, which can't be the
// shell's input: no variable or substitution in it, and, made clean, no
// path under /dev or /proc (/./dev/stdin) or, relative, with a part dev,
// proc or fd (dev/fd/0 from /).
func scriptFile(a string) bool {
	if a == "" || strings.ContainsAny(a, "$`<>()") {
		return false
	}
	c := path.Clean(slash(a))
	if strings.HasPrefix(c, "/") {
		return !devOrProc.MatchString(c)
	}
	return !devPart.MatchString(c)
}

// worktreeScript: a shell run on the line's folder r.Cwd ("" unknown)
// runs p, a regular file inside the thread's worktree once its links
// are followed (Resolve, File).
func (r Rules) worktreeScript(p string) bool {
	if r.wt == "" || r.Resolve == nil || r.File == nil || !scriptFile(p) || r.writesScript(r.line, p) {
		return false
	}
	if r.Cwd == "" && !strings.HasPrefix(slash(p), "/") && !strings.HasPrefix(p, "~") {
		return false
	}
	j := r.joined(p)
	at := r.abs(r.resolve(j))
	return !devOrProc.MatchString(at) && r.under(at, r.wt) && r.file(j)
}

// scriptWriters are the commands that may write, link or move a file
// they name: the floor's writers, and those that fetch or unpack.
var scriptWriters = func() map[string]bool {
	m := setOf("dd git tar unzip patch curl wget")
	for _, set := range []map[string]bool{writers, removers, inPlace} {
		for c := range set {
			m[c] = true
		}
	}
	return m
}()

// writesScript: line writes, links or moves a file named as script p
// (its last part): one of scriptWriters names it, or a redirect does.
// What File answered about p is from before the line ran.
func (r Rules) writesScript(line, p string) bool {
	b := base(path.Clean(slash(p)))
	return slices.ContainsFunc(r.cmds(line), func(w []string) bool {
		argv := strip(w)
		writer := len(argv) > 0 && scriptWriters[base(argv[0])]
		for i, a := range w {
			if (writer || i > 0 && w[i-1] == ">") && base(path.Clean(slash(strings.TrimPrefix(a, "of=")))) == b {
				return true
			}
		}
		return false
	})
}

// docWord is a heredoc's word as bash reads it from s[j]: the word that
// ends the body, with quotes removed ('…', "…" with its \$ \` \" \\, \x,
// $'…' with its escapes) and substitutions kept as written (E$(x)F);
// whether some of it was quoted; the index after it; and sure: false
// where bash and zsh read it apart or the guard can't tell where the
// body ends ($"…", a newline in it, a substitution with a blank or a
// quote in it or left open, a quote left open, a word that starts a
// comment).
type heredocWord struct {
	delim        string
	quoted, sure bool
	end          int
}

func docWord(s []rune, j int) heredocWord {
	var d strings.Builder
	w := heredocWord{sure: j >= len(s) || s[j] != '#'}
	for j < len(s) && !strings.ContainsRune(" \t\n;&|<>()", s[j]) {
		c := s[j]
		next := rune(0)
		if j+1 < len(s) {
			next = s[j+1]
		}
		switch {
		case c == '\'':
			w.quoted = true
			k := j + 1
			for k < len(s) && s[k] != '\'' {
				d.WriteRune(s[k])
				k++
			}
			if k >= len(s) {
				w.sure = false
			}
			j = k + 1
		case c == '"':
			w.quoted = true
			k := j + 1
			for k < len(s) && s[k] != '"' {
				if s[k] == '\\' && k+1 < len(s) && strings.ContainsRune("$`\"\\", s[k+1]) {
					d.WriteRune(s[k+1])
					k += 2
				} else {
					d.WriteRune(s[k])
					k++
				}
			}
			if k >= len(s) {
				w.sure = false
			}
			j = k + 1
		case c == '\\':
			w.quoted = true
			if next == 0 || next == '\n' {
				w.sure = false
			} else {
				d.WriteRune(next)
			}
			j += 2
		case c == '$' && next == '\'':
			w.quoted = true
			k := ansiEnd(s, j+2)
			if k >= len(s) {
				w.sure = false
			}
			d.WriteString(ansiC(string(s[j+2 : k])))
			j = k + 1
		case c == '$' && next == '"':
			// bash's locale string, zsh's $ and a quoted string.
			w.sure = false
			d.WriteRune(c)
			j++
		case c == '$' && (next == '(' || next == '{' || next == '[') || c == '`':
			// A substitution: its text as written, to the close of its
			// bracket.
			open := next
			k := j + 2
			if c == '`' {
				open, k = '`', j+1
			}
			close := map[rune]rune{'(': ')', '{': '}', '[': ']', '`': '`'}[open]
			for depth := 1; k < len(s); k++ {
				if strings.ContainsRune(" \t\n'\"\\", s[k]) {
					break
				}
				if s[k] == close {
					if depth--; depth == 0 {
						break
					}
				}
				if s[k] == open {
					depth++
				}
			}
			if k >= len(s) || s[k] != close {
				w.sure = false
				d.WriteString(string(s[j:k]))
				j = k
			} else {
				d.WriteString(string(s[j : k+1]))
				j = k + 1
			}
		default:
			d.WriteRune(c)
			j++
		}
	}
	w.delim = d.String()
	if strings.Contains(w.delim, "\n") {
		w.sure = false
	}
	w.end = min(j, len(s))
	return w
}

// substitutions are the insides of the $( ) and backticks in s, which run
// where s is expanded.
func substitutions(text string) []string {
	var out []string
	s := []rune(text)
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == '$' && i+1 < len(s) && s[i+1] == '(':
			end := closing(s, i+2, '(', ')')
			out = append(out, string(s[i+2:end]))
			i = end
		case s[i] == '`':
			end := tickClose(s, i+1)
			out = append(out, string(s[i+1:end]))
			i = end
		}
	}
	return out
}

// closing is the index of the close that closes an open before i
// ({ }, ( )), skipping quoted strings, backticks and escapes, or len(s).
func closing(s []rune, i int, open, close rune) int {
	depth := 1
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			if j := slices.Index(s[i+1:], '\''); j >= 0 {
				i += 1 + j
			} else {
				return len(s)
			}
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				switch {
				case s[i] == '\\':
					i++
				case s[i] == '$' && i+1 < len(s) && s[i+1] == '(':
					i = closing(s, i+2, '(', ')')
				case s[i] == '`':
					i = tickClose(s, i+1)
				}
			}
		case '`':
			i = tickClose(s, i+1)
		case open:
			depth++
		case close:
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

// tickClose is the index of the backtick that closes one opened before
// i, or len(s).
func tickClose(s []rune, i int) int {
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '`':
			return i
		}
	}
	return len(s)
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// wrapper is how a command that runs another reads its own options: the
// short ones that take a value (-n 5, -n5), the long ones that do (--x v;
// --x=v is one word), and how many words come before the command
// (timeout's duration).
type wrapper struct {
	short string
	long  []string
	skip  int
}

// wrappers run the command after them; the shell's keywords come before
// one too.
var wrappers = map[string]wrapper{
	"sudo":       {short: "ughpCDrtTU", long: []string{"--user", "--group", "--host", "--prompt", "--close-from", "--chdir", "--role", "--type", "--command-timeout", "--other-user"}},
	"doas":       {short: "uC"},
	"env":        {short: "uCSP", long: []string{"--unset", "--chdir", "--split-string"}},
	"timeout":    {short: "sk", long: []string{"--signal", "--kill-after"}, skip: 1},
	"nice":       {short: "n", long: []string{"--adjustment"}},
	"ionice":     {short: "cnpPu", long: []string{"--class", "--classdata", "--pid", "--pgid", "--uid"}},
	"xargs":      {short: "ILnPsdEa", long: []string{"--arg-file", "--delimiter", "--max-args", "--max-procs", "--max-chars", "--max-lines", "--process-slot-var"}},
	"stdbuf":     {short: "ioe", long: []string{"--input", "--output", "--error"}},
	"time":       {short: "fo", long: []string{"--format", "--output"}},
	"exec":       {short: "a"},
	"caffeinate": {short: "tw"},
	"command":    {}, "builtin": {}, "nohup": {}, "unbuffer": {}, "setsid": {}, "chronic": {},
	"if": {}, "then": {}, "else": {}, "elif": {}, "while": {}, "until": {}, "do": {}, "!": {},
}

// strip drops what runs a command without being it: env assignments, the
// wrappers (sudo, env, timeout 5, xargs …) with their options, and the
// keywords before a command (if, then, do …), and redirections before
// it (> f git …). A wrapper with no command after it is the command (env
// alone prints the environment).
func strip(words []string) []string {
	i, from := 0, -1
	for i < len(words) {
		w := words[i]
		if assignment.MatchString(w) {
			i++
			continue
		}
		if w == "<" || w == ">" {
			for i+1 < len(words) && (words[i+1] == "<" || words[i+1] == ">") {
				i++
			}
			i += 2
			continue
		}
		wr, ok := wrappers[w]
		if !ok {
			return words[i:]
		}
		from = i
		i++
	options:
		for i < len(words) && strings.HasPrefix(words[i], "-") && words[i] != "-" {
			o := words[i]
			i++
			if o == "--" {
				break
			}
			name, val := o, ""
			if strings.HasPrefix(o, "--") {
				var eq bool
				name, val, eq = strings.Cut(o, "=")
				if !eq && slices.ContainsFunc(wr.long, func(l string) bool { return isLong(o, l) }) && i < len(words) {
					val = words[i]
					i++
				}
			} else if k := strings.IndexFunc(o[1:], func(c rune) bool { return strings.ContainsRune(wr.short, c) }); k >= 0 {
				name, val = "-"+o[1+k:2+k], o[2+k:]
				if val == "" && i < len(words) {
					val = words[i]
					i++
				}
			}
			if w == "env" && (name == "-S" || isLong(name, "--split-string")) {
				// env -S 'a b': the string is words.
				words = append(strings.Fields(val), words[i:]...)
				from, i = 0, 0
				break options
			}
		}
		if w == "env" {
			for i < len(words) && assignment.MatchString(words[i]) {
				i++
			}
		}
		i += wr.skip
	}
	if from >= 0 && from < len(words) {
		return words[from:]
	}
	return nil
}

// isLong: a is the long option l, with or without a value, or an
// abbreviation of it. git and getopt_long take any prefix of a long
// option (--force-w for --force-with-lease); one that other options share
// too is an error to them, so matching it here only refuses a command
// that would fail anyway.
func isLong(a, l string) bool {
	name, _, _ := strings.Cut(a, "=")
	return len(name) > 2 && strings.HasPrefix(name, "--") && strings.HasPrefix(l, name)
}

// shellCommand is the string bash -c runs: the first argument after the
// -c that is not an option (bash -c -e -- STRING), -o and -O taking a
// value.
func shellCommand(args []string) (string, bool) {
	i := slices.IndexFunc(args, shellC.MatchString)
	if i < 0 {
		return "", false
	}
	value := strings.ContainsAny(args[i], "oO")
	for j := i + 1; j < len(args); j++ {
		a := args[j]
		switch {
		case value:
			value = false
		case a == "--" || a == "-":
			return at(args, j+1), j+1 < len(args)
		case len(a) > 1 && (a[0] == '-' || a[0] == '+'):
			value = a[1] != '-' && strings.ContainsAny(a, "oO")
		default:
			return a, true
		}
	}
	return "", false
}

// positionals are args without their options, valued options skipping
// their value.
func positionals(args, valued []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch {
		case slices.ContainsFunc(valued, func(v string) bool {
			return args[i] == v || strings.HasPrefix(v, "--") && !strings.Contains(args[i], "=") && isLong(args[i], v)
		}), args[i] == "<", args[i] == ">":
			i++
		case !strings.HasPrefix(args[i], "-"):
			out = append(out, args[i])
		}
	}
	return out
}

func branchOf(ref string) string {
	return strings.TrimPrefix(strings.TrimPrefix(ref, "+"), "refs/heads/")
}

func base(p string) string { return p[strings.LastIndex(p, "/")+1:] }

// at is s[i], or "" past its end.
func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// first is s[0], or def for none.
func first(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

// homes are what a path may start with for the home directory, case
// aside: ~, $HOME, and PowerShell's $env:USERPROFILE and $env:HOME.
var homes = []string{"~", "$home", "${home}", "$env:userprofile", "${env:userprofile}", "$env:home", "${env:home}"}

// drive: a Windows path from its drive's root (C:/, or C: alone);
// driveRoot: the root itself.
var (
	drive     = regexp.MustCompile(`^[A-Za-z]:(/|$)`)
	driveRoot = regexp.MustCompile(`^[A-Za-z]:/?$`)
)

// slash is p with Windows' backslashes as slashes.
func slash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// joined is p made absolute, not cleaned: ~ is home, a relative path is
// from the session's folder; backslashes are slashes.
func (r Rules) joined(p string) string {
	p = slash(p)
	for _, h := range homes {
		if len(p) >= len(h) && strings.EqualFold(p[:len(h)], h) && (len(p) == len(h) || p[len(h)] == '/') {
			p = slash(r.Home) + p[len(h):]
			break
		}
	}
	if !strings.HasPrefix(p, "/") && !drive.MatchString(p) {
		dir := r.Cwd
		if dir == "" {
			dir = "/"
			if len(r.Writable) > 0 {
				dir = r.Writable[0]
			}
		}
		p = slash(dir) + "/" + p
	}
	return p
}

// abs is p made absolute and clean: ~ is home, a relative path is from
// the session's folder. A Windows path keeps its drive (C:/Users/me);
// backslashes are slashes.
func (r Rules) abs(p string) string {
	p = r.joined(p)
	root := "/"
	if drive.MatchString(p) {
		root, p = p[:2]+"/", p[2:]
	}
	var parts []string
	for _, s := range strings.Split(p, "/") {
		switch s {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, s)
		}
	}
	return root + strings.Join(parts, "/")
}

// fold is p as paths are compared: with slashes, and case folded where
// the file system ignores case.
func (r Rules) fold(p string) string {
	p = slash(p)
	if r.CaseFold {
		return strings.ToLower(p)
	}
	return p
}

// under: p is root or inside it, by the file system's idea of case.
func (r Rules) under(p, root string) bool { return under(r.fold(p), r.fold(root)) }

func under(p, root string) bool {
	if root == "" {
		return false
	}
	if p == root {
		return true
	}
	if !strings.HasSuffix(root, "/") {
		root += "/"
	}
	return strings.HasPrefix(p, root)
}

var wildcard = regexp.MustCompile(`[*?[{].*$`)

// isSecret: p is a credential store or inside one (covers).
func (r Rules) isSecret(p string) bool { return r.covers(r.Secrets, p) }

// covers: p is one of roots or inside one (credential stores, a mod's
// reads); a glob counts by the part before its first wildcard, and
// covers a root it could reach, unless it is the root of the file
// system or the home folder.
func (r Rules) covers(roots []string, p string) bool {
	fixed := strings.TrimSuffix(wildcard.ReplaceAllString(p, ""), "/")
	if fixed == "" {
		fixed = "/"
	}
	root := fixed == "/" || driveRoot.MatchString(fixed)
	return slices.ContainsFunc(roots, func(s string) bool {
		return r.under(fixed, s) || (fixed != p && r.under(s, fixed) && !root && r.fold(fixed) != r.fold(r.Home))
	})
}

// otherTemp reports whether a file tool at p (absolute; a glob by the
// part before its first wildcard) reaches into OtherTemps outside
// OwnTemp.
func (r Rules) otherTemp(p string) bool {
	if r.OwnTemp != "" {
		fixed := strings.TrimSuffix(wildcard.ReplaceAllString(p, ""), "/")
		if fixed != "" && r.under(fixed, r.OwnTemp) {
			return false
		}
	}
	return r.covers(r.OtherTemps, p)
}

// isWorktreeRoot: removing p takes a worktree with it: the thread's own,
// tm's worktrees folder, a project's folder in it or a worktree in that.
func (r Rules) isWorktreeRoot(p string) bool {
	if r.Role == "thread" && len(r.Writable) > 0 && r.under(r.Writable[0], p) {
		return true
	}
	wt, p := r.fold(r.Worktrees), r.fold(p)
	if wt == "" {
		return false
	}
	if under(wt, p) {
		return true
	}
	if !under(p, wt) {
		return false
	}
	return len(strings.Split(p[len(wt)+1:], "/")) <= 2
}

// safe keeps a path or branch to the characters a journal line needs.
func safe(s string) string {
	var b strings.Builder
	n := 0
	for _, c := range s {
		ok := c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._/~@+:-", c))
		w := 1
		if c > 0xFFFF {
			w = 2 // two UTF-16 units, as the journal has always counted them
		}
		for range w {
			if n == 100 {
				return b.String()
			}
			if ok {
				b.WriteRune(c)
			} else {
				b.WriteByte('?')
			}
			n++
		}
	}
	return b.String()
}
