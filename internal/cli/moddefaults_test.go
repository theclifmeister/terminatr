package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestModDefaults: tm mod defaults shows what a mod suggests for all
// projects beside what they follow now; --apply writes those the human
// hasn't set to [defaults] after a y, once the mod is on, journaled in
// each project that follows all projects as the human's change. An
// agent is refused.
func TestModDefaults(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "mod", "install", modSource(t, "id = \"policy\"\nname = \"Policy\"\nversion = \"1.0.0\"\napi = \"1.1\"\n"+
		"[defaults]\nauto_close = \"days\"\nauto_close_days = 3\npr_followup = true\n"), "--yes")
	if err := config.SetDefaults("auto_close_days", 5); err != nil {
		t.Fatal(err)
	}
	h.expect(1, "human-only", coord, "mod", "defaults", "policy", "--apply", "--yes")
	out := h.ok(human, "mod", "defaults", "policy")
	for _, want := range []string{`auto_close       "merged" → "days"`, "auto_close_days  5     yours in [defaults], stays (it suggests 3)", "pr_followup      true  already", "It is off: tm mod enable policy"} {
		if !strings.Contains(out, want) {
			t.Errorf("defaults lacks %q:\n%s", want, out)
		}
	}
	h.ok(human, "mod", "enable", "policy", "--yes")
	if out := h.ok(human, "mod", "defaults", "policy"); !strings.Contains(out, "tm mod defaults policy --apply writes them") {
		t.Errorf("without --apply:\n%s", out)
	}
	h.expect(2, "confirm with --yes", human, "mod", "defaults", "policy", "--apply")
	if out := h.ok(human, "mod", "defaults", "policy", "--apply", "--yes"); !strings.Contains(out, `all projects: auto_close = "days"`) || strings.Contains(out, "auto_close_days =") {
		t.Errorf("--apply:\n%s", out)
	}
	cfg, _ := config.Load()
	if s, _ := cfg.AllProjects(); s.AutoClose != config.CloseDays || s.AutoCloseDays != 5 {
		t.Errorf("after --apply: %s %d", s.AutoClose, s.AutoCloseDays)
	}
	p, _ := project.Open("demo")
	if j, _ := os.ReadFile(p.Path("JOURNAL.md")); !strings.Contains(string(j), "settings.auto_close demo days (all projects, suggested by mod:policy)") {
		t.Errorf("journal:\n%s", j)
	}
	if out := h.ok(human, "mod", "defaults", "policy"); !strings.Contains(out, "Nothing to apply.") {
		t.Errorf("again:\n%s", out)
	}
}
