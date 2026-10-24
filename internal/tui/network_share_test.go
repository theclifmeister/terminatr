package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// shareDash is netDash with nas sharing nothing, and mini asking this
// server to share a project for its acme when ask is set.
func shareDash(t *testing.T, w, h int, ask bool) (*dash, *fakeSource) {
	t.Helper()
	m, src := netDash(t, w, h)
	src.data.Peers[1].Projects = nil
	if ask {
		src.data.Peers[0].Shares = []proto.ShareRequest{{ID: "S-91C3-7", Peer: "mini", For: "acme", Note: "the API docs", State: proto.ShareWaiting, Created: peersAt}}
	}
	m.setData(src.data)
	return m, src
}

// menuLabels are an open menu's items' labels.
func menuLabels(t *testing.T, m *dash) (string, []string) {
	t.Helper()
	mv, ok := m.top().(*menuView)
	if !ok {
		t.Fatalf("no menu: %T", m.top())
	}
	var out []string
	for _, it := range mv.items {
		out = append(out, it.label)
	}
	return mv.title, out
}

// TestNetworkShareAsk: a paired server that shares nothing says S asks
// its user; S is a menu of which of your projects it is for, then a note
// with a faint placeholder; the request's row waits under that server.
// Asked again it is refused at once, in words, as it is for a tm that
// takes no share requests; the server's refusal shows in its words.
func TestNetworkShareAsk(t *testing.T) {
	m, src := shareDash(t, 80, 24, false)
	nv := netOpen(t, m)
	if out := netBox(m); !strings.Contains(out, "shares nothing with you yet · S asks its user to share one") {
		t.Fatalf("no hint:\n%s", out)
	}
	nv.sel = "g:nas"
	if out := netBox(m); !strings.Contains(out, "S ask to share") {
		t.Errorf("no S in the keys:\n%s", out)
	}
	press(m, "S")
	title, items := menuLabels(t, m)
	if title != "Ask nas to share a project · which of yours is it for?" || !slices.Equal(items, []string{"for terminatr", "for web", "for no project in particular"}) {
		t.Fatalf("menu %q %q", title, items)
	}
	press(m, "enter")
	in, ok := m.top().(*inputView)
	if !ok || in.placeholder != "what it's for (optional)" || in.value() != "" || !strings.Contains(in.label, "For terminatr.") {
		t.Fatalf("note: %T %+v", m.top(), m.top())
	}
	in.lineEdit = newLine("the API docs")
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.shares, []string{"ask nas terminatr the API docs"}) || m.msg != "asked nas to share a project (S-4F2A-31, for terminatr); its user answers there" {
		t.Fatalf("asked %q, %q", src.shares, m.msg)
	}
	out := netBox(m)
	for _, want := range []string{"└─ asked (S-4F2A-31, for terminatr)", "waiting on nas's user", "asked nas to share a project (S-4F2A-31, for terminatr)"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "S asks its user to share one") || nv.sel != "a:nas" {
		t.Errorf("sel %q, still the hint:\n%s", nv.sel, out)
	}

	// Asked again: refused at once, on the server and on the request.
	for _, sel := range []string{"g:nas", "a:nas"} {
		nv.sel = sel
		press(m, "S")
		if _, ok := m.top().(*networkView); !ok || m.msg != "asked already (S-4F2A-31): it waits for nas's user; one share request waits per paired server" {
			t.Fatalf("again on %s: %T %q", sel, m.top(), m.msg)
		}
	}

	// For no project, without a note.
	nv.sel = "g:mini"
	press(m, "S", "down", "down", "enter")
	drain(t, m, press(m, "enter"))
	if src.shares[1] != "ask mini  " || m.msg != "asked mini to share a project (S-4F2A-32); its user answers there" {
		t.Fatalf("asked %q, %q", src.shares, m.msg)
	}
	press(m, "S")
	if !strings.HasPrefix(m.msg, "asked already (S-4F2A-32)") {
		t.Fatalf("mini again: %q", m.msg)
	}
	// esc on the note asks nothing.
	src.data.Peers[1].Shares = nil
	m.setData(src.data)
	nv.sel = "g:nas"
	press(m, "S", "enter", "esc")
	if len(src.shares) != 2 || m.msg != "nothing asked of nas" {
		t.Fatalf("esc: %q %q", src.shares, m.msg)
	}

	// An older tm: refused at once.
	src.peers.Peers[1].Link.Caps = []string{peer.CapMsg}
	nv.load(m)
	press(m, "S")
	if _, ok := m.top().(*networkView); !ok || m.msg != "nas runs a tm before 0.50.0, which doesn't take share requests; ask its user yourself" {
		t.Fatalf("old tm: %T %q", m.top(), m.msg)
	}

	// The server's refusal, in its words.
	src.peers.Peers[1].Link.Caps = []string{peer.CapMsg, peer.CapShare}
	nv.load(m)
	src.shareErr = errors.New("at most 5 share requests an hour go to nas; ask again later")
	press(m, "S", "enter")
	drain(t, m, press(m, "enter"))
	if m.errMsg != "at most 5 share requests an hour go to nas; ask again later" {
		t.Fatalf("refusal %q", m.errMsg)
	}
}

