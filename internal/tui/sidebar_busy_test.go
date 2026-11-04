package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
)

// busyProjects are a standalone project zoo and acme's members api and
// web, each with a thread; a session's state makes it work.
func busyProjects() []ProjectData {
	s := config.Defaults
	row := func(slug string) ProjectData {
		return ProjectData{Slug: slug, Safety: &s, Threads: []ThreadRow{{Record: &thread.Record{ID: "t-" + slug, Session: "s-" + slug}}}}
	}
	return []ProjectData{row("api"), row("web"), row("zoo")}
}

// busyTree is the tree of busyProjects with program acme, the sessions
// by id and state ("s-api", a thread; "s-d", the director; "c-zoo", a
// coordinator).
func busyTree(tree view.Tree, states map[string]string) []treeRow {
	var sessions []proto.SessionInfo
	for id, state := range states {
		s := proto.SessionInfo{ID: id, State: state}
		switch {
		case id == "s-d":
			s.Role, s.Project = proto.RoleDirector, "acme"
		case strings.HasPrefix(id, "c-"):
			s.Role, s.Project = proto.RoleCoordinator, id[2:]
		default:
			s.Role, s.Project = proto.RoleThread, id[2:]
		}
		sessions = append(sessions, s)
	}
	progs := []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Projects: []string{"api", "web"}}}}
	return buildTree(busyProjects(), sessions, treeIn{tree: tree, programs: progs})
}

// busyDrawn are the keys of the rows that draw the working glyph in
// their state column, each row's own or the work it hides.
func busyDrawn(rows []treeRow) []string {
	var out []string
	for _, r := range rows {
		if strings.Contains(stateCell(r), ic().working) {
			out = append(out, r.key())
		}
	}
	return out
}

// stateCell is the text of r's state column.
func stateCell(r treeRow) string {
	return gridCell(r.drawn(), sideCol{id: view.ColState, w: 2}).s
}

// busyRow is the row of rows with key.
func busyRow(t *testing.T, rows []treeRow, key string) treeRow {
	t.Helper()
	i := slices.IndexFunc(rows, func(r treeRow) bool { return r.key() == key })
	if i < 0 {
		t.Fatalf("no %s in %v", key, rowKeys(rows))
	}
	return rows[i]
}

// TestBusyNearestVisibleRow: a working session shows once, on the
// nearest row you can see: its own while unfolded, its project's while
// that folds, its program's while that folds; never on two rows.
func TestBusyNearestVisibleRow(t *testing.T) {
	thr := map[string]string{"s-api": "working"}
	zoo := map[string]string{"s-zoo": "working"}
	lead := map[string]string{"c-zoo": "working"}
	dir := map[string]string{"s-d": "working"}
	folded := view.Tree{Folded: []string{"zoo"}}
	for _, c := range []struct {
		name   string
		states map[string]string
		tree   view.Tree
		want   []string
	}{
		{"member unfolded: the thread's row", thr, view.Tree{Unfolded: []string{"api"}}, []string{"t:api/t-api"}},
		{"member folded: its project's row", thr, view.Tree{}, []string{"p:api"}},
		{"program folded: its row, not the member's", thr, view.Tree{Folded: []string{"g:acme"}, Unfolded: []string{"api"}}, []string{"g:acme"}},
		{"thread, unfolded", zoo, view.Tree{}, []string{"t:zoo/t-zoo"}},
		{"thread, folded", zoo, folded, []string{"p:zoo"}},
		{"coordinator, unfolded", lead, view.Tree{}, []string{"c:zoo"}},
		{"coordinator, folded", lead, folded, []string{"p:zoo"}},
		// Several: one mark on the folded row.
		{"both, folded", map[string]string{"c-zoo": "working", "s-zoo": "working"}, folded, []string{"p:zoo"}},
		{"director, unfolded", dir, view.Tree{}, []string{"r:acme"}},
		{"director, program folded", dir, view.Tree{Folded: []string{"g:acme"}}, []string{"g:acme"}},
		// Nothing works: nothing shows, folded or not.
		{"idle, folded", map[string]string{"s-zoo": "idle", "c-zoo": "idle"}, folded, nil},
	} {
		if got := busyDrawn(busyTree(c.tree, c.states)); !slices.Equal(got, c.want) {
			t.Errorf("%s: drawn on %v, want %v", c.name, got, c.want)
		}
	}
}

