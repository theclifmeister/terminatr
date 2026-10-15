package mod

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// anythingRunners are the programs that run whatever they are told to:
// shells, language interpreters and launchers. A commands grant of one
// is a grant to run anything, as the human, so the consent says so.
var anythingRunners = []string{
	// shells
	"sh", "bash", "zsh", "fish", "dash", "ksh", "mksh", "csh", "tcsh", "ash", "nu", "elvish", "xonsh", "busybox", "toybox",
	// interpreters
	"python", "pythonw", "py", "pypy", "node", "nodejs", "deno", "bun", "perl", "ruby", "irb", "php", "lua", "luajit",
	"tclsh", "wish", "r", "rscript", "julia", "awk", "gawk", "mawk", "nawk", "jshell", "groovy", "scala", "java",
	// launchers
	"env", "xargs", "nohup", "sudo", "doas", "su", "nice", "timeout", "time", "stdbuf", "setsid", "script", "watch",
	"parallel", "open", "launchctl", "systemd-run", "flatpak-spawn", "osascript", "automator", "shortcuts",
	// Windows
	"cmd", "powershell", "pwsh", "wsl", "wscript", "cscript", "mshta", "rundll32", "start", "runas",
}

// RunsAnything reports whether prog, a commands grant's program, runs
// whatever it is told to (anythingRunners), by its name: case, a
// Windows extension and a version (python3.12, perl5.36) aside.
func RunsAnything(prog string) bool {
	p := strings.ToLower(prog)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		p = strings.TrimSuffix(p, ext)
	}
	p = strings.TrimRight(p, "0123456789.-")
	return slices.Contains(anythingRunners, p)
}

// loginTools are the programs that run on a login of the human's own,
// with what it is a login to.
var loginTools = map[string]string{"gh": "GitHub", "glab": "GitLab"}

// commandWords are p's commands in the consent's words: "runs jq, git;
// runs glab with your GitLab login; runs sh: anything it wants, as you".
func (p Permissions) commandWords() string {
	var plain, parts []string
	for _, c := range p.Commands {
		switch {
		case RunsAnything(c):
			parts = append(parts, "runs "+c+": anything it wants, as you")
		case loginTools[c] != "":
			parts = append(parts, "runs "+c+" with your "+loginTools[c]+" login")
		default:
			plain = append(plain, c)
		}
	}
	if len(plain) > 0 {
		parts = append([]string{"runs " + strings.Join(plain, ", ")}, parts...)
	}
	return strings.Join(parts, "; ")
}

// userHome is the human's home folder, "" when unknown.
var userHome = func() string {
	h, _ := os.UserHomeDir()
	return h
}

// holdsHome reports whether files root r (~/x or absolute, as checked)
// is the home folder, holds it, or is a file system's root.
func holdsHome(r, home string) bool {
	if rest, ok := strings.CutPrefix(r, "~"); ok {
		return filepath.Clean("/"+filepath.ToSlash(rest)) == "/"
	}
	c := filepath.Clean(r)
	if filepath.Dir(c) == c {
		return true
	}
	return home != "" && fsx.Under(home, c)
}

// rootWords is files root r in the consent's words: as written, and
// said plainly when it is or holds the home folder.
func rootWords(r, home string) string {
	if !holdsHome(r, home) {
		return r
	}
	switch c := filepath.Clean(r); {
	case strings.HasPrefix(r, "~"), fsx.Under(c, home):
		return r + " (everything in your home folder)"
	case filepath.Dir(c) == c:
		return r + " (the whole disk, your home folder too)"
	}
	return r + " (your home folder and more)"
}
