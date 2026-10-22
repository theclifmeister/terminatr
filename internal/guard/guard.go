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
//
// Rules for changing the parser and the judge:
//   - A group is walked as a line of its own.
//   - A loop is split at ; into its parts; a check that depends on the
//     order of the parts uses the loop's own parts (loops, from loopCds), not the flat list.
//   - Judging state is rolled back before an inner re-read, so a
//     re-read doesn't see the first read's effects.
//   - A trap's own action is skipped when its effects are followed
//     (Rules.inTrap): the shell runs no trap inside a trap's action.
//   - Command position is decided by cmdPos, not by looking at the
//     previous word.
//   - The elements of an array assignment are read as commands.
//   - Every re-read of text goes through Rules.cmds, so it is read with
//     the judge's budget at its depth.
//
// The floor paths the server hands in are built there with withLanding
// (internal/server): as written, plus the real path of the nearest
// existing folder with the rest joined on, never realPath of the parent
// alone. A PreToolUse hook that has no verdict never answers with
// empty output: agent.FailClosed gives the refusal.
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
	// NoWriteNames are NoWrite's patterns: absolute paths whose last part
	// is a glob (~/.zcompdump*, zsh's completion dumps, whose names carry
	// the host), matched when a call is judged (namedFloor).
	NoWriteNames []string `json:"noWriteNames,omitempty"`
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
	// CdPath: the session's shell may have CDPATH or zsh's cdpath set,
	// or cdable_vars on, before the line (the user's environment or
	// startup files, the agent's env file or shell snapshot), so a
	// relative cd may land elsewhere (nowhere.cd) as after a line that
	// sets it.
	CdPath bool `json:"cdPath,omitempty"`
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
	// physical: the line may make its cds physical; cdpath: it may set
	// CDPATH or zsh's cdpath, or turn on cdable_vars; pwdSet: it may set
	// $PWD, $OLDPWD or the folder stack; autocd: it may turn on autocd
	// (lineFlags).
	physical, cdpath, pwdSet, autocd bool
	// olds are the folders the line was in before its folder now
	// ($OLDPWD, ~-; "" one it can't tell), as judgeLine follows its cds.
	olds []string
	// wt is the thread's worktree, as judgeBash takes it (worktreeOf).
	wt string
	// trapFrom is where the trap actions the line being read sets start
	// in ps.traps (judgeLine).
	trapFrom int
	// sub: the line is a group's or a substitution's (walkLine), which
	// the traps set before it may run in too; inTrap: it is in a trap's
	// action, where they don't (the shell runs no trap in one); trapCwds,
	// for sub, the folders the line's trap actions may run in where it
	// starts (walkLine).
	sub, inTrap bool
	trapCwds    []string
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
	case p.over && p.checked:
		d = unreadable()
		d.Summary = "a command needing too many file-system checks"
		d.Message = fmt.Sprintf("terminatr guard: this command needs more file-system checks (links followed, script files looked up) than the guard makes for one call (%d), so it is refused; split it into shorter commands.", judgeParse/fsCheck)
	case p.over:
		d = unreadable()
	}
	return d
}