// TestBusyLive: a folded row follows its children's states as the data
// changes, with no re-fold, and its row doesn't move a cell.
func TestBusyLive(t *testing.T) {
	m, src, _ := foldDash(t)
	m.tree() // the fold is the view's: folded below by its key
	drain(t, m, keyPress(m, "left"))
	set := func(state string) {
		src.data.Sessions = []proto.SessionInfo{
			{ID: "s-c", Role: proto.RoleCoordinator, Project: "alpha", State: "idle"},
			{ID: "s-t", Role: proto.RoleThread, Project: "alpha", Thread: "t-1", State: state},
		}
		src.data.Projects[0].Threads = []ThreadRow{{Record: &thread.Record{ID: "t-1", Session: "s-t"}}}
		m.setData(src.data)
	}
	set("idle")
	idle := busyRow(t, m.tree(), "p:alpha")
	if !idle.folded() || idle.foldBusy() || strings.Contains(stateCell(idle), ic().working) {
		t.Fatalf("idle: %+v draws %q", idle, stateCell(idle))
	}
	before := ansi.Strip(treeCells(idle, 40, false, false))
	set("working")
	busy := busyRow(t, m.tree(), "p:alpha")
	if !busy.folded() || !busy.foldBusy() || !strings.Contains(stateCell(busy), ic().working) {
		t.Fatalf("working: %+v draws %q", busy, stateCell(busy))
	}
	after := ansi.Strip(treeCells(busy, 40, false, false))
	if ansi.StringWidth(after) != 40 || strings.Index(after, "alpha") != strings.Index(before, "alpha") {
		t.Fatalf("the row moved:\n%q\n%q", before, after)
	}
	set("idle")
	if r := busyRow(t, m.tree(), "p:alpha"); r.foldBusy() || strings.Contains(stateCell(r), ic().working) {
		t.Fatalf("idle again: %+v", r)
	}
}

// TestBusyOverBlocked: a folded project's blocked coordinator gives its
// state column to the work under it, and its needs-you column says what
// it gave up.
func TestBusyOverBlocked(t *testing.T) {
	needs := func(r treeRow) string { return gridCell(r.drawn(), sideCol{id: view.ColNeeds, w: 2}).s }
	folded := view.Tree{Folded: []string{"zoo"}}
	r := busyRow(t, busyTree(folded, map[string]string{"c-zoo": "blocked", "s-zoo": "working"}), "p:zoo")
	if !strings.Contains(stateCell(r), ic().working) || !strings.Contains(needs(r), ic().hint) {
		t.Fatalf("state %q needs %q", stateCell(r), needs(r))
	}
	// Alone, it keeps its own glyph, and no flag it didn't have.
	r = busyRow(t, busyTree(folded, map[string]string{"c-zoo": "blocked"}), "p:zoo")
	if !strings.Contains(stateCell(r), ic().blocked) || strings.Contains(needs(r), ic().hint) {
		t.Fatalf("blocked alone: state %q needs %q", stateCell(r), needs(r))
	}
}

// TestBusySlim: the slim strip lists projects alone, so a project shows
// its sessions' work folded or not; a program only what it hides.
func TestBusySlim(t *testing.T) {
	line := func(rows []treeRow, key string) string {
		t.Helper()
		return ansi.Strip(treeCells(busyRow(t, rows, key), sideSlim-1, true, false))
	}
	w := ic().working
	rows := busyTree(view.Tree{}, map[string]string{"s-zoo": "working", "s-api": "working"})
	if l := line(rows, "p:zoo"); !strings.Contains(l, w) {
		t.Errorf("unfolded project %q", l)
	}
	if l := line(rows, "p:api"); !strings.Contains(l, w) {
		t.Errorf("member %q", l)
	}
	if l := line(rows, "g:acme"); strings.Contains(l, w) {
		t.Errorf("program with its member listed %q", l)
	}
	rows = busyTree(view.Tree{Folded: []string{"g:acme"}}, map[string]string{"s-api": "working"})
	if l := line(rows, "g:acme"); !strings.Contains(l, w) {
		t.Errorf("folded program %q", l)
	}
}

// busyPeers is a paired server with a shared project, its thread's
// session in state, and a stale one whose last values said working.
func busyPeers(state string) []proto.PeerMirror {
	return []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Projects: []proto.PeerMirrorProject{
		{PeerProject: proto.PeerProject{Project: "demo"},
			Sessions: []proto.PeerSession{{ID: "s-1", Role: proto.RoleThread, Project: "demo", Thread: "t-1", State: state}},
			Watch:    &proto.ProjectWatch{Project: "demo", Threads: []proto.WatchThread{{ID: "t-1", Session: "s-1"}}}},
		{PeerProject: proto.PeerProject{Project: "old"}, Stale: true,
			Sessions: []proto.PeerSession{{ID: "s-2", Role: proto.RoleThread, Project: "old", Thread: "t-2", State: "working"}},
			Watch:    &proto.ProjectWatch{Project: "old", Threads: []proto.WatchThread{{ID: "t-2", Session: "s-2"}}}},
	}}}
}

