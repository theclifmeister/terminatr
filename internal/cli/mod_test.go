package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/gittest"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
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
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "No mods of your own yet") || !strings.Contains(out, "● default-theme") || strings.Contains(out, "review-age") {
		t.Fatalf("list: %q", out)
	}
	out := h.ok(human, "mod", "install", src, "--yes")
	for _, want := range []string{"Review age 0.3.1 (review-age)", "reads your projects", "Age every review.", "installed review-age 0.3.1, disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("install output lacks %q:\n%s", want, out)
		}
	}
	h.expect(1, "already installed: tm mod update review-age", human, "mod", "install", src, "--yes")
	// A hint that works (T301 B10): a built-in id can't be updated, nor
	// a mod that records no source.
	h.expect(1, "a built-in mod's id; give your copy another id", human, "mod", "install",
		modSource(t, strings.Replace(modReviewAge, `"review-age"`, `"pr-ci-column"`, 1)), "--yes")
	byHand := filepath.Join(h.root, "mods", "by-hand")
	if err := os.CopyFS(byHand, os.DirFS(modSource(t, strings.Replace(modReviewAge, `"review-age"`, `"by-hand"`, 1)))); err != nil {
		t.Fatal(err)
	}
	h.expect(1, "records no source (installed by hand, or by tm mod dev); tm mod remove by-hand first", human, "mod", "install",
		modSource(t, strings.Replace(modReviewAge, `"review-age"`, `"by-hand"`, 1)), "--yes")
	if err := os.RemoveAll(byHand); err != nil {
		t.Fatal(err)
	}
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
	if err := json.Unmarshal([]byte(h.ok(human, "mod", "list", "--json")), &rows); err != nil || len(rows) != 1+len(mod.BundledIDs()) || rows[0].ID != "review-age" || rows[0].State != "enabled" || rows[0].Grants[0] != "project_read" || rows[0].Builtin || !rows[1].Builtin {
		t.Fatalf("list --json: %+v %v", rows, err)
	}
	h.ok(human, "mod", "disable", "review-age")
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "○ review-age") {
		t.Errorf("after disable: %q", out)
	}
	// The built-in defaults tm can't do without stay on; the templates
	// can be turned off and on; none is removed or updated on its own.
	h.expect(1, "it can't be turned off; pick another theme", human, "mod", "disable", "default-theme")
	h.ok(human, "mod", "disable", "default-templates")
	if out := h.ok(human, "mod", "list"); !strings.Contains(out, "○ default-templates") || !strings.Contains(out, "● default-theme") {
		t.Errorf("after disabling the templates: %q", out)
	}
	h.ok(human, "mod", "enable", "default-templates", "--yes")
	h.expect(1, "built in", human, "mod", "remove", "default-templates", "--yes")
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

