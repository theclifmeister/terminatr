package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// programNeeds prints tm program needs: the program's rolled-up Needs
// you (the programs design §7.1), as tm's server ranks it; a director
// its own program's, the human any program's. Read only.
func programNeeds(e *Env, args []string) error {
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
	if slug, err = programOfCaller(e, slug, "needs"); err != nil {
		return err
	}
	c, _, err := connect(false) // the menus and dialogs are the server's
	if err != nil {
		return err
	}
	defer c.Close()
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
