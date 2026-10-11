package cli

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/project"
)

const modUsage = `usage: tm mod list [--outdated] [--json]
       tm mod install <path | git-url[@tag]> [--yes]
       tm mod enable <id> [--project <slug>] [--yes]
       tm mod disable <id> [--project <slug>]
       tm mod remove <id> [--purge] [--yes]
       tm mod update [<id>] [--yes]
       tm mod validate [<dir>…] [--json]

User mods (docs/SPEC.md §5.3), installed in ~/.terminatr/mods. install
takes a folder or a git URL (https://, ssh://, file://, user@host:path)
with an optional @tag; there is no catalogue. It shows what the mod asks
for and installs it disabled after a y on a terminal (--yes without
one). enable grants what it asks for, after the same question; with
--project it runs for that project too (disable --project takes one
off). update fetches each mod's source again and asks again only for new
permissions or changed rules. remove keeps the mod's data 30 days unless
--purge. validate checks mod folders (the installed mods when none is
named) and exits 1 if any is invalid. All but list and validate are the
human's: an agent is refused.`

func init() { commands["mod"] = runMod }

// stdinTTY says whether a question can be asked on stdin; tests replace it.
var stdinTTY = func(e *Env) bool {
	f, ok := e.Stdin.(*os.File)
	return ok && isTTY(f)
}

// runMod is tm mod (mods design §8).
func runMod(e *Env, args []string) error {
	if len(args) == 0 || args[0] == "help" {
		return usagef("%s", modUsage)
	}
	f := newFlags()
	asJSON, outdated, yes, purge := f.Bool("json"), f.Bool("outdated"), f.Bool("yes"), f.Bool("purge")
	slug := f.String("project")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	allowed := map[string][]string{
		"list": {"json", "outdated"}, "install": {"yes"}, "enable": {"project", "yes"}, "disable": {"project"},
		"remove": {"purge", "yes"}, "update": {"yes"}, "validate": {"json"},
	}
	ok, known := allowed[args[0]]
	if !known {
		return usagef("unknown subcommand %q\n%s", args[0], modUsage)
	}
	for _, n := range []string{"json", "outdated", "yes", "purge", "project"} {
		if f.IsSet(n) && !slices.Contains(ok, n) {
			return usagef("tm mod %s takes no --%s\n%s", args[0], n, modUsage)
		}
	}
	if args[0] != "list" && args[0] != "validate" && e.Caller.IsAgent() {
		return humanOnly()
	}
	one := func() (string, error) {
		if len(pos) != 1 {
			return "", usagef("%s", modUsage)
		}
		return pos[0], nil
	}
	switch args[0] {
	case "list":
		if len(pos) > 0 {
			return usagef("%s", modUsage)
		}
		return e.modList(*outdated, *asJSON)
	case "validate":
		return e.modValidate(pos, *asJSON)
	case "install":
		src, err := one()
		if err != nil {
			return err
		}
		return e.modInstall(src, *yes)
	case "enable":
		id, err := one()
		if err != nil {
			return err
		}
		return e.modEnable(id, *slug, *yes)
	case "disable":
		id, err := one()
		if err != nil {
			return err
		}
		if *slug != "" {
			if _, err := project.Open(*slug); err != nil {
				return err
			}
		}
		if err := mod.Disable(e.Caller, id, *slug); err != nil {
			return modErr(err)
		}
		if *slug != "" {
			fmt.Fprintf(e.Stdout, "%s no longer runs for %s\n", id, *slug)
		} else {
			fmt.Fprintf(e.Stdout, "%s disabled\n", id)
		}
		return nil
	case "remove":
		id, err := one()
		if err != nil {
			return err
		}
		return e.modRemove(id, *purge, *yes)
	case "update":
		if len(pos) > 1 {
			return usagef("%s", modUsage)
		}
		return e.modUpdate(pos, *yes)
	}
	return nil
}

func humanOnly() error {
	return &project.Error{Code: "human-only", Msg: "mods are installed, enabled, updated and removed by the human"}
}

