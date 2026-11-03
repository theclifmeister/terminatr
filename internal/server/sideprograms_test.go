package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
)

// TestSidePrograms: projects.watch's programs and each project's
// program exist only while programs are on; a program whose name a
// project has too is left out, its members standalone.
func TestSidePrograms(t *testing.T) {
	programFixture(t)
	load := func() *config.Config {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	writeConfig(t, "[programs.acme]\nprojects = [\"demo\"]\n")
	if ps := SidePrograms(load()); ps != nil {
		t.Fatalf("programs while off: %+v", ps)
	}
	ps, err := ReadProjects("", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ps)
	if strings.Contains(string(b), `"program"`) {
		t.Errorf("a project's program while off: %s", b)
	}
	writeConfig(t, programsOn+"parallel_threads = 3\nauthority = \"delegate\"\n")
	got := SidePrograms(load())
	if len(got) != 2 || got[0].Slug != "acme" || got[1].Slug != "beta" {
		t.Fatalf("programs %+v", got)
	}
	if got[1].Authority != config.AuthorityDelegate || got[1].ParallelThreads != 3 || got[0].Authority != config.AuthorityPropose ||
		len(got[0].Projects) != 1 || got[0].Projects[0] != "demo" || got[0].Error != "" {
		t.Errorf("programs %+v", got)
	}
	ps, err = ReadProjects("", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		want := map[string]string{"demo": "acme", "web": "beta"}[p.Slug]
		if p.Program != want {
			t.Errorf("%s's program %q, want %q", p.Slug, p.Program, want)
		}
	}
	// A program of a project's name is left out.
	writeConfig(t, "[features]\nprograms = true\n[programs.web]\nprojects = [\"demo\"]\n")
	if got := SidePrograms(load()); len(got) != 0 {
		t.Errorf("a left-out program listed: %+v", got)
	}
	if programOfSide(load(), "demo") != "" {
		t.Error("a member of a left-out program is under it")
	}
}