// TestBusyPaired: a paired server's project and its header, as this
// server's: the nearest visible row; a stale project's last values never
// count; its header's cell keeps its words where they were.
func TestBusyPaired(t *testing.T) {
	row := func(tree view.Tree, peers []proto.PeerMirror, kind int, slug string) treeRow {
		t.Helper()
		rows := buildTree(nil, nil, treeIn{tree: tree, peers: peers, host: "here"})
		i := slices.IndexFunc(rows, func(r treeRow) bool { return r.kind == kind && r.peer == "mini" && r.slug == slug })
		if i < 0 {
			t.Fatalf("no row in %v", rowKeys(rows))
		}
		return rows[i]
	}
	// Unfolded: the thread's own row.
	if r := row(view.Tree{}, busyPeers("working"), treePeerProject, "demo"); r.foldBusy() || strings.Contains(stateCell(r), ic().working) {
		t.Errorf("unfolded project %+v", r)
	}
	if r := row(view.Tree{}, busyPeers("working"), treePeerThread, "demo"); !strings.Contains(stateCell(r), ic().working) {
		t.Errorf("thread row draws %q", stateCell(r))
	}
	// Folded: the project's row, while it works.
	folded := view.Tree{Folded: []string{"@mini/demo"}}
	if r := row(folded, busyPeers("working"), treePeerProject, "demo"); !strings.Contains(stateCell(r), ic().working) {
		t.Errorf("folded project draws %q", stateCell(r))
	}
	if r := row(folded, busyPeers("idle"), treePeerProject, "demo"); strings.Contains(stateCell(r), ic().working) {
		t.Errorf("idle folded project draws %q", stateCell(r))
	}
	if r := row(view.Tree{}, busyPeers("idle"), treePeerProject, "old"); r.busy || strings.Contains(stateCell(r), ic().working) {
		t.Errorf("stale project %+v draws %q", r, stateCell(r))
	}
	// The server's header, folded: the glyph on the right, its words
	// where they were and the row as wide.
	head := view.Tree{Folded: []string{"@mini"}}
	on, off := row(head, busyPeers("working"), treeServer, ""), row(head, busyPeers("idle"), treeServer, "")
	if !on.foldBusy() || off.foldBusy() {
		t.Fatalf("header busy %v, idle %v", on.foldBusy(), off.foldBusy())
	}
	a, b := ansi.Strip(treeCells(on, 40, false, false)), ansi.Strip(treeCells(off, 40, false, false))
	if !strings.Contains(a, ic().working+" ") || strings.Contains(b, ic().working) {
		t.Errorf("header\n%q\n%q", a, b)
	}
	if ansi.StringWidth(a) != ansi.StringWidth(b) || !strings.HasPrefix(a, strings.TrimRight(b, " ")) {
		t.Errorf("the header's words moved:\n%q\n%q", a, b)
	}
	// Unfolded, the mark is on its rows, not the header.
	if l := ansi.Strip(treeCells(row(view.Tree{}, busyPeers("working"), treeServer, ""), 40, false, false)); strings.Contains(l, ic().working) {
		t.Errorf("unfolded header %q", l)
	}
	// The strip keeps the rule's width.
	sa, sb := ansi.Strip(treeCells(on, sideSlim-1, true, false)), ansi.Strip(treeCells(off, sideSlim-1, true, false))
	if !strings.Contains(sa, ic().working) || strings.Contains(sb, ic().working) || ansi.StringWidth(sa) != ansi.StringWidth(sb) {
		t.Errorf("slim header\n%q\n%q", sa, sb)
	}
}

// TestBusyDetails: the footer and the tooltip say what the glyph means
// on a folded row whose coordinator isn't the one working.
func TestBusyDetails(t *testing.T) {
	r := busyRow(t, busyTree(view.Tree{Folded: []string{"zoo"}}, map[string]string{"c-zoo": "idle", "s-zoo": "working"}), "p:zoo")
	if _, l2 := r.details(); !strings.Contains(l2, "coordinator idle · working under it") {
		t.Errorf("details %q", l2)
	}
	if tip := gridTip(r, sideCol{id: view.ColState, w: 2}); !strings.Contains(tip, "working") {
		t.Errorf("tip %q", tip)
	}
}
