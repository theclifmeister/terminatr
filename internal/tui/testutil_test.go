package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// drainFed are the messages drain feeds back into the dashboard: the ones
// a key's command returns for the model to act on.
var drainFed = []reflect.Type{
	reflect.TypeOf(actionMsg{}),
	reflect.TypeOf(loadedMsg{}),
	reflect.TypeOf(bindingMsg{}),
	reflect.TypeOf(modCursorMsg{}),
	reflect.TypeOf(typeSettledMsg{}),
	reflect.TypeOf(typeTimeoutMsg{}),
	reflect.TypeOf(remotePopupMsg{}),
}

// drainDropped are results a test feeds itself (setData, the mod tabs) or
// does not care about, so drain drops them instead of failing.
var drainDropped = []reflect.Type{
	reflect.TypeOf(dataMsg{}),
	reflect.TypeOf(modInputMsg{}),
	reflect.TypeOf(modViewsMsg{}),
	reflect.TypeOf(previewMsg{}),
	reflect.TypeOf(boardMsg{}),
	reflect.TypeOf(tea.QuitMsg{}),
}

// drain runs cmd as the program would: it walks a tea.BatchMsg or
// tea.Sequence recursively, feeds each message of a type in want (by
// default actionMsg, loadedMsg, bindingMsg and modCursorMsg) back into m, and runs
// the command that returns. A message in drainDropped is ignored; any
// other fails the test, so a
// command is never dropped unseen; list the types a test expects beyond
// the defaults in want.
func drain(t testing.TB, m *dash, cmd tea.Cmd, want ...reflect.Type) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	default:
		typ := reflect.TypeOf(msg)
		// A tea.BatchMsg, or tea.Sequence's unexported message: a slice of
		// commands.
		if typ.Kind() == reflect.Slice && typ.Elem() == reflect.TypeOf(tea.Cmd(nil)) {
			v := reflect.ValueOf(msg)
			for i := 0; i < v.Len(); i++ {
				drain(t, m, v.Index(i).Interface().(tea.Cmd), want...)
			}
			return
		}
		for _, w := range append(append([]reflect.Type{}, drainFed...), want...) {
			if typ == w {
				_, next := m.Update(msg)
				drain(t, m, next, want...)
				return
			}
		}
		for _, d := range drainDropped {
			if typ == d {
				return
			}
		}
		t.Fatalf("drain: unexpected message %T (%+v)", msg, msg)
	}
}
