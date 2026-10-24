package tui

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// netDash is remoteDash's dashboard w × h, with mini and nas paired as
// tm peer list says (peersList), studio as this host, on terminatr.
func netDash(t *testing.T, w, h int) (*dash, *fakeSource) {
	t.Helper()
	m, src := remoteDash(t, "terminatr")
	src.peers = peersList()
	host := thisHost
	thisHost = func() string { return "studio" }
	t.Cleanup(func() { thisHost = host })
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, src
}

// netOpen opens the Network popup with prefix+N (a plain N on the
// coordinator's row types through to it).
func netOpen(t *testing.T, m *dash) *networkView {
	t.Helper()
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	press(m, "N")
	nv, ok := m.top().(*networkView)
	if !ok {
		t.Fatalf("N opened %T", m.top())
	}
	return nv
}

// netBox is the popup as drawn: its lines, trimmed.
func netBox(m *dash) string {
	lines := strings.Split(whole(m), "\n")
	g := m.geo
	var out []string
	for _, l := range lines[g.y : g.y+g.h] {
		r := []rune(l)
		out = append(out, strings.TrimRight(string(r[g.x:min(g.x+g.w, len(r))]), " "))
	}
	return strings.Join(out, "\n")
}

// TestNetworkTree: N opens the popup on this server, its projects with
// their share box, then each paired server with its connection and the
// panes it is given, and what it shares from the mirror; nas is offline,
// its project stale.
func TestNetworkTree(t *testing.T) {
	m, _ := netDash(t, 80, 24)
	nv := netOpen(t, m)
	if nv.sel != "g:" {
		t.Errorf("sel %q", nv.sel)
	}
	out := netBox(m)
	for _, want := range []string{
		"Network · studio + 2 paired",
		"◉ studio · this server 4F2A-91C3",
		"shares 0 of 2",
		"terminatr",
		"[ ] not shared",
		"◉ mini · connected 38 ms",
		"panes off",
		"acme-api",
		"shared with you · + link (L)",
		"○ nas · offline 4m · relay",
		"media · stale",
		"enter fold · tab details · P pair · J join · ? keys · esc close",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "linked") {
		t.Errorf("a server's connection says linked:\n%s", out)
	}
	// ← folds a server, → unfolds it; ← on a project goes up to it.
	press(m, "left")
	if out := netBox(m); strings.Contains(out, "[ ] not shared") {
		t.Errorf("folded:\n%s", out)
	}
	press(m, "right", "down", "left")
	if nv.sel != "g:" {
		t.Errorf("← on a project: %q", nv.sel)
	}
	// A paired server's project: enter shows its dashboard.
	nv.sel = "t:mini/acme-api"
	drain(t, m, press(m, "enter"))
	if m.top() != nil || m.current != "@mini/acme-api" {
		t.Errorf("enter on theirs: %T %q", m.top(), m.current)
	}
}

// TestNetworkEmpty: nothing paired, the tree says how to pair and where
// a relay comes from; the projects to share show all the same.
func TestNetworkEmpty(t *testing.T) {
	m, src := netDash(t, 120, 30)
	src.peers = PeerList{}
	netOpen(t, m)
	out := netBox(m)
	for _, want := range []string{"No paired server yet. P pairs with one", "No relay yet?", "[ ] not shared", "Network · studio"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	src.peerErr = errors.New("peers.json: broken")
	m.top().(*networkView).load(m)
	if out := netBox(m); !strings.Contains(out, "The paired servers can't be read: peers.json: broken") {
		t.Errorf("error:\n%s", out)
	}
}

// TestNetworkShare: space on a project shares it after asking (N keeps
// it), and stops sharing it at once; the row says who sees it.
func TestNetworkShare(t *testing.T) {
	m, src := netDash(t, 120, 30)
	netOpen(t, m)
	press(m, "down", " ")
	if cv, ok := m.top().(*confirmView); !ok || cv.title != "Share terminatr" {
		t.Fatalf("space: %T", m.top())
	}
	press(m, "N")
	if len(src.settings) != 0 || m.msg != "not shared" {
		t.Fatalf("N: %v %q", src.settings, m.msg)
	}
	press(m, " ")
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.settings, []string{"projects.terminatr.share=true"}) || !strings.Contains(netBox(m), "[x] shared · seen by mini, nas") {
		t.Fatalf("Y: %v\n%s", src.settings, netBox(m))
	}
	drain(t, m, press(m, " "))
	if _, ok := m.top().(*networkView); !ok {
		t.Fatalf("unsharing asked: %T", m.top())
	}
	if !slices.Equal(src.settings, []string{"projects.terminatr.share=true", "projects.terminatr.share=false"}) {
		t.Fatalf("off: %v", src.settings)
	}
	// The panel's box takes a click.
	x, y := at(t, m, "shared        [ ]")
	mouseAt(t, m, tea.MouseLeft, x+len("shared        "), y)
	if cv, ok := m.top().(*confirmView); !ok || cv.title != "Share terminatr" {
		t.Fatalf("click on the box: %T", m.top())
	}
}

