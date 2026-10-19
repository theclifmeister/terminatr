package tui

import (
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/view"
)

// remoteDash is a dashboard over peerData showing current, mini's
// acme-api read 2 seconds ago, with a goal, something that needs its
// user, an inbox item and a task on deck.
func remoteDash(t *testing.T, current string) (*dash, *fakeSource) {
	t.Helper()
	pinPeersClock(t)
	ps, sessions, peers := peerData()
	p := &peers[0].Projects[0]
	p.Goal, p.Synced = "the billing API", peersAt.Add(-2*time.Second)
	p.Watch.NeedsYou = []proto.WatchNeed{{Why: proto.WhyReview, Task: "T41", Title: "Billing"}}
	p.Watch.Inbox = []proto.WatchItem{{ID: "i1", Kind: "report", Subject: "t-0041", Task: "T41", What: "ready for review", Count: 1}}
	p.Watch.Ready = []proto.WatchTodo{{Task: "T44", Title: "Refunds", Status: "ready"}}
	p.Watch.Questions = 1
	src := &fakeSource{data: Data{ServerOK: true, Projects: ps, Sessions: sessions, Peers: peers}}
	m := newDash(DashOptions{Source: src, Width: 100 + sideDefault, Height: 40, State: DashState{Current: current}})
	m.setData(src.data)
	return m, src
}

// TestRemoteDashboard: a paired server's project shows read-only, from
// the mirror: the breadcrumb in the header, the link's state and how
// fresh the copy is, its goal and coordinator, then the project as the
// coordinator's panel draws it, with no key or command hint. Its row is
// the one you are on in the sidebar. The key bar has nothing that acts
// on the project.
func TestRemoteDashboard(t *testing.T) {
	m, _ := remoteDash(t, "@mini/acme-api")
	out := screen(m)
	for _, want := range []string{
		" tm dashboard · mini › acme-api",
		"◉ mini · linked 38 ms · synced 2s ago",
		"the billing API",
		"coordinator ● working · 22 / 100 context",
		"1 question waiting",
		"T41 ◆ in review Billing",
		"report T41 ready for review",
		"t-0041 T41 ◆ review 3/3",
		"T44",
		"To ask its coordinator something, link a project of yours to mini/acme-api (tm project link).",
		"≡ menu · ↑ ↓ scroll · , settings · ? help · prefix+q quit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, not := range []string{"view-only", "Read-only", "panes writable", "a or click answers", "tm agent explain", "NEEDS YOU 0", " terminatr ─", "project ·", "activate"} {
		if strings.Contains(out, not) {
			t.Errorf("shows %q:\n%s", not, out)
		}
	}
	rows := m.tree()
	if k := hereKey(rows); k != "r:mini/acme-api" {
		t.Errorf("here %q", k)
	}
	if slices.ContainsFunc(rows, func(r treeRow) bool { return r.here && !r.isPeer() }) {
		t.Error("a row of this server is here too")
	}
}

// TestRemoteDashboardPanes: the header says view-only only when mini's
// panes are view-only, "panes writable" when they take keys, and
// neither claims the project read-only.
func TestRemoteDashboardPanes(t *testing.T) {
	for panes, want := range map[string]string{proto.PanesView: "synced 2s ago · view-only", proto.PanesType: "synced 2s ago · panes writable", proto.PanesOff: "synced 2s ago "} {
		m, src := remoteDash(t, "@mini/acme-api")
		src.data.Peers[0].Panes = panes
		m.setData(src.data)
		out := screen(m)
		if !strings.Contains(out, want) || strings.Contains(out, "Read-only") {
			t.Errorf("%s: want %q in\n%s", panes, want, out)
		}
		if panes != proto.PanesView && strings.Contains(out, "view-only") {
			t.Errorf("%s: says view-only:\n%s", panes, out)
		}
	}
}

