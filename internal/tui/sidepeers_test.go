package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
	"github.com/theclifmeister/terminatr/internal/view"
)

// peersAt is the drawing clock of the server groups' tests.
var peersAt = time.Date(2026, 10, 14, 14, 6, 0, 0, time.Local)

// peerData is this server's projects and two paired servers: mini,
// linked at 38 ms, sharing acme-api (its coordinator, a thread in
// review) and infra; nas offline for 4 minutes, its project stale.
func peerData() ([]ProjectData, []proto.SessionInfo, []proto.PeerMirror) {
	ps := []ProjectData{
		{Slug: "terminatr", Threads: []ThreadRow{{Record: &thread.Record{ID: "t-0216", Task: "T232", Title: "Sidebar rework", Session: "s-2"}}}},
		{Slug: "web"},
	}
	sessions := []proto.SessionInfo{
		{ID: "s-1", Role: proto.RoleCoordinator, Project: "terminatr", State: "working"},
		{ID: "s-2", Role: proto.RoleThread, Project: "terminatr", State: "working"},
	}
	synced := peersAt.Add(-4 * time.Minute)
	peers := []proto.PeerMirror{
		{Alias: "mini", ID: "m", State: proto.LinkUp, Since: peersAt.Add(-time.Hour), Watch: true, RTT: 37.6, Projects: []proto.PeerMirrorProject{
			{PeerProject: proto.PeerProject{Project: "acme-api", NeedsYou: 1, Threads: 1}, Synced: peersAt,
				Watch: &proto.ProjectWatch{Project: "acme-api", Threads: []proto.WatchThread{
					{ID: "t-0041", Title: "Billing", Session: "r-2", State: "review", Task: &proto.WatchTask{ID: "T41", StepsDone: 3, StepsTotal: 3}},
				}},
				Sessions: []proto.PeerSession{
					{ID: "r-1", Role: proto.RoleCoordinator, Project: "acme-api", State: "working", Context: 22, ContextWindow: 100},
					{ID: "r-2", Role: proto.RoleThread, Project: "acme-api", Thread: "t-0041", State: "review"},
				}},
			{PeerProject: proto.PeerProject{Project: "infra"}, Synced: peersAt, Watch: &proto.ProjectWatch{Project: "infra"}},
		}},
		{Alias: "nas", ID: "n", State: proto.LinkOffline, Reason: proto.LinkReasonRelay, Since: synced, Watch: true, Projects: []proto.PeerMirrorProject{
			{PeerProject: proto.PeerProject{Project: "media", Threads: 2, NeedsYou: 1}, Stale: true, Synced: synced,
				Sessions: []proto.PeerSession{{ID: "q-1", Role: proto.RoleCoordinator, Project: "media", State: "working"}}},
		}},
	}
	return ps, sessions, peers
}

// peerTree is peerData's tree, folded as t.
func peerTree(t view.Tree) []treeRow {
	ps, sessions, peers := peerData()
	return buildTree(ps, sessions, treeIn{current: "terminatr", tree: t, peers: peers, host: "studio"})
}

func pinPeersClock(t *testing.T) {
	sideClock = func() time.Time { return peersAt }
	t.Cleanup(func() { sideClock = time.Now })
}

// TestSidebarPeersGolden: with paired servers the tree is in server
// groups (this server's header first, then each paired server's with its
// shared projects), at the default width, narrowed and as the slim
// strip, in each icon set, matching testdata/sidebar-peers.golden; mini's
// group folded too. go test -run TestSidebarPeersGolden -update rewrites
// it.
func TestSidebarPeersGolden(t *testing.T) {
	pinPeersClock(t)
	defer setIcons(IconsUnicode)
	var b strings.Builder
	for _, set := range IconChoices[1:] {
		setIcons(set)
		for _, tr := range []view.Tree{{}, {Folded: []string{"@mini"}}} {
			rows := peerTree(tr)
			for _, w := range []int{sideDefault, 24, sideSlim} {
				fmt.Fprintf(&b, "%s, width %d, folded %v\n", set, w, tr.Folded)
				for _, l := range sidebarLines(rows, w, len(rows)+1, nil) {
					b.WriteString(strings.TrimRight(ansi.Strip(l), " ") + "\n")
				}
				b.WriteString("\n")
			}
		}
	}
	path := filepath.Join("testdata", "sidebar-peers.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestSidebarPeersGolden -update writes it)", err)
	}
	if got := b.String(); got != string(want) {
		t.Errorf("the sidebar with peers differs from %s:\n%s", path, got)
	}
}

