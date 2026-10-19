package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
)

// TestMigrateGuidesNote: a file already on the access rules whose guides
// name tm 0.38's texts gets them written out at the start, a backup
// beside it, and each project's note says so, without the two meanings
// of the access migration; the next start leaves it alone.
func TestMigrateGuidesNote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TERMINATR_HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	web, err := project.New(project.Options{Slug: "web"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	old := "sandbox_lists = \"add\"\n\n[defaults.sandbox]\nguides = [\"browser-testing\"]\n"
	os.WriteFile(cfg, []byte(old), 0o600)
	logf := func(string, ...any) {}
	migrateAccess(logf)
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), `title = "Sandbox: browser testing"`) || strings.Contains(string(data), `"browser-testing"`) {
		t.Fatalf("not written out:\n%s", data)
	}
	if bak, err := os.ReadFile(cfg + ".bak-" + release()); err != nil || string(bak) != old {
		t.Fatalf("backup: %q, %v", bak, err)
	}
	items, _ := web.Inbox()
	if len(items) != 1 || items[0].Kind != KindAccessMigration || !strings.Contains(items[0].Summary, "wrote out tm 0.38's guidance names") ||
		!strings.Contains(items[0].Summary, "browser-testing") || strings.Contains(items[0].Summary, "yolo now turns") {
		t.Fatalf("inbox: %+v", items)
	}
	migrateAccess(logf)
	if items, _ := web.Inbox(); len(items) != 1 {
		t.Errorf("a second start noted again: %+v", items)
	}
}
