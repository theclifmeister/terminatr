package cli

import (
	"fmt"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// tm vault init and off (T720, docs/SPEC.md §5.1, Vault); render is in
// vault.go.

// vaultInit implements tm vault init [--yes]: the human's, since it turns
// on files in every project and edits Obsidian's settings.
func vaultInit(e *Env, args []string) error {
	f := newFlags()
	yes := f.Bool("yes")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 {
		return usagef("%s", vaultUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "the human turns the vault on, in their own terminal"}
	}
	pl, err := vault.NewPlan()
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "tm vault init in %s:\n", pl.Root)
	for _, c := range pl.Changes {
		switch {
		case c.What != "" && c.Kept != "":
			fmt.Fprintf(e.Stdout, "  %s: %s; keeps %s\n", c.Rel, c.What, c.Kept)
		case c.What != "":
			fmt.Fprintf(e.Stdout, "  %s: %s\n", c.Rel, c.What)
		default:
			fmt.Fprintf(e.Stdout, "  %s: keeps %s\n", c.Rel, c.Kept)
		}
	}
	fmt.Fprintln(e.Stdout, "  _human/: the folder your new notes go in; tm never writes there")
	if n := len(pl.Reports); n > 0 {
		fmt.Fprintf(e.Stdout, "  %d reports stored before the vault guard: through it (originals kept in state/migrate/)\n", n)
	}
	fmt.Fprintln(e.Stdout, "  derived notes on: a note per task and per thread, rendered on each change")
	if err := e.confirm(*yes, "tm vault init", "Go ahead?"); err != nil {
		return err
	}
	n, err := vault.Init(pl, e.Caller, time.Now())
	if !vault.On() {
		fmt.Fprintln(e.Stdout, "vault on; derived notes render once front matter is YAML (the server's next start moves it)")
	} else {
		fmt.Fprintf(e.Stdout, "vault on: %d derived notes written\n", n)
	}
	fmt.Fprintf(e.Stdout, "Open %s as a vault in Obsidian (Open folder as vault); tm vault doctor checks it.\n", pl.Root)
	return err
}

// vaultOff implements tm vault off [--yes]: the human's.
func vaultOff(e *Env, args []string) error {
	f := newFlags()
	yes := f.Bool("yes")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 {
		return usagef("%s", vaultUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "the human turns the vault off, in their own terminal"}
	}
	fmt.Fprintln(e.Stdout, "tm vault off removes every task and thread note tm rendered and _tm/ (the Bases, Home.md); .obsidian/, _human/, YAML and links stay.")
	if err := e.confirm(*yes, "tm vault off", "Go ahead?"); err != nil {
		return err
	}
	n, err := vault.Off(e.Caller)
	fmt.Fprintf(e.Stdout, "vault off: %d derived notes removed\n", n)
	return err
}
