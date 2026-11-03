package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// tm program accept|decline|leave (the programs design §9.1): this
// server's user answers a paired server's program invite of a project
// here, or takes it out of that program. The Messages tab does the same.

const programMemberUsage = `usage: tm program accept P-… [--project <slug>] [--tasks file|propose] [--starts] [--no-status] [--no-share] [--needs view|answer|off]
       tm program decline P-… [--project <slug>]
       tm program leave [--project <slug>]   (out of the paired server's program it is in)`

// programMember runs accept, decline and leave: the human's.
func programMember(e *Env, verb string, args []string) error {
	f := newFlags()
	slug, tasks, needs := f.String("project"), f.String("tasks"), f.String("needs")
	starts, noStatus, noShare := f.Bool("starts"), f.Bool("no-status"), f.Bool("no-share")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if e.Caller.IsAgent() {
		return &project.Error{Code: "human-only", Msg: "program membership is the user's: tell them, and they answer it (the Messages tab, or tm program accept|decline)"}
	}
	if (verb == "leave") != (len(pos) == 0) || len(pos) > 1 || verb != "accept" && f.anySet("tasks", "needs", "starts", "no-status", "no-share") {
		return usagef("%s", programMemberUsage)
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	if verb == "leave" {
		cfg, _ := config.Load()
		l, in := cfg.ProgramLink(p.Slug)
		if !in {
			return &project.Error{Code: "not-member", Msg: p.Slug + " isn't in a paired server's program"}
		}
		if _, err := link.Leave(e.Caller, &p.Folder, l.Program); err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "%s left %s; its program settings are gone, and %s hears of it\n", p.Slug, link.Shown(l.Program), link.Shown(l.Program))
		return nil
	}
	o := link.AnswerOpts{Accept: verb == "accept", Tasks: config.ProgramTasksFile, Starts: *starts, Status: !*noStatus, Share: !*noShare, NeedsView: true}
	if *tasks != "" {
		o.Tasks = *tasks
	}
	switch *needs {
	case "", proto.NeedsView:
	case proto.NeedsAnswer:
		o.NeedsAnswer = true
	case proto.NeedsOff:
		o.NeedsView = false
	default:
		return usagef("--needs is view, answer or off\n%s", programMemberUsage)
	}
	r, _, err := link.AnswerInvite(e.Caller, p, pos[0], o)
	if err != nil {
		return err
	}
	if o.Accept {
		fmt.Fprintf(e.Stdout, "accepted %s: %s is in %s (%s); %s hears of it\n", r.ID, p.Slug, link.Shown(r.Peer), r.Words(), link.Shown(r.Peer))
		if o.NeedsView {
			fmt.Fprintln(e.Stdout, "if tm server runs, tm peer reload has it read the needs switch now")
		}
		return nil
	}
	fmt.Fprintf(e.Stdout, "declined %s from %s; nothing here changed, %s hears of it\n", r.ID, link.Shown(r.Peer), link.Shown(r.Peer))
	return nil
}
