package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/history"
	"github.com/theclifmeister/terminatr/internal/proto"
)

const historyUsage = `usage: tm history [--project <slug>] [--json]

A project's daily numbers, from tm's own records (the task list, its
archive and the journal), never git: the first record, the counts of
done, open and dropped tasks, the best day, the typical time from start
to done (the median, from 10 timed tasks), then done per day, only
the days with work, in this machine's time zone.`

// runHistory is tm history (docs/SPEC.md §5.1, History). Forwarded, it
// runs in the server, on the server's index; with no server it reads the
// records itself, the same way.
func runHistory(e *Env, args []string) error {
	if len(args) > 0 { // bare, it answers for the project it is run in
		if done, err := helpOr(e, args, historyUsage); done {
			return err
		}
	}
	f := newFlags()
	slug, asJSON := f.String("project"), f.Bool("json")
	pos, err := f.Parse(args)
	if err != nil {
		return usagef("%v\n%s", err, historyUsage)
	}
	if len(pos) != 0 {
		return usagef("%s", historyUsage)
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	h, err := history.Read(p)
	if err != nil {
		return err
	}
	if *asJSON {
		return e.printJSON(h)
	}
	printHistory(e, h)
	return nil
}

func printHistory(e *Env, h proto.History) {
	if h.FirstRecord == "" {
		fmt.Fprintln(e.Stdout, "no records yet")
		return
	}
	fmt.Fprintf(e.Stdout, "first record  %s\n", h.FirstRecord)
	fmt.Fprintf(e.Stdout, "tasks         %d done, %d open, %d dropped\n", h.Done, h.Open, h.Dropped)
	if h.BestDay != nil {
		fmt.Fprintf(e.Stdout, "best day      %d on %s\n", h.BestDay.Done, h.BestDay.Day)
	}
	if h.Typical != "" {
		fmt.Fprintf(e.Stdout, "typical       %s, from %d timed tasks\n", h.Typical, h.Timed)
	} else {
		fmt.Fprintf(e.Stdout, "typical       shows from %d timed tasks; %d so far\n", proto.TypicalFrom, h.Timed)
	}
	for _, d := range h.Days {
		fmt.Fprintf(e.Stdout, "%s  %d\n", d.Day, d.Done)
	}
}
