package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/vault"
)

const vaultUsage = `usage: tm vault migrate --check [--json]

What the move of front matter from TOML (+++) to YAML (---) changes: the live files
that still have TOML, per project, and any that can't move (they stay TOML, which
still reads). The move itself runs once, at the server's first start with this
version; it keeps the originals in state/migrate/ and then writes state/front-yaml.
Changes nothing. Exit codes: 0 listed, 2 usage, 3 I/O.`

func init() {
	commands["vault"] = runVault
}

// vaultFile is one row of tm vault migrate --check --json.
type vaultFile struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Error   string `json:"error,omitempty"`
}

// runVault implements tm vault (docs/SPEC.md §5.1); for now only
// migrate --check.
func runVault(e *Env, args []string) error {
	if done, err := helpOr(e, args, vaultUsage); done {
		return err
	}
	if args[0] != "migrate" {
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
