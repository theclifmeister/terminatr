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
