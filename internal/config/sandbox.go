package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/textsafe"
)

// The sandbox lists of threads (docs/SPEC.md §5.2, Sandbox lists): the
// commands a thread's Bash runs outside its sandbox, and the permission
// rules its agent gets on top of tm's, for a tool the sandbox can't hold
// (Playwright's Chromium needs Mach services macOS's sandbox denies).
// [defaults.sandbox] gives every project's threads them;
// [projects.<slug>.sandbox] a project's, each key it sets replacing all
// projects' list of that key:
//
//	[projects.web.sandbox]
//	excluded_commands = ["npx playwright *"]
//	allow = ["Bash(npx playwright *)"]
//
// tm writes them into a thread's launch settings at each start and
// resume (Claude's sandbox.excludedCommands and permissions.allow); a
// coordinator never gets them. Only the human sets them, in config.toml
// or the settings popups: an excluded command runs with the user's full
// file and network access, tm's socket included.

// Keys of a sandbox table.
const (
	SandboxExcluded = "excluded_commands"
	SandboxAllow    = "allow"
)

// SandboxKeys are a sandbox table's keys, in the settings' order.
var SandboxKeys = []string{SandboxExcluded, SandboxAllow}

// rawSandbox is a [projects.<slug>.sandbox] or [defaults.sandbox] table:
// the lists, and the presets' switches (sandbox_presets.go).
type rawSandbox struct {
	ExcludedCommands *[]string `toml:"excluded_commands"`
	Allow            *[]string `toml:"allow"`
	BrowserTesting   *bool     `toml:"browser_testing"`
	BrowserDownloads *bool     `toml:"browser_downloads"`
	LocalDevServers  *bool     `toml:"local_dev_servers"`
}

// list is the table's list of key, nil when it doesn't set it.
func (r *rawSandbox) list(key string) *[]string {
	if r == nil {
		return nil
	}
	switch key {
	case SandboxExcluded:
		return r.ExcludedCommands
	case SandboxAllow:
		return r.Allow
	}
	return nil
}

// ThreadSandbox is what a project's threads get: each list and switch
// the project's own, else all projects'. Presets are the keys of the
// presets on, on any platform: the launch skips one that doesn't apply
// where it runs.
type ThreadSandbox struct {
	ExcludedCommands []string
	Allow            []string
	Presets          []string
}

// Empty reports whether it opens nothing.
func (t ThreadSandbox) Empty() bool {
	return len(t.ExcludedCommands)+len(t.Allow)+len(t.Presets) == 0
}

// maxSandboxEntry bounds an entry's length.
const maxSandboxEntry = 300

// matchAll is what a pattern that matches everything is made of, once
// its fixed text is gone.
const matchAll = "*: /~."

// broad are the programs that run any other one, or any code, given as
// arguments: an entry that leaves their arguments open opens everything
// they can run.
var broad = []string{"sh", "bash", "zsh", "fish", "dash", "ksh", "csh", "tcsh", "pwsh", "powershell",
	"env", "sudo", "doas", "su", "xargs", "eval", "exec", "command", "builtin", "nohup", "nice", "time", "timeout", "watch", "script",
	"python", "node", "deno", "bun", "ruby", "perl", "php", "lua", "osascript", "awk", "make",
	"npx", "bunx", "pnpx", "uvx", "pipx", "npm", "pnpm", "yarn", "uv", "docker", "podman", "kubectl", "ssh", "git"}

// wrappers run the command after them (env sh -c …): the checks look
// past them, and past their flags, numbers and NAME=value words.
var wrappers = []string{"env", "sudo", "doas", "nohup", "nice", "time", "timeout", "command", "builtin", "exec", "xargs"}

// codeFlags make the program before them run the code that follows (sh
// -c, python3 -c, node -e, npm exec): with that left open, any code.
var codeFlags = []string{"-c", "-lc", "-ic", "-e", "-E", "--eval", "-p", "--print", "-r", "-m", "eval", "exec", "run", "x", "dlx"}

