package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/search"
)

// TestProgramIndex: a program's index is kept at
// state/search-programs/<slug>, apart from a project's, and is never
// opened while programs are off, for a program with no folder, or for one
// a project's slug clashes with.
func TestProgramIndex(t *testing.T) {
	tmHome := t.TempDir()
	t.Setenv("TERMINATR_HOME", tmHome)
	if _, err := program.New(program.Options{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := project.New(project.Options{Slug: "web"}); err != nil {
		t.Fatal(err)
	}
	var si SearchService
	defer si.CloseIdle(0)
	off := &config.Config{}
	if _, _, perr := si.programIndex(off, "acme"); perr == nil || perr.Code != config.ProgramsOff {
		t.Fatalf("off: %v", perr)
	}
	if si.Open() != 0 {
		t.Fatal("an index was opened while off")
	}
	on := &config.Config{Features: config.Features{Programs: true}}
	x, dir, perr := si.programIndex(on, "acme")
	if perr != nil {
		t.Fatal(perr)
	}
	if want := filepath.Join(tmHome, "state", "search-programs", "acme"); x.Dir() != want {
		t.Fatalf("index at %s, want %s", x.Dir(), want)
	}
	if want := filepath.Join(tmHome, "projects", "_programs", "acme"); dir != want {
		t.Fatalf("folder %s", dir)
	}
	r, err := x.Search(search.Query{Words: "instructions"})
	if err != nil || len(r.Hits) == 0 {
		t.Fatalf("PROGRAM.md isn't indexed: %+v %v", r, err)
	}
	// A project keeps its index under state/search.
	px, _, perr := si.index("web")
	if perr != nil || px.Dir() != filepath.Join(tmHome, "state", "search", "web") {
		t.Fatalf("project index %v %v", px, perr)
	}
	if _, _, perr := si.programIndex(on, "nope"); perr == nil {
		t.Fatal("a program with no folder")
	}
	// A hand-made clash: the project wins, the program is left out.
	os.MkdirAll(filepath.Join(tmHome, "projects", "_programs", "web"), 0o755)
	os.WriteFile(filepath.Join(tmHome, "projects", "_programs", "web", program.File), []byte("# x\n"), 0o644)
	if _, _, perr := si.programIndex(on, "web"); perr == nil {
		t.Fatal("a program a project clashes with was opened")
	}
}
