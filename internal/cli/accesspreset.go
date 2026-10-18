package cli

import (
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// accessPreset implements tm access preset list|show|add (T574 §5): the
// presets tm ships, the rules each adds, and adding them to a project's
// or all projects' sandbox table as plain rules. The human's: an agent
// gets human-only, told by the server from the process tree (access is
// a checked command), not only by the environment an agent can unset;
// add, which writes config.toml from this process, refuses to run
// when no server can tell.
func accessPreset(e *Env, args []string) error {
	const cmd = "access preset"
	if len(args) == 0 {
		return srvUsage(cmd, accessUsage)
	}
	if e.Caller.IsAgent() {
		return srvFail(cmd, &project.Error{Code: "human-only", Msg: "presets are the user's: they add them in the Access tab (p) or with tm access preset add"})
	}
	if args[0] == "add" && e.Identify != nil && !e.Identified {
		return srvFail(cmd, &project.Error{Code: "server-down", Msg: "tm server isn't running: it tells who runs tm access preset add; start it (tm server start), or use the Access tab"})
	}
	f := newFlags()
	asJSON, all := f.Bool("json"), f.Bool("all")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return srvFail(cmd, err)
	}
	switch verb := args[0]; {
	case verb == "list" && len(pos) == 0 && !*all:
		if *asJSON {
			return e.printJSON(config.Presets)
		}
		tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
		for _, p := range config.Presets {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Name, p.Title, presetOS(p))
		}
		return tw.Flush()
	case verb == "show" && len(pos) == 1 && !*all:
		p, err := presetNamed(pos[0])
		if err != nil {
			return srvFail(cmd, err)
		}
		if *asJSON {
			return e.printJSON(p)
		}
		fmt.Fprintf(e.Stdout, "%s: %s %s\n", p.Title, p.About, presetOS(p))
		tw := tabwriter.NewWriter(e.Stdout, 0, 0, 2, ' ', 0)
		for _, r := range p.Rules {
			fmt.Fprintf(tw, "%s\t%s\n", presetRuleKind(r), r.Value)
		}
		return tw.Flush()
	case verb == "add" && !*asJSON && (len(pos) == 1 || len(pos) == 2 && !*all):
		return e.presetAdd(pos[0], pos[1:], *all)
	}
	return srvUsage(cmd, accessUsage)
}

// presetNamed is the preset called name, or an error naming them all.
func presetNamed(name string) (config.Preset, error) {
	if p, ok := config.PresetNamed(name); ok {
		return p, nil
	}
	var names []string
	for _, p := range config.Presets {
		names = append(names, p.Name)
	}
	return config.Preset{}, fmt.Errorf("no preset %q: %s", name, strings.Join(names, ", "))
}

// presetOS names the platforms a preset's rules apply on.
func presetOS(p config.Preset) string {
	var names []string
	for _, os := range p.OS {
		names = append(names, config.OSName(os))
	}
	return strings.Join(names, ", ")
}

// presetRuleKind names a preset rule's kind as tm access add takes it.
func presetRuleKind(r config.PresetRule) string {
	switch r.Key {
	case config.SandboxHosts:
		return "host"
	case config.SandboxGuides:
		return "guide"
	case config.SandboxLocalPorts:
		return "local-ports"
	case config.SandboxExcluded:
		return "excluded"
	}
	return r.Key
}

// presetAdd adds preset name's rules that the threads don't have yet to
// the table of the project named in pos (the one here when none), or
// all projects' with all, printing each rule added and each there
// already, and journals them.
func (e *Env) presetAdd(name string, pos []string, all bool) error {
	const cmd = "access preset add"
	p, err := presetNamed(name)
	if err != nil {
		return srvFail(cmd, err)
	}
	slug, table, who := "", config.DefaultsTable, "all projects"
	if !all {
		flag := ""
		if len(pos) == 1 {
			flag = pos[0]
		}
		if slug, err = project.Resolve(flag, e.Getenv, e.Cwd); err != nil {
			return srvFail(cmd, err)
		}
		if slug == "" {
			return srvUsage(cmd, "no project here; name one (tm project list), or --all")
		}
		if _, err := project.Open(slug); err != nil {
			return srvFail(cmd, err)
		}
		table, who = "projects."+slug, slug
	}
	cfg, err := config.Load()
	if err != nil {
		return srvFail(cmd, err)
	}
	add := p.Missing(cfg.ThreadSandbox(slug).Has)
	var words []string
	if len(add) > 0 {
		err := config.EditSandbox(table, func(t *config.SandboxTable) error {
			for _, r := range add {
				t.AddRule(r)
			}
			return nil
		})
		if err != nil {
			return srvFail(cmd, err)
		}
	}
	for _, r := range p.Rules {
		w := presetRuleKind(r) + " " + r.Value
		if !slices.Contains(add, r) {
			fmt.Fprintf(e.Stdout, "already there for %s: %s\n", who, w)
			continue
		}
		words = append(words, w)
		fmt.Fprintf(e.Stdout, "added to %s: %s\n", who, w)
	}
	if len(add) == 0 {
		return nil
	}
	detail := "preset " + p.Name + ": " + strings.Join(words, ", ")
	if all {
		list, _ := project.List()
		for _, sum := range list {
			if pr, err := project.Open(sum.Slug); err == nil {
				pr.Journal(e.Caller, "settings.sandbox", sum.Slug, detail+" (all projects)")
			}
		}
		fmt.Fprintln(e.Stdout, "every project's threads get them from their next start or restart")
		return nil
	}
	if pr, err := project.Open(slug); err == nil {
		pr.Journal(e.Caller, "settings.sandbox", slug, detail)
	}
	fmt.Fprintf(e.Stdout, "threads of %s get them from their next start or restart\n", slug)
	return nil
}
