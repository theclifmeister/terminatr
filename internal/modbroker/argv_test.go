package modbroker

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/plat/caps"
)

func TestCheckRunGit(t *testing.T) {
	cmds := []string{"git", "Git"}
	for _, argv := range [][]string{
		{"git", "-C", "../elsewhere", "status"},
		{"git", "-C../elsewhere", "status"},
		{"git", "-c", "core.pager=x", "log"},
		{"git", "-ccore.pager=x", "log"},
		{"git", "--config-env=core.pager=X", "log"},
		{"git", "--git-dir=/x/.git", "log"},
		{"git", "--git-dir", "/x/.git", "log"},
		{"git", "--work-tree=/x", "status"},
		{"git", "--exec-path=/x", "status"},
		{"git", "--namespace=x", "log"},
		{"git", "--bare", "log"},
		{"git", "log", "--git-dir=/x"},
		{"git", "fetch", "--upload-pack=x", "origin"},
		{"git", "fetch", "--upload-p=x", "origin"},
		{"git", "push", "--receive-pack=x"},
		{"git", "archive", "--exec=x", "--remote=origin"},
		{"git", "rebase", "--exec", "x", "main"},
		{"git", "log", "--ex"},
		{"git", "config", "core.pager", "x"},
		{"git", "config", "set", "core.pager", "x"},
		{"git", "config", "--get", "core.pager", "--add"},
		{"git", "config", "--global", "core.pager", "x"},
		{"git", "rebase", "-x", "make", "main"},
		{"git", "rebase", "-ix", "make", "main"},
		{"git", "rebase", "-xmake", "main"},
		{"git", "rebase", "--exe=make", "main"},
		{"git", "bisect", "run", "make"},
		{"git", "submodule", "foreach", "make"},
		{"git", "submodule", "--quiet", "foreach", "make"},
		{"git", "difftool", "-x", "make"},
		{"git", "difftool", "--extcmd=make"},
		{"git", "difftool", "--extc", "make"},
		{"git", "grep", "-Omake", "x"},
		{"git", "grep", "--open-files-in-pager=make", "x"},
		{"git", "clone", "-u", "make", "origin"},
		{"git", "help", "-w", "log"},
		{"git", "filter-branch", "--tree-filter", "make"},
		{"git", "send-email", "--to-cmd=make"},
		{"git", "credential", "fill"},
		{"git", "credential-osxkeychain", "get"},
		{"git", "daemon"},
		{"git", "--no-pager", "bisect", "run", "make"},
		{"Git", "credential", "fill"},
	} {
		if err := CheckRun(argv, cmds); code(err) != CodeDenied {
			t.Errorf("%q: %v", argv, err)
		}
	}
	for _, argv := range [][]string{
		{"git", "status", "--porcelain=v1", "-b"},
		{"git", "--no-pager", "-P", "log", "-c", "--format=%h/%s", "-n5"},
		{"git", "diff", "origin/main...HEAD", "--numstat", "-M"},
		{"git", "rev-parse", "--show-toplevel"},
		{"git", "config", "--get", "user.name"},
		{"git", "config", "get", "user.name"},
		{"git", "log", "--", "--exec"},
		{"git", "rebase", "-i", "main"},
		{"git", "rebase", "-Xours", "main"},
		{"git", "bisect", "start"},
		{"git", "submodule", "status"},
		{"git", "difftool", "--tool=vimdiff"},
		{"git", "grep", "-n", "x"},
		{"git", "clone", "--depth=1", "https://example.com/x.git"},
		{"git", "log", "--", "run"},
	} {
		if err := CheckRun(argv, cmds); err != nil {
			t.Errorf("%q: %v", argv, err)
		}
	}
	if err := CheckRun([]string{"git"}, nil); code(err) != CodeDenied {
		t.Errorf("an ungranted git: %v", err)
	}
}

func TestCheckRunLoginTools(t *testing.T) {
	cmds := []string{"gh", "glab"}
	for _, argv := range [][]string{
		{"gh", "auth", "token"},
		{"gh", "auth", "status", "--show-token"},
		{"gh", "auth", "status", "-t"},
		{"gh", "auth", "status", "-ht"},
		{"gh", "auth", "login"},
		{"gh", "auth"},
		{"gh", "config", "get", "oauth_token"},
		{"gh", "alias", "set", "x", "y", "--shell"},
		{"gh", "extension", "install", "x/y"},
		{"gh", "ext", "install", "x/y"},
		{"glab", "auth", "status", "--show-token"},
		{"glab", "auth", "status", "--hostname", "gitlab.com", "-t"},
		{"glab", "config", "get", "token"},
		{"glab", "alias", "set", "x", "!y"},
	} {
		if err := CheckRun(argv, cmds); code(err) != CodeDenied {
			t.Errorf("%q: %v", argv, err)
		}
	}
	for _, argv := range [][]string{
		{"gh", "pr", "view", "12", "--json", "state"},
		{"gh", "auth", "status"},
		{"gh", "auth", "status", "--hostname", "github.com"},
		{"glab", "auth", "status", "--hostname", "gitlab.com"},
		{"glab", "api", "--hostname", "gitlab.com", "projects/1"},
		{"glab", "version"},
	} {
		if err := CheckRun(argv, cmds); err != nil {
			t.Errorf("%q: %v", argv, err)
		}
	}
}

