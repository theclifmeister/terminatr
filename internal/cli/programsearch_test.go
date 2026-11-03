package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
)

// TestProgramSearch: the director's and the human's tm program search
// reads its members as a thread does, with prefixed ids; others are
// refused; off, programs-off.
func TestProgramSearch(t *testing.T) {
	h, _ := programHarness(t)
	h.env[caller.EnvProject] = "acme"
	for slug, files := range map[string]map[string]string{
		"demo":  {"memory/relay.md": "## Decided\n- The wombat relay runs on port 4433.\n", "memory/coord.md": "---\naudience: coordinator\n---\n## Decided\n- Wombat Fridays.\n"},
		"loner": {"memory/x.md": "## Decided\n- Wombat alone.\n"},
	} {
		p, err := project.Open(slug)
		if err != nil {
			t.Fatal(err)
		}
		for name, text := range files {
			path := p.Path(filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	out := h.ok(directorAcme, "program", "search", "wombat")
	if !strings.Contains(out, "demo:note:relay#decided") || strings.Contains(out, "coord") || strings.Contains(out, "alone") {
		t.Fatalf("search:\n%s", out)
	}
	if out := h.ok(directorAcme, "program", "search", "show", "demo:note:relay#decided"); !strings.Contains(out, "port 4433") {
		t.Fatalf("show:\n%s", out)
	}
	if out := h.ok(directorAcme, "program", "search", "timeline", "demo:note:relay#decided"); !strings.Contains(out, "> ") {
		t.Fatalf("timeline:\n%s", out)
	}
	h.expect(1, "other-program", directorAcme, "program", "search", "wombat", "--program", "beta")
	h.expect(1, "director-only", caller.Caller{Kind: caller.Coordinator, Project: "demo"}, "program", "search", "wombat")
	if out := h.ok(human, "program", "search", "wombat", "--program", "acme", "--project", "demo"); !strings.Contains(out, "demo:note:relay#decided") {
		t.Fatalf("human:\n%s", out)
	}
	writeTestConfig(t, h, "[programs.acme]\nprojects = [\"demo\", \"web\"]\n")
	h.expect(1, "programs-off", directorAcme, "program", "search", "wombat")
}
