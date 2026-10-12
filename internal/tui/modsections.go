package tui

import (
	"cmp"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/widget"
)

// Panel sections of user mods (mods design §12, Phase 2; docs/SPEC.md
// §4, Mod views): a mod's details.section is a section at the bottom of
// the dashboard's details panel, for the project, task or thread the
// panel shows; its coordinator.section one at the bottom of the
// coordinator's info panel, for its project. The core's sections come
// first, and each mod section's heading carries "mod: <id>". The console
// says which it shows (mod.views), naming the task or thread, so the mod
// hears view.opened for exactly that row and publishes for it; a mod
// that drew nothing for the row adds nothing. Sections show: they take
// no keys or clicks (a mod's interactive panels are its tabs, cards and
// full views), so no cursor row is drawn either.

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

// sectionTree is the tree section v shows for project's task or thread
// (both "": the project's own): the one published for it, else, for the
// project's own, the one for all projects; nil while there is none.
func sectionTree(v proto.ModView, project, task, thr string) *modview.Tree {
	i := slices.IndexFunc(v.Trees, func(t modview.Tree) bool { return t.For(project, task, thr) })
	if i < 0 && task == "" && thr == "" {
		i = slices.IndexFunc(v.Trees, func(t modview.Tree) bool { return t.For("", "", "") })
	}
	if i < 0 || v.Trees[i].Tree == nil {
		return nil
	}
	return &v.Trees[i]
}

// modSections adds the sections of slot for project's task or thread to
// d: per mod a heading marked as its own, then its tree, at most the
// lines it asks for (modview.MaxHeight). A mod that failed says so; one
// that drew nothing for the row adds nothing.
func modSections(d *panel, list []proto.ModView, slot, project, task, thr string) {
	w := max(d.w-2, 1)
	for _, v := range sections(list, slot, project) {
		var lines []string
		if v.State == proto.ModViewFailed {
			lines = failedLines(v, w)
		} else if t := sectionTree(v, project, task, thr); t != nil {
			h := cmp.Or(t.Height, modview.MaxHeight)
			view := widget.NewView()
			view.Set(t.Tree)
			lines = view.Render(w, h, sectionLook{})
		}
		if len(lines) == 0 {
			continue
		}
		d.section(v.Title, "mod: "+v.Mod)
		for _, l := range lines {
			d.add(l)
		}
	}
}

// sectionsShown are the sections of slot a panel w cells wide shows for
// project's task or thread, for mod.views.
func sectionsShown(list []proto.ModView, slot, project, task, thr string, w int) []modview.Shown {
	var out []modview.Shown
	for _, v := range sections(list, slot, project) {
		out = append(out, modview.Shown{Mod: v.Mod, View: v.View, Slot: v.Slot, Project: project, Task: task, Thread: thr,
			Width: max(w-2, 1), Height: modview.MaxHeight})
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