// TestRemoteDashboardStale: an offline server's project keeps its last
// values, dimmed, saying when it was last seen; one never read says so;
// a project no longer shared and a server no longer paired say that.
func TestRemoteDashboardStale(t *testing.T) {
	m, _ := remoteDash(t, "@nas/media")
	out := screen(m)
	for _, want := range []string{" tm dashboard · nas › media", "○ nas · offline 4m · relay · last seen " + peersAt.Add(-4*time.Minute).Format("15:04"),
		"Nothing read from nas yet."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for cur, want := range map[string]string{
		"@mini/gone": "mini doesn't share gone with this server any more.",
		"@old/x":     "old isn't paired with this server any more",
	} {
		m, _ := remoteDash(t, cur)
		if out := screen(m); !strings.Contains(out, want) {
			t.Errorf("%s: missing %q in\n%s", cur, want, out)
		}
	}
}

// TestRemoteDashboardKeys: the keys that would act on the project (open,
// activate, its popup, tasks, inbox, adopt) do nothing there and say
// why; the list keys scroll; settings and help open; the ≡ menu has
// only what the key bar keeps.
func TestRemoteDashboardKeys(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	m.h = 12 // so it scrolls
	for _, k := range []string{"enter", "a", "t", "i", " ", "T", "|"} {
		drain(t, m, press(m, k))
		if m.top() != nil || len(src.lifecycle) > 0 || len(src.opened) > 0 || len(src.adopted) > 0 {
			t.Fatalf("%q acted: %+v %v %v", k, m.top(), src.lifecycle, src.opened)
		}
		if m.msg != "acme-api is mini's: nothing here acts on it" {
			t.Errorf("%q: %q", k, m.msg)
		}
	}
	press(m, "down")
	if m.remoteTop != 1 {
		t.Errorf("down: top %d", m.remoteTop)
	}
	m.wheel(m.geom().main().x+2, 5, 1)
	if m.remoteTop != 2 {
		t.Errorf("wheel: top %d", m.remoteTop)
	}
	press(m, ",")
	if _, ok := m.top().(*settingsView); !ok {
		t.Errorf("settings: %T", m.top())
	}
	m.pop()
	for _, it := range m.dashItems() {
		for _, not := range []string{"project popup", "tasks", "inbox", "open the selected row", "activate / deactivate the project"} {
			if it.label == not {
				t.Errorf("≡ menu has %q", not)
			}
		}
	}
	if m.panel() != "" {
		t.Errorf("a details panel: %q", m.panel())
	}
}

// TestRemoteTargets: enter, a click and the preview on a remote project
// show its dashboard, "@alias/slug" as the view's project; its
// coordinator, threads and the server headers still open nothing.
func TestRemoteTargets(t *testing.T) {
	rows := peerTree(view.Tree{})
	for _, r := range rows {
		t1, ok, why := r.target()
		t2, pok := r.preview()
		switch {
		case r.kind == treePeerProject:
			want := "@" + r.peer + "/" + r.slug
			if !ok || t1.Project != want || !pok || t2.Project != want || targetActs(t1) {
				t.Errorf("%s: target %+v %v, preview %+v %v", r.key(), t1, ok, t2, pok)
			}
			if st := sideKeyStep(rows, sideDefault, r.key(), sideEnter); st.target == nil || st.target.Project != want {
				t.Errorf("%s: enter %+v", r.key(), st)
			}
		case r.kind == treeServer || r.isPeer():
			if ok || why == "" || pok {
				t.Errorf("%s: target ok %v %q, preview %v", r.key(), ok, why, pok)
			}
		}
	}
	for cur, want := range map[string][2]string{"@mini/acme-api": {"mini", "acme-api"}, "@mini/a/b": {"mini", "a/b"}} {
		if a, s, ok := peerPlace(cur); !ok || a != want[0] || s != want[1] {
			t.Errorf("%q: %q %q %v", cur, a, s, ok)
		}
	}
	for _, cur := range []string{"", "acme", "@mini", "@/x", "@mini/"} {
		if _, _, ok := peerPlace(cur); ok {
			t.Errorf("%q is a remote place", cur)
		}
	}
}

// peersList is what tm peer list would say beside peerData's mirror:
// mini linked, nas offline.
func peersList() PeerList {
	return PeerList{ID: "4f2a91c3", Short: "4F2A-91C3", Running: true, Peers: []PeerEntry{
		{Alias: "mini", ID: "m", Short: "91C3-77AB", Relay: "wss://relay.example.net",
			Link: &proto.PeerLink{Alias: "mini", ID: "m", State: proto.LinkUp, RTT: 37.6, Version: "v0.27.0"}},
		{Alias: "nas", ID: "n", Short: "A1B2-C3D4", Relay: "wss://relay.example.net:8443",
			Link: &proto.PeerLink{Alias: "nas", ID: "n", State: proto.LinkOffline, Reason: proto.LinkReasonRelay, Error: "dial: refused"}},
	}}
}

// TestPeersTab: the settings' Peers tab lists the active projects to
// share first, then the paired servers as tm peer list does (alias,
// link, tm, relay) with what the mirror adds; x on a server's row
// removes it after asking, and the list is read again; n keeps it.
func TestPeersTab(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.peers = peersList()
	press(m, ",", "5")
	sv, ok := m.top().(*settingsView)
	if !ok || sv.tab != tabPeers {
		t.Fatalf("top %T", m.top())
	}
	out := screen(m)
	for _, want := range []string{
		"5 peers",
		"SHARED PROJECTS",
		"[ ] terminatr",
		"[ ] web",
		"PAIRED SERVERS · this server 4F2A-91C3",
		"◉ mini  linked · 38 ms · tm 0.27.0 · shares 2 projects",
		"panes  [off] · view · write",
		"it can't open this server's panes",
		"relay  relay.example.net",
		"○ nas  offline 4m · relay · shares 1 project · dial: refused",
		"relay  relay.example.net:8443",
		"space toggle · ← → change · n pair · j join · e relay · u update relay · x remove · esc close",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// x on a project's row does nothing.
	press(m, "x")
	if _, ok := m.top().(*settingsView); !ok {
		t.Fatalf("x on a project: %T", m.top())
	}
	// Past the two projects and mini's panes and relay rows: nas's panes.
	press(m, "down", "down", "down", "down", "x")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("x: %T", m.top())
	}
	press(m, "n")
	if len(src.unpinned) != 0 || m.msg != "nas kept" {
		t.Fatalf("n removed %v, %q", src.unpinned, m.msg)
	}
	press(m, "down", "x")
	drain(t, m, press(m, "y"))
	if !slices.Equal(src.unpinned, []string{"nas"}) {
		t.Fatalf("removed %v", src.unpinned)
	}
	if len(sv.peers.list.Peers) != 1 || !strings.HasPrefix(m.msg, "removed nas;") {
		t.Errorf("after: %+v %q", sv.peers.list.Peers, m.msg)
	}
	for list, want := range map[string]PeerList{
		"isn't paired with another": {},
		"No paired servers":         {ID: "x", Short: "X"},
	} {
		src.peers = want
		sv.peers.load(m)
		out := screen(m)
		if !strings.Contains(out, list) || !strings.Contains(out, "No relay yet?") {
			t.Errorf("missing %q or the relay hint in\n%s", list, out)
		}
		// The projects to share show with nothing paired too.
		if !strings.Contains(out, "[ ] terminatr") {
			t.Errorf("no projects to share with %q:\n%s", list, out)
		}
	}
}

// TestPeersTabShare: space on a project toggles its Shared with paired
// servers, the setting its own Settings tab shows: sharing asks first
// (n keeps it), unsharing goes at once; a click selects a project and a
// click on the selected one toggles it.
func TestPeersTabShare(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.peers = peersList()
	press(m, ",", "5")
	screen(m)
	press(m, "space")
	if cv, ok := m.top().(*confirmView); !ok || cv.title != "Share terminatr" {
		t.Fatalf("space: %T", m.top())
	}
	press(m, "n")
	if len(src.settings) != 0 || m.msg != "not shared" {
		t.Fatalf("n: %v %q", src.settings, m.msg)
	}
	press(m, "space")
	drain(t, m, press(m, "y"))
	if !slices.Equal(src.settings, []string{"projects.terminatr.share=true"}) || !strings.Contains(screen(m), "[x] terminatr") {
		t.Fatalf("y: %v\n%s", src.settings, screen(m))
	}
	drain(t, m, press(m, "space"))
	if _, ok := m.top().(*settingsView); !ok {
		t.Fatalf("unsharing asked: %T", m.top())
	}
	if !slices.Equal(src.settings, []string{"projects.terminatr.share=true", "projects.terminatr.share=false"}) || !strings.Contains(screen(m), "[ ] terminatr") {
		t.Fatalf("off: %v\n%s", src.settings, screen(m))
	}
	clickOn(t, m, "[ ] web")
	sv := m.top().(*settingsView)
	if sv.peers.sel != 1 {
		t.Fatalf("click selects: sel %d", sv.peers.sel)
	}
	clickOn(t, m, "[ ] web")
	if cv, ok := m.top().(*confirmView); !ok || cv.title != "Share web" {
		t.Fatalf("click on the selected: %T", m.top())
	}
}

// TestPeersTabPanes: ← → on a server's panes row move its pane switch
// along off · view · write (docs/SPEC.md §16.12), asking before each
// step that lets it do more; n keeps it as it was; lowering applies at
// once. A click on a word sets that level.
func TestPeersTabPanes(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.peers = peersList()
	press(m, ",", "5")
	sv := m.top().(*settingsView)
	screen(m) // the tab reads the list as it draws
	press(m, "down", "down")
	press(m, "right")
	if _, ok := m.top().(*confirmView); !ok {
		t.Fatalf("→: %T", m.top())
	}
	press(m, "n")
	if len(src.panesSet) != 0 || m.msg != "panes unchanged for mini" {
		t.Fatalf("n: %v %q", src.panesSet, m.msg)
	}
	press(m, "right")
	drain(t, m, press(m, "y"))
	if out := screen(m); !strings.Contains(out, "off · [view] · write") || !strings.Contains(out, "it can watch the coordinators and threads") {
		t.Errorf("after view:\n%s", out)
	}
	press(m, "right")
	drain(t, m, press(m, "y"))
	if out := screen(m); !strings.Contains(out, "off · view · [write]") || !strings.Contains(m.msg, "panes write for mini") {
		t.Errorf("after write (%q):\n%s", m.msg, out)
	}
	// Past write → stays; ← lowers at once.
	press(m, "right")
	if sv.tab != tabPeers || len(src.panesSet) != 2 {
		t.Fatalf("→ at write: tab %d, set %v", sv.tab, src.panesSet)
	}
	drain(t, m, press(m, "left"))
	if _, ok := m.top().(*confirmView); ok {
		t.Fatal("lowering asked")
	}
	// A click on off lowers at once.
	x, y := at(t, m, "off · [view] · write")
	mouseAt(t, m, tea.MouseLeft, x, y)
	if _, ok := m.top().(*confirmView); ok {
		t.Fatal("off asked")
	}
	if !slices.Equal(src.panesSet, []string{"mini view", "mini type", "mini view", "mini off"}) || sv.peers.list.Peers[0].Panes != "off" {
		t.Fatalf("set %v, now %+v", src.panesSet, sv.peers.list.Peers[0])
	}
	// A click on write asks.
	x, y = at(t, m, "[off] · view · write")
	mouseAt(t, m, tea.MouseLeft, x+len("[off] · view · "), y)
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Let mini type into") {
		t.Fatalf("click on write: %T", m.top())
	}
	press(m, "esc")
	// ← → off a panes row switch tabs, as on the other tabs.
	press(m, "up", "up", "left")
	if sv.tab != tabMods {
		t.Errorf("← on a project: tab %d", sv.tab)
	}
}

