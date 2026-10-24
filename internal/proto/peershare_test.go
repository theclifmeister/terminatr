package proto

import "testing"

// TestShareWaits: only the incoming requests that wait count, across the
// peers.
func TestShareWaits(t *testing.T) {
	peers := []PeerMirror{
		{Alias: "mini", Shares: []ShareRequest{{ID: "S-AAAA-1", State: ShareWaiting}, {ID: "S-BBBB-2", Out: true, State: ShareWaiting}}},
		{Alias: "nas", Shares: []ShareRequest{{ID: "S-CCCC-3", State: ShareIsDeclined}}},
		{Alias: "lab", Shares: []ShareRequest{{ID: "S-DDDD-4", State: ShareWaiting}}},
	}
	if n := ShareWaits(peers); n != 2 {
		t.Fatalf("ShareWaits %d", n)
	}
}