// modErr maps internal/mod's refusals to exit 1, with a code.
func modErr(err error) error {
	if errors.Is(err, mod.ErrHumanOnly) {
		return humanOnly()
	}
	if errors.Is(err, config.ErrForm) {
		return err
	}
	return &project.Error{Code: "refused", Msg: err.Error()}
}

// confirm asks q on a terminal; without one, yes must be given.
func (e *Env) confirm(yes bool, cmd, q string) error {
	switch {
	case yes:
		return nil
	case !stdinTTY(e):
		return usagef("%s: confirm with --yes", cmd)
	case !ask(e, q+" [y/N] "):
		return &project.Error{Code: "not-confirmed", Msg: "nothing changed"}
	}
	return nil
}

// registry loads the installed mods with config.toml's tables.
func registry() (*mod.Registry, error) {
	c, err := config.Load()
	if err != nil && c == nil {
		return nil, err
	}
	return mod.Load(c)
}

// modRow is a mod in tm mod list --json.
type modRow struct {
	ID        string   `json:"id"`
	Name      string   `json:"name,omitempty"`
	Version   string   `json:"version,omitempty"`
	State     string   `json:"state"`
	Kinds     []string `json:"kinds"`
	Grants    []string `json:"grants"`
	WantsMore []string `json:"wants_more,omitempty"`
	Projects  []string `json:"projects,omitempty"`
	Error     string   `json:"error,omitempty"`
	Source    string   `json:"source,omitempty"`
	Latest    string   `json:"latest,omitempty"`
	Behind    bool     `json:"behind,omitempty"`
}

