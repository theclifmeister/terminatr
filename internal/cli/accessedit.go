package cli

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/project"
)

// The writers of tm access (T574 §5): add and rm an entry of a sandbox
// list, edit one in place (T619), drop and undrop one of all projects' for a project, set local
// ports, escapes or yolo. The human's: an agent gets human-only, told by
// the server from the process tree (access is a checked command), and
// with no server to tell, the write is refused, as tm access preset add.
// Each write is journaled in the projects it reaches; threads get it
// from their next start or restart.

// accessKinds are the list kinds add and rm take, by their sandbox key.
var accessKinds = map[string]string{
	"write": config.SandboxWrite, "host": config.SandboxHosts, "guide": config.SandboxGuides,
	"excluded": config.SandboxExcluded, "allow": config.SandboxAllow,
	"socket": config.SandboxSockets, "approved": config.SandboxApproved,
}

// accessSettings are the settings set takes, by their config.toml key.
var accessSettings = map[string]string{"local-ports": config.SandboxLocalPorts, "escapes": "sandbox_escapes", "yolo": "yolo"}

// yoloWarning is what yolo opens, put to the human before it is on.
const yoloWarning = "Threads run with no sandbox and no permission prompts. Their commands can read and write anything you can, reach any host, and reach tm's own socket, so they can act as you in tm. The guard still refuses touching tm's own files, changing config.toml or the project folder, printing a secret variable, force-pushing to a protected branch and a thread merging; nothing else is checked."

// accessHuman refuses cmd to an agent, and to anyone when no server
// can tell who runs it.
func (e *Env) accessHuman(cmd string) error {
	if e.Caller.IsAgent() {
		return srvFail(cmd, &project.Error{Code: "human-only", Msg: "access rules are the user's: they change them in the Access tab or with tm access"})
	}
	if e.Identify != nil && !e.Identified {
		return srvFail(cmd, &project.Error{Code: "server-down", Msg: "tm server isn't running: it tells who runs tm access " + strings.TrimPrefix(cmd, "access ") + "; start it (tm server start), or use the Access tab"})
	}
	return nil
}

// accessScope is the table pos (a project, else the one here) or all
// names, its slug ("" for all projects) and how messages name it.
func (e *Env) accessScope(cmd string, pos []string, all bool) (table, slug, who string, err error) {
	if all {
		if len(pos) > 0 {
			return "", "", "", srvUsage(cmd, accessUsage)
		}
		return config.DefaultsTable, "", config.AllProjectsName, nil
	}
	if len(pos) > 1 {
		return "", "", "", srvUsage(cmd, accessUsage)
	}
	flag := ""
	if len(pos) == 1 {
		flag = pos[0]
	}
	if slug, err = project.Resolve(flag, e.Getenv, e.Cwd); err != nil {
		return "", "", "", srvFail(cmd, err)
	}
	if slug == "" {
		return "", "", "", srvUsage(cmd, "no project here; name one (tm project list), or --all")
	}
	if _, err := project.Open(slug); err != nil {
		return "", "", "", srvFail(cmd, err)
	}
	return "projects." + slug, slug, slug, nil
}

// accessJournal journals detail in slug's journal, or every project's
// for all projects.
func (e *Env) accessJournal(action, slug, detail string) {
	if slug != "" {
		if p, err := project.Open(slug); err == nil {
			p.Journal(e.Caller, action, slug, detail)
		}
		return
	}
	list, _ := project.List()
	for _, sum := range list {
		if p, err := project.Open(sum.Slug); err == nil {
			p.Journal(e.Caller, action, sum.Slug, detail+" (all projects)")
		}
	}
}

// accessNext says when threads get a change.
func (e *Env) accessNext(slug string) {
	if slug == "" {
		fmt.Fprintln(e.Stdout, "every project's threads get it from their next start or restart")
		return
	}
	fmt.Fprintf(e.Stdout, "threads of %s get it from their next start or restart\n", slug)
}

