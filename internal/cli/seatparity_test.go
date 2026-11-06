package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
)

// parityCommands are the tm commands that act on a seat's folder: what a
// coordinator runs for its project (the forwarded commands, tm access,
// search, skill and vault).
var parityCommands = []string{"access", "ask", "clear", "context", "design", "done", "history", "inbox", "library", "link", "memory", "project", "report", "search", "skill", "status", "task", "thread", "vault"}

// parityNoise are words usageActions takes for actions: the first word of
// a line of prose.
var parityNoise = map[string]bool{"search lists": true}

// sameAsCoordinator is a row's director for "as a coordinator, on its own
// folder": both reach the same answer.
const sameAsCoordinator = ""

// seatActions are every action of parityCommands: args run it, director
// is the refusal a director gets where a coordinator doesn't (or
// sameAsCoordinator), and why says what it does instead, or why not.
// A new action in a usage fails the test until it has a row.
var seatActions = []struct {
	action   string
	args     []string
	director string
	why      string
}{
	{"task add", []string{"task", "add", "x"}, "unknown-project", "a member's board: tm program route"},
	{"task list", []string{"task", "list"}, "unknown-project", "a member's board: --project <member>"},
	{"task show", []string{"task", "show", "T1"}, "unknown-project", "same"},
	{"task status", []string{"task", "status", "T1", "open"}, "unknown-project", "a member's task: tm program task"},
	{"task edit", []string{"task", "edit", "T1", "--title", "y"}, "unknown-project", "same"},
	{"task steps", []string{"task", "steps", "T1", "add", "s"}, "unknown-project", "same"},
	{"task archive", []string{"task", "archive", "T2"}, "unknown-project", "same"},
	{"task delegate", []string{"task", "delegate", "T1"}, "unknown-project", "tm program start"},
	{"task accept", []string{"task", "accept", "T1"}, "unknown-project", "safety: the user's alone, for both"},
	{"task send-back", []string{"task", "send-back", "T1", "--note", "n"}, "unknown-project", "safety: same"},
	{"context", []string{"context"}, "unknown-project", "tm program context"},
	{"inbox list", []string{"inbox", "list"}, sameAsCoordinator, ""},
	{"inbox read", []string{"inbox", "read", "x"}, sameAsCoordinator, ""},
	{"inbox done", []string{"inbox", "done", "x"}, sameAsCoordinator, ""},
	{"library add", []string{"library", "add", "x"}, sameAsCoordinator, ""},
	{"library list", []string{"library", "list"}, sameAsCoordinator, ""},
	{"library rm", []string{"library", "rm", "t-0001", "x"}, "human-only", "a program's own files are the user's alone to delete"},
	{"memory list", []string{"memory", "list"}, sameAsCoordinator, ""},
	{"memory find", []string{"memory", "find", "word"}, sameAsCoordinator, ""},
	{"memory new", []string{"memory", "new", "n", "--file", "-"}, sameAsCoordinator, ""},
	{"memory show", []string{"memory", "show", "n"}, sameAsCoordinator, ""},
	{"memory edit", []string{"memory", "edit", "n", "--file", "-"}, sameAsCoordinator, ""},
	{"memory rm", []string{"memory", "rm", "n"}, sameAsCoordinator, ""},
	{"memory check", []string{"memory", "check"}, "unknown-project", "checks a thread's memory work; a program has no threads"},
	{"memory index", []string{"memory", "index"}, sameAsCoordinator, ""},
	{"memory template", []string{"memory", "template"}, sameAsCoordinator, ""},
	{"ask add", []string{"ask", "add", "--file", "-"}, "unknown-project", "tm program ask"},
	{"ask list", []string{"ask", "list"}, "unknown-project", "same"},
	{"ask answer", []string{"ask", "answer", "Q1"}, sameAsCoordinator, ""},
	{"ask done", []string{"ask", "done", "Q1"}, "unknown-project", "same"},
	{"ask open", []string{"ask", "open"}, "unknown-project", "same"},
	{"design list", []string{"design", "list"}, "unknown-project", "a project's designs; a program's tasks name a member's with tm program task"},
	{"design set", []string{"design", "set", "x"}, "unknown-project", "same"},
	{"design archive", []string{"design", "archive", "x"}, "unknown-project", "same"},
	{"history", []string{"history"}, "unknown-project", "a project's numbers; a program has no board of its own"},
	{"clear", []string{"clear"}, sameAsCoordinator, ""},
	{"link list", []string{"link", "list"}, "unknown-project", "a project's links; the program channel is tm program send and read"},
	{"link send", []string{"link", "send", "other", "hi"}, "unknown-project", "same"},
	{"link read", []string{"link", "read", "M-1"}, "unknown-project", "same"},
	{"link log", []string{"link", "log", "other"}, "unknown-project", "same"},
	{"link files", []string{"link", "files"}, "unknown-project", "same"},
	{"link accept", []string{"link", "accept", "x"}, "unknown-project", "same"},
	{"link decline", []string{"link", "decline", "x"}, "unknown-project", "same"},
	{"link approve", []string{"link", "approve", "x"}, "unknown-project", "same"},
	{"link cancel", []string{"link", "cancel", "x"}, "unknown-project", "same"},
	{"link drop", []string{"link", "drop", "x"}, "unknown-project", "same"},
	{"link resume", []string{"link", "resume", "x"}, "unknown-project", "same"},
	{"thread list", []string{"thread", "list"}, "unknown-project", "a project's threads: --project <member>"},
	{"thread show", []string{"thread", "show", "t-0001"}, "unknown-project", "same"},
	{"thread start", []string{"thread", "start", "--task", "T1"}, "unknown-project", "tm program start"},
	{"thread stop", []string{"thread", "stop", "t-0001"}, "unknown-project", "a member's coordinator runs its threads"},
	{"thread resolve", []string{"thread", "resolve", "t-0001"}, "unknown-project", "same"},
	{"thread merge", []string{"thread", "merge", "t-0001"}, "unknown-project", "same"},
	{"thread prompt", []string{"thread", "prompt", "t-0001", "hi"}, "unknown-project", "same"},
	{"thread read", []string{"thread", "read", "t-0001"}, "unknown-project", "same"},
	{"thread answer", []string{"thread", "answer", "t-0001", "--choice", "1"}, "unknown-project", "safety: a director answers no thread's menu"},
	{"thread approve", []string{"thread", "approve", "t-0001"}, "unknown-project", "safety: same"},
	{"thread ack", []string{"thread", "ack", "t-0001"}, "unknown-project", "a member's coordinator runs its threads"},
	{"thread adopt", []string{"thread", "adopt", "x"}, "unknown-project", "same"},
	{"report", []string{"report", "--show"}, "thread-only", "a thread's"},
	{"status", []string{"status", "--percent", "5"}, "thread-only", "a thread's"},
	{"done", []string{"done"}, "thread-only", "a thread's"},
	{"project list", []string{"project", "list"}, sameAsCoordinator, ""},
	{"project new", []string{"project", "new", "zz"}, sameAsCoordinator, ""},
	{"project open", []string{"project", "open", "SELF"}, sameAsCoordinator, ""},
	{"project pause", []string{"project", "pause", "SELF"}, sameAsCoordinator, ""},
	{"project resume", []string{"project", "resume", "SELF"}, sameAsCoordinator, ""},
	{"project activate", []string{"project", "activate", "SELF"}, sameAsCoordinator, ""},
	{"project deactivate", []string{"project", "deactivate", "SELF"}, sameAsCoordinator, ""},
	{"project archive", []string{"project", "archive", "SELF"}, sameAsCoordinator, ""},
	{"project unarchive", []string{"project", "unarchive", "SELF"}, sameAsCoordinator, ""},
	{"project delete", []string{"project", "delete", "SELF"}, sameAsCoordinator, ""},
	{"project rename", []string{"project", "rename", "SELF", "zzz"}, sameAsCoordinator, ""},
	{"project link", []string{"project", "link", "demo", "web"}, sameAsCoordinator, ""},
	{"project repo", []string{"project", "repo", "add", "/x"}, "unknown-project", "a program has no repo"},
	{"project remote", []string{"project", "remote", "on", "SELF"}, sameAsCoordinator, ""},
	{"access show", []string{"access", "show"}, sameAsCoordinator, ""},
	{"access explain", []string{"access", "explain", "--command", "ls"}, sameAsCoordinator, ""},
	{"access add", []string{"access", "add", "--project", "demo"}, "human-only", "safety: a director's access is the human's"},
	{"access rm", []string{"access", "rm", "--project", "demo"}, "human-only", "safety: same"},
	{"access edit", []string{"access", "edit", "--project", "demo"}, "human-only", "safety: same"},
	{"access drop", []string{"access", "drop", "x"}, "human-only", "safety: same"},
	{"access undrop", []string{"access", "undrop", "x"}, "human-only", "safety: same"},
	{"access set", []string{"access", "set", "local-ports", "on", "demo"}, "human-only", "safety: same"},
	{"access reset", []string{"access", "reset", "demo"}, "human-only", "safety: same"},
	{"search", []string{"search", "x"}, "unknown-project", "tm program search"},
	{"search show", []string{"search", "show", "x"}, "unknown-project", "same"},
	{"search timeline", []string{"search", "timeline", "x"}, "unknown-project", "same"},
	{"skill coordinator", []string{"skill", "coordinator"}, sameAsCoordinator, ""},
	{"skill director", []string{"skill", "director"}, sameAsCoordinator, ""},
	{"skill thread", []string{"skill", "thread"}, sameAsCoordinator, ""},
	{"vault render", []string{"vault", "render"}, "coordinator-only", "gap (minor): a program's notes are in the vault; its director can't render them"},
	{"vault doctor", []string{"vault", "doctor"}, "refused", "gap (minor): a read-only check open to a coordinator"},
	{"vault init", []string{"vault", "init"}, sameAsCoordinator, ""},
	{"vault migrate", []string{"vault", "migrate"}, sameAsCoordinator, ""},
	{"vault off", []string{"vault", "off"}, sameAsCoordinator, ""},
	{"vault open", []string{"vault", "open"}, sameAsCoordinator, ""},
}