// TestNoPeersNoGroups: without paired servers the tree has no server
// header at all: it is the tree it always was.
func TestNoPeersNoGroups(t *testing.T) {
	ps, sessions, _ := peerData()
	without := buildTree(ps, sessions, treeIn{current: "terminatr"})
	with := buildTree(ps, sessions, treeIn{current: "terminatr", peers: []proto.PeerMirror{}})
	if slices.ContainsFunc(without, func(r treeRow) bool { return r.kind == treeServer || r.isPeer() }) || len(with) != len(without) {
		t.Fatalf("rows without peers %+v", without)
	}
}

// TestPeerRows: the groups' rows from the mirror: this server's header
// first and its rows unchanged, mini's header with its projects, the
// coordinator's context and the thread's progress from its task's steps;
// nas's stale project alone, its state not shown, its counts kept.
func TestPeerRows(t *testing.T) {
	rows := peerTree(view.Tree{})
	var keys []string
	for _, r := range rows {
		keys = append(keys, r.key())
	}
	want := []string{"g:", "p:terminatr", "c:terminatr", "t:terminatr/t-0216", "p:web", "c:web",
		"g:mini", "r:mini/acme-api", "rc:mini/acme-api", "rt:mini/acme-api/t-0041", "r:mini/infra", "rc:mini/infra",
		"g:nas", "r:nas/media"}
	if !slices.Equal(keys, want) {
		t.Fatalf("keys %q, want %q", keys, want)
	}
	if !rows[1].here || rows[6].link.rtt != 37.6 || !rows[6].hint {
		t.Errorf("header rows %+v %+v", rows[1], rows[6])
	}
	if c := rows[8]; c.ctx != 22 || c.state != "working" {
		t.Errorf("remote coordinator %+v", c)
	}
	if th := rows[9]; th.name != "T41" || th.pct != 100 || th.state != "review" || !th.hint || !th.last {
		t.Errorf("remote thread %+v", th)
	}
	if m := rows[13]; !m.stale || !m.fold || m.threads != 2 || !m.hint {
		t.Errorf("stale project %+v", m)
	}
	setIcons(IconsUnicode)
	pinPeersClock(t)
	if l := ansi.Strip(treeCells(rows[12], sideDefault-1, false, false)); !strings.HasPrefix(l, " ○ nas · offline 4m · relay") {
		t.Errorf("offline header %q", l)
	}
	if l := ansi.Strip(treeCells(rows[6], sideDefault-1, false, false)); !strings.HasPrefix(l, " ◉ mini · 38 ms") {
		t.Errorf("linked header %q", l)
	}
	if c := treeCells(rows[13], sideDefault-2, false, false); gridCol(c, gridState) != "·" {
		t.Errorf("stale project's state %q", ansi.Strip(c))
	}
	d1, d2 := rows[12].details()
	if d1 != "nas · offline 4m · relay" || d2 != "last seen "+peersAt.Add(-4*time.Minute).Format("15:04")+" · its projects keep their last values" {
		t.Errorf("offline header's details %q %q", d1, d2)
	}
	if d1, _ := rows[7].details(); d1 != "mini / acme-api · view-only" {
		t.Errorf("remote project's details %q", d1)
	}
}

// TestPeerRowsViewOnly: nothing meant for this server's projects reaches
// a remote row: enter, a click and the preview open nothing (even where
// this server has a project of the same name), space and p say what they
// are for, w W skip them, and 1-9 count this server's projects only.
func TestPeerRowsViewOnly(t *testing.T) {
	rows := peerTree(view.Tree{})
	for _, r := range rows {
		if r.kind != treeServer && !r.isPeer() {
			continue
		}
		if _, ok, why := r.target(); ok || why == "" {
			t.Errorf("%s: target ok %v %q", r.key(), ok, why)
		}
		if _, ok := r.preview(); ok {
			t.Errorf("%s previews", r.key())
		}
		for _, op := range []int{sideActive, sidePin} {
			if st := sideKeyStep(rows, sideDefault, r.key(), op); st.toggle != "" || st.pin != "" || st.target != nil {
				t.Errorf("%s: op %d %+v", r.key(), op, st)
			}
		}
		if st := sideKeyStep(rows, sideDefault, r.key(), sideEnter); st.target != nil || st.msg == "" && st.refold == nil {
			t.Errorf("%s: enter %+v", r.key(), st)
		}
	}
	in := sideKeysIn{tree: func() []treeRow { return rows }, full: func() []treeRow { return rows }, w: sideDefault, h: 40, sel: "p:terminatr"}
	if st := sideNeedStep(in, rows[1], false); st.sel != "" || st.msg != "nothing needs you" {
		t.Errorf("w %+v", st)
	}
	in.sel = "g:"
	if st := sideExtraStep(in, sideNth, "2"); st.sel != "p:web" {
		t.Errorf("2 %+v", st)
	}
}

