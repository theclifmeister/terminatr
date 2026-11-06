package e2e

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/relay"
)

// A pane switch written to peers.json opens nothing on the far server, tm peer
// says it doesn't count, and tm peer allow keeps it (docs/SPEC.md §16.8).
func TestPeerSwitchInTheFileAlone(t *testing.T) {
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
	mini.MustCLI("thread", "start", "Fix the login", "--project", "demo")
	th := threadSession(t, mini, "demo", "t-0001")
	mini.WaitState(th, "idle", agentWait)
	remote := "mini/" + th.ID

	refused := func(what string) {
		t.Helper()
		var last string
		if !Poll(30*time.Second, func() bool {
			w := studio.Attach(120, 40, remote)
			w.WaitExit(wait)
			last = w.Screen()
			if strings.Contains(last, "doesn't share panes with this server") {
				return true
			}
			time.Sleep(time.Second) // the mirror reads mini again
			return false
		}) {
			t.Fatalf("%s:\n%s", what, last)
		}
	}
	refused("with panes off")

	if ok, err := peer.SetPanes(peer.DirIn(mini.Home), "studio", proto.PanesType); !ok || err != nil {
		t.Fatal(ok, err)
	}
	// The human's next change has the server read the file again, and
	// doesn't vouch for the rest of it.
	mini.MustCLI("peer", "name", "studio", "desk")
	refused("with write in the file alone, read again")
	if out := mini.MustCLI("peer", "list"); !strings.Contains(out, "doesn't count: panes write") || strings.Contains(out, "panes: write") {
		t.Fatalf("peer list: %s", out)
	}

	mini.MustCLI("peer", "allow", "studio")
	if out := mini.MustCLI("peer", "list"); strings.Contains(out, "doesn't count") || !strings.Contains(out, "panes: write") {
		t.Fatalf("peer list after allow: %s", out)
	}
	w := studio.Attach(120, 40, remote)
	w.WaitFor("Fake Claude Code", agentWait)
}