func TestArgPaths(t *testing.T) {
	for a, want := range map[string]string{
		"status":          "status",
		"--out=/x":        "--out=/x|/x|=/x",
		"-f/x":            "-f/x|/x",
		"-n5":             "-n5",
		"@/x":             "@/x|/x",
		"if=/x":           "if=/x|/x",
		"file:///x":       "file:///x|/x",
		"--url=file:///x": "--url=file:///x|file:///x|=file:///x|/x",
		"a:/x":            "a:/x|/x",
		"a:~/x":           "a:~/x|~/x",
		"a:/x:/y":         "a:/x:/y|/x|/y",
		"a:b":             "a:b",
		"https://h/x":     "https://h/x",
		"git@h:org/r.git": "git@h:org/r.git",
	} {
		if got := strings.Join(argPaths(a), "|"); got != want {
			t.Errorf("argPaths(%q) = %s, want %s", a, got, want)
		}
	}
}

func TestRunArgPaths(t *testing.T) {
	if caps.Windows {
		t.Skip("echo is a Unix program")
	}
	tb := newTestBroker(t, mod.PermCommands, mod.PermFiles)
	home, own := tb.f.home, tb.f.p.Own
	run := func(cwd string, args ...string) (RunResult, error) {
		t.Helper()
		opts := map[string]any{}
		if cwd != "" {
			opts["cwd"] = cwd
		}
		got, err := tb.call(t, "process.run", map[string]any{"argv": append([]string{"echo"}, args...), "options": opts})
		res, _ := got.(RunResult)
		return res, err
	}
	away := filepath.Join(home, "elsewhere", "c.txt")
	up, _ := filepath.Rel(own, filepath.Join(home, "elsewhere"))
	for _, c := range []struct {
		cwd  string
		args []string
	}{
		{"", []string{away}},
		{"", []string{"~/.ssh/id_rsa"}},
		{"", []string{"~/elsewhere"}},
		{"", []string{up}},
		{"", []string{"../other/x.txt"}},
		{"", []string{"--file=" + away}},
		{"", []string{"-f" + away}},
		{"", []string{"@" + away}},
		{"", []string{"if=" + away}},
		{"", []string{"file://" + away}},
		{"", []string{"ok", "--out=" + up}},
		{"", []string{"localhost:" + away}},
		{"", []string{"a:~/elsewhere"}},
		{"", []string{"--path=" + own + ":" + away}},
		{"~/notes", []string{"link-ssh/id_rsa"}},
		{"~/notes", []string{"link-else"}},
		{"~/notes", []string{"repo/.git/config"}},
		{"~/notes", []string{".env"}},
		{"~/notes", []string{"../elsewhere"}},
		{"~/notes", []string{filepath.Join(home, ".terminatr", "config.toml")}},
	} {
		if _, err := run(c.cwd, c.args...); code(err) != CodeDenied {
			t.Errorf("echo %q in %q: %v", c.args, c.cwd, err)
		}
	}
	for _, c := range []struct {
		cwd  string
		args []string
	}{
		{"", []string{"own.txt", "HEAD..main", "--format=%h/%s", "-n5", "a b", "-"}},
		{"", []string{filepath.Join(own, "own.txt")}},
		{"", []string{"~/notes/a.txt", "--in=~/notes/sub"}},
		{"~/notes", []string{"a.txt", "sub/b.txt", "sub/..", "../notes/a.txt", "file://" + filepath.Join(home, "notes")}},
	} {
		if res, err := run(c.cwd, c.args...); err != nil || res.Code != 0 {
			t.Errorf("echo %q in %q = %+v, %v", c.args, c.cwd, res, err)
		}
	}
	if !tb.audited("denied process.run") {
		t.Errorf("refusal not audited: %q", tb.audit)
	}
}

// TestRunGitCeiling: git run in the mod's folder doesn't find a
// repository above the folder it is in.
func TestRunGitCeiling(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil || caps.Windows {
		t.Skip("no git")
	}
	tb := newTestBroker(t, mod.PermCommands, mod.PermFiles)
	if out, err := exec.Command(git, "init", "-q", tb.f.home).CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	for _, cwd := range []string{"", "~/notes/sub"} {
		got, err := tb.call(t, "process.run", map[string]any{"argv": []string{"git", "rev-parse", "--show-toplevel"}, "cwd": cwd})
		if err != nil {
			t.Fatal(err)
		}
		if res := got.(RunResult); res.Code == 0 {
			t.Errorf("git in %q found %s", cwd, res.Stdout)
		}
	}
	if out, err := exec.Command(git, "init", "-q", filepath.Join(tb.f.home, "notes", "sub")).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	got, err := tb.call(t, "process.run", map[string]any{"argv": []string{"git", "rev-parse", "--show-toplevel"}, "cwd": "~/notes/sub"})
	if res, _ := got.(RunResult); err != nil || res.Code != 0 {
		t.Errorf("git in a repository under the root = %+v, %v", got, err)
	}
}
