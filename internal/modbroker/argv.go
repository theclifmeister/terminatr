package modbroker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

// CheckRun refuses argv a mod granted commands may not run, by its words
// alone: the size limits, a program its commands don't name, and an
// option of the program's that would point it at another folder or
// repository, change its configuration or name a program for it to run
// (argRules). The paths among the arguments are Paths.CheckArgs'.
func CheckRun(argv, commands []string) error {
	if len(argv) == 0 || len(argv) > maxArgs {
		return BadParams("process.run: argv has 1 to %d strings", maxArgs)
	}
	size := 0
	for _, s := range argv {
		size += len(s)
		if strings.ContainsRune(s, 0) {
			return BadParams("process.run: an argument has a NUL in it")
		}
	}
	if size > maxArgBytes {
		return BadParams("process.run: the arguments are too long")
	}
	if !slices.Contains(commands, argv[0]) {
		return Denied("process.run: %q isn't one of the mod's commands", argv[0])
	}
	if rule, ok := argRules[progName(argv[0])]; ok {
		if why := rule(argv[1:]); why != "" {
			return Denied("process.run: %s %s", argv[0], why)
		}
	}
	return nil
}

// gitSafeConfig is the configuration every git a mod runs has, given on
// its command line, above any repository's: no program it names from a
// repository's config (fsmonitor, hooks, a pager) and no repository it
// finds that is bare, which the mod could have written itself (fs.write
// keeps it out of .git folders only).
var gitSafeConfig = []string{
	"core.fsmonitor=false",
	"core.hooksPath=" + os.DevNull,
	"core.pager=cat",
	"protocol.ext.allow=never",
	"safe.bareRepository=explicit",
}

// RunArgv is argv, which CheckRun let through, as the broker runs it:
// git with gitSafeConfig first.
func RunArgv(argv []string) []string {
	if progName(argv[0]) != "git" {
		return argv
	}
	out := []string{argv[0]}
	for _, kv := range gitSafeConfig {
		out = append(out, "-c", kv)
	}
	return append(out, argv[1:]...)
}

// progName is a program's name as argRules has it: git.exe and Git are
// git too, on a file system that ignores case.
func progName(prog string) string {
	return strings.TrimSuffix(strings.ToLower(prog), ".exe")
}

// argRules say why a program's arguments are refused, or "".
var argRules = map[string]func(args []string) string{
	"git":  gitArgs,
	"gh":   loginToolArgs,
	"glab": loginToolArgs,
}

// loginToolRefused are the gh and glab commands a mod may not run: ones
// that show or change the login, the tool's settings, its aliases (which
// may run a shell) and its extensions (programs of their own).
var loginToolRefused = []string{"auth", "config", "alias", "extension", "extensions", "ext"}

// loginToolArgs refuses a gh or glab command of loginToolRefused, but
// for "auth status" without the option that prints the login's token.
func loginToolArgs(args []string) string {
	i := slices.IndexFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") })
	if i < 0 || !slices.Contains(loginToolRefused, args[i]) {
		return ""
	}
	if args[i] == "auth" && i+1 < len(args) && args[i+1] == "status" {
		for _, a := range args[i+2:] {
			if a == "--" {
				break
			}
			name := optName(a)
			long := strings.HasPrefix(name, "--")
			if long && len(name) > 3 && (strings.HasPrefix("--show-token", name) || name == "--show") ||
				!long && strings.HasPrefix(name, "-") && strings.ContainsRune(name, 't') {
				return "auth status may not show the token"
			}
		}
		return ""
	}
	return "takes no " + args[i] + " command"
}

// gitGlobals are the options git may have before its command: none
// that names a folder, a repository or a setting.
var gitGlobals = []string{
	"-p", "--paginate", "-P", "--no-pager", "--no-replace-objects", "--no-lazy-fetch",
	"--no-optional-locks", "--no-advice", "--literal-pathspecs", "--glob-pathspecs",
	"--noglob-pathspecs", "--icase-pathspecs", "-v", "--version", "-h", "--help",
}

// gitRefused are options refused anywhere: a repository or folder other
// than the working one, or a program for git to run. A long option may
// be shortened, so a prefix of one counts.
var gitRefused = []string{
	"--git-dir", "--work-tree", "--exec-path", "--config-env",
	"--upload-pack", "--receive-pack", "--exec",
}

// gitConfigReads are the git config forms that only read.
var gitConfigReads = []string{"get", "list", "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--get-color", "--get-colorbool", "--list", "-l"}

// gitConfigWrites are the git config forms that change a setting.
var gitConfigWrites = []string{"set", "unset", "edit", "rename-section", "remove-section", "-e", "--edit",
	"--add", "--replace-all", "--unset", "--unset-all", "--rename-section", "--remove-section"}

func gitArgs(args []string) string {
	cmd := -1
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			cmd = i
			break
		}
		if !slices.Contains(gitGlobals, a) {
			return "takes no " + optName(a) + " option before its command; the working folder is options.cwd"
		}
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name := optName(a)
		if len(name) <= 2 {
			continue
		}
		for _, r := range gitRefused {
			if strings.HasPrefix(r, name) {
				return "takes no " + r + " option"
			}
		}
	}
	if cmd >= 0 {
		if why := gitRuns(args[cmd], args[cmd+1:]); why != "" {
			return why
		}
	}
	if cmd >= 0 && args[cmd] == "config" {
		rest := args[cmd+1:]
		read := false
		for _, a := range rest {
			name := optName(a)
			if slices.Contains(gitConfigWrites, name) {
				return "config only reads settings"
			}
			read = read || slices.Contains(gitConfigReads, name)
		}
		if !read {
			return "config only reads settings (get, list, --get, --list)"
		}
	}
	return ""
}

