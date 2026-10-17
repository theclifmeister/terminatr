package mod

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/plat/fsx"
)

// narrowTools are the programs vetted to do no more than their
// arguments say: they read, filter or print, and have no option that
// runs a program or writes a file. A commands grant of any program not
// here (nor a loginTool) is a grant to run anything, as the human: a
// build tool reads a Makefile or a script the mod wrote itself, find and
// tar can run a command, ssh and rsync run a remote or helper program,
// and so on, so the list is of what is safe, not of what is not. An
// entry has no option, on GNU or BSD, that runs a program, writes a file
// or sets the system (so not tree -o, uniq IN OUT, file -C, date -s,
// diff -l, which runs pr, nor hostname NAME).
var narrowTools = []string{
	"jq", "grep", "egrep", "fgrep", "cat", "ls", "head", "tail", "wc", "cmp", "stat",
	"which", "echo", "pwd", "uname", "basename", "dirname", "realpath", "readlink", "cut", "tr",
	"comm", "nl", "column", "whoami", "true", "false", "printf", "du", "df", "shellcheck",
	"sha256sum", "sha1sum", "md5sum", "shasum",
}

// RunsAnything reports whether prog, a commands grant's program, may run
// whatever it is told to: it is not one of narrowTools, nor git (whose
// run-a-program options the broker refuses). gh and glab are not narrow:
// the broker refuses only their auth, config, alias and extension
// commands, not those that clone, check out, download or open an
// editor. The name counts but for case, a Windows extension and a
// version (python3.12).
func RunsAnything(prog string) bool {
	p := strings.ToLower(prog)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		p = strings.TrimSuffix(p, ext)
	}
	if slices.Contains(narrowTools, p) || p == "git" {
		return false
	}
	return true
}

// loginTools are the programs that run on a login of the human's own,
// with what it is a login to; the consent names the login as well.
var loginTools = map[string]string{"gh": "GitHub", "glab": "GitLab"}

// commandWords are p's commands in the consent's words: "runs jq, git;
// runs glab with your GitLab login; runs sh: anything it wants, as you".
func (p Permissions) commandWords() string {
	var plain, parts []string
	for _, c := range p.Commands {
		switch {
		case loginTools[c] != "":
			parts = append(parts, "runs "+c+" with your "+loginTools[c]+" login: anything it wants, as you")
		case RunsAnything(c):
			parts = append(parts, "runs "+c+": anything it wants, as you")
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
