package search

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/project"
)

// TestProgramFolder: a program's folder is indexed like a project's,
// with PROGRAM.md where PROJECT.md is and no threads read even if a
// threads folder is there; a project's walk never reads PROGRAM.md.
func TestProgramFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "PROGRAM.md", "---\ngoal: ship SSO\n---\n# Instructions\n\nEvery child uses okta.\n")
	write(t, dir, "PROJECT.md", "# Instructions\n\nstray marzipan\n")
	write(t, dir, "CONTEXT.md", "# Context\n\nThe plan is quartz.\n")
	write(t, dir, "memory/sso.md", "# SSO\n\nThe tenant is shared.\n")
	write(t, dir, "threads/t-0001/brief.md", "# brief\n\nnever pumpernickel\n")
	f := project.Folder{Slug: "acme", Dir: dir, Kind: project.KindProgram}
	x := OpenFolder(&f, filepath.Join(t.TempDir(), "idx"))
	if got := search(t, x, Query{Words: "okta"}); len(got) != 1 || !strings.HasPrefix(got[0], "program") {
		t.Fatalf("okta: %q", got)
	}
	if got := search(t, x, Query{Words: "tenant"}); len(got) != 1 {
		t.Fatalf("tenant: %q", got)
	}
	for _, w := range []string{"pumpernickel", "marzipan"} {
		if got := search(t, x, Query{Words: w}); len(got) != 0 {
			t.Errorf("%s read in a program: %q", w, got)
		}
	}
	srcs, err := walk(dir, project.KindProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs {
		if s.Rel == "PROGRAM.md" {
			t.Error("a project's walk read PROGRAM.md")
		}
	}
	// Open is the project kind, as before.
	if got := search(t, Open(dir, filepath.Join(t.TempDir(), "p")), Query{Words: "pumpernickel"}); len(got) == 0 {
		t.Error("a project's threads aren't read")
	}
}
