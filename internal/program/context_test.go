package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// TestNewTaken: a name the caller says is taken (a paired server's)
// is refused slug-taken, and no folder is made.
func TestNewTaken(t *testing.T) {
	setup(t)
	_, err := New(Options{Slug: "mini", Taken: func(s string) string {
		if s == "mini" {
			return "a paired server is called that"
		}
		return ""
	}})
	if code(err) != "slug-taken" || !strings.Contains(err.Error(), "paired server") {
		t.Fatalf("taken: %v", err)
	}
	if d, _ := Dir("mini"); fileExists(d) {
		t.Fatalf("a folder was made for a refused name")
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// TestContext: tm program context has the program's head, the shared
// parts, the umbrella board and one line per member, a local one with
// its goal and open tasks, a paired one by name; and the start-up block
// stays within its budget.
func TestContext(t *testing.T) {
	dir := setup(t)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[features]\nprograms = true\n[programs.studio]\nprojects = [\"api\", \"mini/ios\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api, err := project.New(project.Options{Slug: "api", Goal: "The API"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Tasks().Add(caller.Caller{Kind: caller.Human}, []tasks.NewTask{{Title: "Login"}}); err != nil {
		t.Fatal(err)
	}
	g, err := New(Options{Slug: "studio", Goal: "One product"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	secs, err := g.Context(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text := project.RenderContext(secs)
	for _, want := range []string{"## Program\nProgram: studio\n", "Goal: One product", "Members: api, mini/ios", "authority=propose",
		"## Standing instructions (PROGRAM.md)", "## Context (CONTEXT.md)", "## Memory index (MEMORY.md)", "## Umbrella tasks (TASKS.md)\n(no tasks)",
		"## Children\napi: The API · tasks: 1 ", "mini/ios: on the paired server mini", "## Inbox", "## Journal"} {
		if !strings.Contains(text, want) {
			t.Errorf("context lacks %q:\n%s", want, text)
		}
	}
	long := strings.Repeat("x", 80) + "\n"
	big := []project.Section{{Title: "Big", Lines: strings.Split(strings.Repeat(long, 400), "\n")}}
	if s := Startup("head", big); len(s) > StartupBudget || !strings.Contains(s, "`tm program context` prints the rest") {
		t.Errorf("start-up block: %d bytes", len(s))
	}
}