// accessAdd implements tm access add|rm <kind> <value> [<project> | --all].
func accessAdd(e *Env, verb string, args []string) error {
	cmd := "access " + verb
	if err := e.accessHuman(cmd); err != nil {
		return err
	}
	f := newFlags()
	all, noAsk := f.Bool("all"), f.Bool("without-asking")
	text, file, replace := f.String("text"), f.String("file"), f.Bool("replace")
	pos, err := f.Parse(args)
	if err != nil {
		return srvFail(cmd, err)
	}
	if len(pos) < 2 {
		return srvUsage(cmd, accessUsage)
	}
	kind, value := pos[0], pos[1]
	key, ok := accessKinds[kind]
	if !ok {
		return srvUsage(cmd, "kinds: write, host, guide, socket, excluded, approved, allow")
	}
	if *noAsk && (verb != "add" || key != config.SandboxExcluded) {
		return srvUsage(cmd, "--without-asking is for tm access add excluded")
	}
	var guide config.Guide
	switch guides := verb == "add" && key == config.SandboxGuides; {
	case !guides && f.anySet("text", "file", "replace"):
		return srvUsage(cmd, "--text, --file and --replace are for tm access add guide")
	case guides && f.IsSet("text") == f.IsSet("file"):
		return srvUsage(cmd, "tm access add guide <title> --text <text> | --file <file> (- for stdin)")
	case guides:
		body := *text
		if f.IsSet("file") {
			if body, err = e.readArg(*file); err != nil {
				return srvFail(cmd, err)
			}
		}
		guide = config.Guide{Title: value, Body: config.GuideBody(body)}
		if err := config.CheckGuideBody(guide.Body); err != nil {
			return srvFail(cmd, &project.Error{Code: "refused", Msg: err.Error()})
		}
	}
	table, slug, who, err := e.accessScope(cmd, pos[2:], *all)
	if err != nil {
		return err
	}
	if verb == "add" {
		if err := config.CheckNewSandboxEntry(key, value); err != nil {
			return srvFail(cmd, &project.Error{Code: "refused", Msg: err.Error()})
		}
		cfg, err := config.Load()
		if err != nil {
			return srvFail(cmd, err)
		}
		for _, r := range cfg.ThreadSandbox(slug).Rules {
			if r.Key == key && r.Value == value && !r.Own && slug != "" {
				if r.Dropped {
					return srvFail(cmd, &project.Error{Code: "refused", Msg: fmt.Sprintf("%s is all projects', dropped for %s: tm access undrop takes it back", value, slug)})
				}
				return srvFail(cmd, &project.Error{Code: "refused", Msg: fmt.Sprintf("all projects give %s already", value)})
			}
		}
	}
	err = config.EditSandbox(table, func(t *config.SandboxTable) error {
		switch has := t.Has(key, value); {
		case verb == "add" && has && key == config.SandboxGuides && !*replace:
			return fmt.Errorf("a guide %s is listed already for %s: --replace changes its text", value, who)
		case verb == "add" && key == config.SandboxGuides:
			t.SetGuide(guide)
		case verb == "add" && has && (key != config.SandboxExcluded || !*noAsk):
			return fmt.Errorf("%s is listed already for %s", value, who)
		case verb == "add":
			t.Add(key, value)
			if key == config.SandboxExcluded {
				t.SetAsk(value, !*noAsk)
			}
		case !has:
			return fmt.Errorf("%s isn't in the %s of %s", value, kind+" rules", who)
		default:
			t.Remove(key, value)
		}
		return nil
	})
	if err != nil {
		return srvFail(cmd, accessErr(err))
	}
	word := config.KindName(key) + " " + value
	if *noAsk {
		word += ", without asking"
	}
	detail := key + " + " + value
	if *replace {
		detail = key + " ~ " + value
	}
	if verb == "rm" {
		detail = key + " - " + value
		fmt.Fprintf(e.Stdout, "removed from %s: %s\n", who, word)
	} else {
		fmt.Fprintf(e.Stdout, "added to %s: %s\n", who, word)
		if w := config.SandboxWarning(key, value); w != "" {
			fmt.Fprintf(e.Stdout, "note: %s %s\n", value, w)
		}
		if key == config.SandboxExcluded {
			fmt.Fprintln(e.Stdout, "note: it runs outside the sandbox, with your full file and network access (tm's socket included)")
		}
	}
	if *noAsk {
		detail += " (ask = false)"
	}
	e.accessJournal("settings.sandbox", slug, detail)
	e.accessNext(slug)
	return nil
}

