package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// remotePeers is the mirror of the remote panel's tests: mini, linked at
// 38 ms and sharing its panes, with acme-api (its coordinator with a
// question and context use, a thread under way on a task with steps and
// a failing PR); nas offline since 14:02 with media, stale.
func remotePeers() []proto.PeerMirror {
	synced := peersAt.Add(-4 * time.Minute)
	return []proto.PeerMirror{
		{Alias: "mini", ID: "m", State: proto.LinkUp, Since: peersAt.Add(-time.Hour), Watch: true, RTT: 37.6,
			Pane: true, Panes: proto.PanesType, Projects: []proto.PeerMirrorProject{
				{PeerProject: proto.PeerProject{Project: "acme-api", NeedsYou: 1, Threads: 2}, Synced: peersAt.Add(-2 * time.Second),
					Watch: &proto.ProjectWatch{Project: "acme-api",
						NeedsYou: []proto.WatchNeed{{Why: proto.WhyCI, Task: "T7", Title: "Billing export", Thread: "t-0007", Session: "r-2",
							PR: "#70 open, checks failed", PRURL: "https://github.com/o/acme/pull/70"}},
						Questions: 1,
						Context:   &proto.WatchContext{Tokens: 84_000, Window: 200_000, Percent: 42},
						Threads: []proto.WatchThread{
							{ID: "t-0007", Title: "Billing export", Session: "r-2", State: "working",
								Task: &proto.WatchTask{ID: "T7", Title: "Billing export", Status: "started", StepsDone: 2, StepsTotal: 5, Current: "Write the CSV writer"},
								PR:   "#70 open, checks failed", PRURL: "https://github.com/o/acme/pull/70", PRBad: true, PRChecks: "fail",
								PRChecked: peersAt.Add(-30 * time.Second), Reports: 2},
							{ID: "t-0008", Title: "Docs", Done: true},
						},
						Ready: []proto.WatchTodo{{Task: "T9", Title: "Rate limits", Status: "ready"}},
					},
					Sessions: []proto.PeerSession{
						{ID: "r-1", Role: proto.RoleCoordinator, Project: "acme-api", State: "working", Model: "opus", Context: 84_000, ContextWindow: 200_000},
						{ID: "r-2", Role: proto.RoleThread, Project: "acme-api", Thread: "t-0007", Task: "T7", State: "working",
							TodosDone: 1, TodosTotal: 3, Current: "Run the tests", Model: "sonnet", Context: 40_000, ContextWindow: 200_000},
					}},
			}},
		{Alias: "nas", ID: "n", State: proto.LinkOffline, Reason: proto.LinkReasonRelay, Since: synced, Watch: true, Projects: []proto.PeerMirrorProject{
			{PeerProject: proto.PeerProject{Project: "media", Threads: 1}, Stale: true, Synced: synced,
				Watch: &proto.ProjectWatch{Project: "media", Threads: []proto.WatchThread{
					{ID: "t-0003", Title: "Transcode", Session: "q-2", State: "idle", Task: &proto.WatchTask{ID: "T3", Title: "Transcode", Status: "started", StepsDone: 1, StepsTotal: 2}},
				}},
				Sessions: []proto.PeerSession{
					{ID: "q-1", Role: proto.RoleCoordinator, Project: "media", State: "working"},
					{ID: "q-2", Role: proto.RoleThread, Project: "media", Thread: "t-0003", State: "idle"},
				}},
		}},
	}
}

// remotePane is the SessionInfo of the pane "<alias>/<sid>", as the
// server attaches it.
func remotePane(alias, sid, role, project, thread string) proto.SessionInfo {
	return proto.SessionInfo{ID: alias + "/" + sid, Server: alias, Role: role, Project: project, Thread: thread}
}

// longAlias is a paired server called after a machine with a long name,
// as a CI runner's.
const longAlias = "runnervm-6qbd8-ubuntu-24-04-x64-standard-runner"

