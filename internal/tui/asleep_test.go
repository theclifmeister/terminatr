package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// TestAsleepCoordinatorRow: a sleeping coordinator's row (and its
// folded project's) shows the asleep glyph in every icon set, says it
// is asleep and that enter wakes it; a running coordinator beside a
// stale asleep mark is shown running.
func TestAsleepCoordinatorRow(t *testing.T) {
	freshConsole(t)
	if got := asleepSet([]proto.AsleepCoordinator{{Project: "demo", ID: "s-1"}}); !got["demo"] || len(got) != 1 {
		t.Fatalf("asleepSet %v", got)
	}
	if asleepSet(nil) != nil {
		t.Fatal("asleepSet of none isn't nil")
	}
	rows := buildTree([]ProjectData{{Slug: "demo"}}, nil, treeIn{current: "demo", asleep: map[string]bool{"demo": true}})
	if len(rows) != 2 || !rows[0].asleep || !rows[1].asleep || rows[1].kind != treeCoordinator {
		t.Fatalf("rows = %+v", rows)
	}
	for _, name := range IconChoices[1:] {
		setIcons(name)
		set := ic()
		if set.asleep == "" || set.asleep == set.none {
			t.Errorf("%s: asleep glyph %q", name, set.asleep)
		}
		c := ansi.Strip(treeCells(rows[1], sideDefault-2, false, false))
		if gridCol(c, gridState) != set.asleep {
			t.Errorf("%s: coordinator row %q", name, c)
		}
		folded := rows[0]
		folded.fold = true
		if c := ansi.Strip(treeCells(folded, sideDefault-2, false, false)); gridCol(c, gridState) != set.asleep {
			t.Errorf("%s: folded project row %q", name, c)
		}
	}
	l1, l2 := rows[1].details()
	if !strings.Contains(l1, "asleep") || !strings.Contains(l2, "enter wakes it") {
		t.Errorf("details %q / %q", l1, l2)
	}
	if got := coordLabel(rows[1]); got != "wake the coordinator" {
		t.Errorf("menu item %q", got)
	}
	running := buildTree([]ProjectData{{Slug: "demo"}}, []proto.SessionInfo{{ID: "s-1", Role: proto.RoleCoordinator, Project: "demo", State: "idle"}},
		treeIn{current: "demo", asleep: map[string]bool{"demo": true}})
	if running[0].asleep || running[1].asleep {
		t.Errorf("a running coordinator shown asleep: %+v", running[1])
	}
}
