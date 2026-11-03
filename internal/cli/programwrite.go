package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// The director's writes (the programs design §4.4, §5): tm program
// route, task, start, send and read. An agent's run goes through the server
// (programForwarded), which tells the caller from the process tree and
// writes the children's folders that the director's own launch rules
// keep it from writing.

// programForwarded are the tm program subcommands an agent runs through
// the server.
var programForwarded = map[string]bool{"route": true, "task": true, "start": true, "send": true, "read": true, "ask": true}

// forwardedProgram reports whether args are a tm program write an
// agent runs in the server.
func forwardedProgram(args []string) bool {
	return len(args) > 1 && args[0] == "program" && programForwarded[args[1]]
}

// ownProgram opens the program a write acts for: a director's own (a
// --program naming another is refused), a coordinator's project's
// program, or for the human the one --program names, else the one
// whose folder the working directory is in.
func (e *Env) ownProgram(cfg *config.Config, flag string) (*program.Program, error) {
	slug := flag
	switch e.Caller.Kind {
	case caller.Director:
		if flag != "" && flag != e.Caller.Project {
			return nil, &project.Error{Code: "other-program", Msg: "a director acts only for its own program"}
		}
		slug = e.Caller.Project
	case caller.Coordinator:
		slug = cfg.ProgramOf(e.Caller.Project)
		if slug == "" || flag != "" && flag != slug {
			return nil, &project.Error{Code: "not-member", Msg: fmt.Sprintf("%s is not a member of a program here", e.Caller.Project)}
		}
	case caller.Human:
		if slug == "" {
			slug = programAt(e.Cwd)
		}
		if slug == "" {
			return nil, usagef("name the program: --program <slug> (tm program list)")
		}
	default:
		return nil, &project.Error{Code: "director-only", Msg: "tm program writes are the director's"}
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return nil, err
	}
	g.Paired = program.PairedVia(e.Caller, g.Slug, e.call)
	return g, nil
}

const programRouteUsage = `usage: tm program route --file <path>|- [--program <slug>] [--json]

The JSON: {"umbrella": "T4" or "a title for a new umbrella task",
 "tasks": [{"key": "api", "project": "acme-api", "title": "…", "notes": "…",
            "steps": ["…"], "priority": "must", "status": "ready", "repo": "…",
            "design": "…", "after": ["infra", "T10", "acme-web/T40"]}]}
Each task goes to a member project's board as ready (or open), filed by the
director; after names an earlier task's key, the same project's T<n> or a
member's <project>/T<n>. All of the tasks are filed, or none.`

func programRoute(e *Env, cfg *config.Config, args []string) error {
	f := newFlags()
	file, slug, asJSON := f.String("file"), f.String("program"), f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *file == "" {
		return usagef("%s", programRouteUsage)
	}
	g, err := e.ownProgram(cfg, *slug)
	if err != nil {
		return err
	}
	text, err := e.readArg(*file)
	if err != nil {
		return err
	}
	var req program.RouteRequest
	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return &project.Error{Code: "invalid-route", Msg: "the route's JSON: " + err.Error()}
	}
	res, err := g.Route(e.Caller, cfg, req)
	if err != nil && (res == nil || len(res.Tasks) == 0) {
		return err
	}
	if *asJSON {
		if perr := e.printJSON(res); perr != nil {
			return perr
		}
		return err
	}
	if res.Umbrella != "" {
		fmt.Fprintf(e.Stdout, "umbrella %s/%s\n", g.Slug, res.Umbrella)
	}
	for _, r := range res.Tasks {
		if r.Ref == "" {
			// A paired member's: filed there, answered by a
			// program-task-filed item.
			fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s %s %s: %s", r.Project, r.Message, r.Result, r.Title)))
			continue
		}
		fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s %s %s: %s", r.Project, r.Ref, r.Result, r.Title)))
	}
	return err
}

const programTaskUsage = `usage: tm program task edit <project>/T<n> [--priority P] [--after T10,acme-web/T40]
                [--design NAME] [--note "…"] [--title "…"] [--repo R] [--status ready|open]
       tm program task show <project>/T<n> | T<n> [--json]
       tm program task add "title" [--priority P] [--notes "…"] [--parts acme-api/T12,…]
                                                  (an umbrella task)
       tm program task list [--json]              (the umbrella tasks, with their parts)
       tm program task edit T<n> [--title "…"] [--priority P] [--note "…"]
                [--parts acme-api/T12,acme-web/T40 | --add-parts acme-web/T41]
 [--program <slug>] on each, for the human.

A director changes a child's task only in these fields: priority, after,
design and a note line; the title, repo, and ready or open only on a task it
filed, before it starts. Other statuses are the child coordinator's, and
done and dropped the user's.

An umbrella's parts are member tasks; a route adds the tasks it files. Its
status follows them (tm sets started, blocked and review); by hand it takes
open or ready, and only the user sets done or dropped.`

