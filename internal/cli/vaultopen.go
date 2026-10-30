package cli

import (
	"fmt"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/plat/open"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vault"
)

// tm vault open (T874): launches Obsidian at the vault through an
// obsidian:// address, adding the vault to Obsidian's own list first only
// on the user's yes and while Obsidian is closed (vault.Register).

// The seams tests replace: no test launches an app or reads this
// machine's processes.
var (
	launchObsidian  = open.Obsidian
	obsidianRunning = vault.ObsidianRunning
)

// vaultOpen implements tm vault open [--register]: the human's, since it
// launches an app and may edit that app's settings.
func vaultOpen(e *Env, args []string) error {
	f := newFlags()
	register := f.Bool("register")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 {
		return usagef("%s", vaultUsage)
	}
	return openVault(e, *register)
}

// openVault opens the vault in Obsidian, registering it with Obsidian on
// register or the user's yes at the prompt.
func openVault(e *Env, register bool) error {
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "the human opens Obsidian, in their own terminal; an agent never launches apps or edits their settings"}
	}
	root, err := home.ProjectsDir()
	if err != nil {
		return err
	}
	path, err := vault.VaultPath()
	if err != nil {
		return err
	}
	cfg := vault.ObsidianConfig()
	if cfg == "" {
		fmt.Fprintln(e.Stdout, "Obsidian's vault list (obsidian.json) isn't here: install Obsidian and start it once.")
		oneTimeSteps(e, path)
		return nil
	}
	id, err := vault.KnownVault(cfg, root)
	if err != nil {
		return err
	}
	if id == "" {
		fmt.Fprintf(e.Stdout, "Obsidian doesn't know the vault yet: tm can add %s to its vault list (%s), changing nothing else, after a backup beside it.\n", tilde(path), textsafe.Line(cfg, 0))
		switch {
		case register:
		case !stdinTTY(e):
			fmt.Fprintln(e.Stdout, "tm vault open --register adds it; or once, in Obsidian:")
			oneTimeSteps(e, path)
			return nil
		case !ask(e, "Add it? [y/N] "):
			fmt.Fprintln(e.Stdout, "Left Obsidian's settings alone. Once, in Obsidian:")
			oneTimeSteps(e, path)
			return nil
		}
		running, err := obsidianRunning()
		if err != nil || running {
			why := "Obsidian is running and would write its list over tm's"
			if err != nil {
				why = fmt.Sprintf("tm can't tell whether Obsidian is running (%v)", err)
			}
			return &project.Error{Code: "obsidian-running", Msg: why + ": quit Obsidian and run tm vault open --register again, or open " + tilde(path) + " in it yourself (Open folder as vault)"}
		}
		var backup string
		if id, backup, err = vault.Register(cfg, path, time.Now()); err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "Added %s to Obsidian's vault list (backup %s).\n", tilde(path), textsafe.Line(backup, 0))
	}
	if err := launchObsidian(vault.OpenURI(id)); err != nil {
		return fmt.Errorf("launching Obsidian: %w", err)
	}
	fmt.Fprintf(e.Stdout, "Opening %s in Obsidian.\n", tilde(path))
	return nil
}

// oneTimeSteps prints how to open the vault in Obsidian by hand.
func oneTimeSteps(e *Env, path string) {
	fmt.Fprintf(e.Stdout, "  1. In Obsidian's vault switcher, choose Open folder as vault.\n  2. Pick %s (it is %s).\n", tilde(path), textsafe.Line(path, 0))
}
