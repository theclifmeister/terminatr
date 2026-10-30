package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// fakeObsidian replaces the app for a test: launches are recorded, and
// running says whether Obsidian runs.
func fakeObsidian(t *testing.T, running *bool) *[]string {
	var launched []string
	oldL, oldR := launchObsidian, obsidianRunning
	launchObsidian = func(u string) error { launched = append(launched, u); return nil }
	obsidianRunning = func() (bool, error) {
		if running == nil {
			return false, errors.New("no process list")
		}
		return *running, nil
	}
	t.Cleanup(func() { launchObsidian, obsidianRunning = oldL, oldR })
	return &launched
}

// obsidianJSON writes Obsidian's vault list in the test's home and
// returns its path.
func obsidianJSON(t *testing.T, body string) string {
	d, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(d, "obsidian", "obsidian.json")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestVaultOpen (T874): tm vault open is the human's; it opens a vault
// Obsidian knows (by its real path) by id, and adds one it doesn't only
// on --register or a yes at the prompt, only while Obsidian is closed,
// after a backup; else it prints the one-time steps.
func TestVaultOpen(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	running := false
	launched := fakeObsidian(t, &running)
	h.expect(1, "human-only", coord, "vault", "open")
	h.expect(1, "human-only", thr, "vault", "open")
	h.expect(2, "", human, "vault", "open", "extra")

	// No Obsidian yet.
	if out := h.ok(human, "vault", "open"); !strings.Contains(out, "install Obsidian and start it once") || !strings.Contains(out, "Open folder as vault") {
		t.Errorf("no obsidian.json:\n%s", out)
	}
	h.ok(human, "vault", "init", "--yes")
	userHome, _ := os.UserHomeDir()
	link := filepath.Join(userHome, vault.LinkName)
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("init made no link in the test's home: %v", err)
	}
	const other = `{"vaults":{"0123456789abcdef":{"path":"/elsewhere","ts":1}},"language":"en"}`
	cfg := obsidianJSON(t, other)

	// Not known, no terminal: the steps and --register, nothing written.
	out := h.ok(human, "vault", "open")
	if !strings.Contains(out, "tm vault open --register adds it") || !strings.Contains(out, "Pick ~/terminatr-obsidian") || len(*launched) != 0 {
		t.Errorf("unknown vault:\n%s", out)
	}
	// A no at the prompt.
	old := stdinTTY
	stdinTTY = func(*Env) bool { return true }
	t.Cleanup(func() { stdinTTY = old })
	if code, out, _ := h.run(human, "n\n", "vault", "open"); code != 0 || !strings.Contains(out, "Left Obsidian's settings alone") {
		t.Errorf("no: %d\n%s", code, out)
	}
	// Obsidian running, or no way to tell: refused.
	running = true
	h.expect(1, "obsidian-running", human, "vault", "open", "--register")
	launched = fakeObsidian(t, nil)
	h.expect(1, "can't tell whether Obsidian is running", human, "vault", "open", "--register")
	if b, _ := os.ReadFile(cfg); string(b) != other || len(*launched) != 0 {
		t.Fatalf("written while running: %s", b)
	}
	if m, _ := filepath.Glob(cfg + ".tm-*"); len(m) != 0 {
		t.Fatalf("backup while refused: %v", m)
	}

	// A yes, Obsidian closed: one entry for the link, a backup, launched.
	running = false
	launched = fakeObsidian(t, &running)
	code, out, errs := h.run(human, "y\n", "vault", "open")
	if code != 0 || !strings.Contains(out, "Added ~/terminatr-obsidian to Obsidian's vault list (backup ") || !strings.Contains(out, "Opening ~/terminatr-obsidian in Obsidian.") {
		t.Fatalf("yes: %d\n%s%s", code, out, errs)
	}
	root, _ := home.ProjectsDir()
	id, err := vault.KnownVault(cfg, root)
	if err != nil || id == "" || len(*launched) != 1 || (*launched)[0] != "obsidian://open?vault="+id {
		t.Fatalf("after yes: %q %v %q", id, err, *launched)
	}
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), `"path":`+quoteJSON(link)) || !strings.HasSuffix(string(b), other[len(`{"vaults":{`):]) {
		t.Errorf("obsidian.json: %s", b)
	}
	if m, _ := filepath.Glob(cfg + ".tm-*.bak"); len(m) != 1 {
		t.Errorf("backups: %v", m)
	} else if b, _ := os.ReadFile(m[0]); string(b) != other {
		t.Errorf("backup: %s", b)
	}

	// Known now: opens without asking or writing, Obsidian running or not.
	running = true
	stdinTTY = old
	if out := h.ok(human, "vault", "open"); !strings.Contains(out, "Opening ~/terminatr-obsidian") || len(*launched) != 2 {
		t.Errorf("known:\n%s", out)
	}
	// Listed by the hidden folder's own path: the same vault.
	obsidianJSON(t, `{"vaults":{"feedfacefeedface":{"path":`+quoteJSON(root)+`,"ts":1}}}`)
	h.ok(human, "vault", "open")
	if (*launched)[2] != "obsidian://open?vault=feedfacefeedface" {
		t.Errorf("by the real path: %q", *launched)
	}
}

// TestVaultInitOpen: init --open inits, then opens; --register only with
// --open.
func TestVaultInitOpen(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	running := false
	launched := fakeObsidian(t, &running)
	h.expect(2, "", human, "vault", "init", "--register", "--yes")
	h.expect(1, "human-only", coord, "vault", "init", "--open", "--yes")
	cfg := obsidianJSON(t, `{"vaults":{}}`)
	out := h.ok(human, "vault", "init", "--open", "--register", "--yes")
	if !strings.Contains(out, "vault on") || !strings.Contains(out, "Added ~/terminatr-obsidian") || len(*launched) != 1 || strings.Contains(out, "Open folder as vault") {
		t.Errorf("init --open:\n%s", out)
	}
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), vault.LinkName) {
		t.Errorf("obsidian.json: %s", b)
	}
}

func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
