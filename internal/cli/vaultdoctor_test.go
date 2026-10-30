package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vault"
)

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

// TestVaultInitLink (T873): tm vault init links ~/terminatr-obsidian
// (a scratch HOME here), names it in the closing line, --no-link makes
// none, and tm vault off removes it.
func TestVaultInitLink(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	hd := t.TempDir()
	t.Setenv("HOME", hd)
	t.Setenv("USERPROFILE", hd)
	link := filepath.Join(hd, "terminatr-obsidian")

	h.expect(2, "", human, "vault", "init", "--yes", "--no-link", "--link", "x")
	if out := h.ok(human, "vault", "init", "--yes", "--no-link"); strings.Contains(out, "terminatr-obsidian") {
		t.Errorf("--no-link:\n%s", out)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("--no-link made a link")
	}
	out := h.ok(human, "vault", "init", "--yes")
	if !strings.Contains(out, "Open ~/terminatr-obsidian as a vault in Obsidian") {
		t.Errorf("init:\n%s", out)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("no link: %v", err)
	}
	if out := h.ok(human, "vault", "doctor"); !strings.Contains(out, "folder link") {
		t.Errorf("doctor:\n%s", out)
	}
	if out := h.ok(human, "vault", "off", "--yes"); !strings.Contains(out, "removed") {
		t.Errorf("off:\n%s", out)
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("off left the link")
	}
}
