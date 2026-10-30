package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vault"
	"github.com/theclifmeister/terminatr/internal/version"
	"github.com/theclifmeister/terminatr/obsidian"
)

// tm vault init and off (T720, docs/SPEC.md §5.1, Vault); render is in
// vault.go.

// vaultInit implements tm vault init [--plugin] [--yes]: the human's,
// since it turns on files in every project and edits Obsidian's
// settings. --plugin also installs the Obsidian plugin (obsidian/),
// bundled from the sources tm embeds.
func vaultInit(e *Env, args []string) error {
	f := newFlags()
	yes, plugin := f.Bool("yes"), f.Bool("plugin")
	linkAt, noLink := f.String("link"), f.Bool("no-link")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 || *noLink && *linkAt != "" {
		return usagef("%s", vaultUsage)
	}
	if e.Caller.Kind != caller.Human {
		return &project.Error{Code: "human-only", Msg: "the human turns the vault on, in their own terminal"}
	}
	pl, err := vault.NewPlan()
	if err != nil {
		return err
	}
	if !*noLink {
		path := *linkAt
		if path == "" {
			path, err = vault.DefaultLink()
		} else {
			path, err = filepath.Abs(path)
		}
		if err != nil {
			return err
		}
		pl.PlanLink(path)
	}
	if *plugin {
		files, err := obsidian.Files(version.Version)
		if err != nil {
			return fmt.Errorf("the Obsidian plugin: %w", err)
		}
		exe, _ := os.Executable()
		if err := pl.Plugin(files, exe); err != nil {
			return err
		}
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
	if lp := pl.Link(); lp != nil {
		switch lp.What {
		case "new":
			fmt.Fprintf(e.Stdout, "  %s: new link to the vault\n", lp.Path)
		case "ours":
			fmt.Fprintf(e.Stdout, "  %s: already a link to the vault; keeps it\n", lp.Path)
		default:
			fmt.Fprintf(e.Stdout, "  %s: is %s; keeps it and makes no link (--link PATH picks another place)\n", textsafe.Line(lp.Path, 0), lp.Why)
		}
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
	open := pl.Root
	if lp := pl.Link(); lp != nil && lp.What != "kept" {
		open = lp.Path
	}
	fmt.Fprintf(e.Stdout, "Open %s as a vault in Obsidian (Open folder as vault); tm vault doctor checks it.\n", tilde(open))
	if *plugin {
		fmt.Fprintln(e.Stdout, "The NEEDS YOU pane is in .obsidian/plugins/terminatr: turn it on in Obsidian (Settings › Community plugins › terminatr); tm turns on no plugin for you.")
	}
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
	fmt.Fprintln(e.Stdout, "tm vault off removes every task and thread note tm rendered and _tm/ (the Bases, Home.md); .obsidian/, _human/, YAML and links stay. A link init made to the vault goes too, while it is still ours.")
	if err := e.confirm(*yes, "tm vault off", "Go ahead?"); err != nil {
		return err
	}
	link := vault.RecordedLink()
	n, err := vault.Off(e.Caller)
	if link != "" {
		if _, lerr := os.Lstat(link); lerr != nil {
			fmt.Fprintf(e.Stdout, "link %s removed\n", textsafe.Line(link, 0))
		} else {
			fmt.Fprintf(e.Stdout, "link %s is no longer ours (changed since init): left alone\n", textsafe.Line(link, 0))
		}
	}
	fmt.Fprintf(e.Stdout, "vault off: %d derived notes removed\n", n)
	return err
}

// tilde writes a path under the home folder as ~/…, as the docs name the
// link.
func tilde(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~/" + filepath.ToSlash(p[len(h)+1:])
	}
	return p
}