// TestRemotePanelGolden: the info panel beside a paired server's panes,
// from the mirror, matching testdata/remote-panel.golden (go test -run
// TestRemotePanelGolden -update rewrites it): mini's coordinator and
// thread, linked; nas's thread while nas is offline, dimmed with when it
// was last read; a server with a long name, narrow; a project no longer
// shared; a server no longer paired.
func TestRemotePanelGolden(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	peers := remotePeers()
	long := slices.Clone(peers[:1])
	long[0].Alias = longAlias
	var b strings.Builder
	write := func(name string, peers []proto.PeerMirror, in proto.SessionInfo, w int) {
		d := loadRemoteInfo(peers, in)
		if d == nil {
			t.Fatalf("%s: no panel", name)
		}
		fmt.Fprintf(&b, "%s (width %d)\n", name, w)
		for _, l := range infoLay(d, w, peersAt, panelOpts{rows: 40}).lines {
			if n := ansi.StringWidth(l); n > w {
				t.Errorf("%s: a line %d cells wide in %d: %q", name, n, w, ansi.Strip(l))
			}
			b.WriteString("│" + strings.TrimRight(ansi.Strip(l), " ") + "\n")
		}
		b.WriteString("\n")
	}
	write("mini's coordinator", peers, remotePane("mini", "r-1", proto.RoleCoordinator, "acme-api", ""), 40)
	write("mini's thread", peers, remotePane("mini", "r-2", proto.RoleThread, "acme-api", "t-0007"), 40)
	view := remotePane("mini", "r-2", proto.RoleThread, "acme-api", "t-0007")
	view.ViewOnly = true
	write("mini's thread, view-only", peers, view, 40)
	write("nas's thread, offline", peers, remotePane("nas", "q-2", proto.RoleThread, "media", "t-0003"), 40)
	write("a long server name", long, remotePane(longAlias, "r-2", proto.RoleThread, "acme-api", "t-0007"), 30)
	write("not shared any more", peers, remotePane("mini", "r-9", proto.RoleThread, "infra", "t-0001"), 40)
	write("not paired any more", nil, remotePane("mini", "r-1", proto.RoleCoordinator, "acme-api", ""), 40)

	path := filepath.Join("testdata", "remote-panel.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test -run TestRemotePanelGolden -update writes it)", err)
	}
	if got := b.String(); got != string(want) {
		t.Errorf("the remote panel differs from %s:\n%s", path, got)
	}
}

// TestLoadRemoteInfo: the pane's session is found by its id there; once
// that server ran it again as another session, by its project and
// thread (or the coordinator); a pane of any other role has no panel.
// Its slug is the remote project's place, "@alias/slug".
func TestLoadRemoteInfo(t *testing.T) {
	peers := remotePeers()
	d := loadRemoteInfo(peers, remotePane("mini", "r-2", proto.RoleThread, "acme-api", "t-0007"))
	if d == nil || !d.remote.found || d.remote.sess.ID != "r-2" || d.slug != "@mini/acme-api" || d.remote.project == nil || d.remote.stale() {
		t.Fatalf("by id: %+v", d)
	}
	d = loadRemoteInfo(peers, remotePane("mini", "r-77", proto.RoleThread, "acme-api", "t-0007"))
	if d == nil || !d.remote.found || d.remote.sess.ID != "r-2" {
		t.Fatalf("by thread: %+v", d)
	}
	d = loadRemoteInfo(peers, remotePane("mini", "r-78", proto.RoleCoordinator, "acme-api", ""))
	if d == nil || !d.remote.found || d.remote.sess.ID != "r-1" {
		t.Fatalf("by the coordinator: %+v", d)
	}
	if d := loadRemoteInfo(peers, remotePane("nas", "q-2", proto.RoleThread, "media", "t-0003")); d == nil || !d.remote.stale() {
		t.Fatalf("offline: %+v", d)
	}
	if d := loadRemoteInfo(peers, remotePane("mini", "r-5", proto.RoleShell, "acme-api", "")); d != nil {
		t.Fatalf("a shell: %+v", d)
	}
	if d := loadRemoteInfo(peers, proto.SessionInfo{ID: "s-1", Role: proto.RoleThread}); d != nil {
		t.Fatalf("this server's session: %+v", d)
	}
}

