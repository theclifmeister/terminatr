// Package guard is the guard's matcher (docs/SPEC.md §8.6, Guard): the
// standing rules as checks on each tool call. It is the Go port of the
// Claude mod's hooks/guard.ts, for agents without a mod: the server
// judges each call a PreToolUse hook reports and the agent's manifest
// answers with the refusal (.Guard in a respond template). The two
// matchers pass the same test vectors
// (internal/agent/claude/mod/tests/guard-vectors.ts): change them
// together.
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
	"path"
	"regexp"
	"slices"
	"strings"
)

// Rules are what a session's guard refuses, as the server works them out
// from the human's settings and the session's role. Off, nothing is
// refused. It is also the body of GET /v1/rules on the mod socket.
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
	// SecretEnv are the names of the secret variables tm gives the
	// session (config env): no command prints them or the whole
	// environment, whichever rules are on (the floor).
	SecretEnv []string `json:"secretEnv,omitempty"`
	// NoWrite are the files and folders no tool writes, whichever rules
	// are on (the floor): tm's settings (config.toml), and a thread's
	// project folder or the parts of it only tm writes for the
	// coordinator. File tools are judged by where they write, shell
	// commands by the paths they redirect into or give a command that
	// writes (cp, mv, tee, rm and the like: judgeNoWrite).
	NoWrite []string `json:"noWrite,omitempty"`
	// AskEscapes: a shell call that asks to run outside the sandbox (its
	// tool's Escape field true) is put to the user (rule "escape"), even
	// where the agent's own rules would let it run: tm answers that
	// prompt for the user only, never for the coordinator (T574 case 6).
	AskEscapes bool `json:"askEscapes,omitempty"`
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
}

// RuleTMFiles is the rule of the floor's refusal to write tm's settings
// or the project folder (Rules.NoWrite).
const RuleTMFiles = "tm-files"

