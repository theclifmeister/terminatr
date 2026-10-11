package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

var updateGolden = flag.Bool("update", false, "rewrite the sidebar's golden files in testdata/")

// goldenTree is a tree with every column of the grid in use: a
// coordinator with remote control on and its context use, threads with
// a PR whose checks pass, fail or run, one without a task, a paused
// project, a coordinator with questions, a folded inactive project and
// an unfolded one whose rows are dormant.
func goldenTree() []treeRow {
	return []treeRow{
		{kind: treeProject, slug: "terminatr", state: "working", current: true, threads: 4, hint: true, pct: -1},
		{kind: treeCoordinator, slug: "terminatr", state: "working", remote: true, ctx: 38, pct: -1, last: true},
		{kind: treeThread, slug: "terminatr", thread: "t-0216", name: "T232", title: "Sidebar rework mockup", state: "working", pct: 40},
		{kind: treeThread, slug: "terminatr", thread: "t-0213", name: "T229", title: "Active and inactive projects", state: "review", pct: 100, ci: "pass", hint: true},
		{kind: treeThread, slug: "terminatr", thread: "t-0215", name: "T231", title: "Windows ConPTY host", state: "blocked", pct: 60, ci: "fail", hint: true},
		{kind: treeThread, slug: "terminatr", thread: "t-0217", name: "t-0217", state: "idle", pct: -1, last: true},
		{kind: treeProject, slug: "herdr", state: "idle", threads: 1, paused: true, pct: -1},
		{kind: treeCoordinator, slug: "herdr", state: "idle", ctx: 12, pct: -1, last: true},
		{kind: treeThread, slug: "herdr", thread: "t-0031", name: "T12", title: "Bump dependencies", state: "working", pct: 20, ci: "pending", last: true},
		{kind: treeProject, slug: "website", state: "blocked", hint: true, pct: -1},
		{kind: treeCoordinator, slug: "website", state: "blocked", ctx: 84, ctxHint: true, hint: true, pct: -1, last: true},
		{kind: treeProject, slug: "dotfiles", inactive: true, threads: 1, pct: -1},
		{kind: treeProject, slug: "notes-app", inactive: true, expanded: true, threads: 1, pct: -1},
		{kind: treeCoordinator, slug: "notes-app", dormant: true, ctx: -1, pct: -1, last: true},
		{kind: treeThread, slug: "notes-app", thread: "t-0004", name: "T7", title: "Zsh prompt", dormant: true, pct: 30, last: true},
	}
}

// TestSidebarGolden: the sidebar in each icon set, at the default width
// (36), narrowed (24: the count/% and PR/CI columns give way) and as the
// slim strip (11), matches testdata/sidebar-<set>.golden. go test -run
// TestSidebarGolden -update rewrites the files.
func TestSidebarGolden(t *testing.T) {
	defer setIcons(IconsUnicode)
	rows := goldenTree()
	for _, set := range IconChoices[1:] {
		setIcons(set)
		var b strings.Builder
		for _, w := range []int{sideDefault, 24, sideSlim} {
			fmt.Fprintf(&b, "width %d\n", w)
			for _, l := range sidebarLines(rows, w, len(rows)+1, nil) {
				b.WriteString(strings.TrimRight(ansi.Strip(l), " ") + "\n")
			}
			b.WriteString("\n")
		}
		path := filepath.Join("testdata", "sidebar-"+set+".golden")
		if *updateGolden {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (go test -run TestSidebarGolden -update writes it)", err)
		}
		if got := b.String(); got != string(want) {
			t.Errorf("%s sidebar differs from %s:\n%s", set, path, got)
		}
	}
}

// TestTreeGridData: buildTree fills the grid's columns from the data: a
// thread's PR checks, its needs-you mark (blocked or waiting on you),
// and the coordinator's when it has questions; a dormant thread has
// neither.
func TestTreeGridData(t *testing.T) {
	ps := []ProjectData{{Slug: "demo", Questions: 2, Threads: []ThreadRow{
		{Record: &thread.Record{ID: "t-0001", Session: "s-2"}, Checks: "pass"},
		{Record: &thread.Record{ID: "t-0002"}, Checks: "fail", Status: &thread.Status{NeedsYou: "review"}},
	}}}
	sessions := []proto.SessionInfo{
		{ID: "s-1", Role: proto.RoleCoordinator, Project: "demo", State: "idle"},
		{ID: "s-2", Role: proto.RoleThread, Project: "demo", State: "blocked"},
	}
	rows := buildTree(ps, sessions, treeIn{current: "demo"})
	if len(rows) != 4 || !rows[0].hint || !rows[1].hint || rows[2].ci != "pass" || !rows[2].hint || rows[3].ci != "fail" || !rows[3].hint {
		t.Fatalf("rows %+v", rows)
	}
	setIcons(IconsUnicode)
	if c := treeCells(rows[2], sideDefault-2, false, false); gridCol(c, gridCI) != "☑" || gridCol(c, gridNeeds) != "⚑" {
		t.Errorf("thread row %q", ansi.Strip(c))
	}
	if c := treeCells(rows[3], sideDefault-2, false, false); gridCol(c, gridCI) != "☒" {
		t.Errorf("thread row %q", ansi.Strip(c))
	}
	if c := treeCells(rows[1], sideDefault-2, false, false); gridCol(c, gridCI) != "" || gridCol(c, gridNeeds) != "⚑" {
		t.Errorf("coordinator row %q", ansi.Strip(c))
	}
	// Unfolded, a project row leaves its state to its coordinator's row.
	if c := treeCells(rows[0], sideDefault-2, false, false); gridCol(c, gridState) != "" {
		t.Errorf("project row %q", ansi.Strip(c))
	}
}