func (r Rules) judge(tool string, input map[string]any) *Denial {
	if !r.On || len(r.Rules) == 0 && len(r.Mods) == 0 && len(r.Own) == 0 && len(r.OtherTemps) == 0 && len(r.SecretEnv) == 0 && len(r.Floor) == 0 && len(r.NoWrite) == 0 && len(r.NoWriteNames) == 0 && !r.AskEscapes && len(r.Unsandboxed) == 0 && len(r.AskFirst) == 0 {
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
	if r.ps.check(r.depth) {
		return p
	}
	q := r.Resolve(p)
	if r.ps.resolved == nil {
		r.ps.resolved = map[string]string{}
	}
	r.ps.resolved[p] = q
	return q
}

// check counts a file-system check at depth against p's budget and
// reports whether the reading is over it.
func (p *parse) check(depth int) bool {
	was := p.over
	if p.stop(fsCheck, depth) {
		p.checked = p.checked || !was && !p.late
		return true
	}
	return false
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
	if r.ps.check(r.depth) {
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
	if r.covers(r.NoWrite, p) || r.namedFloor(p, false) {
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
	m := "a path given to a command that writes that starts with a folder the guard can't place ($OLDPWD or ~- from before the line, $PWD changed by an operator, $PWD, $OLDPWD or the folder stack after the line names them other than to read them, or after a cd $_, a substitution, a positional parameter or a variable the line sets) could be tm's settings or the project folder. Give the path from /, from a variable the line doesn't set ($TMPDIR/x), or relative after a cd to a folder written out; if none of that fits, " + tell + "."
	return &Denial{Rule: RuleTMFiles, Message: "terminatr guard (" + RuleTMFiles + "): " + m, Summary: summary}
}

// lostCd is the tm-files refusal of a relative path after a cd the
// guard can't follow: its sentence says how to write the line so it can.
func (r Rules) lostCd(summary string) *Denial {
	tell := "ask the user"
	if r.Role == "thread" {
		tell = "say so in your report"
	}
	m := "after a cd the guard can't follow (to a variable or a substitution, cd -, popd, or one inside ( ), an if or a loop), a relative path given to a command that writes could be tm's settings or the project folder. Give the path from / or a variable ($TMPDIR/x), cd to a folder written out and join the rest with &&, or run the cd in a call of its own; if none of that fits, " + tell + "."
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
		r.ps.traps, r.ps.defs, r.ps.moving, r.ps.links = nil, nil, false, nil
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
// moves nothing, but in a pipeline's last element (zsh's, and bash's
// with lastpipe, own shell) it leaves them unknown, as it does anywhere
// else (if, a loop, a substitution). A loop holding a cd is judged
// from unknown too (loopCds). A part's ( ) or { } (linePart's group) is
// a line of its own, judged from the part's folders: a cd in it moves
// them for the rest of the group, and a { } run in the line's own
// shell, with nothing before it, leaves the line in the folders the
// group may end in; a ( ) moves nothing after it. One the guard doesn't
// take as the group (linePart's loose) has the part judged from unknown
// too from there on, when it holds a cd; a ${ cmd; } is walked as a
// { }, and one holding a cd has the whole part judged from unknown too.
// A part that runs shell code the
// line doesn't show (eval, trap, a zsh trap function, source, ., also
// through command or builtin; mapfile -C, enable -f: runsCode) may move
// them at any point after, so the
// rest of the line keeps unknown among them; so may a definition the
// guard can't follow (hidesDefs), and a call of a function or an alias
// the line defines while one of them may move them. A loop's parts may
// run again: a cd in it leaves them unknown from its start (loops).
//
// A trap's action (judgeCommands keeps it in ps.traps, as set here or in
// a line this one reads: eval, bash -c) may run at any point from the
// trap on (a signal) and at the end (EXIT), so it is judged as eval's
// arguments are, from each folder the line itself may be in from the
// trap on and those it may end in: the trap leaves the folder unknown
// for the line's later commands, not for its action (lineDirs). A
// function's body, an alias's value, runs where the line calls it: it
// is kept and judged as a trap's action is (definitions).
func (r Rules) judgeLine(command string, cwds []string) *Denial {
	d, _, _ := r.walkLine(command, cwds)
	return d
}

// walkLine is judgeLine, and where the line may end: the folders its
// commands may be in and those its trap actions may run in (lineDirs).
// A group's or a substitution's walk (r.sub) judges the traps set before
// it too, from the folders the line's trap actions may run in there
// (r.trapCwds), which its own cds move.
func (r Rules) walkLine(command string, cwds []string) (d *Denial, st, ts lineDirs) {
	if r.line != "" {
		r.line += "\n"
	}
	r.line += command
	physical, cdpath, pwdSet, autocd := r.lineFlags(command)
	r.physical = r.physical || physical
	r.cdpath = r.cdpath || cdpath
	r.pwdSet = r.pwdSet || pwdSet
	r.autocd = r.autocd || autocd
	r.lineCd = r.lineCd || r.hasCd(command)
	parts, ok := r.topLevel(command)
	if !ok {
		// A line it can't split: one part, with any cd or hidden code in
		// it leaving its relative paths unknown from the start.
		parts = []linePart{{text: command, loose: -1}}
		if r.hasCd(command) || r.runsCode(command) {
			cwds = withDirs(cwds, "")
		}
	}
	// The folders $OLDPWD may hold: before the line, one it can't tell.
	olds := r.olds
	if olds == nil {
		olds = []string{""}
	}
	// st are the folders the line's commands may be in; ts those its
	// trap actions may run in: a trap leaves st unknown for the line's
	// later commands, but not ts, unless an action may move the folder
	// itself (a cd or hidden code in it).
	st = lineDirs{cur: cwds, list: cwds, olds: olds}
	ts = st
	// The trap actions set from here (or, in a group or a substitution,
	// those set before it too), and the folders the line may be in from
	// the first on.
	from := 0
	if r.ps != nil && (!r.sub || r.inTrap) {
		from = len(r.ps.traps)
	} else if r.trapCwds != nil {
		ts.cur, ts.list = r.trapCwds, r.trapCwds
	}
	r.sub, r.trapCwds = false, nil
	trapped := func() bool { return r.ps != nil && len(r.ps.traps) > from }
	ends := r.loopEnds(parts)
	loops := r.loopCds(parts, ends)
	var trapDirs []string
	seen, moves := from, false
	for i, pt := range parts {
		// The folders a part may run in count against the budget, and
		// a line that may be in more than maxFolders is past it.
		if r.ps != nil && (len(st.cur)+len(st.list)+len(st.olds)+len(ts.cur) > maxFolders || r.ps.stop(len(st.cur)*len(pt.text), r.depth)) {
			r.ps.over = true
		}
		if r.spent() {
			return nil, st, ts // Judge refuses the call: no part is let through unread
		}
		// A loop that holds a cd: each pass after the first starts where
		// the cd went.
		if loops[i] {
			st.cur, st.olds = withDirs(st.cur, ""), withDirs(st.olds, "")
			ts.cur = withDirs(ts.cur, "")
		}
		st.begin(r, pt)
		ts.begin(r, pt)
		// A loop's parts may run again after the ones that follow them:
		// the links it makes lead the writes before them.
		if e, ok := ends[i]; ok {
			span := loopSpan(parts, i, e)
			for _, c := range st.cur {
				rc := r
				rc.Cwd = c
				for _, words := range r.cmds(span) {
					r.keepLinks(rc.linksOf(strip(words)))
				}
			}
		}
		rp := r
		rp.olds = st.olds
		rp.trapFrom = from
		made := 0
		if r.ps != nil {
			made = len(r.ps.links)
		}
		var subs, funs []string
		if len(st.cur) == 0 {
			subs, funs = r.subsOf(pt.text)
		}
		for k, c := range st.cur {
			d, s, f := rp.judgeCommands(pt.text, c)
			if d != nil || r.spent() {
				return d, st, ts
			}
			if k == 0 {
				subs, funs = s, f
			}
		}
		// Its group's own commands, from the part's folders on, and
		// those of each substitution it runs in a subshell (the group's
		// own walk reads those in it), which moves nothing after. Read
		// again, they make their links in their own order.
		if r.ps != nil && (pt.group != nil || len(subs) > 0 || len(funs) > 0) {
			r.ps.links = r.ps.links[:made]
		}
		var gst, gts *lineDirs
		if g := pt.group; g != nil {
			subs, funs = r.subsOf(g.lead + " " + g.tail)
			rg := rp.nested()
			rg.sub, rg.trapCwds = true, ts.cur
			d, est, ets := rg.walkLine(g.inner, st.cur)
			if d != nil || r.spent() {
				return d, st, ts
			}
			gst, gts = &est, &ets
		}
		for _, in := range subs {
			rs := rp.nested()
			rs.sub, rs.trapCwds = true, ts.cur
			if d, _, _ := rs.walkLine(in, st.cur); d != nil || r.spent() {
				return d, st, ts
			}
		}
		// A ${ cmd; } runs in the line's own shell, as a { } does; one
		// that may move the folder leaves the part's command (expanded
		// before it runs) and what follows it unknown (end).
		lost := -1
		for _, in := range funs {
			rf := rp.nested()
			rf.sub, rf.trapCwds = true, ts.cur
			if d, _, _ := rf.walkLine(in, st.cur); d != nil || r.spent() {
				return d, st, ts
			}
			if r.hasCd(in) || r.runsCode(in) {
				lost = 0
			}
		}
		// A ( ) or { } the guard doesn't follow (loose), in a part that
		// holds a cd: from it on, the part is in a folder it can't tell.
		if lost < 0 && pt.loose >= 0 && r.hasCd(pt.text) {
			lost = pt.loose
		}
		if lost >= 0 && !slices.Contains(st.cur, "") {
			if d, _, _ := rp.judgeCommands(pt.text[lost:], ""); d != nil || r.spent() {
				return d, st, ts
			}
		}
		// zsh's trap functions (TRAPEXIT() { … }) are traps whose action
		// is the body. Any function's body, and an alias's value, run
		// where the line calls them, which the guard doesn't follow:
		// they are judged as a trap's action is, from each folder the
		// line may be in from the definition on, and a call of one when
		// one may move the folder leaves it unknown, as eval does.
		funcs := trapFuncs(pt.text)
		names, bodies := r.definitions(pt.text)
		for _, body := range append(funcs, bodies...) {
			if r.ps != nil && !slices.Contains(r.ps.traps[from:], body) {
				r.ps.traps = append(r.ps.traps, body)
			}
		}
		if r.ps != nil {
			for _, n := range names {
				if r.ps.defs == nil {
					r.ps.defs = map[string]bool{}
				}
				r.ps.defs[n] = true
			}
			for _, b := range bodies {
				rb := r.nested()
				r.ps.moving = r.ps.moving || rb.hasCd(b) || rb.runsCode(b) || rb.runsTrap(b) || rb.hidesDefs(b) || rb.callsDef(b)
			}
		}
		hidden := r.hidesDefs(pt.text) || r.ps != nil && r.ps.moving && r.callsDef(pt.text)
		if trapped() {
			trapDirs = withDirs(trapDirs, ts.cur...)
			for _, a := range r.ps.traps[seen:] {
				moves = moves || r.hasCd(a) || r.nested().runsCode(a) || r.nested().runsTrap(a) || r.ps.moving && r.nested().callsDef(a)
			}
			seen = len(r.ps.traps)
		}
		st.end(r, parts, i, gst, hidden || len(funcs) > 0 || r.runsCode(pt.text) || r.runsTrap(pt.text))
		ts.end(r, parts, i, gts, hidden || moves || r.runsCode(pt.text))
	}
	if !trapped() {
		return nil, st, ts
	}
	// The actions read here may set traps of their own, which their own
	// reading judges.
	rt := r.nested()
	rt.olds = withDirs(ts.olds, "")
	rt.inTrap = true
	for _, a := range slices.Clone(r.ps.traps[from:]) {
		if d := rt.judgeLine(a, withDirs(trapDirs, ts.cur...)); d != nil || r.spent() {
			return d, st, ts
		}
	}
	return nil, st, ts
}

// lineDirs are the folders a line may be in as judgeLine follows it:
// cur, where its next part runs; list, where its and-or list may have
// left it; olds, what $OLDPWD may hold; hidden, whether a part ran code
// that may have moved it where the guard can't see.
type lineDirs struct {
	cur, list, olds []string
	hidden          bool
}

// begin moves d to the start of part pt.
func (d *lineDirs) begin(r Rules, pt linePart) {
	if listEnd(pt.before) {
		d.list = d.cur
	}
	// A heredoc's body read as commands (topLevel and Commands may
	// tell a << apart differently: $[1<<2]) may hold a cd anywhere.
	if pt.doc && r.hasCd(pt.text) {
		d.lose()
	}
}

// lose adds the unknown folder to d's.
func (d *lineDirs) lose() {
	d.olds = withDirs(withDirs(d.olds, d.cur...), "")
	d.cur = withDirs(d.cur, "")
}

// end moves d past part i of parts, its cd followed; g, where the
// part's group may end (walkLine), nil for none; hidden: the part ran
// code that may move the folder where the guard can't see.
func (d *lineDirs) end(r Rules, parts []linePart, i int, g *lineDirs, hidden bool) {
	pt := parts[i]
	inPipe := pt.before == "|" || pt.after == "|" || pt.after == "&"
	if gr := pt.group; g != nil && gr.brace && gr.lead == "" && !inPipe && r.hasCd(pt.text) {
		// A { } in the line's own shell: where it may end, whether it
		// fails or not.
		d.olds, d.cur = g.olds, g.cur
		d.hidden = d.hidden || g.hidden
		if r.hasCd(gr.tail) {
			d.lose()
		}
	} else if cmd, args, plain := r.plainCd(pt.text); plain && !inPipe {
		var to []string
		for _, c := range d.cur {
			to = withDirs(to, nowhere{dir: c}.cd(r, cmd, args)...)
		}
		// The cd leaves $OLDPWD the folder it left, if it runs. One by
		// autocd (cmd "") may not: its word may be no folder, or autocd
		// off.
		sure := pt.after == "&&" && pt.before != "||" && cmd != ""
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
			d.olds = slices.Clone(d.cur)
		} else {
			d.olds = withDirs(d.olds, d.cur...)
		}
		switch {
		case up:
			d.cur, d.list = to, to
		case sure:
			d.cur = to
		case pt.after == "||":
			d.list = withDirs(d.list, to...)
		default:
			d.cur = withDirs(d.cur, to...)
		}
	} else if (!inPipe || lastPipe(pt)) && (pt.group == nil || pt.group.brace) && r.hasCd(pt.text) || !inPipe && pt.group != nil && pt.group.lead != "" && r.hasCd(pt.text) {
		// zsh, and bash with lastpipe, run a pipeline's last element in
		// the line's own shell.
		d.lose()
	}
	d.hidden = d.hidden || hidden
	if d.hidden {
		d.olds = withDirs(d.olds, "")
		d.cur = withDirs(d.cur, "")
	}
	d.list = withDirs(d.list, d.cur...)
	if listEnd(pt.after) {
		d.cur = d.list
	}
}

// lastPipe: pt is a pipeline's last element (echo | cd x).
func lastPipe(pt linePart) bool { return pt.before == "|" && pt.after != "|" }

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

// loopOpen are the words that open a loop whose body ends at done; the
// words before them in a part (loopLead) run them in the line's flow.
var (
	loopOpen = setOf("for while until select repeat")
	loopLead = setOf("! time -p if then elif else do")
)

// loopEnds are, for each part of parts that opens a loop (for, while,
// until, select, zsh's repeat), the part its done is in, or -1 when the
// guard finds none (done > log 2>&1 read elsewhere). zsh's short forms
// (for x (a b) { … }) are one part, with a bracket the guard doesn't
// follow (loose): no done closes them.
func (r Rules) loopEnds(parts []linePart) map[int]int {
	out := map[int]int{}
	var open []int
	for i, pt := range parts {
		w := strings.Fields(pt.text)
		k := 0
		for k < len(w)-1 && loopLead[w[k]] {
			k++
		}
		switch {
		case len(w) == 0:
		case loopOpen[w[k]] && pt.loose < 0:
			open = append(open, i)
		case w[0] == "done" && len(open) > 0:
			o := open[len(open)-1]
			open = open[:len(open)-1]
			out[o] = i
		}
	}
	for _, o := range open {
		out[o] = -1
	}
	return out
}

// loopCds are the parts that open a loop (loopEnds) whose parts hold a
// cd or code the guard can't see (runsCode): each pass after the first
// starts where the last one left the folder, so the loop is judged from
// an unknown folder too. A loop the guard finds no done for is judged
// from an unknown folder too.
func (r Rules) loopCds(parts []linePart, ends map[int]int) map[int]bool {
	out := map[int]bool{}
	for o, e := range ends {
		if e < 0 {
			out[o] = true
			continue
		}
		for _, pt := range parts[o : e+1] {
			if r.hasCd(pt.text) || r.runsCode(pt.text) {
				out[o] = true
				break
			}
		}
	}
	return out
}

// loopSpan is the text of the parts of the loop part o opens (loopEnds),
// to its done or the line's end.
func loopSpan(parts []linePart, o, e int) string {
	if e < 0 {
		e = len(parts) - 1
	}
	var b strings.Builder
	for _, pt := range parts[o : e+1] {
		b.WriteString(pt.text + "\n")
	}
	return b.String()
}

// hiddenCode are the builtins that run shell code the line doesn't show
// in the shell itself, where a cd in it moves the line's folder.
var hiddenCode = setOf("eval source . " + braceCode)

// braceCode is the command Commands adds after one whose name has braces
// that may give cd or one of hiddenCode ({c,x}d, {eval,x}), or braces it
// can't expand ({'c'd,x}): code the guard can't see, so the folder is
// unknown from there on (runsCode).
const braceCode = "\x00{}"

// runsCode: one of text's commands is one of those, maybe through
// command or builtin, or runs code of its own in the shell: bash's
// mapfile or readarray given a callback (-C, callbacks), enable loading
// a builtin from a shared object (-f).
func (r Rules) runsCode(text string) bool {
	if r.runs(text, hiddenCode) {
		return true
	}
	for _, words := range r.cmds(text) {
		argv := called(words)
		if len(argv) == 0 {
			continue
		}
		switch base(argv[0]) {
		case "mapfile", "readarray":
			if _, ok := callbacks(argv[1:]); ok {
				return true
			}
		case "enable":
			if slices.ContainsFunc(argv[1:], func(a string) bool {
				return len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 'f')
			}) {
				return true
			}
		}
	}
	return false
}

// mapfileOpts are mapfile's options that take a value.
const mapfileOpts = "dnOsucC"

// callbacks are the callbacks mapfile's or readarray's args give it
// (-C cb, -Ccb, -tC cb), which the shell runs as code with the line
// read added (cb INDEX LINE); ok, it is given one.
func callbacks(args []string) (cbs []string, ok bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || len(a) < 2 || a[0] != '-' {
			break
		}
		for k := 1; k < len(a); k++ {
			if !strings.ContainsRune(mapfileOpts, rune(a[k])) {
				continue
			}
			v := a[k+1:]
			if v == "" && i+1 < len(args) {
				i++
				v = args[i]
			}
			if a[k] == 'C' {
				cbs, ok = append(cbs, v), true
			}
			break
		}
	}
	return cbs, ok
}

// runsTrap: one of text's commands is trap. Its action, read as commands
// (judgeLine), may still call what the guard can't see from the line's
// later commands: a function or an alias the line defines in a way it
// can't follow, which may be a cd.
func (r Rules) runsTrap(text string) bool { return r.runs(text, setOf("trap")) }

// runs: one of text's commands is one of names, maybe through command or
// builtin.
func (r Rules) runs(text string, names map[string]bool) bool {
	for _, words := range r.cmds(text) {
		if argv := called(words); len(argv) > 0 && names[argv[0]] {
			return true
		}
	}
	return false
}

// called is the command words run (strip), past command and builtin.
func called(words []string) []string {
	argv := strip(words)
	for len(argv) > 1 && (argv[0] == "command" || argv[0] == "builtin") {
		argv = argv[1:]
	}
	return argv
}

// trapFunc is the head of a zsh trap function's definition: TRAPINT() or
// function TRAPEXIT.
var trapFunc = regexp.MustCompile(`(?:^|[\s;&|(){}])(?:function\s+TRAP[A-Z0-9]+(?:\s*\(\s*\))?|TRAP[A-Z0-9]+\s*\(\s*\))`)

// trapFuncs are the bodies of the trap functions text defines: what
// follows each head, to the part's end.
func trapFuncs(text string) []string {
	var out []string
	for _, m := range trapFunc.FindAllStringIndex(text, -1) {
		if b := strings.TrimSpace(text[m[1]:]); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// funcName is what a function's name may be: any word but the shell's
// operators, quotes, expansions and =.
const funcName = "[^\\s;&|(){}<>'\"$`=\\\\]+"

// funcHead is the head of a function's definition, any name: f(),
// function f or function f().
var funcHead = regexp.MustCompile(`(?:^|[\s;&|(){}])(?:function\s+(` + funcName + `)(?:\s*\(\s*\))?|(` + funcName + `)\s*\(\s*\))`)

// tableName is zsh's functions or aliases table, alone or with a
// subscript, as an assignment or a command that assigns names is given
// it (functions[f]=…, typeset functions, read -A aliases).
var tableName = regexp.MustCompile(`^(?:dis_)?(?:functions|g?aliases|saliases)(?:_source)?(?:\[|$)`)

// tableRef names one of those tables in an expansion
// (${functions[f]::=…}, ${aliases[x]=…}).
var tableRef = regexp.MustCompile(`\b(?:dis_)?(?:functions|g?aliases|saliases)(?:_source)?\b`)

// tableAssign: word w assigns one of those tables (functions[f]=…,
// aliases+=…), with key its subscript and val its value.
func tableAssign(w string) (key, val string, ok bool) {
	loc := assignment.FindStringIndex(w)
	if loc == nil {
		return "", "", false
	}
	n := strings.TrimSuffix(strings.TrimSuffix(w[:loc[1]], "="), "+")
	if !tableName.MatchString(n) {
		return "", "", false
	}
	if i := strings.IndexByte(n, '['); i >= 0 {
		key = strings.TrimSuffix(n[i+1:], "]")
	}
	return key, w[loc[1]:], true
}

// assignsExp: text has a parameter expansion (paramEnd, nested ones
// too) that assigns one of those tables, or a variable whose name it
// computes: zsh's (P) flag, ${…::=…} (whose name may be one), bash's
// ${!n=…}; or one the guard can't read.
func assignsExp(text string) bool {
	if strings.Contains(text, "::=") {
		return true
	}
	s := []rune(text)
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '$' || s[i+1] != '{' {
			continue
		}
		end, ok := paramEnd(s, i)
		if !ok {
			return true
		}
		body := string(s[i+2 : min(end, len(s))])
		switch {
		case strings.HasPrefix(body, "(") && strings.ContainsRune(body[:max(strings.IndexByte(body, ')'), 0)], 'P'),
			strings.HasPrefix(body, "(") && !strings.Contains(body, ")"),
			strings.HasPrefix(body, "!") && strings.Contains(body, "="),
			tableRef.MatchString(body) && strings.Contains(body, "="):
			return true
		}
	}
	return false
}

// nameTakers are the commands whose arguments name the variables they
// assign.
var nameTakers = setOf("read typeset declare export local readonly integer float mapfile readarray vared getopts")

// definitions are the names and bodies of the functions part defines
// (what follows each head, to the part's end), and of its aliases and
// assignments to zsh's functions and aliases tables (their values).
func (r Rules) definitions(part string) (names, bodies []string) {
	for _, m := range funcHead.FindAllStringSubmatchIndex(part, -1) {
		if m[2] >= 0 {
			names = append(names, part[m[2]:m[3]])
		} else {
			names = append(names, part[m[4]:m[5]])
		}
		if b := strings.TrimSpace(part[m[1]:]); b != "" {
			bodies = append(bodies, b)
		}
	}
	for _, words := range r.cmds(part) {
		for _, w := range words {
			if key, val, ok := tableAssign(w); ok {
				names = append(names, key)
				bodies = append(bodies, val)
			}
		}
		if argv := strip(words); len(argv) > 0 && base(argv[0]) == "alias" {
			for _, a := range argv[1:] {
				if n, v, ok := strings.Cut(a, "="); ok && !strings.HasPrefix(a, "-") {
					names = append(names, n)
					bodies = append(bodies, v)
				}
			}
		}
	}
	return names, bodies
}

// callsDef: one of text's commands is a function or an alias the lines
// read define.
func (r Rules) callsDef(text string) bool {
	return r.ps != nil && len(r.ps.defs) > 0 && r.runs(text, r.ps.defs)
}

// hidesDefs: text may define a function or an alias where the guard
// can't follow it, or whose calls it can't tell: alias (zsh's global
// ones expand anywhere in a line), autoload, enable, functions given
// arguments (zsh's functions -c copies one), an assignment to zsh's
// functions or aliases tables, direct (functions[TRAPDEBUG]=…, typeset
// aliases, ${functions[f]::=…}) or by a name given indirectly
// (indirect), which may be one of them. Any of those may be a cd the
// line runs later, or a trap's action calls.
func (r Rules) hidesDefs(text string) bool {
	if assignsExp(text) {
		return true
	}
	for _, words := range r.cmds(text) {
		if slices.ContainsFunc(words, func(w string) bool { _, _, ok := tableAssign(w); return ok }) {
			return true
		}
		argv := strip(words)
		if len(argv) == 0 {
			continue
		}
		switch name := base(argv[0]); {
		case name == "alias" && slices.ContainsFunc(argv[1:], func(a string) bool { return strings.Contains(a, "=") }),
			name == "autoload", name == "enable", name == "functions" && len(argv) > 1,
			nameTakers[name] && slices.ContainsFunc(argv[1:], tableName.MatchString):
			return true
		}
		if indirect(argv) {
			return true
		}
	}
	return false
}

// indirect: command argv assigns a variable whose name it computes: a
// name argument of nameTakers' (the part before an =) or printf's and
// print's -v with an expansion in it. That name may be CDPATH or
// cdpath, zsh's options, functions or aliases, or anything else.
func indirect(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	name := base(argv[0])
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case nameTakers[name]:
			if n, _, _ := strings.Cut(a, "="); strings.ContainsAny(n, "$`") {
				return true
			}
		case (name == "printf" || name == "print") && len(a) > 1 && a[0] == '-' && a[1] != '-':
			if k := strings.IndexByte(a, 'v'); k > 0 {
				n := a[k+1:]
				if n == "" && i+1 < len(argv) {
					n = argv[i+1]
				}
				if strings.ContainsAny(n, "$`") {
					return true
				}
			}
		}
	}
	return false
}

