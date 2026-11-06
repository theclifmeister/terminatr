package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/server"
	"github.com/theclifmeister/terminatr/internal/view"
)

// filteredKeys are the tree's keys as the filter q leaves them, read with
// the boards as they are; peers as peerData's, this server's as its.
func filteredKeys(t *testing.T, peers []proto.PeerMirror, q string) []string {
	t.Helper()
	resetFilter(t, q)
	ps, sessions, _ := peerData()
	rows := buildTree(ps, sessions, treeIn{current: "terminatr", tree: sideTree(ps, nil, view.Tree{}), peers: peers, host: "studio"})
	return rowKeys(sideRows(rows, false))
}

func resetFilter(t *testing.T, q string) {
	t.Helper()
	t.Cleanup(resetConsole)
	setSideFilter(sideFilterState{text: q})
}

// seedBoards has the cache read peers' boards with answers as if they
// came.
func seedBoards(t *testing.T, peers []proto.PeerMirror, answers map[boardKey]proto.PeerPages) {
	t.Helper()
	t.Cleanup(resetConsole)
	for _, k := range sideBoards.due(peers, true, time.Now()) {
		sideBoards.took(k, answers[k], nil)
	}
}

// pagesOn is peerData's mirror with mini's acme-api sharing its pages.
func pagesOn() []proto.PeerMirror {
	_, _, peers := peerData()
	peers[0].Projects[0].Pages = true
	return peers
}

var acmeBoard = map[boardKey]proto.PeerPages{{"mini", "acme-api"}: {Tasks: []proto.PeerPageTask{
	{ID: 7, Title: "Invoice rounding", Status: "open", Steps: []proto.PeerStep{{Text: "a", Done: true}, {Text: "b"}}},
	{ID: 41, Title: "Billing", Status: "review"},
	{ID: 8, Title: "Invoice export", Status: "done"},
	{ID: 9, Title: "Invoice dropped", Status: "dropped"},
}}}

// TestFilterFindsPairedThreads: a paired thread is found by its title,
// task and id, under its project and its server's header; this server's
// header goes when nothing of its own matches.
func TestFilterFindsPairedThreads(t *testing.T) {
	_, _, peers := peerData()
	want := []string{"g:mini", "r:mini/acme-api", "rt:mini/acme-api/t-0041"}
	for _, q := range []string{"billing", "T41", "t-0041"} {
		if got := filteredKeys(t, peers, q); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", q, got, want)
		}
	}
	if got, want := filteredKeys(t, peers, "sidebar"), []string{"g:", "p:terminatr", "t:terminatr/t-0216"}; !slices.Equal(got, want) {
		t.Errorf("a local match: %v, want %v", got, want)
	}
	// The first header kept has no blank line over it.
	setSideFilter(sideFilterState{text: "billing"})
	ps, sessions, _ := peerData()
	if rows := sideRows(buildTree(ps, sessions, treeIn{peers: peers, host: "studio"}), false); rows[0].gap {
		t.Error("the first row keeps its gap")
	}
}

// TestFilterPairedServerName: the server's name or alias finds its rows,
// not its tasks; a display name counts.
func TestFilterPairedServerName(t *testing.T) {
	peers := pagesOn()
	peers[0].Name = "Studio Mini"
	seedBoards(t, peers, acmeBoard)
	want := []string{"g:mini", "r:mini/acme-api", "rc:mini/acme-api", "rt:mini/acme-api/t-0041", "r:mini/infra", "rc:mini/infra"}
	for _, q := range []string{"mini", "studio m"} {
		if got := filteredKeys(t, peers, q); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", q, got, want)
		}
	}
}

