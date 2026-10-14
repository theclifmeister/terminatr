package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/proto"
)

func TestPeerLines(t *testing.T) {
	if peerLines(nil, time.Now()) != nil {
		t.Error("lines without an identity key")
	}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	st := &proto.LinkStatus{ID: "4f2a91c3" + strings.Repeat("0", 56), Short: "4F2A-91C3", Peers: []proto.PeerLink{
		{Alias: "mini", State: proto.LinkUp, Since: now.Add(-3 * time.Minute), RTT: 41.6, Version: "0.27.0", Transport: "relay r.example.net"},
		{Alias: "nas", State: proto.LinkOffline, Reason: proto.LinkReasonRelay, Since: now.Add(-4 * time.Minute), Retry: now.Add(30 * time.Second),
			Error: "dial tcp: connection refused", Transport: "relay r.example.net"},
	}}
	got := strings.Join(peerLines(st, now), "\n")
	want := strings.Join([]string{
		"server id 4F2A-91C3 (" + st.ID + ")",
		"peer      mini  up 3m0s · 42 ms · tm 0.27.0 · relay r.example.net",
		"peer      nas  offline 4m0s · relay · dials again in 30s · relay r.example.net",
		"            dial tcp: connection refused",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
