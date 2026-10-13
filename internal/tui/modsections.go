package tui

import (
	"cmp"
	"strings"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// Panel sections of user mods (mods design §12, Phase 2; docs/SPEC.md
// §4, Mod views): a mod's details.section is a section of the
// dashboard's details panel, for the project, task or thread the panel
// shows, and of a thread's info panel; its coordinator.section one of
// the coordinator's info panel, for its project. The console says which
// it shows (mod.views), naming the task or thread, so the mod hears
// view.opened for exactly that row and publishes for it; a mod that drew
// nothing for the row adds nothing. Where they go among core's blocks,
// their headers, folds and budget are panelsections.go's. Their bodies
// take no keys (a mod's interactive panels are its tabs, cards and full
// views), so no cursor row is drawn in them.

// sectionTarget is the project, task and thread a details panel for r
// shows mod sections for; ok false for a row that has none (a session,
// a card).
func sectionTarget(r row) (project, task, thr string, ok bool) {
	switch {
	case r.card || r.project == "":
		return "", "", "", false
	case r.task != nil:
		return r.project, r.task.Ref(), "", true
	case r.thread != nil:
		return r.project, "", r.thread.ID, true
	case strings.HasPrefix(r.key, "p:"):
		return r.project, "", "", true
	}
	return "", "", "", false
}

// sections are the views of slot of the mods running for project, in
// order.
func sections(list []proto.ModView, slot, project string) []proto.ModView {
	var out []proto.ModView
	for _, v := range list {
		if v.Slot == slot && runsFor(v, project) {
			out = append(out, v)
		}
	}
	return out
}

// failedLines say mod view v failed, with its log and how to restart it.
func failedLines(v proto.ModView, w int) []string {
	out := []string{th().Bad.Render(fit(ic().blocked+" mod failed: "+cmp.Or(v.Err, "its host stopped"), w)) + reset}
	if v.Log != "" {
		out = append(out, faintLines("Its log: "+v.Log, w)...)
	}
	return append(out, faintLines("Restart it in the Mods tab of the settings (,): r.", w)...)
}

// sectionLook is the console's widget look without the cursor's row: a
// section takes no keys, so no row is selected in it.
type sectionLook struct{ widgetLook }

func (l sectionLook) Style(role, s string) string {
	if role == "sel" {
		return s
	}
	return l.widgetLook.Style(role, s)
}
