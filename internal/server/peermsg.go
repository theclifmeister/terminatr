package server

// Coordinator links across servers (docs/SPEC.md §16.11): the link's
// peer.message reaches internal/link's Receive, and the outbox flusher
// delivers this server's queued messages over the links that are up.

import (
	"context"
	"slices"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/peer"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// peerMessages is the server as peer.Messages.
type peerMessages struct{}

func (peerMessages) Message(c caller.Caller, id peer.ID, m proto.PeerMessage) (proto.PeerMessageResult, error) {
	if c.Kind != caller.Peer {
		return proto.PeerMessageResult{}, proto.Errorf(proto.ErrRefused, "peer.message is a paired server's")
	}
	return link.Receive(c.Alias, id, m), nil
}

// peerTransport is the links as the outbox flusher needs them.
type peerTransport struct{ m *peer.Manager }

func (t peerTransport) Ready(alias string) bool {
	st := t.m.Status()
	if st == nil {
		return false
	}
	for _, l := range st.Peers {
		if l.Alias == alias {
			return l.State == proto.LinkUp && slices.Contains(l.Caps, peer.CapMsg)
		}
	}
	return false
}

func (t peerTransport) Call(ctx context.Context, alias, method string, params, result any) error {
	return t.m.Call(ctx, alias, method, params, result)
}
