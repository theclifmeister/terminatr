package cli

import (
	"cmp"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
)

// tm program (the programs design §4.4): programs group projects under a
// director agent. Every command refuses programs-off while [features]
// programs is off.

const programUsage = `usage: tm program new <slug> [--goal "…"] [--json]   (the human's; members are set in Settings or config.toml)
       tm program list [--json]
       tm program show <slug> [--json]
       tm program open <slug> [--agent NAME]   (start or attach its director)
       tm program context [<slug>] [--json]   (the director's context; a director reads its own program's)`

func runProgram(e *Env, args []string) error {
	if len(args) == 0 {
		return usagef("%s", programUsage)
	}
	cfg, _ := config.Load() // a safety error still gives the tables
	if !cfg.ProgramsOn() {
		return programsOff()
	}
	switch args[0] {
	case "new":
		return programNew(e, args[1:])
	case "list", "ls":
		return programList(e, cfg, args[1:])
	case "show":
		return programShow(e, cfg, args[1:])
	case "open":
		return programOpen(e, args[1:])
	case "context":
		return programContext(e, cfg, args[1:])
	}
	return usagef("unknown subcommand %q\n%s", args[0], programUsage)
}

// programsOff is the refusal of every program command while programs
// are off.
func programsOff() error {
	return &project.Error{Code: config.ProgramsOff, Msg: "programs are off on this server; the user turns them on in Settings → all projects → Features"}
}

func programNew(e *Env, args []string) error {
	f := newFlags()
	goal, asJSON := f.String("goal"), f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("%s", programUsage)
	}
	if e.Caller.IsAgent() {
		return &project.Error{Code: "human-only", Msg: "only the human creates programs"}
	}
	g, err := program.New(program.Options{Slug: pos[0], Goal: *goal, Taken: peerTaken})
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(map[string]any{"slug": g.Slug, "dir": g.Dir, "goal": g.Meta.Goal, "members": g.Members})
	}
	fmt.Fprintf(e.Stdout, "created program %s at %s\n", g.Slug, g.Dir)
	if len(g.Members) == 0 {
		fmt.Fprintf(e.Stdout, "it has no members yet: add projects in config.toml's [programs.%s] projects, then tm program open %s\n", g.Slug, g.Slug)
	}
	return nil
}

// peerTaken says why slug can't name a program for the paired servers
// (the one namespace, §3.4): this server's own name, or a paired
// server's alias, name or the name it gives itself, where <name>/T4
// would read as either. "" when none has it.
func peerTaken(slug string) string {
	if slug == peer.SelfName() {
		return "it is this server's own name"
	}
	dir, err := peer.Dir()
	if err != nil {
		return ""
	}
	peers, _ := peer.LoadPeers(dir)
	for _, p := range peers {
		if p.Alias == slug || p.Name == slug || p.Told == slug {
			return "a paired server is called that"
		}
	}
	return ""
}

func programList(e *Env, cfg *config.Config, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usagef("%s", programUsage)
	}
	list, err := program.List(cfg)
	if err != nil {
		return err
	}
	if *asJSON {
		if list == nil {
			list = []program.Summary{}
		}
		return e.printJSON(list)
	}
	if len(list) == 0 {
		fmt.Fprintln(e.Stdout, "no programs (tm program new <slug>)")
		return nil
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	for _, s := range list {
		note := s.Goal
		switch {
		case s.Error != "":
			note = "unreadable: " + s.Error
		case s.LeftOut:
			note = "left out: a project has its name"
		case !s.Folder:
			note = "no folder yet"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Slug, cmp.Or(strings.Join(s.Members, ","), "-"), note)
	}
	return tw.Flush()
}

func programShow(e *Env, cfg *config.Config, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("%s", programUsage)
	}
	g, err := program.Open(pos[0], cfg)
	if err != nil {
		return err
	}
	t, _ := cfg.Program(g.Slug)
	if *asJSON {
		return e.printJSON(map[string]any{"slug": g.Slug, "dir": g.Dir, "goal": g.Meta.Goal, "members": g.Members,
			"authority": t.Authority, "parallel_threads": t.ParallelThreads, "left_out": g.LeftOut})
	}
	fmt.Fprintf(e.Stdout, "Program: %s\nFolder: %s\nGoal: %s\nMembers: %s\nAuthority: %s\n", g.Slug, g.Dir,
		cmp.Or(g.Meta.Goal, "(none set)"), cmp.Or(strings.Join(g.Members, ", "), "(none)"), cmp.Or(t.Authority, config.AuthorityPropose))
	if g.LeftOut {
		fmt.Fprintf(e.Stdout, "Left out: a project is called %s too (tm doctor)\n", g.Slug)
	}
	return nil
}

func programOpen(e *Env, args []string) error {
	f := newFlags()
	agentName := f.String("agent")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("%s", programUsage)
	}
	if e.Caller.IsAgent() {
		return &project.Error{Code: "human-only", Msg: "the human opens programs"}
	}
	if _, err := program.Open(pos[0], nil); err != nil {
		return err
	}
	return e.openSession([]string{"program", "open", pos[0]}, *agentName, func(call func(string, any, any) error, cols, rows int) (string, error) {
		return openDirector(call, pos[0], *agentName, cols, rows)
	})
}

// openDirector is server.OpenDirector over a server connection's call.
func openDirector(call func(method string, params, result any) error, slug, agentName string, cols, rows int) (string, error) {
	list := func() ([]proto.SessionInfo, error) {
		var res proto.SessionListResult
		err := call(proto.MethodSessionList, nil, &res)
		return res.Sessions, err
	}
	start := func(p proto.SessionStartParams) (string, error) {
		var started proto.SessionStartResult
		if err := call(proto.MethodSessionStart, p, &started); err != nil {
			return "", err
		}
		return started.Session.ID, nil
	}
	id, started, err := server.OpenDirector(list, start, slug, agentName, uint16(cols), uint16(rows))
	if err == nil && started {
		if g, gerr := program.Open(slug, nil); gerr == nil {
			_ = g.Journal(caller.Caller{Kind: caller.Human}, "director.start", id, "")
		}
	}
	return id, err
}

// programContext prints tm program context: a director its own
// program's (no other), the human any program's.
func programContext(e *Env, cfg *config.Config, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usagef("%s", programUsage)
	}
	slug := ""
	if len(pos) == 1 {
		slug = pos[0]
	}
	switch e.Caller.Kind {
	case caller.Director:
		if slug != "" && slug != e.Caller.Project {
			return &project.Error{Code: "other-program", Msg: "a director reads only its own program's context"}
		}
		slug = e.Caller.Project
	case caller.Human:
	default:
		return &project.Error{Code: "director-only", Msg: "tm program context is the director's (and the human's)"}
	}
	if slug == "" {
		return usagef("name the program: tm program context <slug> (tm program list)\n%s", programUsage)
	}
	g, err := program.Open(slug, cfg)
	if err != nil {
		return err
	}
	secs, err := g.Context(cfg)
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(secs)
	}
	_, err = fmt.Fprint(e.Stdout, project.RenderContext(secs))
	return err
}
