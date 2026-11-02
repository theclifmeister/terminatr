package project

import (
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
)

// Programs and projects share one namespace (the programs design §3.4):
// a name is a project or a program, never both, so a bare "acme" in
// "acme/T4", a link or the vault means one thing. A program exists when
// config.toml has its [programs.<slug>] table or its folder is under
// home.ProgramsDir, whether or not programs are on: a dormant program
// keeps its name.

// ProgramTaken says why slug can't name a new project, or a paired
// server, here: a program has it. "" when none does. cfg is config.Load's
// (nil reads it).
func ProgramTaken(slug string, cfg *config.Config) string {
	if !programExists(slug, cfg) {
		return ""
	}
	return slug + " is a program (tm program delete " + slug + " removes it)"
}

func programExists(slug string, cfg *config.Config) bool {
	if cfg == nil {
		cfg, _ = config.Load() // a safety error still gives the tables
	}
	if _, ok := cfg.Program(slug); ok {
		return true
	}
	if !ValidSlug(slug) {
		return false
	}
	d, err := home.ProgramsDir()
	if err != nil {
		return false
	}
	fi, err := os.Stat(filepath.Join(d, slug))
	return err == nil && fi.IsDir()
}

// ProgramSlugs lists, sorted, the programs here: the [programs.<slug>]
// tables and the folders under home.ProgramsDir, valid slugs only.
func ProgramSlugs(cfg *config.Config) []string {
	out := cfg.ProgramSlugs()
	if d, err := home.ProgramsDir(); err == nil {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			if e.IsDir() && ValidSlug(e.Name()) && !slices.Contains(out, e.Name()) {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// ProgramClash is a program whose slug something else here has too: a
// project, or a paired server's alias or name. The project, or the
// paired server, wins; the program is left out until one is renamed.
type ProgramClash struct {
	Program string
	With    string // "a project", "a paired server"
}

// ProgramClashes lists the programs that clash with a project here, or
// with one of names, the paired servers' aliases and names. Only a hand
// edit makes one: every command that names a project, a program or a
// paired server refuses a taken name.
func ProgramClashes(cfg *config.Config, names []string) []ProgramClash {
	var out []ProgramClash
	for _, slug := range ProgramSlugs(cfg) {
		switch {
		case projectExists(slug):
			out = append(out, ProgramClash{Program: slug, With: "a project"})
		case slices.Contains(names, slug):
			out = append(out, ProgramClash{Program: slug, With: "a paired server"})
		}
	}
	return out
}

// ProgramLeftOut reports whether program slug clashes with a project
// here, which wins (ProgramClashes): such a program's director is not
// started and nothing routes to it.
func ProgramLeftOut(slug string) bool { return projectExists(slug) }

func projectExists(slug string) bool {
	dir, err := Dir(slug)
	if err != nil {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
