package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/worktree"
)

// parseWith declares a subcommand's flags, parses args and runs its
// parser, as runThread does.
func parseWith[O any](t *testing.T, flags func(f *flagSet) func([]string) (O, error), args ...string) (O, error) {
	t.Helper()
	f := newFlags()
	parse := flags(f)
	pos, err := f.Parse(args)
	if err != nil {
		t.Fatalf("parse %q: %v", args, err)
	}
	return parse(pos)
}

// isUsage checks err is a usage error (exit 2) naming want.
func isUsage(t *testing.T, err error, want string) {
	t.Helper()
	u, ok := err.(*usageError)
	if !ok || !strings.Contains(u.msg, want) {
		t.Errorf("err = %v, want a usage error with %q", err, want)
	}
}

func TestThreadStartFlags(t *testing.T) {
	o, err := parseWith(t, threadStartFlags, "Fix it", "--task", "T3", "--agent", "codex", "--model", "m",
		"--repo", "r", "--base", "b", "--approved-by-user", "--over-cap")
	want := startOpts{title: "Fix it", task: "T3", agent: "codex", model: "m", repo: "r", base: "b", approved: true, overCap: true}
	if err != nil || o != want {
		t.Errorf("got %+v, %v; want %+v", o, err, want)
	}
	if o, err := parseWith(t, threadStartFlags); err != nil || o != (startOpts{}) {
		t.Errorf("no args: %+v, %v", o, err)
	}
	_, err = parseWith(t, threadStartFlags, "Fix", "it")
	isUsage(t, err, "quote the title")
}

func TestThreadSubcommandFlags(t *testing.T) {
	if o, err := parseWith(t, adoptFlags, "s-4", "--title", "x", "--approved-by-user"); err != nil || o != (adoptOpts{session: "s-4", title: "x"}) {
		t.Errorf("adopt: %+v, %v", o, err)
	}
	if all, err := parseWith(t, listFlags, "--all"); err != nil || !all {
		t.Errorf("list --all: %v, %v", all, err)
	}
	_, err := parseWith(t, listFlags, "x")
	isUsage(t, err, "tm thread list")

	if o, err := parseWith(t, readFlags, "t-0001"); err != nil || o != (readOpts{"t-0001", 40}) {
		t.Errorf("read: %+v, %v", o, err)
	}
	if o, err := parseWith(t, readFlags, "t-0001", "--lines", "5"); err != nil || o.lines != 5 {
		t.Errorf("read --lines 5: %+v, %v", o, err)
	}
	_, err = parseWith(t, readFlags, "t-0001", "--lines", "0")
	isUsage(t, err, "--lines takes a positive number")

	if o, err := parseWith(t, promptFlags, "t-0001", "go on"); err != nil || o != (promptOpts{id: "t-0001", text: "go on"}) {
		t.Errorf("prompt: %+v, %v", o, err)
	}
	if o, err := parseWith(t, promptFlags, "t-0001", "--next", "2"); err != nil || o != (promptOpts{id: "t-0001", next: "2"}) {
		t.Errorf("prompt --next: %+v, %v", o, err)
	}
	if o, err := parseWith(t, promptFlags, "t-0001", "go on", "--now"); err != nil || o != (promptOpts{id: "t-0001", text: "go on", mode: proto.PromptNow}) {
		t.Errorf("prompt --now: %+v, %v", o, err)
	}
	if o, err := parseWith(t, promptFlags, "t-0001", "--next", "1", "--interrupt"); err != nil || o != (promptOpts{id: "t-0001", next: "1", mode: proto.PromptInterrupt}) {
		t.Errorf("prompt --next --interrupt: %+v, %v", o, err)
	}
	for _, args := range [][]string{{"t-0001"}, {"t-0001", "x", "--next", "2"}, {"t-0001", "a", "b"}, {"t-0001", "x", "--now", "--interrupt"}} {
		_, err = parseWith(t, promptFlags, args...)
		isUsage(t, err, "tm thread prompt <id>")
	}

	if o, err := parseWith(t, approveFlags, "t-0001"); err != nil || o != (approveOpts{"t-0001", 1}) {
		t.Errorf("approve: %+v, %v", o, err)
	}
	_, err = parseWith(t, approveFlags, "t-0001", "--choice", "10")
	isUsage(t, err, "--choice takes a number 1-9")

	resolve := func(f *flagSet) func([]string) (resolveOpts, error) { return resolveFlags(f, "resolve") }
	if o, err := parseWith(t, resolve, "t-0001", "--discard"); err != nil || o != (resolveOpts{"t-0001", true}) {
		t.Errorf("resolve --discard: %+v, %v", o, err)
	}
	// Only resolve takes --discard.
	f := newFlags()
	resolveFlags(f, "stop")
	if _, err := f.Parse([]string{"t-0001", "--discard"}); err == nil {
		t.Error("stop took --discard")
	}
}