// accessEdit implements tm access edit <kind> <old> <new> [<project> |
// --all] (T619): one entry changed in place, its options with it, in one
// write; all projects' entry takes the projects' drops of it along.
func accessEdit(e *Env, args []string) error {
	const cmd = "access edit"
	if err := e.accessHuman(cmd); err != nil {
		return err
	}
	f := newFlags()
	all, noAsk, asks := f.Bool("all"), f.Bool("without-asking"), f.Bool("asks-first")
	text, file := f.String("text"), f.String("file")
	pos, err := f.Parse(args)
	if err != nil {
		return srvFail(cmd, err)
	}
	if len(pos) < 3 {
		return srvUsage(cmd, accessUsage)
	}
	kind, old, value := pos[0], pos[1], pos[2]
	key, ok := accessKinds[kind]
	if !ok {
		return srvUsage(cmd, "kinds: write, host, guide, socket, excluded, approved, allow")
	}
	ed := config.SandboxEdit{Key: key, Old: old, New: value}
	switch {
	case (*noAsk || *asks) && key != config.SandboxExcluded:
		return srvUsage(cmd, "--without-asking and --asks-first are for tm access edit excluded")
	case *noAsk && *asks:
		return srvUsage(cmd, "--without-asking or --asks-first, not both")
	case *noAsk || *asks:
		ed.Ask = asks
	}
	switch {
	case f.anySet("text", "file") && key != config.SandboxGuides:
		return srvUsage(cmd, "--text and --file are for tm access edit guide")
	case f.IsSet("text") && f.IsSet("file"):
		return srvUsage(cmd, "--text or --file, not both")
	case f.IsSet("text"):
		ed.Body = text
	case f.IsSet("file"):
		body, err := e.readArg(*file)
		if err != nil {
			return srvFail(cmd, err)
		}
		ed.Body = &body
	}
	table, slug, who, err := e.accessScope(cmd, pos[3:], *all)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return srvFail(cmd, err)
	}
	var was *config.SandboxRule
	theirs := false
	for _, r := range cfg.ThreadSandbox(slug).Rules {
		switch {
		case r.Key != key || r.Value != old:
		case r.Own || slug == "":
			was = &r
		default:
			theirs = true
		}
	}
	switch {
	case was == nil && theirs:
		return srvFail(cmd, &project.Error{Code: "refused", Msg: fmt.Sprintf("%s is all projects': change it with --all, or drop it for %s", old, slug)})
	case was == nil:
		return srvFail(cmd, &project.Error{Code: "refused", Msg: fmt.Sprintf("%s isn't in the %s of %s", old, kind+" rules", who)})
	case !ed.Changed(*was):
		return srvFail(cmd, &project.Error{Code: "refused", Msg: "nothing to change: " + old + " is so already"})
	case value != old:
		if err := config.CheckNewSandboxEntry(key, value); err != nil {
			return srvFail(cmd, &project.Error{Code: "refused", Msg: err.Error()})
		}
	}
	if ed.Body != nil {
		if err := config.CheckGuideBody(config.GuideBody(*ed.Body)); err != nil {
			return srvFail(cmd, &project.Error{Code: "refused", Msg: err.Error()})
		}
	}
	moved, err := config.EditSandboxRule(table, ed)
	if err != nil {
		return srvFail(cmd, accessErr(err))
	}
	word := config.KindName(key) + " " + old
	if value != old {
		word += " → " + value
	}
	detail := key + " ~ " + old
	if value != old {
		detail += " → " + value
	}
	if ed.Ask != nil {
		word += map[bool]string{true: ", asks first", false: ", without asking"}[*ed.Ask]
		detail += fmt.Sprintf(" (ask = %v)", *ed.Ask)
	}
	if ed.Body != nil {
		word += ", its text changed"
	}
	fmt.Fprintf(e.Stdout, "changed for %s: %s\n", who, word)
	if w := config.SandboxWarning(key, value); w != "" && value != old {
		fmt.Fprintf(e.Stdout, "note: %s %s\n", value, w)
	}
	if key == config.SandboxExcluded && value != old {
		fmt.Fprintln(e.Stdout, "note: it runs outside the sandbox, with your full file and network access (tm's socket included)")
	}
	e.accessJournal("settings.sandbox", slug, detail)
	for _, s := range moved {
		fmt.Fprintf(e.Stdout, "%s drops %s now, as it dropped %s\n", s, value, old)
		e.accessJournal("settings.sandbox", s, "drop ~ "+old+" → "+value+" (all projects' rule changed)")
	}
	e.accessNext(slug)
	return nil
}