func programTask(e *Env, cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return usagef("%s", programTaskUsage)
	}
	sub := args[0]
	f := newFlags()
	slug, asJSON := f.String("program"), f.Bool("json")
	priority, after, design := f.String("priority"), f.String("after"), f.String("design")
	note, notes, title, repo, status := f.String("note"), f.String("notes"), f.String("title"), f.String("repo"), f.String("status")
	parts, addParts := f.String("parts"), f.String("add-parts")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	g, err := e.ownProgram(cfg, *slug)
	if err != nil {
		return err
	}
	if e.Caller.Kind == caller.Coordinator {
		return &project.Error{Code: "director-only", Msg: "tm program task is the director's"}
	}
	switch sub {
	case "list", "ls":
		if len(pos) != 0 {
			return usagef("%s", programTaskUsage)
		}
		if err := taskList(e, g.Tasks(), nil, *asJSON, "", "", false, false); err != nil || *asJSON {
			return err
		}
		return e.umbrellaParts(g)
	case "show":
		if len(pos) != 1 {
			return usagef("%s", programTaskUsage)
		}
		if program.IsRemoteRef(pos[0]) {
			return e.remoteTaskShow(cfg, g, pos[0], *asJSON)
		}
		s, id, err := programTaskStore(cfg, g, pos[0])
		if err != nil {
			return err
		}
		t, err := s.Get(id)
		if err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(tasks.ToJSON(t))
		}
		if _, err = fmt.Fprint(e.Stdout, shownText(tasks.Detail(t))); err != nil || !t.IsUmbrella() || strings.Contains(pos[0], "/") {
			return err
		}
		deps, _ := program.ReadDeps(g.Slug)
		fmt.Fprintln(e.Stdout, shown("\nParts "+program.PartsText(program.Parts(t, deps))))
		return nil
	case "add":
		if len(pos) != 1 {
			return usagef("%s", programTaskUsage)
		}
		var want []string
		if f.IsSet("parts") {
			// Checked before the task is filed, so a bad part files none.
			if want, err = tasks.ReadParts([]string{*parts}); err != nil {
				return err
			}
			for _, x := range want {
				if err := g.CheckCrossRef(cfg, x); err != nil {
					return err
				}
			}
		}
		out, err := g.Tasks().Add(e.Caller, []tasks.NewTask{{Title: pos[0], Priority: *priority, Notes: *notes}})
		if err != nil {
			return err
		}
		r := out[0]
		if r.Result == "failed" {
			return &project.Error{Code: r.Code, Msg: r.Error}
		}
		if len(want) > 0 {
			if _, err := g.SetParts(e.Caller, cfg, r.ID, want, true); err != nil {
				return err
			}
		}
		if *asJSON {
			return e.printJSON(r)
		}
		fmt.Fprintf(e.Stdout, "%s/%s %s\n", g.Slug, r.ID, r.Result)
		return nil
	case "edit":
		if len(pos) != 1 {
			return usagef("%s", programTaskUsage)
		}
		if !strings.Contains(pos[0], "/") {
			return e.umbrellaEdit(cfg, g, pos[0], f, *asJSON, *title, *priority, *note, *parts, *addParts)
		}
		var ch tasks.ProgramChange
		if f.IsSet("priority") {
			ch.Priority = priority
		}
		if f.IsSet("design") {
			ch.Design = design
		}
		if f.IsSet("after") {
			ch.After = []string{*after}
		}
		if f.IsSet("title") {
			ch.Title = title
		}
		if f.IsSet("repo") {
			ch.Repo = repo
		}
		if f.IsSet("status") {
			st, ok := tasks.ParseStatus(*status)
			if !ok {
				return &tasks.Error{Code: "invalid-status", Msg: fmt.Sprintf("%q is not a status", *status)}
			}
			ch.Status = &st
		}
		ch.Note = *note
		if f.IsSet("notes") {
			return usagef("a director adds a note line with --note; it never rewrites a child's notes")
		}
		if f.anySet("parts", "add-parts") {
			return usagef("parts belong to an umbrella task of the program's board: tm program task edit T<n> --parts …")
		}
		if program.IsRemoteRef(pos[0]) {
			sent, err := g.EditRemote(e.Caller, cfg, pos[0], ch)
			if err != nil {
				return err
			}
			return e.printSent(sent, *asJSON)
		}
		res, err := g.EditTask(e.Caller, cfg, pos[0], ch)
		return e.done(res, err, *asJSON, "edited")
	}
	return usagef("unknown subcommand %q\n%s", sub, programTaskUsage)
}