// TestPeersTabLongName: a long host name as a server's alias is cut on
// its link line, so the link's words still show; its rows stay put.
func TestPeersTabLongName(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.peers = peersList()
	src.peers.Peers[0].Alias = "3a5a3800c87c-E2A37F3E8BF5.local.example.internal.lan"
	press(m, ",", "5")
	out := screen(m)
	for _, want := range []string{"◉ 3a5a3800c87c-E2A37F3E8B…  linked · 38 ms", "panes  [off] · view · write"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// pairStep runs a pairing's waiting command and takes the step it
// brings, returning the next one's.
func pairStep(t *testing.T, m *dash, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("no pairing step to wait for")
	}
	msg, ok := cmd().(pairMsg)
	if !ok {
		t.Fatalf("not a pairing step: %T", msg)
	}
	_, next := m.Update(msg)
	return next
}

// TestPeersTabRelay: e asks for the selected server's relay, starting
// from its own, and sets it as tm peer relay does; a pinned relay shows
// as its host, pinned.
func TestPeersTabRelay(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.peers = peersList()
	press(m, ",", "5")
	screen(m)
	// e on a project does nothing; on mini's panes row it asks.
	press(m, "e")
	if _, ok := m.top().(*settingsView); !ok {
		t.Fatalf("e on a project: %T", m.top())
	}
	press(m, "down", "down", "e")
	in, ok := m.top().(*inputView)
	if !ok || in.text != "wss://relay.example.net" {
		t.Fatalf("e: %T %+v", m.top(), m.top())
	}
	pinned := "wss://203.0.113.5#sha256=" + strings.Repeat("ab", 32)
	in.text = pinned
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.relaySet, []string{"mini " + pinned}) || m.msg != "relay for mini set; set it on mini too" {
		t.Fatalf("set %v, %q", src.relaySet, m.msg)
	}
	// enter on the relay row asks too.
	press(m, "down", "enter")
	if in, ok := m.top().(*inputView); !ok || in.title != "Relay for mini" {
		t.Fatalf("enter on the relay row: %T", m.top())
	}
	press(m, "esc")
	if got := relayWords(pinned); got != "203.0.113.5 (pinned)" {
		t.Errorf("relayWords %q", got)
	}
	for in, want := range map[string]string{
		"192.168.1.20": "192.168.1.20 is a private address",
		"wss://100.101.102.103#sha256=" + strings.Repeat("ab", 32): "100.101.102.103 is a private address",
		"relay.example.net": "",
	} {
		pv := &pairView{input: in}
		if got := pv.relayWarning(); !strings.HasPrefix(got, want) || (want == "" && got != "") {
			t.Errorf("relayWarning(%q) = %q", in, got)
		}
	}
	pv := &pairView{join: true, input: "10.0.0.2#sha256=" + strings.Repeat("ab", 32) + "/7Q4M-K2XD-9HVA-PT3C"}
	if got := pv.relayWarning(); !strings.HasPrefix(got, "10.0.0.2 is a private address") {
		t.Errorf("join relayWarning %q", got)
	}
}

