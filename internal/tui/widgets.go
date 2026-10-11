package tui

import (
	"strings"
	"unicode"

	"github.com/theclifmeister/terminatr/internal/widget"
)

// widgetLook draws the widgets mods publish (internal/widget, mods design
// §6) in this console's theme and icon set, so a mod's view follows the
// theme, the light or dark scheme, NO_COLOR and the icons setting like
// everything tm draws.
type widgetLook struct{}

var _ widget.Look = widgetLook{}

func (widgetLook) Style(role, s string) string {
	t := th()
	switch role {
	case "accent":
		return t.Accent.Render(s)
	case "good":
		return t.Good.Render(s)
	case "warn":
		return t.Warn.Render(s)
	case "bad":
		return t.Bad.Render(s)
	case "info":
		return t.Info.Render(s)
	case "faint":
		return t.Faint.Render(s)
	case "head":
		return t.Head.Render(s)
	case "sel":
		return t.Sel.Render(s)
	}
	return s
}

// Icon is a widget icon's glyph in this console's set. A Nerd Font icon
// draws over the cell after it, so it takes a blank there, as the
// sidebar gives it.
func (widgetLook) Icon(name string) string {
	g := widgetGlyph(ic(), name)
	for _, r := range g {
		if unicode.Is(unicode.Co, r) {
			return g + " "
		}
	}
	return g
}

// ASCII tells an ASCII set by its progress bar, which a loaded set may
// also take from the ascii one.
func (widgetLook) ASCII() bool {
	b := ic().barOn + ic().barOff
	return strings.IndexFunc(b, func(r rune) bool { return r > unicode.MaxASCII }) < 0
}

// widgetGlyph maps widget.Icons, and a progress bar's cells, onto a set.
func widgetGlyph(i *iconSet, name string) string {
	switch name {
	case "working":
		return i.working
	case "blocked":
		return i.blocked
	case "idle":
		return i.idle
	case "starting":
		return i.starting
	case "running":
		return i.running
	case "review":
		return i.review
	case "done":
		return i.done
	case "other":
		return i.other
	case "ci.pass":
		return i.ciPass
	case "ci.fail":
		return i.ciFail
	case "ci.run":
		return i.ciRun
	case "todo.done":
		return i.todoDone
	case "todo.now":
		return i.todoNow
	case "todo.open":
		return i.todoOpen
	case "hint":
		return i.hint
	case "remote":
		return i.remote
	case "paused":
		return i.paused
	case "inactive":
		return i.inactive
	case "folder":
		return i.folder
	case "thread":
		return i.thread
	case "coordinator":
		return i.coord
	case "bar.on":
		return i.barOn
	case "bar.off":
		return i.barOff
	}
	return ""
}
