package peer

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/proto"
)

type fakeMessages struct {
	got []proto.PeerMessage
	c   caller.Caller
	id  ID
}

func (f *fakeMessages) Message(c caller.Caller, id ID, m proto.PeerMessage) (proto.PeerMessageResult, error) {
	f.got, f.c, f.id = append(f.got, m), c, id
	return proto.PeerMessageResult{Status: proto.PeerDelivered}, nil
}

// TestDispatcherMessage: peer.message reaches Messages as the paired
// server, with its id, alone when nothing is exported; without Messages
// it is unknown, and the hello lists CapMsg (and CapShare) only with it.
func TestDispatcherMessage(t *testing.T) {
	ca, cb, err := tcpPair()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ca.Close(); cb.Close() })
	sa, sb := newSession(ca), newSession(cb)
	sa.ping, sb.ping = time.Hour, time.Hour
	fm := &fakeMessages{}
	var pid ID
	pid[0] = 7
	sb.methods = (&dispatcher{msgs: fm, caller: caller.ForPeer("studio"), peer: pid}).methods()
	go sa.serve(func(time.Duration) {})
	go sb.serve(func(time.Duration) {})
	ctx := context.Background()
	var res proto.PeerMessageResult
	m := proto.PeerMessage{Epoch: "e", Seq: 1, Kind: proto.PeerMessageText, From: "acme", To: "api", ID: "M-0700-1", By: "coordinator", Text: "hi"}
	if err := sa.call(ctx, proto.MethodPeerMessage, m, &res); err != nil || res.Status != proto.PeerDelivered {
		t.Fatalf("peer.message: %+v %v", res, err)
	}
	if len(fm.got) != 1 || fm.got[0] != m || fm.c.Kind != caller.Peer || fm.c.Alias != "studio" || fm.id != pid {
		t.Fatalf("handed %+v as %+v %v", fm.got, fm.c, fm.id)
	}
	if err := sa.call(ctx, proto.MethodPeerWatch, map[string]string{"project": "web"}, nil); code(err) != proto.ErrUnknownMethod {
		t.Fatalf("peer.watch without an exporter: %v", err)
	}
	if ms := (&dispatcher{ex: newFakeExport()}).methods(); ms[proto.MethodPeerMessage] != nil {
		t.Fatal("peer.message without Messages")
	}
	if caps := (&Manager{opts: Options{Messages: fm}}).caps(); !slices.Contains(caps, CapMsg) || !slices.Contains(caps, CapShare) || slices.Contains(caps, CapWatch) {
		t.Fatalf("caps %v", caps)
	}
	if caps := (&Manager{}).caps(); slices.Contains(caps, CapMsg) || slices.Contains(caps, CapShare) {
		t.Fatalf("caps without Messages %v", caps)
	}
}