// TestNetworkPanes: V moves a paired server's panes along off · view ·
// write, asking before each raise (N keeps it), lowering at once; the
// panel's switch takes ← → and a click on a word.
func TestNetworkPanes(t *testing.T) {
	m, src := netDash(t, 120, 30)
	nv := netOpen(t, m)
	nv.sel = "g:mini"
	press(m, "V")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Let mini watch") {
		t.Fatalf("V: %T", m.top())
	}
	press(m, "N")
	if len(src.panesSet) != 0 || m.msg != "panes unchanged for mini" {
		t.Fatalf("N: %v %q", src.panesSet, m.msg)
	}
	press(m, "V")
	drain(t, m, press(m, "Y"))
	if out := netBox(m); !strings.Contains(out, "off · [view] · write") || !strings.Contains(out, "● watches your shared panes") {
		t.Errorf("after view:\n%s", out)
	}
	press(m, "V")
	drain(t, m, press(m, "Y"))
	// write → off lowers at once.
	drain(t, m, press(m, "V"))
	if _, ok := m.top().(*confirmView); ok {
		t.Fatal("lowering asked")
	}
	if !slices.Equal(src.panesSet, []string{"mini view", "mini type", "mini off"}) {
		t.Fatalf("set %v", src.panesSet)
	}
	// The panel: tab, down to panes, → asks.
	tab(m)
	if !nv.panel {
		t.Fatal("tab: the panel hasn't the keyboard")
	}
	press(m, "down", "down")
	press(m, "right")
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Let mini watch") {
		t.Fatalf("→ in the panel: %T", m.top())
	}
	press(m, "esc")
	x, y := at(t, m, "[off] · view · write")
	mouseAt(t, m, tea.MouseLeft, x+len("[off] · view · "), y)
	if cv, ok := m.top().(*confirmView); !ok || !strings.Contains(cv.question, "Let mini type into") {
		t.Fatalf("click on write: %T", m.top())
	}
}

// TestNetworkUnpair: X on a paired server asks (N keeps it), Y unpairs
// it as tm peer remove does, and the tree reads the list again.
func TestNetworkUnpair(t *testing.T) {
	m, src := netDash(t, 80, 24)
	nv := netOpen(t, m)
	press(m, "X")
	if _, ok := m.top().(*networkView); !ok {
		t.Fatalf("X on this server: %T", m.top())
	}
	nv.sel = "g:nas"
	press(m, "X")
	cv, ok := m.top().(*confirmView)
	if !ok || cv.title != "Unpair nas" || !strings.Contains(cv.question, "Its connection closes at once") {
		t.Fatalf("X: %T", m.top())
	}
	press(m, "N")
	if len(src.unpinned) != 0 || m.msg != "nas kept" {
		t.Fatalf("N: %v %q", src.unpinned, m.msg)
	}
	press(m, "X")
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.unpinned, []string{"nas"}) || !strings.HasPrefix(m.msg, "unpaired nas;") {
		t.Fatalf("unpaired %v, %q", src.unpinned, m.msg)
	}
	if out := netBox(m); strings.Contains(out, "nas ·") || !strings.Contains(out, "+ 1 paired") {
		t.Errorf("after:\n%s", out)
	}
}

