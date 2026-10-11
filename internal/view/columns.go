package view

import (
	"fmt"
	"slices"
	"strings"
)

// The sidebar's column ids (docs/SPEC.md §4): the grid on the right of a
// full sidebar's rows. They are part of the mod API, so an id is never
// renamed. A mod's own column is "<mod>.<column>" (review-age.age).
const (
	ColCount    = "count"    // a project's open threads, a coordinator's context use, a thread's progress
	ColCI       = "ci"       // a thread's PR checks
	ColState    = "state"    // the coordinator's or the thread's state
	ColRemote   = "remote"   // a coordinator's remote control
	ColPaused   = "paused"   // a paused project
	ColInactive = "inactive" // an inactive project, a dormant row
	ColNeeds    = "needs"    // something here needs you
)

// DefaultColumns is the grid's order when the view names none.
var DefaultColumns = []string{ColCount, ColCI, ColState, ColRemote, ColPaused, ColInactive, ColNeeds}

// SafetyColumns are always shown: an order that leaves one out gets it
// back at its end. Colour is never the only signal, and neither is a
// column a layout dropped.
var SafetyColumns = []string{ColState, ColPaused, ColInactive, ColNeeds}

// MaxColumns bounds the column list a view carries.
const MaxColumns = 32

// maxColID bounds one column id: a mod id and a column id.
const maxColID = 128

// MaxModColumnWidth bounds a mod column's width in cells: the manifest
// check refuses a wider one (internal/mod) and the sidebar never draws
// one wider, so one mod can't push the labels out of the sidebar.
const MaxModColumnWidth = 12

// IsModColumn says whether id names a mod's column, "<mod>.<column>",
// each part [a-z0-9-] and not empty.
func IsModColumn(id string) bool {
	mod, col, ok := strings.Cut(id, ".")
	return ok && idPart(mod) && idPart(col)
}

func idPart(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// validColumns checks a column list read from disk or the wire: its
// size only; Columns drops what it doesn't know.
func validColumns(cols []string) error {
	if len(cols) > MaxColumns {
		return fmt.Errorf("%d sidebar columns, at most %d", len(cols), MaxColumns)
	}
	for _, c := range cols {
		if len(c) > maxColID {
			return fmt.Errorf("a sidebar column id too long")
		}
	}
	return nil
}

// Columns is the grid's column order from cols: the built-in ids and mod
// ids it names, in its order, once each; ids it doesn't know (a newer
// tm's) left out; then any safety column it left out. Empty: the
// default order.
func Columns(cols []string) []string {
	if len(cols) == 0 {
		return DefaultColumns
	}
	out := make([]string, 0, len(cols)+len(SafetyColumns))
	for _, c := range cols {
		if (slices.Contains(DefaultColumns, c) || IsModColumn(c)) && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	for _, c := range SafetyColumns {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// normColumns is cols as Columns orders it, nil for the default order,
// so a view that names none keeps none.
func normColumns(cols []string) []string {
	if len(cols) == 0 {
		return nil
	}
	out := Columns(cols)
	if slices.Equal(out, DefaultColumns) {
		return nil
	}
	return out
}
