package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
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
	if out := h.ok(coord, "vault", "render"); !strings.Contains(out, "task and thread notes: 1 written, 0 removed;") || strings.Contains(out, "_tm/") {
		t.Errorf("render:\n%s", out)
	}
	h.expect(1, "not-your-project", coord, "vault", "render", "--project", "other")
	h.ok(human, "project", "new", "other")
	var rr struct {
		Projects []string `json:"projects"`
		Shared   []struct {
			Path string `json:"path"`
			What string `json:"what"`
		} `json:"shared"`
	}
	// The human's whole render writes _tm/ again: a view for the new project.
	if err := json.Unmarshal([]byte(h.ok(human, "vault", "render", "--json")), &rr); err != nil || len(rr.Projects) != 2 || len(rr.Shared) == 0 || rr.Shared[0].What != "updated" {
		t.Fatalf("render --json: %v %+v", err, rr)
	}
	if err := json.Unmarshal([]byte(h.ok(human, "vault", "render", "--project", "demo", "--days", "1", "--json")), &rr); err != nil || len(rr.Projects) != 1 || len(rr.Shared) != 0 {
		t.Fatalf("render --project: %v %+v", err, rr)
	}
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

// TestVaultRender (T719): tm vault render writes the derived notes and
// the daily notes of the days asked, once the vault is on; a thread
// can't, and --days is checked.
func TestVaultRender(t *testing.T) {
	h := newHarness(t)
	h.ok(human, "project", "new", "demo")
	h.ok(human, "task", "add", "First", "--project", "demo")
	h.expect(1, "vault-off", human, "vault", "render")
	mdfile.WriteYAML(true)
	t.Cleanup(func() { mdfile.WriteYAML(false) })
	d, _ := home.Dir()
	os.MkdirAll(filepath.Join(d, "state"), 0o700)
	os.WriteFile(filepath.Join(d, "state", "vault"), nil, 0o600)
	h.expect(1, "coordinator-only", thr, "vault", "render")
	h.expect(2, "--days takes", human, "vault", "render", "--days", "0")
	h.expect(2, "", human, "vault", "render", "extra")
	out := h.ok(human, "vault", "render", "--days", "3")
	if !strings.Contains(out, "task and thread notes: 1 written, 0 removed; daily notes of the last 3 days: 1 written") {
		t.Fatalf("render: %s", out)
	}
	p, _ := project.Open("demo")
	if _, err := os.Stat(p.Path("tasks", "T1.md")); err != nil {
		t.Fatal(err)
	}
	root, _ := home.ProjectsDir()
	if b, err := os.ReadFile(filepath.Join(root, "_tm", "daily", time.Now().Format(time.DateOnly)+".md")); err != nil || !strings.Contains(string(b), "[[demo/tasks/T1|T1]]") {
		t.Fatalf("daily note: %v\n%s", err, b)
	}
	var res struct {
		Written int `json:"notes_written"`
		Daily   int `json:"daily_written"`
		Days    int `json:"days"`
	}
	if err := json.Unmarshal([]byte(h.ok(coord, "vault", "render", "--json")), &res); err != nil || res.Written != 0 || res.Daily != 0 || res.Days != 2 {
		t.Fatalf("again: %+v, %v", res, err)
	}
}