// TestNetworkName: E asks for a paired server's name, empty with the
// alias as its placeholder; a name sets it, empty clears it, and a name
// that is taken says so.
func TestNetworkName(t *testing.T) {
	m, src := netDash(t, 80, 24)
	nv := netOpen(t, m)
	nv.sel = "g:mini"
	press(m, "E")
	in, ok := m.top().(*inputView)
	if !ok || in.title != "Name for mini" || in.value() != "" || in.placeholder != "mini" {
		t.Fatalf("E: %T %+v", m.top(), m.top())
	}
	in.lineEdit = newLine("office")
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.namesSet, []string{"mini office"}) || m.msg != "mini is called office here" {
		t.Fatalf("set %v, %q", src.namesSet, m.msg)
	}
	if out := netBox(m); !strings.Contains(out, "◉ office (mini) · connected") {
		t.Errorf("the tree didn't read the name again:\n%s", out)
	}
	press(m, "E")
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.namesSet, []string{"mini office", "mini "}) || m.msg != "mini shows by its alias again" {
		t.Fatalf("cleared %v, %q", src.namesSet, m.msg)
	}
	press(m, "E", "esc")
	if m.msg != "name unchanged for mini" || len(src.namesSet) != 2 {
		t.Errorf("esc: %v, %q", src.namesSet, m.msg)
	}
	src.nameErr = errors.New("name-taken: web is already a project here")
	press(m, "E")
	m.top().(*inputView).lineEdit = newLine("web")
	drain(t, m, press(m, "enter"))
	if !strings.Contains(m.msg, "name-taken") || len(src.namesSet) != 2 {
		t.Errorf("taken: %v, %q", src.namesSet, m.msg)
	}
}

// TestNetworkRelay: the panel's relay row edits a server's relay,
// starting from its own, as tm peer relay does; U asks before its relay
// updates itself.
func TestNetworkRelay(t *testing.T) {
	m, src := netDash(t, 120, 30)
	src.peers.Peers[0].Link.RelayVersion = "v0.27.0"
	m.data.Server = ServerInfo{Version: "v0.27.0", Latest: "v0.28.0"}
	nv := netOpen(t, m)
	nv.sel = "g:mini"
	out := netBox(m)
	for _, want := range []string{"relay.example.net · v0.27.0 ·", "update (U)", "U update relay"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	tab(m)
	press(m, "down", "enter")
	in, ok := m.top().(*inputView)
	if !ok || in.title != "Relay for mini" || in.value() != "wss://relay.example.net" {
		t.Fatalf("enter on the relay row: %T %+v", m.top(), m.top())
	}
	pinned := "wss://203.0.113.5#sha256=" + strings.Repeat("ab", 32)
	in.lineEdit = newLine(pinned)
	drain(t, m, press(m, "enter"))
	if !slices.Equal(src.relaySet, []string{"mini " + pinned}) || m.msg != "relay for mini set; set it on mini too" {
		t.Fatalf("set %v, %q", src.relaySet, m.msg)
	}
	if got := relayWords(pinned); got != "203.0.113.5 (pinned)" {
		t.Errorf("relayWords %q", got)
	}
	press(m, "U")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "runs tm v0.27.0; v0.28.0 is out") {
		t.Fatalf("U: %T", m.top())
	}
	drain(t, m, press(m, "Y"))
	if !slices.Equal(src.relayUpdates, []string{"mini"}) || m.msg != "the relay updates" {
		t.Fatalf("asked %v, %q", src.relayUpdates, m.msg)
	}
	// Under the panel's width: the relay row in the tree's place, on tab.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	nv.panel = false
	tab(m)
	if out := netBox(m); !strings.Contains(out, "WHAT MINI MAY DO HERE") || strings.Contains(out, "this server 4F2A-91C3") {
		t.Errorf("narrow panel:\n%s", out)
	}
}

// TestNetworkPair: P asks for the relay, starting from the last one
// used; the code, the six digits and Y pair, and the tree reads the new
// server, selected. J takes a code; N says no, and enter asks again.
func TestNetworkPair(t *testing.T) {
	m, src := netDash(t, 80, 24)
	nv := netOpen(t, m)
	press(m, "P")
	in, ok := m.top().(*inputView)
	if !ok || in.value() != "wss://relay.example.net:8443" {
		t.Fatalf("P: %T", m.top())
	}
	cmd := pairStep(t, m, press(m, "enter"))
	if out := screen(m); !strings.Contains(out, "On the other server: N (Network), then J") {
		t.Errorf("code:\n%s", out)
	}
	cmd = pairStep(t, m, cmd)
	press(m, "Y")
	pairStep(t, m, cmd)
	if m.top() != overlay(nv) || nv.sel != "g:studio" || m.msg != "paired with studio; its link comes up in a moment" {
		t.Fatalf("after: %T %q %q", m.top(), nv.sel, m.msg)
	}
	if out := netBox(m); !strings.Contains(out, "○ studio · no connection yet") {
		t.Errorf("the new server:\n%s", out)
	}

	press(m, "J")
	press(m, "r", "/", "x")
	cmd = pairStep(t, m, press(m, "enter"))
	press(m, "N")
	pairStep(t, m, cmd)
	if out := screen(m); !strings.Contains(out, "Not paired: the digits weren't confirmed here") {
		t.Fatalf("after N:\n%s", out)
	}
	press(m, "enter")
	if in, ok := m.top().(*inputView); !ok || in.value() != "r/x" || in.title != "Join a server" {
		t.Fatalf("enter: %T", m.top())
	}
	if !slices.Equal(src.pairs, []string{"pair wss://relay.example.net:8443", "join r/x"}) {
		t.Errorf("pairs %v", src.pairs)
	}
}

