package server

import (
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
)

// The programs as the sidebars show them (the programs design §11.1):
// each program on this server, its settings and its members in order,
// above the projects. projects.watch sends them while [features]
// programs is on, and nothing about them while it is off, so an older
// console, or one with programs off, draws the members as standalone
// projects.

// SideProgram is one program as the sidebar shows it.
type SideProgram struct {
	Slug string `json:"slug"`
	// Projects are its members in the order its table lists them: a
	// project's slug, or <alias>/<slug> on a paired server.
	Projects []string `json:"projects,omitempty"`
	// Authority is propose or delegate; ParallelThreads its thread
	// cap, 0 for none; Paused refuses routes and starts.
	Authority       string `json:"authority"`
	ParallelThreads int    `json:"parallel_threads,omitempty"`
	Paused          bool   `json:"paused,omitempty"`
	// Goal is PROGRAM.md's; Questions counts the program's open
	// questions, Unread its unhandled inbox items.
	Goal      string `json:"goal,omitempty"`
	Questions int    `json:"questions,omitempty"`
	Unread    int    `json:"unread,omitempty"`
	// Error says why the program's folder doesn't read: its director
	// can't start until it does.
	Error string `json:"error,omitempty"`
}

// SidePrograms are cfg's programs as the sidebars show them, in slug
// order: none while programs are off. A program whose name a project
// has too is left out (the project wins, §3.4), and its members are
// drawn as standalone projects.
func SidePrograms(cfg *config.Config) []SideProgram {
	if !cfg.ProgramsOn() {
		return nil
	}
	var out []SideProgram
	for _, slug := range cfg.ProgramSlugs() {
		if project.ProgramLeftOut(slug) {
			continue
		}
		t, _ := cfg.Program(slug)
		sp := SideProgram{Slug: slug, Projects: t.Projects, Authority: t.Authority, ParallelThreads: t.ParallelThreads, Paused: t.Paused}
		g, err := program.Open(slug, cfg)
		switch { // a folder that doesn't read still shows, so its settings can be reached
		case err != nil:
			sp.Error = err.Error()
		default:
			sp.Goal = g.Meta.Goal
			if qs, err := g.Questions(); err == nil {
				sp.Questions = len(qs)
			}
			if items, err := g.Inbox(); err == nil {
				sp.Unread = len(items)
			}
		}
		out = append(out, sp)
	}
	return out
}
