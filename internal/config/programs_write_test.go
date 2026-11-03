package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TestSetProgram: the program settings popup's writes keep the file's
// comments, refuse what Load would refuse (a project in two programs, a
// bad value or type, an unknown key) and leave the file as it was then.
func TestSetProgram(t *testing.T) {
	write(t, "# mine\n[programs.acme]\nprojects = [\"api\"] # first\n[programs.side]\nprojects = [\"web\"]\n")
	if err := SetProgram("acme", "projects", []string{"api", "docs"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProgram("acme", "authority", AuthorityDelegate); err != nil {
		t.Fatal(err)
	}
	if err := SetProgram("acme", "parallel_threads", 4); err != nil {
		t.Fatal(err)
	}
	if err := SetProgram("new", "paused", true); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	g, _ := c.Program("acme")
	if !slices.Equal(g.Projects, []string{"api", "docs"}) || g.Authority != AuthorityDelegate || g.ParallelThreads != 4 {
		t.Errorf("acme %+v", g)
	}
	if g, ok := c.Program("new"); !ok || !g.Paused {
		t.Errorf("new %+v", g)
	}
	path, _ := Path()
	before, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(before), "# mine\n") {
		t.Errorf("the comment went:\n%s", before)
	}
	for _, bad := range []struct {
		slug, key string
		v         any
	}{
		{"acme", "projects", []string{"api", "web"}}, // web is side's
		{"acme", "authority", "always"},
		{"acme", "parallel_threads", -1},
		{"acme", "paused", "yes"},
		{"acme", "colour", "red"},
		{"Bad", "paused", true},
	} {
		if err := SetProgram(bad.slug, bad.key, bad.v); err == nil {
			t.Errorf("%s.%s = %v written", bad.slug, bad.key, bad.v)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("a refused write changed the file:\n%s", after)
	}
	if err := UnsetProgram("acme", "authority"); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(); func() string { g, _ := c.Program("acme"); return g.Authority }() != AuthorityPropose {
		t.Error("authority isn't back to propose")
	}
}

// TestProgramLinkWrites (B14): the child side's program keys are written
// only by their own writers: accepting writes all four, the settings
// change one of the three on a member only, leaving removes them; the
// generic setting writer, which tm access and agents' paths reach,
// refuses them.
func TestProgramLinkWrites(t *testing.T) {
	write(t, "# mine\n[projects.ios]\nshare = true\n[programs.side]\nprojects = [\"web\"]\n")
	for _, k := range ProgramLinkKeys {
		if err := SetProject("ios", k, true); err == nil {
			t.Errorf("SetProject wrote %s", k)
		}
	}
	if err := SetProgramLinkKey("ios", "program_starts", true); err == nil {
		t.Error("program_starts on a project in no program")
	}
	if err := SetProgramLink("web", ProgramLink{Program: "studio/acme", Tasks: ProgramTasksFile}); err == nil {
		t.Error("a project of a program here joined a paired one")
	}
	if err := SetProgramLink("ios", ProgramLink{Program: "acme", Tasks: ProgramTasksFile}); err == nil {
		t.Error("a program that isn't a paired server's")
	}
	if err := SetProgramLink("ios", ProgramLink{Program: "studio/acme", Tasks: ProgramTasksPropose, Status: true}); err != nil {
		t.Fatal(err)
	}
	if err := SetProgramLinkKey("ios", "program_starts", true); err != nil {
		t.Fatal(err)
	}
	if err := SetProgramLinkKey("ios", "program_tasks", "sideways"); err == nil {
		t.Error("a bad program_tasks")
	}
	if err := SetProgramLinkKey("ios", "program", "nas/x"); err == nil {
		t.Error("program changed by the settings writer")
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if l, in := c.ProgramLink("ios"); !in || l != (ProgramLink{Program: "studio/acme", Tasks: ProgramTasksPropose, Starts: true, Status: true}) {
		t.Fatalf("ios %+v %v", l, in)
	}
	if err := UnsetProgramLink("ios"); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if _, in := c.ProgramLink("ios"); in {
		t.Fatal("still a member")
	}
	path, _ := Path()
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "# mine\n") || !strings.Contains(string(b), "share = true") {
		t.Fatalf("the file:\n%s", b)
	}
}
