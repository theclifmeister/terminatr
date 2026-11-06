package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const historyTasks = `# Tasks

## Now

### T4 Open work
status: ready · created: 2026-10-04 · updated: 2026-10-06

## Done

### T1 First
status: done · created: 2026-10-04 · updated: 2026-10-05

done_at: 2026-10-05T09:00:00Z

### T2 Second
status: done · created: 2026-10-05 · updated: 2026-10-06

done_at: 2026-10-06T09:00:00Z

### T3 Third
status: done · created: 2026-10-05 · updated: 2026-10-06

done_at: 2026-10-06T12:00:00Z
`

const historyJournal = `# Journal

2026-10-04T10:00:00Z human project.new web
2026-10-05T08:00:00Z coordinator task.status T1 started
2026-10-05T09:00:00Z coordinator task.status T1 done
2026-10-06T08:00:00Z coordinator task.status T2 started
2026-10-06T09:00:00Z ticker task.done T2 merged (PR #5)
`

// TestHistorySetups: a plain folder, a repo with no tags, several repos
// with tags and a repo on another host with no pull requests answer the
// same numbers, byte for byte, with git absent from PATH and with a git
// there that says when it is called.
func TestHistorySetups(t *testing.T) {
	h := newHarness(t)
	human := caller.Caller{Kind: caller.Human}

	noTags, _ := gittest.Repo(t)
	tagged, inTagged := gittest.Repo(t)
	inTagged("commit", "--allow-empty", "-m", "one")
	inTagged("tag", "v1.0.0")
	second, inSecond := gittest.Repo(t)
	inSecond("commit", "--allow-empty", "-m", "one")
	inSecond("tag", "v1.0.0")
	host, inHost := gittest.Repo(t)
	inHost("remote", "add", "origin", "https://git.example.com/o/r.git")

	setups := map[string][]string{"plain-folder": nil, "no-tags": {noTags}, "several-repos": {tagged, second}, "another-host": {host}}
	for slug, repos := range setups {
		p, err := project.New(project.Options{Slug: slug, Repos: repos})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Tasks().TasksPath(), []byte(historyTasks), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.Path("JOURNAL.md"), []byte(historyJournal), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) (asJSON, asText string) {
		t.Helper()
		var out [2]string
		for i, extra := range [][]string{{"--json"}, nil} {
			code, stdout, stderr := h.run(human, "", append(append([]string{"history"}, args...), extra...)...)
			if code != 0 {
				t.Fatalf("tm history %v: %d %s", args, code, stderr)
			}
			out[i] = stdout
		}
		return out[0], out[1]
	}
	wantJSON, wantText := run("--project", "plain-folder")
	var got proto.History
	if err := json.Unmarshal([]byte(wantJSON), &got); err != nil {
		t.Fatal(err)
	}
	if got.Done != 3 || got.Open != 1 || got.FirstRecord == "" || len(got.Days) != 2 || got.BestDay == nil || got.BestDay.Done != 2 || got.Timed != 2 {
		t.Errorf("numbers: %s", wantJSON)
	}
	if got.Typical != "" {
		t.Errorf("a typical time from 2 tasks: %s", wantJSON)
	}

	emptyPath := t.TempDir()
	spy := t.TempDir()
	called := filepath.Join(t.TempDir(), "git-called")
	if runtime.GOOS != "windows" {
		script := "#!/bin/sh\necho \"$@\" >> " + called + "\nexit 1\n"
		if err := os.WriteFile(filepath.Join(spy, "git"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{emptyPath, spy} {
		t.Setenv("PATH", path)
		for slug := range setups {
			j, text := run("--project", slug)
			if j != wantJSON {
				t.Errorf("%s, PATH %s: JSON\n%s\nwant\n%s", slug, path, j, wantJSON)
			}
			if text != wantText {
				t.Errorf("%s, PATH %s: text\n%s\nwant\n%s", slug, path, text, wantText)
			}
		}
	}
	if data, err := os.ReadFile(called); err == nil {
		t.Errorf("tm history ran git: %s", data)
	}
}

// TestHistoryThreadOwnProject: a thread reads its own project's numbers
// and not another's.
func TestHistoryThreadOwnProject(t *testing.T) {
	h := newHarness(t)
	for _, slug := range []string{"web", "other"} {
		if _, err := project.New(project.Options{Slug: slug}); err != nil {
			t.Fatal(err)
		}
	}
	th := caller.Caller{Kind: caller.Thread, Project: "web", Thread: "t-0001"}
	if code, _, stderr := h.run(th, "", "history", "--project", "web"); code != 0 {
		t.Errorf("own project: %d %s", code, stderr)
	}
	h.env[caller.EnvProject] = "web"
	if code, _, stderr := h.run(th, "", "history"); code != 0 {
		t.Errorf("bare, in its project: %d %s", code, stderr)
	}
	if code, _, _ := h.run(th, "", "history", "--project", "other"); code == 0 {
		t.Error("a thread read another project's numbers")
	}
}

// TestHistoryInServer: forwarded, a thread's tm history runs in the server
// and answers as the same read does.
func TestHistoryInServer(t *testing.T) {
	h := newHarness(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	_, want, _ := h.run(caller.Caller{Kind: caller.Human}, "", "history", "--project", "web", "--json")
	th := caller.Caller{Kind: caller.Thread, Project: "web", Thread: "t-0001"}
	res := RunInServer(proto.CLIRunParams{Args: []string{"history", "--project", "web", "--json"}, Cwd: h.cwd}, th)
	if res.Code != 0 || res.Stdout != want {
		t.Errorf("in the server: %d %q %q, want %q", res.Code, res.Stdout, res.Stderr, want)
	}
	res = RunInServer(proto.CLIRunParams{Args: []string{"history", "--project", "other"}, Cwd: h.cwd}, th)
	if res.Code == 0 {
		t.Errorf("another project's numbers: %q", res.Stdout)
	}
}
