package tui

import (
	"slices"
	"strings"
	"testing"
)

// TestPeersTabRelayUpdate: each link shows its relay's tm, "update
// available" when the latest release the server knows of is newer, and
// u asks before it has the selected server's relay update itself.
func TestPeersTabRelayUpdate(t *testing.T) {
	m, src := remoteDash(t, "@mini/acme-api")
	src.peers = peersList()
	src.peers.Peers[0].Link.RelayVersion = "v0.27.0"
	m.data.Server = ServerInfo{Version: "v0.27.0", Latest: "v0.28.0"}
	press(m, ",", "4")
	out := screen(m)
	if !strings.Contains(out, "relay relay.example.net · relay v0.27.0 · update available (u)") {
		t.Fatalf("no relay version in\n%s", out)
	}
	if !strings.Contains(out, "u update relay") {
		t.Errorf("no u in the keys:\n%s", out)
	}
	press(m, "u")
	cv, ok := m.top().(*confirmView)
	if !ok || !strings.Contains(cv.question, "runs tm v0.27.0; v0.28.0 is out") {
		t.Fatalf("u: %T %+v", m.top(), cv)
	}
	press(m, "n")
	if len(src.relayUpdates) != 0 {
		t.Fatalf("n asked: %v", src.relayUpdates)
	}
	press(m, "u")
	run(m, press(m, "y"))
	if !slices.Equal(src.relayUpdates, []string{"mini"}) || m.msg != "the relay updates" {
		t.Fatalf("asked %v, %q", src.relayUpdates, m.msg)
	}

	// The relay is at the latest: its version, nothing more.
	m.data.Server = ServerInfo{Version: "v0.28.0"}
	src.peers.Peers[0].Link.RelayVersion = "v0.28.0"
	m.top().(*settingsView).peers.load(m)
	if out := screen(m); !strings.Contains(out, "relay v0.28.0") || strings.Contains(out, "update available") {
		t.Fatalf("latest:\n%s", out)
	}
}

func TestRelayBehind(t *testing.T) {
	for _, c := range []struct {
		si     ServerInfo
		v      string
		behind bool
	}{
		{ServerInfo{Version: "v0.27.0", Latest: "v0.28.0"}, "v0.27.0", true},
		{ServerInfo{Version: "v0.28.0"}, "v0.27.0", true},  // the server is a newer release
		{ServerInfo{Version: "v0.27.0"}, "v0.28.0", false}, // the relay is newer
		{ServerInfo{Version: "dev"}, "v0.27.0", false},     // a source build knows no latest
		{ServerInfo{Version: "v0.28.0"}, "dev", false},
	} {
		if _, got := relayBehind(c.si, c.v); got != c.behind {
			t.Errorf("%+v %s: %v", c.si, c.v, got)
		}
	}
}
