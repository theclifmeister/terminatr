package config

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestRenameProjectRenamesMembers: tm project rename names the new slug
// in the program that lists the old one, in its place, and keeps the
// file's comments and the other programs' lists.
func TestRenameProjectRenamesMembers(t *testing.T) {
	write(t, "# mine\n[projects.api]\nmerge = \"thread\"\n\n[programs.acme]\nprojects = [\"web\", \"api\", \"mini/api\"] # order\n\n[programs.side]\nprojects = [\"docs\"]\n")
	if err := RenameProject("api", "core"); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	g, _ := c.Program("acme")
	if !slices.Equal(g.Projects, []string{"web", "core", "mini/api"}) {
		t.Errorf("acme lists %v", g.Projects)
	}
	if g, _ := c.Program("side"); !slices.Equal(g.Projects, []string{"docs"}) {
		t.Errorf("side lists %v", g.Projects)
	}
	if c.ProgramOf("core") != "acme" || c.ProgramOf("api") != "" {
		t.Errorf("ProgramOf core %q, api %q", c.ProgramOf("core"), c.ProgramOf("api"))
	}
	path, _ := Path()
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# mine\n") || !strings.Contains(string(data), "[projects.core]") {
		t.Errorf("file:\n%s", data)
	}
}

// TestRenameMembersOnlyItsLists: a file whose programs don't list the
// project comes back as it was; a dotted-key list is refused (ErrForm).
func TestRenameMembersOnlyItsLists(t *testing.T) {
	in := []byte("[programs.acme]\nprojects = [\"web\"]\n")
	out, err := RenameMembers(in, "api", "core")
	if err != nil || string(out) != string(in) {
		t.Errorf("unlisted: %q, %v", out, err)
	}
	dotted := []byte("programs.acme.projects = [\"api\"]\n")
	if _, err := RenameMembers(dotted, "api", "core"); !errors.Is(err, ErrForm) {
		t.Errorf("dotted: %v, want ErrForm", err)
	}
}

// TestRemoveMember: archiving or deleting a member takes it out of its
// program, and says which; a project in none changes nothing.
func TestRemoveMember(t *testing.T) {
	write(t, "[programs.acme]\nprojects = [\"web\", \"api\"]\nauthority = \"delegate\"\n")
	g, err := RemoveMember("api")
	if err != nil || g != "acme" {
		t.Fatalf("RemoveMember: %q, %v", g, err)
	}
	c, _ := Load()
	if p, _ := c.Program("acme"); !slices.Equal(p.Projects, []string{"web"}) || p.Authority != AuthorityDelegate {
		t.Errorf("acme %+v", p)
	}
	if g, err := RemoveMember("docs"); err != nil || g != "" {
		t.Errorf("not a member: %q, %v", g, err)
	}
	if _, err := RemoveMember("web"); err != nil {
		t.Fatal(err)
	}
	c, _ = Load()
	if p, ok := c.Program("acme"); !ok || len(p.Projects) != 0 {
		t.Errorf("last member gone: %+v %v", p, ok)
	}
}

// TestRenameProgram: the table and the ones below it take the new name,
// comments stay; onto a taken name is refused and changes nothing.
func TestRenameProgram(t *testing.T) {
	write(t, "# mine\n[programs.acme] # the big one\nprojects = [\"api\"]\nparallel_threads = 3\n\n[programs.side]\nprojects = [\"web\"]\n")
	if err := RenameProgram("acme", "studio"); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Program("acme"); ok {
		t.Error("acme still there")
	}
	if g, ok := c.Program("studio"); !ok || g.ParallelThreads != 3 || !slices.Equal(g.Projects, []string{"api"}) {
		t.Errorf("studio %+v %v", g, ok)
	}
	path, _ := Path()
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "[programs.studio] # the big one") {
		t.Errorf("file:\n%s", data)
	}
	if err := RenameProgram("studio", "side"); !errors.Is(err, ErrProgramSet) {
		t.Errorf("onto side: %v, want ErrProgramSet", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(data) {
		t.Errorf("a refused rename changed the file:\n%s", after)
	}
	if _, err := RenameProgramTable([]byte("programs.x.projects = [\"a\"]\n"), "x", "y"); !errors.Is(err, ErrForm) {
		t.Errorf("dotted: %v, want ErrForm", err)
	}
}
