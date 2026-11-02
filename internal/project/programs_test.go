package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
)

// TestProgramNamespace: programs and projects share one namespace
// (the programs design §3.4): a program's table or folder keeps its name
// from a new project and a paired server's name, whether programs are on
// or not, and a clash made by hand is listed with the project winning.
func TestProgramNamespace(t *testing.T) {
	root := setup(t)
	os.WriteFile(filepath.Join(root, "config.toml"), []byte("[programs.acme]\nprojects = [\"web\"]\n"), 0o600)
	os.MkdirAll(filepath.Join(root, "projects", "_programs", "studio"), 0o755)
	os.MkdirAll(filepath.Join(root, "projects", "_programs", "Not-A-Slug"), 0o755)
	for _, slug := range []string{"acme", "studio"} {
		_, err := New(Options{Slug: slug})
		var e *Error
		if !errors.As(err, &e) || e.Code != "slug-taken" || !strings.Contains(e.Msg, slug+" is a program") {
			t.Fatalf("New(%s): %v", slug, err)
		}
		if _, err := os.Stat(filepath.Join(root, "projects", slug)); err == nil {
			t.Fatalf("%s was made", slug)
		}
		if why := PeerNameTaken(slug, "me"); why != "a program here is called that" {
			t.Fatalf("PeerNameTaken(%s) = %q", slug, why)
		}
	}
	if why := ProgramTaken("web", nil); why != "" {
		t.Fatalf("web: %q", why)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := ProgramSlugs(cfg); !reflect.DeepEqual(got, []string{"acme", "studio"}) {
		t.Fatalf("ProgramSlugs = %v", got)
	}
	if c := ProgramClashes(cfg, []string{"mini"}); len(c) != 0 {
		t.Fatalf("clashes %v", c)
	}
	// By hand: a project folder acme, and a paired server called studio.
	os.MkdirAll(filepath.Join(root, "projects", "acme"), 0o755)
	want := []ProgramClash{{"acme", "a project"}, {"studio", "a paired server"}}
	if c := ProgramClashes(cfg, []string{"studio"}); !reflect.DeepEqual(c, want) {
		t.Fatalf("clashes %v", c)
	}
	if !ProgramLeftOut("acme") || ProgramLeftOut("studio") {
		t.Fatal("ProgramLeftOut")
	}
	// The project list never sees _programs.
	if ps, _ := List(); len(ps) != 0 {
		for _, p := range ps {
			if strings.HasPrefix(p.Slug, "_") {
				t.Fatalf("List has %s", p.Slug)
			}
		}
	}
}
