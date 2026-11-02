package program

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.Env, dir)
	clock := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { clock = clock.Add(time.Second); return clock }
	t.Cleanup(func() { now = time.Now })
	return dir
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// TestNewLayout: a new program is a folder under projects/_programs/
// with the layout of the design's §3.1, and no repo, threads or role
// file.
func TestNewLayout(t *testing.T) {
	root := setup(t)
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("[programs.acme]\nprojects = [\"acme-api\", \"mini/acme-ios\"]\n"), 0o600)
	p, err := New(Options{Slug: "acme", Goal: "ship SSO"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "projects", "_programs", "acme")
	if p.Dir != want || p.Kind != project.KindProgram || !p.IsProgram() {
		t.Fatalf("folder %+v", p.Folder)
	}
	for _, rel := range []string{"PROGRAM.md", "CONTEXT.md", "MEMORY.md", "JOURNAL.md", "memory", "inbox/done", "messages", "designs", "uploads"} {
		if _, err := os.Stat(p.Path(rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	for _, rel := range []string{"threads", "AGENTS.md", "CLAUDE.md", "PROJECT.md"} {
		if _, err := os.Stat(p.Path(rel)); err == nil {
			t.Errorf("%s made", rel)
		}
	}
	if !reflect.DeepEqual(p.Members, []string{"acme-api", "mini/acme-ios"}) {
		t.Fatalf("members %v", p.Members)
	}
	got, err := Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Goal != "ship SSO" || !strings.HasPrefix(got.Instructions, "# Instructions") || got.LeftOut {
		t.Fatalf("open %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "acme")); err == nil {
		t.Fatal("a project folder was made")
	}
	// project.List never sees a program.
	if l, err := project.List(); err != nil || len(l) != 0 {
		t.Fatalf("project.List: %v %v", l, err)
	}
	lines, _, _ := p.JournalTail(5)
	if len(lines) != 1 || !strings.Contains(lines[0], "program.new acme") {
		t.Fatalf("journal %q", lines)
	}
}

// TestNewRefuses: one namespace with projects (§3.4), and the usual
// slug and goal checks.
func TestNewRefuses(t *testing.T) {
	root := setup(t)
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ slug, goal, code string }{
		{"web", "", "slug-taken"},
		{"Acme", "", "invalid-program"},
		{"_programs", "", "invalid-program"},
		{"../web", "", "invalid-program"},
		{"acme", "two\nlines", "invalid-goal"},
	} {
		if _, err := New(Options{Slug: tc.slug, Goal: tc.goal}); code(err) != tc.code {
			t.Errorf("New(%q): %v, want %s", tc.slug, err, tc.code)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "_programs", "web")); err == nil {
		t.Fatal("program web made")
	}
	if _, err := New(Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Slug: "acme"}); code(err) != "program-exists" {
		t.Fatalf("again: %v", err)
	}
	// and the other way round, from B1: a project can't take it.
	if _, err := project.New(project.Options{Slug: "acme"}); code(err) != "slug-taken" {
		t.Fatalf("project acme: %v", err)
	}
	if _, err := Open("nope", nil); code(err) != "unknown-program" {
		t.Fatalf("open nope: %v", err)
	}
}

// TestFolderParts: the journal, inbox, questions, memory and designs
// helpers work on a program folder through project.Folder, and the
// umbrella board is the program's own.
func TestFolderParts(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := p.AddItem("program-needs", "T4", "two children ask", true)
	if err != nil {
		t.Fatal(err)
	}
	items, err := p.Inbox()
	if err != nil || len(items) != 1 || items[0].ID != it.ID {
		t.Fatalf("inbox %v %v", items, err)
	}
	if err := p.DoneItem(it.ID); err != nil {
		t.Fatal(err)
	}
	q, err := p.AddQuestion(project.Question{Question: "Session length?", Options: []project.QuestionOption{{Label: "1h"}, {Label: "8h"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := p.AnswerQuestion(caller.Caller{Kind: caller.Human}, q.ID, []string{"1"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := p.CreateNote("memory/sso.md", []byte("# SSO\n\nOkta for all.\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	idx, _ := os.ReadFile(p.Path("MEMORY.md"))
	if !strings.Contains(string(idx), "sso.md") || strings.Contains(string(idx), "[[acme/") {
		t.Fatalf("MEMORY.md:\n%s", idx)
	}
	title := "SSO"
	if _, _, err := p.SetDesign("sso", project.DesignEdit{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if ds, err := p.Designs(); err != nil || len(ds) != 1 {
		t.Fatalf("designs %v %v", ds, err)
	}
	st := p.Tasks()
	if _, err := st.Add(caller.Caller{Kind: caller.Human}, []tasks.NewTask{{Title: "SSO with Okta"}}); err != nil {
		t.Fatal(err)
	}
	b, err := st.Load()
	if err != nil || len(b.Tasks) != 1 {
		t.Fatalf("board %v %v", b, err)
	}
	lines, _, _ := p.JournalTail(20)
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "T1") }) {
		t.Fatalf("board writes aren't journaled: %q", lines)
	}
	if over, err := p.Oversized(); err != nil || len(over) != 0 {
		t.Fatalf("oversized %v %v", over, err)
	}
}

// TestOwned: tm's own files in a program folder, all inside it.
func TestOwned(t *testing.T) {
	setup(t)
	p, err := New(Options{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	dirs, files := p.Owned()
	for _, x := range append(dirs, files...) {
		if _, ok := strings.CutPrefix(x, p.Dir+string(filepath.Separator)); !ok {
			t.Errorf("%s is outside %s", x, p.Dir)
		}
	}
	for _, want := range []string{p.Path("JOURNAL.md"), p.Path("questions.json"), p.Path("inbox")} {
		if !slices.Contains(append(dirs, files...), want) {
			t.Errorf("%s not owned", want)
		}
	}
	if slices.Contains(files, p.Path("CONTEXT.md")) || slices.Contains(files, p.Path("PROGRAM.md")) {
		t.Error("the director's files are owned")
	}
}

// TestListAndClash: List has the tables and the folders; a program a
// project shares its slug with (a hand edit) is left out, the project
// winning.
func TestListAndClash(t *testing.T) {
	root := setup(t)
	if _, err := New(Options{Slug: "acme", Goal: "g"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("[programs.beta]\nprojects = [\"x\"]\n"), 0o600)
	os.MkdirAll(filepath.Join(root, "projects", "_programs", "web"), 0o755)
	if _, err := project.New(project.Options{Slug: "web"}); err == nil {
		t.Fatal("project web over a program folder")
	}
	// by hand: a project folder next to the program folder.
	os.MkdirAll(filepath.Join(root, "projects", "web"), 0o755)
	l, err := List(nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range l {
		got = append(got, s.Slug)
		switch s.Slug {
		case "acme":
			if !s.Folder || s.Goal != "g" || s.LeftOut {
				t.Errorf("acme %+v", s)
			}
		case "beta":
			if s.Folder || !reflect.DeepEqual(s.Members, []string{"x"}) {
				t.Errorf("beta %+v", s)
			}
		case "web":
			if !s.LeftOut {
				t.Errorf("web %+v", s)
			}
		}
	}
	if !reflect.DeepEqual(got, []string{"acme", "beta", "web"}) {
		t.Fatalf("list %v", got)
	}
}