// RuleEscape is the rule of the guard's question about a call that asks
// to leave the sandbox (Rules.AskEscapes).
const RuleEscape = "escape"

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
func (r Rules) Judge(tool string, input map[string]any) *Denial {
	if !r.On || len(r.Rules) == 0 && len(r.Mods) == 0 && len(r.Own) == 0 && len(r.SecretEnv) == 0 && len(r.Floor) == 0 && len(r.NoWrite) == 0 && !r.AskEscapes {
		return nil
	}
	t, ok := r.Tools[tool]
	if !ok {
		return nil
	}
	var values []string
	for _, f := range t.Fields {
		if s, _ := input[f].(string); s != "" {
			values = append(values, s)
		}
	}
	switch t.Kind {
	case KindShell:
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
		if ask == nil && r.AskEscapes && t.Escape != "" && input[t.Escape] == true {
			ask = &Denial{Rule: RuleEscape, Ask: true, Summary: "a command outside the sandbox",
				Message: "terminatr guard: this command asks to run outside the sandbox, so the user answers it."}
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

// judgeRead judges a file tool reading p (absolute; a glob by the part
// before its first wildcard).
func (r Rules) judgeRead(tool, p string) *Denial {
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
	return r.abs(r.Resolve(r.joined(p)))
}

// judgeWrite judges a file tool writing p (absolute, where the write
// lands), named in a refusal as shown (as the call gave it).
func (r Rules) judgeWrite(tool, shown, p string) *Denial {
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
			m = "a thread doesn't write tm's settings (config.toml) or the project folder. Write in your worktree, or say so in your report."
		} else {
			m = "tm's settings (config.toml) and the parts of the project folder tm keeps (threads/, inbox/, tasks/, journal/, TASKS.md and the like) are written by tm only: use tm's commands, or ask the user."
		}
	case "credentials":
		m = "credential files and tokens are not read here. A tool that needs a token gets it from the secret environment the user gives threads in tm's settings; if the task needs access you don't have, " + tell + " exactly what is missing."
	}
	// A thread never merges while the rule holds: no allowance lifts it.
	return &Denial{Rule: rule, Message: "terminatr guard (" + rule + "): " + m, Summary: summary, Floor: rule == "merge"}
}

// judgeBash checks each simple command of a Bash command line.
func (r Rules) judgeBash(command string) *Denial { return r.judgeLine(command, []string{r.Cwd}) }

// judgeLine judges each simple command of a Bash command line with its
// relative paths from each folder of cwds, as the floor sees them: ""
// is one it can't tell, which refuses a relative path a writer is
// given. A cd (or pushd, popd) moves them only where it is a plain
// command of the line's top level: joined by && it moves them for what
// follows in its list, joined by ; or || it adds a folder they may be
// in (it may fail, or not run); in ( ), a pipeline or the background it
// moves nothing, and anywhere else ({ }, if, a substitution) it leaves
// them unknown. A part that runs shell code the line doesn't show
// (eval, trap, source, ., also through command or builtin) may move them
// at any point after, so the rest of the line keeps unknown among them.
func (r Rules) judgeLine(command string, cwds []string) *Denial {
	parts, ok := topLevel(command)
	if !ok {
		// A heredoc or a line it can't split: one part, with any cd in
		// it leaving its relative paths unknown from the start.
		parts = []linePart{{text: command}}
		if hasCd(command) {
			cwds = withDirs(cwds, "")
		}
	}
	cur, list := cwds, cwds
	hidden := false
	for _, pt := range parts {
		if listEnd(pt.before) {
			list = cur
		}
		for _, c := range cur {
			if d := r.judgeCommands(pt.text, c); d != nil {
				return d
			}
		}
		inPipe := pt.before == "|" || pt.after == "|" || pt.after == "&"
		if cmd, args, plain := plainCd(pt.text); plain && !inPipe {
			var to []string
			for _, c := range cur {
				to = withDirs(to, nowhere{dir: c}.cd(r, cmd, args).dir)
			}
			switch {
			case pt.after == "&&" && pt.before != "||":
				cur = to
			case pt.after == "||":
				list = withDirs(list, to...)
			default:
				cur = withDirs(cur, to...)
			}
		} else if !inPipe && !strings.HasPrefix(pt.text, "(") && hasCd(pt.text) {
			cur = withDirs(cur, "")
		}
		hidden = hidden || runsHidden(pt.text)
		if hidden {
			cur = withDirs(cur, "")
		}
		list = withDirs(list, cur...)
		if listEnd(pt.after) {
			cur = list
		}
	}
	return nil
}

// hiddenCode are the builtins that run shell code the line doesn't show
// in the shell itself, where a cd in it moves the line's folder.
var hiddenCode = setOf("eval trap source .")

// runsHidden: one of text's commands is one of those, maybe through
// command or builtin.
func runsHidden(text string) bool {
	for _, words := range Commands(text) {
		argv := strip(words)
		for len(argv) > 1 && (argv[0] == "command" || argv[0] == "builtin") {
			argv = argv[1:]
		}
		if len(argv) > 0 && hiddenCode[argv[0]] {
			return true
		}
	}
	return false
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
	for _, words := range Commands(part) {
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
			// bash -c STRING runs the string.
			if c, ok := shellCommand(args); ok {
				d = r.judgeLine(c, []string{cwd})
			}
		case cmd == "eval":
			d = r.judgeLine(strings.Join(args, " "), []string{cwd})
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

// tmAgentMod are the tm mod subcommands an agent may run; the others,
// tm project's tmProjectWrites (and repo add) and the dashboard write
// tm's settings in the tm process itself, the human's alone: an agent
// past its sandbox (yolo) that runs one with its session's variables
// unset and out of its session's process tree would pass for the human
// (internal/cli: Run, caller.Narrower), so the floor refuses them.
var (
	tmAgentMod      = setOf("list show validate new test logs examples host")
	tmProjectWrites = setOf("new remote activate deactivate pause resume archive unarchive delete rename mv link open")
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
	}
	return ""
}

// nowhere is the folder a command line's relative paths are from, as
// the floor follows its cd: "" when it can't tell.
type nowhere struct{ dir string }

// cd is where cmd (cd, pushd, popd) with args leaves the line: a
// folder it names plainly, home for none, else unknown.
func (n nowhere) cd(r Rules, cmd string, args []string) nowhere {
	var to []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" {
			to = append(to, a)
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
	if a == "-" || strings.ContainsAny(a, "$`") || strings.HasPrefix(a, "~") && !strings.HasPrefix(a, "~/") && a != "~" {
		return nowhere{}
	}
	if n.dir == "" && !strings.HasPrefix(a, "/") && !strings.HasPrefix(a, "~") && !drive.MatchString(slash(a)) {
		return nowhere{}
	}
	r.Cwd = n.dir
	return nowhere{dir: r.abs(a)}
}

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
// before and after it ("" at the line's ends; a newline is ;, |& is |).
type linePart struct{ text, before, after string }

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
func hasCd(text string) bool {
	for _, words := range Commands(text) {
		if slices.ContainsFunc(words, func(w string) bool { return cdWords[w] }) {
			return true
		}
	}
	return false
}

// plainCd: part is one plain cd, pushd or popd, and its arguments.
func plainCd(part string) (cmd string, args []string, ok bool) {
	if strings.HasPrefix(part, "(") || strings.HasPrefix(part, "{") {
		return "", nil, false
	}
	cmds := Commands(part)
	if len(cmds) != 1 || len(cmds[0]) == 0 || !cdWords[cmds[0][0]] {
		return "", nil, false
	}
	return cmds[0][0], cmds[0][1:], true
}

// topLevel splits a command line at its top level's separators, outside
// quotes, ( ), { }, $( ) and backticks; ok is false for a line with a
// heredoc, or whose quotes or brackets don't close.
func topLevel(line string) (parts []linePart, ok bool) {
	s := []rune(line)
	var stack []rune
	var b strings.Builder
	before := ""
	cut := func(sep string) {
		if t := strings.TrimSpace(b.String()); t != "" {
			parts = append(parts, linePart{text: t, before: before})
			before = sep
		} else if before == "" || sep == ";" {
			before = sep
		}
		if n := len(parts); n > 0 && parts[n-1].after == "" {
			parts[n-1].after = sep
		}
		b.Reset()
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
			stack = append(stack, '(')
		case c == ')':
			if top() != '(' {
				return nil, false
			}
			stack = stack[:len(stack)-1]
		case c == '{' && wordStart(i) && (next == ' ' || next == '\t' || next == '\n'):
			stack = append(stack, '{')
		case c == '}' && top() == '{' && wordStart(i):
			stack = stack[:len(stack)-1]
		case c == '<' && next == '<' && (i+2 >= len(s) || s[i+2] != '<') && prev != '<':
			return nil, false
		case len(stack) > 0:
		case c == '#' && wordStart(i):
			for i < len(s) && s[i] != '\n' {
				i++
			}
			cut(";")
			continue
		case c == ';' || c == '\n':
			cut(";")
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
	return parts, true
}

// judgeNoWrite: the floor's NoWrite, of a simple command, its name and
// arguments and its words as written: a redirect into a path there, or
// a command that writes the paths it is given naming one (cp's and the
// like's target; a folder holding one, for a remover). With r.Cwd ""
// (a cd the line made that it can't follow) a relative path could be
// anywhere, so it is refused.
func (r Rules) judgeNoWrite(cmd string, args, words []string) *Denial {
	if len(r.NoWrite) == 0 {
		return nil
	}
	refuse := func(w string, parents bool) *Denial {
		if w == "" || strings.HasPrefix(w, "&") {
			return nil
		}
		if r.Cwd == "" && !strings.HasPrefix(slash(w), "/") && !strings.HasPrefix(w, "~") && !drive.MatchString(slash(w)) {
			return floor(r.deny(RuleTMFiles, cmd+" of "+safe(w)+" after a cd", ""))
		}
		p := r.abs(w)
		fixed := strings.TrimSuffix(wildcard.ReplaceAllString(p, ""), "/")
		if fixed == "" {
			fixed = "/"
		}
		if r.covers(r.NoWrite, p) || parents && slices.ContainsFunc(r.NoWrite, func(n string) bool { return r.under(n, fixed) || r.globReaches(p, n) }) {
			return floor(r.deny(RuleTMFiles, cmd+" of "+safe(p), ""))
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
		if what := tmWritesSettings(args); what != "" {
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
// body once read.
type heredoc struct {
	delim        string
	tabs, quoted bool
	body         string
}

// Commands splits a command line into simple commands, each a list of
// words with quotes removed; redirections stay words ("<", ">"). A $( )
// or backtick is a word of its command, its inside read as commands too.
// A heredoc's body is read as commands when the line has a shell to run
// it; otherwise only what it substitutes is.
func Commands(line string) [][]string { return commands(line, false) }

// commands is Commands; in arithmetic (arith) << is a shift, no heredoc.
func commands(line string, arith bool) [][]string {
	var out, inner [][]string
	var words []string
	// word is the word as written; lit the same without its unquoted
	// substitutions, what the word is when they print nothing (printenv$(x)
	// is printenv): the word, unless it is substitutions alone.
	var word, lit strings.Builder
	inWord, hasLit := false, false
	var open, docs []heredoc
	endWord := func() {
		if inWord && hasLit {
			words = append(words, lit.String())
		} else if inWord {
			words = append(words, word.String())
		}
		word.Reset()
		lit.Reset()
		inWord, hasLit = false, false
	}
	// text is literal text of the word.
	text := func(t string) {
		word.WriteString(t)
		lit.WriteString(t)
		inWord, hasLit = true, true
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
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
			}
			i++
		case c == '\'':
			end := len(s)
			if j := slices.Index(s[i+1:], '\''); j >= 0 {
				end = i + 1 + j
			}
			text(string(s[i+1 : end]))
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
					out = append(out, Commands(string(s[j+2:end]))...)
					q.WriteString(string(s[j:min(end+1, len(s))]))
					j = end + 1
				case s[j] == '`':
					end := tickClose(s, j+1)
					out = append(out, Commands(string(s[j+1:end]))...)
					q.WriteString(string(s[j:min(end+1, len(s))]))
					j = end + 1
				default:
					q.WriteRune(s[j])
					j++
				}
			}
			text(q.String())
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
			out = append(out, commands(string(s[i+2:min(end, len(s))]), true)...)
			i = end
		case c == '$' && i+1 < len(s) && s[i+1] == '[' && closing(s, i+2, '[', ']') < len(s):
			// Arithmetic, $[ … ], a part of its word: its inside read on
			// its own, as commands.
			end := closing(s, i+2, '[', ']')
			inner = append(inner, commands(string(s[i+2:end]), true)...)
			text(string(s[i : end+1]))
			i = end
		case c == '$' && i+2 < len(s) && s[i+1] == '{' && !strings.ContainsRune(" \t\n|", s[i+2]):
			// A parameter expansion, ${NAME} or ${NAME:-word}, is part of
			// the word (${ cmd; } runs commands: read below as { }). A $( )
			// or backtick in it still runs.
			end := closing(s, i+2, '{', '}')
			for _, in := range substitutions(string(s[i+2 : end])) {
				out = append(out, Commands(in)...)
			}
			text(string(s[i:min(end+1, len(s))]))
			i = end
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			// A command substitution is a word of the command it is in.
			end := closing(s, i+2, '(', ')')
			inner = append(inner, Commands(string(s[i+2:end]))...)
			word.WriteString(string(s[i:min(end+1, len(s))]))
			inWord = true
			i = end
		case c == '`':
			end := tickClose(s, i+1)
			inner = append(inner, Commands(string(s[i+1:end]))...)
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
		case strings.ContainsRune(";&|\n(){}", c):
			endCommand()
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
			var d strings.Builder
			for j < len(s) && !strings.ContainsRune(" \t\n;&|<>()", s[j]) {
				switch s[j] {
				case '\'', '"':
					h.quoted = true
					end := len(s)
					if k := slices.Index(s[j+1:], s[j]); k >= 0 {
						end = j + 1 + k
					}
					d.WriteString(string(s[j+1 : end]))
					j = end + 1
				case '\\':
					h.quoted = true
					if j+1 < len(s) {
						d.WriteRune(s[j+1])
					}
					j += 2
				default:
					d.WriteRune(s[j])
					j++
				}
			}
			h.delim = d.String()
			if h.delim != "" || h.quoted {
				words = append(words, h.delim)
				open = append(open, h)
			}
			i = j - 1
		case c == '<' || c == '>':
			endWord()
			words = append(words, string(c))
		default:
			text(string(c))
		}
	}
	endCommand()
	if len(docs) > 0 {
		// A shell anywhere on the line (ssh host bash, docker exec -i c
		// sh) may run what a heredoc gives it.
		shell := slices.ContainsFunc(out, func(w []string) bool {
			argv := strip(w)
			return len(argv) > 0 && stdinShells[base(argv[0])] || slices.ContainsFunc(w, func(a string) bool { return shells[base(a)] })
		})
		for _, h := range docs {
			if !h.quoted {
				// The line's own shell expands it first.
				for _, in := range substitutions(h.body) {
					out = append(out, Commands(in)...)
				}
			}
			if shell {
				out = append(out, Commands(h.body)...)
			}
		}
	}
	return out
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
			w = 2 // two UTF-16 units, as guard.ts counts them
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
