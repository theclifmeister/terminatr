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

// A case's Folded names this server's projects by slug, a paired
// server's group as "@<alias>" and its project as "@<alias>/<slug>"
// (peerFold). NeedsFirst is [ui] needs_first.
type appCase struct {
	Name         string       `json:"name"`
	Projects     []appProject `json:"projects"`
	Sessions     []appSession `json:"sessions"`
	Peers        []appPeer    `json:"peers,omitempty"`
	Folded       []string     `json:"folded,omitempty"`
	Expanded     []string     `json:"expanded,omitempty"`
	InactiveOpen bool         `json:"inactive_open,omitempty"`
	NeedsFirst   bool         `json:"needs_first,omitempty"`
	Rows         []string     `json:"rows"`
}

// appProject's NeedsYou counts its board's Needs you tasks (Counts).
type appProject struct {
	Slug      string      `json:"slug"`
	Active    bool        `json:"active"`
	Pinned    bool        `json:"pinned,omitempty"`
	NeedsYou  int         `json:"needs_you,omitempty"`
	Questions int         `json:"questions,omitempty"`
	Threads   []appThread `json:"threads,omitempty"`
}

// appPeer is a paired server; its projects' Threads are the watched
// ones (nil: no watch read yet).
type appPeer struct {
	Alias    string           `json:"alias"`
	Projects []appPeerProject `json:"projects"`
}

type appPeerProject struct {
	Project  string       `json:"project"`
	Stale    bool         `json:"stale,omitempty"`
	Sessions []appSession `json:"sessions,omitempty"`
	Threads  []appThread  `json:"threads,omitempty"`
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
	Thread  string `json:"thread,omitempty"`
	State   string `json:"state,omitempty"`
}

// appCases are the cases: every kind of thread the order tells apart,
// done ones, pinned and inactive projects, folds, [ui] needs_first and
// paired servers.
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
		{Name: "needs first off", Projects: needs, Sessions: needsSessions, InactiveOpen: true},
		{Name: "needs first", Projects: needs, Sessions: needsSessions, InactiveOpen: true, NeedsFirst: true},
		{Name: "needs first, folded and expanded", Projects: needs, Sessions: needsSessions, InactiveOpen: true, NeedsFirst: true,
			Folded: []string{"n-q"}, Expanded: []string{"i-t"}},
		{Name: "paired", Projects: projects[2:3], Peers: peers},
		{Name: "paired, folded", Projects: projects[2:3], Peers: peers, Folded: []string{"@mini/demo", "@far"}},
		{Name: "paired, needs first", Projects: needs, Sessions: needsSessions, Peers: peers, NeedsFirst: true},
	}
}

// needs are projects for [ui] needs_first: each way one needs you, or
// doesn't, pinned or not, active or not.
var (
	needs = []appProject{
		{Slug: "n-none", Active: true},
		{Slug: "n-pin", Active: true, Pinned: true},
		{Slug: "n-q", Active: true, Questions: 1},
		{Slug: "n-task", Active: true, NeedsYou: 2},
		{Slug: "n-blocked", Active: true, Threads: []appThread{{ID: "t-1", Session: "s-b"}, {ID: "t-2"}}},
		{Slug: "n-pin-asks", Active: true, Pinned: true, Threads: []appThread{{ID: "t-1", NeedsYou: "go?"}}},
		{Slug: "i-none", Active: false},
		{Slug: "i-t", Active: false, Threads: []appThread{{ID: "t-1", NeedsYou: "go?"}}},
		{Slug: "i-q", Active: false, Pinned: true, Questions: 1},
	}
	needsSessions = []appSession{{ID: "s-b", Role: proto.RoleThread, Project: "n-blocked", State: "blocked"}}
	// peers: a project read, one stale, one not watched yet, and a
	// server whose group folds.
	peers = []appPeer{
		{Alias: "mini", Projects: []appPeerProject{
			{Project: "demo", Sessions: []appSession{
				{ID: "s-1", Role: proto.RoleCoordinator, Project: "demo", State: "working"},
				{ID: "s-2", Role: proto.RoleThread, Project: "demo", Thread: "t-2", State: "blocked"},
				// A session of a thread the watch doesn't list.
				{ID: "s-9", Role: proto.RoleThread, Project: "demo", Thread: "t-9"},
			}, Threads: []appThread{{ID: "t-1"}, {ID: "t-2", Session: "s-2"}, {ID: "t-3", Done: true}}},
			{Project: "old", Stale: true, Threads: []appThread{{ID: "t-1"}}},
			{Project: "new", Sessions: []appSession{{ID: "s-3", Role: proto.RoleThread, Project: "new", Thread: "t-1"}}},
		}},
		{Alias: "far", Projects: []appPeerProject{{Project: "x", Threads: []appThread{{ID: "t-1"}}}}},
	}
)

// appTree is the rows buildTree gives c.
func appTree(c appCase) []string {
	var ps []ProjectData
	for _, p := range c.Projects {
		s := config.Defaults
		s.Active, s.Pinned = p.Active, p.Pinned
		pd := ProjectData{Slug: p.Slug, Safety: &s, Questions: p.Questions}
		if p.NeedsYou > 0 {
			pd.Counts = map[string]int{"needs_you": p.NeedsYou}
		}
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
	var mirrors []proto.PeerMirror
	for _, pe := range c.Peers {
		pm := proto.PeerMirror{Alias: pe.Alias, State: proto.LinkUp}
		for _, p := range pe.Projects {
			mp := proto.PeerMirrorProject{PeerProject: proto.PeerProject{Project: p.Project}, Stale: p.Stale}
			for _, s := range p.Sessions {
				mp.Sessions = append(mp.Sessions, proto.PeerSession{ID: s.ID, Role: s.Role, Project: s.Project, Thread: s.Thread, State: s.State})
			}
			if p.Threads != nil {
				mp.Watch = &proto.ProjectWatch{Project: p.Project}
				for _, t := range p.Threads {
					mp.Watch.Threads = append(mp.Watch.Threads, proto.WatchThread{ID: t.ID, Session: t.Session, Done: t.Done, NeedsYou: t.NeedsYou})
				}
			}
			pm.Projects = append(pm.Projects, mp)
		}
		mirrors = append(mirrors, pm)
	}
	tree := view.Tree{Folded: c.Folded, Expanded: c.Expanded, InactiveOpen: c.InactiveOpen}
	rows := buildTree(ps, sessions, treeIn{tree: tree, peers: mirrors, host: "here"})
	if c.NeedsFirst {
		rows = needsFirstOrder(rows)
	}
	return rowKeys(rows)
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