// printSent says what was queued for a paired member.
func (e *Env) printSent(sent *link.ProgramSent, asJSON bool) error {
	if asJSON {
		return e.printJSON(sent)
	}
	what := map[string]string{"task": "program-task", "edit": "program-task", "start": "program-start"}[sent.Kind]
	answer := "a program-task-filed item"
	if sent.Kind == "start" {
		answer = "a program-start-result item"
	}
	ref := sent.Member
	if sent.Ref != "" {
		ref += "/" + sent.Ref
	}
	fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("queued %s %s to %s: %s says how it went", what, sent.ID, ref, answer)))
	return nil
}

// remoteTaskShow prints a paired member's task as it last pushed it
// (program-status), and what the program sent it that waits.
func (e *Env) remoteTaskShow(cfg *config.Config, g *program.Program, ref string, asJSON bool) error {
	key, n, err := program.ChildRef(ref)
	if err != nil {
		return err
	}
	if cfg.ProgramOf(key) != g.Slug {
		return &project.Error{Code: "not-member", Msg: fmt.Sprintf("%s is not a member of program %s", key, g.Slug)}
	}
	full := fmt.Sprintf("%s/T%d", key, n)
	t, ok := link.RemoteTaskOf(full)
	if asJSON {
		if !ok {
			return e.printJSON(map[string]any{"ref": full, "known": false})
		}
		return e.printJSON(map[string]any{"ref": full, "known": true, "task": t})
	}
	if !ok {
		fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("%s: no status from %s yet (it pushes the tasks the director filed or referenced, while its program_status is on)", full, key)))
		return nil
	}
	line := fmt.Sprintf("%s %s: %s", full, t.Status, t.Title)
	if t.PR > 0 {
		line += fmt.Sprintf(" (PR %d", t.PR)
		if t.Merged {
			line += ", merged"
		}
		line += ")"
	}
	fmt.Fprintln(e.Stdout, shown(line))
	if t.URL != "" {
		fmt.Fprintln(e.Stdout, shown("  "+t.URL))
	}
	fmt.Fprintln(e.Stdout, shown("  pushed "+t.Seen.Local().Format("Jan 2 15:04")))
	return nil
}

// umbrellaEdit changes an umbrella task of g's board: its title,
// priority, a note line, or its parts (set, or added to).
func (e *Env) umbrellaEdit(cfg *config.Config, g *program.Program, ref string, f *flagSet, asJSON bool, title, priority, note, parts, addParts string) error {
	n, ok := tasks.ParseRef(ref)
	if !ok {
		return usagef("%q is not T12 or <project>/T12", ref)
	}
	if f.anySet("design", "after", "repo", "status", "notes") {
		return usagef("an umbrella task takes --title, --priority, --note, --parts and --add-parts (its status follows its parts)")
	}
	if f.IsSet("parts") && f.IsSet("add-parts") {
		return usagef("--parts sets the parts, --add-parts adds to them: pass one")
	}
	s := g.Tasks()
	var res tasks.Result
	var err error
	changed := false
	if f.IsSet("title") {
		if res, err = s.Edit(e.Caller, n, &title, nil, nil, nil); err != nil {
			return err
		}
		changed = res.Changed
	}
	if f.IsSet("priority") {
		if res, err = s.SetPriority(e.Caller, n, priority); err != nil {
			return err
		}
		changed = changed || res.Changed
	}
	if f.IsSet("note") {
		if res, err = s.ProgramEdit(e.Caller, g.Slug, n, tasks.ProgramChange{Note: note}); err != nil {
			return err
		}
		changed = changed || res.Changed
	}
	if f.IsSet("parts") || f.IsSet("add-parts") {
		list, add := parts, false
		if f.IsSet("add-parts") {
			list, add = addParts, true
		}
		if res, err = g.SetParts(e.Caller, cfg, ref, []string{list}, add); err != nil {
			return err
		}
		changed = changed || res.Changed
	}
	if res.Task == nil {
		return usagef("nothing to edit: pass --title, --priority, --note, --parts or --add-parts")
	}
	res.Changed = changed
	return e.done(res, nil, asJSON, "edited")
}

// umbrellaParts prints each open umbrella's parts, with their states as
// the ticker last computed them.
func (e *Env) umbrellaParts(g *program.Program) error {
	b, err := g.Tasks().Load()
	if err != nil {
		return err
	}
	deps, _ := program.ReadDeps(g.Slug)
	for _, t := range b.Tasks {
		if t.IsUmbrella() && !t.Status.Closed() {
			fmt.Fprintln(e.Stdout, shown(fmt.Sprintf("  %s parts %s", t.Ref(), program.PartsText(program.Parts(t, deps)))))
		}
	}
	return nil
}