// TestPeerFolds: ← → fold a remote project as a project, in the view's
// tree under the paired server's alias; ← on it folded goes up to its
// server; enter folds and unfolds a paired server's group; z folds the
// remote projects with this server's and Z unfolds them; a stale
// project has nothing to unfold.
func TestPeerFolds(t *testing.T) {
	var tr view.Tree
	rows := peerTree(tr)
	st := sideKeyStep(rows, sideDefault, "r:mini/acme-api", sideOut)
	if st.refold == nil {
		t.Fatalf("← %+v", st)
	}
	st.refold(&tr)
	if !slices.Equal(tr.Folded, []string{"@mini/acme-api"}) {
		t.Fatalf("folded %q", tr.Folded)
	}
	rows = peerTree(tr)
	if st := sideKeyStep(rows, sideDefault, "r:mini/acme-api", sideOut); st.sel != "g:mini" {
		t.Errorf("← on the folded project %+v", st)
	}
	sideKeyStep(rows, sideDefault, "r:mini/acme-api", sideIn).refold(&tr)
	if len(tr.Folded) != 0 {
		t.Errorf("→ left %q", tr.Folded)
	}
	sideKeyStep(rows, sideDefault, "g:mini", sideEnter).refold(&tr)
	rows = peerTree(tr)
	if i := slices.IndexFunc(rows, func(r treeRow) bool { return r.key() == "g:mini" }); !rows[i].fold || rows[i+1].key() != "g:nas" {
		t.Errorf("folded group %+v", rows[i])
	}
	if st := sideKeyStep(rows, sideDefault, "r:nas/media", sideIn); st.refold != nil {
		t.Errorf("→ on a stale project %+v", st)
	}
	rows = peerTree(view.Tree{})
	z := sideKeyStep(rows, sideDefault, "p:web", sideFoldAll)
	z.refold(&tr)
	for _, f := range []string{"terminatr", "web", "@mini/acme-api", "@mini/infra", "@nas/media"} {
		if !slices.Contains(tr.Folded, f) {
			t.Errorf("z folded %q, not %q", tr.Folded, f)
		}
	}
	sideKeyStep(rows, sideDefault, "p:web", sideUnfoldAll).refold(&tr)
	if len(tr.Folded) != 0 {
		t.Errorf("Z left %q", tr.Folded)
	}
	if err := tr.Valid(); err != nil {
		t.Error(err)
	}
}

// TestPeerKeyBar: the footer's keys on a server header and remote rows.
func TestPeerKeyBar(t *testing.T) {
	rows := peerTree(view.Tree{})
	for key, want := range map[string]string{
		"g:":                      " ? help",
		"g:mini":                  " ⏎ fold · ? help",
		"r:mini/acme-api":         " ← fold · ? help",
		"rt:mini/acme-api/t-0041": " ← project · ? help",
		"r:nas/media":             " ← server · ? help",
	} {
		i := slices.IndexFunc(rows, func(r treeRow) bool { return r.key() == key })
		if got := ansi.Strip(sideKeyBar(&rows[i], true, false, 40)); strings.TrimRight(got, " ") != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}

// TestLoadPeers: a server that can't say (an older one, or an agent's
// caller) and one with no peers both leave Data.Peers empty.
func TestLoadPeers(t *testing.T) {
	if p := loadPeers(func(string, any, any) error { return errors.New("unknown method") }); p != nil {
		t.Errorf("failed call %+v", p)
	}
	if p := loadPeers(func(_ string, _, res any) error { *res.(*proto.PeerList) = proto.PeerList{Gen: 3}; return nil }); p != nil {
		t.Errorf("no peers %+v", p)
	}
	one := func(_ string, _, res any) error {
		*res.(*proto.PeerList) = proto.PeerList{Gen: 3, Peers: []proto.PeerMirror{{Alias: "mini"}}}
		return nil
	}
	if p := loadPeers(one); len(p) != 1 {
		t.Errorf("one peer %+v", p)
	}
}
