package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

const reportForRights = "## Report\nDone.\n\n## Next\n- Merge it\n"

// threadReportCommands are what a thread writes about itself; report is
// stored on stdin, so RunInServer's callers pass it as Stdin.
var threadReportCommands = [][]string{
	{"report"},
	{"status", "--percent", "5", "--activity", "x"},
	{"done"},
}

// unwritten fails unless thread id still has what reported() gave it:
// one report, no status of the caller's, not done.
func unwritten(t *testing.T, slug, id string) {
	t.Helper()
	p, err := project.Open(slug)
	if err != nil {
		t.Fatal(err)
	}
	r, err := thread.Load(p, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reports != 1 || r.Done {
		t.Errorf("%s/%s changed: reports %d, done %v", slug, id, r.Reports, r.Done)
	}
	st, err := thread.ReadStatus(p, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.SelfPercent == 5 || st.Activity == "x" || st.Done {
		t.Errorf("%s/%s status changed: %+v", slug, id, st)
	}
}

// reported is a thread with one report handed in, which tm done would
// accept.
func reported(t *testing.T, h *harness, slug string) string {
	t.Helper()
	p, err := project.Open(slug)
	if err != nil {
		t.Fatal(err)
	}
	r, err := thread.Create(p, thread.Record{Title: "Fix it"})
	if err != nil {
		t.Fatal(err)
	}
	if code, _, errs := h.run(human, reportForRights, "report", "--project", slug, "--thread", r.ID); code != ExitOK {
		t.Fatalf("human's report: %d %s", code, errs)
	}
	return r.ID
}

// TestThreadReportRights: tm report, status and done, as each caller on a
// member of the director's program and on a project outside it.
func TestThreadReportRights(t *testing.T) {
	h, _ := programHarness(t)
	member, outside := reported(t, h, "demo"), reported(t, h, "loner")
	otherThread := caller.Caller{Kind: caller.Thread, Project: "web", Thread: "t-0001"}
	loneCoord := caller.Caller{Kind: caller.Coordinator, Project: "loner"}

	for _, cmd := range threadReportCommands {
		for _, tc := range []struct {
			name string
			c    caller.Caller
			args []string
			code string
		}{
			{"director, member", directorAcme, []string{"--project", "demo", "--thread", member}, "thread-only"},
			{"director, outside its program", directorAcme, []string{"--project", "loner", "--thread", outside}, "thread-only"},
			{"director, a project that doesn't exist", directorAcme, []string{"--project", "ghost", "--thread", "t-0001"}, "thread-only"},
			{"director, its own program", directorAcme, []string{"--project", "acme", "--thread", "t-0001"}, "thread-only"},
			{"director, no flags", directorAcme, nil, "thread-only"},
			{"a paired server's director", caller.ForPeerDirector("mini", "acme"), []string{"--project", "demo", "--thread", member}, "thread-only"},
			{"coordinator, own project", coord, []string{"--project", "demo", "--thread", member}, "thread-only"},
			{"coordinator, another project", loneCoord, []string{"--project", "demo", "--thread", member}, "thread-only"},
			{"thread, another project", otherThread, []string{"--project", "demo"}, "other-project"},
			{"thread, another thread", thr, []string{"--project", "demo", "--thread", member + "9"}, "coordinator-only"},
		} {
			args := append(append([]string{}, cmd...), tc.args...)
			code, out, errs := h.run(tc.c, reportForRights, args...)
			if code != ExitRefused || out != "" || !strings.Contains(errs, tc.code+": ") {
				t.Errorf("%s: tm %v: exit %d, stdout %q, stderr %q; want %s", tc.name, args, code, out, errs, tc.code)
			}
		}
	}
	unwritten(t, "demo", member)
	unwritten(t, "loner", outside)

	// tm report --show is the director's refusal too; the coordinator and
	// the human still read a report that way.
	h.expect(ExitRefused, "thread-only", directorAcme, "report", "--show", "--project", "loner", "--thread", outside)
	for _, c := range []caller.Caller{coord, human} {
		if out := h.ok(c, "report", "--show", "--project", "demo", "--thread", member); !strings.Contains(out, "Done.") {
			t.Errorf("%s: report --show: %q", c.Kind, out)
		}
	}

	// The thread itself and the human, by hand, still write.
	own := caller.Caller{Kind: caller.Thread, Project: "demo", Thread: member}
	for _, c := range []caller.Caller{own, human} {
		extra := []string{"--project", "demo"}
		if c.Kind == caller.Human {
			extra = append(extra, "--thread", member)
		}
		for _, cmd := range threadReportCommands {
			args := append(append([]string{}, cmd...), extra...)
			if code, _, errs := h.run(c, reportForRights, args...); code != ExitOK {
				t.Errorf("%s: tm %v: exit %d, %s", c.Kind, args, code, errs)
			}
		}
	}
}

// The forwarded path (cli.run), where the server names the caller, is
// refused the same way, and leaves the thread as it was. A director that
// unset its session's variables is told apart by the server's view of it.
func TestThreadReportDirectorInServer(t *testing.T) {
	h, _ := programHarness(t)
	id := reported(t, h, "loner")
	flags := []string{"--project", "loner", "--thread", id}
	for _, cmd := range threadReportCommands {
		args := append(append([]string{}, cmd...), flags...)
		res := RunInServer(proto.CLIRunParams{Args: args, Stdin: []byte(reportForRights)}, directorAcme)
		if res.Code != ExitRefused || res.Stdout != "" || !strings.Contains(res.Stderr, "thread-only: ") {
			t.Errorf("tm %v in the server: %+v", args, res)
		}

		var out, errb bytes.Buffer
		e := &Env{
			Stdin: strings.NewReader(reportForRights), Stdout: &out, Stderr: &errb,
			Getenv: func(string) string { return "" }, Cwd: h.cwd, Caller: human,
			Identify: func() (caller.Caller, bool) { return directorAcme, true },
		}
		if code, _ := e.Run(args); code != ExitRefused || !strings.Contains(errb.String(), "thread-only: ") {
			t.Errorf("tm %v without its variables: exit %d, stderr %q", args, code, errb.String())
		}
	}
	unwritten(t, "loner", id)
	p, err := project.Open("loner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(thread.Path(p, id, "reports", "1.md")); !os.IsNotExist(err) {
		t.Errorf("the report was moved aside: %v", err)
	}
}

// With programs off a director is dormant, and still no thread.
func TestThreadReportDirectorProgramsOff(t *testing.T) {
	h, _ := programHarness(t)
	id := reported(t, h, "demo")
	writeTestConfig(t, h, "[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	for _, cmd := range threadReportCommands {
		args := append(append([]string{}, cmd...), "--project", "demo", "--thread", id)
		code, _, errs := h.run(directorAcme, reportForRights, args...)
		if code != ExitRefused || !strings.Contains(errs, "thread-only: ") {
			t.Errorf("tm %v: exit %d, %s", args, code, errs)
		}
	}
	unwritten(t, "demo", id)
}
