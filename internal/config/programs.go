package config

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Programs (docs: the programs design, §3.3, §3.5, §9.1): a program
// groups projects under a director. This file reads what config.toml
// says about them, whatever [features] programs says, so a file a newer
// tm wrote always loads; nothing here writes them yet, and nothing acts
// on them while the switch is off.
//
//	[features]
//	programs = true                 # the server-wide switch; absent = off
//
//	[programs.acme]                 # a program on this server
//	projects  = ["acme-api", "mini/acme-ios"]
//	authority = "propose"           # or "delegate"
//	…
//
//	[projects.acme-ios]             # this project's program on another server
//	program        = "studio/acme"
//	program_tasks  = "file"         # or "propose"
//	program_starts = false
//	program_status = true

// Values of a program's authority.
const (
	AuthorityPropose  = "propose" // the director proposes; the user starts
	AuthorityDelegate = "delegate"
)

// Values of a program's dependency_met: when a task another task comes
// after counts as met.
const (
	DependencyMerged = "merged" // its PR merged, or it is done
	DependencyDone   = "done"   // the user accepted it
)

// Values of a project's program_tasks: what a paired server's director
// filing a task here does.
const (
	ProgramTasksFile    = "file"    // it lands on the board
	ProgramTasksPropose = "propose" // it arrives as an item the coordinator proposes
)

// ProgramsOff is the refusal code of every program path while
// [features] programs is off.
const ProgramsOff = "programs-off"

// ProgramsDirName is the folder under projects/ that holds the programs
// (home.ProgramsDir): not a slug, so no project can be called it.
const ProgramsDirName = "_programs"

// Features is the [features] table: server-wide opt-ins.
type Features struct {
	// Programs turns programs on for this server. Off unless set true.
	Programs bool
}

type rawFeatures struct {
	Programs *bool `toml:"programs"`
}

// Program is one [programs.<slug>] table, defaults filled in.
type Program struct {
	Slug string
	// Projects are its members, in the order the dashboard shows them:
	// a project's slug here, or <alias>/<slug> on a paired server.
	Projects        []string
	Authority       string
	ParallelThreads int // 0: no program cap
	DependencyMet   string
	DirectorAgent   string // "": [defaults] coordinator_agent
	DirectorModel   string
	AutoClear       bool
	RemoteControl   bool
	AnswerFacts     bool
	SearchChildren  bool
	Paused          bool
}

type rawProgram struct {
	Projects        *[]string `toml:"projects"`
	Authority       *string   `toml:"authority"`
	ParallelThreads *int      `toml:"parallel_threads"`
	DependencyMet   *string   `toml:"dependency_met"`
	DirectorAgent   *string   `toml:"director_agent"`
	DirectorModel   *string   `toml:"director_model"`
	AutoClear       *bool     `toml:"auto_clear"`
	RemoteControl   *bool     `toml:"remote_control"`
	AnswerFacts     *bool     `toml:"answer_facts"`
	SearchChildren  *bool     `toml:"search_children"`
	Paused          *bool     `toml:"paused"`
}

// ProgramLink is a project's membership of a program on a paired server,
// its [projects.<slug>] program keys, defaults filled in.
type ProgramLink struct {
	Program string // "<alias>/<program>"
	Tasks   string // ProgramTasksFile or ProgramTasksPropose
	Starts  bool
	Status  bool
}

// programSlugRE is a program's slug: a project slug's rule, since the
// two share one namespace.
var programSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// aliasRE is a paired server's alias (peer.CheckAlias).
var aliasRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidProgramSlug reports whether s can name a program.
func ValidProgramSlug(s string) bool { return programSlugRE.MatchString(s) }

// SplitMember reads a program member: "acme-api" (alias "") or, on a
// paired server, "mini/acme-ios".
func SplitMember(m string) (alias, slug string, ok bool) {
	alias, slug, remote := strings.Cut(m, "/")
	if !remote {
		alias, slug = "", m
	}
	if remote && !aliasRE.MatchString(alias) || !programSlugRE.MatchString(slug) {
		return "", "", false
	}
	return alias, slug, true
}

