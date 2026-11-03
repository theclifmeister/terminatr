package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
)

// TestProgramRename: tm program rename is the human's, refuses a taken
// name, and with no server running renames the folder and the table;
// off, programs-off.
func TestProgramRename(t *testing.T) {
	h, _ := programHarness(t)
	h.expect(1, "human-only", directorAcme, "program", "rename", "acme", "studio")
	h.expect(1, "slug-taken", human, "program", "rename", "acme", "web")
	out := h.ok(human, "program", "rename", "acme", "studio")
	if !strings.Contains(out, "renamed program acme to studio") {
		t.Errorf("out:\n%s", out)
	}
	if _, err := program.Open("studio", nil); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if cfg.ProgramOf("web") != "studio" {
		t.Errorf("web in %q", cfg.ProgramOf("web"))
	}
	h.expect(1, "unknown-program", human, "program", "rename", "acme", "other")

	writeTestConfig(t, h, "[programs.studio]\nprojects = [\"demo\", \"web\"]\n")
	h.expect(1, "programs-off", human, "program", "rename", "studio", "acme")
	if _, err := os.Stat(h.root + "/projects/_programs/studio"); err != nil {
		t.Errorf("an off rename moved it: %v", err)
	}
}

// TestArchiveDeleteMember (the programs design §3.4): archiving a
// program's member asks first (--yes off a terminal) and takes it out
// of the program, as deleting one does; a project in no program is
// archived without asking.
func TestArchiveDeleteMember(t *testing.T) {
	h, _ := programHarness(t)
	h.expect(2, "takes it out of program acme: confirm with --yes", human, "project", "archive", "demo")
	if cfg, _ := config.Load(); cfg.ProgramOf("demo") != "acme" {
		t.Fatal("an unconfirmed archive took demo out")
	}
	if out := h.ok(human, "project", "archive", "demo", "--yes"); !strings.Contains(out, "it left program acme") {
		t.Errorf("archive:\n%s", out)
	}
	if out := h.ok(human, "project", "delete", "web", "--yes"); !strings.Contains(out, "it left program acme") {
		t.Errorf("delete:\n%s", out)
	}
	cfg, _ := config.Load()
	if g, _ := cfg.Program("acme"); len(g.Projects) != 0 {
		t.Errorf("acme still lists %v", g.Projects)
	}
	if out := h.ok(human, "project", "archive", "loner"); strings.Contains(out, "program") {
		t.Errorf("loner:\n%s", out)
	}
}
