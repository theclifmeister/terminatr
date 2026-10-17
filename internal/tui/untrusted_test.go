package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/thread"
)

// Escape sequences a name from a paired server or an agent might carry:
// a clipboard write, a hyperlink, a DCS and an APC string.
const (
	osc52 = "\x1b]52;c;ZWNobyBvd25lZA==\a"
	osc8  = "\x1b]8;;https://example.net\x1b\\"
	dcs   = "\x1bPtmux;\x1b\x1b]52;c;eA==\a\x1b\\"
	apc   = "\x1b_Gf=24;AAAA\x1b\\"
	evil  = osc52 + osc8 + dcs + apc + "\u202e"
)

// leaks reports whether s holds an escape sequence other than a style,
// or a character textsafe drops.
func leaks(s string) bool {
	return plainStyled(s) != s || strings.Contains(s, "\x1b]") || strings.Contains(s, "\x1bP") || strings.Contains(s, "\x1b_")
}

// TestFitKeepsOnlyStyles: fit lets text and colour through, nothing else.
func TestFitKeepsOnlyStyles(t *testing.T) {
	styled := th().Accent.Render("ok") + " plain"
	if got := fit(styled, 20); !strings.Contains(got, th().Accent.Render("ok")) || ansi.StringWidth(got) != 20 {
		t.Fatalf("styles lost: %q", got)
	}
	if got := fit("a"+evil+"b\tc", 10); got != "ab c      " {
		t.Fatalf("%q", got)
	}
}

// TestStatusBarPeerText: a paired server's pane names its project, task
// and block reason; none reaches the terminal as anything but text.
func TestStatusBarPeerText(t *testing.T) {
	freshConsole(t)
	s := proto.SessionInfo{ID: "mini/s-1", Server: "mini" + evil, Project: "x" + evil, Role: proto.RoleThread, Thread: "t-0001" + evil, Task: "T1" + evil, State: "blocked", Reason: "r" + evil, Current: "c" + evil}
	if bar, _ := statusBar(s, nil, "title"+evil, false, 200, "", nil); leaks(bar) {
		t.Fatalf("%q", bar)
	}
}

// TestPeerRowsAreText: the sidebar's peer rows, the remote dashboard and
// the pairing dialog draw a peer's words as text.
func TestPeerRowsAreText(t *testing.T) {
	freshConsole(t)
	pr := proto.PeerMirrorProject{PeerProject: proto.PeerProject{Project: "p" + evil, Goal: "g" + evil},
		Sessions: []proto.PeerSession{{ID: "s-1", Role: proto.RoleCoordinator, Project: "p" + evil, State: "blocked" + evil, Model: "m" + evil}},
		Watch: &proto.ProjectWatch{
			NeedsYou: []proto.WatchNeed{{Why: proto.WhyReview, Task: "T1" + evil, Title: "t" + evil, Thread: "t-0001" + evil, PR: "#1" + evil}},
			Inbox:    []proto.WatchItem{{ID: "i", Kind: "k" + evil, Subject: "s" + evil, Summary: "x" + evil}},
			Threads:  []proto.WatchThread{{ID: "t-0001" + evil, Title: "x" + evil, Task: &proto.WatchTask{ID: "T1" + evil, Status: "started" + evil}, PR: "#1" + evil, Session: "s-1" + evil}},
			Ready:    []proto.WatchTodo{{Task: "T2" + evil, Title: "x" + evil, Status: "ready" + evil}},
		}}
	peers := []proto.PeerMirror{{Alias: "mini", State: proto.LinkUp, Projects: []proto.PeerMirrorProject{pr}}}
	for _, l := range sidebarLines(buildTree(nil, nil, treeIn{peers: peers}), 40, 20, nil) {
		if leaks(l) {
			t.Fatalf("sidebar %q", l)
		}
	}
	for _, l := range remoteLines(peers, "mini", "p"+evil, 120, time.Now()) {
		if leaks(l) {
			t.Fatalf("remote %q", l)
		}
	}
	m, _ := remoteDash(t, "@mini/acme-api")
	pv := &pairView{stage: pairCheck, offer: PairOffer{Name: "mini" + evil, Short: "77AB-91C3", Version: "v0.30" + evil + strings.Repeat("9", 300), Check: "381 204"}}
	for _, l := range strings.Split(pv.render(m), "\n") {
		if leaks(l) || ansi.StringWidth(l) > 200 {
			t.Fatalf("pairing %q", l)
		}
	}
}

// TestLibraryNamesAreText: a library file's name, which the agent chose,
// is drawn as text in the list, the delete question and the viewer.
func TestLibraryNamesAreText(t *testing.T) {
	freshConsole(t)
	pv := &projectView{libOK: true, library: []thread.LibFile{{Thread: "t-0001", Name: "plan" + evil + ".md", Size: 3}}}
	lines, _, _ := pv.libraryLines(80)
	for _, l := range lines {
		if leaks(l) {
			t.Fatalf("%q", l)
		}
	}
}