// trapAction is the action trap's args set ("" for none: a listing, a
// reset, a signal ignored); false for one the guard can't read: a
// command named with a variable or a substitution anywhere in it
// ("$cleanup", $(cat f), g${x}t), or an action word that lost an
// unquoted substitution glued to its text ("gh pr "$(echo merge)" 1").
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
	if r.ps != nil && r.ps.glued[a] {
		return a, false
	}
	for _, words := range r.cmds(a) {
		if argv := strip(words); len(argv) > 0 && strings.ContainsAny(argv[0], "$`") {
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
// there), a command's first word may be what it writes. subs and funs
// are the part's substitutions as its reading found them (subsOf).
func (r Rules) judgeCommands(part, cwd string) (d *Denial, subs, funs []string) {
	fl := r
	fl.Cwd = cwd
	if cwd != "" {
		r.Cwd = cwd
	}
	var read [][]string
	if r.ps != nil {
		read, subs, funs = r.ps.readSubs(part, func(p string) bool { return fl.worktreeScript(p) }, r.depth)
	} else {
		read = readCommands(part, false, func(p string) bool { return fl.worktreeScript(p) })
	}
	return r.judgeWords(read, cwd), subs, funs
}

// judgeWords is judgeCommands of a part's commands as read.
func (r Rules) judgeWords(read [][]string, cwd string) *Denial {
	fl := r
	fl.Cwd = cwd
	if cwd != "" {
		r.Cwd = cwd
	}
	split := false
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
		// The links a command makes lead the line's writes after it
		// (a loop's, those before it too: walkLine), not its own (ln -s
		// ~/x l: l/x is no write).
		own := fl.linksOf(argv)
		var links []madeLink
		if r.ps != nil && len(own) > 0 {
			links = r.ps.links
			r.ps.links = slices.DeleteFunc(slices.Clone(links), func(l madeLink) bool { return slices.Contains(own, l) })
		}
		dw := fl.judgeNoWrite(cmd, args, words)
		if links != nil {
			r.ps.links = links
		}
		if dw != nil {
			return dw
		}
		r.keepLinks(own)
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
		case cmd == "mapfile" || cmd == "readarray":
			// Its callback runs as eval's arguments do, for each line read.
			cbs, _ := callbacks(args)
			for _, cb := range cbs {
				if d = r.nested().judgeLine(cb, []string{cwd}); d != nil {
					break
				}
			}
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

// cd is where cmd (cd, pushd, popd) with args may leave the line ("" a
// folder it can't tell): a
// folder it names plainly, home for none, else unknown: an entry of the
// folder stack (+N, -N) may be any folder, a pattern or braces (cd
// ~/.term*) may name another, and so may a relative one CDPATH looks
// up or cdable_vars takes for a variable's name, when the line names it
// (lineFlags) or the session may have it set (CdPath). A command autocd
// may take for a cd (autoCd) is followed as one with cmd "". Its redirections (pushd x >/dev/null) are no folder. A
// physical cd (cd -P, or one the line may have made physical:
// physicalCd) leaves the folder with its links
// followed (Resolve), so its .. is the real parent; where the guard
// can't follow them, or can't tell a logical cd from a physical one
// that lands elsewhere, the folder is unknown. A logical cd whose folder
// has a .. lands where it says, or, where that is no folder, bash takes
// it physically: where Resolve says too, or unknown without Resolve.
func (n nowhere) cd(r Rules, cmd string, args []string) []string {
	var to []string
	mode := byte(0)
	for _, a := range unredirected(args) {
		if stackIndex.MatchString(a) {
			return []string{""} // cd +1, pushd -2: an entry of the folder stack
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			to = append(to, a)
		} else if i := strings.LastIndexAny(a, "LP"); i > 0 && !strings.HasPrefix(a, "--") {
			mode = a[i]
		}
	}
	switch {
	case cmd == "popd" || len(to) > 1:
		return []string{""}
	case len(to) == 0:
		if cmd == "pushd" {
			return []string{""}
		}
		return []string{r.Home}
	}
	a := to[0]
	if a == "-" || strings.ContainsAny(a, "$`*?[{") || strings.HasPrefix(a, "~") && !strings.HasPrefix(a, "~/") && a != "~" {
		return []string{""}
	}
	if !dotted.MatchString(slash(a)) && !strings.HasPrefix(a, "~") && !drive.MatchString(slash(a)) && (r.cdpath || r.CdPath) {
		return []string{""}
	}
	if n.dir == "" && !strings.HasPrefix(a, "/") && !strings.HasPrefix(a, "~") && !drive.MatchString(slash(a)) {
		return []string{""}
	}
	// On a line that may set $PWD (pwdSet), a logical cd to a relative
	// folder may be taken from it (zsh does) rather than from where the
	// guard follows the line.
	if r.pwdSet && mode != 'P' && !strings.HasPrefix(a, "/") && !strings.HasPrefix(a, "~") && !drive.MatchString(slash(a)) {
		return []string{""}
	}
	r.Cwd = n.dir
	dir := r.abs(a)
	if mode == 'P' || mode == 0 && r.physical {
		if r.Resolve == nil {
			return []string{""}
		}
		real := r.abs(r.resolve(r.joined(a)))
		if mode == 0 && real != dir {
			return []string{""}
		}
		return []string{real}
	}
	if parent.MatchString(slash(a)) {
		if r.Resolve == nil {
			return []string{""}
		}
		return withDirs([]string{dir}, r.abs(r.resolve(r.joined(a))))
	}
	return []string{dir}
}

// stackIndex is an argument of cd, pushd or popd that names an entry of
// the folder stack (+N, -N), not a folder or an option.
var stackIndex = regexp.MustCompile(`^[+-][0-9]+$`)

// maxAlts bounds the words a word's braces may give (braceAlts).
const maxAlts = 4096

// braceSeq is the inside of a {x..y} or {x..y..n} sequence: of numbers,
// or of letters.
var braceSeq = regexp.MustCompile(`^(?:(-?[0-9]+)\.\.(-?[0-9]+)|([A-Za-z])\.\.([A-Za-z]))(?:\.\.(-?[0-9]+))?$`)

// braceAlts are the words brace expansion gives of w: each alternative
// of a {a,b} (nested ones too) and each word of a {x..y} sequence, a
// ${…} aside; w alone for none; nil for more than maxAlts, or more
// than maxParse bytes. Words are read with their quotes gone, so a
// quoted brace counts too.
func braceAlts(w string) []string {
	var out []string
	size := 0
	var expand func(w string) bool
	expand = func(w string) bool {
		pre, alts, post, ok := braceSplit(w)
		if !ok {
			out = append(out, w)
			size += len(w)
			return len(out) <= maxAlts && size <= maxParse
		}
		for _, a := range alts {
			if !expand(pre + a + post) {
				return false
			}
		}
		return true
	}
	if !expand(w) {
		return nil
	}
	return out
}

// braceEnd is the index of the } that closes the brace expansion ({a,b},
// {1..3}, x{a,{b,c}}) opened at s[i], or -1: a { with no comma or
// sequence before its close, or a blank, a quote, an expansion or an
// operator, stays a separator (Commands); and how many runes it read.
func braceEnd(s []rune, i int) (end, n int) {
	depth := 0
	for j := i; j < len(s); j++ {
		switch c := s[j]; {
		case strings.ContainsRune(" \t\n;&|<>()'\"`$\\", c):
			return -1, j - i + 1
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth == 0 {
				if _, _, _, ok := braceSplit(string(s[i : j+1])); ok {
					return j, 2 * (j - i + 1)
				}
				return -1, 2 * (j - i + 1)
			}
		}
	}
	return -1, len(s) - i
}

// hiddenBrace: the { at s[i] opens a brace expansion braceEnd leaves
// alone, one with a quote, an escape or an expansion in it ({'c'd,x},
// {$c,x}), which the shell still expands: a comma or a .. at its top
// level before its close, and no unquoted blank or operator; and how
// many runes it read.
func hiddenBrace(s []rune, i int) (ok bool, n int) {
	depth, split := 0, false
	j := i
	for ; j < len(s); j++ {
		switch c := s[j]; {
		case strings.ContainsRune(" \t\n;&|<>()", c):
			return false, j - i + 1
		case c == '\'':
			q := slices.Index(s[j+1:], '\'')
			if q < 0 {
				return false, len(s) - i
			}
			j += 1 + q
		case c == '"':
			for j++; j < len(s) && s[j] != '"'; j++ {
				e, ok := wordSkip(s, j, true)
				if !ok {
					return false, len(s) - i
				}
				j = e
			}
		case c == '$' && j+1 < len(s) && s[j+1] == '\'':
			j = ansiEnd(s, j+2)
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth == 0 {
				return split, j - i + 1
			}
		case depth == 1 && (c == ',' || c == '.' && j+1 < len(s) && s[j+1] == '.'):
			split = true
		default:
			e, ok := wordSkip(s, j, false)
			if !ok {
				return false, len(s) - i
			}
			j = e
		}
	}
	return false, len(s) - i
}

// cmdPos: the word after words is a command's name, as strip takes it:
// past assignments, redirects, wrappers with their options (command,
// builtin, time -p, env …) and reserved words (if, then, do, ! …).
func cmdPos(words []string) bool {
	const next = "\x00next"
	argv := strip(append(slices.Clone(words), next))
	return len(argv) > 0 && argv[0] == next
}

// braceSplit is w's first brace expansion (braceAlts): the text before
// it, its words and the text after it; false for none.
func braceSplit(w string) (pre string, alts []string, post string, ok bool) {
	for i := 0; i < len(w); i++ {
		if w[i] != '{' {
			continue
		}
		param := i > 0 && w[i-1] == '$'
		depth, commas, end := 0, []int{}, -1
		for j := i; j < len(w) && end < 0; j++ {
			switch {
			case w[j] == '{':
				depth++
			case w[j] == '}':
				if depth--; depth == 0 {
					end = j
				}
			case w[j] == ',' && depth == 1:
				commas = append(commas, j)
			}
		}
		switch {
		case end < 0:
			continue
		case param:
			i = end // ${…}: no braces of the path's
			continue
		case len(commas) > 0:
			from := i + 1
			for _, c := range append(commas, end) {
				alts = append(alts, w[from:c])
				from = c + 1
			}
			return w[:i], alts, w[end+1:], true
		}
		if m := braceSeq.FindStringSubmatch(w[i+1 : end]); m != nil {
			return w[:i], braceRange(m), w[end+1:], true
		}
	}
	return "", nil, "", false
}

// braceRange is the words of the sequence m matched (braceSeq), at most
// maxAlts+1 of them: numbers padded to the width of one given with a
// leading zero.
func braceRange(m []string) []string {
	step, _ := strconv.Atoi(strings.TrimPrefix(m[5], "-"))
	step = max(step, 1)
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	width := 0
	for _, n := range m[1:3] {
		if d := strings.TrimPrefix(n, "-"); len(d) > 1 && d[0] == '0' {
			width = max(width, len(n))
		}
	}
	letters := m[3] != ""
	if letters {
		a, b = int(m[3][0]), int(m[4][0])
	}
	if a > b {
		step = -step
	}
	var out []string
	for c := a; len(out) <= maxAlts && (step > 0 && c <= b || step < 0 && c >= b); c += step {
		if letters {
			out = append(out, string(rune(c)))
		} else {
			out = append(out, fmt.Sprintf("%0*d", width, c))
		}
	}
	return out
}

// dotsParent is path w with each part a pattern that starts with a dot
// (.*, .?, .[.], ..*: bash's globs match a leading dot only when it is
// written) made .., which the link check counts: any may match .. in
// some shell (extglob, zsh's qualifiers).
func dotsParent(w string) string {
	parts := strings.Split(w, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ".") && strings.ContainsAny(p, "*?[") {
			parts[i] = ".."
		}
	}
	return strings.Join(parts, "/")
}

// physicalSetters are the commands whose words may make cds physical:
// set, setopt, unsetopt, shopt and the shells.
var physicalSetters = func() map[string]bool {
	m := setOf("set setopt unsetopt shopt")
	for c := range shells {
		m[c] = true
	}
	return m
}()

// physicalOpts are the options that make cds physical, as optName gives
// them: bash's physical, zsh's chase_links and chase_dots.
var physicalOpts = setOf("physical chaselinks chasedots")

// optName is a shell option's name as zsh compares them: case, _ and -
// aside, without a no that turns it off (nochaselinks), so either way
// counts.
func optName(o string) string {
	o = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(o))
	return strings.TrimPrefix(o, "no")
}

// cdPath names CDPATH or zsh's cdpath, any case.
var cdPath = regexp.MustCompile(`(?i)cd_?path`)

// optionsWord is a word that assigns zsh's options (options[x]=,
// options=, options+=) or names it to a command that may (typeset
// options, read options, printf -v options).
var optionsWord = regexp.MustCompile(`^options(?:\[|\+?=|$)`)

// optsEnv is a word that assigns bash's SHELLOPTS or BASHOPTS, in any
// form (assignment).
var optsEnv = assignOf(`SHELLOPTS|BASHOPTS`)

// lineFlags reads line's commands, their words dequoted and their names
// by base name (/bin/bash), for what the guard can't follow:
//
// physical, one may make its cds physical: set, setopt, unsetopt, shopt
// or a shell given -P (zsh's -w, set -w), an option physicalOpts names
// (set -o physical, shopt -so physical, setopt CHASE_LINKS, unsetopt
// nochaselinks), or physicalCd's words. Either way it counts, so a line
// turning it off does too.
//
// cdpath, it names CDPATH or zsh's cdpath in any form (an assignment,
// typeset, +=, an indirect name n=CDPATH, any case), in its text or its
// words dequoted (C\DPATH), or one may turn on bash's cdable_vars or
// zsh's cdablevars (shopt -s cdable_vars, setopt CDABLE_VARS, zsh's -T,
// set -T): then a relative cd may land elsewhere, a folder a variable
// holds.
//
// pwdSet, it names PWD, OLDPWD, DIRSTACK or zsh's dirstack other than
// to expand it (namesPWD), in its text or its words dequoted: then
// $PWD, ~+, pwd, $OLDPWD, ~- and the folder stack may not be the
// folders the line's cds leave.
//
// autocd, one may turn on zsh's or bash's autocd (setopt AUTO_CD, zsh's
// -J, shopt -s autocd): then any command word alone may be a cd
// (autoCd).
//
// All of them (but pwdSet), a command that may set options the guard
// can't tell: one with an expansion in an option argument (setopt $o);
// emulate; any assignment to zsh's options (optionsWord); a SHELLOPTS=
// or BASHOPTS= one in any form, alone or through env; a command named
// by an expansion; and a name it assigns indirectly (indirect: read $n,
// typeset "$n=x", printf -v "$n"; zsh's (P) flag, ${…::=…}), which may
// be CDPATH or zsh's options.
func (r Rules) lineFlags(line string) (physical, cdpath, pwdSet, autocd bool) {
	cdpath = cdPath.MatchString(line)
	pwdSet = namesPWD(line)
	opts := assignsExp(line)
	for _, words := range r.cmds(line) {
		if indirect(strip(words)) {
			opts = true
		}
		for _, w := range words {
			if namesPWD(w) {
				pwdSet = true
			}
			if optsEnv.MatchString(w) || optionsWord.MatchString(w) {
				opts = true
			}
			if cdPath.MatchString(w) {
				cdpath = true
			}
		}
		argv := strip(words)
		if len(argv) == 0 {
			continue
		}
		name := base(argv[0])
		if name == "emulate" || strings.ContainsAny(argv[0], "$`") {
			opts = true
		}
		if !physicalSetters[name] {
			continue
		}
		if physicalCd.MatchString(strings.Join(argv, " ")) {
			physical = true
		}
		zsh := name == "set" || name == "setopt" || name == "unsetopt" || shells[name]
		for _, a := range argv[1:] {
			if strings.ContainsAny(a, "$`") {
				opts = true
				continue
			}
			if len(a) > 1 && (a[0] == '-' || a[0] == '+') && a[1] != '-' {
				if strings.ContainsRune(a, 'P') || (name == "set" || shells[name]) && strings.ContainsRune(a, 'w') {
					physical = true
				}
				if zsh && strings.ContainsRune(a, 'T') {
					cdpath = true
				}
				if zsh && strings.ContainsRune(a, 'J') {
					autocd = true
				}
				continue
			}
			switch o := optName(a); {
			case physicalOpts[o]:
				physical = true
			case o == "cdablevars":
				cdpath = true
			case o == "autocd":
				autocd = true
			}
		}
	}
	if opts {
		physical, cdpath, autocd = true, true, true
	}
	return physical, cdpath, pwdSet, autocd
}

// pwdExpanded is an expansion of PWD, OLDPWD, DIRSTACK or dirstack that
// only reads it: $PWD, ${PWD}, ${#PWD}, ${PWD:-x}, ${PWD%/*},
// ${DIRSTACK[1]}; not ${PWD:=x} or ${PWD=x}, which may assign it, nor
// one whose body has a $, a backtick or a brace in it, where an
// expansion nested in it may (${PWD:-${PWD:=x}}): those count as naming
// it, failing safe.
var pwdExpanded = regexp.MustCompile("\\$(?:PWD|OLDPWD|DIRSTACK|dirstack)\\b|\\$\\{[#!]?(?:PWD|OLDPWD|DIRSTACK|dirstack)(?:\\[[^]$`{}]*\\])?(?:[^:=}$`{][^}$`{]*|:[^=}$`{][^}$`{]*)?\\}")

// pwdName is PWD, OLDPWD, DIRSTACK or dirstack as a name of its own.
var pwdName = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])(?:PWD|OLDPWD|DIRSTACK|dirstack)(?:[^A-Za-z0-9_]|$)`)

// namesPWD: text names PWD, OLDPWD, DIRSTACK or dirstack other than to
// expand it, in any form: an assignment (PWD=, +=, [0]=, ${PWD:=…}),
// read, declare, typeset, local, export, unset, printf -v, for, an
// indirect name (n=PWD, declare -n), or text that mentions it.
func namesPWD(text string) bool {
	return pwdName.MatchString(pwdExpanded.ReplaceAllString(text, ""))
}

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
// the line's flow (groupLead) come before it; loose, the index in text
// of the first one it opens that isn't group (-1: none), which the
// guard doesn't follow (coproc { }, zsh's for x (a) { }, the second
// block of { } always { }). A $( ), <( ), >( ) or zsh's =( ) and an
// array's a=( ) are none: its walk reads what they run.
type linePart struct {
	text, before, after string
	doc                 bool
	group               *lineGroup
	loose               int
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
var cdWords = setOf("cd pushd popd chdir")

// hasCd: some word of text's commands is cd, pushd, popd or zsh's chdir,
// or one of them may be a cd by autocd (autoCd): not the elements of an
// array, which Commands reads as a command after the assignment's
// (a=(~/x): [a=] [~/x]).
func (r Rules) hasCd(text string) bool {
	cmds := r.cmds(text)
	for i, words := range cmds {
		if slices.ContainsFunc(words, func(w string) bool { return cdWords[w] }) {
			return true
		}
		if i > 0 && len(cmds[i-1]) > 0 && strings.HasSuffix(cmds[i-1][len(cmds[i-1])-1], "=") {
			continue
		}
		if _, ok := r.autoCd(words); ok {
			return true
		}
	}
	return false
}

// autoCd is the folder simple command words may cd to by autocd, which
// a shell reading its commands from its input may have on (zsh's, its
// startup files may turn it on, and bash's interactive one): a command
// word alone, its redirections aside, that is a path (one with a /, ..,
// ~…), or any word when the line may turn autocd on; assignments and
// the keywords of groupLead may come before it, not a wrapper (sudo
// x, command x run x). The shell cds there when it is a folder.
func (r Rules) autoCd(words []string) (dir string, ok bool) {
	argv := unredirected(words)
	for len(argv) > 1 && (assignment.MatchString(argv[0]) || groupLead[argv[0]]) {
		argv = argv[1:]
	}
	if len(argv) != 1 || assignment.MatchString(argv[0]) {
		return "", false
	}
	w := argv[0]
	if r.autocd || strings.Contains(w, "/") || w == ".." || strings.HasPrefix(w, "~") {
		return w, true
	}
	return "", false
}

// plainCd: part is one plain cd, pushd or popd, and its arguments; or a
// command autocd may take as a cd (autoCd), with cmd "".
func (r Rules) plainCd(part string) (cmd string, args []string, ok bool) {
	if strings.HasPrefix(part, "(") || strings.HasPrefix(part, "{") {
		return "", nil, false
	}
	cmds := r.cmds(part)
	if len(cmds) != 1 || len(cmds[0]) == 0 {
		return "", nil, false
	}
	if dir, ok := r.autoCd(cmds[0]); ok && !cdWords[cmds[0][0]] {
		return "", []string{dir}, true
	}
	if !cdWords[cmds[0][0]] {
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
	// its close, in b, and the next it opens (-1: none yet); opened, the
	// bracket open at the top level is gOpen's.
	gOpen, gClose, gLoose := -1, -1, -1
	opened := false
	openAt := func(at int) {
		switch {
		case gOpen < 0:
			gOpen, opened = at, true
		case gLoose < 0:
			gLoose = at
		}
	}
	closeAt := func(at int) {
		if opened {
			gClose, opened = at, false
		}
	}
	cut := func(sep string) {
		raw := b.String()
		if t := strings.TrimSpace(raw); t != "" {
			pt := linePart{text: t, before: before, loose: -1}
			off := len(raw) - len(strings.TrimLeftFunc(raw, unicode.IsSpace))
			if gClose > 0 {
				g := &lineGroup{lead: strings.TrimSpace(raw[:gOpen]), inner: raw[gOpen+1 : gClose-1], tail: raw[gClose:], brace: raw[gOpen] == '{'}
				switch {
				case slices.ContainsFunc(strings.Fields(g.lead), func(w string) bool { return !groupLead[w] }):
					pt.loose = gOpen - off
				case gLoose >= 0:
					pt.group, pt.loose = g, gLoose-off
				default:
					pt.group = g
				}
			}
			parts = append(parts, pt)
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
		gOpen, gClose, gLoose, opened = -1, -1, -1, false
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
			case funStart(s, i):
				j, ok := funEnd(s, i, true)
				if !ok {
					return nil, false // read whole: Commands refuses it
				}
				b.WriteString(string(s[i+1 : j+1]))
				i = j
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
			if funStart(s, i) {
				// bash 5.3's ${ cmd; }: to its close, a word of the part.
				end, ok := funEnd(s, i, false)
				if !ok {
					return nil, false // read whole: Commands refuses it
				}
				j = end
			} else if _, ok := paramEnd(s, i); !ok {
				return nil, false
			}
			if j == len(s) {
				return nil, false
			}
			b.WriteString(string(s[i : j+1]))
			i = j
			continue
		case c == '(':
			if len(stack) == 0 && !strings.ContainsRune("$<>=", prev) {
				openAt(b.Len())
			}
			stack = append(stack, '(')
		case c == ')':
			if top() != '(' {
				return nil, false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				closeAt(b.Len() + 1)
			}
		case c == '{' && wordStart(i) && (next == ' ' || next == '\t' || next == '\n'):
			if len(stack) == 0 {
				openAt(b.Len())
			}
			stack = append(stack, '{')
		case c == '}' && top() == '{' && wordStart(i):
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				closeAt(b.Len() + 1)
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
	if len(r.NoWrite) == 0 && len(r.NoWriteNames) == 0 {
		return nil
	}
	var refuse func(w string, parents bool) *Denial
	refuse = func(w string, parents bool) *Denial {
		// A descriptor (2>&1) or a process substitution (> >(tee f)).
		if w == "" || strings.HasPrefix(w, "&") || strings.HasPrefix(w, "<(") || strings.HasPrefix(w, ">(") {
			return nil
		}
		// Braces give each of their words (touch {a,b}): each is judged.
		switch alts := braceAlts(w); {
		case alts == nil:
			return floor(r.deny(RuleTMFiles, cmd+" of "+safe(w), ""))
		case len(alts) > 1:
			for _, a := range alts {
				if dd := refuse(a, parents); dd != nil {
					return dd
				}
			}
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
			case r.pwdSet:
				kind = "unknown" // the line may have set it (namesPWD)
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
		// cd, it is unknown. A pattern that may match .. (.*) counts as
		// one. A path without one may go through a link an earlier call
		// made (into the project folder, tm's home): it is judged where
		// Resolve says it lands too, whatever its text. And it may go
		// through one the line makes, which isn't there yet (linksOf).
		lands := []string{r.abs(w)}
		pw := dotsParent(w)
		dots := parent.MatchString(slash(pw))
		if dots {
			lands = withDirs(lands, r.abs(pw))
		}
		switch {
		case r.Resolve != nil:
			lands = withDirs(lands, r.abs(r.resolve(r.joined(pw))))
		case dots && r.lineCd:
			return floor(r.lostCd(cmd + " of " + safe(w) + " after a cd"))
		}
		lands, ok := r.throughLinks(lands)
		if !ok {
			return floor(r.unplaced(cmd + " of " + safe(w) + " through a link"))
		}
		for _, p := range lands {
			fixed := strings.TrimSuffix(wildcard.ReplaceAllString(p, ""), "/")
			if fixed == "" {
				fixed = "/"
			}
			if r.covers(r.NoWrite, p) || r.namedFloor(p, parents) || parents && slices.ContainsFunc(r.NoWrite, func(n string) bool { return r.under(n, fixed) || r.globReaches(p, n) }) {
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
	// The paths given, and the targets given with -t (cp -vt d, -td); -T
	// says the target is no folder; install -d makes every path given.
	var paths, targets []string
	noTarget := false
	args = unredirected(args)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-t" || a == "--target-directory") && i+1 < len(args):
			i++
			targets = append(targets, args[i])
		case strings.HasPrefix(a, "--target-directory="):
			targets = append(targets, strings.TrimPrefix(a, "--target-directory="))
		case isLong(a, "--no-target-directory"):
			noTarget = true
		case cmd == "install" && isLong(a, "--directory"):
			all = true
		case intoCmds[cmd] && shortFlags.MatchString(a):
			if cmd == "install" && strings.Contains(a, "d") {
				all = true
			}
			noTarget = noTarget || strings.Contains(a, "T")
			if k := strings.IndexByte(a, 't'); k > 0 {
				if t := a[k+1:]; t != "" {
					targets = append(targets, t)
				} else if i+1 < len(args) {
					i++
					targets = append(targets, args[i])
				}
			}
		case !strings.HasPrefix(a, "-"):
			paths = append(paths, a)
		}
	}
	// cp, install and ln into a folder write each source's name in it:
	// unless -T says otherwise, a target may be one (ln with one path
	// links into the line's folder).
	var into []string
	if intoCmds[cmd] && !all && !noTarget {
		dirs, srcs := targets, paths
		switch {
		case len(dirs) > 0:
		case len(paths) == 1 && cmd == "ln":
			dirs = []string{"."}
		case len(paths) > 0:
			dirs, srcs = paths[len(paths)-1:], paths[:len(paths)-1]
		}
		for _, t := range dirs {
			for _, s := range srcs {
				into = append(into, intoFolder(t, s))
			}
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
	for _, p := range append(paths, into...) {
		if d := refuse(p, recursive); d != nil {
			return d
		}
	}
	return nil
}

// madeLink is a link a line makes (linksOf), which isn't there yet
// for Resolve to follow: its name, absolute, or as written when it is
// relative to a folder the guard can't tell (rel), or any path at all
// (any: an expansion or a pattern gives it); and where it leads, as
// written ("" for one the guard can't read), from the link's folder
// (sym) or absolute.
type madeLink struct {
	name, to  string
	rel, any  bool
	sym, lost bool
}

// linksOf are the links command argv makes (ln, cp -s, cp -l): each
// source linked at the target, or into it (intoFolder), from the
// line's folder (r.Cwd, "" unknown). judgeCommands keeps them in the
// judge's reading (ps.links) for the line's writes.
func (r Rules) linksOf(argv []string) []madeLink {
	if len(argv) == 0 {
		return nil
	}
	cmd, args := base(argv[0]), unredirected(argv[1:])
	sym := false
	switch cmd {
	case "ln":
		sym = slices.ContainsFunc(args, func(a string) bool {
			return isLong(a, "--symbolic") || shortFlags.MatchString(a) && strings.Contains(a, "s")
		})
	case "cp":
		sym = slices.ContainsFunc(args, func(a string) bool {
			return isLong(a, "--symbolic-link") || shortFlags.MatchString(a) && strings.Contains(a, "s")
		})
		if !sym && !slices.ContainsFunc(args, func(a string) bool {
			return isLong(a, "--link") || shortFlags.MatchString(a) && strings.Contains(a, "l")
		}) {
			return nil
		}
	default:
		return nil
	}
	var paths, targets []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case (a == "-t" || a == "--target-directory") && i+1 < len(args):
			i++
			targets = append(targets, args[i])
		case strings.HasPrefix(a, "--target-directory="):
			targets = append(targets, strings.TrimPrefix(a, "--target-directory="))
		case shortFlags.MatchString(a) && strings.IndexByte(a, 't') > 0:
			if t := a[strings.IndexByte(a, 't')+1:]; t != "" {
				targets = append(targets, t)
			} else if i+1 < len(args) {
				i++
				targets = append(targets, args[i])
			}
		case !strings.HasPrefix(a, "-"):
			paths = append(paths, a)
		}
	}
	// Each source's links: at the target and in it, either may be.
	type made struct{ name, to string }
	var out []made
	switch {
	case len(targets) > 0:
		for _, t := range targets {
			for _, s := range paths {
				out = append(out, made{intoFolder(t, s), s})
			}
		}
	case len(paths) == 1:
		out = append(out, made{intoFolder(".", paths[0]), paths[0]})
	case len(paths) > 1:
		t := paths[len(paths)-1]
		for _, s := range paths[:len(paths)-1] {
			out = append(out, made{intoFolder(t, s), s}, made{t, s})
		}
	}
	var links []madeLink
	for _, m := range out {
		if m.name == "" {
			continue
		}
		l := madeLink{name: m.name, to: m.to, sym: sym}
		switch {
		case strings.ContainsAny(m.name, "$`*?["):
			l.any = true
		case strings.HasPrefix(slash(m.name), "/") || strings.HasPrefix(m.name, "~") || drive.MatchString(slash(m.name)):
			l.name = r.abs(m.name)
		case r.Cwd == "":
			l.rel = true
		default:
			l.name = r.abs(m.name)
		}
		// A hard link's source is from the line's folder; a symbolic
		// one's relative target from the link's, where it is found.
		absTo := strings.HasPrefix(slash(m.to), "/") || strings.HasPrefix(m.to, "~") || drive.MatchString(slash(m.to))
		switch {
		case strings.ContainsAny(m.to, "$`"):
			l.lost = true
		case absTo:
			l.to, l.sym = r.abs(m.to), false
		case !sym && r.Cwd == "":
			l.lost = true
		case !sym:
			l.to = r.abs(m.to)
		}
		links = append(links, l)
	}
	return links
}

