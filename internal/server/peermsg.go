package server

// Coordinator links across servers (docs/SPEC.md §16.11): the link's
// peer.message reaches internal/link's Receive, and the outbox flusher
// delivers this server's queued messages over the links that are up;
// the fetcher pulls the files they offer (§16.13).

import (
	"context"
	"io"
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

// FileReady says the link to alias is up and its tm takes files.
func (t peerTransport) FileReady(alias string) bool {
	up, files := t.files(alias)
	return up && files
}

// files says whether the link to alias is up, and whether its tm takes
// files (link.PeerFiles).
func (t peerTransport) files(alias string) (up, files bool) {
	st := t.m.Status()
	if st == nil {
		return false, false
	}
	for _, l := range st.Peers {
		if l.Alias == alias {
			return l.State == proto.LinkUp, slices.Contains(l.Caps, peer.CapFile)
		}
	}
	return false, false
}

func (t peerTransport) FetchFile(ctx context.Context, alias, id string, offset int64, w io.Writer) (peer.FileEnd, error) {
	return t.m.FetchFile(ctx, alias, id, offset, w)
}

func (t peerTransport) Call(ctx context.Context, alias, method string, params, result any) error {
	return t.m.Call(ctx, alias, method, params, result)
}
