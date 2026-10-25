package cli

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/vault"
)

const vaultUsage = `usage: tm vault migrate --check [--json]
       tm vault render [--days N] [--json]
       tm vault doctor [--json]

migrate --check: what the move of front matter from TOML (+++) to YAML (---) changes: the live files
that still have TOML, per project, and any that can't move (they stay TOML, which
still reads). The move itself runs once, at the server's first start with this
version; it keeps the originals in state/migrate/ and then writes state/front-yaml.
Changes nothing.

render: writes the vault's derived notes now, as the ticker does when what they come
from changes: every project's task and thread notes, and the daily notes
(_tm/daily/<date>.md, local days) of the last N days, today included (default 2, at
most 3660), from JOURNAL.md and the archives in journal/. A note is written only when
it changed. Only once the vault is on (state/vault) and front matter is YAML.

doctor: links from one project's notes into another project's folder, or to a
paired server's stub (_peers/<alias>/<slug>), that point at nothing: a project
renamed or deleted, a server unpaired, a project no longer shared. Changes nothing.

Exit codes: 0 done (doctor: nothing found), 1 refused (doctor: found something),
2 usage, 3 I/O.`

func init() {
	commands["vault"] = runVault
}

// vaultFile is one row of tm vault migrate --check --json.
type vaultFile struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Error   string `json:"error,omitempty"`
}

// runVault implements tm vault (docs/SPEC.md §5.1): migrate --check
// render and doctor.
func runVault(e *Env, args []string) error {
	if done, err := helpOr(e, args, vaultUsage); done {
		return err
	}
	switch args[0] {
	case "render":
		return vaultRender(e, args[1:])
	case "doctor":
		return vaultDoctor(e, args[1:])
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

// vaultRender implements tm vault render [--days N] [--json].
func vaultRender(e *Env, args []string) error {
	f := newFlags()
	days, asJSON := f.String("days"), f.Bool("json")
	pos, err := f.Parse(args)
	if isHelp(err) {
		return err
	}
	if err != nil || len(pos) > 0 {
		return usagef("%s", vaultUsage)
	}
	n := vault.TickerDays
	if *days != "" {
		if n, err = strconv.Atoi(*days); err != nil || n < 1 || n > vault.MaxDays {
			return usagef("--days takes a number from 1 to %d", vault.MaxDays)
		}
	}
	if e.Caller.Kind == caller.Thread {
		return &tasks.Error{Code: "coordinator-only", Msg: "tm vault render is the coordinator's or the user's"}
	}
	if !vault.On() {
		return &tasks.Error{Code: "vault-off", Msg: "the vault's derived notes are off: they render once state/vault exists and front matter is YAML (state/front-yaml)"}
	}
	list, err := project.List()
	if err != nil {
		return err
	}
	written, removed := 0, 0
	var errs []error
	for _, s := range list {
		p, err := project.Open(s.Slug)
		if err == nil {
			var w, r int
			w, r, err = vault.Render(p)
			written, removed = written+w, removed+r
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Slug, err))
		}
	}
	daily, err := vault.RenderDaily(time.Now(), n)
	if err != nil {
		errs = append(errs, err)
	}
	if *asJSON {
		if err := e.printJSON(map[string]any{"notes_written": written, "notes_removed": removed, "daily_written": daily, "days": n}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(e.Stdout, "task and thread notes: %d written, %d removed; daily notes of the last %d days: %d written (the rest unchanged or without journal lines)\n", written, removed, n, daily)
	}
	return errors.Join(errs...)
}
