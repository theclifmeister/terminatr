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

// TestVaultDoctorLinks (T724): tm vault doctor lists a link into a
// project that isn't there and exits 1; with none it says so.
func TestVaultDoctorLinks(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	if out := h.ok(human, "vault", "doctor"); !strings.Contains(out, "no broken link") {
		t.Errorf("clean:\n%s", out)
	}
	p, _ := project.Open("demo")
	os.MkdirAll(p.Path("memory"), 0o755)
	os.WriteFile(p.Path("memory", "x.md"), []byte("see [[old/tasks/T1|old T1]]\n"), 0o644)
	code, out, _ := h.run(human, "", "vault", "doctor")
	if code != 1 || !strings.Contains(out, "1 links point at nothing:\n  demo/memory/x.md:1 [[old/tasks/T1]]: no project old (renamed or deleted?)\n") {
		t.Errorf("broken: exit %d\n%s", code, out)
	}
	var res struct {
		Links []vault.Broken `json:"links"`
	}
	code, out, _ = h.run(coord, "", "vault", "doctor", "--json")
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 1 || len(res.Links) != 1 || res.Links[0].Target != "old/tasks/T1" {
		t.Fatalf("json: %v %+v", err, res)
	}
	h.expect(2, "", human, "vault", "doctor", "extra")
}