// TestNetworkShareRequest: a paired server's request is a flagged row
// under it, a need for ! and counted in NEEDS YOU; A lists your active
// projects that aren't shared, then the usual share confirm, whose Y
// accepts with that project; X declines after a Y.
func TestNetworkShareRequest(t *testing.T) {
	m, src := shareDash(t, 80, 24, true)
	if n := m.needsCount(); n != needsYouCount(m.data.Projects, m.data.Sessions)+1 {
		t.Errorf("the header counts %d", n)
	}
	nv := netOpen(t, m)
	out := netBox(m)
	if !strings.Contains(out, `└─⚑ asks you to share a project (for its acme) · "the API docs"`) {
		t.Fatalf("no request row:\n%s", out)
	}
	press(m, "!")
	if nv.sel != "s:mini:S-91C3-7" {
		t.Fatalf("! went to %q", nv.sel)
	}
	out = netBox(m)
	for _, want := range []string{"mini asks you to share a project (S-91C3-7), for its acme", "A choose a project · X decline · ! next · esc close"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}

	// X, then N: nothing.
	press(m, "X")
	if cv, ok := m.top().(*confirmView); !ok || cv.question != "Decline mini's request (S-91C3-7)? Nothing is shared; mini hears of it." {
		t.Fatalf("X: %T", m.top())
	}
	press(m, "N")
	if len(src.shares) != 0 || m.msg != "the request still waits" {
		t.Fatalf("N: %q %q", src.shares, m.msg)
	}

	// A: the projects not shared; the confirm says every paired server.
	src.data.Projects[1].Safety = nil
	press(m, "A")
	title, items := menuLabels(t, m)
	if title != "Share a project with mini? · pick one of yours" || !slices.Equal(items, []string{"terminatr", "web"}) {
		t.Fatalf("A: %q %q", title, items)
	}
	press(m, "enter")
	cv, ok := m.top().(*confirmView)
	if !ok || cv.title != "Share terminatr" || !strings.HasPrefix(cv.question, "Share terminatr with every paired server? mini asked for a project (S-91C3-7, for its acme).") {
		t.Fatalf("confirm: %T %+v", m.top(), m.top())
	}
	press(m, "N")
	if len(src.shares) != 0 || m.msg != "not shared; the request still waits" {
		t.Fatalf("N: %q %q", src.shares, m.msg)
	}
	press(m, "A", "enter")
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.shares, []string{"accept S-91C3-7 terminatr"}) || m.msg != "terminatr is shared with every paired server; mini hears its request was answered (S-91C3-7)" {
		t.Fatalf("Y: %q %q", src.shares, m.msg)
	}
	out = netBox(m)
	if strings.Contains(out, "asks you to share") || !strings.Contains(out, "[x] shared") || nv.sel != "p:terminatr" {
		t.Errorf("after: sel %q\n%s", nv.sel, out)
	}
	if n := m.needsCount(); n != needsYouCount(m.data.Projects, m.data.Sessions) {
		t.Errorf("still counted: %d", n)
	}

	// Another request: X, Y declines it.
	src.data.Peers[0].Shares = []proto.ShareRequest{{ID: "S-91C3-8", Peer: "mini", State: proto.ShareWaiting, Created: peersAt}}
	m.setData(src.data)
	nv.sel = "s:mini:S-91C3-8"
	if out := netBox(m); !strings.Contains(out, "└─⚑ asks you to share a project") || strings.Contains(out, "(for its") {
		t.Fatalf("no plain request:\n%s", out)
	}
	press(m, "X")
	drain(t, m, press(m, "Y"))
	if src.shares[1] != "decline S-91C3-8" || m.msg != "declined S-91C3-8; nothing was shared, mini hears of it" {
		t.Fatalf("declined %q %q", src.shares, m.msg)
	}

	// Everything shared: A says so, X declines.
	src.data.Peers[0].Shares = []proto.ShareRequest{{ID: "S-91C3-9", Peer: "mini", State: proto.ShareWaiting, Created: peersAt}}
	for i := range src.data.Projects {
		p := &src.data.Projects[i]
		if p.Safety == nil {
			sf := *src.data.Projects[0].Safety
			p.Safety = &sf
		}
		p.Safety.Share = true
	}
	m.setData(src.data)
	nv.sel = "s:mini:S-91C3-9"
	press(m, "A")
	if _, ok := m.top().(*networkView); !ok || m.msg != "every active project here is shared already; X declines the request" {
		t.Fatalf("all shared: %T %q", m.top(), m.msg)
	}
}

