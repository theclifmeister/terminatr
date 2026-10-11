package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modReviewAge = `
id      = "review-age"
name    = "Review age"
version = "0.3.1"
api     = "1.0"
[provides]
main    = "main.ts"
[provides.rules]
coordinator = "rules.md"
[permissions]
project_read = true
`

// modSource writes a mod folder with mod.toml and the files it names.
func modSource(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"mod.toml": manifest, "main.ts": "", "rules.md": "Age every review.\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// withTTY makes stdin a terminal for the test.
func withTTY(t *testing.T) {
	old := stdinTTY
	stdinTTY = func(*Env) bool { return true }
	t.Cleanup(func() { stdinTTY = old })
}

func TestModInstallEnableRemove(t *testing.T) {
	h := newHarness(t)
	src := modSource(t, modReviewAge)
	h.expect(1, "human-only", coord, "mod", "install", src, "--yes")
	h.expect(1, "human-only", thr, "mod", "enable", "review-age", "--yes")
	// Without a terminal it needs --yes, and installs nothing.
	h.expect(2, "confirm with --yes", human, "mod", "install", src)
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "No mods installed") {
		t.Fatalf("list: %q", out)
	}
	out := h.ok(human, "mod", "install", src, "--yes")
	for _, want := range []string{"Review age 0.3.1 (review-age)", "reads your projects", "Age every review.", "installed review-age 0.3.1, disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("install output lacks %q:\n%s", want, out)
		}
	}
	h.expect(1, "mod-exists", human, "mod", "install", src, "--yes")
	if out := h.ok(coord, "mod", "list"); !strings.Contains(out, "○ review-age") || !strings.Contains(out, "off") {
		t.Errorf("list after install: %q", out)
	}
	// On a terminal, n keeps it off.
	withTTY(t)
	if code, _, _ := h.run(human, "n\n", "mod", "enable", "review-age"); code != 1 {
		t.Fatalf("n enabled it: exit %d", code)
	}
	if code, out, errs := h.run(human, "y\n", "mod", "enable", "review-age"); code != 0 || !strings.Contains(out, "review-age enabled: reads your projects") {
		t.Fatalf("y: exit %d %q %q", code, out, errs)
	}
	var rows []modRow
	if err := json.Unmarshal([]byte(h.ok(human, "mod", "list", "--json")), &rows); err != nil || len(rows) != 1 || rows[0].State != "enabled" || rows[0].Grants[0] != "project_read" {
		t.Fatalf("list --json: %+v %v", rows, err)
	}
	h.ok(human, "mod", "disable", "review-age")
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "○ review-age") {
		t.Errorf("after disable: %q", out)
	}
	h.expect(1, "not-confirmed", human, "mod", "remove", "review-age")
	h.ok(human, "mod", "remove", "review-age", "--yes")
	if _, err := os.Stat(filepath.Join(h.root, "mods", "review-age")); err == nil {
		t.Error("still installed")
	}
}

func TestModUpdate(t *testing.T) {
	h := newHarness(t)
	src := modSource(t, modReviewAge)
	h.ok(human, "mod", "install", src, "--yes")
	h.ok(human, "mod", "enable", "review-age", "--yes")
	if out := h.ok(human, "mod", "update", "review-age"); !strings.Contains(out, "up to date") {
		t.Fatalf("unchanged: %q", out)
	}
	if out := h.ok(human, "mod", "list", "--outdated"); !strings.Contains(out, "up to date") {
		t.Fatalf("outdated, unchanged: %q", out)
	}
	// A new version with no new permissions updates without asking.
	os.WriteFile(filepath.Join(src, "mod.toml"), []byte(strings.Replace(modReviewAge, "0.3.1", "0.3.2", 1)), 0o644)
	if out := h.ok(human, "mod", "list", "--outdated"); !strings.Contains(out, "→ 0.3.2") {
		t.Fatalf("outdated: %q", out)
	}
	if out := h.ok(human, "mod", "update"); !strings.Contains(out, "updated review-age to 0.3.2") {
		t.Fatalf("update: %q", out)
	}
	// One that asks for more needs consent, and then has it granted.
	more := strings.Replace(modReviewAge, "0.3.1", "0.4.0", 1) + "inbox = true\n"
	os.WriteFile(filepath.Join(src, "mod.toml"), []byte(more), 0o644)
	h.expect(1, "", human, "mod", "update", "review-age")
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "0.3.2") {
		t.Fatalf("updated without consent: %q", out)
	}
	out := h.ok(human, "mod", "update", "review-age", "--yes")
	if !strings.Contains(out, "It now asks for:") || !strings.Contains(out, "tells the coordinator") {
		t.Fatalf("update consent: %q", out)
	}
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "● review-age") || !strings.Contains(out, "tells the coordinator") {
		t.Fatalf("after consent: %q", out)
	}
}

func TestModValidate(t *testing.T) {
	h := newHarness(t)
	good := modSource(t, modReviewAge)
	bad := modSource(t, `id = "Bad"`)
	out := h.ok(thr, "mod", "validate", good)
	if !strings.HasPrefix(out, "ok       review-age 0.3.1") {
		t.Errorf("good: %q", out)
	}
	code, out, _ := h.run(thr, "", "mod", "validate", good, bad)
	if code != 1 || !strings.Contains(out, "invalid  "+bad) || !strings.Contains(out, "has no name") {
		t.Errorf("bad: exit %d %q", code, out)
	}
	h.expect(2, "takes no --yes", human, "mod", "validate", good, "--yes")
	h.expect(2, "unknown subcommand", human, "mod", "search", "x")
}
