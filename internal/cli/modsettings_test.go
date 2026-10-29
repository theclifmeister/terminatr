package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
)

const gatesMod = `id = "dd"
name = "Double diamond"
version = "1.0.0"
api = "1.11"
[provides]
rules = { thread = "rules.md" }
[settings.gates]
type = "bool"
default = false
label = "Stop at each gate"
[settings.pace]
type = "choice"
options = ["calm", "brisk"]
default = "calm"
[settings.max]
type = "int"
default = 3
min = 1
`

// TestModSettings: tm mod settings shows a mod's settings with where
// each value comes from, sets them for all projects or one, checks
// every value before writing any, and --reset follows all projects
// again. An agent reads its own project's, and can't change them.
func TestModSettings(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	src := modSource(t, gatesMod)
	if err := os.WriteFile(filepath.Join(src, "rules.md"), []byte("Diverge.\n<!-- tm:if gates -->\nStop.\n<!-- tm:end -->\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.ok(human, "mod", "install", src, "--yes")
	h.ok(human, "mod", "enable", "dd", "--yes")

	out := h.ok(human, "mod", "settings", "dd")
	for _, want := range []string{"dd settings for all projects", "gates  false   default  (bool) · Stop at each gate", `pace   "calm"  default  (calm|brisk)`} {
		if !strings.Contains(out, want) {
			t.Errorf("settings lacks %q:\n%s", want, out)
		}
	}
	h.ok(human, "mod", "settings", "dd", "pace=brisk")
	out = h.ok(human, "mod", "settings", "dd", "--project", "demo", "gates=true", "max=5")
	for _, want := range []string{"dd settings for demo", "gates  true", "project", `"brisk"  all projects`, "max    5"} {
		if !strings.Contains(out, want) {
			t.Errorf("demo lacks %q:\n%s", want, out)
		}
	}
	cfg, _ := config.Load()
	if m := cfg.Mods["dd"]; m.Settings["pace"] != "brisk" || m.ProjectSettings["demo"]["gates"] != true || m.ProjectSettings["demo"]["max"] != int64(5) {
		t.Fatalf("config: %+v", m)
	}
	if out := h.ok(human, "skill", "thread", "--project", "demo"); !strings.Contains(out, "Diverge.\nStop.") {
		t.Errorf("tm skill thread for demo lacks the gate:\n%s", out)
	}

	// A bad value refuses the whole call: nothing is written.
	h.expect(1, "max must be at least 1", human, "mod", "settings", "dd", "--project", "demo", "gates=false", "max=0")
	h.expect(1, `"yes" isn't true or false`, human, "mod", "settings", "dd", "gates=yes")
	h.expect(1, `has no setting "nope"`, human, "mod", "settings", "dd", "nope=1")
	h.expect(2, "isn't <key>=<value>", human, "mod", "settings", "dd", "gates")
	if cfg, _ := config.Load(); cfg.Mods["dd"].ProjectSettings["demo"]["gates"] != true {
		t.Fatal("a refused call wrote a value")
	}

	// An agent reads its own project's settings, and changes none.
	var got modSettingsOut
	if err := json.Unmarshal([]byte(h.ok(coord, "mod", "settings", "dd", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Project != "demo" || len(got.Settings) != 3 || got.Settings[0].Key != "gates" || got.Settings[0].Value != true || got.Settings[0].From != "project" {
		t.Errorf("an agent's read: %+v", got)
	}
	h.expect(1, "human-only", coord, "mod", "settings", "dd", "gates=false")
	h.expect(1, "human-only", coord, "mod", "settings", "dd", "--reset", "gates")

	h.ok(human, "mod", "settings", "dd", "--project", "demo", "--reset", "gates")
	if out := h.ok(human, "mod", "settings", "dd", "--project", "demo"); !strings.Contains(out, "gates  false    default") {
		t.Errorf("after --reset:\n%s", out)
	}
	h.expect(1, "", human, "mod", "settings", "dd", "--project", "nowhere")
}