// subcommandRE finds a subcommand on a usage line: "tm task add …", or
// an indented "  add …" under a usage: tm task <command> head.
var subcommandRE = regexp.MustCompile(`^[a-z][a-z-]*(\|[a-z][a-z-]*)*$`)

// usageActions are the actions command name's usage names: "task add",
// "task status", or the command alone if it has no subcommands.
func usageActions(name string) []string {
	var out []string
	for _, line := range strings.Split(commandUsage(name), "\n") {
		rest := strings.TrimPrefix(strings.TrimSpace(line), "usage: ")
		var fields []string
		switch {
		case strings.HasPrefix(rest, "tm "+name+" "):
			fields = strings.Fields(strings.TrimPrefix(rest, "tm "+name+" "))
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   "):
			fields = strings.Fields(line)
		}
		if len(fields) > 0 && subcommandRE.MatchString(fields[0]) {
			for _, w := range strings.Split(fields[0], "|") {
				if !parityNoise[name+" "+w] {
					out = append(out, name+" "+w)
				}
			}
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) == 0 {
		return []string{name}
	}
	return out
}

// refusalRE is the code of a refusal on stderr: "tm task: human-only: …".
var refusalRE = regexp.MustCompile(`^tm [a-z ]+: ([a-z-]+):`)

// outcome is a command's result in a word: ok, the refusal's code, or its
// exit code.
func outcome(code int, errs string) string {
	if code == 0 {
		return "ok"
	}
	if m := refusalRE.FindStringSubmatch(errs); m != nil {
		return m[1]
	}
	return "exit " + strconv.Itoa(code)
}

// TestSeatParityCLI: every action of a seat's commands works for a director
// as for a coordinator, on its own folder, or names what a director gets
// instead. A new action in a usage fails until it has a row in seatActions.
func TestSeatParityCLI(t *testing.T) {
	h, _ := programHarness(t)
	h.ok(human, "task", "add", "T", "--project", "demo")
	h.ok(human, "task", "add", "T", "--project", "demo")

	var rows []string
	for _, r := range seatActions {
		rows = append(rows, r.action)
	}
	for _, name := range parityCommands {
		if !slices.Contains(Commands(), name) {
			t.Errorf("tm %s is no command", name)
		}
		for _, action := range usageActions(name) {
			if !slices.Contains(rows, action) {
				t.Errorf("tm %s has no row in seatActions: make it work for a director, or say why not", action)
			}
		}
	}

	run := func(c caller.Caller, args []string) string {
		h.env[caller.EnvProject] = c.Project
		args = slices.Clone(args)
		for i := range args {
			if args[i] == "SELF" {
				args[i] = c.Project
			}
		}
		code, _, errs := h.run(c, "# A note\n\nIts body.\n", args...)
		return outcome(code, errs)
	}
	coord := caller.Caller{Kind: caller.Coordinator, Project: "demo"}
	for _, r := range seatActions {
		c, d := run(coord, r.args), run(directorAcme, r.args)
		switch {
		case r.director == sameAsCoordinator && d != c:
			t.Errorf("tm %s: coordinator %q, director %q: make them the same, or say why not", r.action, c, d)
		case r.director != sameAsCoordinator && d != r.director:
			t.Errorf("tm %s: a director gets %q, want %q (%s)", r.action, d, r.director, r.why)
		case r.director != sameAsCoordinator && r.why == "":
			t.Errorf("tm %s: a director's difference needs its reason", r.action)
		}
	}
}

// TestDirectorFileRoots: a director's --file may be in its program's
// folder, as a coordinator's in its project's; not in a member's, nor in
// another program's.
func TestDirectorFileRoots(t *testing.T) {
	h, g := programHarness(t)
	prev := tempRoots
	tempRoots = func(*Env) ([]string, []string, bool) { return nil, nil, false }
	t.Cleanup(func() { tempRoots = prev })
	h.env[caller.EnvProject] = "acme"
	note := filepath.Join(g.Dir, "note.md")
	if err := os.WriteFile(note, []byte("# A note\n\nIts body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.ok(directorAcme, "memory", "new", "n", "--file", note)

	other, err := program.New(program.Options{Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{other.Dir, member.Dir} {
		theirs := filepath.Join(dir, "note.md")
		if err := os.WriteFile(theirs, []byte("# Theirs\n\nIts body.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.expect(1, "outside-roots", directorAcme, "memory", "new", "m", "--file", theirs)
	}
}

// TestDirectorPaneHint: a human-only refusal in a director's pane says
// where to run it, as in a coordinator's (T990).
func TestDirectorPaneHint(t *testing.T) {
	h, _ := programHarness(t)
	h.env[caller.EnvProject] = "acme"
	_, _, errs := h.run(directorAcme, "", "project", "new", "zz")
	if !strings.Contains(errs, "this ran in the director's session") {
		t.Errorf("no hint: %s", errs)
	}
}
