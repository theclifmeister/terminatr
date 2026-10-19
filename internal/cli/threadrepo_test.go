package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// TestThreadRepo: which repo a new thread works in. --repo wins over the
// task's repo; with neither, the project's one repo, or a plain folder
// when it has none; with several, tm refuses to guess (pick-repo); none
// is a plain folder whatever the project has.
func TestThreadRepo(t *testing.T) {
	base := t.TempDir()
	dir := func(parts ...string) string {
		d := filepath.Join(append([]string{base}, parts...)...)
		os.MkdirAll(d, 0o755)
		return d
	}
	web, api, other := dir("web"), dir("api"), dir("other")
	app1, app2 := dir("a", "app"), dir("b", "app")
	task := func(repo string) *tasks.Task { return &tasks.Task{ID: 5, Title: "Fix it", Repo: repo} }
	cases := []struct {
		name    string
		repos   []string
		flag    string
		task    *tasks.Task
		c       caller.Caller
		want    string
		code    string
		hasText string
	}{
		{name: "no repos", want: ""},
		{name: "no repos, task without one", task: task(""), want: ""},
		{name: "one repo", repos: []string{web}, want: web},
		{name: "one repo, none", repos: []string{web}, task: task("none"), want: ""},
		{name: "several, no choice", repos: []string{web, api}, code: "pick-repo", hasText: "web (" + web + "), api (" + api + ")"},
		{name: "several, task without one", repos: []string{web, api}, task: task(""), code: "pick-repo", hasText: "tm task edit T5 --repo NAME"},
		{name: "several, task's by name", repos: []string{web, api}, task: task("api"), want: api},
		{name: "several, task's by path", repos: []string{web, api}, task: task(api), want: api},
		{name: "several, task's none", repos: []string{web, api}, task: task("none"), want: ""},
		{name: "--repo over the task's", repos: []string{web, api}, flag: "web", task: task("api"), want: web},
		{name: "--repo none over the task's", repos: []string{web, api}, flag: "none", task: task("api"), want: ""},
		{name: "--repo path", repos: []string{web, api}, flag: api, want: api},
		{name: "--repo unknown name", repos: []string{web, api}, flag: "shop", code: "unknown-repo"},
		{name: "--repo unlisted path, agent", repos: []string{web}, flag: other, code: "unlisted-repo"},
		{name: "--repo unlisted path, human", repos: []string{web}, flag: other, c: human, want: other},
		{name: "task's repo gone", repos: []string{web, api}, task: task("shop"), code: "unknown-repo", hasText: "T5's repo"},
		{name: "a shared name", repos: []string{app1, app2}, flag: "app", code: "ambiguous-repo"},
		{name: "a shared name, by path", repos: []string{app1, app2}, flag: app2, want: app2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.c
			if c.Kind == "" {
				c = coord
			}
			e := &Env{Caller: c, Cwd: base}
			got, err := e.threadRepo(&project.Project{Slug: "demo", Repos: tc.repos}, tc.flag, tc.task)
			var te *tasks.Error
			switch {
			case tc.code != "" && (!errors.As(err, &te) || te.Code != tc.code || !strings.Contains(te.Msg, tc.hasText)):
				t.Fatalf("err %v, want %s with %q", err, tc.code, tc.hasText)
			case tc.code == "" && (err != nil || got != tc.want):
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// TestTaskRepoField: a task's repo is set with tm task add and edit, a
// project repo by name or path or none, refused otherwise, shown by tm
// task show, list, --json and tm context; tm task delegate refuses to
// guess one in a project of several repos.
func TestTaskRepoField(t *testing.T) {
	h := newHarness(t)
	base := t.TempDir()
	web, api := filepath.Join(base, "web"), filepath.Join(base, "api")
	os.MkdirAll(web, 0o755)
	os.MkdirAll(api, 0o755)
	h.ok(human, "project", "new", "demo", "--repo", web, "--repo", api)
	h.env[caller.EnvProject] = "demo"

	h.ok(coord, "task", "add", "Fix the form", "--repo", "web")
	if out := h.ok(coord, "task", "show", "T1"); !strings.Contains(out, "repo: web") {
		t.Fatalf("show:\n%s", out)
	}
	if out := h.ok(coord, "task", "list"); !strings.Contains(out, "repo:web") {
		t.Fatalf("list:\n%s", out)
	}
	if out := h.ok(coord, "context"); !strings.Contains(out, "repo:web") {
		t.Fatalf("context lacks the task's repo:\n%s", out)
	}
	h.expect(1, "unknown-repo", coord, "task", "add", "Other", "--repo", "shop")
	h.expect(1, "unknown-repo", coord, "task", "edit", "T1", "--repo", "shop")

	// A path, relative too, is kept as the project lists the repo.
	h.cwd = base
	h.ok(coord, "task", "edit", "T1", "--repo", "./api")
	var js map[string]any
	json.Unmarshal([]byte(h.ok(coord, "task", "show", "T1", "--json")), &js)
	if js["repo"] != api {
		t.Fatalf("json repo %v, want %s", js["repo"], api)
	}
	h.ok(coord, "task", "edit", "T1", "--repo", "none")
	if out := h.ok(coord, "task", "show", "T1"); !strings.Contains(out, "repo: none") {
		t.Fatalf("show after none:\n%s", out)
	}
	h.ok(coord, "task", "edit", "T1", "--repo", "")
	if out := h.ok(coord, "task", "show", "T1"); strings.Contains(out, "repo:") {
		t.Fatalf("show after clearing:\n%s", out)
	}

	// A plan's items are checked one by one.
	code, out, _ := h.run(coord, `[{"title":"A","repo":"api"},{"title":"B","repo":"shop"}]`, "task", "add", "--json")
	var res []map[string]string
	json.Unmarshal([]byte(out), &res)
	if code != 1 || len(res) != 2 || res[0]["result"] != "created" || res[1]["code"] != "unknown-repo" {
		t.Fatalf("plan: exit %d %v", code, res)
	}

	// Several repos, none chosen: no thread, and the refusal lists them.
	h.expect(1, "pick-repo", coord, "task", "delegate", "T1", "--approved-by-user")
	h.expect(1, "web ("+web+"), api ("+api+")", coord, "task", "delegate", "T1", "--approved-by-user")
	h.expect(1, "pick-repo", coord, "thread", "start", "Loose work", "--approved-by-user")
	if out := h.ok(coord, "thread", "list"); strings.Contains(out, "t-0001") {
		t.Fatalf("a refused start left a thread:\n%s", out)
	}
}

// TestResolvePlainFolder: a thread without a repo has a plain folder,
// which resolve keeps while files are in it (nothing there was handed
// in), removes once empty, and --discard removes with its files; a
// folder that isn't the one tm made for it is never discarded.
func TestResolvePlainFolder(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	wt, _ := thread.WorktreeDir("demo", "t-0001", "Notes")
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, "notes.md"), []byte("x"), 0o644)
	rec, err := thread.Create(p, thread.Record{Title: "Notes", Agent: "claude", Worktree: wt, State: thread.Stopped})
	if err != nil {
		t.Fatal(err)
	}
	out := h.ok(coord, "thread", "resolve", rec.ID, "--project", "demo")
	if !strings.Contains(out, "kept folder "+wt+" (1 file not handed in") {
		t.Fatalf("resolve with a file left: %q", out)
	}
	out = h.ok(coord, "thread", "resolve", rec.ID, "--discard", "--project", "demo")
	if !strings.Contains(out, "removed folder "+wt+" (discarded)") {
		t.Fatalf("discard: %q", out)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("folder still there: %v", err)
	}

	// Empty: plain resolve removes it.
	wt2, _ := thread.WorktreeDir("demo", "t-0002", "Empty")
	os.MkdirAll(wt2, 0o755)
	rec2, _ := thread.Create(p, thread.Record{Title: "Empty", Agent: "claude", Worktree: wt2, State: thread.Stopped})
	if out := h.ok(coord, "thread", "resolve", rec2.ID, "--project", "demo"); !strings.Contains(out, "removed folder "+wt2) {
		t.Fatalf("resolve empty: %q", out)
	}

	// A record naming some other folder: kept, and never discarded.
	elsewhere := t.TempDir()
	os.WriteFile(filepath.Join(elsewhere, "keep.md"), []byte("x"), 0o644)
	rec3, _ := thread.Create(p, thread.Record{Title: "Odd", Agent: "claude", Worktree: elsewhere, State: thread.Stopped})
	h.expect(1, "discard-refused", coord, "thread", "resolve", rec3.ID, "--discard", "--project", "demo")
	if _, err := os.Stat(filepath.Join(elsewhere, "keep.md")); err != nil {
		t.Fatal("a refused discard removed a file")
	}
}
