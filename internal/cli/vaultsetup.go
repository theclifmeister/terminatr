package cli

import (
	"fmt"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// tm vault init, render and off (T720, docs/SPEC.md §5.1, Vault).

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

// vaultRender implements tm vault render [--project P]: the repair, for
// the human and a coordinator (its own project).
func vaultRender(e *Env, args []string) error {
	f := newFlags()
	slug := f.String("project", "")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 {
		return usagef("%s", vaultUsage)
	}
	switch e.Caller.Kind {
	case caller.Human:
	case caller.Coordinator:
		if *slug == "" {
			*slug = e.Caller.Project
		}
		if *slug != e.Caller.Project {
			return &project.Error{Code: "not-your-project", Msg: "a coordinator renders its own project's notes"}
		}
	default:
		return &project.Error{Code: "refused", Msg: "the human and the coordinator render the vault"}
	}
	if !vault.On() {
		return &project.Error{Code: "vault-off", Msg: "the derived notes are off (or front matter is still TOML): the human turns them on with tm vault init"}
	}
	var ps []*project.Project
	if *slug != "" {
		p, err := project.Open(*slug)
		if err != nil {
			return err
		}
		ps = append(ps, p)
	} else {
		list, err := project.List()
		if err != nil {
			return err
		}
		for _, s := range list {
			if p, err := project.Open(s.Slug); err == nil && s.Error == "" {
				ps = append(ps, p)
			}
		}
	}
	var failed error
	for _, p := range ps {
		w, r, err := vault.Render(p)
		if err != nil {
			fmt.Fprintf(e.Stderr, "tm vault render: %s: %v\n", p.Slug, err)
			failed = &exitError{ExitRefused}
		}
		fmt.Fprintf(e.Stdout, "%s: %d notes written, %d removed\n", p.Slug, w, r)
	}
	if *slug == "" {
		cs, err := vault.RenderShared()
		if err != nil {
			return err
		}
		for _, c := range cs {
			if c.What != "" {
				fmt.Fprintf(e.Stdout, "%s: %s\n", c.Rel, c.What)
			} else {
				fmt.Fprintf(e.Stdout, "%s: keeps %s\n", c.Rel, c.Kept)
			}
		}
	}
	return failed
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

// vaultHealth prints vault.Health's findings, for tm vault doctor; it
// reports whether any is worse than ok.
func vaultHealth(e *Env, fs []vault.Finding) (bad bool) {
	for _, f := range fs {
		mark := "ok  "
		if f.Status != "ok" {
			mark, bad = f.Status+"", true
		}
		fmt.Fprintf(e.Stdout, "%-4s %-17s %s\n", mark, f.Name, f.Detail)
		for _, file := range f.Files {
			fmt.Fprintf(e.Stdout, "       %s\n", file)
		}
	}
	return bad
}