// keepLinks keeps links in the judge's reading (ps.links), for the
// line's writes after them.
func (r Rules) keepLinks(links []madeLink) {
	if r.ps == nil {
		return
	}
	for _, l := range links {
		if !slices.Contains(r.ps.links, l) {
			r.ps.links = append(r.ps.links, l)
		}
	}
}

// throughLinks are lands (absolute, clean) with where each may lead
// through the links the line makes (linksOf), those links' own
// followed in turn; false for a path through one whose target the
// guard can't read, or one that leads to more than maxLinkLands.
func (r Rules) throughLinks(lands []string) ([]string, bool) {
	if r.ps == nil || len(r.ps.links) == 0 {
		return lands, true
	}
	for i := 0; i < len(lands); i++ {
		if len(lands) > maxLinkLands {
			return lands, false
		}
		p := lands[i]
		parts := strings.Split(strings.Trim(slash(p), "/"), "/")
		for k := 1; k <= len(parts); k++ {
			prefix, rest := "/"+strings.Join(parts[:k], "/"), strings.Join(parts[k:], "/")
			for _, l := range r.ps.links {
				switch {
				case l.any:
				case l.rel:
					np := strings.Split(strings.Trim(slash(l.name), "/"), "/")
					if len(np) > k || !slices.Equal(parts[k-len(np):k], np) {
						continue
					}
				case r.fold(l.name) != r.fold(prefix):
					continue
				}
				if l.lost {
					return lands, false
				}
				to := l.to
				if l.sym {
					to = r.abs(path.Dir(prefix) + "/" + to)
				}
				lands = withDirs(lands, r.abs(to+"/"+rest))
			}
		}
	}
	return lands, true
}