// accessDrop implements tm access drop|undrop <value> <project>.
func accessDrop(e *Env, verb string, args []string) error {
	cmd := "access " + verb
	if err := e.accessHuman(cmd); err != nil {
		return err
	}
	pos, err := newFlags().Parse(args)
	if err != nil {
		return srvFail(cmd, err)
	}
	if len(pos) < 1 || len(pos) > 2 {
		return srvUsage(cmd, accessUsage)
	}
	value := pos[0]
	table, slug, _, err := e.accessScope(cmd, pos[1:], false)
	if err != nil {
		return err
	}
	if verb == "drop" {
		cfg, err := config.Load()
		if err != nil {
			return srvFail(cmd, err)
		}
		found := false
		for _, r := range cfg.ThreadSandbox(slug).Rules {
			found = found || r.Value == value && !r.Own
		}
		if !found {
			return srvFail(cmd, &project.Error{Code: "refused", Msg: fmt.Sprintf("all projects have no rule %q to drop (tm access show %s lists them)", value, slug)})
		}
	}
	err = config.EditSandbox(table, func(t *config.SandboxTable) error {
		has := t.Has(config.SandboxDrop, value)
		switch {
		case verb == "drop" && has:
			return fmt.Errorf("%s drops %s already", slug, value)
		case verb == "drop":
			t.Add(config.SandboxDrop, value)
		case !has:
			return fmt.Errorf("%s doesn't drop %s", slug, value)
		default:
			t.Remove(config.SandboxDrop, value)
		}
		return nil
	})
	if err != nil {
		return srvFail(cmd, accessErr(err))
	}
	if verb == "drop" {
		fmt.Fprintf(e.Stdout, "%s drops all projects' %s\n", slug, value)
		e.accessJournal("settings.sandbox", slug, "drop + "+value)
	} else {
		fmt.Fprintf(e.Stdout, "%s takes all projects' %s back\n", slug, value)
		e.accessJournal("settings.sandbox", slug, "drop - "+value)
	}
	e.accessNext(slug)
	return nil
}

// accessSet implements tm access set local-ports|escapes|yolo on|off
// [<project> | --all]; yolo on asks first.
func accessSet(e *Env, args []string) error {
	const cmd = "access set"
	if err := e.accessHuman(cmd); err != nil {
		return err
	}
	f := newFlags()
	all, yes := f.Bool("all"), f.Bool("yes")
	pos, err := f.Parse(args)
	if err != nil {
		return srvFail(cmd, err)
	}
	if len(pos) < 2 {
		return srvUsage(cmd, accessUsage)
	}
	key, ok := accessSettings[pos[0]]
	if !ok || pos[1] != "on" && pos[1] != "off" {
		return srvUsage(cmd, "tm access set local-ports|escapes|yolo on|off [<project> | --all]")
	}
	on := pos[1] == "on"
	table, slug, who, err := e.accessScope(cmd, pos[2:], *all)
	if err != nil {
		return err
	}
	if key == "yolo" && on {
		if err := e.confirm(*yes, cmd, "Turn yolo on for "+who+"? "+yoloWarning); err != nil {
			return err
		}
	}
	if key == config.SandboxLocalPorts {
		err = config.EditSandbox(table, func(t *config.SandboxTable) error {
			if slug == "" && !on {
				t.LocalPorts = nil // off is all projects' default
				return nil
			}
			t.LocalPorts = &on
			return nil
		})
	} else if slug == "" {
		err = config.SetDefaults(key, on)
	} else {
		err = config.SetProject(slug, key, on)
	}
	if err != nil {
		return srvFail(cmd, accessErr(err))
	}
	fmt.Fprintf(e.Stdout, "%s %s for %s\n", pos[0], pos[1], who)
	switch {
	case key == config.SandboxLocalPorts && on:
		fmt.Fprintln(e.Stdout, "note: a thread can then connect to every service listening on a local port, and listen on ports itself")
	case key == "yolo" && on:
		fmt.Fprintln(e.Stdout, "note: the sandbox is off for its threads")
	}
	action := "settings.sandbox"
	if key != config.SandboxLocalPorts {
		action = "settings." + key
	}
	e.accessJournal(action, slug, fmt.Sprintf("%s = %v", key, on))
	e.accessNext(slug)
	return nil
}

// accessErr is a write's error as the CLI reports it: the file's own
// form refusal as it is, a check's as refused.
func accessErr(err error) error {
	if err == config.ErrForm {
		return err
	}
	return &project.Error{Code: "refused", Msg: err.Error()}
}
