package cli

import (
	"bufio"
	"cmp"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
)

// tm program (the programs design §4.4): programs group projects under a
// director agent. Every command refuses programs-off while [features]
// programs is off, but for the human's two that manage dormant data
// (§3.5): status and delete.

const programUsage = `usage: tm program new <slug> [--goal "…"] [--json]   (the human's; members are set in Settings or config.toml)
       tm program list [--json]
       tm program show <slug> [--json]
       tm program open <slug> [--agent NAME]   (start or attach its director)
       tm program context [<slug>] [--json]   (the director's context; a director reads its own program's)
       tm program route --file <path>|-   (file one task per member project; tm program route --help)
       tm program task edit <project>/T<n> …   (priority, after, design, a note; tm program task --help)
       tm program start <project>/T<n> [--agent NAME] [--model M] [--approved-by-user]   (a thread in a member; tm program start --help)
       tm program search <words>… [--project p]   (the members' histories, as a thread sees them; tm program search --help)
       tm program send <to> "text" [--re M-n]   (the program channel, director and member coordinators)
       tm program read M-n   (a program channel message, framed as data)
       tm program status [--json]   (the human's: on or off, and the programs, dormant while off)
       tm program delete <slug> [--yes]   (the human's: its folder to the trash, its table out of config.toml; asks first)`

func runProgram(e *Env, args []string) error {
	if len(args) == 0 {
		return usagef("%s", programUsage)
	}
	cfg, _ := config.Load() // a safety error still gives the tables
	switch args[0] {
	case "status":
		return programStatus(e, cfg, args[1:])
	case "delete":
		return programDelete(e, args[1:])
	}
	if !features.Programs(cfg) {
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
	case "route":
		return programRoute(e, cfg, args[1:])
	case "task":
		return programTask(e, cfg, args[1:])
	case "search":
		return programSearch(e, cfg, args[1:])
	case "start":
		return programStart(e, cfg, args[1:])
	case "send":
		return programSend(e, cfg, args[1:])
	case "read":
		return programRead(e, cfg, args[1:])
	}
	return usagef("unknown subcommand %q\n%s", args[0], programUsage)
}

// programsOff is the refusal of every program command while programs
// are off.
func programsOff() error {
	return &project.Error{Code: features.OffCode, Msg: features.OffMsg}
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

// programStatus prints tm program status, the human's whatever the
// switch says: "programs: off, 2 programs dormant", then each with its
// members.
func programStatus(e *Env, cfg *config.Config, args []string) error {
	f := newFlags()
	asJSON := f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usagef("%s", programUsage)
	}
	if e.Caller.IsAgent() {
		return &project.Error{Code: "human-only", Msg: "tm program status is the user's"}
	}
	on := features.Programs(cfg)
	list, err := program.List(cfg)
	if err != nil {
		return err
	}
	if *asJSON {
		if list == nil {
			list = []program.Summary{}
		}
		return e.printJSON(map[string]any{"on": on, "programs": list})
	}
	fmt.Fprintln(e.Stdout, ProgramStatusLine(on, len(list)))
	tw := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	for _, s := range list {
		fmt.Fprintf(tw, "  %s\t%s\n", s.Slug, cmp.Or(strings.Join(s.Members, ", "), "(no members)"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if !on && len(list) > 0 {
		fmt.Fprintln(e.Stdout, "Their data is kept; the user turns programs back on in Settings → all projects → Features, or clears one with tm program delete <slug>.")
	}
	return nil
}

// ProgramStatusLine is tm program status's first line.
func ProgramStatusLine(on bool, n int) string {
	word := "programs"
	if n == 1 {
		word = "program"
	}
	if !on {
		if n == 0 {
			return "programs: off"
		}
		return fmt.Sprintf("programs: off, %d %s dormant", n, word)
	}
	return fmt.Sprintf("programs: on, %d %s", n, word)
}

// programDelete is tm program delete <slug>, the human's whatever the
// switch says: asks first (the slug typed on a terminal, else --yes),
// and refuses while the program's director runs.
func programDelete(e *Env, args []string) error {
	f := newFlags()
	yes := f.Bool("yes")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("%s", programUsage)
	}
	slug := pos[0]
	if e.Caller.IsAgent() {
		return &project.Error{Code: "human-only", Msg: "only the user deletes programs"}
	}
	if _, err := program.Dir(slug); err != nil {
		return err
	}
	if c, _, err := connect(false); err == nil {
		var res proto.SessionListResult
		err := c.Call(proto.MethodSessionList, nil, &res)
		c.Close()
		if err != nil {
			return fromProto(err)
		}
		for _, s := range res.Sessions {
			if s.Role == proto.RoleDirector && s.Project == slug {
				return &project.Error{Code: "director-running", Msg: fmt.Sprintf("program %s's director is running (%s): stop it first", slug, s.ID)}
			}
		}
	}
	if !*yes {
		if !stdinTTY(e) {
			return usagef("tm program delete %s moves the program to the trash and removes its settings: confirm with --yes", slug)
		}
		fmt.Fprintf(e.Stdout, "Delete program %s? Its folder moves to the trash and its [programs.%s] table leaves config.toml; its projects stay as they are. Type %s to confirm: ", slug, slug, slug)
		line, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != slug {
			return &project.Error{Code: "not-confirmed", Msg: "nothing deleted"}
		}
	}
	dst, err := program.Delete(e.Caller, slug, time.Now())
	if err != nil {
		return err
	}
	if dst == "" {
		fmt.Fprintf(e.Stdout, "deleted program %s: its settings are gone (it had no folder)\n", slug)
		return nil
	}
	fmt.Fprintf(e.Stdout, "deleted program %s: moved to %s\n", slug, dst)
	return nil
}
