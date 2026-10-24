package tui

import (
	"io"
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

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
	for _, k := range []string{"enter", "A", "T", "I", "S", "O", "|", "D"} {
		drain(t, m, press(m, k))
		if m.top() != nil || len(src.lifecycle) > 0 || len(src.opened) > 0 || len(src.adopted) > 0 {
			t.Fatalf("%q acted: %+v %v %v", k, m.top(), src.lifecycle, src.opened)
		}
		want := "acme-api is mini's: nothing here acts on it"
		if k == "A" || k == "T" || k == "I" {
			want = "acme-api is mini's: no project popup here; your messages with it are in the Messages tab of your project linked to it"
		}
		if m.msg != want {
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

// TestRemoteDashboardPrefix (T690): prefix then a key on the remote
// dashboard is no way round it: the project popup, its Tasks and Inbox
// tabs, save-and-clear and remote control neither open nor act on this
// server's project the list rows are of (once its first), and say why;
// settings and help still open.
func TestRemoteDashboardPrefix(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	popup := "acme-api is mini's: no project popup here; your messages with it are in the Messages tab of your project linked to it"
	nothing := "acme-api is mini's: nothing here acts on it"
	for _, c := range []struct{ key, want string }{
		{"a", popup}, {"A", popup}, {"t", popup}, {"T", popup}, {"i", popup}, {"I", popup},
		{"C", nothing}, {"r", nothing}, {"S", nothing}, {"O", nothing},
	} {
		drain(t, m, press(m, m.prefix))
		drain(t, m, press(m, c.key))
		if m.top() != nil || len(src.saveCleared) > 0 || len(src.remote) > 0 || len(src.lifecycle) > 0 || len(src.adopted) > 0 {
			t.Fatalf("prefix+%s acted: %T %v %v %v", c.key, m.top(), src.saveCleared, src.remote, src.lifecycle)
		}
		if m.msg != c.want {
			t.Errorf("prefix+%s: %q", c.key, m.msg)
		}
	}
	if slug := m.projectHere(); slug != "" {
		t.Errorf("project here %q", slug)
	}
	drain(t, m, press(m, m.prefix))
	drain(t, m, press(m, ","))
	if _, ok := m.top().(*settingsView); !ok {
		t.Errorf("prefix+,: %T", m.top())
	}
	drain(t, m, press(m, m.prefix))
	drain(t, m, press(m, "?"))
	if _, ok := m.top().(*helpView); !ok {
		t.Errorf("prefix+?: %T", m.top())
	}
	// On this server's project, prefix+a is its popup again.
	m, _ = remoteDash(t, "terminatr")
	drain(t, m, press(m, m.prefix))
	drain(t, m, press(m, "a"))
	if pv := m.projectPopupView(); pv == nil || pv.slug != "terminatr" {
		t.Errorf("prefix+a on terminatr: %+v", pv)
	}
}

// TestRemotePanePopup: over a paired server's pane, prefix+A, I and T
// open that server's popup of its project over the pane ("@alias/slug",
// remotepopup.go); when the sidebar's mirror says that server serves
// none to this one (an older tm, or its pane switch off), they say so
// in the status bar instead and the ≡ menu leaves them out. This
// server's panes and the other popups open as ever.
func TestRemotePanePopup(t *testing.T) {
	newClient := func(peers []proto.PeerMirror, side bool) *client {
		c := &client{dashboard: true, prefix: chord{'b'}, end: make(chan struct{}), log: log.New(io.Discard, "", 0),
			focus: &pane{info: proto.SessionInfo{ID: "mini/s-1", Server: "mini", Role: proto.RoleCoordinator, Project: "terminatr"}}}
		if side {
			c.side = &sidebar{peers: peers}
		}
		return c
	}
	serves := []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Popup: true, Pane: true, Panes: proto.PanesView}}
	old := []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Pane: true, Panes: proto.PanesView}}
	off := []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Popup: true, Pane: true, Panes: proto.PanesOff}}
	for _, k := range []string{"a", "A", "i", "t"} {
		for name, peers := range map[string][]proto.PeerMirror{"older tm": old, "switch off": off} {
			c := newClient(peers, true)
			c.key(uv.Key{Code: 'b', Mod: uv.ModCtrl})
			c.key(cmdKey(k))
			if c.over != nil {
				t.Fatalf("%s: prefix+%s opened %+v", name, k, c.over)
			}
			if want := "terminatr is mini's: no project popup here; your messages with it are in the Messages tab of your project linked to it"; c.flash != want {
				t.Errorf("%s: prefix+%s: %q", name, k, c.flash)
			}
			for _, it := range c.sessionItems() {
				if it.label == "project popup" || it.label == "tasks" || it.label == "inbox" {
					t.Errorf("%s: ≡ menu has %q", name, it.label)
				}
			}
		}
		for name, c := range map[string]*client{"serves": newClient(serves, true), "no sidebar": newClient(nil, false)} {
			c.key(uv.Key{Code: 'b', Mod: uv.ModCtrl})
			c.key(cmdKey(k))
			if c.over == nil || c.over.Project != "@mini/terminatr" || c.over.Key != strings.ToUpper(k) {
				t.Errorf("%s: prefix+%s: over %+v, flash %q", name, k, c.over, c.flash)
			}
		}
	}
	c := newClient(serves, true)
	if !slices.ContainsFunc(c.sessionItems(), func(it aitem) bool { return it.label == "project popup" }) {
		t.Error("≡ menu lacks the project popup of a server that serves it")
	}
	if c.remotePopup(",") {
		t.Error("settings refused")
	}
	c.focus.info.Server = ""
	if c.remotePopup("A") {
		t.Error("this server's pane refused")
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

// TestPeerDisplayName: a paired server with a display name (tm peer
// name) shows by it on the remote dashboard, its breadcrumb and the
// sidebar, while its rows stay keyed by the alias (the Network popup's
// E sets it: TestNetworkName).
func TestPeerDisplayName(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.data.Peers[0].Name = "work"
	m.setData(src.data)
	out := screen(m)
	for _, want := range []string{
		" tm dashboard · work › acme-api",
		"◉ work · linked 38 ms · synced 2s ago",
		"link a project of yours to work/acme-api (tm project link).",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "mini") {
		t.Errorf("the alias shows:\n%s", out)
	}
	var head *treeRow
	for _, r := range m.tree() {
		if r.kind == treeServer && r.peer == "mini" {
			head = &r
		}
	}
	if head == nil || head.server() != "work" || peerKey(*head) != "g:mini" {
		t.Fatalf("server header %+v", head)
	}
	if got := peerShownKey(src.data.Peers, "mini/acme-api"); got != "work/acme-api" {
		t.Errorf("peerShownKey %q", got)
	}
	if got := peerShownKey(src.data.Peers, "nas/x"); got != "nas/x" {
		t.Errorf("peerShownKey without a name %q", got)
	}
}
