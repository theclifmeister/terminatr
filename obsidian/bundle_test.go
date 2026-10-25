package obsidian

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestFiles: the plugin bundles from the embedded sources into one
// CommonJS main.js that requires only Obsidian and node built-ins, with
// tm's version in its manifest.
func TestFiles(t *testing.T) {
	files, err := Files("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		ID, Version   string
		IsDesktopOnly bool
	}
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil || m.ID != ID || m.Version != "1.2.3" || !m.IsDesktopOnly {
		t.Fatalf("manifest %s: %v", files["manifest.json"], err)
	}
	if dev, _ := Files("dev"); !strings.Contains(string(dev["manifest.json"]), `"version": "0.0.0"`) {
		t.Fatalf("dev manifest %s", dev["manifest.json"])
	}
	main := string(files["main.js"])
	for _, s := range []string{"module.exports", "needs", "--json", "STABLE_MS"} {
		if !strings.Contains(main, s) {
			t.Errorf("main.js lacks %q", s)
		}
	}
	for _, r := range regexp.MustCompile(`require\("([^"]+)"\)`).FindAllStringSubmatch(main, -1) {
		if !slices.Contains(External, r[1]) {
			t.Errorf("main.js requires %q", r[1])
		}
	}
	// Never a setting, a pairing or a share: no command but the ones
	// needs.ts builds, and no --approved-by-user.
	// Accepting a link is the console's too.
	for _, s := range []string{"approved-by-user", "config.toml", `"pair"`, `"share"`, "innerHTML", "MarkdownRenderer", `"link", "accept"`} {
		if strings.Contains(main, s) {
			t.Errorf("main.js has %q", s)
		}
	}
	if len(files["styles.css"]) == 0 {
		t.Error("no styles.css")
	}
}

// TestNodeTests runs the plugin's own tests (node --test) when a node
// that runs TypeScript as is is there; CI's obsidian job always runs
// them, with tsc --strict.
func TestNodeTests(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node isn't on PATH")
	}
	out, err := exec.Command(node, "--version").Output()
	if err != nil || !regexp.MustCompile(`^v(2[3-9]|[3-9][0-9])\.`).Match(out) {
		t.Skipf("node %s runs no TypeScript as is", strings.TrimSpace(string(out)))
	}
	cmd := exec.Command(node, "--test", filepath.Join("test", "needs.test.ts"))
	cmd.Env = append(os.Environ(), "NODE_NO_WARNINGS=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
}