// TestFilterSameNames: a same-named project here and on a server, and on
// two servers, keep their own rows.
func TestFilterSameNames(t *testing.T) {
	peers := pagesOn()
	peers[0].Projects[0].Project = "terminatr"
	peers[0].Projects[1].Project = "web"
	peers[1].State, peers[1].Projects[0].Stale = proto.LinkUp, false
	peers[1].Projects[0].Project, peers[1].Projects[0].Pages = "terminatr", true
	seedBoards(t, peers, map[boardKey]proto.PeerPages{
		{"mini", "terminatr"}: {Tasks: []proto.PeerPageTask{{ID: 7, Title: "Same words", Status: "open"}}},
		{"nas", "terminatr"}:  {Tasks: []proto.PeerPageTask{{ID: 7, Title: "Same words", Status: "open"}}},
	})
	want := []string{"g:mini", "r:mini/terminatr", "rk:mini/terminatr/T7", "g:nas", "r:nas/terminatr", "rk:nas/terminatr/T7"}
	if got := filteredKeys(t, peers, "same words"); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestFilterFindsPairedTasks: open tasks only, by id and title, under
// their project and server; none without a filter; a task whose thread
// matches is that thread's row alone.
func TestFilterFindsPairedTasks(t *testing.T) {
	peers := pagesOn()
	seedBoards(t, peers, acmeBoard)
	want := []string{"g:mini", "r:mini/acme-api", "rk:mini/acme-api/T7"}
	if got := filteredKeys(t, peers, "invoice"); !slices.Equal(got, want) {
		t.Errorf("invoice: %v, want %v", got, want)
	}
	if got := filteredKeys(t, peers, "t7"); !slices.Equal(got, want) {
		t.Errorf("t7: %v, want %v", got, want)
	}
	if got := filteredKeys(t, peers, ""); slices.ContainsFunc(got, func(k string) bool { return strings.HasPrefix(k, "rk:") }) {
		t.Errorf("no filter: %v", got)
	}
	if got, want := filteredKeys(t, peers, "billing"), []string{"g:mini", "r:mini/acme-api", "rt:mini/acme-api/t-0041"}; !slices.Equal(got, want) {
		t.Errorf("billing: %v, want %v", got, want)
	}
}

// TestFilterPagesOffOffline: a project with pages off, or whose server
// went off, shows no tasks, kept or not, and its board is forgotten.
func TestFilterPagesOffOffline(t *testing.T) {
	peers := pagesOn()
	seedBoards(t, peers, acmeBoard)
	none := func(name string, peers []proto.PeerMirror) {
		t.Helper()
		if got := filteredKeys(t, peers, "invoice"); len(got) != 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
	off := pagesOn()
	off[0].Projects[0].Pages = false
	none("pages off", off)
	down := pagesOn()
	down[0].State = proto.LinkOffline
	none("offline", down)
	stale := pagesOn()
	stale[0].Projects[0].Stale = true
	none("stale", stale)

	sideBoards.due(off, false, time.Now())
	if got := sideBoards.tasksOf(boardKey{"mini", "acme-api"}); got != nil {
		t.Errorf("forgotten: %v", got)
	}
	// Back on, it is read again at once.
	if got := sideBoards.due(pagesOn(), true, time.Now()); !slices.Equal(got, []boardKey{{"mini", "acme-api"}}) {
		t.Errorf("back on: %v", got)
	}
}

// TestBoardsDue: reads only while the filter shows, once per interval; a
// failed read loses the rows and waits its turn; the limits hold; a read
// landing after its board went stays out.
func TestBoardsDue(t *testing.T) {
	t.Cleanup(resetConsole)
	peers, at := pagesOn(), time.Now()
	if got := sideBoards.due(peers, false, at); got != nil {
		t.Fatalf("filter off: %v", got)
	}
	key := boardKey{"mini", "acme-api"}
	if got := sideBoards.due(peers, true, at); !slices.Equal(got, []boardKey{key}) {
		t.Fatalf("first: %v", got)
	}
	if got := sideBoards.due(peers, true, at.Add(boardReadEvery-time.Second)); got != nil {
		t.Fatalf("too soon: %v", got)
	}
	sideBoards.took(key, acmeBoard[key], nil)
	if got := sideBoards.tasksOf(key); len(got) != 2 || got[0].stepsDone != 1 || got[0].stepsTotal != 2 {
		t.Fatalf("kept: %+v", got)
	}
	sideBoards.took(key, proto.PeerPages{}, errors.New("no"))
	if got := sideBoards.tasksOf(key); got != nil {
		t.Fatalf("failed: %v", got)
	}
	if got := sideBoards.due(peers, true, at.Add(boardReadEvery)); len(got) != 1 {
		t.Fatalf("its turn: %v", got)
	}

	big := proto.PeerPages{}
	for i := range boardMaxTasks + 5 {
		big.Tasks = append(big.Tasks, proto.PeerPageTask{ID: i, Title: "x", Status: "open"})
	}
	sideBoards.took(key, big, nil)
	if got := sideBoards.tasksOf(key); len(got) != boardMaxTasks {
		t.Fatalf("%d tasks kept", len(got))
	}

	many := []proto.PeerMirror{{Alias: "m", State: proto.LinkUp}}
	for i := range boardsMax + 3 {
		many[0].Projects = append(many[0].Projects, proto.PeerMirrorProject{PeerProject: proto.PeerProject{Project: strings.Repeat("p", i+1), Pages: true}})
	}
	if got := boardsReadable(many); len(got) != boardsMax {
		t.Fatalf("%d boards", len(got))
	}

	sideBoards.reset()
	sideBoards.took(key, acmeBoard[key], nil)
	if got := sideBoards.tasksOf(key); got != nil {
		t.Fatalf("a late read: %v", got)
	}
}

// TestFilterInactiveBelowPaired: this server's inactive match below a
// paired one keeps its divider; alone, it needs none, as ever.
func TestFilterInactiveBelowPaired(t *testing.T) {
	_, _, peers := peerData()
	t.Cleanup(resetConsole)
	ps := []ProjectData{{Slug: "terminatr"}, {Slug: "billing-old", Safety: &config.Safety{Active: false}}}
	setSideFilter(sideFilterState{text: "billing"})
	rows := buildTree(ps, nil, treeIn{tree: sideTree(ps, nil, view.Tree{}), peers: peers, host: "studio"})
	want := []string{"g:", "g:mini", "r:mini/acme-api", "rt:mini/acme-api/t-0041", dividerKey, "p:billing-old"}
	if got := rowKeys(sideRows(rows, false)); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	setSideFilter(sideFilterState{text: "old"})
	rows = buildTree(ps, nil, treeIn{tree: sideTree(ps, nil, view.Tree{}), peers: peers, host: "studio"})
	if got, want := rowKeys(sideRows(rows, false)), []string{"g:", "p:billing-old"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestFilterPairedMemberTask: a paired member's rows under its program
// find too, with this server's header over the program.
func TestFilterPairedMemberTask(t *testing.T) {
	peers := pagesOn()
	seedBoards(t, peers, acmeBoard)
	progs := []ProgramData{{SideProgram: server.SideProgram{Slug: "acme", Projects: []string{"mini/acme-api"}}}}
	ps, sessions, _ := peerData()
	setSideFilter(sideFilterState{text: "invoice"})
	rows := buildTree(ps, sessions, treeIn{tree: sideTree(ps, progs, view.Tree{}), peers: peers, programs: progs, host: "studio"})
	got := rowKeys(sideRows(rows, false))
	for _, k := range []string{"g:", "g:acme", "g:acme/r:mini/acme-api", "g:acme/rk:mini/acme-api/T7", "g:mini", "r:mini/acme-api", "rk:mini/acme-api/T7"} {
		if !slices.Contains(got, k) {
			t.Errorf("%s is missing from %v", k, got)
		}
	}
}

// TestSidebarTaskRow on the dashboard: the read comes on a tick while the
// filter shows, never per keystroke; the row draws, its footer says what
// it is, enter shows its project's dashboard, and the slim strip lists
// its project alone.
func TestSidebarTaskRow(t *testing.T) {
	m, src := sideDash(t)
	data := src.data
	data.Peers = pagesOn()
	src.data = data
	m.setData(data)
	src.boards = map[string]proto.PeerPages{"mini/acme-api": acmeBoard[boardKey{"mini", "acme-api"}]}

	drain(t, m, m.readSideBoards())
	if n := src.boardReads.Load(); n != 0 {
		t.Fatalf("%d reads with no filter", n)
	}
	keyPress(m, "/")
	for _, k := range strings.Split("invoice", "") {
		keyPress(m, k)
	}
	if n := src.boardReads.Load(); n != 0 {
		t.Fatalf("%d reads per keystroke", n)
	}
	if got := treeKeys(m); len(got) != 0 {
		t.Fatalf("before the read: %v", got)
	}
	drain(t, m, m.readSideBoards())
	drain(t, m, m.readSideBoards())
	if n := src.boardReads.Load(); n != 1 {
		t.Fatalf("%d reads", n)
	}
	if got, want := treeKeys(m), []string{"g:mini", "r:mini/acme-api", "rk:mini/acme-api/T7"}; !slices.Equal(got, want) {
		t.Fatalf("after the read: %v, want %v", got, want)
	}
	if w := ansi.Strip(whole(m)); !strings.Contains(w, "T7 Invoice") || !strings.Contains(w, "mini") {
		t.Fatalf("the row isn't drawn:\n%s", w)
	}
	keyPress(m, "enter")
	keyPress(m, "down")
	keyPress(m, "down")
	if c := sideCursorKey(m); c != "rk:mini/acme-api/T7" {
		t.Fatalf("cursor %q", c)
	}
	var row treeRow
	for _, r := range m.tree() {
		if r.key() == "rk:mini/acme-api/T7" {
			row = r
		}
	}
	l1, l2 := row.details()
	if !strings.Contains(l1, "mini / acme-api T7 Invoice rounding") || !strings.Contains(l2, "open · 50%") {
		t.Fatalf("details: %q %q", l1, l2)
	}
	if rt, ok := row.preview(); !ok || rt.Project != "@mini/acme-api" {
		t.Fatalf("preview %+v %v", rt, ok)
	}
	keyPress(m, "enter")
	if alias, slug, ok := m.remote(); !ok || alias != "mini" || slug != "acme-api" {
		t.Fatalf("enter shows %q", m.current)
	}
	if got := rowKeys(shownRows(m.tree(), sideSlim)); slices.Contains(got, "rk:mini/acme-api/T7") {
		t.Fatalf("the slim strip: %v", got)
	}
}

// TestFilterNoMatchNote: "no match" only when nothing matched, a paired
// match with none of this server's counting as a match.
func TestFilterNoMatchNote(t *testing.T) {
	m, src := sideDash(t)
	data := src.data
	_, _, data.Peers = peerData()
	m.setData(data)
	keyPress(m, "/")
	for _, k := range strings.Split("billing", "") {
		keyPress(m, k)
	}
	if w := ansi.Strip(whole(m)); strings.Contains(w, "no match") || !strings.Contains(w, "◉ mini") {
		t.Fatalf("a paired match:\n%s", w)
	}
	keyPress(m, "x")
	if w := ansi.Strip(whole(m)); !strings.Contains(w, "no match") || strings.Contains(w, "◉ mini") {
		t.Fatalf("nothing matches:\n%s", w)
	}
}