// programTaskStore is the board ref is on: a member's (<project>/T<n>)
// or the program's own (T<n>).
func programTaskStore(cfg *config.Config, g *program.Program, ref string) (*tasks.Store, int, error) {
	if !strings.Contains(ref, "/") {
		n, ok := tasks.ParseRef(ref)
		if !ok {
			return nil, 0, usagef("%q is not T12 or <project>/T12", ref)
		}
		return g.Tasks(), n, nil
	}
	slug, n, err := program.ChildRef(ref)
	if err != nil {
		return nil, 0, err
	}
	if cfg.ProgramOf(slug) != g.Slug {
		return nil, 0, &project.Error{Code: "not-member", Msg: fmt.Sprintf("%s is not a member of program %s", slug, g.Slug)}
	}
	child, err := project.OpenWith(slug, cfg)
	if err != nil {
		return nil, 0, err
	}
	return child.Tasks(), n, nil
}

const programSendUsage = `usage: tm program send <to> "text" | --file <path>|- [--re M-n] [--from <slug>] [--program <slug>]

The program channel: the director writes to a member project's coordinator
(<to> a member's slug); a member's coordinator writes to its program (<to> the
program's slug). Text only, framed as data on the other side, where the item
says how to read it. The human names the sending side with --from.
A paired member (<server>/<project>) gets it over the link; a member of a
paired server's program writes to it as <server>/<program>.`

func programSend(e *Env, cfg *config.Config, args []string) error {
	f := newFlags()
	file, re, from, slug, asJSON := f.String("file"), f.String("re"), f.String("from"), f.String("program"), f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || len(pos) > 2 || len(pos) == 2 && f.IsSet("file") || len(pos) == 1 && !f.IsSet("file") {
		return usagef("%s", programSendUsage)
	}
	if f.IsSet("from") && e.Caller.IsAgent() {
		return usagef("--from is the human's: an agent sends as itself")
	}
	text := ""
	if len(pos) == 2 {
		text = pos[1]
	} else if text, err = e.readArg(*file); err != nil {
		return err
	}
	if m, done, err := e.sendToPairedProgram(cfg, *from, pos[0], link.Opts{Text: text, Re: *re}); done {
		if err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(m)
		}
		fmt.Fprintf(e.Stdout, "queued %s to %s (%s)\n", m.ID, pos[0], link.KB(m.Bytes))
		return nil
	}
	pslug := *slug
	if pslug == "" && !e.Caller.IsAgent() {
		// The human: the program is the side that isn't a project.
		switch {
		case *from != "" && cfg.ProgramOf(*from) != "":
			pslug = cfg.ProgramOf(*from)
		case *from != "":
			pslug = *from
		default:
			pslug = cfg.ProgramOf(pos[0])
		}
	}
	g, err := e.ownProgram(cfg, pslug)
	if err != nil {
		return err
	}
	m, err := g.Send(e.Caller, cfg, *from, pos[0], link.Opts{Text: text, Re: *re})
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(m)
	}
	verb := "sent"
	if m.State == link.StateQueued {
		verb = "queued" // to a paired member, through the outbox
	}
	fmt.Fprintf(e.Stdout, "%s %s to %s (%s)\n", verb, m.ID, pos[0], link.KB(m.Bytes))
	return nil
}

// sendToPairedProgram sends a member project's message to the paired
// server's program it is in (to is "<server>/<program>", as its program
// key names it): its coordinator, or the human with --from. done is
// false when to isn't that.
func (e *Env) sendToPairedProgram(cfg *config.Config, from, to string, o link.Opts) (*link.Message, bool, error) {
	if !program.IsRemote(to) {
		return nil, false, nil
	}
	slug := from
	switch {
	case e.Caller.Kind == caller.Coordinator:
		slug = e.Caller.Project
	case e.Caller.IsAgent():
		return nil, false, nil
	}
	l, in := cfg.ProgramLink(slug)
	if slug == "" || !in || l.Program != to {
		return nil, false, nil
	}
	p, err := project.OpenWith(slug, cfg)
	if err != nil {
		return nil, true, err
	}
	m, err := link.SendProgramRemote(e.Caller, &p.Folder, to, o)
	return m, true, err
}

// programRead prints a program channel message, framed as data: in the
// director's program folder, or a coordinator's project folder.
func programRead(e *Env, cfg *config.Config, args []string) error {
	f := newFlags()
	slug := f.String("program")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("usage: tm program read M-n [--program <slug>]")
	}
	var box *project.Folder
	if e.Caller.Kind == caller.Coordinator {
		p, err := e.openProject("")
		if err != nil {
			return err
		}
		box = &p.Folder
	} else {
		g, err := e.ownProgram(cfg, *slug)
		if err != nil {
			return err
		}
		box = &g.Folder
	}
	m, err := link.Read(box, pos[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(e.Stdout, shownText(link.Frame(box.Slug, m)))
	return err
}