// ProgramSlugs lists, sorted, the programs the file has a table for,
// whatever the switch says.
func (c *Config) ProgramSlugs() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.programs))
	for slug := range c.programs {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// Program returns program slug's table, defaults filled in, and whether
// the file has it.
func (c *Config) Program(slug string) (Program, bool) {
	if c == nil {
		return Program{}, false
	}
	r, ok := c.programs[slug]
	if !ok {
		return Program{}, false
	}
	return r.program(slug), true
}

// ProgramOf is the program on this server that lists member (a slug, or
// <alias>/<slug>), "" for none.
func (c *Config) ProgramOf(member string) string {
	for _, slug := range c.ProgramSlugs() {
		if r := c.programs[slug]; r.Projects != nil && slices.Contains(*r.Projects, member) {
			return slug
		}
	}
	return ""
}

// ProgramLink returns project slug's membership of a paired server's
// program, and whether it has one.
func (c *Config) ProgramLink(slug string) (ProgramLink, bool) {
	if c == nil {
		return ProgramLink{}, false
	}
	r, ok := c.projects[slug]
	if !ok || r.Program == nil || *r.Program == "" {
		return ProgramLink{}, false
	}
	l := ProgramLink{Program: *r.Program, Tasks: ProgramTasksFile, Status: true}
	if r.ProgramTasks != nil {
		l.Tasks = *r.ProgramTasks
	}
	if r.ProgramStarts != nil {
		l.Starts = *r.ProgramStarts
	}
	if r.ProgramStatus != nil {
		l.Status = *r.ProgramStatus
	}
	return l, true
}

func (r rawProgram) program(slug string) Program {
	p := Program{Slug: slug, Authority: AuthorityPropose, DependencyMet: DependencyMerged,
		AutoClear: true, AnswerFacts: true, SearchChildren: true}
	if r.Projects != nil {
		p.Projects = slices.Clone(*r.Projects)
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setB := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	set(&p.Authority, r.Authority)
	set(&p.DependencyMet, r.DependencyMet)
	set(&p.DirectorAgent, r.DirectorAgent)
	set(&p.DirectorModel, r.DirectorModel)
	if r.ParallelThreads != nil {
		p.ParallelThreads = *r.ParallelThreads
	}
	setB(&p.AutoClear, r.AutoClear)
	setB(&p.RemoteControl, r.RemoteControl)
	setB(&p.AnswerFacts, r.AnswerFacts)
	setB(&p.SearchChildren, r.SearchChildren)
	setB(&p.Paused, r.Paused)
	return p
}

// checkPrograms checks the [programs.*] tables and the projects'
// program keys, whatever the switch says (§3.3): slugs, member forms,
// values, and that a project is in at most one program. A clash of a
// program's slug with a project or a paired server is not an error here
// (config doesn't see those): project.ProgramClash reports it, and the
// project wins.
func (c *Config) checkPrograms() error {
	if c.defaults.Program != nil || c.defaults.ProgramTasks != nil || c.defaults.ProgramStarts != nil || c.defaults.ProgramStatus != nil {
		return fmt.Errorf("%s: defaults can't set program, program_tasks, program_starts or program_status; they are each project's own", c.Path)
	}
	in := map[string]string{} // member → program
	for _, slug := range c.ProgramSlugs() {
		r := c.programs[slug]
		if !ValidProgramSlug(slug) {
			return fmt.Errorf("%s: programs.%s: %q is not a program slug (a-z, 0-9 and -, lower case)", c.Path, slug, slug)
		}
		at := func(key string) string { return fmt.Sprintf("%s: programs.%s.%s", c.Path, slug, key) }
		if r.Projects != nil {
			for _, m := range *r.Projects {
				alias, ps, ok := SplitMember(m)
				if !ok {
					return fmt.Errorf("%s: %q is not a project (<slug>, or <server>/<slug> on a paired server)", at("projects"), m)
				}
				if prev, dup := in[m]; dup {
					if prev == slug {
						return fmt.Errorf("%s lists %s twice", at("projects"), m)
					}
					return fmt.Errorf("%s: %s is in programs %s and %s; a project belongs to at most one program", c.Path, m, prev, slug)
				}
				in[m] = slug
				if alias == "" {
					if l, ok := c.ProgramLink(ps); ok {
						return fmt.Errorf("%s: %s is in program %s here and in %s; a project belongs to at most one program", c.Path, ps, slug, l.Program)
					}
				}
			}
		}
		if r.Authority != nil && *r.Authority != AuthorityPropose && *r.Authority != AuthorityDelegate {
			return fmt.Errorf("%s is %q: %s or %s", at("authority"), *r.Authority, AuthorityPropose, AuthorityDelegate)
		}
		if r.DependencyMet != nil && *r.DependencyMet != DependencyMerged && *r.DependencyMet != DependencyDone {
			return fmt.Errorf("%s is %q: %s or %s", at("dependency_met"), *r.DependencyMet, DependencyMerged, DependencyDone)
		}
		if r.ParallelThreads != nil && (*r.ParallelThreads < 0 || *r.ParallelThreads > MaxParallelThreads) {
			return fmt.Errorf("%s is %d: 0 (no program cap) to %d", at("parallel_threads"), *r.ParallelThreads, MaxParallelThreads)
		}
		if r.DirectorAgent != nil && *r.DirectorAgent != "" {
			if err := CheckAgent(*r.DirectorAgent); err != nil {
				return fmt.Errorf("%s %w", at("director_agent"), err)
			}
		}
		if r.DirectorModel != nil && *r.DirectorModel != "" {
			if err := CheckModels([]string{*r.DirectorModel}); err != nil {
				return fmt.Errorf("%s %w", at("director_model"), err)
			}
		}
	}
	for _, slug := range c.ProjectSlugs() {
		r := c.projects[slug]
		at := func(key string) string { return fmt.Sprintf("%s: projects.%s.%s", c.Path, slug, key) }
		if r.Program != nil && *r.Program != "" {
			if alias, ps, ok := SplitMember(*r.Program); !ok || alias == "" {
				return fmt.Errorf("%s is %q: a paired server's program, <server>/<program>", at("program"), *r.Program)
			} else if !ValidProgramSlug(ps) {
				return fmt.Errorf("%s is %q: %q is not a program slug", at("program"), *r.Program, ps)
			}
		}
		if r.ProgramTasks != nil && *r.ProgramTasks != ProgramTasksFile && *r.ProgramTasks != ProgramTasksPropose {
			return fmt.Errorf("%s is %q: %s or %s", at("program_tasks"), *r.ProgramTasks, ProgramTasksFile, ProgramTasksPropose)
		}
	}
	return nil
}

// RemoveProgram drops program slug's [programs.<slug>] table from the
// settings file, its members' membership with it: tm program delete,
// the human's. Comments and the other tables stay as they were. ErrForm
// when the file sets it in a form the line editor doesn't change (a
// dotted key, an inline table).
func RemoveProgram(slug string) error {
	return edit(func(data []byte) ([]byte, error) {
		if data == nil {
			return nil, nil
		}
		return DropTable(data, "programs."+slug)
	})
}