// TestNeedsYouShareRequest: the all-projects NEEDS YOU has a row per
// share request waiting, whose enter opens Network at it; a project's
// counts it as network.
func TestNeedsYouShareRequest(t *testing.T) {
	m, _ := shareDash(t, 120, 30, true)
	rows := needsRows(m.data, allProjects)
	i := slices.IndexFunc(rows, func(r row) bool { return r.key == "ns:mini:S-91C3-7" })
	if i < 0 || rows[i].what != "a share request" || rows[i].who != "mini" || rows[i].rest != "asks you to share a project for its acme · enter opens Network" {
		t.Fatalf("rows %+v", rows)
	}
	m.current = allProjects
	m.setData(m.data)
	m.sel = "ns:mini:S-91C3-7"
	press(m, "enter")
	if nv, ok := m.top().(*networkView); !ok || nv.sel != "s:mini:S-91C3-7" {
		t.Fatalf("enter: %T", m.top())
	}
	d := m.data
	d.Projects = d.Projects[:1]
	d.Projects[0].Links = nil
	for _, r := range buildRows(d, "terminatr", []string{"needs"}, nil) {
		if r.head == "NEEDS YOU" && (r.network != 1 || r.count != 0) {
			t.Errorf("header %+v", r)
		}
	}
}

// TestNetworkShareGolden: the asked row and the request at 80×24 and
// 160×36, matching testdata/network-share-<w>x<h>.golden (-update
// rewrites them).
func TestNetworkShareGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 36}} {
		m, src := shareDash(t, size[0], size[1], true)
		src.data.Peers[1].Shares = []proto.ShareRequest{{ID: "S-4F2A-33", Out: true, Peer: "nas", For: "terminatr", State: proto.ShareWaiting, Created: peersAt}}
		m.setData(src.data)
		nv := netOpen(t, m)
		var b strings.Builder
		for _, sel := range []string{"g:mini", "s:mini:S-91C3-7", "a:nas"} {
			nv.sel = sel
			b.WriteString("== " + sel + "\n" + ansi.Strip(netBox(m)) + "\n")
		}
		got := b.String()
		path := filepath.Join("testdata", fmt.Sprintf("network-share-%dx%d.golden", size[0], size[1]))
		if *updateGolden {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (go test -run TestNetworkShareGolden -update writes it)", err)
		}
		if got != string(want) {
			t.Errorf("the Network popup differs from %s:\n%s", path, got)
		}
	}
}
