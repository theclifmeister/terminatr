package server

import (
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// A remote pane by the server's display name is the pane by its alias;
// an alias, a local id and a name nobody has stay as they are.
func TestRemoteCanon(t *testing.T) {
	peers := []proto.PeerMirror{{Alias: "acme-lt-07", Name: "work"}, {Alias: "mini"}}
	for in, want := range map[string]string{
		"work/s-4":       "acme-lt-07/s-4",
		"acme-lt-07/s-4": "acme-lt-07/s-4",
		"mini/s-1":       "mini/s-1",
		"office/s-1":     "office/s-1",
		"s-3":            "s-3",
	} {
		if got := remoteCanonIn(peers, in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
