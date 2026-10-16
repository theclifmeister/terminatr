package rename

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/home/hometest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/thread"
)

func TestMain(m *testing.M) { os.Exit(hometest.Run(m.Run)) }

var human = caller.Caller{Kind: caller.Human}

// setup gives the test a fresh TERMINATR_HOME with project demo and
// returns its root.
func setup(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.Env, root)
	if _, err := project.New(project.Options{Slug: "demo"}); err != nil {
		t.Fatal(err)
	}
	return root
}

func code(err error) string {
	var e *project.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestCheckRefuses(t *testing.T) {
	cases := []struct {
		name, from, to, config string
		mkdir                  []string // under the home dir
		want                   string
	}{
		{name: "unknown project", from: "nope", to: "demo2", want: "unknown-project"},
		{name: "invalid slug", from: "demo", to: "Demo 2", want: "invalid-project"},
		{name: "unchanged", from: "demo", to: "demo", want: "unchanged"},
		{name: "project exists", from: "demo", to: "other", mkdir: []string{"projects/other"}, want: "project-exists"},
		{name: "worktrees left over", from: "demo", to: "old", mkdir: []string{"worktrees/old"}, want: "project-exists"},
		{name: "settings for the new slug", from: "demo", to: "demo2", config: "[projects.demo2]\nyolo = true\n", want: "project-exists"},
		{name: "dotted keys", from: "demo", to: "demo2", config: "projects.demo.yolo = true\n", want: "settings-form"},
		{name: "inline table", from: "demo", to: "demo2", config: "projects = { demo = { yolo = true } }\n", want: "settings-form"},
		{name: "ok", from: "demo", to: "demo2", config: "[projects.demo]\nyolo = true\n"},
		{name: "ok without config", from: "demo", to: "demo2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := setup(t)
			for _, d := range c.mkdir {
				os.MkdirAll(filepath.Join(root, d), 0o755)
			}
			if c.config != "" {
				os.WriteFile(filepath.Join(root, "config.toml"), []byte(c.config), 0o600)
			}
			err := Check(Options{From: c.from, To: c.to, Caller: human})
			if c.want == "" {
				if err != nil {
					t.Fatalf("Check: %v", err)
				}
				return
			}
			if code(err) != c.want {
				t.Fatalf("Check: %v (code %q), want %s", err, code(err), c.want)
			}
			// A refused rename changes nothing.
			if _, err := Project(Options{From: c.from, To: c.to, Caller: human}); code(err) != c.want {
				t.Fatalf("Project: %v, want %s", err, c.want)
			}
			if c.from == "demo" {
				if _, err := os.Stat(filepath.Join(root, "projects", "demo")); err != nil {
					t.Fatalf("demo moved: %v", err)
				}
			}
		})
	}
}

func TestUnder(t *testing.T) {
	wt := filepath.Join(string(filepath.Separator)+"h", "worktrees", "demo")
	cases := []struct {
		path, rel string
		ok        bool
	}{
		{filepath.Join(wt, "t-0001-x"), "t-0001-x", true},
		{filepath.Join(wt, "t-0001-x", "sub"), filepath.Join("t-0001-x", "sub"), true},
		{filepath.Join(wt, "a", "..", "t-0002"), "t-0002", true},
		{wt, "", false},
		{wt + string(filepath.Separator), "", false},
		{wt + "2", "", false}, // project demo2's worktrees, not demo's
		{wt + "2" + string(filepath.Separator) + "t-0001", "", false},
		{filepath.Join(wt, "..", "other", "t-0001"), "", false},
		{filepath.Join(wt, ".."), "", false},
		{filepath.Dir(wt), "", false},
		{"", "", false},
	}
	for _, c := range cases {
		rel, ok := under(wt, c.path)
		if rel != c.rel || ok != c.ok {
			t.Errorf("under(%q, %q) = %q, %v; want %q, %v", wt, c.path, rel, ok, c.rel, c.ok)
		}
	}
}

// The project folder failing to move after the worktrees folder moved
// puts the worktrees folder back: an error means nothing changed.
func TestProjectRollsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only folder doesn't stop a rename on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root renames in a read-only folder")
	}
	root := setup(t)
	wt := filepath.Join(root, "worktrees", "demo", "t-0001-x")
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, "f"), []byte("x"), 0o644)
	projects := filepath.Join(root, "projects")
	if err := os.Chmod(projects, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(projects, 0o755) })

	if _, err := Project(Options{From: "demo", To: "demo2", Caller: human}); err == nil {
		t.Fatal("rename went through in a read-only projects folder")
	}
	os.Chmod(projects, 0o755)
	if b, err := os.ReadFile(filepath.Join(wt, "f")); err != nil || string(b) != "x" {
		t.Fatalf("worktree not back: %v %q", err, b)
	}
	for _, gone := range []string{"worktrees/demo2", "projects/demo2"} {
		if _, err := os.Lstat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s: %v", gone, err)
		}
	}
	if _, err := project.Open("demo"); err != nil {
		t.Fatalf("demo: %v", err)
	}
}

