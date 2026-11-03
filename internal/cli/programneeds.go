package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/config"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// programNeeds prints tm program needs: the program's rolled-up Needs
// you (the programs design §7.1), as tm's server ranks it; a director
// its own program's, the human any program's. Read only; --open has the
// consoles open the program's needs dialog over the director's pane for
// the user (§7.3), which the director never fills.
func programNeeds(e *Env, args []string) error {
	f := newFlags()
	asJSON, open := f.Bool("json"), f.Bool("open")
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
	if slug, err = programOfCaller(e, slug, "needs"); err != nil {
		return err
	}
	c, _, err := connect(false) // the menus and dialogs are the server's
	if err != nil {
		return err
	}
	defer c.Close()
	if *open {
		var res proto.ProgramNeedsOpenResult
		if err := c.Call(proto.MethodProgramNeedsOpen, proto.ProgramNeedsParams{Program: slug}, &res); err != nil {
			return err
		}
		if *asJSON {
			return e.printJSON(res)
		}
		if res.Waiting == 0 {
			_, err = fmt.Fprintf(e.Stdout, "nothing waits on the user in %s: no dialog opened\n", slug)
			return err
		}
		_, err = fmt.Fprintf(e.Stdout, "asked: the program's needs dialog opens over the director's pane for the user (%d waiting); they answer it there, not you\n", res.Waiting)
		return err
	}
	var res proto.ProgramNeedsResult
	if err := c.Call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: slug}, &res); err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(res)
	}
	_, err = fmt.Fprint(e.Stdout, project.RenderContext([]project.Section{program.NeedsSection(&res)}))
	return err
}

// programOfCaller is the program a director's or the human's tm program
// <what> reads: a director's own (naming another is other-program), the
// human's named one.
func programOfCaller(e *Env, slug, what string) (string, error) {
	switch e.Caller.Kind {
	case caller.Director:
		if slug != "" && slug != e.Caller.Project {
			return "", &project.Error{Code: "other-program", Msg: "a director reads only its own program's " + what}
		}
		return e.Caller.Project, nil
	case caller.Human:
	default:
		return "", &project.Error{Code: "director-only", Msg: "tm program " + what + " is the director's (and the human's)"}
	}
	if slug == "" {
		return "", usagef("name the program: tm program %s <slug> (tm program list)\n%s", what, programUsage)
	}
	return slug, nil
}

// liveProgramNeeds is program slug's rolled-up Needs you from tm's
// server, nil when it doesn't answer: the context part says so.
func liveProgramNeeds(slug string) *proto.ProgramNeedsResult {
	c, _, err := connect(false)
	if err != nil {
		return nil
	}
	defer c.Close()
	var res proto.ProgramNeedsResult
	if c.Call(proto.MethodProgramNeeds, proto.ProgramNeedsParams{Program: slug}, &res) != nil {
		return nil
	}
	return &res
}

const programWakeUsage = `usage: tm program wake <project> [--program <slug>] [--json]

Starts a member project's coordinator when it isn't running, as enter on
its coordinator row does, so the tasks you filed and the messages you sent
there don't wait until the user opens the project. It starts no threads, so
a director may under propose and delegate. A coordinator already running is
left as it is. Refused for a project that isn't a local member of your
program (a paired member's coordinator is its user's), and for an inactive
or paused one: those stay the user's.`

// programWake runs tm program wake: the server's program.wake, which
// checks the caller (the program's own director, or the human) and the
// member; the human's program defaults to the member's own.
func programWake(e *Env, cfg *config.Config, args []string) error {
	if done, err := helpOr(e, args, programWakeUsage); done {
		return err
	}
	f := newFlags()
	slug, asJSON := f.String("program"), f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("%s", programWakeUsage)
	}
	member := pos[0]
	switch e.Caller.Kind {
	case caller.Director:
		if *slug != "" && *slug != e.Caller.Project {
			return &project.Error{Code: "other-program", Msg: "a director wakes only its own program's members"}
		}
		*slug = e.Caller.Project
	case caller.Human:
		if *slug == "" {
			*slug = cfg.ProgramOf(member)
		}
		if *slug == "" {
			return &project.Error{Code: "not-member", Msg: member + " is not a member of a program here (tm program list)"}
		}
	default:
		return &project.Error{Code: "director-only", Msg: "tm program wake is the director's (and the human's)"}
	}
	c, _, err := connect(false) // the coordinator is the server's to start
	if err != nil {
		return err
	}
	defer c.Close()
	var res proto.ProgramOpenResult
	if err := c.Call(proto.MethodProgramWake, proto.ProgramWakeParams{Program: *slug, Project: member}, &res); err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(map[string]any{"project": member, "session": res.Session, "started": res.Started})
	}
	if res.Started {
		_, err = fmt.Fprintf(e.Stdout, "woke %s's coordinator (%s): it works from its inbox\n", member, res.Session)
	} else {
		_, err = fmt.Fprintf(e.Stdout, "%s's coordinator is already running (%s)\n", member, res.Session)
	}
	return err
}
