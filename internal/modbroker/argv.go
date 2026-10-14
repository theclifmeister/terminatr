package modbroker

import (
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
	if rule, ok := argRules[argv[0]]; ok {
		if why := rule(argv[1:]); why != "" {
			return Denied("process.run: %s %s", argv[0], why)
		}
	}
	return nil
}

// argRules say why a program's arguments are refused, or "".
var argRules = map[string]func(args []string) string{
	"git": gitArgs,
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
// follows a leading @ and a file:// URL's path.
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
	return slices.DeleteFunc(out, func(s string) bool { return s == "" })
}
