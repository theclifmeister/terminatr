package e2e

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/emu"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// viewReports is what a terminal sends a console with no one typing:
// focus in and out, mouse motion and wheel over the pane, and its
// answers to the console's queries (colours, DA, DSR, kitty flags,
// colour scheme, window size).
var viewReports = []string{
	"\x1b[I", "\x1b[O", "\x1b[I",
	"\x1b[<35;80;10M", "\x1b[<35;81;11M", "\x1b[<64;80;10M", "\x1b[<65;80;10M",
	"\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\", "\x1b]10;rgb:d4d4/d4d4/d4d4\x07",
	"\x1b]4;1;rgb:cccc/0000/0000\x1b\\", "\x1b[?62;22c", "\x1b[?1u",
	"\x1b[?997;1n", "\x1b[12;40R", "\x1b[4;800;1200t",
}

// TestRemotePaneViewNoTakeover (T686): studio shows mini's thread pane,
// writable, and its terminal sends what terminals send with no one
// typing, in the sidebar and in the pane: mini raises no takeover, and
// nothing reaches the agent's prompt. The first key typed into the pane
// raises one, and mini's log names it.
func TestRemotePaneViewNoTakeover(t *testing.T) {
	mini, _, _ := threadEnv(t)
	studio := New(t)
	rs := relay.New(relay.DefaultConfig())
	hs := httptest.NewServer(rs)
	t.Cleanup(func() { rs.Shutdown(); hs.Close() })
	pairDirect(t, studio, mini, "studio", "mini", "ws"+strings.TrimPrefix(hs.URL, "http"))
	writeConfig(t, mini, "[projects.demo]\nshare = true\n")

	mini.MustCLI("server", "start")
	studio.MustCLI("server", "start")
	waitPeerState(t, studio, "mini", "up", "")
	mini.MustCLI("peer", "panes", "studio", "write")
	mini.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)

	const cols, rows = 150, 34
	w := studio.Window(cols, rows)
	w.WaitFor("SESSIONS", wait)
	y := -1
	w.WaitUntil("mini's thread row", agentWait, func(sc string) bool { y = treeRow(sc, "demo", "t-0001"); return y >= 0 })
	w.Click(4, y)
	w.WaitUntil("mini's thread pane", agentWait, func(sc string) bool {
		return strings.Contains(sc, "Fake Claude Code") && lastLine(sc, "mini › demo › ")
	})
	report := func() {
		for _, r := range viewReports {
			w.Type(r)
			time.Sleep(50 * time.Millisecond)
		}
	}
	// The sidebar has the keyboard, the pane shows; then enter goes into
	// the pane.
	report()
	w.Key(emu.Key{Special: emu.KeyEnter})
	w.WaitFor("remote · ", wait)
	report()
	takeover := func() bool {
		return strings.Contains(mini.MustCLI("inbox", "list", "--project", "demo"), "takeover")
	}
	if Poll(3*time.Second, takeover) {
		t.Fatalf("viewing raised a takeover:\n%s", mini.MustCLI("inbox", "list", "--project", "demo"))
	}
	if sc := mini.Screen(th); strings.TrimSpace(promptLine(sc)) != "❯" {
		t.Fatalf("something reached the agent's prompt:\n%s", sc)
	}

	w.Type("x")
	if !Poll(agentWait, func() bool { return strings.TrimSpace(promptLine(mini.Screen(th))) == "❯ x" }) {
		t.Fatalf("the key never reached the prompt:\n%s", mini.Screen(th))
	}
	waitInbox(t, mini, "from the paired server studio")
	b, _ := os.ReadFile(filepath.Join(mini.Home, "logs", "server.log"))
	if !strings.Contains(string(b), `takeover from peer:studio on "x"`) {
		t.Fatalf("mini's log doesn't name the key that took over:\n%s", b)
	}
}

// promptLine is the screen's last line with the prompt box's ❯.
func promptLine(sc string) string {
	line := ""
	for _, l := range strings.Split(sc, "\n") {
		if strings.Contains(l, "❯") {
			line = l
		}
	}
	return line
}
