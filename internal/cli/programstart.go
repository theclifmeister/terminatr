package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/features"
	"github.com/theclifmeister/terminatr/internal/program"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/ticker"
)

// tm program start (the programs design §4.4, §10): a director starts a
// thread for a member project's task. The program's checks come first
// (program.StartCheck: membership, authority, dependencies, no
// --over-cap), then tm thread start's own: the member's start rules,
// pause and caps (project, program, all projects). A cap refusal records
// the member's coordinator as waiting for a slot; a start that passes
// ends the slot held for the director (thread.Started).

const programStartUsage = `usage: tm program start <project>/T<n> [--agent NAME] [--model M] [--approved-by-user] [--json]

Starts a thread in a member project for one of its tasks, as tm thread start
there would. Under authority = "delegate" the director starts within the
caps; under "propose" only with --approved-by-user, once the user said so in
chat. Without it, a task whose dependencies are still open or unknown is
refused (waiting-on). The member's own start_threads setting holds too.
--over-cap is never the director's: a cap refusal waits for a slot-free item.`

func programStart(e *Env, cfg *config.Config, args []string) error {
	if done, err := helpOr(e, args, programStartUsage); done {
		return err
	}
	f := newFlags()
	slug, asJSON := f.String("program"), f.Bool("json")
	agentName, model := f.String("agent"), f.String("model")
	approved, overCap := f.Bool("approved-by-user"), f.Bool("over-cap")
	pos, err := f.Parse(args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("%s", programStartUsage)
	}
	g, err := e.ownProgram(cfg, *slug)
	if err != nil {
		return err
	}
	child, id, err := g.StartCheck(e.Caller, cfg, program.StartRequest{Ref: pos[0], Approved: *approved, OverCap: *overCap}, mergedThreads())
	if err != nil {
		return err
	}
	o := startOpts{task: "T" + strconv.Itoa(id), agent: *agentName, model: *model, approved: *approved, overCap: *overCap}
	if e.Caller.Kind == caller.Director {
		o.program = g.Slug
	}
	return e.threadStart(child, o, *asJSON)
}

// mergedThreads says whether a thread's PR merged, as the ticker last
// saw it (its state file); nil without one.
func mergedThreads() func(slug, id string) bool {
	paths, err := client.ResolvePaths()
	if err != nil {
		return nil
	}
	st := ticker.Load(ticker.StatePath(paths.Sessions))
	return st.Merged
}

// warnWaiting is a coordinator's warning on starting task t of project
// p while what it comes after is open (decision 13: after stays
// advisory for coordinators; only a director's start is refused).
func (e *Env) warnWaiting(cfg *config.Config, p *project.Project, t *tasks.Task) {
	if t == nil || len(t.After)+len(t.AfterX) == 0 || !features.Programs(cfg) {
		return
	}
	prog := cfg.ProgramOf(p.Slug)
	if prog == "" {
		return
	}
	if open := program.Waiting(cfg, prog, p.Slug, t, mergedThreads()); len(open) > 0 {
		fmt.Fprintf(e.Stderr, "warning: %s comes after %s, not met yet: starting anyway\n", t.Ref(), strings.Join(open, ", "))
	}
}
