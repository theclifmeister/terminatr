package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// TestVaultMigrateCheck: tm vault migrate --check lists the files the
// move to YAML would rewrite and those it can't, and changes nothing.
func TestVaultMigrateCheck(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	p, _ := project.Open("demo")
	os.MkdirAll(p.Path("memory"), 0o755)
	note := "+++\ntitle = \"Retries\"\n+++\n## Decided\n"
	os.WriteFile(p.Path("memory", "retries.md"), []byte(note), 0o644)
	os.WriteFile(p.Path("memory", "broken.md"), []byte("+++\ntitle = \n+++\n"), 0o644)

	out := h.ok(human, "vault", "migrate", "--check")
	for _, want := range []string{"rewrites 1 files' front matter", "  demo/memory/retries.md\n", "  demo/memory/broken.md: stays TOML: "} {
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
	if err := json.Unmarshal([]byte(h.ok(coord, "vault", "migrate", "--check", "--json")), &res); err != nil || res.Migrated || len(res.Files) != 2 || res.Files[0].Path != "memory/broken.md" || res.Files[0].Error == "" {
		t.Fatalf("json: %v %+v", err, res)
	}
	vault.Migrate(time.Now())
	if out := h.ok(human, "vault", "migrate", "--check"); !strings.Contains(out, "1 live files still have TOML") {
		t.Errorf("after the move:\n%s", out)
	}
	h.expect(2, "", human, "vault", "migrate")
	h.expect(2, "", human, "vault", "nope")
}
