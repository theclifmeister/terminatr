package config

import (
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

// rawSandbox is a [projects.<slug>.sandbox] or [defaults.sandbox] table.
type rawSandbox struct {
	ExcludedCommands *[]string `toml:"excluded_commands"`
	Allow            *[]string `toml:"allow"`
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

// ThreadSandbox is what a project's threads get: each list the
// project's own, else all projects'.
type ThreadSandbox struct {
	ExcludedCommands []string
	Allow            []string
}

// Empty reports whether it opens nothing.
func (t ThreadSandbox) Empty() bool { return len(t.ExcludedCommands)+len(t.Allow) == 0 }

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
	"python", "python3", "node", "deno", "bun", "ruby", "perl", "php", "lua", "osascript", "awk", "make",
	"npx", "bunx", "pnpx", "uvx", "pipx", "npm", "pnpm", "yarn", "uv", "docker", "podman", "kubectl", "ssh", "git"}

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
		return fmt.Errorf("%q is longer than %d characters", e[:40]+"…", maxSandboxEntry)
	case !textsafe.Safe(e):
		return fmt.Errorf("%q has control or invisible characters", textsafe.Clean(e, false))
	case strings.TrimSpace(e) != e:
		return fmt.Errorf("%q has spaces around it", e)
	}
	return nil
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
	switch {
	case strings.Trim(e, matchAll) == "":
		return fmt.Errorf("%q matches every command: that is no sandbox at all", e)
	case strings.HasPrefix(e, "Bash(") || strings.HasPrefix(e, "PowerShell("):
		return fmt.Errorf("%q is a permission rule: an excluded command is the command itself, e.g. npx playwright *", e)
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

// wholeTools are the tools whose rule may never match every call: every
// command, or every file written. Edit allow rules also open the Bash
// sandbox for writing.
var wholeTools = []string{"Bash", "PowerShell", "Edit", "Write", "MultiEdit", "NotebookEdit"}

// CheckSandboxAllow checks one allow rule: Claude's permission rule
// syntax (Tool or Tool(what), e.g. Bash(npx playwright *)), never one
// that lets every command run or every file be written without a
// prompt. tm's own deny rules win over it either way.
func CheckSandboxAllow(e string) error {
	if err := checkSandboxText(e); err != nil {
		return err
	}
	m := allowRule.FindStringSubmatch(e)
	if m == nil {
		return fmt.Errorf("%q isn't a permission rule: Tool or Tool(what), e.g. Bash(npx playwright *)", e)
	}
	if slices.Contains(wholeTools, m[1]) && strings.Trim(m[2], matchAll) == "" {
		return fmt.Errorf("%q allows every %s call: name what it is for, e.g. %s(npx playwright *)", e, m[1], m[1])
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

// SandboxWarning is why an entry of key's list, which passes its check,
// still opens more than it seems to, or "": a program that runs others
// with its arguments left open, a tool allowed whole, an Edit rule
// (which opens the Bash sandbox for writing there too).
func SandboxWarning(key, e string) string {
	cmd := e
	if key == SandboxAllow {
		m := allowRule.FindStringSubmatch(e)
		if m == nil {
			return ""
		}
		switch {
		case m[2] == "" || strings.Trim(m[2], matchAll) == "":
			return "allows every " + m[1] + " call without a prompt"
		case m[1] == "Edit" || m[1] == "Write" || m[1] == "MultiEdit" || m[1] == "NotebookEdit":
			return "also lets the thread's sandboxed commands write there"
		case m[1] != "Bash" && m[1] != "PowerShell":
			return ""
		}
		if c, ok := strings.CutSuffix(m[2], ":*"); ok {
			cmd = c + " *"
		} else {
			cmd = m[2]
		}
	}
	fixed, _, open := strings.Cut(cmd, "*")
	if !open {
		return ""
	}
	words := strings.Fields(fixed)
	if len(words) == 1 && slices.Contains(broad, strings.TrimSuffix(words[0], ":")) {
		return words[0] + " runs other programs or code: with its arguments open, this lets through anything it can run"
	}
	return ""
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