// TestModInstallSubfolder: tm mod install takes a git URL with
// //<subfolder>, copies in only that folder, and list --outdated and
// update follow its newest tag that changed the folder.
func TestModInstallSubfolder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	h := newHarness(t)
	repo := t.TempDir()
	git := gittest.In(t, repo)
	write := func(name, body string) {
		p := filepath.Join(repo, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("README.md", "mods\n")
	for name, body := range map[string]string{"mod.toml": modReviewAge, "main.ts": "", "rules.md": "Age every review.\n"} {
		write("mods/review-age/"+name, body)
	}
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "one")
	git("tag", "v1.0.0")
	url := "file://" + repo + "//mods/review-age"
	h.expect(2, "..", human, "mod", "install", "file://"+repo+"//mods/../mods/review-age", "--yes")
	out := h.ok(human, "mod", "install", url+"@v1.0.0", "--yes")
	if !strings.Contains(out, "From "+url+"@v1.0.0.") || !strings.Contains(out, "installed review-age 0.3.1") {
		t.Fatalf("install: %q", out)
	}
	if _, err := os.Stat(filepath.Join(h.root, "mods", "review-age", "README.md")); err == nil {
		t.Error("the repository's root was copied in")
	}
	write("README.md", "more\n")
	git("commit", "-q", "-am", "two")
	git("tag", "v1.1.0")
	if out := h.ok(human, "mod", "list", "--outdated"); !strings.Contains(out, "up to date") {
		t.Fatalf("outdated after a tag outside the folder: %q", out)
	}
	write("mods/review-age/mod.toml", strings.Replace(modReviewAge, "0.3.1", "0.3.2", 1))
	git("commit", "-q", "-am", "three")
	git("tag", "v1.2.0")
	if out := h.ok(human, "mod", "list", "--outdated"); !strings.Contains(out, "→ v1.2.0") {
		t.Fatalf("outdated: %q", out)
	}
	if out := h.ok(human, "mod", "update", "review-age"); !strings.Contains(out, "updated review-age to 0.3.2") || !strings.Contains(out, url+"@v1.2.0") {
		t.Fatalf("update: %q", out)
	}
	if out := h.ok(human, "mod", "list", "--json"); !strings.Contains(out, url+"@v1.2.0") {
		t.Fatalf("source after update: %q", out)
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
	// A keymap whose own actions want one key, as the console would
	// refuse it (T301 B3); the sidebar's and a list's may share one.
	keys := t.TempDir()
	for name, text := range map[string]string{
		"mod.toml":    "id = \"keys\"\nname = \"Keys\"\nversion = \"1.0.0\"\napi = \"1.0\"\n[provides]\nkeymap = \"keymap.toml\"\n",
		"keymap.toml": "\"list.up\" = [\"K\"]\n\"list.down\" = [\"K\"]\n\"side.page-down\" = [\"ctrl+d\"]\n\"list.page-down\" = [\"ctrl+d\"]\n",
	} {
		if err := os.WriteFile(filepath.Join(keys, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ = h.run(thr, "", "mod", "validate", keys)
	if code != 1 || !strings.Contains(out, "list.down: its key is taken by list.up") || strings.Contains(out, "page-down") {
		t.Errorf("own conflict: exit %d %q", code, out)
	}
	h.expect(2, "takes no --yes", human, "mod", "validate", good, "--yes")
	h.expect(2, "unknown subcommand", human, "mod", "search", "x")
}

// TestModRun: tm mod run sends the command to the server, the human's
// only, with the project when one is given.
func TestModRun(t *testing.T) {
	h := newHarness(t)
	var got []proto.ModCommandParams
	old := runModCommand
	runModCommand = func(p proto.ModCommandParams) error { got = append(got, p); return nil }
	t.Cleanup(func() { runModCommand = old })
	h.expect(1, "human-only", thr, "mod", "run", "lab", "relabel")
	h.expect(2, "usage", human, "mod", "run", "lab")
	h.expect(2, "takes no --yes", human, "mod", "run", "lab", "relabel", "--yes")
	if out := h.ok(human, "mod", "run", "lab", "relabel"); !strings.Contains(out, "ran lab:relabel") {
		t.Fatalf("out %q", out)
	}
	if len(got) != 1 || got[0] != (proto.ModCommandParams{Mod: "lab", Command: "relabel"}) {
		t.Fatalf("sent %+v", got)
	}
}

// TestModExamples: tm mod examples shows the folder and each example's
// install lines, an agent may run it, and a built-in one says to copy it
// under another id first.
func TestModExamples(t *testing.T) {
	h := newHarness(t)
	out := h.ok(thr, "mod", "examples")
	for _, want := range []string{mod.ExamplesURL, "git clone --depth 1 --branch " + mod.ExamplesRef + " " + mod.ExamplesRepo, "tm mod install " + mod.ExamplesURL + "/nord",
		"cp -R terminatr/examples/mods/pr-ci-column my-pr-ci-column", "tm mod install my-pr-ci-column", "memory-search  an agent tool", "tm mod install " + mod.ExamplesURL + "/standup",
		"gitlab  a code host (GitLab, through glab) · runs glab with your GitLab login: anything it wants, as you\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("examples lack %q:\n%s", want, out)
		}
	}
	var got struct {
		Examples []struct {
			ID      string   `json:"id"`
			Builtin bool     `json:"builtin"`
			Install []string `json:"install"`
		} `json:"examples"`
	}
	if err := json.Unmarshal([]byte(h.ok(human, "mod", "examples", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Examples) != len(mod.Examples) {
		t.Fatalf("%d examples", len(got.Examples))
	}
	for _, x := range got.Examples {
		if x.Builtin != (x.ID == "pr-ci-column" || x.ID == "memory-tab" || x.ID == "tasks-tab" || x.ID == "ticker") || len(x.Install) == 0 {
			t.Errorf("%+v", x)
		}
	}
}

// TestModEnableToolsAgentMod: enabling a mod that offers tools says,
// while the Agent mod is off, that Claude threads won't get them (T301
// L1); with it on it says nothing.
func TestModEnableToolsAgentMod(t *testing.T) {
	h := newHarness(t)
	src := modSource(t, `id = "lint"
name = "Lint"
version = "1.0.0"
api = "1.0"
[provides]
main = "main.ts"
[[provides.tools]]
id = "ping"
description = "Answers pong."
[permissions]
tools = true
`)
	if out := h.ok(human, "mod", "install", src, "--yes"); !strings.Contains(out, mod.ToolsAgentModNote) {
		t.Errorf("install consent lacks the Agent mod: %q", out)
	}
	if out := h.ok(human, "mod", "enable", "lint", "--yes"); !strings.Contains(out, "The Agent mod is off: Claude threads get its tools only") {
		t.Errorf("enable, Agent mod off: %q", out)
	}
	cfg := filepath.Join(h.root, "config.toml")
	old, _ := os.ReadFile(cfg)
	if err := os.WriteFile(cfg, append([]byte("[agent_mod]\nenabled = true\n"), old...), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := h.ok(human, "mod", "enable", "lint", "--yes"); strings.Contains(out, "Agent mod is off") {
		t.Errorf("enable, Agent mod on: %q", out)
	}
}
