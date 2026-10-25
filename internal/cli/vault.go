package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/vault"
)

const vaultUsage = `usage: tm vault init [--yes]
       tm vault render [--project <slug>]
       tm vault doctor [--json]
       tm vault off [--yes]
       tm vault migrate --check [--json]

The projects folder is an Obsidian vault (docs/SPEC.md §5.1, Vault).

init (the human): merges tm's keys into .obsidian/ (ignore filters, links as tm
writes them, new notes in _human/, links never rewritten on a rename, the core
plugins tm's notes need, graph colours, property types, the tm.css snippet),
writes _tm/ (the Bases and Home.md) and _human/, puts reports stored before the
vault guard through it (originals kept in state/migrate/), turns the derived
notes on (state/vault) and renders them. It lists what it changes and asks first
(--yes without a terminal). A key or file you changed since tm wrote it is kept.

render (the human; a coordinator for its own project): renders the derived notes
again, and with no --project the Bases and Home.md; the ticker does it on change.

doctor (the human, a coordinator): Obsidian Sync or Publish on, link auto-update
on, plugins that rewrite notes, tm's settings changed, derived notes edited outside
tm, front matter that doesn't parse, files still TOML, reports before the guard,
and links that point at nothing: inside a project, into another project's folder
(renamed or deleted), or to a paired server's stub (_peers/<alias>/<slug>). tm
doctor shows the same under "vault" once the folder is a vault.

off (the human): turns the derived notes off and removes them and _tm/; .obsidian/,
_human/, YAML front matter and links stay. Asks first (--yes without a terminal).

migrate --check: what the move of front matter from TOML (+++) to YAML (---)
changes: the live files that still have TOML, per project, and any that can't move
(they stay TOML, which still reads). The move itself runs once, at the server's
first start with this version; it keeps the originals in state/migrate/ and then
writes state/front-yaml.

doctor and migrate change nothing. Exit codes: 0 done (doctor: nothing broken),
1 refused (doctor: a broken link or a failed check), 2 usage, 3 I/O.`

func init() {
	commands["vault"] = runVault
}

// vaultFile is one row of tm vault migrate --check --json.
type vaultFile struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Error   string `json:"error,omitempty"`
}

// runVault implements tm vault (docs/SPEC.md §5.1): init, render,
// doctor, off and migrate --check.
func runVault(e *Env, args []string) error {
	if done, err := helpOr(e, args, vaultUsage); done {
		return err
	}
	switch args[0] {
	case "doctor":
		return vaultDoctor(e, args[1:])
	case "init":
		return vaultInit(e, args[1:])
	case "render":
		return vaultRender(e, args[1:])
	case "off":
		return vaultOff(e, args[1:])
	case "migrate":
	default:
		return usagef("%s", vaultUsage)
	}
	f := newFlags()
	check, asJSON := f.Bool("check"), f.Bool("json")
	pos, err := f.Parse(args[1:])
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 || !*check {
		return usagef("%s", vaultUsage)
	}
	files, err := vault.Check()
	if err != nil && len(files) == 0 {
		return err
	}
	if *asJSON {
		rows := []vaultFile{}
		for _, f := range files {
			r := vaultFile{Project: f.Slug, Path: f.Rel}
			if f.Err != nil {
				r.Error = f.Err.Error()
			}
			rows = append(rows, r)
		}
		return e.printJSON(map[string]any{"migrated": vault.Migrated(), "files": rows})
	}
	ok := 0
	for _, f := range files {
		if f.Err == nil {
			ok++
		}
	}
	switch {
	case vault.Migrated() && len(files) == 0:
		fmt.Fprintln(e.Stdout, "front matter is YAML: the move ran (state/front-yaml) and no live file has TOML left")
	case vault.Migrated():
		fmt.Fprintf(e.Stdout, "the move ran (state/front-yaml); %d live files still have TOML front matter, which reads as before:\n", len(files))
	case ok == 0:
		fmt.Fprintln(e.Stdout, "no live file's front matter moves to YAML; the next server start only writes state/front-yaml")
	default:
		fmt.Fprintf(e.Stdout, "the next server start rewrites %d files' front matter as YAML (originals kept in state/migrate/):\n", ok)
	}
	for _, f := range files {
		if f.Err != nil {
			fmt.Fprintf(e.Stdout, "  %s/%s: stays TOML: %v\n", f.Slug, f.Rel, f.Err)
		} else {
			fmt.Fprintf(e.Stdout, "  %s/%s\n", f.Slug, f.Rel)
		}
	}
	if err != nil {
		fmt.Fprintf(e.Stderr, "tm vault: %v\n", err)
	}
	return nil
}
