package tui

import (
	"slices"
	"testing"
)

// TestSideMenuSame: a sidebar row's menu is the same on the dashboard
// and in a session, item for item, but for how each names the keys: the
// dashboard its own keys and enter, a session prefix+ for the popups;
// a session without the dashboard has no popups.
func TestSideMenuSame(t *testing.T) {
	rows := []treeRow{
		{kind: treeProject, slug: "alpha"},
		{kind: treeProject, slug: "beta", inactive: true, pinned: true},
		{kind: treeDivider, fold: true},
		{kind: treeCoordinator, slug: "alpha"},
		{kind: treePeerProject, peer: "nas", slug: "gamma"},
		{kind: treeThread, slug: "alpha", thread: "t-0001", session: "s-3"},
	}
	labels := func(items []sideMenuItem) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.label)
		}
		return out
	}
	want := [][]string{
		{"show its dashboard", "open its coordinator", "fold", "project popup", "tasks", "inbox", "deactivate", "pin to the top"},
		{"show its dashboard", "expand", "project popup", "tasks", "inbox", "activate", "unpin"},
		{"unfold"},
		{"open the coordinator", "project popup"},
		{"show its dashboard", "fold"},
		{"attach"},
	}
	for i, r := range rows {
		got := labels(sideMenuItems(r, true))
		if !slices.Equal(got, want[i]) {
			t.Errorf("%v: %q, want %q", r.kind, got, want[i])
		}
		c := &client{dashboard: true}
		var session []string
		for _, it := range c.sideItems(r, Target{}) {
			session = append(session, it.label)
			if it.run == nil {
				t.Errorf("%v: %q does nothing in a session", r.kind, it.label)
			}
		}
		if !slices.Equal(session, want[i]) {
			t.Errorf("%v in a session: %q, want %q", r.kind, session, want[i])
		}
		bare := labels(sideMenuItems(r, false))
		if slices.Contains(bare, "project popup") || slices.Contains(bare, "tasks") {
			t.Errorf("%v without popups: %q", r.kind, bare)
		}
	}
	c := &client{dashboard: true}
	for _, it := range c.sideItems(rows[0], Target{}) {
		if it.label == "tasks" && it.key != "prefix+t" {
			t.Errorf("a session's tasks item names %q", it.key)
		}
	}
}
