package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// A thread reads only its own project's tm data; coordinators and the
// human read any (docs/SPEC.md §11.1, T336). The refusal is one answer,
// the same for a project that exists and one that doesn't.
func TestThreadSeesOwnProjectOnly(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "other")
	h.ok(human, "task", "add", "Theirs", "--project", "other")
	h.env[caller.EnvProject] = "demo"

	reads := [][]string{
		{"task", "list"},
		{"task", "show", "T1"},
		{"thread", "list"},
		{"thread", "show", "t-0001"},
		{"context"},
		{"report", "--show"},
		{"status", "--percent", "5", "--activity", "x"},
		{"done"},
		{"skill", "thread"},
	}
	for _, args := range reads {
		var answers []string
		for _, slug := range []string{"other", "ghost"} {
			code, out, errs := h.run(thr, "", append(args, "--project", slug)...)
			if code != ExitRefused || out != "" || !strings.Contains(errs, "other-project: a thread sees only its own project") {
				t.Fatalf("thread: tm %v --project %s: exit %d, stdout %q, stderr %q", args, slug, code, out, errs)
			}
			answers = append(answers, errs)
		}
		if answers[0] != answers[1] {
			t.Errorf("tm %v: other answers %q, a missing project %q", args, answers[0], answers[1])
		}
	}

	// Its own project still answers, named or not.
	h.ok(thr, "task", "list")
	h.ok(thr, "task", "list", "--project", "demo")
	h.ok(thr, "skill", "thread", "--project", "demo")

	// A cwd in another project's folder doesn't name it either.
	delete(h.env, caller.EnvProject)
	h.cwd = filepath.Join(h.root, "projects", "other")
	h.expect(1, "other-project", thr, "task", "list")
	h.env[caller.EnvProject] = "demo"

	// A thread with no project names none.
	lost := caller.Caller{Kind: caller.Thread, Thread: "t-0001"}
	h.expect(1, "other-project", lost, "task", "list", "--project", "demo")

	// tm project list shows a thread its own project only.
	var list []map[string]any
	json.Unmarshal([]byte(h.ok(thr, "project", "list", "--json")), &list)
	if len(list) != 1 || list[0]["slug"] != "demo" {
		t.Fatalf("thread's project list %v", list)
	}

	// Coordinators and the human still read another project.
	for _, c := range []caller.Caller{coord, human} {
		if out := h.ok(c, "task", "list", "--project", "other"); !strings.Contains(out, "Theirs") {
			t.Errorf("%s: task list --project other %q", c.Kind, out)
		}
		h.ok(c, "thread", "list", "--project", "other")
		h.ok(c, "context", "--project", "other")
		json.Unmarshal([]byte(h.ok(c, "project", "list", "--json")), &list)
		if len(list) != 2 {
			t.Errorf("%s: project list %v", c.Kind, list)
		}
	}
}

// The forwarded path (cli.run) answers a thread the same way.
func TestThreadOwnProjectInServer(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "other")
	for _, slug := range []string{"other", "ghost"} {
		res := RunInServer(proto.CLIRunParams{Args: []string{"task", "list", "--project", slug}}, thr)
		if res.Code != ExitRefused || res.Stderr != "tm task: other-project: a thread sees only its own project\n" {
			t.Fatalf("--project %s: %+v", slug, res)
		}
	}
	if res := RunInServer(proto.CLIRunParams{Args: []string{"task", "list"}}, thr); res.Code != ExitOK {
		t.Fatalf("own project: %+v", res)
	}
	if res := RunInServer(proto.CLIRunParams{Args: []string{"task", "list", "--project", "other"}}, coord); res.Code != ExitOK {
		t.Fatalf("coordinator: %+v", res)
	}
}

// A thread that unsets its session's variables runs tm here, as the
// human by the environment; the server's view of it still narrows every
// project command, the read ones too.
func TestThreadOwnProjectWithoutEnv(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "project", "new", "other")
	for _, args := range [][]string{{"task", "list"}, {"thread", "list"}, {"context"}, {"report", "--show", "--thread", "t-0001"}, {"skill", "thread"}} {
		var out, errb bytes.Buffer
		e := &Env{
			Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(string) string { return "" }, Cwd: h.cwd, Caller: human,
			Identify: func() (caller.Caller, bool) { return thr, true },
		}
		if code, _ := e.Run(append(args, "--project", "other")); code != ExitRefused || !strings.Contains(errb.String(), "other-project") {
			t.Errorf("tm %v --project other: exit %d, stderr %q", args, code, errb.String())
		}
	}
}