// gitRunsCommands are git commands that run a program or shell command
// they are given, ask the credential helpers for a login, or serve.
var gitRunsCommands = []string{
	"filter-branch", "send-email", "credential", "credential-cache", "credential-store",
	"daemon", "instaweb", "web--browse", "upload-pack", "receive-pack", "upload-archive", "shell",
}

// gitRunOptions are, by git command, its options that take a program or
// shell command to run, or that set the configuration of the repository
// it makes (which may name a program: core.fsmonitor, an alias), seed it
// from a template, or make it where git won't find it as a .git folder
// (bare, or its git folder elsewhere), so the mod could write its config:
// the short ones and the long ones (a prefix of a long one counts).
var gitRunOptions = map[string]struct{ short, long []string }{
	"rebase":   {[]string{"x"}, []string{"--exec"}},
	"difftool": {[]string{"x"}, []string{"--extcmd"}},
	"grep":     {[]string{"O"}, []string{"--open-files-in-pager"}},
	"clone": {[]string{"u", "c"}, []string{"--upload-pack", "--config", "--template",
		"--separate-git-dir", "--bare", "--mirror"}},
	"init": {nil, []string{"--template", "--separate-git-dir", "--bare"}},
	"help": {[]string{"w"}, []string{"--web"}},
}

// gitConfigSubcommands are, by git command, its commands that change the
// human's git config (git maintenance register adds the repository to
// it, and start schedules git to run there too).
var gitConfigSubcommands = map[string][]string{
	"maintenance": {"register", "unregister", "start", "stop"},
}

// gitRuns says why git command cmd, with its arguments rest, is refused
// for running a program it is given, or "".
func gitRuns(cmd string, rest []string) string {
	if slices.Contains(gitRunsCommands, cmd) || strings.HasPrefix(cmd, "credential-") {
		return "takes no " + cmd + " command"
	}
	sub := slices.IndexFunc(rest, func(a string) bool { return !strings.HasPrefix(a, "-") })
	switch {
	case cmd == "bisect" && sub >= 0 && rest[sub] == "run",
		cmd == "submodule" && sub >= 0 && rest[sub] == "foreach",
		sub >= 0 && slices.Contains(gitConfigSubcommands[cmd], rest[sub]):
		return "takes no " + cmd + " " + rest[sub]
	}
	opts, ok := gitRunOptions[cmd]
	if !ok {
		return ""
	}
	for _, a := range rest {
		if a == "--" {
			break
		}
		name := optName(a)
		switch {
		case strings.HasPrefix(name, "--"):
			for _, l := range opts.long {
				if len(name) > 2 && strings.HasPrefix(l, name) {
					return cmd + " takes no " + l + " option"
				}
			}
		case strings.HasPrefix(name, "-"):
			// A cluster (-ix) or an attached value (-xmake) holds it too.
			for _, sh := range opts.short {
				if strings.Contains(name[1:], sh) {
					return cmd + " takes no -" + sh + " option"
				}
			}
		}
	}
	return ""
}

// optName is an option without its =value.
func optName(a string) string {
	name, _, _ := strings.Cut(a, "=")
	return name
}

// CheckArgs refuses an argument that names a file or folder outside what
// the mod may read, the program running in dir (a checked folder): an
// absolute path, a ~ one, or a relative one that leads out of dir or
// through a link, as the argument itself, an option's value (--out=x,
// -fx), a key=value's value or an @file.
func (p Paths) CheckArgs(args []string, dir string) error {
	return p.checkArgs(args, dir, func(path string) error {
		_, err := p.Check(path, false)
		return err
	})
}

// CheckArgsByName is CheckArgs by the names only (CheckName).
func (p Paths) CheckArgsByName(args []string, dir string) error {
	return p.checkArgs(args, dir, func(path string) error {
		_, err := p.CheckName(path, false)
		return err
	})
}

func (p Paths) checkArgs(args []string, dir string, check func(string) error) error {
	for _, a := range args {
		for _, c := range argPaths(a) {
			path := c
			switch {
			case c == "~" || strings.HasPrefix(c, "~/"), filepath.IsAbs(c):
			case caps.Windows && rootless(c):
				// /x alone is a switch to a Windows program.
				if !strings.ContainsAny(c[1:], `/\`) {
					continue
				}
			default:
				path = filepath.Join(dir, c)
			}
			if err := check(path); err != nil {
				return Denied("process.run: the argument %q: %v", a, err)
			}
		}
	}
	return nil
}

// argPaths are the words of an argument that may be paths: itself, what
// follows its first =, an option's attached value (-C/x → /x), what
// follows a leading @, a file:// URL's path and an absolute or ~ path
// after a colon (host:/x).
func argPaths(a string) []string {
	out := []string{a}
	if _, v, ok := strings.Cut(a, "="); ok {
		out = append(out, v)
	}
	if strings.HasPrefix(a, "-") {
		v := strings.TrimLeft(a, "-")
		v = strings.TrimLeft(v, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
		if v != "" {
			out = append(out, v)
		}
	}
	if v, ok := strings.CutPrefix(a, "@"); ok {
		out = append(out, v)
	}
	if _, v, ok := strings.Cut(a, "file://"); ok {
		out = append(out, v)
	}
	// host:/path, a:~/x, a list a:/b:/c: each part after a colon that is
	// an absolute or ~ path (not a URL's //host, nor a Windows drive's).
	for i, part := range strings.Split(a, ":") {
		if i == 0 || i == 1 && caps.Windows && len(a) > 1 && a[1] == ':' {
			continue
		}
		if part == "~" || strings.HasPrefix(part, "~/") ||
			strings.HasPrefix(part, "/") && !strings.HasPrefix(part, "//") {
			out = append(out, part)
		}
	}
	return slices.DeleteFunc(out, func(s string) bool { return s == "" })
}