// TestRemotePanelHits: nothing in a remote panel acts on its server: no
// line opens a task or the questions; a PR opens in the browser; a
// running thread's row shows its pane, "<alias>/<id>", while the server
// shares its panes with this one, and nothing while it doesn't.
func TestRemotePanelHits(t *testing.T) {
	peers := remotePeers()
	coord := remotePane("mini", "r-1", proto.RoleCoordinator, "acme-api", "")
	kinds := func(peers []proto.PeerMirror, in proto.SessionInfo) map[hitKind][]infoHit {
		out := map[hitKind][]infoHit{}
		for _, h := range infoLay(loadRemoteInfo(peers, in), 40, peersAt, panelOpts{}).hits {
			out[h.kind] = append(out[h.kind], h)
		}
		return out
	}
	got := kinds(peers, coord)
	if len(got[hitTask]) > 0 || len(got[hitQuestions]) > 0 || len(got[hitSection]) > 0 {
		t.Fatalf("a coordinator's panel acts there: %v", got)
	}
	if len(got[hitSession]) == 0 || got[hitSession][0].session != "mini/r-2" {
		t.Fatalf("the thread row: %+v", got[hitSession])
	}
	if len(got[hitPR]) == 0 || got[hitPR][0].url != "https://github.com/o/acme/pull/70" {
		t.Fatalf("the PR: %+v", got[hitPR])
	}
	off := remotePeers()
	off[0].Panes = proto.PanesOff
	if got := kinds(off, coord); len(got[hitSession]) > 0 {
		t.Fatalf("panes off, a thread row opens one: %+v", got[hitSession])
	}
	got = kinds(peers, remotePane("mini", "r-2", proto.RoleThread, "acme-api", "t-0007"))
	if len(got[hitTask]) > 0 || len(got[hitPR]) == 0 {
		t.Fatalf("a thread's panel: %v", got)
	}
}

// TestRemotePanelKeys: beside a paired server's pane the panel polls
// from the mirror and shows no mod sections; its hint leaves out enter
// and a, which say the panel is view-only, and its arrows scroll.
func TestRemotePanelKeys(t *testing.T) {
	setIcons(IconsUnicode)
	defer resetConsole()
	infoClock = func() time.Time { return peersAt }
	defer func() { infoClock = time.Now }()
	c := infoClient(t, 200)
	c.rows = 10
	in := remotePane("mini", "r-1", proto.RoleCoordinator, "acme-api", "")
	c.v.Focus = in.ID
	c.focus = &pane{info: in}
	c.views = []proto.ModView{tickerView()}
	d, shown := c.pollInfo(&infoCache{}, nil, remotePeers())
	if d == nil || d.remote == nil || len(shown) != 0 {
		t.Fatalf("poll: %+v, shown %v", d, shown)
	}
	c.mu.Lock()
	c.showInfo(d)
	c.mu.Unlock()
	text := ansi.Strip(strings.Join(c.info.lines, "\n"))
	if strings.Contains(text, "ticker") || !strings.Contains(text, "◉ linked 38 ms · synced 2s ago") {
		t.Fatalf("the panel:\n%s", text)
	}
	key := func(k uv.Key) {
		c.mu.Lock()
		c.kb = areaInfo
		c.infoKeyboard(k)
	}
	if h := c.infoHintNow(); h != remoteHint {
		t.Fatalf("hint %q", h)
	}
	key(uv.Key{Code: 'a', Text: "a"})
	if c.flash != "mini's: view-only here" {
		t.Fatalf("a: %q", c.flash)
	}
	key(uv.Key{Code: uv.KeyEnter})
	if c.flash != "mini's: view-only here" || c.kb != areaInfo {
		t.Fatalf("enter: %q", c.flash)
	}
	key(uv.Key{Code: uv.KeyDown})
	if c.info.top != 1 {
		t.Fatalf("down: top %d", c.info.top)
	}
}
