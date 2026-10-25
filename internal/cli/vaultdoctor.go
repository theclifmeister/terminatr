package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"

	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// vaultDoctor implements tm vault doctor: the vault's health
// (vault.Health: Obsidian's settings, tm's notes edited outside tm,
// front matter, reports before the guard) and the links that point at
// nothing, between projects and inside each (vault.Links). It exits 1
// for a broken link or a failed check; warnings don't count.
func vaultDoctor(e *Env, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 {
		return usagef("%s", vaultUsage)
	}
	if e.Caller.Kind != caller.Human && e.Caller.Kind != caller.Coordinator {
		return &project.Error{Code: "refused", Msg: "the human and the coordinator check the vault"}
	}
	broken, err := vault.Links()
	if err != nil {
		return err
	}
	checks := vault.Health()
	failed := len(broken) > 0
	for _, c := range checks {
		failed = failed || c.Status == "fail"
	}
	if *asJSON {
		if broken == nil {
			broken = []vault.Broken{}
		}
		if err := e.printJSON(map[string]any{"checks": checks, "links": broken}); err != nil {
			return err
		}
	} else if vaultHealth(e, checks); len(broken) == 0 {
		fmt.Fprintln(e.Stdout, "no broken link inside a project, between projects or to a paired server's stub")
	} else {
		fmt.Fprintf(e.Stdout, "%d links point at nothing:\n", len(broken))
		for _, b := range broken {
			fmt.Fprintf(e.Stdout, "  %s:%d [[%s]]: %s\n", textsafe.Line(b.File, 0), b.Line, textsafe.Line(b.Target, 0), b.Why)
		}
	}
	if failed {
		return &exitError{ExitRefused}
	}
	return nil
}

// vaultHealth prints vault.Health's findings, each with its files.
func vaultHealth(e *Env, fs []vault.Finding) {
	for _, f := range fs {
		fmt.Fprintf(e.Stdout, "%-4s %-17s %s\n", f.Status, f.Name, f.Detail)
		for _, file := range f.Files {
			fmt.Fprintf(e.Stdout, "       %s\n", textsafe.Line(file, 0))
		}
	}
}
