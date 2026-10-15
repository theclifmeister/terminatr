// Package ci holds tests of the CI configuration itself.
package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func root(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestWeeklyPipesUseBash: a `run` that pipes into tee must set
// shell: bash, or the default `bash -e` hides the left side's status
// (no pipefail) and the step is green whatever the canary says.
func TestWeeklyPipesUseBash(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(root(t), ".github/workflows/weekly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	steps := regexp.MustCompile(`(?m)^      - `).Split(string(b), -1)
	piped := 0
	for _, s := range steps {
		if !regexp.MustCompile(`(?m)^\s+run:.*\|\s*tee\b`).MatchString(s) {
			continue
		}
		piped++
		if !regexp.MustCompile(`(?m)^        shell: bash$`).MatchString(s) {
			t.Errorf("step pipes into tee without shell: bash:\n%s", s)
		}
	}
	if piped < 2 {
		t.Errorf("found %d piped steps, want the canary and the plugin check", piped)
	}
}

func TestCIUsesCodeFilter(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(root(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "scripts/ci-code-files.sh") {
		t.Error("ci.yml does not use scripts/ci-code-files.sh for the docs-only filter")
	}
}

func TestCodeFilesScript(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"README.md\nCONTRIBUTING.md\ndocs/SPEC.md\nsite/index.md\ndocs/a/b.md\n", ""},
		{"internal/skill/rules/thread.safety.md\n", "internal/skill/rules/thread.safety.md\n"},
		{"internal/mod/bundled/default-templates/templates/bug.md\nREADME.md\n", "internal/mod/bundled/default-templates/templates/bug.md\n"},
		{"examples/mods/README.md\n", "examples/mods/README.md\n"},
		{"cmd/tm/main.go\ndocs/x.md\n", "cmd/tm/main.go\n"},
		{"", ""},
	}
	for _, c := range cases {
		cmd := exec.Command("sh", filepath.Join(root(t), "scripts/ci-code-files.sh"))
		cmd.Stdin = strings.NewReader(c.in)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if string(out) != c.want {
			t.Errorf("%q: got %q, want %q", c.in, out, c.want)
		}
	}
}
