package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestPrograms: [features] programs, the [programs.*] tables and a
// project's program keys are read, defaults filled in, whatever the
// switch says (programs design §3.3, §3.5).
func TestPrograms(t *testing.T) {
	write(t, "")
	if c, _ := Load(); c.ProgramsOn() || len(c.ProgramSlugs()) != 0 {
		t.Fatalf("no file: %+v", c)
	}
	write(t, `
[features]
programs = true
experimnt = 1

[programs.acme]
projects = ["acme-api", "mini/acme-ios"]
authority = "delegate"
parallel_threads = 3
director_model = "opus"

[programs.side]

[projects.acme-api]
yolo = false

[projects.web]
program = "studio/shop"
program_starts = true
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.ProgramsOn() || !reflect.DeepEqual(c.ProgramSlugs(), []string{"acme", "side"}) {
		t.Fatalf("on %v programs %v", c.ProgramsOn(), c.ProgramSlugs())
	}
	if !reflect.DeepEqual(c.Unknown, []string{"features.experimnt"}) {
		t.Fatalf("unknown %v", c.Unknown)
	}
	p, ok := c.Program("acme")
	want := Program{Slug: "acme", Projects: []string{"acme-api", "mini/acme-ios"}, Authority: AuthorityDelegate, ParallelThreads: 3,
		DependencyMet: DependencyMerged, DirectorModel: "opus", AutoClear: true, AnswerFacts: true, SearchChildren: true}
	if !ok || !reflect.DeepEqual(p, want) {
		t.Fatalf("acme %+v", p)
	}
	if p, _ := c.Program("side"); p.Authority != AuthorityPropose || p.Projects != nil {
		t.Fatalf("side %+v", p)
	}
	if c.ProgramOf("acme-api") != "acme" || c.ProgramOf("mini/acme-ios") != "acme" || c.ProgramOf("web") != "" {
		t.Fatal("ProgramOf")
	}
	l, ok := c.ProgramLink("web")
	if !ok || l != (ProgramLink{Program: "studio/shop", Tasks: ProgramTasksFile, Starts: true, Status: true}) {
		t.Fatalf("link %+v", l)
	}
	if _, ok := c.ProgramLink("acme-api"); ok {
		t.Fatal("acme-api has no link")
	}
	// The program keys aren't safety settings.
	if own := c.Own("web"); len(own) != 0 {
		t.Fatalf("own %v", own)
	}
	// Off: still read and checked.
	write(t, "[features]\nprograms = false\n[programs.acme]\nprojects = [\"x\", \"x\"]\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "lists x twice") {
		t.Fatalf("off: %v", err)
	}
}

func TestBadPrograms(t *testing.T) {
	for body, want := range map[string]string{
		"[programs.Acme]\n":                                                         `"Acme" is not a program slug`,
		"[programs.acme]\nprojects = [\"Acme\"]\n":                                  `"Acme" is not a project`,
		"[programs.acme]\nprojects = [\"a/b/c\"]\n":                                 `"a/b/c" is not a project`,
		"[programs.acme]\nprojects = [\"/b\"]\n":                                    `"/b" is not a project`,
		"[programs.acme]\nauthority = \"all\"\n":                                    "programs.acme.authority is \"all\"",
		"[programs.acme]\ndependency_met = \"x\"\n":                                 "programs.acme.dependency_met",
		"[programs.acme]\nparallel_threads = 100\n":                                 "programs.acme.parallel_threads is 100",
		"[programs.acme]\nparallel_threads = -1\n":                                  "programs.acme.parallel_threads is -1",
		"[programs.acme]\ndirector_agent = \"a b\"\n":                               "programs.acme.director_agent",
		"[programs.acme]\nauthorty = \"propose\"\n":                                 "unknown setting programs.acme.authorty",
		"[programs.acme]\nprojects = [\"x\"]\n[programs.b]\nprojects = [\"x\"]\n":   "x is in programs acme and b",
		"[programs.acme]\nprojects = [\"x\"]\n[projects.x]\nprogram = \"mini/b\"\n": "x is in program acme here and in mini/b",
		"[projects.x]\nprogram = \"b\"\n":                                           "projects.x.program is \"b\"",
		"[projects.x]\nprogram = \"mini/B\"\n":                                      "projects.x.program is \"mini/B\"",
		"[projects.x]\nprogram_tasks = \"auto\"\n":                                  "projects.x.program_tasks",
		"[projects.x]\nprogram_status = \"yes\"\n":                                  "program_status",
		"[projects.x]\nprogrm = \"mini/b\"\n":                                       "unknown setting projects.x.progrm",
		"[defaults]\nprogram = \"mini/b\"\n":                                        "defaults can't set program",
		"[features]\nprograms = \"yes\"\n":                                          "programs",
	} {
		write(t, body)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err %v, want %q", body, err, want)
		}
	}
}

func TestSplitMember(t *testing.T) {
	for in, want := range map[string][3]string{
		"acme-api":                     {"", "acme-api", "true"},
		"mini/acme-ios":                {"mini", "acme-ios", "true"},
		"-x":                           {"", "", "false"},
		"mini/":                        {"", "", "false"},
		strings.Repeat("a", 33) + "/x": {"", "", "false"},
	} {
		a, s, ok := SplitMember(in)
		if a != want[0] || s != want[1] || (ok != (want[2] == "true")) {
			t.Errorf("SplitMember(%q) = %q %q %v", in, a, s, ok)
		}
	}
}
