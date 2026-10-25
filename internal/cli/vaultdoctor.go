package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// vaultDoctor implements tm vault doctor: for now the cross-project
// links (vault.CrossLinks).
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
	broken, err := vault.CrossLinks()
	if err != nil {
		return err
	}
	if *asJSON {
		if broken == nil {
			broken = []vault.Broken{}
		}
		if err := e.printJSON(map[string]any{"links": broken}); err != nil {
			return err
		}
	} else if len(broken) == 0 {
		fmt.Fprintln(e.Stdout, "no broken link between projects or to a paired server's stub")
	} else {
		fmt.Fprintf(e.Stdout, "%d links point at nothing:\n", len(broken))
		for _, b := range broken {
			fmt.Fprintf(e.Stdout, "  %s:%d [[%s]]: %s\n", textsafe.Line(b.File, 0), b.Line, textsafe.Line(b.Target, 0), b.Why)
		}
	}
	if len(broken) > 0 {
		return &exitError{ExitRefused}
	}
	return nil
}