// TestPeersTabPair: n asks for the relay, starting from the last one
// used; the code shows once it works, then the other server and its six
// digits, and y pins it: the list reads it, selected, and the footer
// says so without the code.
func TestPeersTabPair(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.peers = peersList()
	press(m, ",", "5")
	screen(m)
	press(m, "n")
	in, ok := m.top().(*inputView)
	if !ok || in.text != "wss://relay.example.net:8443" {
		t.Fatalf("n: %T %+v", m.top(), m.top())
	}
	cmd := press(m, "enter")
	if _, ok := m.top().(*pairView); !ok {
		t.Fatalf("enter: %T", m.top())
	}
	cmd = pairStep(t, m, cmd)
	out := screen(m)
	for _, want := range []string{"Pair a server", "Pairing code (works once, for 10 minutes; 10:00 left)", "relay.example.net/7Q4M-K2XD-9HVA-PT3C", "Waiting for the other server"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	cmd = pairStep(t, m, cmd)
	out = screen(m)
	for _, want := range []string{"The other server: studio.local", "server id 77AB-91C3 · tm 0.27.0", "Check: 381 204", "Pair only if the other side shows the same six digits.", "y they match · n they don't · esc cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	press(m, "y")
	pairStep(t, m, cmd)
	sv, ok := m.top().(*settingsView)
	if !ok {
		t.Fatalf("after: %T", m.top())
	}
	if !slices.Equal(src.pairs, []string{"pair wss://relay.example.net:8443"}) || m.msg != "paired with studio; its link comes up in a moment" {
		t.Errorf("pairs %v, %q", src.pairs, m.msg)
	}
	if it, _ := sv.peers.selected(m); it.kind != itemPanes || it.peer != 2 || !strings.Contains(screen(m), "○ studio  no link yet") {
		t.Errorf("sel %d:\n%s", sv.peers.sel, screen(m))
	}
}

// TestPeersTabJoin: j takes a code, shows the six digits; n says no,
// and the popup says why with enter to try again, which asks for the
// code again as typed.
func TestPeersTabJoin(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	press(m, ",", "5")
	if out := screen(m); !strings.Contains(out, "Press n to pair") {
		t.Errorf("empty tab:\n%s", out)
	}
	press(m, "j")
	if in, ok := m.top().(*inputView); !ok || in.text != "" {
		t.Fatalf("j: %T", m.top())
	}
	press(m, "r", "/", "x")
	cmd := pairStep(t, m, press(m, "enter"))
	if !strings.Contains(screen(m), "Check: 381 204") {
		t.Fatalf("join:\n%s", screen(m))
	}
	press(m, "n")
	pairStep(t, m, cmd)
	out := screen(m)
	if !strings.Contains(out, "Not paired: the digits weren't confirmed here") || !strings.Contains(out, "enter try again · esc close") {
		t.Fatalf("after n:\n%s", out)
	}
	press(m, "enter")
	if in, ok := m.top().(*inputView); !ok || in.text != "r/x" || in.title != "Join a server" {
		t.Fatalf("enter: %T", m.top())
	}
	if !slices.Equal(src.pairs, []string{"join r/x"}) || len(src.peers.Peers) != 0 {
		t.Errorf("pairs %v, peers %v", src.pairs, src.peers.Peers)
	}
}

// TestPeersTabPairErrors: a relay that can't be reached says so in the
// popup; esc while the code waits cancels the pairing and closes it.
func TestPeersTabPairErrors(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	press(m, ",", "5")
	screen(m)
	src.pairErr = pairWords(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, "wss://relay.example.net")
	press(m, "n", "r", "e", "l", "a", "y")
	pairStep(t, m, press(m, "enter"))
	if out := screen(m); !strings.Contains(out, "Not paired: can't reach the relay relay.example.net") {
		t.Fatalf("unreachable:\n%s", out)
	}
	press(m, "esc")
	if _, ok := m.top().(*settingsView); !ok {
		t.Fatalf("esc: %T", m.top())
	}

	src.pairErr = nil
	press(m, "n", "r", "e", "l", "a", "y")
	cmd := pairStep(t, m, press(m, "enter"))
	pv := m.top().(*pairView)
	press(m, "esc")
	if _, ok := m.top().(*settingsView); !ok || m.msg != "not paired" {
		t.Fatalf("esc: %T %q", m.top(), m.msg)
	}
	// The pairing ends unconfirmed; its steps find no popup.
	msg := cmd().(pairMsg)
	if msg.offer == nil {
		t.Fatalf("step %+v", msg)
	}
	m.Update(msg)
	if end := <-pv.events; !end.done || end.err == nil {
		t.Fatalf("end %+v", end)
	}
	if strings.Contains(m.msg, "7Q4M") {
		t.Errorf("the code in the footer: %q", m.msg)
	}
}

// TestPairDialogLongName: a long dotted host name stays on one line,
// cut with … to the dialog's width, so the rows below don't move.
func TestPairDialogLongName(t *testing.T) {
	m, _ := remoteDash(t, "@mini/acme-api")
	pv := &pairView{stage: pairCheck, offer: PairOffer{Name: "3a5a3800c87c-E2A37F3E8BF5.local.example.internal.lan", Short: "77AB-91C3", Version: "v0.27.0", Check: "381 204"}}
	var line string
	for _, l := range strings.Split(pv.render(m), "\n") {
		if strings.Contains(l, "The other server: ") {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "…") || strings.Contains(pv.render(m), "internal.lan") {
		t.Errorf("name not cut on one line:\n%s", pv.render(m))
	}
	if !strings.Contains(pv.render(m), "server id 77AB-91C3") || !strings.Contains(pv.render(m), "381 204") {
		t.Errorf("rows missing:\n%s", pv.render(m))
	}
}
