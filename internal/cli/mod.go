package cli

import (
	"cmp"
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
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tui"
)

const modUsage = `usage: tm mod list [--outdated] [--json]
       tm mod show <id> [--json]
       tm mod install <path | git-url[//subfolder][@tag] | github-tree-url> [--yes]
       tm mod enable <id> [--project <slug>] [--yes]
       tm mod disable <id> [--project <slug>]
       tm mod remove <id> [--purge] [--yes]
       tm mod update [<id>] [--yes]
       tm mod validate [<dir>…] [--json]
       tm mod new <id> [--kind column|hook|theme|icons|keymap|rules] [--dir <dir>]
       tm mod dev <dir> [--yes]
       tm mod test [<dir>…] [--json]
       tm mod logs <id> [--follow|-f] [--lines|-n N]
       tm mod run <id> <command> [--project <slug>]
       tm mod defaults <id> [--apply] [--yes]
       tm mod examples [--json]

User mods (docs/SPEC.md §5.3), installed in ~/.terminatr/mods. install
takes a folder or a git URL (https://, ssh://, file://, user@host:path)
with an optional //<subfolder> holding the mod and an optional @tag,
e.g. https://example.com/mods.git//nord@v1.0.0, or a GitHub folder's
URL (https://github.com/<owner>/<repo>/tree/<tag>/<subfolder>); only
the subfolder is fetched and copied in. There is no catalogue. It shows what the mod asks
for and installs it disabled after a y on a terminal (--yes without
one). enable grants what it asks for, after the same question; with
--project it runs for that project too (disable --project takes one
off). update fetches each mod's source again and asks again only for new
permissions or changed rules. run runs one of an enabled mod's commands
(its [provides].commands), as its key or ≡ menu entry on the dashboard
does, in the project given, if any. defaults shows the values an
enabled mod suggests for all projects' non-safety settings beside what
they follow now; --apply writes them to config.toml's [defaults] as
your change, after a y (--yes without a terminal), leaving each one
[defaults] already sets. remove keeps the mod's data 30 days unless
--purge. validate checks mod folders (the installed mods when none is
named) and exits 1 if any is invalid.

For authors: new scaffolds a mod in ./<id> (a hook unless --kind),
with the mod API's types from this tm and, for code, a fixture. dev
links a folder in as an installed mod, enables it (after the same
question), restarts its host on every save and tails its log until
interrupted; it then disables it, leaving the link (tm mod remove
unlinks it). test replays each code mod's tests/*.json fixtures with a
fake broker (the current folder when none is named) and exits 1 if
any fails. logs prints the end of a mod's log, and follows it with -f.
examples shows where terminatr's example mods are and how to install
each; it installs nothing. All but list, validate, new, test, logs and
examples are the human's: an agent is refused.`

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
	asJSON, outdated, yes, purge, apply := f.Bool("json"), f.Bool("outdated"), f.Bool("yes"), f.Bool("purge"), f.Bool("apply")
	slug := f.String("project")
	kind, dir := f.String("kind"), f.String("dir")
	follow, followF := f.Bool("follow"), f.Bool("f")
	lines, linesN := f.String("lines"), f.String("n")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	allowed := map[string][]string{
		"list": {"json", "outdated"}, "show": {"json"}, "install": {"yes"}, "enable": {"project", "yes"}, "disable": {"project"},
		"remove": {"purge", "yes"}, "update": {"yes"}, "validate": {"json"},
		"new": {"kind", "dir"}, "dev": {"yes"}, "test": {"json"}, "logs": {"follow", "f", "lines", "n"},
		"run":      {"project"},
		"defaults": {"apply", "yes"},
		"examples": {"json"},
	}
	ok, known := allowed[args[0]]
	if !known {
		return usagef("unknown subcommand %q\n%s", args[0], modUsage)
	}
	for _, n := range []string{"json", "outdated", "yes", "purge", "apply", "project", "kind", "dir", "follow", "f", "lines", "n"} {
		if f.IsSet(n) && !slices.Contains(ok, n) {
			return usagef("tm mod %s takes no --%s\n%s", args[0], n, modUsage)
		}
	}
	if !agentMod[args[0]] && e.Caller.IsAgent() {
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
	case "show":
		id, err := one()
		if err != nil {
			return err
		}
		return e.modShow(id, *asJSON)
	case "validate":
		return e.modValidate(pos, *asJSON)
	case "new":
		id, err := one()
		if err != nil {
			return err
		}
		return e.modNew(id, *kind, *dir)
	case "test":
		return e.modTest(pos, *asJSON)
	case "examples":
		if len(pos) > 0 {
			return usagef("%s", modUsage)
		}
		return e.modExamples(*asJSON)
	case "logs":
		id, err := one()
		if err != nil {
			return err
		}
		return e.modLogs(id, *follow || *followF, cmp.Or(*lines, *linesN))
	case "dev":
		d, err := one()
		if err != nil {
			return err
		}
		return e.modDev(d, *yes)
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
		if err := optProject(*slug); err != nil {
			return err
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
	case "run":
		if len(pos) != 2 {
			return usagef("%s", modUsage)
		}
		return e.modRun(pos[0], pos[1], *slug)
	case "defaults":
		id, err := one()
		if err != nil {
			return err
		}
		return e.modDefaults(id, *apply, *yes)
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

// modRun is tm mod run: the mod gets command.run (mod.command), as
// from the command's key on the dashboard, but may open no view.
func (e *Env) modRun(id, cmd, slug string) error {
	if err := mod.CheckID(id); err != nil {
		return usagef("%v", err)
	}
	if err := optProject(slug); err != nil {
		return err
	}
	if err := runModCommand(proto.ModCommandParams{Mod: id, Command: cmd, Project: slug}); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "ran %s\n", mod.CommandName(id, cmd))
	return nil
}

// runModCommand sends mod.command, starting the server if none runs;
// tests replace it.
var runModCommand = func(p proto.ModCommandParams) error {
	c, _, err := connect(true)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(proto.MethodModCommand, p, nil)
}

// agentMod are the tm mod subcommands an agent may run: they read, or
// write only in the folder given.
var agentMod = map[string]bool{"list": true, "show": true, "validate": true, "new": true, "test": true, "logs": true, "examples": true}

// exampleRow is an example mod in tm mod examples --json.
type exampleRow struct {
	ID      string   `json:"id"`
	Surface string   `json:"surface"`
	Perms   []string `json:"permissions"`
	Builtin bool     `json:"builtin,omitempty"`
	Install []string `json:"install"`
}

// modExamples is tm mod examples (mods design §13): the folder's link
// and each example's install lines, as the Mods tab's e shows them. It
// installs and enables nothing.
func (e *Env) modExamples(asJSON bool) error {
	if asJSON {
		rows := []exampleRow{}
		for _, x := range mod.Examples {
			rows = append(rows, exampleRow{ID: x.ID, Surface: x.Surface, Perms: append([]string{}, x.Perms()...),
				Builtin: mod.IsBundled(x.ID), Install: mod.InstallLines(x.ID)})
		}
		return e.printJSON(struct {
			URL      string       `json:"url"`
			Repo     string       `json:"repo"`
			Examples []exampleRow `json:"examples"`
		}{mod.ExamplesURL, mod.ExamplesRepo, rows})
	}
	fmt.Fprintf(e.Stdout, "terminatr's example mods, one for each place a mod can show up: %s\n", mod.ExamplesURL)
	fmt.Fprintln(e.Stdout, "Nothing here is installed. Each installs with one line, then turns on in the Mods tab or with tm mod enable <id>:")
	for _, x := range mod.Examples {
		fmt.Fprintf(e.Stdout, "\n%s  %s · %s\n", x.ID, x.Surface, strings.Join(x.Words(), " · "))
		for _, l := range mod.InstallLines(x.ID) {
			fmt.Fprintf(e.Stdout, "  %s\n", l)
		}
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

// installedMod is the installed mod id, else the refusal that none is.
func installedMod(r *mod.Registry, id string) (mod.Mod, error) {
	m, ok := r.Get(id)
	if !ok {
		return m, &project.Error{Code: "refused", Msg: fmt.Sprintf("no mod %q is installed", id)}
	}
	return m, nil
}

// optProject checks that the project named by an optional --project
// exists; "" names none.
func optProject(slug string) error {
	if slug == "" {
		return nil
	}
	_, err := project.Open(slug)
	return err
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
	Commit    string   `json:"commit,omitempty"`
	Installed string   `json:"installed,omitempty"`
	Updated   string   `json:"updated,omitempty"`
	Latest    string   `json:"latest,omitempty"`
	Behind    bool     `json:"behind,omitempty"`
	// Builtin: it ships in tm (a bundled mod); it updates with tm.
	Builtin bool `json:"builtin,omitempty"`
}

// setSource fills the row's fields from where the mod came from.
func (r *modRow) setSource(src mod.Source) {
	r.Source, r.Commit = mod.Clean(src.String(), false), mod.Clean(src.Commit, false)
	if !src.Installed.IsZero() {
		r.Installed = src.Installed.UTC().Format(time.RFC3339)
	}
	if !src.Updated.IsZero() {
		r.Updated = src.Updated.UTC().Format(time.RFC3339)
	}
}

// modShow prints one installed mod: what it is and where it came from.
func (e *Env) modShow(id string, asJSON bool) error {
	r, err := registry()
	if err != nil {
		return err
	}
	m, ok := r.Get(id)
	if !ok {
		return &project.Error{Code: "no-mod", Msg: "no mod " + id + ": tm mod list shows the installed ones"}
	}
	row := modRow{ID: m.ID, State: string(m.State), Kinds: []string{}, Grants: m.Grants, WantsMore: m.WantsMore, Projects: m.Projects, Error: m.Reason(), Builtin: m.Builtin}
	if row.Grants == nil {
		row.Grants = []string{}
	}
	if m.Manifest != nil {
		row.Name, row.Version, row.Kinds = mod.Clean(m.Manifest.Name, false), m.Manifest.Version, m.Manifest.Kinds()
	}
	src, hasSrc := mod.ReadSource(m.Dir)
	if hasSrc {
		row.setSource(src)
	}
	if asJSON {
		return e.printJSON(row)
	}
	fmt.Fprintf(e.Stdout, "%s %s\n", m.ID, cmp.Or(row.Version, "-"))
	if row.Name != "" {
		fmt.Fprintf(e.Stdout, "name:       %s\n", row.Name)
	}
	fmt.Fprintf(e.Stdout, "state:      %s\n", m.Status())
	if len(row.Kinds) > 0 {
		fmt.Fprintf(e.Stdout, "kinds:      %s\n", strings.Join(row.Kinds, " · "))
	}
	switch {
	case m.Builtin:
		fmt.Fprintln(e.Stdout, "source:     built into tm (updates with tm)")
	case !hasSrc:
		fmt.Fprintln(e.Stdout, "source:     none recorded (installed by hand, or by tm mod dev)")
	default:
		fmt.Fprintf(e.Stdout, "source:     %s\n", mod.Clean(row.Source, false))
		if row.Commit != "" {
			fmt.Fprintf(e.Stdout, "commit:     %s\n", mod.Clean(row.Commit, false))
		}
		if row.Installed != "" {
			fmt.Fprintf(e.Stdout, "installed:  %s\n", row.Installed)
		}
		if row.Updated != "" {
			fmt.Fprintf(e.Stdout, "updated:    %s\n", row.Updated)
		}
	}
	return nil
}

func (e *Env) modList(outdated, asJSON bool) error {
	r, err := registry()
	if err != nil {
		return err
	}
	rows, shown := []modRow{}, []mod.Mod{}
	for _, m := range r.Mods {
		row := modRow{ID: m.ID, State: string(m.State), Kinds: []string{}, Grants: m.Grants, WantsMore: m.WantsMore, Projects: m.Projects, Error: m.Reason(), Builtin: m.Builtin}
		if row.Grants == nil {
			row.Grants = []string{}
		}
		if m.Manifest != nil {
			row.Name, row.Version, row.Kinds = mod.Clean(m.Manifest.Name, false), m.Manifest.Version, m.Manifest.Kinds()
		}
		src, hasSrc := mod.ReadSource(m.Dir)
		if hasSrc {
			row.setSource(src)
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
	if err := tw.Flush(); err != nil {
		return err
	}
	if !outdated && !slices.ContainsFunc(shown, func(m mod.Mod) bool { return !m.Builtin }) {
		fmt.Fprintln(e.Stdout, "No mods of your own yet. tm mod install <path | git-url[@tag]> installs one.")
	}
	return nil
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
			if !m.Builtin { // tm's own tests check those
				dirs = append(dirs, m.Dir)
			}
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
		return nil, &project.Error{Code: "invalid-mod", Msg: (&mod.FetchError{From: src.String(), Err: err}).Error()}
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
		if why := mod.Taken(r, m.ID); why != "" {
			return &project.Error{Code: "mod-exists", Msg: why}
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
	if err := optProject(slug); err != nil {
		return err
	}
	r, err := registry()
	if err != nil {
		return err
	}
	m, err := installedMod(r, id)
	switch {
	case err != nil:
		return err
	case m.State == mod.Invalid || m.State == mod.Incompatible:
		return &project.Error{Code: "invalid-mod", Msg: fmt.Sprintf("%s can't be enabled: %s", id, m.Reason())}
	}
	// Turning it on, or granting what it wants more, is consent; running
	// it for one more project grants nothing new.
	if m.State != mod.Enabled || len(m.WantsMore) > 0 {
		fmt.Fprint(e.Stdout, m.Consent())
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
	if m, ok := r.Get(id); ok {
		fmt.Fprintf(e.Stdout, "%s enabled: %s\n", id, m.Status())
		if c, _ := config.Load(); m.Manifest != nil && m.Manifest.Permissions.Tools && (c == nil || !c.AgentMod) {
			fmt.Fprintf(e.Stdout, "The Agent mod is off: %s.\n", mod.ToolsAgentModNote)
		}
	}
	return nil
}

// modDefaults is tm mod defaults: the defaults mod id suggests for all
// projects (mods design §7, Tighten only), each beside what they follow
// now, applied with apply as the human's change after a question.
func (e *Env) modDefaults(id string, apply, yes bool) error {
	c, err := config.Load()
	if err != nil && c == nil {
		return err
	}
	r, err := mod.Load(c)
	if err != nil {
		return err
	}
	m, err := installedMod(r, id)
	switch {
	case err != nil:
		return err
	case m.Manifest == nil:
		return &project.Error{Code: "invalid-mod", Msg: fmt.Sprintf("%s: %s", id, m.Reason())}
	case len(m.Manifest.Defaults) == 0:
		fmt.Fprintf(e.Stdout, "%s suggests no defaults\n", id)
		return nil
	}
	sgs := m.Manifest.Suggestions(c)
	fmt.Fprintf(e.Stdout, "%s suggests for all projects (a project's own settings still win):\n", id)
	tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
	for _, sg := range sgs {
		now, want := mod.SettingWords(sg.Now), mod.SettingWords(sg.Value)
		switch {
		case sg.Yours:
			fmt.Fprintf(tw, "  %s\t%s\tyours in [defaults], stays (it suggests %s)\n", sg.Key, now, want)
		case !sg.Applies():
			fmt.Fprintf(tw, "  %s\t%s\talready\n", sg.Key, now)
		default:
			fmt.Fprintf(tw, "  %s\t%s → %s\n", sg.Key, now, want)
		}
	}
	tw.Flush()
	if !slices.ContainsFunc(sgs, mod.Suggestion.Applies) {
		fmt.Fprintln(e.Stdout, "Nothing to apply.")
		return nil
	}
	if m.State != mod.Enabled {
		fmt.Fprintf(e.Stdout, "It is off: tm mod enable %s, then tm mod defaults %s --apply.\n", id, id)
		return nil
	}
	if !apply {
		fmt.Fprintf(e.Stdout, "tm mod defaults %s --apply writes them to config.toml's [defaults], as your change.\n", id)
		return nil
	}
	if err := e.confirm(yes, "tm mod defaults "+id+" --apply", "Apply them to all projects?"); err != nil {
		return err
	}
	done, err := tui.ApplyModDefaults(e.Caller, id)
	for _, sg := range done {
		fmt.Fprintf(e.Stdout, "all projects: %s = %s\n", sg.Key, mod.SettingWords(sg.Value))
	}
	if err != nil {
		return modErr(err)
	}
	return nil
}

func (e *Env) modRemove(id string, purge, yes bool) error {
	mod.SweepRemoved(time.Now())
	r, err := registry()
	if err != nil {
		return err
	}
	m, err := installedMod(r, id)
	if err != nil {
		return err
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
		m, err := installedMod(r, ids[0])
		if err != nil {
			return err
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
// human named it, so a built-in mod or one without a recorded source is
// an error.
func (e *Env) updateOne(m mod.Mod, yes, named bool) error {
	if m.Builtin && !named {
		return nil
	}
	st, err := mod.FetchUpdate(m)
	var fe *mod.FetchError
	switch {
	case errors.Is(err, mod.ErrNoSource) && !named:
		return nil
	case errors.As(err, &fe):
		return &project.Error{Code: "invalid-mod", Msg: fe.Error()}
	case err != nil:
		return err
	case st == nil:
		fmt.Fprintf(e.Stdout, "%s is up to date\n", m.ID)
		return nil
	}
	defer st.Close()
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
