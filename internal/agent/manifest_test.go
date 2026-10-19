package agent

import (
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestHookexecFunc(t *testing.T) {
	f := funcs["hookexec"].(func(string, ...string) (string, error))
	got, err := f(`C:\Program Files\tm.exe`, "hook", "--agent", "claude")
	want := `"command":"C:\\Program Files\\tm.exe","args":["hook","--agent","claude"]`
	if err != nil || got != want {
		t.Fatalf("hookexec = %s, %v; want %s", got, err, want)
	}
	if got, _ := f("/bin/tm"); got != `"command":"/bin/tm","args":[]` {
		t.Fatalf("hookexec without args = %s", got)
	}
}

func TestPosixPath(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\a\p`: "/c/Users/a/p",
		`d:/x`:         "/d/x",
		`C:`:           "/c",
		"/home/u":      "/home/u",
	} {
		if got := posixPath(in); got != want {
			t.Errorf("posixPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeGlob(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u/p":          "/home/u/p",
		"/home/u/my project": "/home/u/my project",
		"/home/u/[work]/app": `/home/u/\[work\]/app`,
		"/a/{x,y}/b*?!":      `/a/\{x,y\}/b\*\?\!`,
	} {
		if got := escapeGlob(in); got != want {
			t.Errorf("escapeGlob(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSiblingGlobs: the patterns for a folder's siblings match every
// sibling name made of nameChars, and never the folder itself nor what
// is in it.
func TestSiblingGlobs(t *testing.T) {
	if got := siblingGlobs(`C:\r\.git\worktrees\t-0005-x`); got != nil {
		t.Errorf("a Windows path: %q", got)
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX paths")
	}
	own := "/r/.git/worktrees/t-0005-x"
	globs := siblingGlobs(own)
	if len(globs) != 2*len("t-0005-x") {
		t.Errorf("%d patterns: %q", len(globs), globs)
	}
	// As Claude's sandbox reads them (2.1.303): regexp characters but
	// brackets quoted, * any run of characters but /, and a pattern
	// covers what is below what it matches.
	quote := strings.NewReplacer(".", `\.`, "^", `\^`, "$", `\$`, "+", `\+`, "{", `\{`, "}", `\}`,
		"(", `\(`, ")", `\)`, "|", `\|`, `\`, `\\`, "*", "[^/]*", "?", "[^/]")
	match := func(p string) bool {
		for _, g := range globs {
			if regexp.MustCompile("^" + quote.Replace(g) + "(/.*)?$").MatchString(p) {
				return true
			}
		}
		return false
	}
	for _, n := range []string{"t-0006-y", "t-0005-y", "t-0005-x1", "t-0005", "t-0005-", "t", "t-00051-x", "feature", "T-0005-x", "a_b", "-x"} {
		if !match("/r/.git/worktrees/" + n) {
			t.Errorf("sibling %s not matched", n)
		}
		if !match("/r/.git/worktrees/" + n + "/config.worktree") {
			t.Errorf("sibling %s's config.worktree not matched", n)
		}
	}
	for _, p := range []string{own, own + "/HEAD", own + "/index", "/r/.git/worktrees", "/r/.git/config", "/r/.git/worktrees2/a"} {
		if match(p) {
			t.Errorf("%s matched", p)
		}
	}
	if got := siblingGlobs("/r/[x]/.git/worktrees/a"); got != nil {
		t.Errorf("glob characters in the path: %q", got)
	}
	if got := charClass('t'); got != "[-0-9A-Z_a-su-z]" {
		t.Errorf("charClass(t) = %s", got)
	}
	if got := charClass('-'); got != "[0-9A-Z_a-z]" {
		t.Errorf("charClass(-) = %s", got)
	}
}

func TestRulesEscapeGlobs(t *testing.T) {
	rules := funcs["rules"].(func(string, []string) []string)
	dirs := []string{"/Users/a/[work]/app"}
	if got := rules("Edit(/%s/**)", dirs); got[0] != `Edit(//Users/a/\[work\]/app/**)` {
		t.Errorf("Edit rule = %q", got[0])
	}
	if got := rules("Read(/%s/**)", dirs); got[0] != `Read(//Users/a/\[work\]/app/**)` {
		t.Errorf("Read rule = %q", got[0])
	}
	// A command rule is not a path glob.
	if got := rules("Bash(%s:*)", []string{"git [x]"}); got[0] != "Bash(git [x]:*)" {
		t.Errorf("Bash rule = %q", got[0])
	}
}
