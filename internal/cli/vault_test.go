package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// TestVaultMigrateCheck: tm vault migrate --check lists the files the
// move to YAML would rewrite (a new project's too: tm writes TOML until
// the move) and those it can't, and changes nothing.
func TestVaultMigrateCheck(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	os.MkdirAll(p.Path("memory"), 0o755)
	note := "+++\ntitle = \"Retries\"\n+++\n## Decided\n"
	os.WriteFile(p.Path("memory", "retries.md"), []byte(note), 0o644)
	os.WriteFile(p.Path("memory", "broken.md"), []byte("+++\ntitle = \n+++\n"), 0o644)

	out := h.ok(human, "vault", "migrate", "--check")
	for _, want := range []string{"rewrites 3 files' front matter", "  demo/PROJECT.md\n", "  demo/TASKS.md\n", "  demo/memory/retries.md\n", "  demo/memory/broken.md: stays TOML: "} {
		if !strings.Contains(out, want) {
			t.Errorf("check lacks %q:\n%s", want, out)
		}
	}
	if data, _ := os.ReadFile(p.Path("memory", "retries.md")); string(data) != note || vault.Migrated() {
		t.Fatal("check changed something")
	}
	var res struct {
		Migrated bool        `json:"migrated"`
		Files    []vaultFile `json:"files"`
	}
	if err := json.Unmarshal([]byte(h.ok(coord, "vault", "migrate", "--check", "--json")), &res); err != nil || res.Migrated || len(res.Files) != 4 || res.Files[2].Path != "memory/broken.md" || res.Files[2].Error == "" {
		t.Fatalf("json: %v %+v", err, res)
	}
	vault.Migrate(time.Now())
	if out := h.ok(human, "vault", "migrate", "--check"); !strings.Contains(out, "1 live files still have TOML") {
		t.Errorf("after the move:\n%s", out)
	}
	h.expect(2, "", human, "vault", "migrate")
	h.expect(2, "", human, "vault", "nope")
}

// TestVaultInitRenderOff (T720): init and off are the human's and ask
// first (--yes without a terminal); render is the human's and a
// coordinator's for its own project, and refuses while the vault is
// off; doctor reports its checks beside the links.
func TestVaultInitRenderOff(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.expect(2, "confirm with --yes", human, "vault", "init")
	h.expect(1, "human-only", coord, "vault", "init", "--yes")
	h.expect(1, "vault-off", human, "vault", "render")
	out := h.ok(human, "vault", "init", "--yes")
	for _, want := range []string{".obsidian/app.json: new", "_tm/bases/Tasks.base: new", "derived notes render once front matter is YAML", "as a vault in Obsidian"} {
		if !strings.Contains(out, want) {
			t.Errorf("init lacks %q:\n%s", want, out)
		}
	}
	vault.Migrate(time.Now())
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	h.ok(human, "task", "add", "One", "--project", "demo")
	if out := h.ok(coord, "vault", "render"); !strings.Contains(out, "demo: 1 notes written, 0 removed") {
		t.Errorf("render:\n%s", out)
	}
	h.expect(1, "not-your-project", coord, "vault", "render", "--project", "other")
	var res struct {
		Checks []vault.Finding `json:"checks"`
		Links  []vault.Broken  `json:"links"`
	}
	if err := json.Unmarshal([]byte(h.ok(human, "vault", "doctor", "--json")), &res); err != nil || len(res.Checks) == 0 || res.Checks[0].Name != "derived notes" || res.Checks[0].Detail != "on" {
		t.Fatalf("doctor: %v %+v", err, res)
	}
	h.expect(1, "human-only", coord, "vault", "off", "--yes")
	if out := h.ok(human, "vault", "off", "--yes"); !strings.Contains(out, "vault off: 1 derived notes removed") {
		t.Errorf("off:\n%s", out)
	}
	if vault.On() {
		t.Fatal("on after off")
	}
}