func TestThreadAnswerFlags(t *testing.T) {
	a, err := parseWith(t, answerFlags, "t-0001", "--choice", "1, 3", "--option", "Pear", "--text", "kiwi", "--question", "2")
	want := answerArgs{id: "t-0001", choices: []int{1, 3}, options: []string{"Pear"}, text: "kiwi", withText: true, question: 2}
	if err != nil || !reflect.DeepEqual(a, want) {
		t.Errorf("got %+v, %v; want %+v", a, err, want)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"t-0001"}, "answer with --choice N"},
		{[]string{"t-0001", "--choice", "1,x"}, "--choice takes a number 1-9"},
		{[]string{"t-0001", "--text", "x", "--question", "5"}, "--question takes a number 1-4"},
		{[]string{"--text", "x"}, "usage: tm thread answer"},
	} {
		_, err := parseWith(t, answerFlags, c.args...)
		isUsage(t, err, c.want)
	}
}

// stateHost is a code host whose PRs are all in one state.
type stateHost struct {
	codehost.NoHost
	state string
}

func (s stateHost) PRState(string, string) string { return s.state }

// TestBranchCleanup: resolve deletes the thread's own branch only when
// nothing is lost, and says why it kept it otherwise.
func TestBranchCleanup(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	repo, rec := threadWithWorktree(t, h, "demo")
	p, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.RemoveForce(repo, rec.Worktree); err != nil {
		t.Fatal(err)
	}
	host := &stateHost{}
	old := pickHost
	pickHost = func(string, codehost.Config) codehost.Host { return host }
	t.Cleanup(func() { pickHost = old })
	branch := rec.Branch
	check := func(discard bool, wantDid string, deleted bool) {
		t.Helper()
		did, gone := branchCleanup(p, rec, discard)
		if got := strings.Join(did, "; "); got != wantDid {
			t.Errorf("did %q, want %q", got, wantDid)
		}
		if (len(gone) == 1) != deleted || worktree.BranchExists(repo, branch) == deleted {
			t.Errorf("deleted %q, branch exists %v; want deleted %v", gone, worktree.BranchExists(repo, branch), deleted)
		}
	}
	commit := func() {
		t.Helper()
		gittest.Run(t, repo, "branch", "-f", branch, "main")
		wt := filepath.Join(t.TempDir(), "wt")
		gittest.Run(t, repo, "worktree", "add", "-q", wt, branch)
		os.WriteFile(filepath.Join(wt, "g"), []byte("y"), 0o644)
		gittest.Run(t, wt, "add", ".")
		gittest.Run(t, wt, "commit", "-q", "-m", "work")
		gittest.Run(t, repo, "worktree", "remove", wt)
	}

	commit()
	check(false, "kept branch "+branch+" (no merged PR found)", false)
	host.state = "OPEN"
	check(false, "kept branch "+branch+" (PR open)", false)
	host.state = "MERGED"
	check(false, "kept branch "+branch+" (PR merged, but unmerged commits on "+branch+")", false)
	check(true, "deleted branch "+branch+" (discarded)", true)
	if did, gone := branchCleanup(p, rec, false); did != nil || gone != nil {
		t.Errorf("no branch left: did %q, deleted %q", did, gone)
	}

	gittest.Run(t, repo, "branch", branch, "main")
	host.state = ""
	check(false, "deleted branch "+branch+" (merged into main)", true)
}
