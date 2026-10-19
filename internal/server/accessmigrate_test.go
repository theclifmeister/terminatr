package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
)

// TestMigrateAccess: the first start moves config.toml, keeping the old
// file beside it, and each project's journal and inbox say what moved,
// what was kept for the user and the two meanings that change; the next
// start leaves it alone. One that can't move keeps the file and tells
// each project once.
func TestMigrateAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	web, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	old := `[defaults.sandbox]
excluded_commands = ["docker compose *"]
allow = ["Bash(git status)", "Edit(~/src/fixtures/**)"]
browser_testing = true

[projects.web.sandbox]
excluded_commands = []
`
	os.WriteFile(cfg, []byte(old), 0o600)
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, f) }
	migrateAccess(logf)
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), `drop = ["docker compose *"]`) || strings.Contains(string(data), "browser_testing") {
		t.Fatalf("not moved:\n%s", data)
	}
	bak, err := os.ReadFile(cfg + ".bak-" + release())
	if err != nil || string(bak) != old {
		t.Fatalf("backup: %q, %v", bak, err)
	}
	items, _ := web.Inbox()
	if len(items) != 1 || items[0].Kind != KindAccessMigration || !items[0].NeedsUser {
		t.Fatalf("inbox: %+v", items)
	}
	for _, want := range []string{"added drop docker compose *", "Bash(git status)", "browser_testing", "yolo now turns the sandbox off", "escape prompts go only to you",
		"Check write folder ~/src/fixtures (all projects)", "Codex threads can write there too", "tm access rm write ~/src/fixtures --all"} {
		if !strings.Contains(items[0].Summary, want) {
			t.Errorf("note lacks %q: %s", want, items[0].Summary)
		}
	}
	if lines, _, _ := web.JournalTail(5); len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "settings.migrate") {
		t.Errorf("journal: %q", lines)
	}
	migrateAccess(logf)
	if items, _ := web.Inbox(); len(items) != 1 {
		t.Errorf("a second start noted again: %+v", items)
	}

	// Not moved (here a table in a form the line editor doesn't
	// change): the file stays, one note however many starts.
	for _, it := range items {
		web.DoneItem(it.ID)
	}
	stuck := "[projects.web.sandbox]\nexcluded_commands = []\n\n[[projects.web.guard_allow]]\nrule = \"worktree-only\"\npath = \"~/src/x/*\"\n"
	os.WriteFile(cfg, []byte(stuck), 0o600)
	migrateAccess(logf)
	migrateAccess(logf)
	if data, _ := os.ReadFile(cfg); string(data) != stuck {
		t.Errorf("changed:\n%s", data)
	}
	items, _ = web.Inbox()
	if len(items) != 1 || !strings.Contains(items[0].Summary, "read as it was") || !items[0].NeedsUser {
		t.Errorf("inbox: %+v", items)
	}

}

// TestTellYolo: a project with yolo on is told once, needing the user,
// that yolo turns Claude's sandbox off too, whatever the migration did;
// the others aren't, and a later start says nothing.
func TestTellYolo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	y, _ := project.New(project.Options{Slug: "y"})
	n, _ := project.New(project.Options{Slug: "n"})
	os.WriteFile(filepath.Join(home, "config.toml"), []byte("sandbox_lists = \"add\"\n[projects.y]\nyolo = true\n"), 0o600)
	logf := func(string, ...any) {}
	migrateAccess(logf)
	tellYolo(logf)
	tellYolo(logf)
	items, _ := y.Inbox()
	if len(items) != 1 || !items[0].NeedsUser || !strings.Contains(items[0].Summary, "sandbox off for Claude threads") {
		t.Errorf("y: %+v", items)
	}
	if items, _ := n.Inbox(); len(items) != 0 {
		t.Errorf("n: %+v", items)
	}
	y.DoneItem(items[0].ID)
	tellYolo(logf)
	if items, _ := y.Inbox(); len(items) != 0 {
		t.Errorf("told again: %+v", items)
	}
}
