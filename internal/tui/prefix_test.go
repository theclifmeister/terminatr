package tui

import (
	"maps"
	"slices"
	"testing"
)

// TestPrefixCmdSets: the key sets the prefix commands are told apart by
// are those they always were, made of the one table.
func TestPrefixCmdSets(t *testing.T) {
	set := func(keys ...string) map[string]bool {
		out := map[string]bool{}
		for _, k := range keys {
			out[k] = true
		}
		return out
	}
	for _, c := range []struct {
		name      string
		got, want map[string]bool
	}{
		{"prefixCommands", prefixCommands, set("a", "]", "[", "i", "t", ",", "?", "n")},
		{"popupCommands", popupCommands, set("a", "i", "t", ",", "?", "n")},
		{"paneCommands", paneCommands, set("{", "}", "b", "tab", "|", ";")},
	} {
		if !maps.Equal(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, slices.Sorted(maps.Keys(c.got)), slices.Sorted(maps.Keys(c.want)))
		}
	}
	if len(sessionMenu) != len(prefixCmds) || sessionMenu[0].key != "d" || sessionMenu[len(sessionMenu)-1].key != "prefix" {
		t.Errorf("sessionMenu %v", sessionMenu)
	}
}