// A rename with no worktrees folder moves only the project folder.
func TestProjectWithoutWorktrees(t *testing.T) {
	root := setup(t)
	res, err := Project(Options{From: "demo", To: "demo2", Caller: human})
	if err != nil {
		t.Fatal(err)
	}
	if res.Dir != filepath.Join(root, "projects", "demo2") || res.Worktrees != "" || res.Threads != 0 || len(res.Notes) != 0 {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Lstat(filepath.Join(root, "worktrees", "demo2")); !os.IsNotExist(err) {
		t.Fatalf("worktrees/demo2: %v", err)
	}
}

// Threads: one in <worktrees>/demo follows the move, an adopted one
// elsewhere stays, and a worktree git can't repair and agent state that
// can't move become notes, not errors.
func TestProjectThreadsAndNotes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := setup(t)
	p, err := project.Open("demo")
	if err != nil {
		t.Fatal(err)
	}
	// A worktree whose .git file points nowhere: git worktree repair fails.
	wt := filepath.Join(root, "worktrees", "demo", "t-0001-x")
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(root, "nowhere")+"\n"), 0o644)
	own, err := thread.Create(p, thread.Record{Title: "x", Agent: "claude", Worktree: wt, State: thread.Stopped})
	if err != nil {
		t.Fatal(err)
	}
	adopted := t.TempDir()
	other, err := thread.Create(p, thread.Record{Title: "y", Agent: "claude", Worktree: adopted, State: thread.Stopped})
	if err != nil {
		t.Fatal(err)
	}
	// demo2's would-be worktrees share demo's prefix but aren't demo's.
	sibling := filepath.Join(root, "worktrees", "demo2x", "t-0003")
	third, err := thread.Create(p, thread.Record{Title: "z", Agent: "claude", Worktree: sibling, State: thread.Stopped})
	if err != nil {
		t.Fatal(err)
	}

	var moves [][2]string
	res, err := Project(Options{From: "demo", To: "demo2", Caller: human, MoveAgentDir: func(from, to string) error {
		moves = append(moves, [2]string{from, to})
		if strings.HasSuffix(to, "t-0001-x") {
			return errors.New("busy")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	newWT := filepath.Join(root, "worktrees", "demo2")
	moved := filepath.Join(newWT, "t-0001-x")
	if res.Worktrees != newWT || res.Threads != 1 {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Stat(filepath.Join(moved, ".git")); err != nil {
		t.Fatalf("worktree not moved: %v", err)
	}

	p, err = project.Open("demo2")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{own.ID: moved, other.ID: adopted, third.ID: sibling} {
		r, err := thread.Load(p, id)
		if err != nil || r.Worktree != want {
			t.Fatalf("%s: %v worktree %q, want %q", id, err, r.Worktree, want)
		}
	}

	oldDir, newDir := filepath.Join(root, "projects", "demo"), filepath.Join(root, "projects", "demo2")
	if len(moves) != 2 || moves[0] != [2]string{oldDir, newDir} || moves[1] != [2]string{wt, moved} {
		t.Fatalf("MoveAgentDir calls %v", moves)
	}
	notes := strings.Join(res.Notes, "\n")
	if len(res.Notes) != 2 ||
		!strings.Contains(notes, moved+": git doesn't know its new place") ||
		!strings.Contains(notes, moved+": agent state not carried over: busy") {
		t.Fatalf("notes:\n%s", notes)
	}
	j, _ := os.ReadFile(p.Path("JOURNAL.md"))
	if !strings.Contains(string(j), "project.rename demo2 demo → demo2") {
		t.Fatalf("journal:\n%s", j)
	}
}

// Linked projects' messages with the renamed one move to its new slug.
func TestProjectMovesMessages(t *testing.T) {
	setup(t)
	o, err := project.New(project.Options{Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(o.Path("messages", "demo"), 0o755)
	os.WriteFile(o.Path("messages", "demo", "m.md"), []byte("hi"), 0o644)
	res, err := Project(Options{From: "demo", To: "demo2", Caller: human})
	if err != nil || len(res.Notes) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	if b, err := os.ReadFile(o.Path("messages", "demo2", "m.md")); err != nil || string(b) != "hi" {
		t.Fatalf("messages: %v %q", err, b)
	}
	if _, err := os.Lstat(o.Path("messages", "demo")); !os.IsNotExist(err) {
		t.Fatalf("messages/demo: %v", err)
	}
}