// program is a command word's program as the checks compare it: its
// file name, without a version (/usr/bin/python3.12 is python).
func program(word string) string {
	if i := strings.LastIndexAny(word, `/\`); i >= 0 {
		word = word[i+1:]
	}
	word = strings.ToLower(strings.TrimSuffix(word, ".exe"))
	if t := strings.TrimRight(word, "0123456789."); t != "" {
		word = t
	}
	return word
}

// openProgram is why a command pattern leaves its program open (*e*,
// *-*, python*: any program whose name fits), or "".
func openProgram(cmd string) string {
	words := strings.Fields(cmd)
	if len(words) > 0 && strings.Contains(words[0], "*") {
		return fmt.Sprintf("%q leaves the program open (%s matches many programs): name it, e.g. npx playwright *", cmd, words[0])
	}
	return ""
}

// codeWarning is why command pattern cmd, open at its first *, lets any
// program or code through, or "": a program that runs others with its
// arguments open (bash *, /bin/sh *, env sh *), or with the code it runs
// open (sh -c *, python3 -c *).
func codeWarning(cmd string) string {
	fixed, _, open := strings.Cut(cmd, "*")
	if !open {
		return ""
	}
	words := strings.Fields(fixed)
	for wrapped := false; len(words) > 1; words = words[1:] {
		w := words[0]
		switch {
		case slices.Contains(wrappers, program(w)):
			wrapped = true
			continue
		case wrapped && (strings.HasPrefix(w, "-") || strings.Contains(w, "=") || strings.Trim(w, "0123456789.smhd") == ""):
			continue
		}
		break
	}
	switch {
	case len(words) == 1 && slices.Contains(broad, program(strings.TrimSuffix(words[0], ":"))):
		return words[0] + " runs other programs or code: with its arguments open, this lets through anything it can run"
	case len(words) == 2 && slices.Contains(broad, program(words[0])) && slices.Contains(codeFlags, words[1]):
		return words[0] + " " + words[1] + " runs the code that follows it: left open, that is any code"
	}
	return ""
}

// shellOps are what splits a call into several commands, or keeps it in
// the sandbox whatever the lists say (a redirect, a substitution):
// an excluded command containing one never matches.
var shellOps = []string{";", "&", "|", "<", ">", "`", "$(", "\n"}

// checkSandboxText checks what every entry must be: one line of plain
// text, without spaces around it.
func checkSandboxText(e string) error {
	switch {
	case e == "":
		return fmt.Errorf("an entry can't be empty")
	case len(e) > maxSandboxEntry:
		return fmt.Errorf("%q is longer than %d characters", textsafe.Clip(textsafe.Clean(e, false), 40), maxSandboxEntry)
	case !textsafe.Safe(e):
		return fmt.Errorf("%q has control or invisible characters", textsafe.Clean(e, false))
	case strings.TrimSpace(e) != e:
		return fmt.Errorf("%q has spaces around it", e)
	}
	return nil
}

// hasExpansion is whether e has a $ that starts an expansion anywhere in
// it: a name or ${, or a special parameter ($1 $@ $* $$ $? $# $! $- $0);
// $( is a shellOp. A $ at the end or before a space is plain.
func hasExpansion(e string) bool {
	for i := 0; i+1 < len(e); i++ {
		if e[i] != '$' {
			continue
		}
		switch c := e[i+1]; {
		case c == '{', c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '@', c == '*', c == '$', c == '?', c == '#', c == '!', c == '-':
			return true
		}
	}
	return false
}

// CheckExcludedCommand checks one excluded command: a command pattern
// in Claude's Bash rule syntax ("npx playwright *": a trailing " *"
// takes any arguments, none too; without it the command must match
// exactly), never one that matches every command, which would be no
// sandbox at all (sandbox_escapes and yolo are the settings for that).
func CheckExcludedCommand(e string) error {
	if err := checkSandboxText(e); err != nil {
		return err
	}
	switch l := strings.ToLower(e); {
	case strings.Trim(e, matchAll) == "":
		return fmt.Errorf("%q matches every command: that is no sandbox at all", e)
	case strings.HasPrefix(l, "bash(") || strings.HasPrefix(l, "powershell("):
		return fmt.Errorf("%q is a permission rule: an excluded command is the command itself, e.g. npx playwright *", e)
	case openProgram(e) != "":
		return errors.New(openProgram(e))
	}
	if len([]rune(e)) < 2 {
		return fmt.Errorf("%q is too short for a command: it would match programs by accident", e)
	}
	if hasExpansion(e) {
		return fmt.Errorf("%q has a variable: an excluded command is one command, without substitutions", e)
	}
	for _, op := range shellOps {
		if strings.Contains(e, op) {
			return fmt.Errorf("%q has %q: an excluded command is one command, without pipes, redirects, substitutions or ;", e, strings.TrimSpace(op))
		}
	}
	return nil
}

// allowRule is a permission rule: a tool's name, then optionally what it
// applies to in parentheses.
var allowRule = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*)(?:\((.+)\))?$`)

// wholeTools are the tools whose rule must name what it is for: never
// every command, every file read or written, or every site fetched.
// Edit allow rules also open the Bash sandbox for writing. Each with an
// example of what to name.
var wholeTools = map[string]string{
	"bash": "npx playwright *", "powershell": "npx playwright *",
	"edit": "~/Library/Caches/ms-playwright/**", "write": "~/Library/Caches/ms-playwright/**",
	"multiedit": "~/Library/Caches/ms-playwright/**", "notebookedit": "~/Library/Caches/ms-playwright/**",
	"read": "~/Library/Caches/ms-playwright/**", "glob": "~/Library/Caches/ms-playwright/**", "grep": "~/Library/Caches/ms-playwright/**",
	"webfetch": "domain:playwright.dev",
}

// editTools are the tools whose allow rules write files.
var editTools = []string{"edit", "write", "multiedit", "notebookedit"}

// bashRule is a Bash or PowerShell rule's command pattern, its legacy
// prefix form (npx:*) as the command form (npx *).
func bashRule(what string) string {
	if c, ok := strings.CutSuffix(what, ":*"); ok {
		return c + " *"
	}
	return what
}

// CheckSandboxAllow checks one allow rule: Claude's permission rule
// syntax (Tool or Tool(what), e.g. Bash(npx playwright *)), never one
// that lets every command run, every file be read or written, or every
// site be fetched without a prompt: those must name what they are for.
// Tool names count whatever their case. tm's own deny rules win over
// it either way.
func CheckSandboxAllow(e string) error {
	if err := checkSandboxText(e); err != nil {
		return err
	}
	m := allowRule.FindStringSubmatch(e)
	if m == nil {
		return fmt.Errorf("%q isn't a permission rule: Tool or Tool(what), e.g. Bash(npx playwright *)", e)
	}
	tool, what := strings.ToLower(m[1]), m[2]
	ex, whole := wholeTools[tool]
	if !whole {
		return nil
	}
	if tool == "webfetch" {
		what = strings.TrimPrefix(what, "domain:")
	}
	if strings.Trim(what, matchAll) == "" {
		return fmt.Errorf("%q allows every %s call: name what it is for, e.g. %s(%s)", e, m[1], m[1], ex)
	}
	if tool == "bash" || tool == "powershell" {
		if why := openProgram(bashRule(what)); why != "" {
			return errors.New(why)
		}
	}
	return nil
}

// checkSandboxEntry checks an entry of key's list.
func checkSandboxEntry(key, e string) error {
	switch key {
	case SandboxExcluded:
		return CheckExcludedCommand(e)
	case SandboxAllow:
		return CheckSandboxAllow(e)
	}
	return fmt.Errorf("unknown sandbox list %q", key)
}

// CheckSandboxList checks a whole list of key: each entry, none twice.
func CheckSandboxList(key string, list []string) error {
	for i, e := range list {
		if err := checkSandboxEntry(key, e); err != nil {
			return err
		}
		if slices.Contains(list[:i], e) {
			return fmt.Errorf("%q is listed twice", e)
		}
	}
	return nil
}

// SandboxProblems is, for each entry of key's list, why it fails the
// sandbox checks ("" for one that passes): the rule it breaks, or that
// it is listed twice. A config.toml with any such entry drops the
// project's sandbox lists and presets at launch.
func SandboxProblems(key string, list []string) []string {
	out := make([]string, len(list))
	for i, e := range list {
		if err := checkSandboxEntry(key, e); err != nil {
			out[i] = err.Error()
		} else if slices.Contains(list[:i], e) {
			out[i] = fmt.Sprintf("%q is listed twice", e)
		}
	}
	return out
}

// SandboxWarning is why an entry of key's list, which passes its check,
// still opens more than it seems to, or "": a program that runs others
// or code with that left open, a tool allowed whole (an MCP server's
// every tool), an Edit rule (which opens the Bash sandbox for writing
// there too), more so high up a tree or where files start programs.
func SandboxWarning(key, e string) string {
	if key != SandboxAllow {
		return codeWarning(e)
	}
	m := allowRule.FindStringSubmatch(e)
	if m == nil {
		return ""
	}
	tool := strings.ToLower(m[1])
	server, one := strings.CutPrefix(tool, "mcp__")
	switch {
	case one && strings.Contains(server, "__"):
		return "" // one MCP tool: its rule has no target to name
	case one:
		return "allows every tool of the MCP server " + m[1][len("mcp__"):] + " without a prompt"
	case m[2] == "" || strings.Trim(m[2], matchAll) == "":
		return "allows every " + m[1] + " call without a prompt"
	case slices.Contains(editTools, tool):
		return editWarning(m[2])
	case tool == "bash" || tool == "powershell":
		return codeWarning(bashRule(m[2]))
	}
	return ""
}

// startFiles are the file and folder names whose contents start
// programs as the user, outside any sandbox: shell profiles, git hooks
// and config, agents' and tm's settings, launch agents.
var startFiles = []string{".zshrc", ".zshenv", ".zprofile", ".bashrc", ".bash_profile", ".profile", ".bash_login",
	".config", ".ssh", ".gitconfig", ".git", ".claude", ".codex", ".terminatr", "LaunchAgents", ".npmrc", ".local"}

// editWarning is an Edit allow rule's warning: always that the thread
// writes there unprompted and its sandboxed commands can too; plainer
// where that reaches files that start programs, or a whole tree high
// up (~/Work/**, //Users/**).
func editWarning(path string) string {
	const base = "lets the thread change files there without asking, and its sandboxed commands write there too"
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	for _, p := range parts {
		if slices.Contains(startFiles, p) {
			return base + ": " + p + " holds files that start programs, so this lets the thread run code as you outside the sandbox"
		}
	}
	fixed := 0
	for _, p := range parts {
		if p == ".." {
			return base + ": " + path + " climbs out of the folder it names (..), which may reach other repositories and your settings"
		}
		if strings.ContainsAny(p, "*?[") {
			break
		}
		fixed++
	}
	// High up: ~/<folder>, //Users/<name>; a relative path has only a
	// leading wildcard.
	high := 0
	if strings.HasPrefix(path, "~") || strings.HasPrefix(path, "//") {
		high = 2
	}
	if fixed < len(parts) && fixed <= high {
		return base + ": " + path + " is a whole tree high up, which may hold other repositories and your settings"
	}
	return base
}

// SandboxCovered is the allow rule of allow that lets excluded command
// e run without a prompt, or "": a Bash rule whose pattern takes e's.
// The two together run e outside the sandbox without asking.
func SandboxCovered(e string, allow []string) string {
	for _, a := range allow {
		m := allowRule.FindStringSubmatch(a)
		if m == nil || m[2] == "" || !strings.EqualFold(m[1], "bash") {
			continue
		}
		if globMatch(bashRule(m[2]), e) {
			return a
		}
	}
	return ""
}

// globMatch reports whether s fits pattern p, where * is any text and
// a trailing " *" no arguments too, as Claude matches Bash rules.
func globMatch(p, s string) bool {
	if q, ok := strings.CutSuffix(p, " *"); ok && s == q {
		return true
	}
	// match[j]: p[:i] fits s[:j], row by row of p.
	match := make([]bool, len(s)+1)
	match[0] = true
	for i := 0; i < len(p); i++ {
		next := make([]bool, len(s)+1)
		if p[i] == '*' {
			next[0] = match[0]
			for j := 1; j <= len(s); j++ {
				next[j] = next[j-1] || match[j]
			}
		} else {
			for j := 1; j <= len(s); j++ {
				next[j] = match[j-1] && p[i] == s[j-1]
			}
		}
		match = next
	}
	return match[len(s)]
}

// checkSandbox checks a table's lists.
func checkSandbox(r *rawSandbox, path, table string) error {
	for _, k := range SandboxKeys {
		if l := r.list(k); l != nil {
			if err := CheckSandboxList(k, *l); err != nil {
				return fmt.Errorf("%s: %s.sandbox.%s: %w", path, table, k, err)
			}
		}
	}
	return nil
}

// ThreadSandbox is what project slug's threads get: for each list, the
// project's own when its table sets it (an empty one too), else all
// projects' ([defaults.sandbox]). None for a nil Config.
func (c *Config) ThreadSandbox(slug string) (ThreadSandbox, error) {
	var out ThreadSandbox
	if c == nil {
		return out, nil
	}
	if err := checkSandbox(c.defaults.Sandbox, c.Path, DefaultsTable); err != nil {
		return out, err
	}
	own := c.projects[slug].Sandbox
	if err := checkSandbox(own, c.Path, "projects."+slug); err != nil {
		return out, err
	}
	pick := func(key string) []string {
		if l := own.list(key); l != nil {
			return slices.Clone(*l)
		}
		if l := c.defaults.Sandbox.list(key); l != nil {
			return slices.Clone(*l)
		}
		return nil
	}
	out.ExcludedCommands, out.Allow = pick(SandboxExcluded), pick(SandboxAllow)
	for _, k := range SandboxPresets {
		on := own.preset(k)
		if on == nil {
			on = c.defaults.Sandbox.preset(k)
		}
		if on != nil && *on {
			out.Presets = append(out.Presets, k)
		}
	}
	return out, nil
}

// SandboxOf is one table's sandbox lists by key, only those it sets:
// all projects' ([defaults.sandbox]) for slug "", else the project's
// own. None for a nil Config.
func (c *Config) SandboxOf(slug string) map[string][]string {
	if c == nil {
		return nil
	}
	r := c.defaults.Sandbox
	if slug != "" {
		r = c.projects[slug].Sandbox
	}
	out := map[string][]string{}
	for _, k := range SandboxKeys {
		if l := r.list(k); l != nil {
			out[k] = slices.Clone(*l)
		}
	}
	return out
}

// sandboxTable is the sandbox table of table, DefaultsTable or a
// project's ("projects.<slug>").
func sandboxTable(table, key string) (string, error) {
	if table != DefaultsTable && !strings.HasPrefix(table, "projects.") {
		return "", fmt.Errorf("no sandbox lists in [%s]", table)
	}
	if !slices.Contains(SandboxKeys, key) {
		return "", fmt.Errorf("unknown sandbox list %q", key)
	}
	return table + ".sandbox", nil
}

// SetSandbox writes key's list in table's sandbox table (DefaultsTable
// or "projects.<slug>"), checked as Load checks it; an empty list too,
// which on a project means none, not all projects'.
func SetSandbox(table, key string, list []string) error {
	t, err := sandboxTable(table, key)
	if err != nil {
		return err
	}
	if err := CheckSandboxList(key, list); err != nil {
		return err
	}
	if list == nil {
		list = []string{}
	}
	return Set(t, key, list)
}

// UnsetSandbox removes key's list from table's sandbox table: a
// project's then follows all projects'. ErrForm when the file sets it
// in a form the line editor doesn't change.
func UnsetSandbox(table, key string) error {
	t, err := sandboxTable(table, key)
	if err != nil {
		return err
	}
	return Unset(t, key)
}

// ExcludedMatch reports whether command line cmd matches excluded
// command e as Claude matches it: e's pattern takes cmd, and cmd is one
// command (a pipe, redirect, substitution or ; keeps it in the sandbox).
func ExcludedMatch(e, cmd string) bool {
	for _, op := range shellOps {
		if strings.Contains(cmd, op) {
			return false
		}
	}
	return globMatch(e, cmd)
}
