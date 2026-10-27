package tui

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
)

// The Mac app's sidebar lists the rows this sidebar does (T762): which
// projects, coordinators and threads, in its order, folded as it folds
// them. testdata/sidebar-app.json holds cases and the rows buildTree
// gives them; the app's SidebarTreeTests read the same file and check
// SidebarTree.rows gives the same, so neither side can change the rule
// alone. go test ./internal/tui -run TestSidebarAppFixture -update
// rewrites the rows.

type appCase struct {
	Name         string       `json:"name"`
	Projects     []appProject `json:"projects"`
	Sessions     []appSession `json:"sessions"`
	Folded       []string     `json:"folded,omitempty"`
	Expanded     []string     `json:"expanded,omitempty"`
	InactiveOpen bool         `json:"inactive_open,omitempty"`
	Rows         []string     `json:"rows"`
}

type appProject struct {
	Slug    string      `json:"slug"`
	Active  bool        `json:"active"`
	Pinned  bool        `json:"pinned,omitempty"`
	Threads []appThread `json:"threads,omitempty"`
}

type appThread struct {
	ID          string `json:"id"`
	Session     string `json:"session,omitempty"`
	NeedsYou    string `json:"needs_you,omitempty"`
	Done        bool   `json:"done,omitempty"`
	Reports     int    `json:"reports,omitempty"`
	ReportAcked int    `json:"report_acked,omitempty"`
}

type appSession struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Project string `json:"project"`
	State   string `json:"state,omitempty"`
}

// appCases are the cases: every kind of thread the order tells apart,
// done ones, pinned and inactive projects, folds.
func appCases() []appCase {
	projects := []appProject{
		{Slug: "alpha", Active: true, Threads: []appThread{
			{ID: "t-1"},
			{ID: "t-2", Session: "s-2"},
			{ID: "t-3", NeedsYou: "which one?"},
			{ID: "t-4", Done: true},
			{ID: "t-5", Reports: 2, ReportAcked: 1},
			{ID: "t-6", Session: "s-6"},
			{ID: "t-7", Session: "s-7"},
			{ID: "t-8", Done: true, Session: "s-8"},
		}},
		{Slug: "beta", Active: false, Pinned: true, Threads: []appThread{{ID: "t-1"}, {ID: "t-2", Done: true}}},
		{Slug: "gamma", Active: true, Pinned: true},
		{Slug: "delta", Active: false},
		{Slug: "eps", Active: true, Threads: []appThread{{ID: "t-9", Done: true}}},
	}
	sessions := []appSession{
		{ID: "s-c", Role: proto.RoleCoordinator, Project: "alpha", State: "working"},
		{ID: "s-2", Role: proto.RoleThread, Project: "alpha", State: "working"},
		{ID: "s-6", Role: proto.RoleThread, Project: "alpha", State: "idle"},
		{ID: "s-7", Role: proto.RoleThread, Project: "alpha", State: "blocked"},
		{ID: "s-8", Role: proto.RoleThread, Project: "alpha", State: "working"},
	}
	return []appCase{
		{Name: "unfolded", Projects: projects, Sessions: sessions},
		{Name: "inactive open", Projects: projects, Sessions: sessions, InactiveOpen: true},
		{Name: "folds", Projects: projects, Sessions: sessions, Folded: []string{"alpha"}, Expanded: []string{"beta"}, InactiveOpen: true},
		{Name: "nothing runs", Projects: projects},
	}
}

// appTree is the rows buildTree gives c.
func appTree(c appCase) []string {
	var ps []ProjectData
	for _, p := range c.Projects {
		s := config.Defaults
		s.Active, s.Pinned = p.Active, p.Pinned
		pd := ProjectData{Slug: p.Slug, Safety: &s}
		for _, t := range p.Threads {
			pd.Threads = append(pd.Threads, ThreadRow{
				Record: &thread.Record{ID: t.ID, Session: t.Session, Done: t.Done, Reports: t.Reports, ReportAck: t.ReportAcked},
				Status: &thread.Status{NeedsYou: t.NeedsYou},
			})
		}
		ps = append(ps, pd)
	}
	var sessions []proto.SessionInfo
	for _, s := range c.Sessions {
		sessions = append(sessions, proto.SessionInfo{ID: s.ID, Role: s.Role, Project: s.Project, State: s.State})
	}
	tree := view.Tree{Folded: c.Folded, Expanded: c.Expanded, InactiveOpen: c.InactiveOpen}
	return rowKeys(buildTree(ps, sessions, treeIn{tree: tree}))
}

func TestSidebarAppFixture(t *testing.T) {
	cases := appCases()
	for i := range cases {
		cases[i].Rows = appTree(cases[i])
	}
	want, err := json.MarshalIndent(map[string]any{"cases": cases}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	path := filepath.Join("testdata", "sidebar-app.json")
	if *updateGolden {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is out of date: rewrite it with go test ./internal/tui -run TestSidebarAppFixture -update, and run the app's swift test", path)
	}
	// The order the cases pin: what waits on you, a report to read,
	// working, running, then the rest, done ones too.
	if rows := cases[0].Rows; !slices.Equal(rows[:12], []string{"p:gamma", "c:gamma", "p:alpha", "c:alpha",
		"t:alpha/t-3", "t:alpha/t-7", "t:alpha/t-5", "t:alpha/t-2", "t:alpha/t-8", "t:alpha/t-6", "t:alpha/t-1", "t:alpha/t-4"}) {
		t.Fatalf("rows %v", rows)
	}
}