// maxLinkLands bounds the paths throughLinks follows one write to.
const maxLinkLands = 64

// intoCmds are the writers that copy or link what they are given into
// a folder, under each one's own name.
var intoCmds = setOf("cp install ln")

// intoFolder is where cmd puts source s in folder t: t/name, its name
// any (*) when an expansion gives it; "" for none (., .., /).
func intoFolder(t, s string) string {
	b := base(strings.TrimRight(slash(s), "/"))
	switch {
	case b == "" || b == "." || b == "..":
		return ""
	case strings.ContainsAny(b, "$`"):
		b = "*"
	}
	return strings.TrimRight(t, "/") + "/" + b
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
	// traps are the actions of the traps the lines read set, in order,
	// and the bodies of the functions they define (judgeLine).
	traps []string
	// defs are the names of the functions and aliases the lines read
	// define; moving, whether one of them may move the folder.
	defs   map[string]bool
	moving bool
	// links are the links the lines read make (linksOf).
	links []madeLink
	// resolved and files are what the file system answered (resolve,
	// file).
	resolved map[string]string
	files    map[string]bool
	// checked: the file-system checks put it past its budget.
	checked bool
	// glued are words that lost an unquoted substitution glued to their
	// text (lit), as read.
	glued map[string]bool
	// subs and funs, while collect, are the insides of the substitutions
	// and of the ${ cmd; } read outside any other (nest 0: those in $[ ],
	// (( )) or a heredoc count), the line's own (readSubs).
	collect    bool
	nest       int
	subs, funs []string
}

