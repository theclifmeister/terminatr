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

const vaultUsage = `usage: tm vault init [--yes]
       tm vault render [--project <slug>] [--days N] [--json]
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

render (the human, a coordinator; not a thread): writes the derived notes now, as
the ticker does when what they come from changes: the task and thread notes of
--project's project (a coordinator's own by default; every project's for the human),
and the daily notes (_tm/daily/<date>.md, local days) of the last N days, today
included (--days, default 2, at most 3660), from JOURNAL.md and the archives in
journal/. The human's render with no --project also writes _tm/'s Bases and
Home.md, keeping one you edited. A note is written only when it changed. Only once
the vault is on (state/vault) and front matter is YAML.

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
	case "render":
		return vaultRender(e, args[1:])
	case "doctor":
		return vaultDoctor(e, args[1:])
	case "init":
		return vaultInit(e, args[1:])
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

// vaultRender implements tm vault render [--project <slug>] [--days N]
// [--json]: the task and thread notes of one project or all, the daily
// notes of the last N days, and with the human's whole render _tm/'s
// Bases and Home.md (vault.RenderShared).
func vaultRender(e *Env, args []string) error {
	f := newFlags()
	slug, days, asJSON := f.String("project"), f.String("days"), f.Bool("json")
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
	switch e.Caller.Kind {
	case caller.Human:
	case caller.Coordinator:
		if *slug == "" {
			*slug = e.Caller.Project
		}
		if *slug != e.Caller.Project {
			return &tasks.Error{Code: "not-your-project", Msg: "a coordinator renders its own project's notes"}
		}
	default:
		return &tasks.Error{Code: "coordinator-only", Msg: "tm vault render is the coordinator's or the user's"}
	}
	if !vault.On() {
		return &tasks.Error{Code: "vault-off", Msg: "the vault's derived notes are off: they render once state/vault exists and front matter is YAML (state/front-yaml); the user turns them on with tm vault init"}
	}
	var slugs []string
	if *slug != "" {
		if _, err := project.Open(*slug); err != nil {
			return err
		}
		slugs = []string{*slug}
	} else {
		list, err := project.List()
		if err != nil {
			return err
		}
		for _, s := range list {
			slugs = append(slugs, s.Slug)
		}
	}
	written, removed := 0, 0
	var errs []error
	for _, s := range slugs {
		p, err := project.Open(s)
		if err == nil {
			var w, r int
			w, r, err = vault.Render(p)
			written, removed = written+w, removed+r
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s, err))
		}
	}
	daily, err := vault.RenderDaily(time.Now(), n)
	if err != nil {
		errs = append(errs, err)
	}
	shared := []vaultChange{}
	if *slug == "" {
		cs, err := vault.RenderShared()
		if err != nil {
			errs = append(errs, err)
		}
		for _, c := range cs {
			shared = append(shared, vaultChange{Path: c.Rel, What: c.What, Kept: c.Kept})
		}
	}
	if *asJSON {
		if err := e.printJSON(map[string]any{"projects": slugs, "notes_written": written, "notes_removed": removed, "daily_written": daily, "days": n, "shared": shared}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(e.Stdout, "task and thread notes: %d written, %d removed; daily notes of the last %d days: %d written (the rest unchanged or without journal lines)\n", written, removed, n, daily)
		for _, c := range shared {
			if c.What != "" {
				fmt.Fprintf(e.Stdout, "%s: %s\n", c.Path, c.What)
			} else {
				fmt.Fprintf(e.Stdout, "%s: keeps %s\n", c.Path, c.Kept)
			}
		}
	}
	return errors.Join(errs...)
}

// vaultChange is a file of _tm/ tm vault render wrote or kept, in --json.
type vaultChange struct {
	Path string `json:"path"`
	What string `json:"what,omitempty"`
	Kept string `json:"kept,omitempty"`
}
