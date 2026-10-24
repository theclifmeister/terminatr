package tui

import "testing"

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