func (e *Env) modList(outdated, asJSON bool) error {
	r, err := registry()
	if err != nil {
		return err
	}
	rows, shown := []modRow{}, []mod.Mod{}
	for _, m := range r.Mods {
		row := modRow{ID: m.ID, State: string(m.State), Kinds: []string{}, Grants: m.Grants, WantsMore: m.WantsMore, Projects: m.Projects, Error: m.Reason()}
		if row.Grants == nil {
			row.Grants = []string{}
		}
		if m.Manifest != nil {
			row.Name, row.Version, row.Kinds = mod.Clean(m.Manifest.Name, false), m.Manifest.Version, m.Manifest.Kinds()
		}
		src, hasSrc := mod.ReadSource(m.Dir)
		if hasSrc {
			row.Source = src.String()
		}
		if outdated {
			if !hasSrc {
				continue
			}
			next, behind, err := mod.Latest(src)
			if err != nil {
				fmt.Fprintf(e.Stderr, "tm mod list: %s: %v\n", m.ID, err)
				continue
			}
			if !behind {
				continue
			}
			row.Latest, row.Behind = next, true
		}
		rows, shown = append(rows, row), append(shown, m)
	}
	if asJSON {
		return e.printJSON(rows)
	}
	if len(rows) == 0 {
		if outdated {
			fmt.Fprintln(e.Stdout, "Every mod is up to date.")
		} else {
			fmt.Fprintln(e.Stdout, "No mods installed. tm mod install <path | git-url[@tag]> installs one.")
		}
		return nil
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for i, row := range rows {
		m := shown[i]
		name, version, kinds := row.Name, row.Version, strings.Join(row.Kinds, " · ")
		for _, s := range []*string{&name, &version, &kinds} {
			if *s == "" {
				*s = "-"
			}
		}
		last := m.Status()
		if outdated {
			last = "→ " + row.Latest
		}
		fmt.Fprintf(tw, "%s %s\t%s\t%s\t%s\t%s\n", modGlyph(m), m.ID, name, version, kinds, last)
	}
	return tw.Flush()
}

// modGlyph is the Mods tab's mark for m (mods design §9): ● on, ○ off,
// ▲ failed or unusable, ◆ wants more.
func modGlyph(m mod.Mod) string {
	switch {
	case m.State == mod.Invalid || m.State == mod.Incompatible:
		return "▲"
	case m.State == mod.Enabled && len(m.WantsMore) > 0:
		return "◆"
	case m.State == mod.Enabled:
		return "●"
	}
	return "○"
}

// validRow is a folder in tm mod validate --json.
type validRow struct {
	Dir     string   `json:"dir"`
	ID      string   `json:"id,omitempty"`
	Version string   `json:"version,omitempty"`
	OK      bool     `json:"ok"`
	Errors  []string `json:"errors,omitempty"`
}

func (e *Env) modValidate(dirs []string, asJSON bool) error {
	if len(dirs) == 0 {
		r, err := registry()
		if err != nil {
			return err
		}
		for _, m := range r.Mods {
			dirs = append(dirs, m.Dir)
		}
	}
	rows := []validRow{}
	bad := false
	for _, d := range dirs {
		dir := e.abs(d)
		row := validRow{Dir: dir, OK: true}
		m, err := mod.Validate(dir)
		if m != nil {
			row.ID, row.Version = m.ID, m.Version
		}
		if err != nil {
			row.OK, bad = false, true
			for _, l := range strings.Split(err.Error(), "\n") {
				row.Errors = append(row.Errors, mod.Clean(l, false))
			}
		}
		rows = append(rows, row)
	}
	if asJSON {
		if err := e.printJSON(rows); err != nil {
			return err
		}
	} else {
		for _, row := range rows {
			if row.OK {
				fmt.Fprintf(e.Stdout, "ok       %s %s  %s\n", row.ID, row.Version, row.Dir)
				continue
			}
			fmt.Fprintf(e.Stdout, "invalid  %s\n", row.Dir)
			for _, l := range row.Errors {
				fmt.Fprintf(e.Stdout, "  %s\n", l)
			}
		}
	}
	if bad {
		return &exitError{ExitRefused}
	}
	return nil
}

// fetch reads src and stages the mod, printing why it is invalid.
func (e *Env) fetch(src mod.Source) (*mod.Staged, error) {
	st, err := mod.Fetch(src)
	if err != nil {
		if st != nil {
			st.Close()
		}
		return nil, &project.Error{Code: "invalid-mod", Msg: fmt.Sprintf("%s: %s", src, strings.ReplaceAll(mod.Clean(err.Error(), true), "\n", "; "))}
	}
	return st, nil
}

func (e *Env) modInstall(arg string, yes bool) error {
	mod.SweepRemoved(time.Now())
	src, err := mod.ParseSource(arg, e.Cwd)
	if err != nil {
		return usagef("%v", err)
	}
	st, err := e.fetch(src)
	if err != nil {
		return err
	}
	defer st.Close()
	m := st.Manifest
	if r, err := registry(); err == nil {
		if _, ok := r.Get(m.ID); ok {
			return &project.Error{Code: "mod-exists", Msg: fmt.Sprintf("%s is already installed: tm mod update %s", m.ID, m.ID)}
		}
	}
	fmt.Fprintf(e.Stdout, "%s\nFrom %s. It installs disabled: tm mod enable %s turns it on.\n", mod.Consent(m, st.Dir), src, m.ID)
	if err := e.confirm(yes, "tm mod install "+arg, "Install "+mod.Clean(m.Name, false)+"?"); err != nil {
		return err
	}
	if err := mod.Install(e.Caller, st); err != nil {
		return modErr(err)
	}
	fmt.Fprintf(e.Stdout, "installed %s %s, disabled: tm mod enable %s\n", m.ID, m.Version, m.ID)
	return nil
}

func (e *Env) modEnable(id, slug string, yes bool) error {
	if slug != "" {
		if _, err := project.Open(slug); err != nil {
			return err
		}
	}
	r, err := registry()
	if err != nil {
		return err
	}
	m, ok := r.Get(id)
	switch {
	case !ok:
		return &project.Error{Code: "refused", Msg: fmt.Sprintf("no mod %q is installed", id)}
	case m.State == mod.Invalid || m.State == mod.Incompatible:
		return &project.Error{Code: "invalid-mod", Msg: fmt.Sprintf("%s can't be enabled: %s", id, m.Reason())}
	}
	// Turning it on, or granting what it wants more, is consent; running
	// it for one more project grants nothing new.
	if m.State != mod.Enabled || len(m.WantsMore) > 0 {
		fmt.Fprint(e.Stdout, mod.Consent(m.Manifest, m.Dir))
		if err := e.confirm(yes, "tm mod enable "+id, "Enable "+mod.Clean(m.Manifest.Name, false)+"?"); err != nil {
			return err
		}
	}
	if err := mod.Enable(e.Caller, id, slug); err != nil {
		return modErr(err)
	}
	r, err = registry()
	if err != nil {
		return err
	}
	if m, ok = r.Get(id); ok {
		fmt.Fprintf(e.Stdout, "%s enabled: %s\n", id, m.Status())
	}
	return nil
}

func (e *Env) modRemove(id string, purge, yes bool) error {
	mod.SweepRemoved(time.Now())
	r, err := registry()
	if err != nil {
		return err
	}
	m, ok := r.Get(id)
	if !ok {
		return &project.Error{Code: "refused", Msg: fmt.Sprintf("no mod %q is installed", id)}
	}
	name := id
	if m.Manifest != nil {
		name = mod.Clean(m.Manifest.Name, false)
	}
	q := "Remove " + name + "? Its data is kept for 30 days."
	if purge {
		q = "Remove " + name + " and its data?"
	}
	if err := e.confirm(yes, "tm mod remove "+id, q); err != nil {
		return err
	}
	if err := mod.Remove(e.Caller, id, purge); err != nil {
		return modErr(err)
	}
	fmt.Fprintf(e.Stdout, "removed %s\n", id)
	return nil
}

func (e *Env) modUpdate(ids []string, yes bool) error {
	r, err := registry()
	if err != nil {
		return err
	}
	var todo []mod.Mod
	if len(ids) == 1 {
		m, ok := r.Get(ids[0])
		if !ok {
			return &project.Error{Code: "refused", Msg: fmt.Sprintf("no mod %q is installed", ids[0])}
		}
		todo = append(todo, m)
	} else {
		todo = r.Mods
	}
	failed := false
	for _, m := range todo {
		if err := e.updateOne(m, yes, len(ids) == 1); err != nil {
			fmt.Fprintf(e.Stderr, "tm mod update: %s: %v\n", m.ID, err)
			failed = true
		}
	}
	if failed {
		return &exitError{ExitRefused}
	}
	return nil
}

// updateOne updates installed mod m from its source; named says the
// human named it, so a mod without a recorded source is an error.
func (e *Env) updateOne(m mod.Mod, yes, named bool) error {
	src, ok := mod.ReadSource(m.Dir)
	if !ok {
		if named {
			return errors.New("it records no source (installed by hand, or by tm mod dev)")
		}
		return nil
	}
	next, err := mod.Next(src)
	if err != nil {
		return err
	}
	st, err := e.fetch(next)
	if err != nil {
		return err
	}
	defer st.Close()
	if st.Source.Hash == src.Hash {
		fmt.Fprintf(e.Stdout, "%s is up to date\n", m.ID)
		return nil
	}
	from := "?"
	if m.Manifest != nil {
		from = m.Manifest.Version
	}
	fmt.Fprintf(e.Stdout, "%s %s → %s (%s)\n", m.ID, from, st.Manifest.Version, st.Source)
	ch := mod.Diff(m.Manifest, m.Dir, st.Manifest, st.Dir)
	var grant []string
	if ch.Asks() {
		fmt.Fprint(e.Stdout, ch.Describe(st.Manifest))
		if err := e.confirm(yes, "tm mod update "+m.ID, "Update "+mod.Clean(st.Manifest.Name, false)+"?"); err != nil {
			return err
		}
		if m.State == mod.Enabled {
			grant = ch.NewPerms
		}
	}
	if err := mod.Replace(e.Caller, m.ID, st, grant); err != nil {
		return modErr(err)
	}
	fmt.Fprintf(e.Stdout, "updated %s to %s\n", m.ID, st.Manifest.Version)
	return nil
}
