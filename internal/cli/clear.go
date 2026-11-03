package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const clearUsage = `usage: tm clear [--project <slug> | --program <slug>] [--json]

Save and clear the project's coordinator, as auto_clear does: at its next idle moment
it is asked to save what it learned, and once it rested after that its conversation
is cleared (its agent's clear prompt). The human's and the coordinator's.
--program does the same for the program's director (programs on only): the human's
and that director's; a director's tm clear names its own program.
Exit codes: 0 asked, 1 refused (no coordinator, or its agent has no clear), 2 usage, 3 I/O.`

// runClear is the user's save and clear of a coordinator (docs/SPEC.md
// §7.5, Auto-clear): the context hint's key and /saveclear in the
// coordinator's pane run it too. The server has the ticker take
// auto-clear's steps (coordinator.clear).
func runClear(e *Env, args []string) error {
	if len(args) > 0 && args[0] == "help" {
		return &helpError{}
	}
	f := newFlags()
	slug, prog, asJSON := f.String("project"), f.String("program"), f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *slug != "" && *prog != "" {
		return usagef("%s", clearUsage)
	}
	if *prog != "" || e.Caller.Kind == caller.Director {
		return clearDirector(e, *prog, *asJSON)
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	if err := e.coordinatorOnly(p, "tm clear"); err != nil {
		return err
	}
	var res proto.CoordinatorClearResult
	if err := e.call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Project: p.Slug}, &res); err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(res)
	}
	fmt.Fprintf(e.Stdout, "asked the coordinator (%s) to save what it learned at its next idle moment; its conversation is cleared once it rested after that\n", res.Session)
	return nil
}

// clearDirector is tm clear --program: the save and clear of program
// slug's director, the human's or that director's own (the programs
// design §4.1). The server refuses programs-off.
func clearDirector(e *Env, slug string, asJSON bool) error {
	switch e.Caller.Kind {
	case caller.Director:
		if slug != "" && slug != e.Caller.Project {
			return &project.Error{Code: "other-program", Msg: "a director clears only itself"}
		}
		slug = e.Caller.Project
	case caller.Human:
	default:
		return &project.Error{Code: "director-only", Msg: "tm clear --program is the director's (and the human's)"}
	}
	var res proto.CoordinatorClearResult
	if err := e.call(proto.MethodCoordinatorClear, proto.CoordinatorClearParams{Program: slug}, &res); err != nil {
		return err
	}
	if asJSON {
		return e.printJSON(res)
	}
	fmt.Fprintf(e.Stdout, "asked the director (%s) to save what it learned at its next idle moment; its conversation is cleared once it rested after that\n", res.Session)
	return nil
}
