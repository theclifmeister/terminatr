package cli

import (
	"fmt"
	"strings"

	"github.com/theclifmeister/terminatr/internal/project"
)

const designUsage = `usage: tm design <command> [--project <slug>] [--json]

  set NAME [--title "…"] [--task T12] [--artifact URL] [--answer "…"]... [--answers-file F|-]
                                record a design: its title, the task that made it, its page
                                (an https URL) and the user's decided answers, one line each
                                (--answer again for each, or one per line of a file; they
                                replace the ones recorded, and --answer "" clears them)
  list [--all] [--archived]     the designs, recorded or named by a task, with their status;
                                --all adds the done ones, --archived the archived ones
  archive NAME | unarchive NAME hide a design (any status) wherever designs are listed,
                                until it is unarchived; on the user's word

The tasks that build a design name it (tm task edit T12 --design NAME --after T10,T11);
with the vault on, tm renders designs/NAME.md and designs/NAME.canvas from both.
Exit codes: 0 done, 1 refused, 2 usage, 3 I/O.`

// designJSON is a design as tm design list --json gives it: its record
// and its status from its build.
type designJSON struct {
	project.Design
	Status string `json:"status"`
}

// runDesign is the design store (docs/SPEC.md §5.1, designs.json).
func runDesign(e *Env, args []string) error {
	if done, err := helpOr(e, args, designUsage); done {
		return err
	}
	sub := args[0]
	f := newFlags()
	slug, asJSON := f.String("project"), f.Bool("json")
	title, task, artifact := f.String("title"), f.String("task"), f.String("artifact")
	answers, answersFile := f.List("answer"), f.String("answers-file")
	all, archived := f.Bool("all"), f.Bool("archived")
	pos, err := f.Parse(args[1:])
	if err != nil {
		return err
	}
	p, err := e.openProject(*slug)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "ls":
		if len(pos) != 0 || f.anySet("title", "task", "artifact", "answer", "answers-file") {
			return usagef("%s", designUsage)
		}
		builds, err := p.BoardDesigns()
		if err != nil {
			return err
		}
		ds := []designJSON{}
		for _, b := range builds {
			if b.Shown(*all, *archived) {
				ds = append(ds, designJSON{Design: b.Design, Status: b.Status()})
			}
		}
		if *asJSON {
			return e.printJSON(ds)
		}
		for _, d := range ds {
			line := d.Name + " · " + d.Status
			if d.Archived {
				line += " · archived"
			}
			if d.Title != "" {
				line += " · " + d.Title
			}
			if d.Task != "" {
				line += " · " + d.Task
			}
			line += fmt.Sprintf(" · %d answers", len(d.Answers))
			if d.Artifact != "" {
				line += " · " + d.Artifact
			}
			fmt.Fprintln(e.Stdout, shown(line))
		}
		return nil
	case "archive", "unarchive":
		if len(pos) != 1 || f.anySet("title", "task", "artifact", "answer", "answers-file", "all", "archived") {
			return usagef("%s", designUsage)
		}
		if err := e.coordinatorOnly(p, "tm design "+sub); err != nil {
			return err
		}
		d, changed, err := p.ArchiveDesign(pos[0], sub == "archive")
		if err != nil {
			return err
		}
		if changed {
			if err := p.Journal(e.Caller, "design."+sub, d.Name, ""); err != nil {
				return err
			}
		}
		if *asJSON {
			return e.printJSON(map[string]any{"design": d, "changed": changed})
		}
		if changed {
			fmt.Fprintf(e.Stdout, "%sd design %s\n", sub, d.Name)
		} else {
			fmt.Fprintf(e.Stdout, "design %s: already %sd\n", d.Name, sub)
		}
		return nil
	case "set":
		if len(pos) != 1 {
			return usagef("%s", designUsage)
		}
		if err := e.coordinatorOnly(p, "tm design set"); err != nil {
			return err
		}
		if f.anySet("answer") && f.anySet("answers-file") {
			return usagef("set takes --answer or --answers-file, not both")
		}
		var ed project.DesignEdit
		if f.IsSet("title") {
			ed.Title = title
		}
		if f.IsSet("task") {
			ed.Task = task
		}
		if f.IsSet("artifact") {
			ed.Artifact = artifact
		}
		if f.IsSet("answer") {
			ed.Answers = answers
		}
		if f.IsSet("answers-file") {
			text, err := e.readArg(*answersFile)
			if err != nil {
				return err
			}
			lines := strings.Split(text, "\n")
			ed.Answers = &lines
		}
		d, changed, err := p.SetDesign(pos[0], ed)
		if err != nil {
			return err
		}
		if changed {
			if err := p.Journal(e.Caller, "design.set", d.Name, ""); err != nil {
				return err
			}
		}
		if *asJSON {
			return e.printJSON(map[string]any{"design": d, "changed": changed})
		}
		if changed {
			fmt.Fprintf(e.Stdout, "set design %s\n", d.Name)
		} else {
			fmt.Fprintf(e.Stdout, "design %s: nothing changed\n", d.Name)
		}
		return nil
	}
	return usagef("unknown subcommand %q\n%s", sub, designUsage)
}