// TestNetworkPairErrors: a relay that can't be reached says so in the
// pairing popup; esc while the code waits cancels it, back on Network.
func TestNetworkPairErrors(t *testing.T) {
	m, src := netDash(t, 80, 24)
	netOpen(t, m)
	src.pairErr = pairWords(&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, "wss://relay.example.net")
	press(m, "P")
	m.top().(*inputView).lineEdit = newLine("relay")
	pairStep(t, m, press(m, "enter"))
	if out := screen(m); !strings.Contains(out, "Not paired: can't reach the relay relay.example.net") {
		t.Fatalf("unreachable:\n%s", out)
	}
	press(m, "esc")
	if _, ok := m.top().(*networkView); !ok {
		t.Fatalf("esc: %T", m.top())
	}
}

// TestNetworkKeys: N on a row that doesn't type through opens the popup,
// and ? lists its keys; esc closes each.
func TestNetworkKeys(t *testing.T) {
	m, _ := netDash(t, 80, 24)
	m.sel = "p:web"
	press(m, "N")
	if _, ok := m.top().(*networkView); !ok {
		t.Fatalf("N: %T", m.top())
	}
	press(m, "?")
	if _, ok := m.top().(*netKeysView); !ok || !strings.Contains(screen(m), "E V U X") {
		t.Fatalf("?: %T\n%s", m.top(), screen(m))
	}
	press(m, "esc", "esc")
	if m.top() != nil {
		t.Errorf("esc: %T", m.top())
	}
}

// TestNetworkTextFromAPeerIsPlain: a version or relay version the other
// end chose shows as plain text, whatever it holds.
func TestNetworkTextFromAPeerIsPlain(t *testing.T) {
	m, src := netDash(t, 160, 36)
	l := src.peers.Peers[0].Link
	l.RelayVersion = "v0.27.0\x1b]52;c;AAAA\x07\x1b[2J"
	l.Version = "v0.27.0\x1b]0;owned\x07"
	m.data.Peers[0].Version = ""
	nv := netOpen(t, m)
	nv.sel = "g:mini"
	if out := m.render(); strings.Contains(out, "\x1b]52") || strings.Contains(out, "\x1b]0;owned") {
		t.Fatalf("raw bytes in\n%q", out)
	}
}

// TestNetworkGolden: the popup at 80×24 (tree and two detail lines),
// 120×30 and 160×36 (the details panel beside it), on this server and
// on mini, matching testdata/network-<w>x<h>.golden (go test -run
// TestNetworkGolden -update rewrites them).
func TestNetworkGolden(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 36}} {
		m, src := netDash(t, size[0], size[1])
		src.peers.Peers[0].Name = "work"
		src.peers.Peers[0].Panes = "view"
		src.peers.Peers[0].Link.RelayVersion = "v0.27.0"
		m.data.Server = ServerInfo{Version: "v0.27.0", Latest: "v0.28.0"}
		m.data.Peers[0].Pane, m.data.Peers[0].Panes, m.data.Peers[0].Popup = true, "view", true
		nv := netOpen(t, m)
		var b strings.Builder
		for _, sel := range []string{"g:", "p:terminatr", "g:mini", "t:mini/acme-api"} {
			nv.sel = sel
			b.WriteString("== " + sel + "\n" + ansi.Strip(netBox(m)) + "\n")
		}
		got := b.String()
		path := filepath.Join("testdata", fmt.Sprintf("network-%dx%d.golden", size[0], size[1]))
		if *updateGolden {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (go test -run TestNetworkGolden -update writes it)", err)
		}
		if got != string(want) {
			t.Errorf("the Network popup differs from %s:\n%s", path, got)
		}
	}
}

// tab presses tab.
func tab(m *dash) { m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) }
