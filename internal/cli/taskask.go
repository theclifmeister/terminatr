package cli

import (
	"fmt"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/modaction"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// taskAsk is tm task accept and send-back: the dashboard's A and X as
// commands (docs/SPEC.md §6.3, §4 Accept and send back), the human's
// alone. Like the keys it changes no task: it drops the same accept or
// send-back inbox item, the user's word, for the coordinator to act on.
func taskAsk(e *Env, p *project.Project, s *tasks.Store, kind string, pos []string, note string, noteSet, asJSON bool) error {
	usage := "accept T12"
	if kind == project.KindSendBack {
		usage = `send-back T12 --note "what to change"`
	}
	id, err := oneRef(pos, 1, usage)
	if err != nil {
		return err
	}
	if kind == project.KindAccept && noteSet {
		return usagef("accept takes no note; tm task send-back T12 --note … sends a task back")
	}
	if e.Caller.Kind != caller.Human {
		return &tasks.Error{Code: "human-only", Msg: "only the user accepts a task or sends it back (A or X in the Tasks tab, or tm task " + kind + " in their own terminal); relay their word"}
	}
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	ref := t.Ref()
	if why := modaction.NotAskable(t, kind); why != "" {
		return &tasks.Error{Code: "not-" + kind, Msg: why}
	}
	if kind == project.KindSendBack {
		if note, err = project.SendBackNote(note); err != nil {
			return &tasks.Error{Code: "invalid-note", Msg: err.Error()}
		}
	}
	items, err := p.Inbox()
	if err != nil {
		return err
	}
	if k := project.TaskAsked(items, ref); k != "" {
		if k != kind {
			return &tasks.Error{Code: "already-asked", Msg: fmt.Sprintf("%s waits on the coordinator already (%s)", ref, k)}
		}
		if asJSON {
			return e.printJSON(map[string]any{"task": ref, "kind": kind, "asked": false})
		}
		fmt.Fprintf(e.Stdout, "%s waits on the coordinator already (%s)\n", ref, k)
		return nil
	}
	var asked bool
	if kind == project.KindAccept {
		asked, err = p.AskAccept(e.Caller, ref)
	} else {
		asked, err = p.AskSendBack(e.Caller, ref, note)
	}
	if err != nil {
		return err
	}
	if asJSON {
		return e.printJSON(map[string]any{"task": ref, "kind": kind, "asked": asked})
	}
	switch {
	case !asked:
		fmt.Fprintf(e.Stdout, "%s waits on the coordinator already\n", ref)
	case kind == project.KindAccept:
		fmt.Fprintf(e.Stdout, "told the coordinator you accept %s; it marks it done\n", ref)
	default:
		fmt.Fprintf(e.Stdout, "sent %s back with your note; the coordinator passes it on\n", ref)
	}
	return nil
}