// sub reads body, the inside of a substitution ($( ), backticks, <( ),
// >( )) that runs in a subshell of a line read at depth, one level
// deeper; while collecting outside any other, it is kept too.
func (p *parse) sub(body string, depth int) [][]string {
	if p.collect && p.nest == 0 {
		p.subs = append(p.subs, body)
	}
	p.nest++
	defer func() { p.nest-- }()
	return p.read(body, false, nil, depth+1)
}

// fun reads body, the inside of bash 5.3's ${ cmd; } or ${| cmd; }, which
// runs in the line's own shell, as sub does; while collecting outside
// any substitution, it is kept in funs.
func (p *parse) fun(body string, depth int) [][]string {
	if p.collect && p.nest == 0 {
		p.funs = append(p.funs, body)
	}
	p.nest++
	defer func() { p.nest-- }()
	return p.read(body, false, nil, depth+1)
}

// readSubs is read of line, and the insides of the substitutions and of
// the ${ cmd; } it runs outside any other (sub, fun).
func (p *parse) readSubs(line string, script func(string) bool, depth int) (cmds [][]string, subs, funs []string) {
	p.collect, p.nest, p.subs, p.funs = true, 0, nil, nil
	cmds = p.read(line, false, script, depth)
	subs, funs = p.subs, p.funs
	p.collect, p.subs, p.funs = false, nil, nil
	return cmds, subs, funs
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
// its own as it is expanded, and of the ${ cmd; } it runs in its own
// shell (funs), those in them not counted: read with the judge's budget.
func (r Rules) subsOf(text string) (subs, funs []string) {
	if r.ps == nil {
		return nil, nil
	}
	_, subs, funs = r.ps.readSubs(text, nil, r.depth)
	return subs, funs
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
	// dropped: an unquoted substitution of the word is in word, not lit.
	dropped := false
	// quoted: some of the word is quoted or escaped ("2">f writes 2's
	// output to f: no descriptor); braced: it has a brace expansion
	// (braceEnd), which makes it a word for each alternative.
	quoted, braced := false, false
	var open, docs []heredoc
	// redir is the index of the last < or > read as a redirect (>| is
	// one); fed: a pipe feeds the next command, and fedShell, it is a
	// shell.
	redir := -1
	fed, fedShell := false, false
	endWord := func() {
		w := ""
		if inWord && hasLit {
			if dropped && lit.String() != word.String() {
				if p.glued == nil {
					p.glued = map[string]bool{}
				}
				p.glued[lit.String()] = true
			}
			w = lit.String()
		} else if inWord {
			w = word.String()
		}
		switch {
		case !inWord:
		case braced && len(words) > 0 && (words[len(words)-1] == ">" || words[len(words)-1] == "<"):
			// A redirect's word stays one: zsh writes each of its words
			// (judgeNoWrite judges them), bash none.
			words = append(words, w)
		case braced:
			// Past maxAlts the line is past the guard's limits.
			alts := braceAlts(w)
			if alts == nil || p.stop(len(w)*len(alts), depth) {
				p.over = true
			}
			p.stop(len(words), depth)
			if cmdPos(words) && slices.ContainsFunc(alts, func(a string) bool { return cdWords[a] || hiddenCode[a] }) {
				inner = append(inner, []string{braceCode})
			}
			words = append(words, alts...)
		default:
			words = append(words, w)
		}
		word.Reset()
		lit.Reset()
		inWord, hasLit, quoted, dropped, braced = false, false, false, false, false
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
	// braceAt and hiddenAt are braceEnd and hiddenBrace at s[i], their
	// reading charged to the budget: past it they find none, so a run of
	// braces is read a bounded number of times.
	braceI, braceE := -1, -1
	braceAt := func(i int) int {
		if i != braceI {
			braceI, braceE = i, -1
			if !p.over {
				e, n := braceEnd(s, i)
				if !p.stop(n, depth) {
					braceE = e
				}
			}
		}
		return braceE
	}
	hiddenAt := func(i int) bool {
		if p.over {
			return false
		}
		ok, n := hiddenBrace(s, i)
		return !p.stop(n, depth) && ok
	}
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
				case funStart(s, j):
					end, ok := funEnd(s, j, true)
					if !ok {
						p.over = true
						return nil
					}
					out = append(out, p.fun(funBody(s, j, end), depth)...)
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
		case funStart(s, i):
			// bash 5.3's ${ cmd; } and ${| cmd; }: commands run in the
			// line's own shell, a part of the word (fun). One whose end
			// the guard can't tell for sure (funEnd) makes the line
			// unreadable.
			end, ok := funEnd(s, i, false)
			if !ok {
				p.over = true
				return nil
			}
			inner = append(inner, p.fun(funBody(s, i, end), depth)...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord, dropped = true, true
			i = end
		case c == '$' && i+2 < len(s) && s[i+1] == '{':
			// A parameter expansion, ${NAME} or ${NAME:-word}, is part of
			// the word. A $( ) or a backtick in it still runs; a ${ cmd; }
			// in it makes the line unreadable (paramEnd).
			end, ok := paramEnd(s, i)
			if !ok {
				p.over = true
				return nil
			}
			subs, funs, ok := substitutions(string(s[i+2 : end]))
			if !ok {
				p.over = true
				return nil
			}
			for _, in := range subs {
				out = append(out, p.sub(in, depth)...)
			}
			for _, in := range funs {
				out = append(out, p.fun(in, depth)...)
			}
			text(string(s[i:min(end+1, len(s))]))
			i = end
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			// A command substitution is a word of the command it is in.
			end := closing(s, i+2, '(', ')')
			inner = append(inner, p.sub(string(s[i+2:end]), depth)...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord, dropped = true, true
			i = end
		case c == '`':
			end := tickClose(s, i+1)
			inner = append(inner, p.sub(string(s[i+1:end]), depth)...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord, dropped = true, true
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
		case c == '|' && i > 0 && redir == i-1 && s[i-1] == '>':
			// >|, a redirect that overwrites under noclobber: its word
			// follows.
		case (c == '<' || c == '>') && i+1 < len(s) && s[i+1] == '(' && !arith:
			// A process substitution, <( ) or >( ), is a word of its
			// command (cp <(x) f copies to f), its inside read as commands
			// too.
			end := closing(s, i+2, '(', ')')
			inner = append(inner, p.sub(string(s[i+2:end]), depth)...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord, dropped = true, true
			i = end
		case c == '{' && braceAt(i) > i:
			// A brace expansion ({a,b}, x{1..3}) is a part of its word
			// (zsh's {cd x} is a group).
			end := braceAt(i)
			text(string(s[i : end+1]))
			braced = true
			i = end
		case c == '{' && hiddenAt(i) && !p.stop(len(words), depth) && cmdPos(words):
			// A command's name with braces the guard can't expand: they
			// may give any command (braceCode). Read on as before.
			inner = append(inner, []string{braceCode})
			fallthrough
		case strings.ContainsRune(";&|\n(){}", c):
			endCommand()
			if c == '|' && (i+1 >= len(s) || s[i+1] != '|') && (i == 0 || s[i-1] != '|') {
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
				subs, funs, ok := substitutions(h.body)
				if !ok {
					p.over = true
					return nil
				}
				for _, in := range subs {
					out = append(out, p.sub(in, depth)...)
				}
				for _, in := range funs {
					out = append(out, p.fun(in, depth)...)
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
// bash's BASH_ENV, sh's ENV, zsh's ZDOTDIR (its .zshenv), in any form
// (assignment: BASH_ENV+=, ENV[0]=).
var extraEnv = assignOf(`BASH_ENV|ENV|ZDOTDIR`)

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
// where s is expanded, and of its ${ cmd; } (funs); false for a ${ cmd; }
// whose end the guard can't tell (funEnd).
func substitutions(text string) (subs, funs []string, ok bool) {
	s := []rune(text)
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] == '$' && i+1 < len(s) && s[i+1] == '(':
			end := closing(s, i+2, '(', ')')
			subs = append(subs, string(s[i+2:end]))
			i = end
		case s[i] == '`':
			end := tickClose(s, i+1)
			subs = append(subs, string(s[i+1:end]))
			i = end
		case funStart(s, i):
			end, ok := funEnd(s, i, false)
			if !ok {
				return nil, nil, false
			}
			funs = append(funs, funBody(s, i, end))
			i = end
		}
	}
	return subs, funs, true
}

// funStart: s[i] starts bash 5.3's ${ cmd; } or ${| cmd; }: a ${ then a
// blank or a |.
func funStart(s []rune, i int) bool {
	return i+2 < len(s) && s[i] == '$' && s[i+1] == '{' && strings.ContainsRune(" \t\n|", s[i+2])
}

// funLead are the reserved words after which, when they are in command
// position themselves, the next word still is (funEnd).
var funLead = setOf("! time -p if then elif else while until do")

// funEnd is the index of the } that ends the ${ cmd; } or ${| cmd; } at
// s[i], its body read as bash reads commands: past quotes, escapes,
// substitutions, parameter expansions, subshells and { } groups. A word
// is in command position where a command starts (after ;, &, |, &&, ||,
// a newline or a (, or after a reserved word that is itself in command
// position), never after an argument or right after a redirect (its
// word is the target). A { of its own (a blank or an operator after it)
// opens a group there or right after a ), and a } closes (the body or
// a group) only as a whole word after ;, & or a newline, a blank, an
// operator or the end after it (or, within double quotes, dq, the
// closing quote or another expansion), with no group left open. false
// where the guard can't tell the end for sure: none, a comment, a
// heredoc, a } anywhere else (${ echo }, a}b, {a,b}, ${ a && }, a
// ${x:-…} holding a ${ cmd; }), or a group it may miss: any other { of
// its own, a ( ) pair (a function's name()), function, coproc or case
// where a command starts.
func funEnd(s []rune, i int, dq bool) (int, bool) {
	cmd, closes, target, after := true, false, false, false
	groups, parens := 0, 0
	j := i + 2
	if j < len(s) && s[j] == '|' {
		j++
	}
	for ; j < len(s); j++ {
		c := s[j]
		next := rune(0)
		if j+1 < len(s) {
			next = s[j+1]
		}
		if target && c != ' ' && c != '\t' {
			// A redirect's word: no command, no close.
			if strings.ContainsRune("\n;&|()<>", c) {
				return 0, false
			}
			k, ok := funWord(s, j)
			if !ok {
				return 0, false
			}
			target, j = false, k-1
			continue
		}
		// paren: the token before this one was a ).
		paren := false
		if c != ' ' && c != '\t' {
			paren, after = after, false
		}
		switch {
		case c == ' ' || c == '\t':
		case c == '\n' || c == ';':
			cmd, closes = true, true
		case c == '&' && next == '>':
			j++
			if j+1 < len(s) && s[j+1] == '>' {
				j++
			}
			target, closes = true, false
		case c == '&' && next == '&', c == '|' && (next == '|' || next == '&'):
			j++
			cmd, closes = true, false
		case c == '&':
			cmd, closes = true, true
		case c == '|':
			cmd, closes = true, false
		case c == '<' && next == '<':
			return 0, false
		case c == '<' || c == '>':
			if next == '>' || next == '&' || next == '|' || c == '<' && next == '>' {
				j++
			}
			target, closes = true, false
		case c == '(':
			k := j + 1
			for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			if k < len(s) && s[k] == ')' {
				return 0, false // name() { …: a function's body
			}
			parens++
			cmd, closes = true, false
		case c == ')':
			if parens == 0 {
				return 0, false
			}
			parens--
			cmd, closes, after = false, false, true
		case c == '}':
			if !closes || parens > 0 || !funSep(s, j+1, dq && groups == 0) {
				return 0, false
			}
			if groups == 0 {
				return j, true
			}
			groups--
			cmd, closes = false, false
		case c == '{' && (j+1 >= len(s) || strings.ContainsRune(" \t\n;&|()<>", next)):
			if !cmd && !paren {
				return 0, false
			}
			groups++
			cmd, closes = true, false
		case c == '#':
			return 0, false
		default:
			k, ok := funWord(s, j)
			if !ok {
				return 0, false
			}
			w := string(s[j:k])
			if cmd && (w == "function" || w == "coproc" || w == "case") {
				return 0, false
			}
			cmd = cmd && funLead[w]
			closes = false
			j = k - 1
		}
	}
	return 0, false
}

// funSep: s[k] may follow a } that closes (funEnd): the end, a blank, an
// operator, or, within double quotes (dq), the closing quote or another
// expansion ("${ a; }${ b; }").
func funSep(s []rune, k int, dq bool) bool {
	return k >= len(s) || strings.ContainsRune(" \t\n;&|()<>", s[k]) || dq && strings.ContainsRune("\"$`", s[k])
}

// funWord is the index after the word at s[j] in a ${ cmd; } body
// (funEnd); false for a } in it outside quotes and substitutions, or
// something left open.
func funWord(s []rune, j int) (int, bool) {
	k := j
	for ; k < len(s) && !strings.ContainsRune(" \t\n;&|()<>", s[k]); k++ {
		switch {
		case s[k] == '\'':
			q := slices.Index(s[k+1:], '\'')
			if q < 0 {
				return 0, false
			}
			k += 1 + q
		case s[k] == '$' && k+1 < len(s) && s[k+1] == '\'':
			if k = ansiEnd(s, k+2); k >= len(s) {
				return 0, false
			}
		case s[k] == '"':
			for k++; k < len(s) && s[k] != '"'; k++ {
				e, ok := wordSkip(s, k, true)
				if !ok {
					return 0, false
				}
				k = e
			}
			if k >= len(s) {
				return 0, false
			}
		case s[k] == '}':
			return 0, false
		default:
			e, ok := wordSkip(s, k, false)
			if !ok {
				return 0, false
			}
			k = e
		}
	}
	return k, true
}

// wordSkip is the index of the last rune of what starts at s[k] in a
// word that funEnd or hiddenBrace reads past (within double quotes, dq):
// an escape, a $( ), a backtick, a ${ cmd; } or a ${…} (paramEnd); k
// itself for any other rune; false for one left open, or a ${…} holding
// a ${ cmd; }.
func wordSkip(s []rune, k int, dq bool) (int, bool) {
	var e int
	switch {
	case s[k] == '\\':
		e = k + 1
	case s[k] == '$' && k+1 < len(s) && s[k+1] == '(':
		e = closing(s, k+2, '(', ')')
	case s[k] == '`':
		e = tickClose(s, k+1)
	case funStart(s, k):
		end, ok := funEnd(s, k, dq)
		if !ok {
			return 0, false
		}
		e = end
	case s[k] == '$' && k+1 < len(s) && s[k+1] == '{':
		end, ok := paramEnd(s, k)
		if !ok {
			return 0, false
		}
		e = end
	default:
		return k, true
	}
	return e, e < len(s)
}

// paramEnd is the index of the } that closes the ${…} parameter
// expansion at s[i] (closing; len(s) for none); false when the guard
// can't read it: a ${ cmd; } inside it, whose end closing's count of
// braces may take for its own.
func paramEnd(s []rune, i int) (int, bool) {
	end := closing(s, i+2, '{', '}')
	for k := i + 2; k < end; k++ {
		if funStart(s, k) {
			return end, false
		}
	}
	return end, true
}

// funBody is the commands of the ${ cmd; } at s[i] whose } is at end.
func funBody(s []rune, i, end int) string {
	return strings.TrimPrefix(string(s[i+2:end]), "|")
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

// assignment is a word bash takes as an assignment before a command (and
// env as one it sets): a name, a [subscript] or none, a + or none, then
// = (a=1, a+=1, a[0]=1, a[k]+=1).
var assignment = assignOf(`[A-Za-z_][A-Za-z0-9_]*`)

// assignOf is a word that assigns a variable whose name matches names, as
// assignment reads one.
func assignOf(names string) *regexp.Regexp {
	return regexp.MustCompile(`^(?:` + names + `)(?:\[[^=]*\])?\+?=`)
}

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

// namedFloor: p (absolute; it may be a glob) may name a file one of
// NoWriteNames matches: in its folder, its name matches the pattern, or
// as a glob, its text before the wildcard and the pattern's meet
// (rm ~/.zcomp*, rm ~/.*). With parents (a remover), a folder holding
// the pattern's folder counts too.
func (r Rules) namedFloor(p string, parents bool) bool {
	fp := r.fold(p)
	return slices.ContainsFunc(r.NoWriteNames, func(n string) bool {
		fn := r.fold(n)
		dir, pat := path.Dir(fn), path.Base(fn)
		if parents {
			fixed := strings.TrimSuffix(wildcard.ReplaceAllString(fp, ""), "/")
			if under(dir, fp) || fixed != fp && (fixed == "" || under(dir, fixed)) {
				return true
			}
		}
		if path.Dir(fp) != dir {
			return false
		}
		name := path.Base(fp)
		if ok, err := path.Match(pat, name); ok || err != nil {
			return true
		}
		lit, plit := wildcard.ReplaceAllString(name, ""), wildcard.ReplaceAllString(pat, "")
		return lit != name && (strings.HasPrefix(lit, plit) || strings.HasPrefix(plit, lit))
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
