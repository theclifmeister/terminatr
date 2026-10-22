// Package tui is the client: the dashboard and the single attached pane
// (docs/SPEC.md §4).
//
// State the server echoes back into the dashboard (selection, cursor,
// folds) needs a pending guard in fromView (selPending, treePending): a
// view update that arrives while this console's own change is on its way
// must not overwrite it.
package tui
