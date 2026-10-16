package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestSidePRHostMark: a project whose PR host fails (T357) gets a red ▲
// right after its name, the name cut before it; its tooltip and details
// line say so; the slim strip draws its name red. Without failures, or
// on a row too narrow for both, there is no mark.
func TestSidePRHostMark(t *testing.T) {
	freshConsole(t)
	ps := []ProjectData{{Slug: "demo", PRHostFails: 3}, {Slug: "a-very-long-project-name"}, {Slug: "ok"}}
	ps[1].PRHostFails = 4
	rows := buildTree(ps, nil, treeIn{current: "ok"})
	if rows[0].prFails != 3 || rows[2].prFails != 4 || rows[4].prFails != 0 {
		t.Fatalf("rows %+v", rows)
	}
	if c := treeCells(rows[0], sideDefault-2, false, false); !strings.Contains(ansi.Strip(c), "■ demo ▲ ") || !strings.Contains(c, th().Bad.Render(" ▲")) {
		t.Errorf("project row %q", c)
	}
	if c := ansi.Strip(treeCells(rows[2], 24, false, false)); !strings.Contains(c, "… ▲") {
		t.Errorf("long name not cut before the mark: %q", c)
	}
	if c := ansi.Strip(treeCells(rows[4], sideDefault-2, false, false)); strings.Contains(c, "▲") {
		t.Errorf("a working PR host marked: %q", c)
	}
	if c := ansi.Strip(treeCells(rows[0], 9, false, false)); strings.Contains(c, "▲") {
		t.Errorf("a row too narrow for the mark: %q", c)
	}
	d1, d2 := rows[0].details()
	if d1 != "demo: PR host failing" || !strings.Contains(d2, "3 PR polls in a row") || !strings.Contains(d2, "inbox") {
		t.Errorf("details %q %q", d1, d2)
	}
	if d1, _ := rows[4].details(); strings.Contains(d1, "PR host") {
		t.Errorf("a working PR host's details %q", d1)
	}
	if c := slimCells(rows[0], sideSlim-1, th().Sel); !strings.Contains(c, th().Bad.Render(fit("demo", sideSlim-6))) {
		t.Errorf("slim strip %q", c)
	}

	// The tooltip: over the ▲, not over the name.
	h := len(rows) + 2
	lines := sidebarLines(rows, sideDefault, h, nil)
	y := 1 // under PROJECTS
	line := ansi.Strip(lines[y])
	x := ansi.StringWidth(line[:strings.Index(line, "▲")])
	if tip := sideTip(rows, sideDefault, h, 0, x, y); !strings.HasPrefix(tip, "PR host failing: ") || !strings.Contains(tip, "3 PR polls") {
		t.Errorf("tooltip over the mark %q (line %q, x %d)", tip, line, x)
	}
	if tip := sideTip(rows, sideDefault, h, 0, 4, y); strings.Contains(tip, "PR host") {
		t.Errorf("tooltip over the name %q", tip)
	}
}
